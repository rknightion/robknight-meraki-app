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

// §3.3 — Device memory usage history handler tests.

// TestHandle_DeviceMemoryHistory_EmitsPerSerialFrames verifies that one frame
// per serial is emitted with the correct label set and UsagePercent values.
func TestHandle_DeviceMemoryHistory_EmitsPerSerialFrames(t *testing.T) {
	const payload = `{
		"items": [
			{
				"serial": "Q2SW-MEMD-0001",
				"network": {"id": "N1", "name": "HQ"},
				"intervals": [
					{
						"startTs": "2026-04-17T10:00:00Z",
						"endTs":   "2026-04-17T10:05:00Z",
						"memory": {
							"used": {
								"maximum": 1024000,
								"percentages": {"maximum": 65.5}
							},
							"free": {"maximum": 512000}
						}
					},
					{
						"startTs": "2026-04-17T10:05:00Z",
						"endTs":   "2026-04-17T10:10:00Z",
						"memory": {
							"used": {
								"maximum": 1100000,
								"percentages": {"maximum": 72.3}
							},
							"free": {"maximum": 436000}
						}
					}
				]
			},
			{
				"serial": "Q2SW-MEMD-0002",
				"network": {"id": "N1", "name": "HQ"},
				"intervals": [
					{
						"startTs": "2026-04-17T10:00:00Z",
						"endTs":   "2026-04-17T10:05:00Z",
						"memory": {
							"used": {
								"maximum": 256000,
								"percentages": {"maximum": 25.0}
							},
							"free": {"maximum": 768000}
						}
					}
				]
			}
		],
		"meta": {"counts": {"items": {"total": 2, "remaining": 0}}}
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/devices/system/memory/usage/history") {
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
		Queries: []MerakiQuery{{RefID: "A", Kind: KindDeviceMemoryHistory, OrgID: "o1"}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	// 2 serials → 2 frames.
	if got := len(resp.Frames); got != 2 {
		t.Fatalf("got %d frames, want 2 (one per serial)", got)
	}

	seen := map[string]int{} // serial → row count
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
		if serial == "" {
			t.Fatalf("frame missing serial label; labels=%v", vf.Labels)
		}
		if metric := vf.Labels["metric"]; metric != "usagePercent" {
			t.Errorf("metric label = %q, want usagePercent", metric)
		}
		rows, _ := f.RowLen()
		seen[serial] = rows
	}

	if rows := seen["Q2SW-MEMD-0001"]; rows != 2 {
		t.Errorf("Q2SW-MEMD-0001: got %d rows, want 2", rows)
	}
	if rows := seen["Q2SW-MEMD-0002"]; rows != 1 {
		t.Errorf("Q2SW-MEMD-0002: got %d rows, want 1", rows)
	}
}

// TestHandle_DeviceMemoryHistory_ClampsMaxTimespan verifies that a window
// beyond the 31-day cap is clamped without error.
func TestHandle_DeviceMemoryHistory_ClampsMaxTimespan(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[],"meta":{"counts":{"items":{"total":0,"remaining":0}}}}`))
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
		Queries: []MerakiQuery{{RefID: "A", Kind: KindDeviceMemoryHistory, OrgID: "o1"}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if resp == nil {
		t.Fatal("nil response")
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
