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

// v0.8 — handler tests for the richer switch visualisations.

// TestHandle_SwitchFleetPowerHistory_EmitsTimeseriesFrame verifies the handler
// parses the `[{ts, draw}]` response into a two-field frame and sorts by
// timestamp ascending.
func TestHandle_SwitchFleetPowerHistory_EmitsTimeseriesFrame(t *testing.T) {
	// Intentionally out-of-order to confirm sorting.
	const payload = `[
	  {"ts":"2026-04-19T04:00:00Z","draw":64.5},
	  {"ts":"2026-04-19T00:00:00Z","draw":62.0},
	  {"ts":"2026-04-19T08:00:00Z","draw":60.1}
	]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/summary/switch/power/history") {
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
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	from := now.Add(-24 * time.Hour)
	resp, err := Handle(context.Background(), client, &QueryRequest{
		Range:   TimeRange{From: from.UnixMilli(), To: now.UnixMilli()},
		Queries: []MerakiQuery{{RefID: "A", Kind: KindSwitchFleetPowerHistory, OrgID: "o1"}},
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
	tsField, _ := frame.FieldByName("ts")
	if tsField == nil {
		t.Fatalf("missing ts field")
	}
	t0 := tsField.At(0).(time.Time)
	t1 := tsField.At(1).(time.Time)
	if !t0.Before(t1) {
		t.Fatalf("expected ascending sort, got %v >= %v", t0, t1)
	}
	valField, _ := frame.FieldByName("drawWatts")
	if valField == nil {
		t.Fatalf("missing drawWatts field")
	}
	if valField.At(0).(float64) != 62.0 {
		t.Fatalf("expected first row watts=62.0, got %v", valField.At(0))
	}
}

// TestHandle_SwitchPortsClientsOverview_AggregatesPerSwitch confirms the
// handler sums per-port online clients into clientsOnline and counts
// non-zero ports into activePortCount.
func TestHandle_SwitchPortsClientsOverview_AggregatesPerSwitch(t *testing.T) {
	const payload = `{"items":[
	  {"name":"sw-a","serial":"Q2-A","mac":"00:00:00:00:00:01","network":{"id":"N1","name":"HQ"},"model":"MS120-8","ports":[
	    {"portId":"1","counts":{"byStatus":{"online":1}}},
	    {"portId":"2","counts":{"byStatus":{"online":2}}},
	    {"portId":"3","counts":{"byStatus":{"online":0}}}
	  ]},
	  {"name":"sw-b","serial":"Q2-B","network":{"id":"N1","name":"HQ"},"model":"MS220-8","ports":[
	    {"portId":"1","counts":{"byStatus":{"online":5}}}
	  ]}
	]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/switch/ports/clients/overview/byDevice") {
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
	}))
	t.Cleanup(srv.Close)

	client, _ := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	resp, err := Handle(context.Background(), client, &QueryRequest{
		Queries: []MerakiQuery{{RefID: "A", Kind: KindSwitchPortsClientsOverview, OrgID: "o1"}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	var frame data.Frame
	_ = json.Unmarshal(resp.Frames[0], &frame)
	rows, _ := frame.RowLen()
	if rows != 2 {
		t.Fatalf("got %d rows, want 2", rows)
	}
	clientsField, _ := frame.FieldByName("clientsOnline")
	activeField, _ := frame.FieldByName("activePortCount")
	if clientsField == nil || activeField == nil {
		t.Fatalf("missing aggregation columns: %+v", frame.Fields)
	}
	// sw-a (alphabetised by switch name): 1+2+0 = 3 online, 2 active ports
	if got := clientsField.At(0).(int64); got != 3 {
		t.Fatalf("sw-a clientsOnline: got %d want 3", got)
	}
	if got := activeField.At(0).(int64); got != 2 {
		t.Fatalf("sw-a activePortCount: got %d want 2", got)
	}
}

// TestHandle_SwitchNeighborsTopology_FlattensLldpCdp verifies LLDP and CDP
// entries become distinct rows with a `source` column, and filter-by-serial
// works.
func TestHandle_SwitchNeighborsTopology_FlattensLldpCdp(t *testing.T) {
	const payload = `{"items":[
	  {"name":"sw-a","serial":"Q2-A","network":{"id":"N1","name":"HQ"},"ports":[
	    {"portId":"4","lastUpdatedAt":"2026-04-19T17:51:32Z",
	      "lldp":[{"name":"System name","value":"AP-Lounge"},{"name":"Port ID","value":"aa:bb:cc:dd:ee:ff"}],
	      "cdp":[]},
	    {"portId":"9","lastUpdatedAt":"2026-04-19T17:51:32Z",
	      "lldp":[],
	      "cdp":[{"name":"Device ID","value":"opnsense"},{"name":"Platform","value":"FreeBSD"},{"name":"Port ID","value":"WAN"}]}
	  ]},
	  {"name":"sw-b","serial":"Q2-B","network":{"id":"N1","name":"HQ"},"ports":[
	    {"portId":"1","lastUpdatedAt":"2026-04-19T17:51:32Z",
	      "lldp":[{"name":"System name","value":"host-jules"}],
	      "cdp":[]}
	  ]}
	]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/switch/ports/topology/discovery/byDevice") {
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
	}))
	t.Cleanup(srv.Close)

	client, _ := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	// No serial filter — expect 3 rows total (port 4 LLDP, port 9 CDP, port 1 LLDP).
	resp, err := Handle(context.Background(), client, &QueryRequest{
		Queries: []MerakiQuery{{RefID: "A", Kind: KindSwitchNeighborsTopology, OrgID: "o1"}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	var frame data.Frame
	_ = json.Unmarshal(resp.Frames[0], &frame)
	if rows, _ := frame.RowLen(); rows != 3 {
		t.Fatalf("unfiltered: got %d rows, want 3", rows)
	}
	srcField, _ := frame.FieldByName("source")
	peerField, _ := frame.FieldByName("peerSystemName")
	if srcField == nil || peerField == nil {
		t.Fatalf("missing expected columns: %+v", frame.Fields)
	}
	// Confirm both LLDP and CDP rows present.
	seenSources := map[string]bool{}
	for i := 0; i < 3; i++ {
		seenSources[srcField.At(i).(string)] = true
	}
	if !seenSources["LLDP"] || !seenSources["CDP"] {
		t.Fatalf("expected both LLDP and CDP rows, got %+v", seenSources)
	}

	// Filter by serial Q2-B — expect 1 row.
	client2, _ := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	resp, _ = Handle(context.Background(), client2, &QueryRequest{
		Queries: []MerakiQuery{{RefID: "A", Kind: KindSwitchNeighborsTopology, OrgID: "o1", Serials: []string{"Q2-B"}}},
	}, Options{})
	var frame2 data.Frame
	_ = json.Unmarshal(resp.Frames[0], &frame2)
	if rows, _ := frame2.RowLen(); rows != 1 {
		t.Fatalf("filtered: got %d rows, want 1", rows)
	}
}

// TestHandle_NetworkDhcpServersSeen_WithNetworkIDs verifies the handler maps
// the v4/servers/seen payload into a DHCP-rogue table, with `trusted`
// reflecting the API's `isAllowed` bool.
func TestHandle_NetworkDhcpServersSeen_WithNetworkIDs(t *testing.T) {
	const payload = `[
	  {"mac":"aa:bb:cc:dd:ee:01","ipv4":{"address":"10.0.0.1"},"vlan":1,"isAllowed":true,
	    "lastSeenAt":"2026-04-19T10:00:00Z","clientId":"client-1",
	    "lastPacket":{"type":"offer"},
	    "seenBy":[{"name":"sw-a","serial":"Q2-A","interface":"5"}]},
	  {"mac":"aa:bb:cc:dd:ee:02","ipv4":{"address":"10.0.0.99"},"vlan":1,"isAllowed":false,
	    "lastSeenAt":"2026-04-19T11:00:00Z","clientId":"client-2",
	    "lastPacket":{"type":"ack"},
	    "seenBy":[{"name":"sw-b","serial":"Q2-B","interface":"7"}]}
	]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/switch/dhcp/v4/servers/seen") {
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
	}))
	t.Cleanup(srv.Close)

	client, _ := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	resp, err := Handle(context.Background(), client, &QueryRequest{
		Queries: []MerakiQuery{{RefID: "A", Kind: KindNetworkDhcpServersSeen, OrgID: "o1", NetworkIDs: []string{"N1"}}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	var frame data.Frame
	_ = json.Unmarshal(resp.Frames[0], &frame)
	rows, _ := frame.RowLen()
	if rows != 2 {
		t.Fatalf("got %d rows, want 2", rows)
	}
	trustedField, _ := frame.FieldByName("trusted")
	if trustedField == nil {
		t.Fatalf("missing trusted column: %+v", frame.Fields)
	}
	if trustedField.At(0).(bool) != true || trustedField.At(1).(bool) != false {
		t.Fatalf("trusted values: got [%v, %v], want [true, false]", trustedField.At(0), trustedField.At(1))
	}
}

// TestHandle_NetworkSwitchStacks_FilterBySerial confirms that when a caller
// passes serials, only stacks containing at least one of them are emitted.
func TestHandle_NetworkSwitchStacks_FilterBySerial(t *testing.T) {
	// Org-level statuses used to resolve serial → network.
	const orgStatuses = `{"items":[
	  {"serial":"Q2-A","name":"sw-a","network":{"id":"N1","name":"HQ"},"ports":[]}
	]}`
	const stacksPayload = `[
	  {"id":"stack-1","name":"core","serials":["Q2-A","Q2-B"]},
	  {"id":"stack-2","name":"edge","serials":["Q2-X","Q2-Y"]}
	]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/switch/ports/statuses/bySwitch"):
			_, _ = w.Write([]byte(orgStatuses))
		case strings.Contains(r.URL.Path, "/switch/stacks"):
			_, _ = w.Write([]byte(stacksPayload))
		default:
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	client, _ := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	resp, err := Handle(context.Background(), client, &QueryRequest{
		Queries: []MerakiQuery{{RefID: "A", Kind: KindNetworkSwitchStacks, OrgID: "o1", Serials: []string{"Q2-A"}}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	var frame data.Frame
	_ = json.Unmarshal(resp.Frames[0], &frame)
	rows, _ := frame.RowLen()
	if rows != 1 {
		t.Fatalf("got %d rows, want 1 (only stack-1 matches Q2-A)", rows)
	}
	stackNameField, _ := frame.FieldByName("stackName")
	if got := stackNameField.At(0).(string); got != "core" {
		t.Fatalf("got stackName %q, want %q", got, "core")
	}
}

// TestHandle_SwitchRoutingInterfaces_404Fallback confirms that when the L3
// endpoint returns 404 (typical for L2-only models) the handler emits an
// empty frame rather than an error, so the always-visible "L3 interfaces"
// panel shows its no-value text.
func TestHandle_SwitchRoutingInterfaces_404Fallback(t *testing.T) {
	const orgStatuses = `{"items":[
	  {"serial":"Q2-L2","name":"sw-l2","network":{"id":"N1","name":"HQ"},"ports":[]}
	]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/switch/ports/statuses/bySwitch"):
			_, _ = w.Write([]byte(orgStatuses))
		case strings.Contains(r.URL.Path, "/switch/stacks"):
			_, _ = w.Write([]byte("[]"))
		case strings.Contains(r.URL.Path, "/switch/routing/interfaces"):
			http.Error(w, "not found", http.StatusNotFound)
		default:
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	client, _ := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	resp, err := Handle(context.Background(), client, &QueryRequest{
		Queries: []MerakiQuery{{RefID: "A", Kind: KindSwitchRoutingInterfaces, OrgID: "o1", Serials: []string{"Q2-L2"}}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	var frame data.Frame
	_ = json.Unmarshal(resp.Frames[0], &frame)
	rows, _ := frame.RowLen()
	if rows != 0 {
		t.Fatalf("got %d rows, want 0 (L2 switch 404 fallback)", rows)
	}
	// Schema fields must still be present so the panel can bind columns.
	for _, col := range []string{"interfaceId", "name", "subnet", "vlanId", "ipv4Address", "defaultGateway", "source"} {
		if f, _ := frame.FieldByName(col); f == nil {
			t.Fatalf("missing column %q in empty frame; fields=%v", col, frame.Fields)
		}
	}
}

// TestHandle_SwitchPorts_WidenedFrame verifies the new columns added by v0.8
// are present and populated when the device-scoped statuses feed returns
// the rich shape (errors, spanningTree, activeProfile, trafficInKbps).
func TestHandle_SwitchPorts_WidenedFrame(t *testing.T) {
	const orgPayload = `{"items":[
	  {"serial":"Q2-A","name":"sw-a","network":{"id":"N1","name":"HQ"},"ports":[{"portId":"1"}]}
	]}`
	const devStatusesPayload = `[
	  {"portId":"1","enabled":true,"status":"Connected","speed":"1 Gbps","duplex":"full",
	   "clientCount":1,"powerUsageInWh":2.1,"isUplink":false,
	   "errors":[],"warnings":[],
	   "spanningTree":{"statuses":["Forwarding","Is edge","Is peer-to-peer"]},
	   "activeProfile":{"id":"p1","name":"Access Points","isActive":true},
	   "securePort":{"enabled":false,"active":false,"authenticationStatus":"Disabled"},
	   "usageInKb":{"total":40239,"sent":40137,"recv":102},
	   "trafficInKbps":{"total":91.5,"sent":91.3,"recv":0.2}}
	]`
	const devConfigPayload = `[{"portId":"1","vlan":100,"allowedVlans":"all"}]`
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

	client, _ := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	resp, err := Handle(context.Background(), client, &QueryRequest{
		Queries: []MerakiQuery{{RefID: "A", Kind: KindSwitchPorts, OrgID: "o1", Serials: []string{"Q2-A"}}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	var frame data.Frame
	_ = json.Unmarshal(resp.Frames[0], &frame)

	// Every new column must be present.
	for _, col := range []string{"errors", "warnings", "isUplink", "stpState", "activeProfile", "trafficKbps", "trafficKbpsSent", "trafficKbpsRecv", "usageKbSent", "usageKbRecv", "secureAuth"} {
		if f, _ := frame.FieldByName(col); f == nil {
			t.Fatalf("missing column %q; fields=%v", col, frame.Fields)
		}
	}

	// Spot-check values on port 1.
	stp, _ := frame.FieldByName("stpState")
	if got := stp.At(0).(string); !strings.Contains(got, "Forwarding") {
		t.Fatalf("stpState: got %q, want to contain %q", got, "Forwarding")
	}
	prof, _ := frame.FieldByName("activeProfile")
	if got := prof.At(0).(string); got != "Access Points" {
		t.Fatalf("activeProfile: got %q, want %q", got, "Access Points")
	}
	tk, _ := frame.FieldByName("trafficKbps")
	if got := tk.At(0).(float64); got != 91.5 {
		t.Fatalf("trafficKbps: got %v, want 91.5", got)
	}
	secure, _ := frame.FieldByName("secureAuth")
	if got := secure.At(0).(string); got != "Disabled" {
		t.Fatalf("secureAuth: got %q, want %q", got, "Disabled")
	}
}

// TestHandle_SwitchPortConfig_WidenedFrame confirms the widened config
// frame includes every field the panel needs.
func TestHandle_SwitchPortConfig_WidenedFrame(t *testing.T) {
	const payload = `[
	  {"portId":"1","name":"office","enabled":true,"type":"access","vlan":100,
	   "rstpEnabled":true,"stpGuard":"root guard","linkNegotiation":"Auto negotiate",
	   "udld":"Alert only","isolationEnabled":false,"stormControlEnabled":true,
	   "daiTrusted":false,"portScheduleId":"sch1","adaptivePolicyGroupId":"apg1",
	   "accessPolicyType":"MAC allow list","accessPolicyNumber":0,
	   "macAllowList":["aa:aa:aa:aa:aa:aa","bb:bb:bb:bb:bb:bb"],
	   "stickyMacAllowList":["cc:cc:cc:cc:cc:cc"],"stickyMacAllowListLimit":5,
	   "tags":["printer"]}
	]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/switch/ports") {
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
	}))
	t.Cleanup(srv.Close)

	client, _ := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	resp, err := Handle(context.Background(), client, &QueryRequest{
		Queries: []MerakiQuery{{RefID: "A", Kind: KindSwitchPortConfig, OrgID: "o1", Serials: []string{"Q2-A"}}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	var frame data.Frame
	_ = json.Unmarshal(resp.Frames[0], &frame)

	for _, col := range []string{"rstpEnabled", "stpGuard", "linkNegotiation", "udld", "isolationEnabled", "stormControlEnabled", "daiTrusted", "portScheduleId", "adaptivePolicyGroupId", "accessPolicyType", "accessPolicyNumber", "macAllowList", "stickyMacAllowList", "stickyMacAllowListLimit"} {
		if f, _ := frame.FieldByName(col); f == nil {
			t.Fatalf("missing column %q; fields=%v", col, frame.Fields)
		}
	}
	macField, _ := frame.FieldByName("macAllowList")
	got := macField.At(0).(string)
	if !strings.Contains(got, "aa:aa:aa:aa:aa:aa") || !strings.Contains(got, "bb:bb:bb:bb:bb:bb") {
		t.Fatalf("macAllowList joined incorrectly: %q", got)
	}
}
