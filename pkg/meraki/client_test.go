package meraki

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newTestClient wires a Client up against an httptest server with a deterministic cache
// (no jitter) and no rate limiter. Caller supplies the handler. The returned cache is
// exposed so individual tests can drive clock/cache state directly.
func newTestClient(t *testing.T, handler http.Handler) (*Client, *TTLCache, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	cache, err := NewTTLCacheWithConfig(TTLCacheConfig{
		PerOrgSize:  16,
		JitterRatio: 0, // deterministic
		Rand:        func() float64 { return 0.5 },
	})
	if err != nil {
		t.Fatalf("NewTTLCacheWithConfig: %v", err)
	}
	client, err := NewClient(ClientConfig{
		APIKey:  "test-key",
		BaseURL: srv.URL,
		Cache:   cache,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client, cache, srv
}

// TestClient_Get_SingleflightCoalescesConcurrentMisses asserts N concurrent Get calls for
// the same cache key fan in to exactly one HTTP round-trip.
func TestClient_Get_SingleflightCoalescesConcurrentMisses(t *testing.T) {
	t.Parallel()

	var hits int64
	release := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt64(&hits, 1)
		<-release // hold the response until all goroutines have joined the singleflight
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	client, _, _ := newTestClient(t, handler)

	const N = 20
	var wg sync.WaitGroup
	errs := make(chan error, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var out map[string]bool
			if err := client.Get(context.Background(), "foo", "orgA", nil, time.Minute, &out); err != nil {
				errs <- err
				return
			}
			if !out["ok"] {
				errs <- nil
			}
		}()
	}

	// Give every goroutine a chance to reach the singleflight.
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Get returned %v", err)
		}
	}
	if got := atomic.LoadInt64(&hits); got != 1 {
		t.Fatalf("expected singleflight to coalesce to 1 HTTP hit; got %d", got)
	}
}

// TestClient_Get_NegativeCaches404 exercises the negative-404 path: one 404 response, then
// subsequent Get calls for the same key return NotFoundError without a second round-trip.
func TestClient_Get_NegativeCaches404(t *testing.T) {
	t.Parallel()

	var hits int64
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt64(&hits, 1)
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":["not there"]}`))
	})
	client, _, _ := newTestClient(t, handler)

	var out any
	err := client.Get(context.Background(), "missing", "orgA", nil, time.Minute, &out)
	if !IsNotFound(err) {
		t.Fatalf("first call: expected NotFoundError, got %T: %v", err, err)
	}
	// Second call should short-circuit on the negative cache without another round-trip.
	err = client.Get(context.Background(), "missing", "orgA", nil, time.Minute, &out)
	if !IsNotFound(err) {
		t.Fatalf("second call: expected NotFoundError, got %T: %v", err, err)
	}
	if got := atomic.LoadInt64(&hits); got != 1 {
		t.Fatalf("expected 1 HTTP hit with negative cache; got %d", got)
	}
}

// TestClient_Get_DoesNotCache401 — 401s must NOT be negative-cached, because a bad API
// key is often fixed immediately afterwards. Each subsequent call should still round-trip.
func TestClient_Get_DoesNotCache401(t *testing.T) {
	t.Parallel()

	var hits int64
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt64(&hits, 1)
		w.WriteHeader(http.StatusUnauthorized)
	})
	client, _, _ := newTestClient(t, handler)

	var out any
	_ = client.Get(context.Background(), "locked", "orgA", nil, time.Minute, &out)
	_ = client.Get(context.Background(), "locked", "orgA", nil, time.Minute, &out)
	if got := atomic.LoadInt64(&hits); got != 2 {
		t.Fatalf("expected both calls to round-trip (401 not cached); got %d", got)
	}
}

// TestClient_Get_StaleWhileRevalidate_ServesStaleThenRefreshes primes the cache with a
// stale entry and asserts (a) Get returns the stale value immediately and (b) an async
// refresh populates the cache with the new value afterwards.
func TestClient_Get_StaleWhileRevalidate_ServesStaleThenRefreshes(t *testing.T) {
	t.Parallel()

	var hits int64
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt64(&hits, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"value":"fresh"}`))
	})
	client, cache, _ := newTestClient(t, handler)

	clock := newFrozenClock()
	cache.SetClock(clock.Now)

	// Seed a cache entry that is already stale: TTL=60s + stale-grace=30s, advance past TTL.
	// Note: Get's internal urlValuesToMap(nil) produces an empty map — CacheKey hashes that
	// identically to the nil param passed to Store, so the two key computations collide.
	key := CacheKey("orgA", "foo", map[string]string{})
	cache.Store("orgA", key, []byte(`{"value":"stale"}`), 60*time.Second, 30*time.Second)
	// Pre-check: Lookup must see this entry as stale right after advancing the clock.
	clock.Advance(70 * time.Second)
	if r := cache.Lookup("orgA", key); !r.Hit || !r.Stale {
		t.Fatalf("test setup broken: expected stale hit in cache before client.Get; got %+v", r)
	}

	var out map[string]string
	if err := client.Get(context.Background(), "foo", "orgA", nil, time.Minute, &out); err != nil {
		t.Fatalf("Get returned %v", err)
	}
	if out["value"] != "stale" {
		t.Fatalf("expected stale value served immediately, got %q", out["value"])
	}
	// The async refresh runs on context.Background with no freshness floor; its cache
	// Store uses the real (frozen) clock. Give it up to 1s to complete.
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt64(&hits) >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := atomic.LoadInt64(&hits); got != 1 {
		t.Fatalf("expected async refresh to fire exactly once; got %d", got)
	}
}

// TestClient_GetAll_SingleflightCoalescesPaginatedWalks mirrors the single-page test but
// against the multi-page walk — concurrent callers must share the full pagination pass.
func TestClient_GetAll_SingleflightCoalescesPaginatedWalks(t *testing.T) {
	t.Parallel()

	var hits int64
	release := make(chan struct{})
	page := 0
	var pageMu sync.Mutex
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		<-release
		pageMu.Lock()
		page++
		current := page
		pageMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		// Page 1 returns a single element with a Link rel=next header pointing at the same
		// server; page 2 returns an empty array and no Link header.
		if current == 1 {
			next := r.URL.Scheme + "://" + r.Host + r.URL.Path + "?cursor=p2"
			w.Header().Set("Link", `<`+next+`>; rel="next"`)
			_, _ = w.Write([]byte(`[{"id":1}]`))
			return
		}
		_, _ = w.Write([]byte(`[]`))
	})
	client, _, _ := newTestClient(t, handler)

	const N = 10
	var wg sync.WaitGroup
	errs := make(chan error, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var out []map[string]int
			if _, err := client.GetAll(context.Background(), "items", "orgA", url.Values{"perPage": []string{"1"}}, time.Minute, &out); err != nil {
				errs <- err
				return
			}
			if len(out) != 1 || out[0]["id"] != 1 {
				errs <- nil
			}
		}()
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent GetAll returned %v", err)
		}
	}
	// Exactly 2 HTTP hits: one per page, shared by all 10 callers.
	if got := atomic.LoadInt64(&hits); got != 2 {
		t.Fatalf("expected singleflight to coalesce multi-page walk to 2 HTTP hits; got %d", got)
	}
}

// TestClient_IPRateLimiter_AppliesBeforeOrgLimiter verifies both limiters fire on every
// request, and that the IP bucket rejects when the 100 rps (200 burst) ceiling is hit even
// while the per-org bucket still has tokens.
func TestClient_IPRateLimiter_AppliesBeforeOrgLimiter(t *testing.T) {
	t.Parallel()

	clock := newFakeClock(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	var ipSlept, orgSlept int64
	ipLimiter := NewRateLimiter(RateLimiterConfig{
		RequestsPerSecond: 2,
		Burst:             1,
		JitterRatio:       0,
		Clock:             clock.Now,
		Sleep:             fakeSleep(clock, &ipSlept),
	})
	orgLimiter := NewRateLimiter(RateLimiterConfig{
		RequestsPerSecond: 100,
		Burst:             100,
		JitterRatio:       0,
		Clock:             clock.Now,
		Sleep:             fakeSleep(clock, &orgSlept),
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	client, err := NewClient(ClientConfig{
		APIKey:        "test",
		BaseURL:       srv.URL,
		RateLimiter:   orgLimiter,
		IPRateLimiter: ipLimiter,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	// First call fits inside the IP burst (size 1) — no wait.
	if _, _, err := client.Do(context.Background(), http.MethodGet, "foo", "orgA", nil, nil); err != nil {
		t.Fatalf("first Do: %v", err)
	}
	// Second call exhausts the IP bucket. With rate=2/s we expect ~500ms of wait attributed
	// to the IP limiter; the org bucket should stay unused (100 rps leaves tokens spare).
	if _, _, err := client.Do(context.Background(), http.MethodGet, "foo", "orgA", nil, nil); err != nil {
		t.Fatalf("second Do: %v", err)
	}
	if atomic.LoadInt64(&ipSlept) == 0 {
		t.Fatalf("expected IP limiter to record waiting, got 0 slept ns")
	}
	if atomic.LoadInt64(&orgSlept) != 0 {
		t.Fatalf("expected org limiter to NOT wait; got %v", time.Duration(atomic.LoadInt64(&orgSlept)))
	}
}

// TestClient_UserAgent verifies the client sends the spec-compliant Meraki User-Agent.
func TestClient_UserAgent(t *testing.T) {
	t.Parallel()

	var seen string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	})
	client, _, _ := newTestClient(t, handler)

	var out any
	if err := client.Get(context.Background(), "foo", "orgA", nil, 0, &out); err != nil {
		t.Fatalf("Get: %v", err)
	}
	want := BuildUserAgent()
	if seen != want {
		t.Fatalf("User-Agent %q, want %q", seen, want)
	}
	// Sanity: match the exact §7.2 target.
	if want != "GrafanaMerakiPlugin/"+ClientVersion+" rknightion" {
		t.Fatalf("BuildUserAgent() %q does not match expected format", want)
	}
}

