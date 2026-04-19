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

// TestHandle_ConfigurationChanges_EmitsTableRows stubs the /configurationChanges endpoint
// with a two-entry payload and asserts the handler produces a single table frame with the
// expected column set and row count.
func TestHandle_ConfigurationChanges_EmitsTableRows(t *testing.T) {
	t.Parallel()

	ts1, _ := time.Parse(time.RFC3339, "2026-04-18T10:00:00Z")
	ts2, _ := time.Parse(time.RFC3339, "2026-04-18T11:30:00Z")
	body := []map[string]any{
		{
			"ts":          ts1.Format(time.RFC3339Nano),
			"adminName":   "Alice",
			"adminEmail":  "alice@example.com",
			"adminId":     "admin-1",
			"page":        "via API",
			"label":       "PUT /api/v1/organizations/123/networks/N_1",
			"oldValue":    `{"name":"Old"}`,
			"newValue":    `{"name":"New"}`,
			"networkId":   "N_1",
			"networkName": "HQ",
			"networkUrl":  "https://n1.meraki.com/HQ/manage",
			"ssidName":    "Guest",
			"ssidNumber":  0,
			"client":      map[string]any{"id": "c-1", "type": "api"},
		},
		{
			"ts":         ts2.Format(time.RFC3339Nano),
			"adminName":  "Bob",
			"adminEmail": "bob@example.com",
			"adminId":    "admin-2",
			"page":       "via dashboard",
			"label":      "Changed timezone",
			"oldValue":   `"UTC"`,
			"newValue":   `"America/Los_Angeles"`,
		},
	}
	var receivedPerPage string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/configurationChanges") {
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		receivedPerPage = r.URL.Query().Get("perPage")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)

	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	q := MerakiQuery{Kind: KindConfigurationChanges, OrgID: "123"}
	frames, err := handleConfigurationChanges(context.Background(), client, q, TimeRange{
		From: ts1.Add(-time.Hour).UnixMilli(),
		To:   ts2.Add(time.Hour).UnixMilli(),
	}, Options{})
	if err != nil {
		t.Fatalf("handleConfigurationChanges: %v", err)
	}
	if len(frames) != 1 {
		t.Fatalf("expected 1 frame; got %d", len(frames))
	}
	frame := frames[0]
	if frame.Name != "configuration_changes" {
		t.Fatalf("frame name %q, want configuration_changes", frame.Name)
	}
	// Expect 14 columns — the documented Meraki fields plus the derived clientType.
	wantCols := []string{
		"ts", "adminName", "adminEmail", "adminId",
		"networkName", "networkId", "ssidName", "ssidNumber",
		"page", "label", "oldValue", "newValue",
		"clientType", "networkUrl",
	}
	if got := len(frame.Fields); got != len(wantCols) {
		t.Fatalf("expected %d fields; got %d", len(wantCols), got)
	}
	for i, name := range wantCols {
		if got := frame.Fields[i].Name; got != name {
			t.Errorf("field[%d].Name = %q, want %q", i, got, name)
		}
	}
	if got := frame.Fields[0].Len(); got != 2 {
		t.Fatalf("expected 2 rows; got %d", got)
	}
	// Row 0 should have the enriched metadata populated.
	if got, _ := frame.Fields[4].At(0).(string); got != "HQ" {
		t.Errorf("networkName[0] = %q, want HQ", got)
	}
	if got, _ := frame.Fields[6].At(0).(string); got != "Guest" {
		t.Errorf("ssidName[0] = %q, want Guest", got)
	}
	if got, _ := frame.Fields[7].At(0).(*int64); got == nil || *got != 0 {
		t.Errorf("ssidNumber[0] = %v, want *int64(0)", got)
	}
	if got, _ := frame.Fields[12].At(0).(string); got != "api" {
		t.Errorf("clientType[0] = %q, want api", got)
	}
	// Row 1 is an org-level dashboard edit — networkName and ssidNumber must be empty/nil
	// so per-field noValue overrides on the frontend render a placeholder instead of the
	// panel-level "no changes" blurb leaking into the cell.
	if got, _ := frame.Fields[4].At(1).(string); got != "" {
		t.Errorf("networkName[1] = %q, want empty", got)
	}
	if got, _ := frame.Fields[7].At(1).(*int64); got != nil {
		t.Errorf("ssidNumber[1] = %v, want nil", got)
	}
	if got, _ := frame.Fields[12].At(1).(string); got != "" {
		t.Errorf("clientType[1] = %q, want empty", got)
	}
	// perPage default should round-trip as the documented 5000.
	if receivedPerPage != "5000" {
		t.Fatalf("perPage %q, want 5000", receivedPerPage)
	}
}

// TestHandle_ConfigurationChanges_RequiresOrgID guards the handler contract.
func TestHandle_ConfigurationChanges_RequiresOrgID(t *testing.T) {
	t.Parallel()

	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "k", BaseURL: "https://unused"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = handleConfigurationChanges(context.Background(), client, MerakiQuery{Kind: KindConfigurationChanges}, TimeRange{}, Options{})
	if err == nil {
		t.Fatalf("expected error for missing orgId")
	}
}
