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

// TestHandle_CameraOnboarding_Table verifies the onboarding handler emits a
// single table frame with the documented column set, per-row drilldownUrl
// populated (productType hard-coded to "camera"), and one row per camera in
// the stub payload.
func TestHandle_CameraOnboarding_Table(t *testing.T) {
	const payload = `[
	  {"serial":"Q2AA-AAAA-AAAA","network":{"id":"N1","name":"HQ"},"status":"complete","updatedAt":"2026-04-17T10:00:00Z"},
	  {"serial":"Q2AA-BBBB-BBBB","network":{"id":"N1","name":"HQ"},"status":"connected","updatedAt":"2026-04-17T11:00:00Z"}
	]`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/camera/onboarding/statuses") {
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
		Queries: []MerakiQuery{{RefID: "A", Kind: KindCameraOnboarding, OrgID: "o1"}},
	}, Options{PluginPathPrefix: "/a/robknight-meraki-app"})
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
	rows, _ := frame.RowLen()
	if rows != 2 {
		t.Fatalf("got %d rows, want 2", rows)
	}

	for _, name := range []string{"serial", "network_id", "network_name", "status", "updatedAt", "drilldownUrl"} {
		if f, _ := frame.FieldByName(name); f == nil {
			t.Fatalf("frame missing %q column; got fields=%v", name, frame.Fields)
		}
	}

	ddField, _ := frame.FieldByName("drilldownUrl")
	if got, _ := ddField.ConcreteAt(0); got != "/a/robknight-meraki-app/cameras/Q2AA-AAAA-AAAA" {
		t.Fatalf("row 0 drilldownUrl = %v, want /a/robknight-meraki-app/cameras/Q2AA-AAAA-AAAA", got)
	}
}

// TestHandle_CameraBoundaryAreas_Table verifies the areas handler flattens
// Meraki's `{serial, networkId, boundaries:{id,type,name,vertices}}` wire
// shape into a flat table frame with Kind="area" on every row.
func TestHandle_CameraBoundaryAreas_Table(t *testing.T) {
	const payload = `[
	  {"serial":"Q2AA-AAAA-AAAA","networkId":"N1","boundaries":{"id":"b-area-1","type":"area","name":"Entrance","vertices":[{"x":0.1,"y":0.2},{"x":0.3,"y":0.4}]}},
	  {"serial":"Q2AA-AAAA-AAAA","networkId":"N1","boundaries":{"id":"b-area-2","type":"area","name":"Lobby","vertices":[]}}
	]`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/camera/boundaries/areas/byDevice") {
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
		Queries: []MerakiQuery{{RefID: "A", Kind: KindCameraBoundaryAreas, OrgID: "o1", Serials: []string{"Q2AA-AAAA-AAAA"}}},
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
	kindField, _ := frame.FieldByName("kind")
	for i := 0; i < rows; i++ {
		if got, _ := kindField.ConcreteAt(i); got != "area" {
			t.Fatalf("row %d kind = %v, want area", i, got)
		}
	}
}

// TestHandle_CameraBoundaryLines_Table verifies the lines handler preserves
// the `directionVertex` field when present, emitting it as two nullable
// float columns.
func TestHandle_CameraBoundaryLines_Table(t *testing.T) {
	const payload = `[
	  {"serial":"Q2AA-AAAA-AAAA","networkId":"N1","boundaries":{"id":"b-line-1","type":"line","name":"Main crossing","vertices":[{"x":0.0,"y":0.5},{"x":1.0,"y":0.5}],"directionVertex":{"x":0.5,"y":0.75}}}
	]`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/camera/boundaries/lines/byDevice") {
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
		Queries: []MerakiQuery{{RefID: "A", Kind: KindCameraBoundaryLines, OrgID: "o1"}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	var frame data.Frame
	if err := json.Unmarshal(resp.Frames[0], &frame); err != nil {
		t.Fatalf("decode frame: %v", err)
	}
	dvXField, _ := frame.FieldByName("directionVertex_x")
	if got, _ := dvXField.ConcreteAt(0); got == nil {
		t.Fatalf("directionVertex_x row 0 should be populated")
	}
	kindField, _ := frame.FieldByName("kind")
	if got, _ := kindField.ConcreteAt(0); got != "line" {
		t.Fatalf("row 0 kind = %v, want line", got)
	}
}

// TestHandle_CameraDetectionsHistory_EmitsPerBoundaryObjectFrames verifies
// one frame is emitted per (boundaryId, objectType, direction) tuple when
// explicit boundary IDs are passed via q.Metrics[0].
func TestHandle_CameraDetectionsHistory_EmitsPerBoundaryObjectFrames(t *testing.T) {
	// One boundary × two object types (person, vehicle) = two entries, each
	// of which fans out to in+out series → 4 frames total.
	const payload = `[
	  {"boundaryId":"b1","type":"area","results":{"startTime":"2026-04-17T10:00:00Z","endTime":"2026-04-17T11:00:00Z","objectType":"person","in":5,"out":3}},
	  {"boundaryId":"b1","type":"area","results":{"startTime":"2026-04-17T10:00:00Z","endTime":"2026-04-17T11:00:00Z","objectType":"vehicle","in":2,"out":1}}
	]`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/camera/detections/history/byBoundary/byInterval") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(payload))
			return
		}
		http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	resp, err := Handle(context.Background(), client, &QueryRequest{
		Queries: []MerakiQuery{{
			RefID:   "A",
			Kind:    KindCameraDetectionsHistory,
			OrgID:   "o1",
			Metrics: []string{"b1"},
		}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got := len(resp.Frames); got != 4 {
		t.Fatalf("got %d frames, want 4 (b1 × {person,vehicle} × {in,out})", got)
	}
	seen := map[string]struct{}{}
	for _, raw := range resp.Frames {
		var f data.Frame
		if err := json.Unmarshal(raw, &f); err != nil {
			t.Fatalf("decode: %v", err)
		}
		vf, _ := f.FieldByName("value")
		if vf == nil {
			t.Fatalf("frame missing value field; fields=%v", f.Fields)
		}
		seen[vf.Labels["object_type"]+"/"+vf.Labels["direction"]] = struct{}{}
	}
	for _, want := range []string{"person/in", "person/out", "vehicle/in", "vehicle/out"} {
		if _, ok := seen[want]; !ok {
			t.Fatalf("expected a frame labelled %q; got %v", want, seen)
		}
	}
}

// TestHandle_CameraDetectionsHistory_EmptyWhenNoBoundaries verifies the
// handler short-circuits with an info-notice empty frame when no boundary
// IDs are resolvable from the serial (i.e. the camera has no boundaries
// configured).
func TestHandle_CameraDetectionsHistory_EmptyWhenNoBoundaries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Both /areas and /lines return empty lists — simulates a camera
		// with no boundaries configured.
		if strings.Contains(r.URL.Path, "/camera/boundaries/") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
			return
		}
		http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	resp, err := Handle(context.Background(), client, &QueryRequest{
		Queries: []MerakiQuery{{
			RefID:   "A",
			Kind:    KindCameraDetectionsHistory,
			OrgID:   "o1",
			Serials: []string{"Q2AA-AAAA-AAAA"},
		}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got := len(resp.Frames); got != 1 {
		t.Fatalf("got %d frames, want 1 (empty)", got)
	}
	var frame data.Frame
	if err := json.Unmarshal(resp.Frames[0], &frame); err != nil {
		t.Fatalf("decode: %v", err)
	}
	rows, _ := frame.RowLen()
	if rows != 0 {
		t.Fatalf("expected empty frame; got %d rows", rows)
	}
	if len(frame.Meta.Notices) == 0 {
		t.Fatalf("expected info-notice on empty frame; got %v", frame.Meta)
	}
}
