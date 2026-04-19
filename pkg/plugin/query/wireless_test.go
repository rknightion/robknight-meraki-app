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

// Wireless handler tests for the endpoints added in §2.1 and §3.2.
// Each test stubs Meraki via httptest.NewServer and asserts the emitted frame shape.

// ---------------------------------------------------------------------------
// §2.1 — wirelessApClientCounts
// ---------------------------------------------------------------------------

// TestHandle_WirelessApClientCounts_Table verifies that handleWirelessApClientCounts
// emits one table frame with one row per device and the expected column set.
//
// Uses the 2026-04 items-envelope wire shape.
func TestHandle_WirelessApClientCounts_Table(t *testing.T) {
	const payload = `{"items":[
	  {"serial":"Q2AA-0001","network":{"id":"N1"},"counts":{"byStatus":{"online":7}}},
	  {"serial":"Q2AA-0002","network":{"id":"N2"},"counts":{"byStatus":{"online":3}}}
	],"meta":{"counts":{"items":{"total":2,"remaining":0}}}}`
	const networksPayload = `[
	  {"id":"N1","organizationId":"o1","name":"HQ Wireless","productTypes":["wireless"]},
	  {"id":"N2","organizationId":"o1","name":"Branch Wireless","productTypes":["wireless"]}
	]`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/wireless/clients/overview/byDevice"):
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

	resp, err := Handle(context.Background(), client, &QueryRequest{
		Queries: []MerakiQuery{{RefID: "A", Kind: KindWirelessApClientCounts, OrgID: "o1"}},
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

	rows, _ := frame.RowLen()
	if rows != 2 {
		t.Fatalf("got %d rows, want 2", rows)
	}

	for _, col := range []string{"serial", "networkId", "networkName", "online"} {
		if f, _ := frame.FieldByName(col); f == nil {
			t.Fatalf("frame missing %q column; fields=%v", col, frame.Fields)
		}
	}

	// Verify network name was resolved from the stub.
	netNameField, _ := frame.FieldByName("networkName")
	if got, _ := netNameField.ConcreteAt(0); got != "HQ Wireless" {
		t.Errorf("row 0 networkName = %v, want HQ Wireless", got)
	}

	// Verify online count.
	onlineField, _ := frame.FieldByName("online")
	if got, _ := onlineField.ConcreteAt(0); got != int64(7) {
		t.Errorf("row 0 online = %v, want 7", got)
	}
}

// ---------------------------------------------------------------------------
// §3.2 — wirelessPacketLossByNetwork
// ---------------------------------------------------------------------------

// TestHandle_WirelessPacketLossByNetwork_Table verifies handleWirelessPacketLossByNetwork
// emits one table frame with the expected columns and one row per network.
func TestHandle_WirelessPacketLossByNetwork_Table(t *testing.T) {
	const payload = `[
	  {
	    "network":{"id":"N1"},
	    "downstream":{"total":10000,"lost":50,"lossPercentage":0.5},
	    "upstream":  {"total":8000, "lost":20,"lossPercentage":0.25},
	    "total":     {"total":18000,"lost":70,"lossPercentage":0.39}
	  },
	  {
	    "network":{"id":"N2"},
	    "downstream":{"total":5000,"lost":200,"lossPercentage":4.0},
	    "upstream":  {"total":4000,"lost":100,"lossPercentage":2.5},
	    "total":     {"total":9000,"lost":300,"lossPercentage":3.33}
	  }
	]`
	const networksPayload = `[
	  {"id":"N1","organizationId":"o1","name":"Net1","productTypes":["wireless"]},
	  {"id":"N2","organizationId":"o1","name":"Net2","productTypes":["wireless"]}
	]`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/wireless/devices/packetLoss/byNetwork"):
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
		Range: TimeRange{
			From: now.Add(-24*time.Hour).UnixMilli(),
			To:   now.UnixMilli(),
		},
		Queries: []MerakiQuery{{RefID: "A", Kind: KindWirelessPacketLossByNetwork, OrgID: "o1"}},
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

	rows, _ := frame.RowLen()
	if rows != 2 {
		t.Fatalf("got %d rows, want 2", rows)
	}

	for _, col := range []string{"networkId", "networkName", "downstreamLossPct", "upstreamLossPct", "totalLossPct"} {
		if f, _ := frame.FieldByName(col); f == nil {
			t.Fatalf("frame missing %q column; fields=%v", col, frame.Fields)
		}
	}

	dsLossField, _ := frame.FieldByName("downstreamLossPct")
	if got, _ := dsLossField.ConcreteAt(0); got != 0.5 {
		t.Errorf("row 0 downstreamLossPct = %v, want 0.5", got)
	}
}

// TestHandle_WirelessPacketLossByNetwork_Respects90DayCap verifies that a
// time range longer than 90 days is clamped to the MaxTimespan.
func TestHandle_WirelessPacketLossByNetwork_Respects90DayCap(t *testing.T) {
	capturedT0 := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/wireless/devices/packetLoss/byNetwork"):
			capturedT0 = r.URL.Query().Get("t0")
			_, _ = w.Write([]byte(`[]`))
		case strings.HasSuffix(r.URL.Path, "/networks"):
			_, _ = w.Write([]byte(`[]`))
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
	// Request 200 days — should be clamped to 90.
	resp, err := Handle(context.Background(), client, &QueryRequest{
		Range: TimeRange{
			From: now.Add(-200 * 24 * time.Hour).UnixMilli(),
			To:   now.UnixMilli(),
		},
		Queries: []MerakiQuery{{RefID: "A", Kind: KindWirelessPacketLossByNetwork, OrgID: "o1"}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(resp.Frames) == 0 {
		t.Fatal("expected at least one frame")
	}

	// The t0 sent to Meraki must be within 90 days + freshness floor of now.
	if capturedT0 == "" {
		t.Fatal("no t0 captured — did the stub match?")
	}
	t0, err := time.Parse("2006-01-02T15:04:05Z", capturedT0)
	if err != nil {
		t.Fatalf("parse t0 %q: %v", capturedT0, err)
	}
	elapsed := now.Sub(t0)
	maxAllowed := 90*24*time.Hour + 2*meraki.FreshnessFloor + time.Minute // generous fuzz
	if elapsed > maxAllowed {
		t.Errorf("t0 is %v in the past, expected <= %v (90d cap)", elapsed, maxAllowed)
	}
}

// ---------------------------------------------------------------------------
// §3.2 — wirelessDevicesEthernetStatuses
// ---------------------------------------------------------------------------

// TestHandle_WirelessDevicesEthernetStatuses_Table verifies the ethernet-statuses
// handler emits one table frame with the expected column set.
func TestHandle_WirelessDevicesEthernetStatuses_Table(t *testing.T) {
	const payload = `[
	  {
	    "serial":"Q2AA-0001",
	    "name":"AP-HQ-1",
	    "network":{"id":"N1"},
	    "model":"MR46",
	    "power":{"ac":{"isConnected":true},"poe":{"isConnected":false,"maximum":0}},
	    "ports":[
	      {"name":"LAN","speed":"1000Mbps","duplex":"full","poe":{"isConnected":false}}
	    ]
	  },
	  {
	    "serial":"Q2AA-0002",
	    "name":"AP-HQ-2",
	    "network":{"id":"N1"},
	    "model":"MR44",
	    "power":{"ac":{"isConnected":false},"poe":{"isConnected":true,"maximum":30}},
	    "ports":[
	      {"name":"LAN","speed":"1000Mbps","duplex":"full","poe":{"isConnected":true}}
	    ]
	  }
	]`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/wireless/devices/ethernet/statuses") {
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

	resp, err := Handle(context.Background(), client, &QueryRequest{
		Queries: []MerakiQuery{{RefID: "A", Kind: KindWirelessDevicesEthernetStatuses, OrgID: "o1"}},
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

	rows, _ := frame.RowLen()
	if rows != 2 {
		t.Fatalf("got %d rows, want 2", rows)
	}

	for _, col := range []string{"serial", "name", "networkId", "model", "power", "primarySpeed", "primaryDuplex", "primaryPoe"} {
		if f, _ := frame.FieldByName(col); f == nil {
			t.Fatalf("frame missing %q column; fields=%v", col, frame.Fields)
		}
	}

	// AP-HQ-1 uses AC power.
	powerField, _ := frame.FieldByName("power")
	if got, _ := powerField.ConcreteAt(0); got != "ac" {
		t.Errorf("row 0 power = %v, want ac", got)
	}
	// AP-HQ-2 uses PoE.
	if got, _ := powerField.ConcreteAt(1); got != "poe" {
		t.Errorf("row 1 power = %v, want poe", got)
	}
}

// ---------------------------------------------------------------------------
// §3.2 — wirelessDevicesCpuLoadHistory
// ---------------------------------------------------------------------------

// TestHandle_WirelessDevicesCpuLoadHistory_EmitsPerSerialFrames verifies that
// handleWirelessDevicesCpuLoadHistory emits one frame per AP serial with correct
// label and value.
func TestHandle_WirelessDevicesCpuLoadHistory_EmitsPerSerialFrames(t *testing.T) {
	// 2026-04 wire shape: items-envelope, per-item series[{ts,cpuLoad5}],
	// cpuLoad5 in raw kernel load × 2^16 units (cpuCount=4 → 0.5 load →
	// ~12.8% per-core utilization).
	const payload = `{"items":[
	  {
	    "serial":"Q2AA-0001",
	    "network":{"id":"N1"},
	    "cpuCount":4,
	    "series":[
	      {"ts":"2026-04-18T10:00:00Z","cpuLoad5":32768},
	      {"ts":"2026-04-18T10:05:00Z","cpuLoad5":36864}
	    ]
	  },
	  {
	    "serial":"Q2AA-0002",
	    "network":{"id":"N1"},
	    "cpuCount":4,
	    "series":[
	      {"ts":"2026-04-18T10:00:00Z","cpuLoad5":13107}
	    ]
	  }
	]}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/wireless/devices/system/cpu/load/history") {
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
		Range: TimeRange{
			From: now.Add(-1 * time.Hour).UnixMilli(),
			To:   now.UnixMilli(),
		},
		Queries: []MerakiQuery{{RefID: "A", Kind: KindWirelessDevicesCpuLoadHistory, OrgID: "o1"}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	// Expect two frames: one per serial.
	if got := len(resp.Frames); got != 2 {
		t.Fatalf("got %d frames, want 2", got)
	}

	// Decode both frames and assert serial labels.
	serials := make([]string, 0, 2)
	for i, raw := range resp.Frames {
		var frame data.Frame
		if err := json.Unmarshal(raw, &frame); err != nil {
			t.Fatalf("decode frame %d: %v", i, err)
		}
		// The value field carries the serial label.
		valueField, _ := frame.FieldByName("value")
		if valueField == nil {
			t.Fatalf("frame %d has no value field; fields=%v", i, frame.Fields)
		}
		serial, ok := valueField.Labels["serial"]
		if !ok {
			t.Fatalf("frame %d value field has no serial label; labels=%v", i, valueField.Labels)
		}
		serials = append(serials, serial)
	}
	if len(serials) != 2 {
		t.Fatalf("got %d serial labels, want 2", len(serials))
	}
}

// TestHandle_WirelessDevicesCpuLoadHistory_Respects1DayCap verifies that a
// time range longer than 1 day is clamped by KnownEndpointRanges.
func TestHandle_WirelessDevicesCpuLoadHistory_Respects1DayCap(t *testing.T) {
	capturedT0 := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/wireless/devices/system/cpu/load/history") {
			capturedT0 = r.URL.Query().Get("t0")
			_, _ = w.Write([]byte(`{"items":[]}`))
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
	// Request 7 days — should be clamped to 1 day.
	resp, err := Handle(context.Background(), client, &QueryRequest{
		Range: TimeRange{
			From: now.Add(-7 * 24 * time.Hour).UnixMilli(),
			To:   now.UnixMilli(),
		},
		Queries: []MerakiQuery{{RefID: "A", Kind: KindWirelessDevicesCpuLoadHistory, OrgID: "o1"}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(resp.Frames) == 0 {
		t.Fatal("expected at least one frame")
	}

	if capturedT0 == "" {
		t.Fatal("no t0 captured — did the stub match?")
	}
	t0, err := time.Parse("2006-01-02T15:04:05Z", capturedT0)
	if err != nil {
		t.Fatalf("parse t0 %q: %v", capturedT0, err)
	}
	elapsed := now.Sub(t0)
	maxAllowed := 1*24*time.Hour + 2*meraki.FreshnessFloor + time.Minute // generous fuzz
	if elapsed > maxAllowed {
		t.Errorf("t0 is %v in the past, expected <= %v (1d cap)", elapsed, maxAllowed)
	}
}
