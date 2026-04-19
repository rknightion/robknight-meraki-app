package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// TestHandle_ConfigurationChangesTimeline_EmitsNumericWideFrame verifies that the timeline
// handler produces a wide frame with numeric per-page columns — the whole point of the
// kind is to give the timeseries viz a real number field so it stops reporting
// "data is missing a number field". The previous client-side groupingToMatrix transform
// emitted string cells; this test guards against regressing back to that shape.
func TestHandle_ConfigurationChangesTimeline_EmitsNumericWideFrame(t *testing.T) {
	t.Parallel()

	// Three changes inside a single bucket (events-timeline uses 1h buckets for a 24h
	// window) split across two page values — we expect two numeric columns summing to 3.
	base, _ := time.Parse(time.RFC3339, "2026-04-18T10:00:00Z")
	body := []map[string]any{
		{"ts": base.Format(time.RFC3339Nano), "page": "via API"},
		{"ts": base.Add(5 * time.Minute).Format(time.RFC3339Nano), "page": "via API"},
		{"ts": base.Add(10 * time.Minute).Format(time.RFC3339Nano), "page": "via dashboard"},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/configurationChanges") {
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)

	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	q := MerakiQuery{Kind: KindConfigurationChangesTimeline, OrgID: "123"}
	// 24h window → 1h buckets (47 buckets once quantised to exclusive end).
	frames, err := handleConfigurationChangesTimeline(context.Background(), client, q, TimeRange{
		From: base.Add(-time.Hour).UnixMilli(),
		To:   base.Add(23 * time.Hour).UnixMilli(),
	}, Options{})
	if err != nil {
		t.Fatalf("handleConfigurationChangesTimeline: %v", err)
	}
	if len(frames) != 1 {
		t.Fatalf("expected 1 frame; got %d", len(frames))
	}
	frame := frames[0]
	if frame.Name != "configuration_changes_timeline" {
		t.Errorf("frame name %q, want configuration_changes_timeline", frame.Name)
	}
	// Expect ts + two observed pages.
	if got := len(frame.Fields); got != 3 {
		t.Fatalf("expected 3 fields (ts + 2 pages); got %d", got)
	}
	// First field is the time column.
	if got := frame.Fields[0].Name; got != "ts" {
		t.Fatalf("field[0].Name = %q, want ts", got)
	}
	// Remaining fields MUST be int64 — Grafana timeseries rejects string value fields.
	var totalByPage = map[string]int64{}
	for i := 1; i < len(frame.Fields); i++ {
		f := frame.Fields[i]
		sum := int64(0)
		for j := 0; j < f.Len(); j++ {
			v, ok := f.At(j).(int64)
			if !ok {
				t.Fatalf("field %q value at row %d is %T, want int64", f.Name, j, f.At(j))
			}
			sum += v
		}
		totalByPage[f.Name] = sum
	}
	if got := totalByPage["via API"]; got != 2 {
		t.Errorf("via API total = %d, want 2", got)
	}
	if got := totalByPage["via dashboard"]; got != 1 {
		t.Errorf("via dashboard total = %d, want 1", got)
	}
}

// TestHandle_ConfigurationChangesTimeline_EmptyChangesEmitsNumericFallback guards the
// second symptom of the original bug: a window with no changes must still produce at
// least one numeric field so the timeseries viz renders its empty-state placeholder
// rather than the "missing a number field" error.
func TestHandle_ConfigurationChangesTimeline_EmptyChangesEmitsNumericFallback(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]"))
	}))
	t.Cleanup(srv.Close)

	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	now := time.Now().UTC()
	frames, err := handleConfigurationChangesTimeline(context.Background(), client,
		MerakiQuery{Kind: KindConfigurationChangesTimeline, OrgID: "123"},
		TimeRange{From: now.Add(-time.Hour).UnixMilli(), To: now.UnixMilli()}, Options{})
	if err != nil {
		t.Fatalf("handleConfigurationChangesTimeline: %v", err)
	}
	if len(frames) != 1 {
		t.Fatalf("expected 1 frame; got %d", len(frames))
	}
	frame := frames[0]
	// ts column + a single fallback numeric column so the viz still has a number field.
	if got := len(frame.Fields); got != 2 {
		t.Fatalf("expected 2 fields (ts + fallback); got %d", got)
	}
	if got := frame.Fields[1].Name; got != "changes" {
		t.Errorf("fallback field = %q, want changes", got)
	}
	for j := 0; j < frame.Fields[1].Len(); j++ {
		if _, ok := frame.Fields[1].At(j).(int64); !ok {
			t.Fatalf("fallback field row %d = %T, want int64", j, frame.Fields[1].At(j))
		}
	}
}

// TestHandle_ConfigurationChangesTimeline_RequiresOrgID guards the handler contract.
func TestHandle_ConfigurationChangesTimeline_RequiresOrgID(t *testing.T) {
	t.Parallel()

	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "k", BaseURL: "https://unused"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = handleConfigurationChangesTimeline(context.Background(), client,
		MerakiQuery{Kind: KindConfigurationChangesTimeline}, TimeRange{}, Options{})
	if err == nil {
		t.Fatalf("expected error for missing orgId")
	}
}
