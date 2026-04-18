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

// Phase 8 (MX appliance + VPN) handler tests. Each test stubs the Meraki API
// via httptest.NewServer and asserts the emitted frame shape. The tests live in
// a dedicated file (rather than dispatch_test.go) so concurrent Wave 2 work
// editing dispatch_test.go doesn't merge-conflict with this file.

// TestHandle_ApplianceUplinkStatuses_FlattensInterfaces covers the core
// flatten-on-interface behaviour of handleApplianceUplinkStatuses. The stub
// returns one MX appliance with two uplinks; the handler must emit one frame
// with two rows, one per (serial, interface) pair, preserving the status
// column and computing a drilldown URL per row.
func TestHandle_ApplianceUplinkStatuses_FlattensInterfaces(t *testing.T) {
	const uplinksPayload = `[
	  {
	    "serial": "Q2XX-APPL-0001",
	    "model": "MX68",
	    "networkId": "N1",
	    "lastReportedAt": "2026-04-17T12:34:56Z",
	    "highAvailability": {"role": "primary", "enabled": true},
	    "uplinks": [
	      {"interface": "wan1", "status": "active", "ip": "10.0.0.1", "gateway": "10.0.0.254", "publicIp": "1.2.3.4", "primaryDns": "1.1.1.1", "secondaryDns": "8.8.8.8", "ipAssignedBy": "dhcp"},
	      {"interface": "wan2", "status": "ready", "ip": "10.0.1.1", "gateway": "10.0.1.254", "publicIp": "5.6.7.8", "primaryDns": "1.1.1.1", "secondaryDns": "", "ipAssignedBy": "static"}
	    ]
	  }
	]`
	// Not critical but exercised by the handler's best-effort network name lookup.
	const networksPayload = `[
	  {"id": "N1", "organizationId": "o1", "name": "Branch-HQ", "productTypes": ["appliance"]}
	]`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/appliance/uplink/statuses"):
			_, _ = w.Write([]byte(uplinksPayload))
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
		Queries: []MerakiQuery{{RefID: "A", Kind: KindApplianceUplinkStatuses, OrgID: "o1"}},
	}, Options{PluginPathPrefix: "/a/rknightion-meraki-app"})
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
	rows, err := frame.RowLen()
	if err != nil {
		t.Fatalf("RowLen: %v", err)
	}
	if rows != 2 {
		t.Fatalf("got %d rows, want 2 (1 appliance × 2 uplinks)", rows)
	}

	// Both statuses should appear (order matches insertion).
	statusField, _ := frame.FieldByName("status")
	if statusField == nil {
		t.Fatalf("frame missing status column; fields=%v", frame.Fields)
	}
	gotStatuses := map[string]int{}
	for i := range rows {
		v, _ := statusField.ConcreteAt(i)
		gotStatuses[v.(string)]++
	}
	if gotStatuses["active"] != 1 {
		t.Fatalf("active status count = %d, want 1 (got %v)", gotStatuses["active"], gotStatuses)
	}
	if gotStatuses["ready"] != 1 {
		t.Fatalf("ready status count = %d, want 1 (got %v)", gotStatuses["ready"], gotStatuses)
	}

	// drilldownUrl should be populated for every row since PluginPathPrefix was set.
	drillField, _ := frame.FieldByName("drilldownUrl")
	if drillField == nil {
		t.Fatalf("frame missing drilldownUrl column; fields=%v", frame.Fields)
	}
	for i := range rows {
		got, _ := drillField.ConcreteAt(i)
		url := got.(string)
		if url == "" {
			t.Fatalf("row %d drilldownUrl empty", i)
		}
		if !strings.Contains(url, "Q2XX-APPL-0001") {
			t.Fatalf("row %d drilldownUrl = %q, want contains serial", i, url)
		}
		if !strings.Contains(url, "/appliances/") {
			t.Fatalf("row %d drilldownUrl = %q, want /appliances/ family prefix", i, url)
		}
	}

	// Network name should resolve to "Branch-HQ" via the networks lookup.
	nameField, _ := frame.FieldByName("networkName")
	if nameField == nil {
		t.Fatalf("frame missing networkName column")
	}
	if got, _ := nameField.ConcreteAt(0); got != "Branch-HQ" {
		t.Fatalf("networkName = %v, want Branch-HQ", got)
	}
}

// TestHandle_ApplianceUplinksOverview_ProducesWideRow verifies the KPI wide
// frame shape: one row, five int64 fields (active, ready, failed, notConnected,
// total), with total computed from the four status buckets.
func TestHandle_ApplianceUplinksOverview_ProducesWideRow(t *testing.T) {
	const payload = `{"counts":{"byStatus":{"active":5,"ready":1,"failed":2,"notConnected":3}}}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/appliance/uplinks/statuses/overview") {
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
		Queries: []MerakiQuery{{RefID: "A", Kind: KindApplianceUplinksOverview, OrgID: "o1"}},
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
		t.Fatalf("got %d rows, want 1 (KPI wide frame)", rows)
	}

	wants := map[string]int64{
		"active":       5,
		"ready":        1,
		"failed":       2,
		"notConnected": 3,
		"total":        11,
	}
	for name, want := range wants {
		f, _ := frame.FieldByName(name)
		if f == nil {
			t.Fatalf("frame missing %q field; fields=%v", name, frame.Fields)
		}
		v, _ := f.ConcreteAt(0)
		got, ok := v.(int64)
		if !ok {
			t.Fatalf("%s field is %T, want int64", name, v)
		}
		if got != want {
			t.Fatalf("%s = %d, want %d", name, got, want)
		}
	}
}

// TestHandle_ApplianceVpnStatuses_FlattensPeerKinds verifies the VPN statuses
// handler flattens both peer arrays (merakiVpnPeers + thirdPartyVpnPeers) into
// one row per peer with a `peerKind` column distinguishing the origin. Meraki
// peers carry networkId; thirdParty peers carry name + publicIp.
func TestHandle_ApplianceVpnStatuses_FlattensPeerKinds(t *testing.T) {
	const payload = `[
	  {
	    "networkId": "N1",
	    "networkName": "HQ",
	    "deviceStatus": "online",
	    "deviceSerial": "Q2XX-MX-0001",
	    "vpnMode": "hub",
	    "merakiVpnPeers": [
	      {
	        "networkId": "N2",
	        "networkName": "Branch-1",
	        "reachability": "reachable",
	        "usageSummary": {"sentKilobytes": 500, "receivedKilobytes": 1000}
	      }
	    ],
	    "thirdPartyVpnPeers": [
	      {"name": "AWS-VPG", "publicIp": "54.239.28.85", "reachability": "unreachable"}
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
		Queries: []MerakiQuery{{RefID: "A", Kind: KindApplianceVpnStatuses, OrgID: "o1"}},
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
	if rows != 2 {
		t.Fatalf("got %d rows, want 2 (1 meraki peer + 1 thirdParty peer)", rows)
	}

	kindField, _ := frame.FieldByName("peerKind")
	reachField, _ := frame.FieldByName("reachability")
	if kindField == nil || reachField == nil {
		t.Fatalf("frame missing peerKind/reachability column; fields=%v", frame.Fields)
	}

	seenKinds := map[string]string{}
	for i := range rows {
		k, _ := kindField.ConcreteAt(i)
		r, _ := reachField.ConcreteAt(i)
		seenKinds[k.(string)] = r.(string)
	}
	if seenKinds["meraki"] != "reachable" {
		t.Fatalf("meraki peer reachability = %q, want reachable (seen=%v)", seenKinds["meraki"], seenKinds)
	}
	if seenKinds["thirdParty"] != "unreachable" {
		t.Fatalf("thirdParty peer reachability = %q, want unreachable (seen=%v)", seenKinds["thirdParty"], seenKinds)
	}
}

// TestHandle_ApplianceVpnStats_EmitsPerPairRow verifies that the stats handler
// merges the four summary arrays (latency/jitter/loss/mos) by their
// (senderUplink, receiverUplink) key, producing one row per pair.
func TestHandle_ApplianceVpnStats_EmitsPerPairRow(t *testing.T) {
	// One network, one peer, one matching (sender, receiver) pair across both
	// latency and loss summaries. Merging must produce a SINGLE row carrying
	// both avgLatencyMs and avgLossPercentage.
	const payload = `[
	  {
	    "networkId": "N1",
	    "networkName": "HQ",
	    "merakiVpnPeers": [
	      {
	        "networkId": "N2",
	        "networkName": "Branch-1",
	        "usageSummary": {"sentKilobytes": 1024, "receivedKilobytes": 2048},
	        "latencySummaries": [{"senderUplink": "wan1", "receiverUplink": "wan1", "avgLatencyMs": 15.5}],
	        "lossPercentageSummaries": [{"senderUplink": "wan1", "receiverUplink": "wan1", "avgLossPercentage": 0.25}]
	      }
	    ]
	  }
	]`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/appliance/vpn/stats") {
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

	now := time.Now().UTC()
	resp, err := Handle(context.Background(), client, &QueryRequest{
		Range: TimeRange{
			From: now.Add(-2 * time.Hour).UnixMilli(),
			To:   now.UnixMilli(),
		},
		Queries: []MerakiQuery{{RefID: "A", Kind: KindApplianceVpnStats, OrgID: "o1"}},
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
		t.Fatalf("got %d rows, want 1 (merged pair)", rows)
	}

	// Both metrics should be populated on the single merged row.
	latencyField, _ := frame.FieldByName("avgLatencyMs")
	lossField, _ := frame.FieldByName("avgLossPercentage")
	if latencyField == nil || lossField == nil {
		t.Fatalf("frame missing latency/loss columns; fields=%v", frame.Fields)
	}
	if v, _ := latencyField.ConcreteAt(0); v != float64(15.5) {
		t.Fatalf("avgLatencyMs = %v, want 15.5", v)
	}
	if v, _ := lossField.ConcreteAt(0); v != float64(0.25) {
		t.Fatalf("avgLossPercentage = %v, want 0.25", v)
	}

	// Usage summary fields were threaded through the merge.
	sentField, _ := frame.FieldByName("sentKilobytes")
	if v, _ := sentField.ConcreteAt(0); v != int64(1024) {
		t.Fatalf("sentKilobytes = %v, want 1024", v)
	}
}

// TestHandle_DeviceUplinksLossLatency_ClampsTo5Min verifies the handler
// resolves a >5-min window down to the 5-minute endpoint cap (truncation
// annotation emitted as a frame notice), and emits two frames per
// (serial, uplink, ip) combination — one each for the lossPercent and
// latencyMs metrics. Null samples must survive as *float64 nil values so
// gaps render correctly in the timeseries panel.
func TestHandle_DeviceUplinksLossLatency_ClampsTo5Min(t *testing.T) {
	// One (serial, uplink, ip) combo with one null + one real sample per
	// metric. The null enforces the nullable-preservation contract.
	const lossLatencyPayload = `[
	  {
	    "serial": "Q2XX-APPL-0001",
	    "uplink": "wan1",
	    "ip": "1.2.3.4",
	    "timeSeries": [
	      {"ts": "2026-04-17T12:00:00Z", "lossPercent": 0.0, "latencyMs": 12.5},
	      {"ts": "2026-04-17T12:01:00Z", "lossPercent": null, "latencyMs": null}
	    ]
	  }
	]`
	// Empty networks + devices responses so the best-effort lookups don't 500 the test.
	const networksPayload = `[]`
	const devicesPayload = `[]`

	var capturedT0, capturedT1 string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/devices/uplinksLossAndLatency"):
			capturedT0 = r.URL.Query().Get("t0")
			capturedT1 = r.URL.Query().Get("t1")
			_, _ = w.Write([]byte(lossLatencyPayload))
		case strings.HasSuffix(r.URL.Path, "/networks"):
			_, _ = w.Write([]byte(networksPayload))
		case strings.HasSuffix(r.URL.Path, "/devices"):
			_, _ = w.Write([]byte(devicesPayload))
		default:
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	// Request a 1-hour range; the endpoint caps at 5 min so the resolver must
	// emit a truncation annotation.
	now := time.Now().UTC()
	resp, err := Handle(context.Background(), client, &QueryRequest{
		Range: TimeRange{
			From: now.Add(-1 * time.Hour).UnixMilli(),
			To:   now.UnixMilli(),
		},
		Queries: []MerakiQuery{{RefID: "A", Kind: KindDeviceUplinksLossLatency, OrgID: "o1"}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	// Confirm the request the handler sent respected the 5-minute cap: parse
	// the t0/t1 the test server captured and check their delta.
	t0, parseErr := time.Parse(time.RFC3339, capturedT0)
	if parseErr != nil {
		t.Fatalf("could not parse captured t0 %q: %v", capturedT0, parseErr)
	}
	t1, parseErr := time.Parse(time.RFC3339, capturedT1)
	if parseErr != nil {
		t.Fatalf("could not parse captured t1 %q: %v", capturedT1, parseErr)
	}
	if delta := t1.Sub(t0); delta != 5*time.Minute {
		t.Fatalf("t1-t0 = %s, want 5m (handler should clamp a 1h range to the endpoint cap)", delta)
	}

	// Expect two frames: one per metric (loss, latency) for the single combo.
	if got := len(resp.Frames); got != 2 {
		t.Fatalf("got %d frames, want 2 (one per metric)", got)
	}

	// Verify at least one frame carries the truncation notice. Notices are
	// attached to the first frame only per the dispatcher contract.
	var firstFrame data.Frame
	if err := json.Unmarshal(resp.Frames[0], &firstFrame); err != nil {
		t.Fatalf("decode frame 0: %v", err)
	}
	if meta := firstFrame.Meta; meta == nil || len(meta.Notices) == 0 {
		t.Fatalf("frame[0] missing truncation notice; meta=%+v", meta)
	}
	foundTruncNotice := false
	for _, n := range firstFrame.Meta.Notices {
		if strings.Contains(strings.ToLower(n.Text), "truncated") {
			foundTruncNotice = true
			break
		}
	}
	if !foundTruncNotice {
		t.Fatalf("frame[0] notices did not mention truncation; got %+v", firstFrame.Meta.Notices)
	}

	// Verify that the value field is *float64 and nil samples round-trip.
	// Collect the two frames and find the one whose metric label is lossPercent.
	var lossFrame data.Frame
	foundLoss := false
	for _, raw := range resp.Frames {
		var f data.Frame
		if err := json.Unmarshal(raw, &f); err != nil {
			t.Fatalf("decode frame: %v", err)
		}
		vf, _ := f.FieldByName("value")
		if vf == nil {
			continue
		}
		if vf.Labels["metric"] == "lossPercent" {
			lossFrame = f
			foundLoss = true
			break
		}
	}
	if !foundLoss {
		t.Fatalf("no frame carried metric=lossPercent label")
	}
	vf, _ := lossFrame.FieldByName("value")
	if vf.Len() != 2 {
		t.Fatalf("loss frame has %d samples, want 2", vf.Len())
	}
	// Second sample should be a nil *float64 preserving the gap.
	second, _ := vf.At(1).(*float64)
	if second != nil {
		t.Fatalf("loss frame row 1 = %v, want nil (null preserved)", second)
	}
	first, _ := vf.At(0).(*float64)
	if first == nil {
		t.Fatalf("loss frame row 0 is nil, want 0.0 (*float64 populated)")
	}
	if *first != 0.0 {
		t.Fatalf("loss frame row 0 = %v, want 0.0", *first)
	}

	// Sanity-check the label set.
	if got := vf.Labels["serial"]; got != "Q2XX-APPL-0001" {
		t.Fatalf("loss frame serial label = %q, want Q2XX-APPL-0001", got)
	}
	if got := vf.Labels["uplink"]; got != "wan1" {
		t.Fatalf("loss frame uplink label = %q, want wan1", got)
	}
}

// TestHandle_PortForwardingRules_PerNetwork verifies the port-forwarding
// handler concatenates rules across multiple networks into one table and
// annotates each row with the owning networkId.
func TestHandle_PortForwardingRules_PerNetwork(t *testing.T) {
	// Two networks, each with one rule, returned by separate paths.
	const rulesN1 = `{"rules":[{"name":"Web-N1","protocol":"tcp","publicPort":"443","localPort":"443","lanIp":"10.0.0.10","uplink":"wan1","allowedIps":["any"]}]}`
	const rulesN2 = `{"rules":[{"name":"SSH-N2","protocol":"tcp","publicPort":"2222","localPort":"22","lanIp":"10.1.0.10","uplink":"wan2","allowedIps":["192.0.2.0/24"]}]}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/networks/N1/appliance/firewall/portForwardingRules"):
			_, _ = w.Write([]byte(rulesN1))
		case strings.Contains(r.URL.Path, "/networks/N2/appliance/firewall/portForwardingRules"):
			_, _ = w.Write([]byte(rulesN2))
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
		Queries: []MerakiQuery{{RefID: "A", Kind: KindAppliancePortForwarding, NetworkIDs: []string{"N1", "N2"}}},
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
	if rows != 2 {
		t.Fatalf("got %d rows, want 2 (one rule per network)", rows)
	}

	// Spot-check that both networkIDs are present.
	netIDField, _ := frame.FieldByName("networkId")
	if netIDField == nil {
		t.Fatalf("frame missing networkId column; fields=%v", frame.Fields)
	}
	seenNetIDs := map[string]bool{}
	for i := range rows {
		v, _ := netIDField.ConcreteAt(i)
		seenNetIDs[v.(string)] = true
	}
	if !seenNetIDs["N1"] {
		t.Fatalf("N1 rule missing from frame (seen=%v)", seenNetIDs)
	}
	if !seenNetIDs["N2"] {
		t.Fatalf("N2 rule missing from frame (seen=%v)", seenNetIDs)
	}
}

// TestHandle_ApplianceSettings_EmitsRowPerNetwork verifies the settings
// handler concatenates per-network config snapshots into one table, preserving
// the deploymentMode column.
func TestHandle_ApplianceSettings_EmitsRowPerNetwork(t *testing.T) {
	const settingsN1 = `{"clientTrackingMethod":"MAC address","deploymentMode":"routed","dynamicDns":{"enabled":true,"prefix":"hq","url":"hq-123.dynamic-m.com"}}`
	const settingsN2 = `{"clientTrackingMethod":"IP address","deploymentMode":"passthrough","dynamicDns":{"enabled":false,"prefix":"","url":""}}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/networks/N1/appliance/settings"):
			_, _ = w.Write([]byte(settingsN1))
		case strings.Contains(r.URL.Path, "/networks/N2/appliance/settings"):
			_, _ = w.Write([]byte(settingsN2))
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
		Queries: []MerakiQuery{{RefID: "A", Kind: KindApplianceSettings, NetworkIDs: []string{"N1", "N2"}}},
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
	if rows != 2 {
		t.Fatalf("got %d rows, want 2 (one per network)", rows)
	}

	// Both deployment modes present + tied to the correct network.
	modeField, _ := frame.FieldByName("deploymentMode")
	netIDField, _ := frame.FieldByName("networkId")
	if modeField == nil || netIDField == nil {
		t.Fatalf("frame missing deploymentMode or networkId column; fields=%v", frame.Fields)
	}
	modeByNet := map[string]string{}
	for i := range rows {
		net, _ := netIDField.ConcreteAt(i)
		mode, _ := modeField.ConcreteAt(i)
		modeByNet[net.(string)] = mode.(string)
	}
	if modeByNet["N1"] != "routed" {
		t.Fatalf("N1 deploymentMode = %q, want routed (seen=%v)", modeByNet["N1"], modeByNet)
	}
	if modeByNet["N2"] != "passthrough" {
		t.Fatalf("N2 deploymentMode = %q, want passthrough (seen=%v)", modeByNet["N2"], modeByNet)
	}
}
