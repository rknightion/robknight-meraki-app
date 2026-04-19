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

// ---------------------------------------------------------------------------
// §4.4.3-1a — wirelessClientCountHistory
// ---------------------------------------------------------------------------

// TestHandle_WirelessClientCountHistory_EmitsPerNetworkFrames verifies that
// handleWirelessClientCountHistory emits one timeseries frame per network
// with network_id labels on the value field.
func TestHandle_WirelessClientCountHistory_EmitsPerNetworkFrames(t *testing.T) {
	const payloadN1 = `[
	  {"startTs":"2026-04-18T10:00:00Z","endTs":"2026-04-18T10:05:00Z","clientCount":12},
	  {"startTs":"2026-04-18T10:05:00Z","endTs":"2026-04-18T10:10:00Z","clientCount":15}
	]`
	const payloadN2 = `[
	  {"startTs":"2026-04-18T10:00:00Z","endTs":"2026-04-18T10:05:00Z","clientCount":3}
	]`
	const networksPayload = `[
	  {"id":"N1","organizationId":"o1","name":"HQ","productTypes":["wireless"]},
	  {"id":"N2","organizationId":"o1","name":"Branch","productTypes":["wireless"]}
	]`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/networks/N1/wireless/clientCountHistory"):
			_, _ = w.Write([]byte(payloadN1))
		case strings.Contains(r.URL.Path, "/networks/N2/wireless/clientCountHistory"):
			_, _ = w.Write([]byte(payloadN2))
		case strings.HasSuffix(r.URL.Path, "/networks"):
			_, _ = w.Write([]byte(networksPayload))
		default:
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	now := time.Now()
	resp, err := Handle(context.Background(), client, &QueryRequest{
		Range: TimeRange{From: now.Add(-24 * time.Hour).UnixMilli(), To: now.UnixMilli()},
		Queries: []MerakiQuery{{
			RefID:      "A",
			Kind:       KindWirelessClientCountHistory,
			OrgID:      "o1",
			NetworkIDs: []string{"N1", "N2"},
		}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got := len(resp.Frames); got != 2 {
		t.Fatalf("got %d frames, want 2", got)
	}
	seenNetworks := map[string]bool{}
	for i, raw := range resp.Frames {
		var frame data.Frame
		if err := json.Unmarshal(raw, &frame); err != nil {
			t.Fatalf("decode frame %d: %v", i, err)
		}
		valueField, _ := frame.FieldByName("value")
		if valueField == nil {
			t.Fatalf("frame %d has no value field", i)
		}
		nid, ok := valueField.Labels["network_id"]
		if !ok {
			t.Fatalf("frame %d value field missing network_id label: %v", i, valueField.Labels)
		}
		seenNetworks[nid] = true
	}
	if !seenNetworks["N1"] || !seenNetworks["N2"] {
		t.Errorf("expected per-network frames for N1 and N2, got %v", seenNetworks)
	}
}

// ---------------------------------------------------------------------------
// §4.4.3-1a — wirelessFailedConnections
// ---------------------------------------------------------------------------

// TestHandle_WirelessFailedConnections_Table verifies the failed-connections
// handler aggregates events into a (serial, ssid, type, count) wide frame.
func TestHandle_WirelessFailedConnections_Table(t *testing.T) {
	const payload = `[
	  {"ts":"2026-04-18T10:00:00Z","type":"assoc","serial":"Q2AA-0001","clientMac":"aa:bb","ssidNumber":0,"failureStep":"assoc","band":"5","channel":36},
	  {"ts":"2026-04-18T10:01:00Z","type":"assoc","serial":"Q2AA-0001","clientMac":"aa:bb","ssidNumber":0,"failureStep":"assoc","band":"5","channel":36},
	  {"ts":"2026-04-18T10:02:00Z","type":"dhcp","serial":"Q2AA-0002","clientMac":"cc:dd","ssidNumber":1,"failureStep":"dhcp","band":"2.4","channel":6}
	]`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/wireless/failedConnections") {
			_, _ = w.Write([]byte(payload))
			return
		}
		http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	now := time.Now()
	resp, err := Handle(context.Background(), client, &QueryRequest{
		Range: TimeRange{From: now.Add(-1 * time.Hour).UnixMilli(), To: now.UnixMilli()},
		Queries: []MerakiQuery{{
			RefID:      "A",
			Kind:       KindWirelessFailedConnections,
			OrgID:      "o1",
			NetworkIDs: []string{"N1"},
		}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got := len(resp.Frames); got != 1 {
		t.Fatalf("got %d frames, want 1", got)
	}
	var frame data.Frame
	if err := json.Unmarshal(resp.Frames[0], &frame); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, col := range []string{"serial", "ssid", "type", "count"} {
		if f, _ := frame.FieldByName(col); f == nil {
			t.Fatalf("frame missing %q column; fields=%v", col, frame.Fields)
		}
	}
	rows, _ := frame.RowLen()
	if rows != 2 {
		t.Fatalf("got %d rows, want 2 (two distinct (serial,ssid,type) keys)", rows)
	}
	// First row: Q2AA-0001, ssid 0, assoc, count 2.
	countField, _ := frame.FieldByName("count")
	if got, _ := countField.ConcreteAt(0); got != int64(2) {
		t.Errorf("row 0 count = %v, want 2", got)
	}
}

// ---------------------------------------------------------------------------
// §4.4.3-1a — wirelessLatencyStats
// ---------------------------------------------------------------------------

// TestHandle_WirelessLatencyStats_EmitsPerNetworkFrames verifies the latency
// handler emits per-network timeseries frames with category labels.
func TestHandle_WirelessLatencyStats_EmitsPerNetworkFrames(t *testing.T) {
	const payload = `[
	  {"startTs":"2026-04-18T10:00:00Z","endTs":"2026-04-18T10:05:00Z","avgLatencyMs":12.5,"backgroundTrafficMs":0,"bestEffortTrafficMs":15.0,"videoTrafficMs":0,"voiceTrafficMs":0},
	  {"startTs":"2026-04-18T10:05:00Z","endTs":"2026-04-18T10:10:00Z","avgLatencyMs":14.0,"backgroundTrafficMs":0,"bestEffortTrafficMs":18.0,"videoTrafficMs":0,"voiceTrafficMs":0}
	]`
	const networksPayload = `[{"id":"N1","organizationId":"o1","name":"HQ","productTypes":["wireless"]}]`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/wireless/latencyHistory"):
			_, _ = w.Write([]byte(payload))
		case strings.HasSuffix(r.URL.Path, "/networks"):
			_, _ = w.Write([]byte(networksPayload))
		default:
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	now := time.Now()
	resp, err := Handle(context.Background(), client, &QueryRequest{
		Range: TimeRange{From: now.Add(-24 * time.Hour).UnixMilli(), To: now.UnixMilli()},
		Queries: []MerakiQuery{{
			RefID:      "A",
			Kind:       KindWirelessLatencyStats,
			OrgID:      "o1",
			NetworkIDs: []string{"N1"},
		}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	// Expect 2 frames: avg + bestEffort (the other categories are zero).
	if got := len(resp.Frames); got != 2 {
		t.Fatalf("got %d frames, want 2", got)
	}
	seenCategories := map[string]bool{}
	for i, raw := range resp.Frames {
		var frame data.Frame
		if err := json.Unmarshal(raw, &frame); err != nil {
			t.Fatalf("decode frame %d: %v", i, err)
		}
		valueField, _ := frame.FieldByName("value")
		if valueField == nil {
			t.Fatalf("frame %d has no value field", i)
		}
		cat, ok := valueField.Labels["category"]
		if !ok {
			t.Fatalf("frame %d has no category label: %v", i, valueField.Labels)
		}
		seenCategories[cat] = true
	}
	if !seenCategories["avg"] || !seenCategories["bestEffort"] {
		t.Errorf("expected avg+bestEffort categories, got %v", seenCategories)
	}
}

// ---------------------------------------------------------------------------
// §4.4.3-1a — deviceRadioStatus
// ---------------------------------------------------------------------------

// TestHandle_DeviceRadioStatus_Table verifies the radio-status handler emits a
// wide table with one row per AP and booleans per band.
func TestHandle_DeviceRadioStatus_Table(t *testing.T) {
	// 2026-04 wire shape: items-envelope, and `enabled` moved to ssid.enabled.
	const payload = `{"items":[
	  {
	    "serial":"Q2AA-0001",
	    "network":{"id":"N1"},
	    "basicServiceSets":[
	      {"ssid":{"enabled":true},"radio":{"band":"2.4","isBroadcasting":true}},
	      {"ssid":{"enabled":true},"radio":{"band":"5","isBroadcasting":true}}
	    ]
	  },
	  {
	    "serial":"Q2AA-0002",
	    "network":{"id":"N1"},
	    "basicServiceSets":[
	      {"ssid":{"enabled":true},"radio":{"band":"5","isBroadcasting":true}},
	      {"ssid":{"enabled":false},"radio":{"band":"6","isBroadcasting":false}}
	    ]
	  }
	]}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/wireless/ssids/statuses/byDevice"):
			_, _ = w.Write([]byte(payload))
		case strings.Contains(r.URL.Path, "/organizations/o1/devices"):
			_, _ = w.Write([]byte(`[{"serial":"Q2AA-0001","name":"AP-1","productType":"wireless"},{"serial":"Q2AA-0002","name":"AP-2","productType":"wireless"}]`))
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
		Queries: []MerakiQuery{{RefID: "A", Kind: KindDeviceRadioStatus, OrgID: "o1"}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got := len(resp.Frames); got != 1 {
		t.Fatalf("got %d frames, want 1", got)
	}
	var frame data.Frame
	if err := json.Unmarshal(resp.Frames[0], &frame); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, col := range []string{"serial", "name", "band2_4", "band5", "band6", "enabled"} {
		if f, _ := frame.FieldByName(col); f == nil {
			t.Fatalf("frame missing %q column; fields=%v", col, frame.Fields)
		}
	}
	rows, _ := frame.RowLen()
	if rows != 2 {
		t.Fatalf("got %d rows, want 2", rows)
	}
	band24Field, _ := frame.FieldByName("band2_4")
	if got, _ := band24Field.ConcreteAt(0); got != true {
		t.Errorf("row 0 band2_4 = %v, want true", got)
	}
	band5Field, _ := frame.FieldByName("band5")
	if got, _ := band5Field.ConcreteAt(0); got != true {
		t.Errorf("row 0 band5 = %v, want true", got)
	}
	// Row 1 (Q2AA-0002) only has band5 active and 6 GHz BSSID was not broadcasting.
	if got, _ := band5Field.ConcreteAt(1); got != true {
		t.Errorf("row 1 band5 = %v, want true", got)
	}
	band6Field, _ := frame.FieldByName("band6")
	if got, _ := band6Field.ConcreteAt(1); got != false {
		t.Errorf("row 1 band6 = %v, want false", got)
	}
}
