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

// TestHandle_Organizations is a round-trip smoke test: stub the Meraki
// /organizations endpoint, run Handle with a single organizations query,
// and confirm we get a well-formed frame back with the stubbed row.
func TestHandle_Organizations(t *testing.T) {
	const payload = `[{"id":"o1","name":"Lab","url":"https://dashboard.meraki.com/o/o1","api":{"enabled":true}}]`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/organizations") {
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
	}))
	t.Cleanup(srv.Close)

	client, err := meraki.NewClient(meraki.ClientConfig{
		APIKey:  "fake",
		BaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	resp, err := Handle(context.Background(), client, &QueryRequest{
		Queries: []MerakiQuery{{RefID: "A", Kind: KindOrganizations}},
	})
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

	rows, err := frame.RowLen()
	if err != nil {
		t.Fatalf("RowLen: %v", err)
	}
	if rows != 1 {
		t.Fatalf("got %d rows, want 1", rows)
	}

	idField, _ := frame.FieldByName("id")
	if idField == nil {
		t.Fatalf("frame missing id field; got fields=%v", frame.Fields)
	}
	if got, _ := idField.ConcreteAt(0); got != "o1" {
		t.Fatalf("row 0 id = %v, want o1", got)
	}
}

// TestHandle_SensorReadingsHistory confirms the history handler emits one
// frame per (serial, metric) pair with Prometheus-style labels on the value
// field — Grafana's timeseries viz relies on these labels to infer series
// grouping and legend names, so the shape is load-bearing for the chart to
// render at all.
func TestHandle_SensorReadingsHistory(t *testing.T) {
	// Two sensors, two metrics — expect 4 frames (one per pair).
	const payload = `[
	  {"ts":"2026-04-17T10:00:00Z","serial":"S1","metric":"temperature","network":{"id":"N1","name":"Lab"},"temperature":{"celsius":21.4,"fahrenheit":70.5}},
	  {"ts":"2026-04-17T10:05:00Z","serial":"S1","metric":"temperature","network":{"id":"N1","name":"Lab"},"temperature":{"celsius":21.5,"fahrenheit":70.7}},
	  {"ts":"2026-04-17T10:00:00Z","serial":"S1","metric":"humidity","network":{"id":"N1","name":"Lab"},"humidity":{"relativePercentage":55.0}},
	  {"ts":"2026-04-17T10:00:00Z","serial":"S2","metric":"temperature","network":{"id":"N1","name":"Lab"},"temperature":{"celsius":19.2,"fahrenheit":66.6}},
	  {"ts":"2026-04-17T10:05:00Z","serial":"S2","metric":"temperature","network":{"id":"N1","name":"Lab"},"temperature":{"celsius":19.4,"fahrenheit":66.9}}
	]`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/sensor/readings/history") {
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

	now := time.Now().UTC()
	resp, err := Handle(context.Background(), client, &QueryRequest{
		Range: TimeRange{
			From: now.Add(-6 * time.Hour).UnixMilli(),
			To:   now.UnixMilli(),
		},
		Queries: []MerakiQuery{{RefID: "A", Kind: KindSensorReadingsHistory, OrgID: "o1"}},
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got := len(resp.Frames); got != 3 {
		t.Fatalf("got %d frames, want 3 (S1/temp, S1/hum, S2/temp)", got)
	}

	// Pick any frame and verify labels + field shape.
	var frame data.Frame
	if err := json.Unmarshal(resp.Frames[0], &frame); err != nil {
		t.Fatalf("decode frame 0: %v", err)
	}
	if len(frame.Fields) != 2 {
		t.Fatalf("frame[0] has %d fields, want 2 (ts, value); got %v",
			len(frame.Fields), frame.Fields)
	}
	valueField, _ := frame.FieldByName("value")
	if valueField == nil {
		t.Fatalf("frame[0] missing value field")
	}
	if valueField.Labels["serial"] == "" {
		t.Fatalf("frame[0] value labels missing serial; got %v", valueField.Labels)
	}
	if valueField.Labels["metric"] == "" {
		t.Fatalf("frame[0] value labels missing metric; got %v", valueField.Labels)
	}
	if valueField.Labels["network_id"] != "N1" {
		t.Fatalf("frame[0] value labels network_id = %q, want N1", valueField.Labels["network_id"])
	}
	// First frame is deterministic (sorted): S1 + humidity comes before S1 + temperature.
	if got := valueField.Labels["serial"]; got != "S1" {
		t.Fatalf("frame[0] serial = %q, want S1 (sort order)", got)
	}
	if got := valueField.Labels["metric"]; got != "humidity" {
		t.Fatalf("frame[0] metric = %q, want humidity (sort order)", got)
	}
}
