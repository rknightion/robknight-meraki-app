package plugin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newTestGrafanaClient builds a GrafanaClient aimed at the given httptest
// server. Bypasses NewGrafanaClient because that path requires a real
// backend.GrafanaCfg populated from gRPC — tests care about Probe's HTTP
// behaviour, not the cfg plumbing.
func newTestGrafanaClient(baseURL string) *GrafanaClient {
	return &GrafanaClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   "test-token",
		hc:      &http.Client{Timeout: 2 * time.Second},
	}
}

func TestGrafanaClient_Probe(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		wantReady  bool
		wantReason string // substring match
		wantErr    bool
	}{
		{
			name:       "ready when provisioning endpoint returns 200",
			status:     http.StatusOK,
			wantReady:  true,
			wantReason: "ready",
		},
		{
			name:       "not ready on 401 — toggle off",
			status:     http.StatusUnauthorized,
			wantReady:  false,
			wantReason: "externalServiceAccounts",
		},
		{
			name:       "not ready on 403 — permission missing",
			status:     http.StatusForbidden,
			wantReady:  false,
			wantReason: "externalServiceAccounts",
		},
		{
			name:       "unreachable on 404",
			status:     http.StatusNotFound,
			wantReady:  false,
			wantReason: "404",
		},
		{
			name:       "unreachable on 500",
			status:     http.StatusInternalServerError,
			wantReady:  false,
			wantReason: "500",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got, want := r.URL.Path, "/api/v1/provisioning/alert-rules"; got != want {
					t.Fatalf("unexpected path: %q want %q", got, want)
				}
				if r.URL.Query().Get("limit") != "1" {
					t.Fatalf("probe must use limit=1 to stay cheap; got %q", r.URL.RawQuery)
				}
				if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
					t.Fatalf("Authorization header = %q, want %q", got, "Bearer test-token")
				}
				w.WriteHeader(tc.status)
			}))
			t.Cleanup(srv.Close)

			c := newTestGrafanaClient(srv.URL)
			ready, reason, err := c.Probe(context.Background())
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if ready != tc.wantReady {
				t.Fatalf("ready = %v, want %v (reason=%q)", ready, tc.wantReady, reason)
			}
			if !strings.Contains(reason, tc.wantReason) {
				t.Fatalf("reason = %q, want substring %q", reason, tc.wantReason)
			}
		})
	}
}

// TestGrafanaClient_Probe_TransportError confirms the classifier returns an
// error (rather than silently degrading) when the HTTP round-trip itself
// fails. CheckHealth uses this distinction to log at debug level instead of
// surfacing a misleading "unreachable — Grafana returned 0" message.
func TestGrafanaClient_Probe_TransportError(t *testing.T) {
	// Point at a non-routable address so the HTTP client fails fast on the
	// context deadline. Localhost on a reserved-high port yields connection
	// refused which is good enough to exercise the err-path.
	c := &GrafanaClient{
		baseURL: "http://127.0.0.1:1", // port 1 — nothing listens here
		token:   "test-token",
		hc:      &http.Client{Timeout: 200 * time.Millisecond},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	t.Cleanup(cancel)
	ready, _, err := c.Probe(ctx)
	if err == nil {
		t.Fatal("expected transport error, got nil")
	}
	if ready {
		t.Fatal("ready = true on transport error")
	}
}
