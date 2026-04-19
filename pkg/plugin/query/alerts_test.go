package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// Status-filter + KPI-time-filter regression tests (see alerts.go's
// applyAlertsStatus helper and alertsStatusSentinel).
//
// Contract:
//   - Default sentinel (no metrics[1]) = "active" → active=true, tsStart/tsEnd
//     skipped so a long-running alert that started before the picker window
//     still shows up in KPI tiles and current-state tables.
//   - Explicit sentinel "all" → active=true&resolved=true&dismissed=true, and
//     tsStart/tsEnd applied (historical mode, e.g. the timeline bar chart).
//   - Explicit "resolved" / "dismissed" → just that boolean + time filter.

// TestHandle_Alerts_DefaultSentinel_IsActiveAndSkipsTime verifies the default
// (no metrics[1]) produces a "currently firing" snapshot — Active=true only,
// no tsStart/tsEnd — so active alerts are visible regardless of picker.
func TestHandle_Alerts_DefaultSentinel_IsActiveAndSkipsTime(t *testing.T) {
	var captured atomic.Pointer[url.Values]
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/assurance/alerts") {
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		q := r.URL.Query()
		captured.Store(&q)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)

	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	// Real picker window — the handler must ignore it in active mode.
	from := time.Now().Add(-2 * 24 * time.Hour)
	to := time.Now()
	if _, err := Handle(context.Background(), client, &QueryRequest{
		Range:   TimeRange{From: from.UnixMilli(), To: to.UnixMilli()},
		Queries: []MerakiQuery{{RefID: "A", Kind: KindAlerts, OrgID: "o1"}},
	}, Options{}); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	qs := captured.Load()
	if qs == nil {
		t.Fatal("stub never captured a request")
	}
	if got := qs.Get("active"); got != "true" {
		t.Errorf("active = %q, want true", got)
	}
	for _, k := range []string{"resolved", "dismissed"} {
		if got := qs.Get(k); got != "" {
			t.Errorf("%s = %q, want unset", k, got)
		}
	}
	for _, k := range []string{"tsStart", "tsEnd"} {
		if got := qs.Get(k); got != "" {
			t.Errorf("%s = %q, want unset in active mode", k, got)
		}
	}
}

// TestHandle_Alerts_AllSentinel_SendsExplicitBooleansAndNoTimeFilter
// asserts that an explicit "all" sentinel pushes
// active=true&resolved=true&dismissed=true but does NOT apply the picker
// window. Rationale: Meraki's tsStart filters on `alert.startedAt`, so
// narrowing the window hides long-running actives and resolved-in-window
// alerts whose startedAt is older. User feedback 2026-04-19 flagged the
// empty table on a 24h window despite MTTR showing 36 resolutions.
func TestHandle_Alerts_AllSentinel_SendsExplicitBooleansAndNoTimeFilter(t *testing.T) {
	var captured atomic.Pointer[url.Values]
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		captured.Store(&q)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)

	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	from := time.Now().Add(-7 * 24 * time.Hour)
	to := time.Now()
	if _, err := Handle(context.Background(), client, &QueryRequest{
		Range:   TimeRange{From: from.UnixMilli(), To: to.UnixMilli()},
		Queries: []MerakiQuery{{RefID: "A", Kind: KindAlerts, OrgID: "o1", Metrics: []string{"", "all"}}},
	}, Options{}); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	qs := captured.Load()
	if qs == nil {
		t.Fatal("stub never captured a request")
	}
	for _, k := range []string{"active", "resolved", "dismissed"} {
		if got := qs.Get(k); got != "true" {
			t.Errorf("query %s = %q, want %q", k, got, "true")
		}
	}
	if got := qs.Get("tsStart"); got != "" {
		t.Errorf("tsStart = %q, want empty string (all sentinel skips time filter)", got)
	}
	if got := qs.Get("tsEnd"); got != "" {
		t.Errorf("tsEnd = %q, want empty string (all sentinel skips time filter)", got)
	}
}

// TestHandle_Alerts_SpecificStatusSentinel_SendsOnlyThatOne confirms that a
// non-"all" sentinel ("resolved") sets exactly one boolean, matching the
// user-picked status tab.
func TestHandle_Alerts_SpecificStatusSentinel_SendsOnlyThatOne(t *testing.T) {
	var captured atomic.Pointer[url.Values]
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		captured.Store(&q)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)

	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	// Metrics[0]=severity, Metrics[1]="resolved" → only Resolved=true.
	if _, err := Handle(context.Background(), client, &QueryRequest{
		Queries: []MerakiQuery{{RefID: "A", Kind: KindAlerts, OrgID: "o1", Metrics: []string{"", "resolved"}}},
	}, Options{}); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	qs := captured.Load()
	if qs == nil {
		t.Fatal("stub never captured a request")
	}
	if got := qs.Get("resolved"); got != "true" {
		t.Errorf("resolved = %q, want true", got)
	}
	if got := qs.Get("active"); got != "" {
		t.Errorf("active = %q, want unset", got)
	}
	if got := qs.Get("dismissed"); got != "" {
		t.Errorf("dismissed = %q, want unset", got)
	}
}

// TestHandle_AlertsOverview_HitsCorrectEndpointAndOmitsTimeRange asserts that
// the KPI overview handler calls /assurance/alerts/overview (NOT /byType —
// that variant has no counts.bySeverity aggregate) and skips tsStart/tsEnd.
// The KPIs are a currently-firing snapshot; Meraki's tsStart/tsEnd filter on
// alert startedAt would hide long-running alerts that pre-date the window.
func TestHandle_AlertsOverview_HitsCorrectEndpointAndOmitsTimeRange(t *testing.T) {
	var captured atomic.Pointer[url.Values]
	var capturedPath atomic.Value // string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Reject /byType explicitly — the handler must use the sibling endpoint.
		if strings.Contains(r.URL.Path, "/assurance/alerts/overview/byType") {
			http.Error(w, "handler hit /byType but should use /overview", http.StatusBadRequest)
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/assurance/alerts/overview") {
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		q := r.URL.Query()
		captured.Store(&q)
		capturedPath.Store(r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"counts":{"total":0,"bySeverity":[]}}`))
	}))
	t.Cleanup(srv.Close)

	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	// Supply a real picker window — the handler must ignore it in default
	// (active) mode.
	from := time.Now().Add(-2 * 24 * time.Hour)
	to := time.Now()
	if _, err := Handle(context.Background(), client, &QueryRequest{
		Range:   TimeRange{From: from.UnixMilli(), To: to.UnixMilli()},
		Queries: []MerakiQuery{{RefID: "A", Kind: KindAlertsOverview, OrgID: "o1"}},
	}, Options{}); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	qs := captured.Load()
	if qs == nil {
		t.Fatal("stub never captured a request")
	}
	if got := qs.Get("tsStart"); got != "" {
		t.Errorf("tsStart = %q, want unset (KPI is a snapshot)", got)
	}
	if got := qs.Get("tsEnd"); got != "" {
		t.Errorf("tsEnd = %q, want unset (KPI is a snapshot)", got)
	}
	// Default sentinel is now "active" — only Active=true should be set.
	if got := qs.Get("active"); got != "true" {
		t.Errorf("active = %q, want true", got)
	}
	for _, k := range []string{"resolved", "dismissed"} {
		if got := qs.Get(k); got != "" {
			t.Errorf("%s = %q, want unset in default active mode", k, got)
		}
	}
}

// TestHandle_AlertsOverviewByNetwork_DefaultActive asserts that the byNetwork
// handler defaults to active-only with no time filter — same contract as the
// alerts list and KPI overview. Explicit "all" would enable time-filtered
// historical mode; default keeps the per-network table as a live snapshot.
func TestHandle_AlertsOverviewByNetwork_DefaultActive(t *testing.T) {
	var captured atomic.Pointer[url.Values]
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/assurance/alerts/overview/byNetwork") {
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		q := r.URL.Query()
		captured.Store(&q)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[],"meta":{"counts":{"items":0}}}`))
	}))
	t.Cleanup(srv.Close)

	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	from := time.Now().Add(-2 * 24 * time.Hour)
	to := time.Now()
	if _, err := Handle(context.Background(), client, &QueryRequest{
		Range:   TimeRange{From: from.UnixMilli(), To: to.UnixMilli()},
		Queries: []MerakiQuery{{RefID: "A", Kind: KindAlertsOverviewByNetwork, OrgID: "o1"}},
	}, Options{}); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	qs := captured.Load()
	if qs == nil {
		t.Fatal("stub never captured a request")
	}
	if got := qs.Get("active"); got != "true" {
		t.Errorf("active = %q, want true", got)
	}
	for _, k := range []string{"resolved", "dismissed", "tsStart", "tsEnd"} {
		if got := qs.Get(k); got != "" {
			t.Errorf("%s = %q, want unset in default active mode", k, got)
		}
	}
}

// §3.4 — Alerts overview byNetwork + historical handler tests.

// TestHandle_AlertsOverviewByNetwork_Table verifies the handler emits a single
// table frame with the correct column set and one row per network.
func TestHandle_AlertsOverviewByNetwork_Table(t *testing.T) {
	const payload = `{
		"items": [
			{
				"networkId": "N_aaa",
				"networkName": "HQ",
				"alertCount": 5,
				"severityCounts": [
					{"type": "critical", "count": 2},
					{"type": "warning",  "count": 3}
				]
			},
			{
				"networkId": "N_bbb",
				"networkName": "Branch",
				"alertCount": 1,
				"severityCounts": [
					{"type": "informational", "count": 1}
				]
			}
		],
		"meta": {"counts": {"items": 6}}
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/assurance/alerts/overview/byNetwork") {
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
	}))
	t.Cleanup(srv.Close)

	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	resp, err := Handle(context.Background(), client, &QueryRequest{
		Queries: []MerakiQuery{{RefID: "A", Kind: KindAlertsOverviewByNetwork, OrgID: "o1"}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got := len(resp.Frames); got != 1 {
		t.Fatalf("got %d frames, want 1", got)
	}

	var frame data.Frame
	if err := json.Unmarshal(resp.Frames[0], &frame); err != nil {
		t.Fatalf("decode frame: %v", err)
	}

	rows, _ := frame.RowLen()
	if rows != 2 {
		t.Fatalf("got %d rows, want 2", rows)
	}

	for _, col := range []string{"networkId", "networkName", "critical", "warning", "informational", "total"} {
		if f, _ := frame.FieldByName(col); f == nil {
			t.Errorf("frame missing %q column; fields=%v", col, frame.Fields)
		}
	}

	// Verify HQ row: critical=2, warning=3, total=5.
	networkIDField, _ := frame.FieldByName("networkId")
	criticalField, _ := frame.FieldByName("critical")
	warningField, _ := frame.FieldByName("warning")
	totalField, _ := frame.FieldByName("total")
	for i := 0; i < rows; i++ {
		nid, _ := networkIDField.ConcreteAt(i)
		if nid == "N_aaa" {
			if got, _ := criticalField.ConcreteAt(i); got.(int64) != 2 {
				t.Errorf("N_aaa critical = %v, want 2", got)
			}
			if got, _ := warningField.ConcreteAt(i); got.(int64) != 3 {
				t.Errorf("N_aaa warning = %v, want 3", got)
			}
			if got, _ := totalField.ConcreteAt(i); got.(int64) != 5 {
				t.Errorf("N_aaa total = %v, want 5", got)
			}
		}
	}
}

// TestHandle_AlertsOverviewHistorical_EmitsPerSeverityFrames verifies that three
// frames are emitted (one per severity bucket) with labels on the value field.
func TestHandle_AlertsOverviewHistorical_EmitsPerSeverityFrames(t *testing.T) {
	const payload = `{
		"items": [
			{
				"segmentStart": "2026-04-17T10:00:00Z",
				"totals": {"critical": 3, "warning": 5, "informational": 2}
			},
			{
				"segmentStart": "2026-04-17T11:00:00Z",
				"totals": {"critical": 1, "warning": 4, "informational": 3}
			}
		],
		"meta": {"counts": {"items": 2}}
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/assurance/alerts/overview/historical") {
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
	}))
	t.Cleanup(srv.Close)

	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	from := time.Now().Add(-6 * time.Hour)
	to := time.Now()
	resp, err := Handle(context.Background(), client, &QueryRequest{
		Range: TimeRange{
			From: from.UnixMilli(),
			To:   to.UnixMilli(),
		},
		Queries: []MerakiQuery{{RefID: "A", Kind: KindAlertsOverviewHistorical, OrgID: "o1"}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	// 3 frames: one per severity (critical, warning, informational).
	if got := len(resp.Frames); got != 3 {
		t.Fatalf("got %d frames, want 3 (one per severity)", got)
	}

	seen := map[string]int{}
	for _, raw := range resp.Frames {
		var f data.Frame
		if err := json.Unmarshal(raw, &f); err != nil {
			t.Fatalf("decode: %v", err)
		}
		vf, _ := f.FieldByName("value")
		if vf == nil {
			t.Fatalf("frame missing value field; fields=%v", f.Fields)
		}
		sev := vf.Labels["severity"]
		if sev == "" {
			t.Fatalf("frame missing severity label; labels=%v", vf.Labels)
		}
		rows, _ := f.RowLen()
		seen[sev] = rows
	}

	for _, sev := range []string{"critical", "warning", "informational"} {
		if seen[sev] != 2 {
			t.Errorf("severity %s: got %d rows, want 2", sev, seen[sev])
		}
	}
}

// TestHandle_AlertsOverviewHistorical_ClampsMaxTimespan verifies 31-day cap.
func TestHandle_AlertsOverviewHistorical_ClampsMaxTimespan(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[],"meta":{"counts":{"items":0}}}`))
	}))
	t.Cleanup(srv.Close)

	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	// 60-day range → clamped to 31 days.
	to := time.Now()
	from := to.Add(-60 * 24 * time.Hour)
	resp, err := Handle(context.Background(), client, &QueryRequest{
		Range: TimeRange{
			From: from.UnixMilli(),
			To:   to.UnixMilli(),
		},
		Queries: []MerakiQuery{{RefID: "A", Kind: KindAlertsOverviewHistorical, OrgID: "o1"}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if resp == nil {
		t.Fatal("nil response")
	}
	// 3 frames (one per severity) each with 0 rows.
	if got := len(resp.Frames); got != 3 {
		t.Fatalf("got %d frames, want 3", got)
	}
	// No error-level notices.
	for _, raw := range resp.Frames {
		var f data.Frame
		if err := json.Unmarshal(raw, &f); err != nil {
			continue
		}
		if f.Meta != nil {
			for _, n := range f.Meta.Notices {
				if n.Severity == data.NoticeSeverityError {
					t.Errorf("unexpected error notice: %s", n.Text)
				}
			}
		}
	}
}
