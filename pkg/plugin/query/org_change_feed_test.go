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

// TestHandle_OrgChangeFeed_UnionsAuditAndEvents verifies the handler unions
// configuration-changes (source=audit) and network events (source=event),
// filters events to severity≥warn via the severityForEvent heuristic, sorts
// by time descending, and caps at orgChangeFeedRowCap rows.
func TestHandle_OrgChangeFeed_UnionsAuditAndEvents(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	auditResp := []map[string]any{
		{
			"ts":        now.Add(-1 * time.Hour).Format(time.RFC3339Nano),
			"adminName": "Alice",
			"label":     "Changed SSID",
			"page":      "Network-wide",
			"oldValue":  `"a"`,
			"newValue":  `"b"`,
			"networkId": "N1",
		},
	}
	networksResp := []map[string]any{
		{"id": "N1", "name": "Main"},
	}
	eventsResp := map[string]any{
		"events": []map[string]any{
			{
				"occurredAt":   now.Add(-30 * time.Minute).Format(time.RFC3339Nano),
				"type":         "port_failed",
				"category":     "Errors",
				"description":  "Port 4 failed",
				"deviceSerial": "Q1",
				"deviceName":   "switch-1",
				"networkId":    "N1",
			},
			{
				// Info event — should be ELIDED by severity filter.
				"occurredAt":   now.Add(-10 * time.Minute).Format(time.RFC3339Nano),
				"type":         "association",
				"category":     "wireless",
				"description":  "Client associated",
				"deviceSerial": "AP1",
				"networkId":    "N1",
			},
		},
		"pageStartAt": now.Add(-60 * time.Minute).Format(time.RFC3339Nano),
		"pageEndAt":   now.Format(time.RFC3339Nano),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/configurationChanges"):
			_ = json.NewEncoder(w).Encode(auditResp)
		case strings.HasSuffix(r.URL.Path, "/networks"):
			_ = json.NewEncoder(w).Encode(networksResp)
		case strings.Contains(r.URL.Path, "/events"):
			_ = json.NewEncoder(w).Encode(eventsResp)
		default:
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	frames, err := handleOrgChangeFeed(context.Background(), client, MerakiQuery{
		Kind:  KindOrgChangeFeed,
		OrgID: "123",
	}, TimeRange{}, Options{})
	if err != nil {
		t.Fatalf("handleOrgChangeFeed: %v", err)
	}
	if len(frames) != 1 {
		t.Fatalf("expected 1 frame; got %d", len(frames))
	}

	frame := frames[0]
	wantFields := []string{"time", "source", "title", "text", "severity"}
	if got := len(frame.Fields); got != len(wantFields) {
		t.Fatalf("expected %d fields; got %d", len(wantFields), got)
	}
	for i, want := range wantFields {
		if got := frame.Fields[i].Name; got != want {
			t.Errorf("field[%d].Name = %q, want %q", i, got, want)
		}
	}

	// One audit + one warning event; info event filtered out → 2 rows total.
	if got := frame.Fields[0].Len(); got != 2 {
		t.Fatalf("expected 2 rows (1 audit + 1 warning event; info event filtered); got %d", got)
	}

	// Row ordering: event (-30m) is more recent than audit (-1h), so row 0 is
	// the event and row 1 is the audit.
	src0, _ := frame.Fields[1].At(0).(string)
	if src0 != "event" {
		t.Errorf("row 0 source = %q, want event (most recent)", src0)
	}
	src1, _ := frame.Fields[1].At(1).(string)
	if src1 != "audit" {
		t.Errorf("row 1 source = %q, want audit", src1)
	}
}

// TestHandle_OrgChangeFeed_RequiresOrgID guards the handler contract.
func TestHandle_OrgChangeFeed_RequiresOrgID(t *testing.T) {
	t.Parallel()

	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "k", BaseURL: "https://unused"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := handleOrgChangeFeed(context.Background(), client, MerakiQuery{Kind: KindOrgChangeFeed}, TimeRange{}, Options{}); err == nil {
		t.Fatalf("expected error for missing orgId")
	}
}

// TestSeverityForEvent_BucketsKnownKeywords pins the allow-list so changes to
// the list are deliberate + test-reviewed.
func TestSeverityForEvent_BucketsKnownKeywords(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		ev   meraki.NetworkEvent
		want string
	}{
		{"outage", meraki.NetworkEvent{Category: "Connectivity", Type: "wan_down"}, "critical"},
		{"loss", meraki.NetworkEvent{Category: "uplink", Type: "packet_loss"}, "critical"},
		{"fail", meraki.NetworkEvent{Category: "wireless", Type: "auth_fail"}, "warning"},
		{"reboot", meraki.NetworkEvent{Category: "switch", Type: "device_reboot"}, "warning"},
		{"info", meraki.NetworkEvent{Category: "wireless", Type: "association"}, "info"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := severityForEvent(tc.ev); got != tc.want {
				t.Errorf("severityForEvent(%q/%q) = %q, want %q", tc.ev.Category, tc.ev.Type, got, tc.want)
			}
		})
	}
}
