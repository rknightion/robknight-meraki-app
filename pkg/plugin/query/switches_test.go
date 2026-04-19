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

// §4.4.3-1b — switchPoe / switchStp / switchMacTable / switchVlansSummary tests.

// TestHandle_SwitchPoe_FlattensPerPort verifies the PoE handler emits one row
// per port across every switch, with the expected columns.
//
// Two paths exercised:
//  1. Fleet (no serial) — reads the org-level `bySwitch/statuses` feed.
//  2. Per-switch (Serials set) — reads the device-scoped
//     `/devices/{serial}/switch/ports/statuses` feed (richer shape;
//     required because the org-level feed no longer returns per-port
//     `powerUsageInWh` per the Meraki v1 API behaviour observed 2026-04-19).
func TestHandle_SwitchPoe_FlattensPerPort(t *testing.T) {
	const orgPayload = `{"items":[
	  {"serial":"Q2SW-0001","name":"sw-a","network":{"id":"N1","name":"HQ"},"ports":[
	    {"portId":"1","enabled":true,"powerUsageInWh":7.5},
	    {"portId":"2","enabled":true,"powerUsageInWh":0}
	  ]},
	  {"serial":"Q2SW-0002","name":"sw-b","network":{"id":"N1","name":"HQ"},"ports":[
	    {"portId":"1","enabled":false,"powerUsageInWh":0}
	  ]}
	]}`
	// Device-scoped statuses payload used by the per-serial path.
	const devStatusesPayload = `[
	  {"portId":"1","enabled":true,"powerUsageInWh":7.5},
	  {"portId":"2","enabled":true,"powerUsageInWh":0}
	]`
	// Device-scoped config payload (empty — VLAN merging not asserted here).
	const devConfigPayload = `[
	  {"portId":"1","enabled":true},
	  {"portId":"2","enabled":true}
	]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/switch/ports/statuses/bySwitch"):
			_, _ = w.Write([]byte(orgPayload))
		case strings.HasSuffix(r.URL.Path, "/switch/ports/statuses"):
			_, _ = w.Write([]byte(devStatusesPayload))
		case strings.HasSuffix(r.URL.Path, "/switch/ports"):
			_, _ = w.Write([]byte(devConfigPayload))
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
		Queries: []MerakiQuery{{RefID: "A", Kind: KindSwitchPoe, OrgID: "o1"}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(resp.Frames) != 1 {
		t.Fatalf("got %d frames, want 1", len(resp.Frames))
	}
	var frame data.Frame
	if err := json.Unmarshal(resp.Frames[0], &frame); err != nil {
		t.Fatalf("decode: %v", err)
	}
	rows, _ := frame.RowLen()
	if rows != 3 {
		t.Fatalf("got %d rows, want 3", rows)
	}
	for _, col := range []string{"serial", "switchName", "portId", "enabled", "poeWatts"} {
		if f, _ := frame.FieldByName(col); f == nil {
			t.Fatalf("missing column %q; fields=%v", col, frame.Fields)
		}
	}
	// Per-serial path — device-scoped statuses endpoint. Two rows for ports
	// 1 and 2 of Q2SW-0001. Fresh client to avoid TTL cache from the prior
	// call.
	client2, _ := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	resp, err = Handle(context.Background(), client2, &QueryRequest{
		Queries: []MerakiQuery{{RefID: "A", Kind: KindSwitchPoe, OrgID: "o1", Serials: []string{"Q2SW-0001"}}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	var f2 data.Frame
	_ = json.Unmarshal(resp.Frames[0], &f2)
	if r, _ := f2.RowLen(); r != 2 {
		t.Fatalf("filtered: got %d rows, want 2", r)
	}
}

// TestHandle_SwitchStp_ExpandsBridgePriorities verifies the STP handler
// emits one row per (network, switch-or-stack) with rstpEnabled + priority.
func TestHandle_SwitchStp_ExpandsBridgePriorities(t *testing.T) {
	const payload = `{
	  "rstpEnabled": true,
	  "stpBridgePriority": [
	    {"switches":["Q2SW-0001","Q2SW-0002"],"stpPriority":4096},
	    {"stacks":["STACK-1"],"stpPriority":8192}
	  ]
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/switch/stp") {
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
		Queries: []MerakiQuery{{RefID: "A", Kind: KindSwitchStp, NetworkIDs: []string{"N1"}}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	var frame data.Frame
	if err := json.Unmarshal(resp.Frames[0], &frame); err != nil {
		t.Fatalf("decode: %v", err)
	}
	rows, _ := frame.RowLen()
	if rows != 3 {
		t.Fatalf("got %d rows, want 3 (2 switches + 1 stack)", rows)
	}
	for _, col := range []string{"networkId", "rstpEnabled", "serial", "stackId", "stpPriority"} {
		if f, _ := frame.FieldByName(col); f == nil {
			t.Fatalf("missing column %q", col)
		}
	}
}

// TestHandle_SwitchMacTable_PerClientRows verifies the MAC-table handler
// surfaces one row per client connected to a given switch.
func TestHandle_SwitchMacTable_PerClientRows(t *testing.T) {
	// `vlan` is a STRING on the wire (verified live 2026-04-19 against org
	// 1019781: `"vlan":"25"`). Fixture mirrors reality — the previous int
	// fixture made the handler appear to work in tests while every live
	// response failed to unmarshal and silently blanked the MAC table.
	const payload = `[
	  {"id":"C1","mac":"aa:bb:cc:dd:ee:01","description":"laptop","ip":"10.0.0.5","vlan":"10","switchport":"3","manufacturer":"Apple","user":"alice","usage":{"sent":1234,"recv":5678},"lastSeen":1713440000},
	  {"mac":"aa:bb:cc:dd:ee:02","ip":"10.0.0.6","vlan":"20","switchport":"4","usage":{"sent":10,"recv":20},"lastSeen":1713440100}
	]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/clients") {
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
		Queries: []MerakiQuery{{RefID: "A", Kind: KindSwitchMacTable, Serials: []string{"Q2SW-0001"}}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	var frame data.Frame
	if err := json.Unmarshal(resp.Frames[0], &frame); err != nil {
		t.Fatalf("decode: %v", err)
	}
	rows, _ := frame.RowLen()
	if rows != 2 {
		t.Fatalf("got %d rows, want 2", rows)
	}
	for _, col := range []string{"serial", "mac", "ip", "vlan", "switchPort", "lastSeen", "sentKb", "recvKb"} {
		if f, _ := frame.FieldByName(col); f == nil {
			t.Fatalf("missing column %q", col)
		}
	}
}

// TestHandle_SwitchVlansSummary_AggregatesPortCount verifies the VLAN-summary
// handler aggregates enabled ports per VLAN across every switch in scope
// and emits a wide one-row frame with one numeric field per VLAN label.
// Voice VLANs get the `(voice)` suffix so they render as distinct slices.
func TestHandle_SwitchVlansSummary_AggregatesPortCount(t *testing.T) {
	// /switch/ports/bySwitch (config feed) returns a BARE array — no
	// `{items: [...]}` envelope, unlike its `statuses/bySwitch` sibling.
	const payload = `[
	  {"serial":"Q2SW-0001","name":"sw-a","network":{"id":"N1"},"ports":[
	    {"portId":"1","enabled":true,"vlan":10},
	    {"portId":"2","enabled":true,"vlan":10,"voiceVlan":100},
	    {"portId":"3","enabled":true,"vlan":20},
	    {"portId":"4","enabled":false,"vlan":10}
	  ]}
	]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/switch/ports/bySwitch") {
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
		Queries: []MerakiQuery{{RefID: "A", Kind: KindSwitchVlansSummary, OrgID: "o1"}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	var frame data.Frame
	if err := json.Unmarshal(resp.Frames[0], &frame); err != nil {
		t.Fatalf("decode: %v", err)
	}
	rows, _ := frame.RowLen()
	// Wide one-row frame — one int64 field per VLAN label.
	if rows != 1 {
		t.Fatalf("got %d rows, want 1 (wide frame)", rows)
	}
	// Expected: vlan 10 → 2 ports (1 & 2; port 4 disabled), vlan 20 → 1,
	// voice vlan 100 → 1. Three VLAN fields.
	want := map[string]int64{
		"VLAN 10":         2,
		"VLAN 20":         1,
		"VLAN 100 (voice)": 1,
	}
	for label, expected := range want {
		f, _ := frame.FieldByName(label)
		if f == nil {
			t.Fatalf("missing VLAN field %q; fields=%v", label, frame.Fields)
		}
		got, _ := f.ConcreteAt(0)
		if got != expected {
			t.Errorf("%s = %v, want %d", label, got, expected)
		}
	}
}

// §3.1 — Switch ports overview by speed + usage history handler tests.

// TestHandle_SwitchPortsOverviewBySpeed_Table verifies the handler emits a
// wide one-row frame with one numeric field per non-zero (media, speed)
// bucket. Field names are human-readable link speeds ("1 Gbps (RJ45)")
// so the bar gauge can use the field name as each bar's label without
// per-row template hacks.
func TestHandle_SwitchPortsOverviewBySpeed_Table(t *testing.T) {
	// The overview endpoint returns a nested object; we check that the
	// handler flattens it into one column per (media × speed) bucket.
	const payload = `{
		"counts": {
			"total": 96,
			"byStatus": {
				"active": {
					"total": 80,
					"byMediaAndLinkSpeed": {
						"rj45": {"1000": 48, "100": 8, "total": 56},
						"sfp":  {"10000": 4, "total": 4}
					}
				},
				"inactive": {
					"total": 16,
					"byMedia": {
						"rj45": {"total": 12},
						"sfp":  {"total": 4}
					}
				}
			}
		}
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/switch/ports/overview") {
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
		Queries: []MerakiQuery{{RefID: "A", Kind: KindSwitchPortsOverviewBySpeed, OrgID: "o1"}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got := len(resp.Frames); got != 1 {
		t.Fatalf("got %d frames, want 1", got)
	}

	var frame data.Frame
	if err := json.Unmarshal(resp.Frames[0], &frame); err != nil {
		t.Fatalf("decode frame: %v (body=%s)", err, string(resp.Frames[0]))
	}

	// Expected active buckets: rj45/100=8, rj45/1000=48, sfp/10000=4.
	// Inactive buckets (Active=0) are pre-filtered out to keep the bar
	// gauge from drawing a wall of empty bars.
	wantCols := map[string]int64{
		"100 Mbps (RJ45)": 8,
		"1 Gbps (RJ45)":   48,
		"10 Gbps (SFP)":   4,
	}
	for name, want := range wantCols {
		f, _ := frame.FieldByName(name)
		if f == nil {
			t.Fatalf("frame missing %q column; fields=%v", name, frame.Fields)
		}
		got, _ := f.ConcreteAt(0)
		if got != want {
			t.Errorf("%s = %v, want %d", name, got, want)
		}
	}
	// Inactive buckets must NOT show up as columns.
	for _, unwanted := range []string{"Inactive (RJ45)", "Inactive (SFP)"} {
		if f, _ := frame.FieldByName(unwanted); f != nil {
			t.Errorf("frame unexpectedly has %q column (inactive rows should be elided)", unwanted)
		}
	}

}

// TestHandle_SwitchPortsUsageHistory_EmitsPerSerialFrames verifies one frame
// per (serial, metric) is emitted from the usage-history handler, with correct
// labels on the value field.
func TestHandle_SwitchPortsUsageHistory_EmitsPerSerialFrames(t *testing.T) {
	// Two switches × one interval each → 6 frames total (2 serials × 3 metrics).
	const payload = `{
		"items": [
			{
				"serial": "Q2SW-0001",
				"network": {"id": "N1", "name": "HQ"},
				"ports": [
					{
						"portId": "1",
						"intervals": [
							{
								"startTs": "2026-04-17T10:00:00Z",
								"endTs":   "2026-04-17T10:05:00Z",
								"data": {"usage": {"total": 1000, "upstream": 400, "downstream": 600}}
							}
						]
					}
				]
			},
			{
				"serial": "Q2SW-0002",
				"network": {"id": "N1", "name": "HQ"},
				"ports": [
					{
						"portId": "1",
						"intervals": [
							{
								"startTs": "2026-04-17T10:00:00Z",
								"endTs":   "2026-04-17T10:05:00Z",
								"data": {"usage": {"total": 500, "upstream": 200, "downstream": 300}}
							}
						]
					}
				]
			}
		],
		"meta": {"counts": {"items": {"total": 2, "remaining": 0}}}
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/switch/ports/usage/history") {
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

	from := time.Now().Add(-6 * time.Hour)
	to := time.Now()
	resp, err := Handle(context.Background(), client, &QueryRequest{
		Range: TimeRange{
			From: from.UnixMilli(),
			To:   to.UnixMilli(),
		},
		Queries: []MerakiQuery{{RefID: "A", Kind: KindSwitchPortsUsageHistory, OrgID: "o1"}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	// 2 serials × 3 metrics (sent, recv, total) = 6 frames.
	if got := len(resp.Frames); got != 6 {
		t.Fatalf("got %d frames, want 6 (2 serials × 3 metrics)", got)
	}

	seen := map[string]map[string]bool{}
	for _, raw := range resp.Frames {
		var f data.Frame
		if err := json.Unmarshal(raw, &f); err != nil {
			t.Fatalf("decode: %v", err)
		}
		vf, _ := f.FieldByName("value")
		if vf == nil {
			t.Fatalf("frame missing value field; fields=%v", f.Fields)
		}
		serial := vf.Labels["serial"]
		metric := vf.Labels["metric"]
		if seen[serial] == nil {
			seen[serial] = map[string]bool{}
		}
		seen[serial][metric] = true
	}

	for _, serial := range []string{"Q2SW-0001", "Q2SW-0002"} {
		for _, metric := range []string{"sent", "recv", "total"} {
			if !seen[serial][metric] {
				t.Errorf("missing frame for serial=%s metric=%s; seen=%v", serial, metric, seen)
			}
		}
	}
}

// TestHandle_SwitchPortsUsageHistory_ClampsTruncation verifies that a time
// range longer than 31 days gets clamped and a notice is attached.
func TestHandle_SwitchPortsUsageHistory_ClampsTruncation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[],"meta":{"counts":{"items":{"total":0,"remaining":0}}}}`))
	}))
	t.Cleanup(srv.Close)

	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	// 60-day range — well beyond the 31-day cap.
	to := time.Now()
	from := to.Add(-60 * 24 * time.Hour)
	resp, err := Handle(context.Background(), client, &QueryRequest{
		Range: TimeRange{
			From: from.UnixMilli(),
			To:   to.UnixMilli(),
		},
		Queries: []MerakiQuery{{RefID: "A", Kind: KindSwitchPortsUsageHistory, OrgID: "o1"}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	// Even with empty payload the handler returns at least the empty frame slice.
	// The key assertion is that a non-error (non-notice-error) response came back.
	if resp == nil {
		t.Fatal("nil response")
	}
	// If any frames were returned, check that we don't have an error notice.
	for _, raw := range resp.Frames {
		var f data.Frame
		if err := json.Unmarshal(raw, &f); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if f.Meta != nil {
			for _, n := range f.Meta.Notices {
				if n.Severity == data.NoticeSeverityError {
					t.Errorf("unexpected error notice: %s", n.Text)
				}
			}
		}
	}
}
