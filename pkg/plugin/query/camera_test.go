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
	}, Options{PluginPathPrefix: "/a/rknightion-meraki-app"})
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

	// Required columns per the plan.
	for _, name := range []string{"serial", "network_id", "network_name", "status", "updatedAt", "drilldownUrl"} {
		if f, _ := frame.FieldByName(name); f == nil {
			t.Fatalf("frame missing %q column; got fields=%v", name, frame.Fields)
		}
	}

	// drilldownUrl must be hard-coded to the "cameras" family route since the
	// handler passes productType="camera" explicitly.
	ddField, _ := frame.FieldByName("drilldownUrl")
	if got, _ := ddField.ConcreteAt(0); got != "/a/rknightion-meraki-app/cameras/Q2AA-AAAA-AAAA" {
		t.Fatalf("row 0 drilldownUrl = %v, want /a/rknightion-meraki-app/cameras/Q2AA-AAAA-AAAA", got)
	}
}

// TestHandle_CameraAnalyticsOverview_EmitsPerZoneFrames verifies that the
// overview handler fans out into one frame per (serial, zoneId) pair,
// carrying Prometheus-style labels on the value field. With 2 zones on one
// serial we expect 2 frames.
func TestHandle_CameraAnalyticsOverview_EmitsPerZoneFrames(t *testing.T) {
	const payload = `[
	  {"startTs":"2026-04-17T10:00:00Z","endTs":"2026-04-17T10:05:00Z","zoneId":"0","entrances":5,"averageCount":2.1},
	  {"startTs":"2026-04-17T10:05:00Z","endTs":"2026-04-17T10:10:00Z","zoneId":"0","entrances":7,"averageCount":2.5},
	  {"startTs":"2026-04-17T10:00:00Z","endTs":"2026-04-17T10:05:00Z","zoneId":"42","entrances":1,"averageCount":0.2},
	  {"startTs":"2026-04-17T10:05:00Z","endTs":"2026-04-17T10:10:00Z","zoneId":"42","entrances":3,"averageCount":0.7}
	]`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/camera/analytics/overview") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(payload))
			return
		}
		// /organizations/{orgId}/devices is called by resolveDeviceNames —
		// return an empty list so the fallback display-name path is exercised.
		if strings.HasSuffix(r.URL.Path, "/devices") {
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

	now := time.Now().UTC()
	resp, err := Handle(context.Background(), client, &QueryRequest{
		Range: TimeRange{
			From: now.Add(-1 * time.Hour).UnixMilli(),
			To:   now.UnixMilli(),
		},
		Queries: []MerakiQuery{{RefID: "A", Kind: KindCameraAnalyticsOverview, OrgID: "o1", Serials: []string{"Q2AA-AAAA-AAAA"}}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got := len(resp.Frames); got != 2 {
		t.Fatalf("got %d frames, want 2 (zones 0 + 42)", got)
	}

	// Each frame should carry the zone_id + serial + object_type labels on value.
	seenZones := map[string]struct{}{}
	for i, raw := range resp.Frames {
		var f data.Frame
		if err := json.Unmarshal(raw, &f); err != nil {
			t.Fatalf("decode frame %d: %v", i, err)
		}
		vf, _ := f.FieldByName("value")
		if vf == nil {
			t.Fatalf("frame[%d] missing value field; fields=%v", i, f.Fields)
		}
		if vf.Labels["serial"] == "" {
			t.Fatalf("frame[%d] value missing serial label; got %v", i, vf.Labels)
		}
		if got := vf.Labels["object_type"]; got != "person" {
			t.Fatalf("frame[%d] object_type = %q, want person (default)", i, got)
		}
		seenZones[vf.Labels["zone_id"]] = struct{}{}
	}
	if _, ok := seenZones["0"]; !ok {
		t.Fatalf("expected zone_id=0 label across frames; got %v", seenZones)
	}
	if _, ok := seenZones["42"]; !ok {
		t.Fatalf("expected zone_id=42 label across frames; got %v", seenZones)
	}
}

// TestHandle_CameraAnalyticsOverview_RespectsSevenDayCap verifies that when
// the panel time range exceeds the endpoint's 7-day MaxTimespan, the outbound
// t0/t1 query params get clamped to exactly 7 days. The handler path goes
// through KnownEndpointRanges[cameraAnalyticsEndpoint].Resolve which is the
// shared clamp logic — the test asserts the observable query string.
func TestHandle_CameraAnalyticsOverview_RespectsSevenDayCap(t *testing.T) {
	var gotT0, gotT1 string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/camera/analytics/overview") {
			gotT0 = r.URL.Query().Get("t0")
			gotT1 = r.URL.Query().Get("t1")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/devices") {
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

	// Ask for a 30-day window; Resolve must clamp it to 7d.
	now := time.Now().UTC()
	_, err = Handle(context.Background(), client, &QueryRequest{
		Range: TimeRange{
			From: now.Add(-30 * 24 * time.Hour).UnixMilli(),
			To:   now.UnixMilli(),
		},
		Queries: []MerakiQuery{{RefID: "A", Kind: KindCameraAnalyticsOverview, OrgID: "o1", Serials: []string{"Q2AA-AAAA-AAAA"}}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if gotT0 == "" || gotT1 == "" {
		t.Fatalf("expected t0/t1 to be sent; got t0=%q t1=%q", gotT0, gotT1)
	}

	t0, err := time.Parse(time.RFC3339, gotT0)
	if err != nil {
		t.Fatalf("parse t0 %q: %v", gotT0, err)
	}
	t1, err := time.Parse(time.RFC3339, gotT1)
	if err != nil {
		t.Fatalf("parse t1 %q: %v", gotT1, err)
	}

	// t1 - t0 must equal exactly 7 days within a small tolerance — the
	// FreshnessFloor subtraction and truncation together produce an exact
	// 7d span once the Resolve clamp kicks in.
	span := t1.Sub(t0)
	want := 7 * 24 * time.Hour
	if diff := span - want; diff < -time.Second || diff > time.Second {
		t.Fatalf("t1 - t0 = %s, want %s (±1s)", span, want)
	}
}
