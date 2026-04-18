package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// floorPlanTestServer multiplexes the three Meraki endpoints the
// sensorFloorPlan handler needs: /organizations/{o}/networks (to resolve
// the networks-in-org set that receives floor-plan probes), the per-network
// /networks/{n}/floorPlans call, and the /sensor/readings/latest feed that
// the handler joins against. Test cases control whether each endpoint
// returns a body by flipping the `payloads` map.
func floorPlanTestServer(t *testing.T, payloads map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for suffix, body := range payloads {
			if strings.Contains(r.URL.Path, suffix) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(body))
				return
			}
		}
		http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
	}))
	return srv
}

// TestHandle_SensorFloorPlan_WithAnchors asserts the happy path: a floor
// plan with anchor coordinates for each placed MT sensor yields a wide
// frame with populated x/y, one row per (serial, metric) reading.
func TestHandle_SensorFloorPlan_WithAnchors(t *testing.T) {
	floorPlans := `[
	  {
	    "floorPlanId":"g_1",
	    "name":"HQ Floor 1",
	    "center":{"lat":37.77,"lng":-122.38},
	    "bottomLeftCorner":{"lat":37.769,"lng":-122.381},
	    "topRightCorner":{"lat":37.771,"lng":-122.379},
	    "devices":[
	      {"serial":"Q-MT-0001","lat":37.7705,"lng":-122.3805,"productType":"sensor"}
	    ]
	  }
	]`
	readings := `[
	  {"serial":"Q-MT-0001","network":{"id":"N1","name":"Lab"},"readings":[
	    {"ts":"2026-04-17T10:00:00Z","metric":"temperature","temperature":{"celsius":21.5,"fahrenheit":70.7}},
	    {"ts":"2026-04-17T10:00:00Z","metric":"co2","co2":{"concentration":640}}
	  ]}
	]`
	srv := floorPlanTestServer(t, map[string]string{
		"/floorPlans":               floorPlans,
		"/sensor/readings/latest":   readings,
	})
	t.Cleanup(srv.Close)

	client, _ := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	resp, err := Handle(context.Background(), client, &QueryRequest{
		Queries: []MerakiQuery{{
			RefID: "A", Kind: KindSensorFloorPlan, OrgID: "o1", NetworkIDs: []string{"N1"},
		}},
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
		t.Fatalf("got %d rows, want 2 (temperature + co2 for one sensor)", rows)
	}
	// Assert field presence.
	for _, name := range []string{"floor_plan_id", "floor_plan_name", "serial", "metric", "value", "x", "y"} {
		if f, _ := frame.FieldByName(name); f == nil {
			t.Fatalf("frame missing field %q; got %v", name, frame.Fields)
		}
	}
	// Anchors present → x/y non-nil for both rows.
	xField, _ := frame.FieldByName("x")
	yField, _ := frame.FieldByName("y")
	for i := 0; i < rows; i++ {
		x := xField.At(i)
		y := yField.At(i)
		if xp, _ := x.(*float64); xp == nil {
			t.Fatalf("row %d: expected populated x, got nil (y=%v)", i, y)
		}
		if yp, _ := y.(*float64); yp == nil {
			t.Fatalf("row %d: expected populated y, got nil (x=%v)", i, x)
		}
	}
}

// TestHandle_SensorFloorPlan_WithoutAnchors asserts that a floor plan
// whose devices have no anchor coordinates still emits readings rows, but
// with nil x/y so the panel falls back to a grid layout.
func TestHandle_SensorFloorPlan_WithoutAnchors(t *testing.T) {
	// Device placed on the plan (serial matches a reading) but lat/lng omitted.
	floorPlans := `[
	  {
	    "floorPlanId":"g_1",
	    "name":"HQ Floor 1",
	    "devices":[{"serial":"Q-MT-0001","productType":"sensor"}]
	  }
	]`
	readings := `[
	  {"serial":"Q-MT-0001","network":{"id":"N1","name":"Lab"},"readings":[
	    {"ts":"2026-04-17T10:00:00Z","metric":"temperature","temperature":{"celsius":21.5,"fahrenheit":70.7}}
	  ]}
	]`
	srv := floorPlanTestServer(t, map[string]string{
		"/floorPlans":             floorPlans,
		"/sensor/readings/latest": readings,
	})
	t.Cleanup(srv.Close)

	client, _ := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	resp, err := Handle(context.Background(), client, &QueryRequest{
		Queries: []MerakiQuery{{
			RefID: "A", Kind: KindSensorFloorPlan, OrgID: "o1", NetworkIDs: []string{"N1"},
		}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	var frame data.Frame
	if err := json.Unmarshal(resp.Frames[0], &frame); err != nil {
		t.Fatalf("decode frame: %v", err)
	}
	rows, _ := frame.RowLen()
	if rows != 1 {
		t.Fatalf("got %d rows, want 1", rows)
	}
	xField, _ := frame.FieldByName("x")
	yField, _ := frame.FieldByName("y")
	if xp, _ := xField.At(0).(*float64); xp != nil {
		t.Fatalf("expected nil x without anchor, got %v", *xp)
	}
	if yp, _ := yField.At(0).(*float64); yp != nil {
		t.Fatalf("expected nil y without anchor, got %v", *yp)
	}
}

// TestHandle_SensorFloorPlan_NoPlanConfigured asserts the zero-plan branch:
// the handler emits a zero-row frame and attaches an informational notice
// so the UI renders a "no floor plan configured" message instead of a
// silent empty chart.
func TestHandle_SensorFloorPlan_NoPlanConfigured(t *testing.T) {
	srv := floorPlanTestServer(t, map[string]string{
		"/floorPlans":             `[]`,
		"/sensor/readings/latest": `[]`,
	})
	t.Cleanup(srv.Close)

	client, _ := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	resp, err := Handle(context.Background(), client, &QueryRequest{
		Queries: []MerakiQuery{{
			RefID: "A", Kind: KindSensorFloorPlan, OrgID: "o1", NetworkIDs: []string{"N1"},
		}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	var frame data.Frame
	if err := json.Unmarshal(resp.Frames[0], &frame); err != nil {
		t.Fatalf("decode frame: %v", err)
	}
	rows, _ := frame.RowLen()
	if rows != 0 {
		t.Fatalf("got %d rows, want 0 (no floor plan configured)", rows)
	}
	if frame.Meta == nil || len(frame.Meta.Notices) == 0 {
		t.Fatalf("expected a frame notice for no-floor-plan branch; got meta=%+v", frame.Meta)
	}
	found := false
	for _, n := range frame.Meta.Notices {
		if strings.Contains(strings.ToLower(n.Text), "no floor plan") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected 'no floor plan' notice text; got %+v", frame.Meta.Notices)
	}
}
