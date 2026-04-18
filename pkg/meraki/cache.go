package meraki

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
)

// TTLCache is a thread-safe LRU cache with per-entry TTL expiry, optional
// stale-while-revalidate grace, TTL jitter (to avoid synchronised expiry
// stampedes), per-org partitioning (so a chatty big-org operator can't evict
// a quiet small-org operator's entries before their TTL fires), and
// negative-404 caching (to stop repeated Meraki round-trips for endpoints
// the current API key doesn't have access to).
//
// It wraps golang-lru/v2 per partition and stores entries as opaque byte
// slices. Callers are expected to marshal their values to JSON (or similar)
// before inserting. Expired entries are lazily removed on Lookup; there is no
// background sweeper.
type TTLCache struct {
	cfg TTLCacheConfig

	mu        sync.Mutex
	orgCaches map[string]*lru.Cache[string, cacheEntry]
}

// TTLCacheConfig parameterizes a TTLCache. Zero fields take safe defaults.
type TTLCacheConfig struct {
	// PerOrgSize caps the number of entries held per-org partition. When zero, 512 is used.
	// The empty-string key ("") is the shared partition used for org-less endpoints like
	// /organizations (the listing itself) — it uses the same cap.
	PerOrgSize int

	// JitterRatio is the ±fraction applied to each TTL at Store time. 0 disables jitter.
	// Values outside [0, 1] are clamped. A ratio of 0.1 means each TTL is randomised in
	// [ttl*0.9, ttl*1.1].
	JitterRatio float64

	// NotFoundTTL is the lifetime of a negative-cached 404. Zero falls back to 60s.
	NotFoundTTL time.Duration

	// Clock is an optional override for time.Now. Tests use a frozen clock.
	Clock func() time.Time

	// Rand is an optional override producing a value in [0,1). Tests can pin this to 0.5 so
	// jitter cancels out to the nominal TTL.
	Rand func() float64
}

// DefaultTTLCachePerOrgSize is the default entries-per-org-partition cap.
const DefaultTTLCachePerOrgSize = 512

// DefaultTTLCacheNotFoundTTL is the fallback lifetime for negative-cached 404 responses.
const DefaultTTLCacheNotFoundTTL = 60 * time.Second

type cacheEntry struct {
	value    []byte
	expires  time.Time // freshness ends at this moment
	stale    time.Time // stale-grace ends; between expires and stale the entry is served with isStale=true
	notFound bool      // true iff this is a negative-cached 404 (value is nil/empty)
}

// CacheLookup describes the outcome of a Lookup call. Hit is true for both fresh and stale
// entries; Stale additionally signals the caller should trigger an async refresh. NotFound
// flags a negative-cached 404 so the client short-circuits without an HTTP request.
type CacheLookup struct {
	Value    []byte
	Hit      bool
	Stale    bool
	NotFound bool
}

// NewTTLCache creates a cache with per-org partitions of the given size. Jitter and
// stale-grace default to off; pass a TTLCacheConfig via NewTTLCacheWithConfig for the
// richer semantics. Kept as the primary constructor for backward compatibility.
func NewTTLCache(size int) (*TTLCache, error) {
	return NewTTLCacheWithConfig(TTLCacheConfig{PerOrgSize: size})
}

// NewTTLCacheWithConfig builds a TTLCache with full config surface. All zero fields fall
// back to documented defaults.
func NewTTLCacheWithConfig(cfg TTLCacheConfig) (*TTLCache, error) {
	if cfg.PerOrgSize <= 0 {
		cfg.PerOrgSize = DefaultTTLCachePerOrgSize
	}
	if cfg.JitterRatio < 0 {
		cfg.JitterRatio = 0
	}
	if cfg.JitterRatio > 1 {
		cfg.JitterRatio = 1
	}
	if cfg.NotFoundTTL <= 0 {
		cfg.NotFoundTTL = DefaultTTLCacheNotFoundTTL
	}
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	if cfg.Rand == nil {
		r := rand.New(rand.NewSource(time.Now().UnixNano())) //nolint:gosec // jitter, not crypto
		var rmu sync.Mutex
		cfg.Rand = func() float64 {
			rmu.Lock()
			defer rmu.Unlock()
			return r.Float64()
		}
	}
	return &TTLCache{
		cfg:       cfg,
		orgCaches: map[string]*lru.Cache[string, cacheEntry]{},
	}, nil
}

// partition returns (and lazily creates) the LRU partition for the given orgID. Must be
// called with c.mu held.
func (c *TTLCache) partition(orgID string) *lru.Cache[string, cacheEntry] {
	if part, ok := c.orgCaches[orgID]; ok {
		return part
	}
	// Ignore the error: golang-lru only returns an error when size <= 0, which we clamp above.
	part, _ := lru.New[string, cacheEntry](c.cfg.PerOrgSize)
	c.orgCaches[orgID] = part
	return part
}

// Lookup returns the entry for (orgID, key). Fresh hits set Hit=true, Stale=false. Stale
// entries (past expires but within stale-grace) set Hit=true, Stale=true — the caller
// should serve the value AND trigger an async refresh. Negative-404 entries set both Hit
// and NotFound.
func (c *TTLCache) Lookup(orgID, key string) CacheLookup {
	c.mu.Lock()
	defer c.mu.Unlock()
	part := c.partition(orgID)
	entry, ok := part.Get(key)
	if !ok {
		return CacheLookup{}
	}
	now := c.cfg.Clock()
	// Past stale-grace: evict and miss.
	if !entry.stale.IsZero() && now.After(entry.stale) {
		part.Remove(key)
		return CacheLookup{}
	}
	// No stale-grace configured and past TTL: evict and miss.
	if entry.stale.IsZero() && now.After(entry.expires) {
		part.Remove(key)
		return CacheLookup{}
	}
	result := CacheLookup{
		Value:    entry.value,
		Hit:      true,
		NotFound: entry.notFound,
	}
	if now.After(entry.expires) {
		result.Stale = true
	}
	return result
}

// Store records a fresh value under (orgID, key). ttl is the freshness window; staleGrace,
// when > 0, extends the entry lifetime past expiry so Lookup can serve stale values while
// the caller revalidates in the background. The stored TTL is jittered by ±JitterRatio
// (when configured) so parallel replicas don't all expire in lock-step.
func (c *TTLCache) Store(orgID, key string, value []byte, ttl, staleGrace time.Duration) {
	if ttl <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	jittered := c.applyJitter(ttl)
	now := c.cfg.Clock()
	expires := now.Add(jittered)
	stale := expires
	if staleGrace > 0 {
		stale = expires.Add(staleGrace)
	}
	c.partition(orgID).Add(key, cacheEntry{
		value:   value,
		expires: expires,
		stale:   stale,
	})
}

// StoreNotFound writes a negative-cache entry for (orgID, key) indicating the endpoint
// returned 404. Subsequent Lookup calls hit immediately with NotFound=true, letting the
// client short-circuit without a round-trip. ttl defaults to NotFoundTTL when zero.
//
// Only 404s are worth negative-caching: 401/403 indicate a key/permission problem that
// might resolve on the very next call once the user fixes config; 5xx are transient; 412
// is our own app-plugin signal that the key isn't configured and should resolve as soon
// as the user saves it. Callers are expected to gate this on HTTP 404 explicitly.
func (c *TTLCache) StoreNotFound(orgID, key string, ttl time.Duration) {
	if ttl <= 0 {
		ttl = c.cfg.NotFoundTTL
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	jittered := c.applyJitter(ttl)
	expires := c.cfg.Clock().Add(jittered)
	c.partition(orgID).Add(key, cacheEntry{
		value:    nil,
		expires:  expires,
		stale:    expires,
		notFound: true,
	})
}

// Get returns the cached value and true iff a FRESH (not stale, not 404) entry exists
// for key under the org-less partition. Kept for backward compatibility with tests and
// any legacy call sites; new code should use Lookup.
func (c *TTLCache) Get(key string) ([]byte, bool) {
	r := c.Lookup("", key)
	if !r.Hit || r.Stale || r.NotFound {
		return nil, false
	}
	return r.Value, true
}

// Set stores value under key with the given TTL in the org-less partition. Kept for
// backward compatibility. No stale-grace is applied — for SWR semantics use Store.
func (c *TTLCache) Set(key string, value []byte, ttl time.Duration) {
	c.Store("", key, value, ttl, 0)
}

// Purge removes all entries from every partition. Useful for tests and hot-reloads of
// plugin settings.
func (c *TTLCache) Purge() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, part := range c.orgCaches {
		part.Purge()
	}
}

// applyJitter returns d with ±JitterRatio randomised. d <= 0 returns d unchanged. Must be
// called with c.mu held (to serialize Rand calls against the shared RNG).
func (c *TTLCache) applyJitter(d time.Duration) time.Duration {
	if d <= 0 || c.cfg.JitterRatio == 0 {
		return d
	}
	factor := 1.0 + (c.cfg.Rand()*2-1)*c.cfg.JitterRatio
	if factor < 0 {
		factor = 0
	}
	return time.Duration(float64(d) * factor)
}

// CacheKey produces a deterministic key from orgID + path + query parameters.
// Params keys are sorted so the key is stable regardless of map iteration order.
func CacheKey(orgID, path string, params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+params[k])
	}
	raw, _ := json.Marshal(struct {
		OrgID  string `json:"o"`
		Path   string `json:"p"`
		Params string `json:"q"`
	}{
		OrgID:  orgID,
		Path:   path,
		Params: strings.Join(parts, "&"),
	})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
