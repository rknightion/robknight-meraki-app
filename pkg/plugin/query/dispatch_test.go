package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// TestHandle_Organizations is a round-trip smoke test: stub the Meraki
// /organizations endpoint, run Handle with a single organizations query,
// and confirm we get a well-formed frame back with the stubbed row.
func TestHandle_Organizations(t *testing.T) {
	const payload = `[{"id":"o1","name":"Lab","url":"https://dashboard.meraki.com/o/o1","api":{"enabled":true}}]`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/organizations") {
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
	}))
	t.Cleanup(srv.Close)

	client, err := meraki.NewClient(meraki.ClientConfig{
		APIKey:  "fake",
		BaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	resp, err := Handle(context.Background(), client, &QueryRequest{
		Queries: []MerakiQuery{{RefID: "A", Kind: KindOrganizations}},
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

	rows, err := frame.RowLen()
	if err != nil {
		t.Fatalf("RowLen: %v", err)
	}
	if rows != 1 {
		t.Fatalf("got %d rows, want 1", rows)
	}

	idField, _ := frame.FieldByName("id")
	if idField == nil {
		t.Fatalf("frame missing id field; got fields=%v", frame.Fields)
	}
	if got, _ := idField.ConcreteAt(0); got != "o1" {
		t.Fatalf("row 0 id = %v, want o1", got)
	}
}

// TestHandle_SensorReadingsHistory confirms the history handler emits one
// frame per (serial, metric) pair with Prometheus-style labels on the value
// field — Grafana's timeseries viz relies on these labels to infer series
// grouping and legend names, so the shape is load-bearing for the chart to
// render at all.
func TestHandle_SensorReadingsHistory(t *testing.T) {
	// Two sensors, two metrics — expect 4 frames (one per pair).
	const payload = `[
	  {"ts":"2026-04-17T10:00:00Z","serial":"S1","metric":"temperature","network":{"id":"N1","name":"Lab"},"temperature":{"celsius":21.4,"fahrenheit":70.5}},
	  {"ts":"2026-04-17T10:05:00Z","serial":"S1","metric":"temperature","network":{"id":"N1","name":"Lab"},"temperature":{"celsius":21.5,"fahrenheit":70.7}},
	  {"ts":"2026-04-17T10:00:00Z","serial":"S1","metric":"humidity","network":{"id":"N1","name":"Lab"},"humidity":{"relativePercentage":55.0}},
	  {"ts":"2026-04-17T10:00:00Z","serial":"S2","metric":"temperature","network":{"id":"N1","name":"Lab"},"temperature":{"celsius":19.2,"fahrenheit":66.6}},
	  {"ts":"2026-04-17T10:05:00Z","serial":"S2","metric":"temperature","network":{"id":"N1","name":"Lab"},"temperature":{"celsius":19.4,"fahrenheit":66.9}}
	]`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/sensor/readings/history") {
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
			From: now.Add(-6 * time.Hour).UnixMilli(),
			To:   now.UnixMilli(),
		},
		Queries: []MerakiQuery{{RefID: "A", Kind: KindSensorReadingsHistory, OrgID: "o1"}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got := len(resp.Frames); got != 3 {
		t.Fatalf("got %d frames, want 3 (S1/temp, S1/hum, S2/temp)", got)
	}

	// Pick any frame and verify labels + field shape.
	var frame data.Frame
	if err := json.Unmarshal(resp.Frames[0], &frame); err != nil {
		t.Fatalf("decode frame 0: %v", err)
	}
	if len(frame.Fields) != 2 {
		t.Fatalf("frame[0] has %d fields, want 2 (ts, value); got %v",
			len(frame.Fields), frame.Fields)
	}
	valueField, _ := frame.FieldByName("value")
	if valueField == nil {
		t.Fatalf("frame[0] missing value field")
	}
	if valueField.Labels["serial"] == "" {
		t.Fatalf("frame[0] value labels missing serial; got %v", valueField.Labels)
	}
	if valueField.Labels["metric"] == "" {
		t.Fatalf("frame[0] value labels missing metric; got %v", valueField.Labels)
	}
	if valueField.Labels["network_id"] != "N1" {
		t.Fatalf("frame[0] value labels network_id = %q, want N1", valueField.Labels["network_id"])
	}
	// First frame is deterministic (sorted): S1 + humidity comes before S1 + temperature.
	if got := valueField.Labels["serial"]; got != "S1" {
		t.Fatalf("frame[0] serial = %q, want S1 (sort order)", got)
	}
	if got := valueField.Labels["metric"]; got != "humidity" {
		t.Fatalf("frame[0] metric = %q, want humidity (sort order)", got)
	}
}

// TestHandle_SensorReadingsHistory_ChunksLongWindows confirms that the
// handler splits queries exceeding Meraki's 7-day maximum timespan into
// sequential 7-day chunks and concatenates readings across them. The Meraki
// sensor-history endpoint rejects `t1-t0 > 7d`, so a 30-day panel range
// (e.g. the "Battery (30d)" tile) MUST hit the API multiple times or the
// result is an empty frame. Also asserts the handler does NOT emit an
// `interval` query param — the endpoint has no such field per the OpenAPI
// spec; sending one was silently ignored but not part of the contract.
func TestHandle_SensorReadingsHistory_ChunksLongWindows(t *testing.T) {
	const payload = `[]`
	type call struct {
		t0, t1   string
		interval string
	}
	calls := make([]call, 0)
	var mu sync.Mutex

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/sensor/readings/history") {
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		mu.Lock()
		calls = append(calls, call{
			t0:       r.URL.Query().Get("t0"),
			t1:       r.URL.Query().Get("t1"),
			interval: r.URL.Query().Get("interval"),
		})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
	}))
	t.Cleanup(srv.Close)

	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	now := time.Now().UTC()
	from := now.Add(-30 * 24 * time.Hour).UnixMilli()
	to := now.UnixMilli()
	if _, err := Handle(context.Background(), client, &QueryRequest{
		Range:         TimeRange{From: from, To: to},
		MaxDataPoints: 500,
		Queries:       []MerakiQuery{{RefID: "A", Kind: KindSensorReadingsHistory, OrgID: "o1"}},
	}, Options{}); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	// 30 days / 7-day chunk ⇒ 5 chunks (4 full 7d windows plus a partial).
	if len(calls) != 5 {
		t.Fatalf("expected 5 chunked requests, got %d: %+v", len(calls), calls)
	}
	for i, c := range calls {
		if c.interval != "" {
			t.Fatalf("call %d: unexpected interval param %q (endpoint has no interval field)", i, c.interval)
		}
		if c.t0 == "" || c.t1 == "" {
			t.Fatalf("call %d: missing t0/t1 (t0=%q t1=%q)", i, c.t0, c.t1)
		}
		t0, err0 := time.Parse(time.RFC3339, c.t0)
		t1, err1 := time.Parse(time.RFC3339, c.t1)
		if err0 != nil || err1 != nil {
			t.Fatalf("call %d: t0/t1 parse: %v / %v", i, err0, err1)
		}
		if span := t1.Sub(t0); span > 7*24*time.Hour+time.Second {
			t.Fatalf("call %d: chunk timespan %s exceeds 7d limit", i, span)
		}
	}
}

// TestHandle_Alerts_FollowsLinkHeaderPagination confirms that our shared
// c.GetAll() correctly follows Meraki's RFC5988 Link: rel=next header on the
// assurance alerts endpoint. The stub serves two pages (2 alerts each) with
// the first response advertising a next link; the handler should return a
// frame with rows from BOTH pages.
//
// The test is wire-level: it relies on QueryKind("alerts") being registered
// in dispatch.go's handlers map — the coordinator adds that entry after
// consolidating B1/B2/B3. Until then the test will fail with "unknown query
// kind" which is fine; it still compiles.
func TestHandle_Alerts_FollowsLinkHeaderPagination(t *testing.T) {
	const page1 = `[
	  {"id":"a1","severity":"critical","type":"unreachable","categoryType":"connectivity","title":"Device unreachable","description":"AP1 stopped responding","startedAt":"2026-04-17T09:00:00Z","network":{"id":"N1","name":"Lab"},"device":{"serial":"S1","name":"AP-1","productType":"wireless"}},
	  {"id":"a2","severity":"warning","type":"high_latency","categoryType":"performance","title":"High latency","description":"Uplink RTT>200ms","startedAt":"2026-04-17T09:05:00Z","network":{"id":"N1","name":"Lab"}}
	]`
	const page2 = `[
	  {"id":"a3","severity":"informational","type":"firmware_pending","categoryType":"configuration","title":"Firmware pending","description":"Update available","startedAt":"2026-04-17T08:55:00Z","network":{"id":"N2","name":"Office"}},
	  {"id":"a4","severity":"critical","type":"power_loss","categoryType":"connectivity","title":"Power loss","description":"Switch lost power","startedAt":"2026-04-17T08:50:00Z","network":{"id":"N2","name":"Office"},"device":{"serial":"S4","name":"SW-1","productType":"switch"}}
	]`

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/assurance/alerts") {
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		// First request has no startingAfter; return page1 + Link: next.
		if r.URL.Query().Get("startingAfter") == "" {
			// Embed an absolute URL so GetAll follows it verbatim
			// (resolveURL treats http(s):// as bypass-baseURL).
			nextURL := srv.URL + "/organizations/o1/assurance/alerts?startingAfter=p2"
			w.Header().Set("Link", "<"+nextURL+`>; rel="next"`)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(page1))
			return
		}
		// Second page — no next link.
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(page2))
	}))
	t.Cleanup(srv.Close)

	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	resp, err := Handle(context.Background(), client, &QueryRequest{
		Queries: []MerakiQuery{{RefID: "A", Kind: QueryKind("alerts"), OrgID: "o1"}},
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

	rows, err := frame.RowLen()
	if err != nil {
		t.Fatalf("RowLen: %v", err)
	}
	if rows != 4 {
		t.Fatalf("got %d rows, want 4 (2 per page × 2 pages)", rows)
	}

	// Spot-check that both pages contributed — id a1 is on page 1, a4 on page 2.
	severityField, _ := frame.FieldByName("severity")
	if severityField == nil {
		t.Fatalf("frame missing severity field; got fields=%v", frame.Fields)
	}
	// We don't assert order (Meraki's sort is ts-descending and our stub
	// keeps that order), but both a1 (critical) and a4 (critical) should
	// appear, plus warning and informational somewhere.
	seen := map[string]int{}
	for i := range rows {
		v, _ := severityField.ConcreteAt(i)
		if s, ok := v.(string); ok {
			seen[s]++
		}
	}
	if seen["critical"] != 2 {
		t.Fatalf("severity critical count = %d, want 2 (seen=%v)", seen["critical"], seen)
	}
	if seen["warning"] != 1 {
		t.Fatalf("severity warning count = %d, want 1 (seen=%v)", seen["warning"], seen)
	}
	if seen["informational"] != 1 {
		t.Fatalf("severity informational count = %d, want 1 (seen=%v)", seen["informational"], seen)
	}
}

// TestHandle_AlertsOverview_ProducesWideRow verifies the overview handler
// emits a single-row wide frame shaped (critical, warning, informational,
// total) — mirroring the sensor_alert_summary KPI frame shape.
//
// The handler calls /assurance/alerts/overview (NOT .../byType); the byType
// sibling returns an items[] array with no counts.bySeverity aggregate, so
// using it silently degrades the KPI tiles to total-only.
func TestHandle_AlertsOverview_ProducesWideRow(t *testing.T) {
	const payload = `{
	  "counts": {
	    "total": 9,
	    "bySeverity": [
	      {"type":"critical","count":3},
	      {"type":"warning","count":5},
	      {"type":"informational","count":1}
	    ]
	  }
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Must NOT match /byType — that's the wrong endpoint for this handler.
		if !strings.HasSuffix(r.URL.Path, "/assurance/alerts/overview") {
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
		Queries: []MerakiQuery{{RefID: "A", Kind: QueryKind("alertsOverview"), OrgID: "o1"}},
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
	rows, err := frame.RowLen()
	if err != nil {
		t.Fatalf("RowLen: %v", err)
	}
	if rows != 1 {
		t.Fatalf("got %d rows, want 1 (KPI wide frame)", rows)
	}

	wants := map[string]int64{
		"critical":      3,
		"warning":       5,
		"informational": 1,
		"total":         9,
	}
	for name, want := range wants {
		f, _ := frame.FieldByName(name)
		if f == nil {
			t.Fatalf("frame missing %s field; got fields=%v", name, frame.Fields)
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

// TestHandle_SwitchPorts_GroupsByStack covers the stack-aware flattening in
// handleSwitchPorts. The stub returns three entries for the org-level
// /switch/ports/statuses/bySwitch endpoint:
//   - One standalone switch (no switchStackId in the JSON).
//   - Two entries representing a 2-member stack (same switchStackId on both).
// The emitted frame must carry a `stackId` column that is empty for the
// standalone switch and equal for the stack members.
//
// Like the alerts tests above, this test uses the QueryKind string literal
// directly so the file compiles before the coordinator registers the kind in
// the handlers map; when the kind is unregistered, Handle returns an error
// frame and this test fails with an "unknown query kind" notice — expected.
func TestHandle_SwitchPorts_GroupsByStack(t *testing.T) {
	// The Meraki bySwitch endpoint wraps its result in {items: [...]} —
	// verified against api.meraki.com 2026-04-18. See switchPortsBySwitchResponse.
	const payload = `{"items":[
	  {
	    "serial": "SW-STANDALONE",
	    "name": "Closet-1",
	    "model": "MS120-8",
	    "network": {"id": "N1", "name": "Lab"},
	    "ports": [
	      {"portId": "1", "enabled": true, "status": "Connected", "speed": "1 Gbps", "duplex": "full", "clientCount": 3, "powerUsageInWh": 4.2, "vlan": 10, "allowedVlans": "10,20"}
	    ]
	  },
	  {
	    "serial": "SW-STACK-A",
	    "name": "Core-1",
	    "model": "MS250-48",
	    "network": {"id": "N1", "name": "Lab"},
	    "switchStackId": "stack-123",
	    "ports": [
	      {"portId": "1", "enabled": true, "status": "Connected", "speed": "10 Gbps", "duplex": "full", "clientCount": 0, "vlan": 1}
	    ]
	  },
	  {
	    "serial": "SW-STACK-B",
	    "name": "Core-2",
	    "model": "MS250-48",
	    "network": {"id": "N1", "name": "Lab"},
	    "switchStackId": "stack-123",
	    "ports": [
	      {"portId": "1", "enabled": true, "status": "Connected", "speed": "10 Gbps", "duplex": "full", "clientCount": 1, "vlan": 1},
	      {"portId": "2", "enabled": false, "status": "Disabled", "speed": "", "clientCount": 0}
	    ]
	  }
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
		Queries: []MerakiQuery{{RefID: "A", Kind: QueryKind("switchPorts"), OrgID: "o1"}},
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

	rows, err := frame.RowLen()
	if err != nil {
		t.Fatalf("RowLen: %v", err)
	}
	// 1 port (standalone) + 1 port (stack A) + 2 ports (stack B) = 4 rows.
	if rows != 4 {
		t.Fatalf("got %d rows, want 4 (1 standalone port + 3 stack ports)", rows)
	}

	serialField, _ := frame.FieldByName("serial")
	stackField, _ := frame.FieldByName("stackId")
	portField, _ := frame.FieldByName("portId")
	speedField, _ := frame.FieldByName("speedMbps")
	if serialField == nil || stackField == nil || portField == nil || speedField == nil {
		t.Fatalf("frame missing required column(s); got fields=%v", frame.Fields)
	}

	// Build a (serial,portId) -> stackId lookup so we can assert without
	// depending on the flatten order.
	stackBySerialPort := map[string]string{}
	speedBySerialPort := map[string]int64{}
	for i := range rows {
		s, _ := serialField.ConcreteAt(i)
		p, _ := portField.ConcreteAt(i)
		st, _ := stackField.ConcreteAt(i)
		sp, _ := speedField.ConcreteAt(i)
		key := s.(string) + "|" + p.(string)
		stackBySerialPort[key] = st.(string)
		speedBySerialPort[key] = sp.(int64)
	}

	// Standalone switch: empty stackId.
	if got := stackBySerialPort["SW-STANDALONE|1"]; got != "" {
		t.Fatalf("standalone port stackId = %q, want empty", got)
	}
	// Both stack members: same non-empty stackId.
	if got := stackBySerialPort["SW-STACK-A|1"]; got != "stack-123" {
		t.Fatalf("stack member A stackId = %q, want stack-123", got)
	}
	if got := stackBySerialPort["SW-STACK-B|1"]; got != "stack-123" {
		t.Fatalf("stack member B stackId = %q, want stack-123", got)
	}

	// Speed parse sanity: "1 Gbps" -> 1000, "10 Gbps" -> 10000, "" -> 0.
	if got := speedBySerialPort["SW-STANDALONE|1"]; got != 1000 {
		t.Fatalf("standalone port speedMbps = %d, want 1000", got)
	}
	if got := speedBySerialPort["SW-STACK-A|1"]; got != 10000 {
		t.Fatalf("stack-A port speedMbps = %d, want 10000", got)
	}
	if got := speedBySerialPort["SW-STACK-B|2"]; got != 0 {
		t.Fatalf("disabled port speedMbps = %d, want 0 (empty speed string)", got)
	}
}

// TestHandle_SwitchPortPacketCounters_SingleRow covers the packet-counters
// handler. Meraki's endpoint returns an array of {portId, packets:[...]}
// entries (one per port on the switch); we filter to the requested port and
// flatten its `packets` array into one row per counter bucket.
//
// The handler overloads q.Metrics[0] as the port id since MerakiQuery has no
// dedicated portId field; the frontend must set it when emitting a
// switchPortPacketCounters query.
func TestHandle_SwitchPortPacketCounters_SingleRow(t *testing.T) {
	const payload = `[
	  {
	    "portId": "1",
	    "packets": [
	      {"desc":"Total","total":100,"sent":60,"recv":40,"ratePerSec":{"total":1.0,"sent":0.6,"recv":0.4}},
	      {"desc":"Broadcast","total":20,"sent":15,"recv":5,"ratePerSec":{"total":0.2,"sent":0.15,"recv":0.05}},
	      {"desc":"CRC align errors","total":0,"sent":0,"recv":0}
	    ]
	  },
	  {
	    "portId": "2",
	    "packets": [
	      {"desc":"Total","total":999,"sent":999,"recv":0}
	    ]
	  }
	]`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/switch/ports/statuses/packets") {
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
		Queries: []MerakiQuery{{
			RefID:   "A",
			Kind:    QueryKind("switchPortPacketCounters"),
			Serials: []string{"SW-1"},
			Metrics: []string{"1"}, // port id overloaded onto Metrics[0]
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
		t.Fatalf("decode frame: %v (body=%s)", err, string(resp.Frames[0]))
	}
	rows, err := frame.RowLen()
	if err != nil {
		t.Fatalf("RowLen: %v", err)
	}
	// We asked for port "1" which has 3 bucket rows; port "2" must be filtered out.
	if rows != 3 {
		t.Fatalf("got %d rows, want 3 (only port 1's 3 counter buckets)", rows)
	}

	// Verify the columns we expect exist.
	for _, col := range []string{"desc", "total", "sent", "recv", "ratePerSecTotal", "ratePerSecSent", "ratePerSecRecv"} {
		if f, _ := frame.FieldByName(col); f == nil {
			t.Fatalf("frame missing %q column; got fields=%v", col, frame.Fields)
		}
	}

	// Spot-check the Total bucket (first row in the stub).
	descField, _ := frame.FieldByName("desc")
	totalField, _ := frame.FieldByName("total")
	rateField, _ := frame.FieldByName("ratePerSecTotal")
	foundTotal := false
	for i := range rows {
		desc, _ := descField.ConcreteAt(i)
		if desc == "Total" {
			foundTotal = true
			if tot, _ := totalField.ConcreteAt(i); tot != int64(100) {
				t.Fatalf("Total bucket total = %v, want 100", tot)
			}
			if r, _ := rateField.ConcreteAt(i); r != float64(1.0) {
				t.Fatalf("Total bucket ratePerSecTotal = %v, want 1.0", r)
			}
		}
	}
	if !foundTotal {
		t.Fatalf("no Total bucket in emitted frame; desc values were missing the expected entry")
	}
}


// TestHandle_WirelessChannelUtil_EmitsPerBandFrames verifies the channel-util handler
// flattens the `byBand` array server-side and emits one frame per (serial, band) pair,
// each with Prometheus-style labels on the value field. Four frames are expected:
// two serials × two bands.
func TestHandle_WirelessChannelUtil_EmitsPerBandFrames(t *testing.T) {
	// Two serials × two bands × 3 intervals. The interval shape mirrors the real
	// byDevice/byInterval response (nested byBand inside each entry).
	const payload = `[
	  {"startTs":"2026-04-17T10:00:00Z","endTs":"2026-04-17T10:10:00Z","serial":"Q2XX-AAAA-AAAA","mac":"aa:aa:aa:aa:aa:aa","network":{"id":"N1"},"byBand":[
	    {"band":"2.4","wifi":{"percentage":10.0},"nonWifi":{"percentage":2.0},"total":{"percentage":12.0}},
	    {"band":"5","wifi":{"percentage":20.0},"nonWifi":{"percentage":3.0},"total":{"percentage":23.0}}
	  ]},
	  {"startTs":"2026-04-17T10:10:00Z","endTs":"2026-04-17T10:20:00Z","serial":"Q2XX-AAAA-AAAA","mac":"aa:aa:aa:aa:aa:aa","network":{"id":"N1"},"byBand":[
	    {"band":"2.4","wifi":{"percentage":11.0},"nonWifi":{"percentage":2.0},"total":{"percentage":13.0}},
	    {"band":"5","wifi":{"percentage":21.0},"nonWifi":{"percentage":3.0},"total":{"percentage":24.0}}
	  ]},
	  {"startTs":"2026-04-17T10:20:00Z","endTs":"2026-04-17T10:30:00Z","serial":"Q2XX-AAAA-AAAA","mac":"aa:aa:aa:aa:aa:aa","network":{"id":"N1"},"byBand":[
	    {"band":"2.4","wifi":{"percentage":12.0},"nonWifi":{"percentage":2.0},"total":{"percentage":14.0}},
	    {"band":"5","wifi":{"percentage":22.0},"nonWifi":{"percentage":3.0},"total":{"percentage":25.0}}
	  ]},
	  {"startTs":"2026-04-17T10:00:00Z","endTs":"2026-04-17T10:10:00Z","serial":"Q2XX-BBBB-BBBB","mac":"bb:bb:bb:bb:bb:bb","network":{"id":"N1"},"byBand":[
	    {"band":"2.4","wifi":{"percentage":15.0},"nonWifi":{"percentage":2.0},"total":{"percentage":17.0}},
	    {"band":"5","wifi":{"percentage":25.0},"nonWifi":{"percentage":3.0},"total":{"percentage":28.0}}
	  ]},
	  {"startTs":"2026-04-17T10:10:00Z","endTs":"2026-04-17T10:20:00Z","serial":"Q2XX-BBBB-BBBB","mac":"bb:bb:bb:bb:bb:bb","network":{"id":"N1"},"byBand":[
	    {"band":"2.4","wifi":{"percentage":16.0},"nonWifi":{"percentage":2.0},"total":{"percentage":18.0}},
	    {"band":"5","wifi":{"percentage":26.0},"nonWifi":{"percentage":3.0},"total":{"percentage":29.0}}
	  ]},
	  {"startTs":"2026-04-17T10:20:00Z","endTs":"2026-04-17T10:30:00Z","serial":"Q2XX-BBBB-BBBB","mac":"bb:bb:bb:bb:bb:bb","network":{"id":"N1"},"byBand":[
	    {"band":"2.4","wifi":{"percentage":17.0},"nonWifi":{"percentage":2.0},"total":{"percentage":19.0}},
	    {"band":"5","wifi":{"percentage":27.0},"nonWifi":{"percentage":3.0},"total":{"percentage":30.0}}
	  ]}
	]`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/wireless/devices/channelUtilization/history/byDevice/byInterval") {
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
			From: now.Add(-6 * time.Hour).UnixMilli(),
			To:   now.UnixMilli(),
		},
		Queries: []MerakiQuery{{RefID: "A", Kind: KindWirelessChannelUtil, OrgID: "o1"}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	// 2 serials × 2 bands = 4 frames.
	if got := len(resp.Frames); got != 4 {
		t.Fatalf("got %d frames, want 4 (2 serials × 2 bands)", got)
	}

	// Confirm the first frame carries a well-formed (serial, band) label set. The handler
	// sorts by serial then band, so frame[0] is Q2XX-AAAA-AAAA @ 2.4 GHz.
	var frame data.Frame
	if err := json.Unmarshal(resp.Frames[0], &frame); err != nil {
		t.Fatalf("decode frame 0: %v", err)
	}
	valueField, _ := frame.FieldByName("value")
	if valueField == nil {
		t.Fatalf("frame[0] missing value field; fields=%v", frame.Fields)
	}
	if got := valueField.Labels["serial"]; got != "Q2XX-AAAA-AAAA" {
		t.Fatalf("frame[0] serial = %q, want Q2XX-AAAA-AAAA", got)
	}
	if got := valueField.Labels["band"]; got != "2.4" {
		t.Fatalf("frame[0] band = %q, want 2.4", got)
	}

	// Collect the distinct band label values across all frames — should be {"2.4", "5"}.
	seenBands := map[string]struct{}{}
	for i, raw := range resp.Frames {
		var f data.Frame
		if err := json.Unmarshal(raw, &f); err != nil {
			t.Fatalf("decode frame %d: %v", i, err)
		}
		vf, _ := f.FieldByName("value")
		if vf == nil {
			continue
		}
		seenBands[vf.Labels["band"]] = struct{}{}
	}
	if _, ok := seenBands["2.4"]; !ok {
		t.Fatalf("expected to see band=2.4 label across frames; got %v", seenBands)
	}
	if _, ok := seenBands["5"]; !ok {
		t.Fatalf("expected to see band=5 label across frames; got %v", seenBands)
	}
}

// TestHandle_NetworkSsids_Table verifies the SSID handler emits a single table frame with
// the expected column set and preserves row values across networks.
func TestHandle_NetworkSsids_Table(t *testing.T) {
	// Two SSIDs on one network. The Meraki endpoint always returns 15 SSIDs; we
	// only stub two for concision.
	const payload = `[
	  {"number":0,"name":"Corp","enabled":true,"splashPage":"None","authMode":"8021x-radius"},
	  {"number":1,"name":"Guest","enabled":false,"splashPage":"Click-through splash page","authMode":"open"}
	]`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/wireless/ssids") {
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
		Queries: []MerakiQuery{{RefID: "A", Kind: KindNetworkSsids, NetworkIDs: []string{"N_corp"}}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got := len(resp.Frames); got != 1 {
		t.Fatalf("got %d frames, want 1", got)
	}

	var frame data.Frame
	if err := json.Unmarshal(resp.Frames[0], &frame); err != nil {
		t.Fatalf("decode frame 0: %v", err)
	}

	// Required columns per the plan.
	for _, name := range []string{"number", "name", "enabled", "splashPage", "authMode", "networkId"} {
		if f, _ := frame.FieldByName(name); f == nil {
			t.Fatalf("frame missing %q column; got fields=%v", name, frame.Fields)
		}
	}
	rows, _ := frame.RowLen()
	if rows != 2 {
		t.Fatalf("got %d rows, want 2", rows)
	}

	// Spot-check one row to confirm wiring.
	nameField, _ := frame.FieldByName("name")
	if got, _ := nameField.ConcreteAt(0); got != "Corp" {
		t.Fatalf("row 0 name = %v, want Corp", got)
	}
	enabledField, _ := frame.FieldByName("enabled")
	if got, _ := enabledField.ConcreteAt(1); got != false {
		t.Fatalf("row 1 enabled = %v, want false", got)
	}
}

// TestHandle_DeviceAvailabilities_Status verifies the availabilities handler surfaces the
// device status column with distinct values, one row per device.
func TestHandle_DeviceAvailabilities_Status(t *testing.T) {
	// Three devices with distinct statuses — confirms we don't collapse or filter.
	const payload = `[
	  {"serial":"Q2XX-1111-1111","name":"AP-1","mac":"11:11:11:11:11:11","productType":"wireless","status":"online","tags":["edge"],"network":{"id":"N1","name":"HQ"}},
	  {"serial":"Q2XX-2222-2222","name":"AP-2","mac":"22:22:22:22:22:22","productType":"wireless","status":"alerting","tags":[],"network":{"id":"N1","name":"HQ"}},
	  {"serial":"Q2XX-3333-3333","name":"AP-3","mac":"33:33:33:33:33:33","productType":"wireless","status":"offline","tags":[],"network":{"id":"N2","name":"Branch"}}
	]`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/devices/availabilities") {
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
		Queries: []MerakiQuery{{RefID: "A", Kind: KindDeviceAvailabilities, OrgID: "o1"}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got := len(resp.Frames); got != 1 {
		t.Fatalf("got %d frames, want 1", got)
	}

	var frame data.Frame
	if err := json.Unmarshal(resp.Frames[0], &frame); err != nil {
		t.Fatalf("decode frame 0: %v", err)
	}

	statusField, _ := frame.FieldByName("status")
	if statusField == nil {
		t.Fatalf("frame missing status column; fields=%v", frame.Fields)
	}
	rows, _ := frame.RowLen()
	if rows != 3 {
		t.Fatalf("got %d rows, want 3", rows)
	}

	gotStatuses := make([]string, 0, 3)
	for i := range rows {
		v, _ := statusField.ConcreteAt(i)
		gotStatuses = append(gotStatuses, v.(string))
	}
	// The server returns rows in insertion order; our handler preserves it.
	want := []string{"online", "alerting", "offline"}
	for i, w := range want {
		if gotStatuses[i] != w {
			t.Fatalf("row %d status = %q, want %q (full=%v)", i, gotStatuses[i], w, gotStatuses)
		}
	}

	// Sanity-check a couple of other surface columns to verify the full shape.
	serialField, _ := frame.FieldByName("serial")
	if serialField == nil {
		t.Fatalf("frame missing serial column")
	}
	if got, _ := serialField.ConcreteAt(0); got != "Q2XX-1111-1111" {
		t.Fatalf("row 0 serial = %v, want Q2XX-1111-1111", got)
	}
	netIDField, _ := frame.FieldByName("network_id")
	if got, _ := netIDField.ConcreteAt(2); got != "N2" {
		t.Fatalf("row 2 network_id = %v, want N2", got)
	}
}
