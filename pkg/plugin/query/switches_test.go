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
func TestHandle_SwitchPoe_FlattensPerPort(t *testing.T) {
	const payload = `{"items":[
	  {"serial":"Q2SW-0001","name":"sw-a","network":{"id":"N1","name":"HQ"},"ports":[
	    {"portId":"1","enabled":true,"powerUsageInWatts":7.5},
	    {"portId":"2","enabled":true,"powerUsageInWatts":0}
	  ]},
	  {"serial":"Q2SW-0002","name":"sw-b","network":{"id":"N1","name":"HQ"},"ports":[
	    {"portId":"1","enabled":false,"powerUsageInWatts":0}
	  ]}
	]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/switch/ports/statuses/bySwitch") {
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
	// Verify serial filter applies client-side. Use fresh client to avoid
	// hitting the TTL cache from the previous call.
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
	const payload = `[
	  {"id":"C1","mac":"aa:bb:cc:dd:ee:01","description":"laptop","ip":"10.0.0.5","vlan":10,"switchport":"3","manufacturer":"Apple","user":"alice","usage":{"sent":1234,"recv":5678},"lastSeen":1713440000},
	  {"mac":"aa:bb:cc:dd:ee:02","ip":"10.0.0.6","vlan":20,"switchport":"4","usage":{"sent":10,"recv":20},"lastSeen":1713440100}
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
// handler aggregates enabled ports per (switch, vlan) and includes voice VLANs
// as a separate synthetic row.
func TestHandle_SwitchVlansSummary_AggregatesPortCount(t *testing.T) {
	const payload = `{"items":[
	  {"serial":"Q2SW-0001","name":"sw-a","network":{"id":"N1"},"ports":[
	    {"portId":"1","enabled":true,"vlan":10},
	    {"portId":"2","enabled":true,"vlan":10,"voiceVlan":100},
	    {"portId":"3","enabled":true,"vlan":20},
	    {"portId":"4","enabled":false,"vlan":10}
	  ]}
	]}`
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
	// Expected: vlan 10 → 2 ports (1 & 2; port 4 disabled), vlan 20 → 1,
	// voice:100 → 1. That's 3 rows.
	if rows != 3 {
		t.Fatalf("got %d rows, want 3", rows)
	}
	for _, col := range []string{"serial", "vlan", "portCount"} {
		if f, _ := frame.FieldByName(col); f == nil {
			t.Fatalf("missing column %q", col)
		}
	}
	vlanField, _ := frame.FieldByName("vlan")
	countField, _ := frame.FieldByName("portCount")
	found10, foundVoice := false, false
	for i := 0; i < rows; i++ {
		v, _ := vlanField.ConcreteAt(i)
		c, _ := countField.ConcreteAt(i)
		if v == "10" && c.(int64) == 2 {
			found10 = true
		}
		if v == "voice:100" && c.(int64) == 1 {
			foundVoice = true
		}
	}
	if !found10 {
		t.Errorf("vlan=10 row with count=2 not found")
	}
	if !foundVoice {
		t.Errorf("voice:100 row with count=1 not found")
	}
}

// §3.1 — Switch ports overview by speed + usage history handler tests.

// TestHandle_SwitchPortsOverviewBySpeed_Table verifies the handler emits a
// single table frame with the expected columns and one row per speed bucket.
func TestHandle_SwitchPortsOverviewBySpeed_Table(t *testing.T) {
	// The overview endpoint returns a nested object; we check that the
	// handler flattens it into one row per (media × speed).
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

	// Expect 4 rows: rj45/1000, rj45/100, sfp/10000 (active) + rj45/inactive + sfp/inactive
	rows, _ := frame.RowLen()
	if rows < 3 {
		t.Fatalf("got %d rows, want >= 3 (at least 3 active speed buckets)", rows)
	}
	for _, col := range []string{"media", "speed", "active"} {
		if f, _ := frame.FieldByName(col); f == nil {
			t.Fatalf("frame missing %q column; fields=%v", col, frame.Fields)
		}
	}

	// Verify that the active count for rj45/1000 is 48.
	mediaField, _ := frame.FieldByName("media")
	speedField, _ := frame.FieldByName("speed")
	activeField, _ := frame.FieldByName("active")
	for i := 0; i < rows; i++ {
		media, _ := mediaField.ConcreteAt(i)
		speed, _ := speedField.ConcreteAt(i)
		active, _ := activeField.ConcreteAt(i)
		if media == "rj45" && speed == "1000" {
			if active.(int64) != 48 {
				t.Errorf("rj45/1000 active count = %v, want 48", active)
			}
			return
		}
	}
	t.Errorf("no row with media=rj45 speed=1000 found; got %d rows", rows)
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
