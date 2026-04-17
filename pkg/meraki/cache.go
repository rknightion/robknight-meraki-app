package meraki

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
)

// TTLCache is a thread-safe LRU cache with per-entry TTL expiry.
//
// It wraps golang-lru/v2 and stores entries as opaque byte slices. Callers are expected to
// marshal their values to JSON (or similar) before inserting. Expired entries are lazily
// removed on Get; there is no background sweeper.
type TTLCache struct {
	lru   *lru.Cache[string, cacheEntry]
	mu    sync.Mutex
	clock func() time.Time
}

type cacheEntry struct {
	value   []byte
	expires time.Time
}

// NewTTLCache creates a cache with the given maximum entry count.
func NewTTLCache(size int) (*TTLCache, error) {
	if size <= 0 {
		size = 1024
	}
	inner, err := lru.New[string, cacheEntry](size)
	if err != nil {
		return nil, err
	}
	return &TTLCache{lru: inner, clock: time.Now}, nil
}

// Get returns the cached value and true iff a non-expired entry exists for key.
func (c *TTLCache) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.lru.Get(key)
	if !ok {
		return nil, false
	}
	if c.clock().After(entry.expires) {
		c.lru.Remove(key)
		return nil, false
	}
	return entry.value, true
}

// Set stores value under key with the given TTL. A non-positive TTL skips caching.
func (c *TTLCache) Set(key string, value []byte, ttl time.Duration) {
	if ttl <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lru.Add(key, cacheEntry{value: value, expires: c.clock().Add(ttl)})
}

// Purge removes all entries. Useful for tests and hot-reloads of plugin settings.
func (c *TTLCache) Purge() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lru.Purge()
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
