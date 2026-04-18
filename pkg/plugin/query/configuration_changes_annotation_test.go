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

// TestHandle_ConfigurationChangesAnnotation_EmitsAnnotationFrame asserts the
// annotation handler returns a four-column frame (time, title, text, tags)
// shaped for a Grafana AnnotationDataLayer. Row content is opaque to Grafana
// beyond those columns — we assert the shape, not the cosmetic formatting.
func TestHandle_ConfigurationChangesAnnotation_EmitsAnnotationFrame(t *testing.T) {
	t.Parallel()

	ts1, _ := time.Parse(time.RFC3339, "2026-04-18T10:00:00Z")
	ts2, _ := time.Parse(time.RFC3339, "2026-04-18T11:30:00Z")
	body := []map[string]any{
		{
			"ts":        ts1.Format(time.RFC3339Nano),
			"adminName": "Alice",
			"adminId":   "admin-1",
			"page":      "Network-wide",
			"label":     "Changed SSID name",
			"oldValue":  `"GuestWifi"`,
			"newValue":  `"Guest-WiFi"`,
			"networkId": "N_1",
		},
		{
			"ts":        ts2.Format(time.RFC3339Nano),
			"adminName": "Bob",
			"adminId":   "admin-2",
			"page":      "Organization",
			"label":     "Added admin",
			"oldValue":  `""`,
			"newValue":  `"carol@example.com"`,
		},
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
	q := MerakiQuery{Kind: KindConfigurationChangesAnnotation, OrgID: "123"}
	frames, err := handleConfigurationChangesAnnotation(context.Background(), client, q, TimeRange{
		From: ts1.Add(-time.Hour).UnixMilli(),
		To:   ts2.Add(time.Hour).UnixMilli(),
	}, Options{})
	if err != nil {
		t.Fatalf("handleConfigurationChangesAnnotation: %v", err)
	}
	if len(frames) != 1 {
		t.Fatalf("expected 1 frame; got %d", len(frames))
	}
	frame := frames[0]

	wantFields := []string{"time", "title", "text", "tags"}
	if got := len(frame.Fields); got != len(wantFields) {
		t.Fatalf("expected %d fields; got %d", len(wantFields), got)
	}
	for i, want := range wantFields {
		if got := frame.Fields[i].Name; got != want {
			t.Errorf("field[%d].Name = %q, want %q", i, got, want)
		}
	}
	if got := frame.Fields[0].Len(); got != 2 {
		t.Fatalf("expected 2 rows; got %d", got)
	}

	// Row 0 sanity: title includes actor + label; tags includes network + page.
	title0, _ := frame.Fields[1].At(0).(string)
	if !strings.Contains(title0, "Alice") || !strings.Contains(title0, "Changed SSID name") {
		t.Errorf("title[0] = %q, want to contain Alice and Changed SSID name", title0)
	}
	tags0, _ := frame.Fields[3].At(0).(string)
	if !strings.Contains(tags0, "network:N_1") || !strings.Contains(tags0, "page:Network-wide") {
		t.Errorf("tags[0] = %q, want network:N_1 and page:Network-wide", tags0)
	}

	// Row 1: no networkId, so tag string should NOT contain a network:... entry.
	tags1, _ := frame.Fields[3].At(1).(string)
	if strings.Contains(tags1, "network:") {
		t.Errorf("tags[1] = %q, should not contain a network tag (org-level change)", tags1)
	}
	if !strings.Contains(tags1, "page:Organization") {
		t.Errorf("tags[1] = %q, want page:Organization", tags1)
	}
}

// TestHandle_ConfigurationChangesAnnotation_RequiresOrgID guards the handler
// contract — a missing OrgID must surface as an error (which runOne turns
// into a notice) rather than an empty successful frame.
func TestHandle_ConfigurationChangesAnnotation_RequiresOrgID(t *testing.T) {
	t.Parallel()

	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "k", BaseURL: "https://unused"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = handleConfigurationChangesAnnotation(context.Background(), client, MerakiQuery{Kind: KindConfigurationChangesAnnotation}, TimeRange{}, Options{})
	if err == nil {
		t.Fatalf("expected error for missing orgId")
	}
}
