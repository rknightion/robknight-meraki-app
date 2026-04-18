package query

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// TestHandle_AlertsMttrSummary_ComputesMeanAndPercentiles stubs the assurance
// alerts feed with a mix of resolved + open alerts and asserts the summary
// handler emits a single wide frame with mean/p50/p95 durations plus counts.
func TestHandle_AlertsMttrSummary_ComputesMeanAndPercentiles(t *testing.T) {
	t.Parallel()

	base, _ := time.Parse(time.RFC3339, "2026-04-18T10:00:00Z")
	// Resolved durations (seconds): 60, 120, 300, 600, 3600. Mean = 936;
	// nearest-rank percentiles on n=5: p50 = rank 3 = 300, p95 = rank 5 = 3600.
	mkAlert := func(id string, startOffsetS, resolvedOffsetS int) map[string]any {
		m := map[string]any{
			"id":        id,
			"startedAt": base.Add(time.Duration(startOffsetS) * time.Second).Format(time.RFC3339Nano),
			"severity":  "warning",
		}
		if resolvedOffsetS > 0 {
			m["resolvedAt"] = base.Add(time.Duration(resolvedOffsetS) * time.Second).Format(time.RFC3339Nano)
		}
		return m
	}
	resp := []map[string]any{
		mkAlert("a1", 0, 60),      // 60s
		mkAlert("a2", 0, 120),     // 120s
		mkAlert("a3", 0, 300),     // 300s
		mkAlert("a4", 0, 600),     // 600s
		mkAlert("a5", 0, 3600),    // 3600s
		mkAlert("a6", 0, 0),       // open (no resolvedAt)
		mkAlert("a7", 0, 0),       // open
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/assurance/alerts") {
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)

	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	frames, err := handleAlertsMttrSummary(context.Background(), client, MerakiQuery{
		Kind:  KindAlertsMttrSummary,
		OrgID: "org-1",
	}, TimeRange{
		From: base.UnixMilli(),
		To:   base.Add(time.Hour).UnixMilli(),
	}, Options{})
	if err != nil {
		t.Fatalf("handleAlertsMttrSummary: %v", err)
	}
	if len(frames) != 1 {
		t.Fatalf("expected 1 frame; got %d", len(frames))
	}
	frame := frames[0]

	wantFields := []string{"mttrMeanSeconds", "mttrP50Seconds", "mttrP95Seconds", "resolvedCount", "openCount"}
	if got := len(frame.Fields); got != len(wantFields) {
		t.Fatalf("expected %d fields; got %d", len(wantFields), got)
	}
	for i, want := range wantFields {
		if got := frame.Fields[i].Name; got != want {
			t.Errorf("field[%d].Name = %q, want %q", i, got, want)
		}
	}
	if got := frame.Fields[0].Len(); got != 1 {
		t.Fatalf("expected 1 row; got %d", got)
	}

	get := func(idx int) float64 {
		switch v := frame.Fields[idx].At(0).(type) {
		case float64:
			return v
		case int64:
			return float64(v)
		}
		t.Fatalf("field[%d] unexpected type %T", idx, frame.Fields[idx].At(0))
		return 0
	}
	const eps = 0.001
	if mean := get(0); math.Abs(mean-936.0) > eps {
		t.Errorf("mttrMeanSeconds = %v, want ~936", mean)
	}
	if p50 := get(1); math.Abs(p50-300.0) > eps {
		t.Errorf("mttrP50Seconds = %v, want 300", p50)
	}
	if p95 := get(2); math.Abs(p95-3600.0) > eps {
		t.Errorf("mttrP95Seconds = %v, want 3600", p95)
	}
	if resolved := get(3); resolved != 5 {
		t.Errorf("resolvedCount = %v, want 5", resolved)
	}
	if open := get(4); open != 2 {
		t.Errorf("openCount = %v, want 2", open)
	}
}

// TestHandle_AlertsMttrSummary_EmptyFeedEmitsZeroRow guards the "no alerts"
// branch — a stat panel needs a row to render at all.
func TestHandle_AlertsMttrSummary_EmptyFeedEmitsZeroRow(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)

	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	frames, err := handleAlertsMttrSummary(context.Background(), client, MerakiQuery{Kind: KindAlertsMttrSummary, OrgID: "o"}, TimeRange{}, Options{})
	if err != nil {
		t.Fatalf("handleAlertsMttrSummary: %v", err)
	}
	if frames[0].Fields[0].Len() != 1 {
		t.Fatalf("expected 1 row even when feed is empty; got %d", frames[0].Fields[0].Len())
	}
}

// TestHandle_AlertsMttrSummary_RequiresOrgID guards the handler contract.
func TestHandle_AlertsMttrSummary_RequiresOrgID(t *testing.T) {
	t.Parallel()

	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "k", BaseURL: "https://unused"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = handleAlertsMttrSummary(context.Background(), client, MerakiQuery{Kind: KindAlertsMttrSummary}, TimeRange{}, Options{})
	if err == nil {
		t.Fatalf("expected error for missing orgId")
	}
}
