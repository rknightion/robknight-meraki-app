package meraki

import (
	"bytes"
	"strconv"
	"sync"
	"testing"
	"time"
)

// frozenClock produces a deterministic time value that can be advanced by the test.
type frozenClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *frozenClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *frozenClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newFrozenClock() *frozenClock {
	return &frozenClock{now: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func TestTTLCacheSetGetWithinTTL(t *testing.T) {
	t.Parallel()

	c, err := NewTTLCache(8)
	if err != nil {
		t.Fatalf("NewTTLCache: %v", err)
	}
	clock := newFrozenClock()
	c.SetClock(clock.Now)

	c.Set("k", []byte("hello"), time.Minute)

	// Advance less than the TTL.
	clock.Advance(30 * time.Second)

	got, ok := c.Get("k")
	if !ok {
		t.Fatal("expected cache hit within TTL")
	}
	if !bytes.Equal(got, []byte("hello")) {
		t.Fatalf("value=%q, want %q", got, "hello")
	}
}

func TestTTLCacheSetZeroTTLNoop(t *testing.T) {
	t.Parallel()

	c, err := NewTTLCache(8)
	if err != nil {
		t.Fatalf("NewTTLCache: %v", err)
	}
	c.Set("k", []byte("hello"), 0)
	if _, ok := c.Get("k"); ok {
		t.Fatal("Set with TTL=0 should not cache anything")
	}

	c.Set("k2", []byte("hello"), -time.Second)
	if _, ok := c.Get("k2"); ok {
		t.Fatal("Set with negative TTL should not cache anything")
	}
}

func TestTTLCacheExpiry(t *testing.T) {
	t.Parallel()

	c, err := NewTTLCache(8)
	if err != nil {
		t.Fatalf("NewTTLCache: %v", err)
	}
	clock := newFrozenClock()
	c.SetClock(clock.Now)

	c.Set("k", []byte("hello"), time.Minute)
	clock.Advance(2 * time.Minute)

	if _, ok := c.Get("k"); ok {
		t.Fatal("expected cache miss after TTL expiry")
	}
	// Also verify a second Get still misses (entry was removed).
	if _, ok := c.Get("k"); ok {
		t.Fatal("second Get should still miss")
	}
}

func TestTTLCacheLRUEviction(t *testing.T) {
	t.Parallel()

	c, err := NewTTLCache(2)
	if err != nil {
		t.Fatalf("NewTTLCache: %v", err)
	}

	c.Set("a", []byte("1"), time.Minute)
	c.Set("b", []byte("2"), time.Minute)

	// Touch "a" so it becomes more-recently-used than "b".
	if _, ok := c.Get("a"); !ok {
		t.Fatal("expected hit for a")
	}

	// Adding a third entry should evict "b" (the LRU item).
	c.Set("c", []byte("3"), time.Minute)

	if _, ok := c.Get("b"); ok {
		t.Fatal("expected b to be evicted")
	}
	if _, ok := c.Get("a"); !ok {
		t.Fatal("expected a to still be present")
	}
	if _, ok := c.Get("c"); !ok {
		t.Fatal("expected c to be present")
	}
}

func TestTTLCachePurge(t *testing.T) {
	t.Parallel()

	c, err := NewTTLCache(8)
	if err != nil {
		t.Fatalf("NewTTLCache: %v", err)
	}
	for i := 0; i < 5; i++ {
		c.Set("k"+strconv.Itoa(i), []byte{byte(i)}, time.Minute)
	}
	c.Purge()
	for i := 0; i < 5; i++ {
		if _, ok := c.Get("k" + strconv.Itoa(i)); ok {
			t.Fatalf("k%d should have been purged", i)
		}
	}
}

func TestCacheKeyDeterministic(t *testing.T) {
	t.Parallel()

	// Build two maps with the same entries inserted in different orders.
	paramsA := map[string]string{
		"a": "1",
		"b": "2",
		"c": "3",
	}
	paramsB := map[string]string{
		"c": "3",
		"a": "1",
		"b": "2",
	}

	k1 := CacheKey("org", "/foo", paramsA)
	k2 := CacheKey("org", "/foo", paramsB)
	if k1 != k2 {
		t.Fatalf("CacheKey not deterministic: %s vs %s", k1, k2)
	}

	// Run many times to guard against accidental reliance on iteration order.
	for i := 0; i < 100; i++ {
		if got := CacheKey("org", "/foo", paramsA); got != k1 {
			t.Fatalf("iteration %d: non-deterministic key %s vs %s", i, got, k1)
		}
	}
}

func TestCacheKeyDistinguishesOrgID(t *testing.T) {
	t.Parallel()

	params := map[string]string{"x": "1"}
	a := CacheKey("orgA", "/foo", params)
	b := CacheKey("orgB", "/foo", params)
	if a == b {
		t.Fatalf("CacheKey should differ for different orgIDs, both got %s", a)
	}

	// Also sanity-check: same org+path with different params differs.
	paramsAlt := map[string]string{"x": "2"}
	c := CacheKey("orgA", "/foo", paramsAlt)
	if c == a {
		t.Fatalf("CacheKey should differ when params differ, both got %s", a)
	}
}

// TestTTLCache_StaleWhileRevalidate exercises the SWR lifecycle: fresh,
// stale-but-servable, evicted. Jitter is pinned to 0.5 (ratio cancels out) so
// the expected clock boundaries are deterministic.
func TestTTLCache_StaleWhileRevalidate(t *testing.T) {
	t.Parallel()

	c, err := NewTTLCacheWithConfig(TTLCacheConfig{
		PerOrgSize: 8,
		Rand:       func() float64 { return 0.5 },
	})
	if err != nil {
		t.Fatalf("NewTTLCacheWithConfig: %v", err)
	}
	clock := newFrozenClock()
	c.SetClock(clock.Now)

	c.Store("orgA", "k", []byte("v"), 60*time.Second, 30*time.Second)

	// Fresh hit: Stale=false.
	if r := c.Lookup("orgA", "k"); !r.Hit || r.Stale {
		t.Fatalf("expected fresh hit, got %+v", r)
	}
	// Past expires but within stale-grace: Hit=true, Stale=true.
	clock.Advance(75 * time.Second)
	r := c.Lookup("orgA", "k")
	if !r.Hit || !r.Stale {
		t.Fatalf("expected stale hit after 75s, got %+v", r)
	}
	if string(r.Value) != "v" {
		t.Fatalf("stale hit should carry the cached value, got %q", r.Value)
	}
	// Past stale-grace: evicted.
	clock.Advance(30 * time.Second)
	if r := c.Lookup("orgA", "k"); r.Hit {
		t.Fatalf("expected miss past stale-grace, got %+v", r)
	}
}

// TestTTLCache_NegativeCache ensures 404 entries surface as Hit+NotFound with
// empty Value, and expire after the configured NotFoundTTL.
func TestTTLCache_NegativeCache(t *testing.T) {
	t.Parallel()

	c, err := NewTTLCacheWithConfig(TTLCacheConfig{
		PerOrgSize:  8,
		NotFoundTTL: 120 * time.Second,
		Rand:        func() float64 { return 0.5 },
	})
	if err != nil {
		t.Fatalf("NewTTLCacheWithConfig: %v", err)
	}
	clock := newFrozenClock()
	c.SetClock(clock.Now)

	c.StoreNotFound("orgA", "k", 0) // 0 → default NotFoundTTL

	r := c.Lookup("orgA", "k")
	if !r.Hit || !r.NotFound {
		t.Fatalf("expected NotFound hit, got %+v", r)
	}
	if len(r.Value) != 0 {
		t.Fatalf("NotFound entry should have empty value, got %q", r.Value)
	}

	clock.Advance(121 * time.Second)
	if r := c.Lookup("orgA", "k"); r.Hit {
		t.Fatalf("expected NotFound entry to expire, got %+v", r)
	}
}

// TestTTLCache_PartitionedByOrg verifies a big-org operator cannot evict a
// small-org operator's entries before their TTL fires. Partition cap = 2 per
// org; after overflowing orgA's partition, orgB's entries must remain.
func TestTTLCache_PartitionedByOrg(t *testing.T) {
	t.Parallel()

	c, err := NewTTLCacheWithConfig(TTLCacheConfig{
		PerOrgSize: 2,
		Rand:       func() float64 { return 0.5 },
	})
	if err != nil {
		t.Fatalf("NewTTLCacheWithConfig: %v", err)
	}

	// orgA fills its partition and overflows it.
	c.Store("orgA", "a1", []byte("1"), time.Minute, 0)
	c.Store("orgA", "a2", []byte("2"), time.Minute, 0)
	c.Store("orgA", "a3", []byte("3"), time.Minute, 0) // evicts a1 (LRU, same partition)

	// orgB stores one entry; it must survive unaffected.
	c.Store("orgB", "b1", []byte("B"), time.Minute, 0)

	// Add many more orgA entries — should NOT touch orgB's partition.
	for i := 4; i < 10; i++ {
		c.Store("orgA", "a"+strconv.Itoa(i), []byte{byte(i)}, time.Minute, 0)
	}

	r := c.Lookup("orgB", "b1")
	if !r.Hit {
		t.Fatalf("orgB entry evicted by orgA activity; per-org partitioning broken")
	}
	// orgA's first entry must be evicted, latest retained.
	if r := c.Lookup("orgA", "a1"); r.Hit {
		t.Fatalf("orgA/a1 should have been evicted")
	}
	if r := c.Lookup("orgA", "a9"); !r.Hit {
		t.Fatalf("orgA/a9 should still be present")
	}
}

// TestTTLCache_TTLJitter exercises the jitter applied at Store time. With
// Rand()=1.0 (maximum positive jitter) and ratio=0.1, a 60s TTL should expand
// to 66s — so an entry that would normally expire at +60 still hits at +63.
func TestTTLCache_TTLJitter(t *testing.T) {
	t.Parallel()

	c, err := NewTTLCacheWithConfig(TTLCacheConfig{
		PerOrgSize:  8,
		JitterRatio: 0.1,
		Rand:        func() float64 { return 1.0 },
	})
	if err != nil {
		t.Fatalf("NewTTLCacheWithConfig: %v", err)
	}
	clock := newFrozenClock()
	c.SetClock(clock.Now)

	c.Store("orgA", "k", []byte("v"), 60*time.Second, 0)

	// At +63s: without jitter this would miss; with +10% jitter it still hits.
	clock.Advance(63 * time.Second)
	if r := c.Lookup("orgA", "k"); !r.Hit {
		t.Fatalf("expected hit at +63s with +10%% jitter on 60s TTL, got %+v", r)
	}
	// At +67s: past even the jittered 66s boundary — miss.
	clock.Advance(4 * time.Second)
	if r := c.Lookup("orgA", "k"); r.Hit {
		t.Fatalf("expected miss at +67s with +10%% jitter on 60s TTL, got %+v", r)
	}
}

// TestTTLCache_BackwardCompatibleGetSet sanity-checks that the legacy Get/Set
// surface still works (so the rest of the test suite — and any future call
// site that hasn't migrated to Store/Lookup — keeps compiling and behaving).
func TestTTLCache_BackwardCompatibleGetSet(t *testing.T) {
	t.Parallel()

	c, err := NewTTLCache(8)
	if err != nil {
		t.Fatalf("NewTTLCache: %v", err)
	}
	clock := newFrozenClock()
	c.SetClock(clock.Now)
	c.SetRand(func() float64 { return 0.5 })

	c.Set("k", []byte("v"), time.Minute)
	got, ok := c.Get("k")
	if !ok || string(got) != "v" {
		t.Fatalf("legacy Get returned ok=%v value=%q, want true + \"v\"", ok, got)
	}
	clock.Advance(2 * time.Minute)
	if _, ok := c.Get("k"); ok {
		t.Fatalf("legacy Get should have missed past TTL")
	}
}
