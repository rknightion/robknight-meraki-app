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

// TestHandle_DeviceAvailabilitiesChangeHistory_EmitsTableFrame stubs the endpoint with a
// single online→offline transition and verifies the handler emits the expected columns,
// including the computed oldStatus/newStatus extraction from the details envelope and
// the drilldownUrl routed against productType.
func TestHandle_DeviceAvailabilitiesChangeHistory_EmitsTableFrame(t *testing.T) {
	t.Parallel()

	ts, _ := time.Parse(time.RFC3339, "2026-04-18T12:00:00Z")
	body := []map[string]any{
		{
			"ts": ts.Format(time.RFC3339Nano),
			"device": map[string]any{
				"serial":      "Q111-AAAA-0001",
				"name":        "AP-1",
				"productType": "wireless",
				"model":       "MR36",
			},
			"details": map[string]any{
				"old": []map[string]string{{"name": "status", "value": "online"}},
				"new": []map[string]string{{"name": "status", "value": "offline"}},
			},
			"network": map[string]any{
				"id":   "N_42",
				"name": "HQ",
			},
		},
	}
	var sawStatus, sawPerPage string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/devices/availabilities/changeHistory") {
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		sawStatus = r.URL.Query().Get("statuses[]")
		sawPerPage = r.URL.Query().Get("perPage")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)

	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	q := MerakiQuery{
		Kind:    KindDeviceAvailabilityChanges,
		OrgID:   "123",
		Metrics: []string{"offline"}, // push through as statuses[] filter
	}
	frames, err := handleDeviceAvailabilitiesChangeHistory(context.Background(), client, q, TimeRange{
		From: ts.Add(-time.Hour).UnixMilli(),
		To:   ts.Add(time.Hour).UnixMilli(),
	}, Options{PluginPathPrefix: "/a/rknightion-meraki-app"})
	if err != nil {
		t.Fatalf("handler returned %v", err)
	}
	if len(frames) != 1 {
		t.Fatalf("expected 1 frame; got %d", len(frames))
	}
	frame := frames[0]
	if frame.Name != "device_availability_changes" {
		t.Fatalf("frame name %q, want device_availability_changes", frame.Name)
	}
	// Expect 10 columns — see handler for the ordered list.
	if got := len(frame.Fields); got != 10 {
		t.Fatalf("expected 10 fields; got %d", got)
	}
	if got := frame.Fields[0].Len(); got != 1 {
		t.Fatalf("expected 1 row; got %d", got)
	}
	// Verify oldStatus / newStatus were extracted from details.
	oldStatus := frame.Fields[7].At(0).(string)
	newStatus := frame.Fields[8].At(0).(string)
	if oldStatus != "online" || newStatus != "offline" {
		t.Fatalf("expected online→offline transition, got %q → %q", oldStatus, newStatus)
	}
	// Verify drilldownUrl routes wireless to the access-points page.
	drill := frame.Fields[9].At(0).(string)
	if !strings.Contains(drill, "/access-points/Q111-AAAA-0001") {
		t.Fatalf("drilldownUrl %q should route wireless to /access-points/", drill)
	}
	// Verify the statuses filter was pushed down via q.Metrics.
	if sawStatus != "offline" {
		t.Fatalf("expected statuses[]=offline to be sent; got %q", sawStatus)
	}
	if sawPerPage != "1000" {
		t.Fatalf("expected default perPage=1000; got %q", sawPerPage)
	}
}

// TestHandle_DeviceAvailabilitiesChangeHistory_RequiresOrgID guards the handler contract.
func TestHandle_DeviceAvailabilitiesChangeHistory_RequiresOrgID(t *testing.T) {
	t.Parallel()

	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "k", BaseURL: "https://unused"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = handleDeviceAvailabilitiesChangeHistory(context.Background(), client, MerakiQuery{Kind: KindDeviceAvailabilityChanges}, TimeRange{}, Options{})
	if err == nil {
		t.Fatalf("expected error for missing orgId")
	}
}
