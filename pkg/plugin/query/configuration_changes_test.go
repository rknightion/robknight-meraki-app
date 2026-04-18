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
			"ts":         ts1.Format(time.RFC3339Nano),
			"adminName":  "Alice",
			"adminEmail": "alice@example.com",
			"adminId":    "admin-1",
			"page":       "via API",
			"label":      "PUT /api/v1/organizations/123/networks/N_1",
			"oldValue":   `{"name":"Old"}`,
			"newValue":   `{"name":"New"}`,
			"networkId":  "N_1",
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
	// Expect 9 columns — one per documented Meraki field.
	if got := len(frame.Fields); got != 9 {
		t.Fatalf("expected 9 fields; got %d", got)
	}
	if got := frame.Fields[0].Len(); got != 2 {
		t.Fatalf("expected 2 rows; got %d", got)
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
