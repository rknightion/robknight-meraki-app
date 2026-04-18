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

// v0.5 §4.4.3-1c handler tests.

// TestHandle_ApplianceTrafficShaping_FlattensPerNetwork verifies the
// trafficShaping handler hits both per-network snapshot endpoints and emits a
// single table frame with one row per network. Fields from both endpoints
// (default-rules flag + global bandwidth caps from /trafficShaping; defaultUplink
// and loadBalancingEnabled from /uplinkSelection) must appear on the same row.
func TestHandle_ApplianceTrafficShaping_FlattensPerNetwork(t *testing.T) {
	const shapingN1 = `{"defaultRulesEnabled": true, "globalBandwidthLimits": {"limitUp": 500000, "limitDown": 1000000}}`
	const uplinkN1 = `{
	  "activeActiveAutoVpnEnabled": true,
	  "defaultUplink": "wan1",
	  "loadBalancingEnabled": true,
	  "failoverAndFailback": {"immediate": {"enabled": true}},
	  "wanTrafficUplinkPreferences": [
	    {"preferredUplink": "wan1", "trafficFilters": [{"type": "custom"}]}
	  ],
	  "vpnTrafficUplinkPreferences": [
	    {"preferredUplink": "bestForVoIP", "failOverCriterion": "poorPerformance", "trafficFilters": [{"type": "applicationCategory"}, {"type": "application"}]}
	  ]
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/networks/N1/appliance/trafficShaping/uplinkSelection"):
			_, _ = w.Write([]byte(uplinkN1))
		case strings.HasSuffix(r.URL.Path, "/networks/N1/appliance/trafficShaping"):
			_, _ = w.Write([]byte(shapingN1))
		default:
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	resp, err := Handle(context.Background(), client, &QueryRequest{
		Queries: []MerakiQuery{{RefID: "A", Kind: KindApplianceTrafficShaping, NetworkIDs: []string{"N1"}}},
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
	rows, err := frame.RowLen()
	if err != nil {
		t.Fatalf("RowLen: %v", err)
	}
	if rows != 1 {
		t.Fatalf("got %d rows, want 1", rows)
	}

	checks := map[string]any{
		"networkId":             "N1",
		"defaultRulesEnabled":   true,
		"globalLimitUpKbps":     int64(500000),
		"globalLimitDownKbps":   int64(1000000),
		"defaultUplink":         "wan1",
		"loadBalancingEnabled":  true,
		"activeActiveAutoVpn":   true,
		"immediateFailover":     true,
		"wanTrafficPreferences": int64(1),
		"vpnTrafficPreferences": int64(1),
	}
	for name, want := range checks {
		f, _ := frame.FieldByName(name)
		if f == nil {
			t.Fatalf("frame missing %q column; fields=%v", name, frame.Fields)
		}
		got, _ := f.ConcreteAt(0)
		if got != want {
			t.Fatalf("%s = %v (%T), want %v (%T)", name, got, got, want, want)
		}
	}
}

// TestHandle_ApplianceFailoverEvents_FiltersToUplinkChangeTypes verifies that
// the failover handler:
//  1. Sends productType=appliance + includedEventTypes[] defaults to the MX
//     uplink-change event list.
//  2. Emits one row per event with the uplink column extracted from the
//     nested eventData map.
func TestHandle_ApplianceFailoverEvents_FiltersToUplinkChangeTypes(t *testing.T) {
	const payload = `{"events":[
	  {"occurredAt":"2026-04-17T12:00:00Z","networkId":"N1","type":"uplink_change","description":"WAN 1 down","category":"appliance","productType":"appliance","deviceSerial":"Q2XX-MX-0001","deviceName":"MX-HQ","eventData":{"uplink":"wan1"}},
	  {"occurredAt":"2026-04-17T12:05:00Z","networkId":"N1","type":"cellular_up","description":"Cellular came up","category":"appliance","productType":"appliance","deviceSerial":"Q2XX-MX-0001","deviceName":"MX-HQ","eventData":{"interface":"cellular"}}
	],"pageStartAt":"2026-04-17T12:00:00Z","pageEndAt":"2026-04-17T12:05:00Z"}`

	var capturedProductType string
	var capturedIncludedTypes []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/networks/N1/events") {
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		capturedProductType = r.URL.Query().Get("productType")
		capturedIncludedTypes = r.URL.Query()["includedEventTypes[]"]
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
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
		Queries: []MerakiQuery{{
			RefID:      "A",
			Kind:       KindApplianceFailoverEvents,
			NetworkIDs: []string{"N1"},
		}},
	}, Options{PluginPathPrefix: "/a/robknight-meraki-app"})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got := len(resp.Frames); got != 1 {
		t.Fatalf("got %d frames, want 1", got)
	}

	if capturedProductType != "appliance" {
		t.Fatalf("productType = %q, want appliance", capturedProductType)
	}
	// Must include the default uplink-change event type set.
	foundUplinkChange := false
	foundCellularUp := false
	for _, tp := range capturedIncludedTypes {
		if tp == "uplink_change" {
			foundUplinkChange = true
		}
		if tp == "cellular_up" {
			foundCellularUp = true
		}
	}
	if !foundUplinkChange || !foundCellularUp {
		t.Fatalf("includedEventTypes[] missing defaults (uplink_change or cellular_up); got %v", capturedIncludedTypes)
	}

	var frame data.Frame
	if err := json.Unmarshal(resp.Frames[0], &frame); err != nil {
		t.Fatalf("decode frame: %v", err)
	}
	rows, err := frame.RowLen()
	if err != nil {
		t.Fatalf("RowLen: %v", err)
	}
	if rows != 2 {
		t.Fatalf("got %d rows, want 2", rows)
	}

	// Uplink column extraction: row 0 has uplink=wan1 (from eventData.uplink),
	// row 1 has interface=cellular (from eventData.interface).
	typeField, _ := frame.FieldByName("type")
	uplinkField, _ := frame.FieldByName("uplink")
	if typeField == nil || uplinkField == nil {
		t.Fatalf("frame missing type/uplink column; fields=%v", frame.Fields)
	}
	byType := map[string]string{}
	for i := range rows {
		tp, _ := typeField.ConcreteAt(i)
		up, _ := uplinkField.ConcreteAt(i)
		byType[tp.(string)] = up.(string)
	}
	if byType["uplink_change"] != "wan1" {
		t.Fatalf("uplink_change uplink = %q, want wan1 (seen=%v)", byType["uplink_change"], byType)
	}
	if byType["cellular_up"] != "cellular" {
		t.Fatalf("cellular_up uplink = %q, want cellular (seen=%v)", byType["cellular_up"], byType)
	}
}

// TestHandle_ApplianceFailoverEvents_CallerMetricsOverrideDefault verifies
// that when q.Metrics is populated, the default uplink-change type list is
// overridden (same override behaviour as the existing networkEvents kind).
func TestHandle_ApplianceFailoverEvents_CallerMetricsOverrideDefault(t *testing.T) {
	var capturedIncludedTypes []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedIncludedTypes = r.URL.Query()["includedEventTypes[]"]
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"events":[]}`))
	}))
	t.Cleanup(srv.Close)

	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = Handle(context.Background(), client, &QueryRequest{
		Queries: []MerakiQuery{{
			RefID:      "A",
			Kind:       KindApplianceFailoverEvents,
			NetworkIDs: []string{"N1"},
			Metrics:    []string{"only_this_type"},
		}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if len(capturedIncludedTypes) != 1 || capturedIncludedTypes[0] != "only_this_type" {
		t.Fatalf("includedEventTypes[] = %v, want [only_this_type] (caller override)", capturedIncludedTypes)
	}
}

// TestHandle_ApplianceVpnHeatmap_EmitsLongFormatFrame verifies the heatmap
// reshape: one row per meraki peer-pair, value=1 when reachable else 0,
// thirdPartyVpnPeers skipped.
func TestHandle_ApplianceVpnHeatmap_EmitsLongFormatFrame(t *testing.T) {
	const payload = `[
	  {
	    "networkId": "N1",
	    "networkName": "HQ",
	    "deviceStatus": "online",
	    "deviceSerial": "Q2XX-MX-0001",
	    "vpnMode": "hub",
	    "merakiVpnPeers": [
	      {"networkId": "N2", "networkName": "Branch-1", "reachability": "reachable"},
	      {"networkId": "N3", "networkName": "Branch-2", "reachability": "unreachable"}
	    ],
	    "thirdPartyVpnPeers": [
	      {"name": "AWS-VPG", "publicIp": "54.239.28.85", "reachability": "reachable"}
	    ]
	  }
	]`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/appliance/vpn/statuses") {
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
		Queries: []MerakiQuery{{RefID: "A", Kind: KindApplianceVpnHeatmap, OrgID: "o1"}},
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
	rows, err := frame.RowLen()
	if err != nil {
		t.Fatalf("RowLen: %v", err)
	}
	// 2 meraki peers; the thirdParty peer must be skipped.
	if rows != 2 {
		t.Fatalf("got %d rows, want 2 (meraki peers only; thirdParty skipped)", rows)
	}

	peerField, _ := frame.FieldByName("peerNetworkName")
	valueField, _ := frame.FieldByName("value")
	if peerField == nil || valueField == nil {
		t.Fatalf("frame missing peerNetworkName/value column; fields=%v", frame.Fields)
	}
	byPeer := map[string]float64{}
	for i := range rows {
		p, _ := peerField.ConcreteAt(i)
		v, _ := valueField.ConcreteAt(i)
		byPeer[p.(string)] = v.(float64)
	}
	if byPeer["Branch-1"] != 1 {
		t.Fatalf("Branch-1 value = %v, want 1 (reachable)", byPeer["Branch-1"])
	}
	if byPeer["Branch-2"] != 0 {
		t.Fatalf("Branch-2 value = %v, want 0 (unreachable)", byPeer["Branch-2"])
	}
	if _, ok := byPeer["AWS-VPG"]; ok {
		t.Fatalf("thirdParty peer AWS-VPG should be skipped; byPeer=%v", byPeer)
	}
}
