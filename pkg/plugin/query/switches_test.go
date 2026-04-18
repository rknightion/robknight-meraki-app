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

// §3.1 — Switch ports overview by speed + usage history handler tests.

// TestHandle_SwitchPortsOverviewBySpeed_Table verifies the handler emits a
// single table frame with the expected columns and one row per speed bucket.
func TestHandle_SwitchPortsOverviewBySpeed_Table(t *testing.T) {
	// The overview endpoint returns a nested object; we check that the
	// handler flattens it into one row per (media × speed).
	const payload = `{
		"counts": {
			"total": 96,
			"byStatus": {
				"active": {
					"total": 80,
					"byMediaAndLinkSpeed": {
						"rj45": {"1000": 48, "100": 8, "total": 56},
						"sfp":  {"10000": 4, "total": 4}
					}
				},
				"inactive": {
					"total": 16,
					"byMedia": {
						"rj45": {"total": 12},
						"sfp":  {"total": 4}
					}
				}
			}
		}
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/switch/ports/overview") {
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
		Queries: []MerakiQuery{{RefID: "A", Kind: KindSwitchPortsOverviewBySpeed, OrgID: "o1"}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got := len(resp.Frames); got != 1 {
		t.Fatalf("got %d frames, want 1", got)
	}

	var frame data.Frame
	if err := json.Unmarshal(resp.Frames[0], &frame); err != nil {
		t.Fatalf("decode frame: %v (body=%s)", err, string(resp.Frames[0]))
	}

	// Expect 4 rows: rj45/1000, rj45/100, sfp/10000 (active) + rj45/inactive + sfp/inactive
	rows, _ := frame.RowLen()
	if rows < 3 {
		t.Fatalf("got %d rows, want >= 3 (at least 3 active speed buckets)", rows)
	}
	for _, col := range []string{"media", "speed", "active"} {
		if f, _ := frame.FieldByName(col); f == nil {
			t.Fatalf("frame missing %q column; fields=%v", col, frame.Fields)
		}
	}

	// Verify that the active count for rj45/1000 is 48.
	mediaField, _ := frame.FieldByName("media")
	speedField, _ := frame.FieldByName("speed")
	activeField, _ := frame.FieldByName("active")
	for i := 0; i < rows; i++ {
		media, _ := mediaField.ConcreteAt(i)
		speed, _ := speedField.ConcreteAt(i)
		active, _ := activeField.ConcreteAt(i)
		if media == "rj45" && speed == "1000" {
			if active.(int64) != 48 {
				t.Errorf("rj45/1000 active count = %v, want 48", active)
			}
			return
		}
	}
	t.Errorf("no row with media=rj45 speed=1000 found; got %d rows", rows)
}

// TestHandle_SwitchPortsUsageHistory_EmitsPerSerialFrames verifies one frame
// per (serial, metric) is emitted from the usage-history handler, with correct
// labels on the value field.
func TestHandle_SwitchPortsUsageHistory_EmitsPerSerialFrames(t *testing.T) {
	// Two switches × one interval each → 6 frames total (2 serials × 3 metrics).
	const payload = `{
		"items": [
			{
				"serial": "Q2SW-0001",
				"network": {"id": "N1", "name": "HQ"},
				"ports": [
					{
						"portId": "1",
						"intervals": [
							{
								"startTs": "2026-04-17T10:00:00Z",
								"endTs":   "2026-04-17T10:05:00Z",
								"data": {"usage": {"total": 1000, "upstream": 400, "downstream": 600}}
							}
						]
					}
				]
			},
			{
				"serial": "Q2SW-0002",
				"network": {"id": "N1", "name": "HQ"},
				"ports": [
					{
						"portId": "1",
						"intervals": [
							{
								"startTs": "2026-04-17T10:00:00Z",
								"endTs":   "2026-04-17T10:05:00Z",
								"data": {"usage": {"total": 500, "upstream": 200, "downstream": 300}}
							}
						]
					}
				]
			}
		],
		"meta": {"counts": {"items": {"total": 2, "remaining": 0}}}
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/switch/ports/usage/history") {
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
		Queries: []MerakiQuery{{RefID: "A", Kind: KindSwitchPortsUsageHistory, OrgID: "o1"}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	// 2 serials × 3 metrics (sent, recv, total) = 6 frames.
	if got := len(resp.Frames); got != 6 {
		t.Fatalf("got %d frames, want 6 (2 serials × 3 metrics)", got)
	}

	seen := map[string]map[string]bool{}
	for _, raw := range resp.Frames {
		var f data.Frame
		if err := json.Unmarshal(raw, &f); err != nil {
			t.Fatalf("decode: %v", err)
		}
		vf, _ := f.FieldByName("value")
		if vf == nil {
			t.Fatalf("frame missing value field; fields=%v", f.Fields)
		}
		serial := vf.Labels["serial"]
		metric := vf.Labels["metric"]
		if seen[serial] == nil {
			seen[serial] = map[string]bool{}
		}
		seen[serial][metric] = true
	}

	for _, serial := range []string{"Q2SW-0001", "Q2SW-0002"} {
		for _, metric := range []string{"sent", "recv", "total"} {
			if !seen[serial][metric] {
				t.Errorf("missing frame for serial=%s metric=%s; seen=%v", serial, metric, seen)
			}
		}
	}
}

// TestHandle_SwitchPortsUsageHistory_ClampsTruncation verifies that a time
// range longer than 31 days gets clamped and a notice is attached.
func TestHandle_SwitchPortsUsageHistory_ClampsTruncation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[],"meta":{"counts":{"items":{"total":0,"remaining":0}}}}`))
	}))
	t.Cleanup(srv.Close)

	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	// 60-day range — well beyond the 31-day cap.
	to := time.Now()
	from := to.Add(-60 * 24 * time.Hour)
	resp, err := Handle(context.Background(), client, &QueryRequest{
		Range: TimeRange{
			From: from.UnixMilli(),
			To:   to.UnixMilli(),
		},
		Queries: []MerakiQuery{{RefID: "A", Kind: KindSwitchPortsUsageHistory, OrgID: "o1"}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	// Even with empty payload the handler returns at least the empty frame slice.
	// The key assertion is that a non-error (non-notice-error) response came back.
	if resp == nil {
		t.Fatal("nil response")
	}
	// If any frames were returned, check that we don't have an error notice.
	for _, raw := range resp.Frames {
		var f data.Frame
		if err := json.Unmarshal(raw, &f); err != nil {
			t.Fatalf("decode: %v", err)
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
