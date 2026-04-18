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

// TestHandle_ClientsList_FansOutNetworks asserts the handler concatenates
// rows from multiple per-network calls into a single wide frame, populates
// the networkId column with the right per-row value, and decodes the inline
// `usage` band into the three numeric columns.
func TestHandle_ClientsList_FansOutNetworks(t *testing.T) {
	t.Parallel()

	first := time.Date(2026, 4, 17, 9, 0, 0, 0, time.UTC)
	last := time.Date(2026, 4, 18, 9, 0, 0, 0, time.UTC)

	respByNetwork := map[string]string{
		"N_a": `[{"mac":"aa:aa","ip":"10.0.0.1","description":"Laptop","ssid":"corp","vlan":"10","status":"Online",
		           "firstSeen":"` + first.Format(time.RFC3339) + `","lastSeen":"` + last.Format(time.RFC3339) + `",
		           "usage":{"sent":12.5,"recv":120.0,"total":132.5}}]`,
		"N_b": `[{"mac":"bb:bb","ip":"10.0.1.1","description":"Phone","ssid":"corp","vlan":"20","status":"Online",
		           "firstSeen":"` + first.Format(time.RFC3339) + `","lastSeen":"` + last.Format(time.RFC3339) + `",
		           "usage":{"sent":1,"recv":2,"total":3}}]`,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Path: /networks/<id>/clients (no /api/v1 prefix when BaseURL is the
		// raw test-server URL).
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) < 3 || parts[0] != "networks" || parts[2] != "clients" {
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		body, ok := respByNetwork[parts[1]]
		if !ok {
			http.Error(w, "unknown network", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	q := MerakiQuery{
		Kind:       KindClientsList,
		OrgID:      "org-1",
		NetworkIDs: []string{"N_a", "N_b"},
	}
	frames, err := handleClientsList(context.Background(), client, q,
		TimeRange{From: first.Add(-time.Hour).UnixMilli(), To: last.Add(time.Hour).UnixMilli()},
		Options{})
	if err != nil {
		t.Fatalf("handleClientsList: %v", err)
	}
	if len(frames) != 1 {
		t.Fatalf("expected 1 frame; got %d", len(frames))
	}
	frame := frames[0]
	if got, _ := frame.RowLen(); got != 2 {
		t.Fatalf("expected 2 rows; got %d", got)
	}

	netField, _ := frame.FieldByName("networkId")
	if netField == nil {
		t.Fatalf("frame missing networkId field; got %v", frame.Fields)
	}
	got0, _ := netField.ConcreteAt(0)
	got1, _ := netField.ConcreteAt(1)
	if got0 != "N_a" || got1 != "N_b" {
		t.Errorf("networkId rows = (%v, %v), want (N_a, N_b)", got0, got1)
	}

	totalField, _ := frame.FieldByName("usageTotalKb")
	if totalField == nil {
		t.Fatalf("frame missing usageTotalKb field")
	}
	if v, _ := totalField.ConcreteAt(0); v.(float64) != 132.5 {
		t.Errorf("usageTotalKb[0] = %v, want 132.5", v)
	}
}

// TestHandle_ClientLookup_NotFound asserts the not-found branch returns a
// zero-row frame with an Info notice — the §G.20 zero-row + notice pattern.
func TestHandle_ClientLookup_NotFound(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"errors":["Not found"]}`, http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	frames, err := handleClientLookup(context.Background(), client, MerakiQuery{
		Kind:    KindClientLookup,
		OrgID:   "org-1",
		Metrics: []string{"00:11:22:33:44:55"},
	}, TimeRange{}, Options{})
	if err != nil {
		t.Fatalf("handleClientLookup not-found: %v", err)
	}
	if len(frames) != 1 {
		t.Fatalf("expected 1 frame; got %d", len(frames))
	}
	frame := frames[0]
	if got, _ := frame.RowLen(); got != 0 {
		t.Errorf("expected zero rows; got %d", got)
	}
	notices := frame.Meta.Notices
	if len(notices) == 0 || notices[0].Severity != data.NoticeSeverityInfo {
		t.Errorf("expected an Info notice, got %+v", notices)
	}
}

// TestHandle_ClientLookup_FoundEmitsRecords asserts a successful search emits
// one row per record (one network sighting per row).
func TestHandle_ClientLookup_FoundEmitsRecords(t *testing.T) {
	t.Parallel()

	first := time.Date(2026, 4, 17, 9, 0, 0, 0, time.UTC)
	last := time.Date(2026, 4, 18, 9, 0, 0, 0, time.UTC)

	body := map[string]any{
		"clientId":    "k123",
		"mac":         "00:11:22:33:44:55",
		"description": "Laptop",
		"records": []map[string]any{
			{
				"network":   map[string]any{"id": "N_a", "name": "Lab"},
				"clientId":  "k123",
				"ip":        "10.0.0.1",
				"firstSeen": first.Format(time.RFC3339),
				"lastSeen":  last.Format(time.RFC3339),
				"usage":     map[string]any{"sent": 1, "recv": 2, "total": 3},
				"ssid":      "corp",
				"status":    "Online",
				"vlan":      "10",
			},
			{
				"network":   map[string]any{"id": "N_b", "name": "HQ"},
				"clientId":  "k123",
				"ip":        "10.0.1.1",
				"firstSeen": first.Format(time.RFC3339),
				"lastSeen":  last.Format(time.RFC3339),
				"usage":     map[string]any{"sent": 4, "recv": 5, "total": 9},
				"status":    "Offline",
				"vlan":      "20",
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/clients/search") {
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		if r.URL.Query().Get("mac") == "" {
			http.Error(w, "mac is required", http.StatusBadRequest)
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
	frames, err := handleClientLookup(context.Background(), client, MerakiQuery{
		Kind:    KindClientLookup,
		OrgID:   "org-1",
		Metrics: []string{"00:11:22:33:44:55"},
	}, TimeRange{}, Options{})
	if err != nil {
		t.Fatalf("handleClientLookup: %v", err)
	}
	if len(frames) != 1 {
		t.Fatalf("expected 1 frame; got %d", len(frames))
	}
	frame := frames[0]
	if got, _ := frame.RowLen(); got != 2 {
		t.Fatalf("expected 2 rows; got %d", got)
	}
	netField, _ := frame.FieldByName("networkId")
	if v0, _ := netField.ConcreteAt(0); v0 != "N_a" {
		t.Errorf("networkId[0] = %v, want N_a", v0)
	}
}

// TestHandle_ClientLookup_RequiresMAC asserts the empty-MAC path emits a
// zero-row frame with an Info notice rather than a 4xx-like error.
func TestHandle_ClientLookup_RequiresMAC(t *testing.T) {
	t.Parallel()

	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "k", BaseURL: "https://unused"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	frames, err := handleClientLookup(context.Background(), client, MerakiQuery{
		Kind:  KindClientLookup,
		OrgID: "org-1",
	}, TimeRange{}, Options{})
	if err != nil {
		t.Fatalf("handleClientLookup empty-mac: %v", err)
	}
	if len(frames) != 1 {
		t.Fatalf("expected 1 frame; got %d", len(frames))
	}
	if got, _ := frames[0].RowLen(); got != 0 {
		t.Errorf("expected zero rows; got %d", got)
	}
	if len(frames[0].Meta.Notices) == 0 {
		t.Errorf("expected an Info notice on the empty-mac frame")
	}
}

// TestHandle_ClientSessions_EmitsPerCategoryFrames asserts the per-client
// latency handler emits one frame per traffic category with labels on the
// value field — long-format frames would render as an empty chart.
func TestHandle_ClientSessions_EmitsPerCategoryFrames(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)
	t1 := time.Date(2026, 4, 12, 0, 0, 0, 0, time.UTC)
	body := []map[string]any{
		{
			"startTs":              t0.Format(time.RFC3339),
			"endTs":                t1.Format(time.RFC3339),
			"avgLatencyMs":         15.5,
			"backgroundAvgLatencyMs": 1.0,
			"bestEffortAvgLatencyMs": 12.0,
			"videoAvgLatencyMs":      0,
			"voiceAvgLatencyMs":      0,
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/wireless/clients/") {
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
	q := MerakiQuery{
		Kind:       KindClientSessions,
		OrgID:      "org-1",
		NetworkIDs: []string{"N_1"},
		Metrics:    []string{"k123"},
	}
	frames, err := handleClientSessions(context.Background(), client, q,
		TimeRange{From: t0.Add(-7 * 24 * time.Hour).UnixMilli(), To: t1.UnixMilli()},
		Options{})
	if err != nil {
		t.Fatalf("handleClientSessions: %v", err)
	}
	// Expect 3 frames: overall + background + bestEffort. video & voice are
	// zero-only, the elision rule drops them (overall is always emitted).
	if len(frames) != 3 {
		t.Fatalf("expected 3 frames (overall, background, bestEffort); got %d", len(frames))
	}

	// Each frame carries a labelled value field — that's the §G.18 contract.
	for i, f := range frames {
		valueField, _ := f.FieldByName("value")
		if valueField == nil || valueField.Labels["clientId"] != "k123" {
			t.Errorf("frame[%d] missing clientId label; labels=%+v", i, valueField.Labels)
		}
	}
}

// TestHandle_ClientSessions_RequiresIDs guards the contract: missing network
// or client id must surface as an error.
func TestHandle_ClientSessions_RequiresIDs(t *testing.T) {
	t.Parallel()

	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "k", BaseURL: "https://unused"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := handleClientSessions(context.Background(), client, MerakiQuery{Kind: KindClientSessions, OrgID: "x"}, TimeRange{}, Options{}); err == nil {
		t.Errorf("expected error for missing networkId")
	}
	if _, err := handleClientSessions(context.Background(), client, MerakiQuery{Kind: KindClientSessions, OrgID: "x", NetworkIDs: []string{"N"}}, TimeRange{}, Options{}); err == nil {
		t.Errorf("expected error for missing clientId")
	}
}
