package query

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// TestHandleSensorAqiComposite covers the server-side AQI computation path.
// The piecewise-linear weights must match src/scene-helpers/sensorMetrics.ts
// so the bar-gauge panel's thresholds line up with what the documentation
// table claims; we pin a deterministic reading set and assert the score
// stays within ±0.5 of the hand-calculated expectation.
func TestHandleSensorAqiComposite(t *testing.T) {
	// Two sensors with different ambient air profiles:
	// - S1: CO₂=800, TVOC=300, PM2.5=5 → moderate/good inputs
	// - S2: CO₂=1200, TVOC=1500, PM2.5=40 → poor on every input
	// Expected sub-scores (piecewise-linear, 100 at good, 0 at bad):
	//   S1 CO₂ (good=600, bad=1500): 100 * (1 - (800-600)/(1500-600))   = 77.78
	//   S1 TVOC (good=220, bad=2200): 100 * (1 - (300-220)/(2200-220))  = 95.96
	//   S1 PM2.5 (good=10, bad=55): value <= good → 100
	//   S1 weighted: 0.30*77.78 + 0.35*95.96 + 0.35*100 ≈ 91.92
	//   S2 CO₂: 100 * (1 - (1200-600)/900) = 33.33
	//   S2 TVOC: 100 * (1 - (1500-220)/1980) = 35.35
	//   S2 PM2.5: 100 * (1 - (40-10)/45) = 33.33
	//   S2 weighted: 0.30*33.33 + 0.35*35.35 + 0.35*33.33 ≈ 34.04
	const readingsPayload = `[
	  {"serial":"S1","network":{"id":"N1","name":"Lab"},"readings":[
	    {"ts":"2026-04-19T10:00:00Z","metric":"co2","co2":{"concentration":800}},
	    {"ts":"2026-04-19T10:00:00Z","metric":"tvoc","tvoc":{"concentration":300}},
	    {"ts":"2026-04-19T10:00:00Z","metric":"pm25","pm25":{"concentration":5}}
	  ]},
	  {"serial":"S2","network":{"id":"N1","name":"Lab"},"readings":[
	    {"ts":"2026-04-19T10:00:00Z","metric":"co2","co2":{"concentration":1200}},
	    {"ts":"2026-04-19T10:00:00Z","metric":"tvoc","tvoc":{"concentration":1500}},
	    {"ts":"2026-04-19T10:00:00Z","metric":"pm25","pm25":{"concentration":40}}
	  ]}
	]`
	const devicesPayload = `[
	  {"serial":"S1","name":"Lounge","productType":"sensor"},
	  {"serial":"S2","name":"Garage","productType":"sensor"}
	]`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/sensor/readings/latest"):
			_, _ = w.Write([]byte(readingsPayload))
		case strings.Contains(r.URL.Path, "/devices"):
			_, _ = w.Write([]byte(devicesPayload))
		default:
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	frames, err := handleSensorAqiComposite(context.Background(), client, MerakiQuery{
		Kind:  KindSensorAqiComposite,
		OrgID: "o1",
	}, TimeRange{}, Options{})
	if err != nil {
		t.Fatalf("handleSensorAqiComposite: %v", err)
	}
	if len(frames) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(frames))
	}
	frame := frames[0]

	if len(frame.Fields) != 3 {
		t.Fatalf("expected 3 fields (serial, name, score), got %d: %v", len(frame.Fields), frame.Fields)
	}
	if frame.Fields[0].Name != "serial" || frame.Fields[1].Name != "name" || frame.Fields[2].Name != "score" {
		t.Fatalf("field order = %s/%s/%s, want serial/name/score",
			frame.Fields[0].Name, frame.Fields[1].Name, frame.Fields[2].Name)
	}
	if frame.Rows() != 2 {
		t.Fatalf("expected 2 rows, got %d", frame.Rows())
	}

	// Rows are sorted by ascending score: S2 (~34) first, S1 (~86) second.
	serial0, _ := frame.Fields[0].At(0).(string)
	name0, _ := frame.Fields[1].At(0).(string)
	score0, _ := frame.Fields[2].At(0).(float64)
	serial1, _ := frame.Fields[0].At(1).(string)
	score1, _ := frame.Fields[2].At(1).(float64)

	if serial0 != "S2" || serial1 != "S1" {
		t.Errorf("row order = %s/%s, want S2/S1 (worst first)", serial0, serial1)
	}
	if name0 != "Garage" {
		t.Errorf("S2 name = %q, want Garage (from /devices lookup)", name0)
	}
	if math.Abs(score0-34.04) > 0.5 {
		t.Errorf("S2 score = %.2f, want ≈34.04", score0)
	}
	if math.Abs(score1-91.92) > 0.5 {
		t.Errorf("S1 score = %.2f, want ≈91.92", score1)
	}
}

// TestHandleSensorAqiComposite_PartialInputs ensures a sensor that reports
// only a subset of the three inputs still gets a weighted score computed
// from the inputs present — matching the client-side behaviour documented
// on aqiCompositeScore.
func TestHandleSensorAqiComposite_PartialInputs(t *testing.T) {
	const readingsPayload = `[
	  {"serial":"S3","network":{"id":"N1","name":"Lab"},"readings":[
	    {"ts":"2026-04-19T10:00:00Z","metric":"co2","co2":{"concentration":800}}
	  ]}
	]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/devices") {
			_, _ = w.Write([]byte("[]"))
			return
		}
		_, _ = w.Write([]byte(readingsPayload))
	}))
	t.Cleanup(srv.Close)

	client, _ := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	frames, err := handleSensorAqiComposite(context.Background(), client, MerakiQuery{
		Kind: KindSensorAqiComposite, OrgID: "o1",
	}, TimeRange{}, Options{})
	if err != nil {
		t.Fatalf("handleSensorAqiComposite: %v", err)
	}
	if frames[0].Rows() != 1 {
		t.Fatalf("expected 1 row, got %d", frames[0].Rows())
	}
	// CO₂-only sub-score: 100 * (1 - (800-600)/(1500-600)) = 77.78
	score, _ := frames[0].Fields[2].At(0).(float64)
	if math.Abs(score-77.78) > 0.5 {
		t.Errorf("score = %.2f, want ≈77.78 (re-normalised over CO₂ only)", score)
	}
	// Name falls back to serial when /devices returns no entry.
	name, _ := frames[0].Fields[1].At(0).(string)
	if name != "S3" {
		t.Errorf("name = %q, want S3 (fallback)", name)
	}
}

// TestHandleSensorAqiComposite_RequiresOrg mirrors the guard on every
// org-scoped handler so misconfigured panels don't silently 200 with an
// empty frame the user would interpret as "no sensors reporting".
func TestHandleSensorAqiComposite_RequiresOrg(t *testing.T) {
	_, err := handleSensorAqiComposite(context.Background(), nil, MerakiQuery{Kind: KindSensorAqiComposite}, TimeRange{}, Options{})
	if err == nil {
		t.Fatalf("expected error for missing orgId")
	}
	if !strings.Contains(err.Error(), "orgId is required") {
		t.Errorf("got %q, want mention of orgId", err.Error())
	}
}

// TestHandleSensorReadingsHistory_EmptyFallbackShape pins the empty-frame
// shape the dispatcher now emits for zero-result sensor history queries.
// Grafana's statetimeline viz rejected the previous zero-row frame with a
// "Data does not have a time field" error, which surfaced on every sensor
// detail page that stacked a panel for a metric the sensor doesn't report.
// The replacement frame carries one typed time value and a nullable value
// column so the viz recognises the time field and falls back to the
// panel's `noValue` text.
func TestHandleSensorReadingsHistory_EmptyFallbackShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]"))
	}))
	t.Cleanup(srv.Close)

	client, _ := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	resp, err := Handle(context.Background(), client, &QueryRequest{
		Range: TimeRange{From: 1700000000000, To: 1700086400000},
		Queries: []MerakiQuery{{
			RefID: "A", Kind: KindSensorReadingsHistory, OrgID: "o1",
			Serials: []string{"Q3CC-HV6P-H5XK"}, Metrics: []string{"water"},
		}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(resp.Frames) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(resp.Frames))
	}
	var frame data.Frame
	if err := json.Unmarshal(resp.Frames[0], &frame); err != nil {
		t.Fatalf("decode frame: %v", err)
	}
	if len(frame.Fields) != 2 {
		t.Fatalf("expected 2 fields (ts, value), got %d", len(frame.Fields))
	}
	if frame.Fields[0].Type() != data.FieldTypeTime {
		t.Errorf("field[0] type = %v, want time — statetimeline needs this", frame.Fields[0].Type())
	}
	if frame.Fields[1].Type() != data.FieldTypeNullableFloat64 {
		t.Errorf("field[1] type = %v, want nullable float64 (so Grafana sees a null row)", frame.Fields[1].Type())
	}
	if frame.Rows() != 1 {
		t.Errorf("expected 1 row (null value) as an anchor for the time field, got %d", frame.Rows())
	}
}
