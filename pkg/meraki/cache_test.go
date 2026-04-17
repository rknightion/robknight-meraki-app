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
