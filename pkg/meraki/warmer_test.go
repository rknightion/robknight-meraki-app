package meraki

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// warmerTestServer returns a server that serves a fixed list of orgs and a
// per-org networks list, counting per-path hits so tests can assert on
// fan-out shape. Org IDs returned: "o1", "o2".
func warmerTestServer(t *testing.T, orgsHits, networksHits *int64) *httptest.Server {
	t.Helper()
	const orgsPayload = `[{"id":"o1","name":"A"},{"id":"o2","name":"B"}]`
	const networksPayload = `[{"id":"N1","organizationId":"%s","name":"Lab","productTypes":["switch"]}]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/organizations"):
			atomic.AddInt64(orgsHits, 1)
			_, _ = w.Write([]byte(orgsPayload))
		case strings.Contains(r.URL.Path, "/networks"):
			atomic.AddInt64(networksHits, 1)
			// Echo the org id back so tests can inspect routing.
			parts := strings.Split(r.URL.Path, "/")
			orgID := ""
			for i, p := range parts {
				if p == "organizations" && i+1 < len(parts) {
					orgID = parts[i+1]
					break
				}
			}
			_, _ = fmt.Fprintf(w, networksPayload, orgID)
		default:
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestWarmer_RefreshOnce_PopulatesCache asserts a single pass fetches the
// orgs list and one networks list per org. The cache is then warm: a
// follow-up Get for the same keys does not increment the hit counters.
func TestWarmer_RefreshOnce_PopulatesCache(t *testing.T) {
	t.Parallel()

	var orgsHits, networksHits int64
	srv := warmerTestServer(t, &orgsHits, &networksHits)

	cache, err := NewTTLCacheWithConfig(TTLCacheConfig{PerOrgSize: 16, JitterRatio: 0, Rand: func() float64 { return 0.5 }})
	if err != nil {
		t.Fatalf("NewTTLCacheWithConfig: %v", err)
	}
	client, err := NewClient(ClientConfig{APIKey: "k", BaseURL: srv.URL, Cache: cache})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	w := NewWarmer(client, WarmerConfig{OrgsTTL: time.Hour, NetworksTTL: 15 * time.Minute}, nil)

	if err := w.RefreshOnce(context.Background()); err != nil {
		t.Fatalf("RefreshOnce: %v", err)
	}
	if got := atomic.LoadInt64(&orgsHits); got != 1 {
		t.Errorf("orgsHits = %d, want 1", got)
	}
	if got := atomic.LoadInt64(&networksHits); got != 2 {
		t.Errorf("networksHits = %d, want 2 (one per org)", got)
	}

	// Second RefreshOnce inside the TTL window: every call should be a
	// cache hit, the upstream counts must not change.
	if err := w.RefreshOnce(context.Background()); err != nil {
		t.Fatalf("RefreshOnce (cached): %v", err)
	}
	if got := atomic.LoadInt64(&orgsHits); got != 1 {
		t.Errorf("orgsHits after cached pass = %d, want 1", got)
	}
	if got := atomic.LoadInt64(&networksHits); got != 2 {
		t.Errorf("networksHits after cached pass = %d, want 2", got)
	}
}

// TestWarmer_StartStop_RunsAtLeastOnce drives the loop with a
// fake Sleep that signals via a channel. After observing one tick we
// cancel; the loop must close `done` promptly and the cache must contain
// at least one fresh org list.
func TestWarmer_StartStop_RunsAtLeastOnce(t *testing.T) {
	t.Parallel()

	var orgsHits, networksHits int64
	srv := warmerTestServer(t, &orgsHits, &networksHits)

	cache, _ := NewTTLCacheWithConfig(TTLCacheConfig{PerOrgSize: 16, JitterRatio: 0, Rand: func() float64 { return 0.5 }})
	client, _ := NewClient(ClientConfig{APIKey: "k", BaseURL: srv.URL, Cache: cache})

	tick := make(chan struct{}, 4)
	cfg := WarmerConfig{
		OrgsTTL:     time.Hour,
		NetworksTTL: 15 * time.Minute,
		Interval:    time.Hour, // unused — Sleep is hijacked
		Sleep: func(ctx context.Context, _ time.Duration) {
			select {
			case tick <- struct{}{}:
			default:
			}
			<-ctx.Done()
		},
	}
	w := NewWarmer(client, cfg, nil)
	w.Start(context.Background())

	select {
	case <-tick:
	case <-time.After(2 * time.Second):
		t.Fatal("warmer never reached its sleep call")
	}

	stopped := make(chan struct{})
	go func() {
		w.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return within 2s of cancellation")
	}

	if got := atomic.LoadInt64(&orgsHits); got < 1 {
		t.Errorf("orgsHits = %d, want >= 1", got)
	}
	if got := atomic.LoadInt64(&networksHits); got < 2 {
		t.Errorf("networksHits = %d, want >= 2 (one per org)", got)
	}
}

// TestWarmer_RefreshOnce_PerOrgErrorDoesNotAbortPass: if one org's
// networks fetch fails, subsequent orgs in the same pass still get warmed.
func TestWarmer_RefreshOnce_PerOrgErrorDoesNotAbortPass(t *testing.T) {
	t.Parallel()

	const orgsPayload = `[{"id":"bad","name":"Bad"},{"id":"ok","name":"OK"}]`
	const networksPayload = `[{"id":"N1","organizationId":"ok","name":"Lab","productTypes":["switch"]}]`

	var okNetworksHits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/organizations"):
			_, _ = w.Write([]byte(orgsPayload))
		case strings.Contains(r.URL.Path, "/organizations/bad/networks"):
			http.Error(w, "boom", http.StatusInternalServerError)
		case strings.Contains(r.URL.Path, "/organizations/ok/networks"):
			atomic.AddInt64(&okNetworksHits, 1)
			_, _ = w.Write([]byte(networksPayload))
		default:
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	cache, _ := NewTTLCacheWithConfig(TTLCacheConfig{PerOrgSize: 16, JitterRatio: 0, Rand: func() float64 { return 0.5 }})
	client, _ := NewClient(ClientConfig{APIKey: "k", BaseURL: srv.URL, Cache: cache, MaxRetries: 1})

	w := NewWarmer(client, WarmerConfig{OrgsTTL: time.Hour, NetworksTTL: 15 * time.Minute}, nil)

	err := w.RefreshOnce(context.Background())
	if err == nil {
		t.Fatal("expected error from failing org, got nil")
	}
	if got := atomic.LoadInt64(&okNetworksHits); got < 1 {
		t.Errorf("okNetworksHits = %d, want >= 1 — failing org must not abort the pass", got)
	}
}

// TestWarmer_NewWarmer_DefaultsAndIntervalFloor confirms zero-valued
// fields fall back to the documented defaults and the interval can never
// drop below minWarmerInterval.
func TestWarmer_NewWarmer_DefaultsAndIntervalFloor(t *testing.T) {
	t.Parallel()

	w := NewWarmer(nil, WarmerConfig{}, nil)
	if w.cfg.OrgsTTL != defaultWarmerOrgsTTL {
		t.Errorf("OrgsTTL = %v, want %v", w.cfg.OrgsTTL, defaultWarmerOrgsTTL)
	}
	if w.cfg.NetworksTTL != defaultWarmerNetworksTTL {
		t.Errorf("NetworksTTL = %v, want %v", w.cfg.NetworksTTL, defaultWarmerNetworksTTL)
	}
	wantInterval := defaultWarmerNetworksTTL / 2
	if w.cfg.Interval != wantInterval {
		t.Errorf("Interval = %v, want %v (= min(orgs,networks)/2)", w.cfg.Interval, wantInterval)
	}

	// Pathological tiny TTLs must clamp to minWarmerInterval.
	w2 := NewWarmer(nil, WarmerConfig{OrgsTTL: time.Second, NetworksTTL: time.Second}, nil)
	if w2.cfg.Interval != minWarmerInterval {
		t.Errorf("Interval with tiny TTLs = %v, want clamp to %v", w2.cfg.Interval, minWarmerInterval)
	}
}

// TestWarmer_Stop_BeforeStart confirms Stop is a no-op when Start was
// never called — important because the App may build a Warmer before
// realising the API key is absent and skip Start.
func TestWarmer_Stop_BeforeStart(t *testing.T) {
	t.Parallel()
	w := NewWarmer(nil, WarmerConfig{OrgsTTL: time.Hour, NetworksTTL: 15 * time.Minute}, nil)
	w.Stop() // must not panic, must return promptly
}
