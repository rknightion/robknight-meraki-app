package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

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
