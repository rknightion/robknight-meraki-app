package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// stubServer for topology tests serves three endpoints:
//
//   - /organizations/{org}/networks  → list of networks
//   - /organizations/{org}/devices   → list of devices (with lat/lng)
//   - /devices/{serial}/lldpCdp      → per-device neighbor map
//
// We track LLDP fan-out via an atomic counter so tests can assert the
// per-device fan-out actually happened (and didn't blow past the cap).
type topologyStub struct {
	networksJSON string
	devicesJSON  string
	lldpJSON     map[string]string // serial → JSON body (or empty for 404)
	lldpCalls    int64
}

func (s *topologyStub) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/networks"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(s.networksJSON))
		case strings.HasSuffix(r.URL.Path, "/devices"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(s.devicesJSON))
		case strings.Contains(r.URL.Path, "/lldpCdp"):
			atomic.AddInt64(&s.lldpCalls, 1)
			// Extract serial: path is /api/v1/devices/{serial}/lldpCdp
			parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
			var serial string
			for i, p := range parts {
				if p == "devices" && i+1 < len(parts) {
					serial = parts[i+1]
					break
				}
			}
			body, ok := s.lldpJSON[serial]
			if !ok {
				http.Error(w, `{"errors":["not found"]}`, http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		default:
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
		}
	})
}

// TestHandle_NetworkGeo asserts the geo-aggregation handler returns one
// row per network with at least one geo-tagged device, drops un-tagged
// networks, and attaches a data.Notice counting the dropped networks.
func TestHandle_NetworkGeo(t *testing.T) {
	stub := &topologyStub{
		networksJSON: `[
		  {"id":"N_lab","organizationId":"o1","name":"Lab","productTypes":["wireless","switch"]},
		  {"id":"N_office","organizationId":"o1","name":"Office","productTypes":["appliance"]},
		  {"id":"N_empty","organizationId":"o1","name":"Empty","productTypes":["wireless"]}
		]`,
		devicesJSON: `[
		  {"serial":"Q-LAB-1","networkId":"N_lab","lat":51.50,"lng":-0.10,"name":"AP-1","model":"MR46","productType":"wireless"},
		  {"serial":"Q-LAB-2","networkId":"N_lab","lat":51.52,"lng":-0.12,"name":"SW-1","model":"MS220-8P","productType":"switch"},
		  {"serial":"Q-OFFICE-1","networkId":"N_office","lat":40.71,"lng":-74.00,"name":"MX-1","model":"MX67","productType":"appliance"},
		  {"serial":"Q-EMPTY-1","networkId":"N_empty","lat":0,"lng":0,"name":"AP-untagged","model":"MR46","productType":"wireless"}
		]`,
	}
	srv := httptest.NewServer(stub.handler())
	t.Cleanup(srv.Close)

	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	resp, err := Handle(context.Background(), client, &QueryRequest{
		Queries: []MerakiQuery{{RefID: "A", Kind: KindNetworkGeo, OrgID: "o1"}},
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

	// Field shape — networkId, name, lat, lng.
	wantFields := []string{"networkId", "name", "lat", "lng"}
	if got := len(frame.Fields); got != len(wantFields) {
		t.Fatalf("got %d fields, want %d (%v)", got, len(wantFields), wantFields)
	}
	for i, f := range frame.Fields {
		if f.Name != wantFields[i] {
			t.Errorf("field[%d].Name = %q, want %q", i, f.Name, wantFields[i])
		}
	}

	rows, _ := frame.RowLen()
	if rows != 2 {
		t.Fatalf("got %d rows, want 2 (Lab + Office; Empty has no geo-tagged devices)", rows)
	}

	// Sorted by id: N_lab first, then N_office.
	if got, _ := frame.Fields[0].ConcreteAt(0); got != "N_lab" {
		t.Errorf("row 0 networkId = %v, want N_lab", got)
	}
	// Lab centroid = mean of (51.50,-0.10), (51.52,-0.12) → (51.51, -0.11).
	if got, _ := frame.Fields[2].ConcreteAt(0); got.(float64) < 51.50 || got.(float64) > 51.52 {
		t.Errorf("row 0 lat = %v, want ≈51.51", got)
	}

	// Notice attached for the dropped network.
	if frame.Meta == nil || len(frame.Meta.Notices) == 0 {
		t.Fatalf("expected Meta.Notices for the dropped network; got Meta=%+v", frame.Meta)
	}
	if !strings.Contains(frame.Meta.Notices[0].Text, "1 network") {
		t.Errorf("notice text = %q, want it to mention 1 dropped network", frame.Meta.Notices[0].Text)
	}
}

// TestHandle_DeviceLldpCdp_NetworkScope asserts the per-network fan-out
// emits the two-frame Node Graph contract with the expected nodes/edges.
func TestHandle_DeviceLldpCdp_NetworkScope(t *testing.T) {
	stub := &topologyStub{
		networksJSON: `[{"id":"N_lab","organizationId":"o1","name":"Lab","productTypes":["wireless","switch"]}]`,
		devicesJSON: `[
		  {"serial":"Q-SW-1","networkId":"N_lab","name":"core-sw","model":"MS425-32","productType":"switch"},
		  {"serial":"Q-AP-1","networkId":"N_lab","name":"ap-floor1","model":"MR46","productType":"wireless"}
		]`,
		lldpJSON: map[string]string{
			"Q-SW-1": `{
			  "sourceMac":"00:18:0a:00:00:01",
			  "ports": {
			    "1":  {"lldp":{"systemName":"Q-AP-1","portId":"wired0","managementAddress":"10.0.0.5"}},
			    "24": {"cdp": {"deviceId":"upstream-router","portId":"GigabitEthernet0/1","address":"10.0.0.1","platform":"Cisco ASR1001-X"}}
			  }
			}`,
			"Q-AP-1": `{
			  "sourceMac":"00:18:0a:00:00:02",
			  "ports": {
			    "wired0": {"cdp":{"deviceId":"Q-SW-1","portId":"1","address":"10.0.0.10","platform":"Meraki MS"}}
			  }
			}`,
		},
	}
	srv := httptest.NewServer(stub.handler())
	t.Cleanup(srv.Close)

	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	resp, err := Handle(context.Background(), client, &QueryRequest{
		Queries: []MerakiQuery{{
			RefID:      "A",
			Kind:       KindDeviceLldpCdp,
			OrgID:      "o1",
			NetworkIDs: []string{"N_lab"},
		}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got := len(resp.Frames); got != 2 {
		t.Fatalf("got %d frames, want 2 (nodes + edges)", got)
	}

	var nodes data.Frame
	if err := json.Unmarshal(resp.Frames[0], &nodes); err != nil {
		t.Fatalf("decode nodes: %v", err)
	}
	wantNodeFields := []string{"id", "title", "subtitle", "mainstat"}
	for i, f := range nodes.Fields {
		if i >= len(wantNodeFields) {
			t.Fatalf("nodes has more fields than expected: %v", nodes.Fields)
		}
		if f.Name != wantNodeFields[i] {
			t.Errorf("nodes.Fields[%d].Name = %q, want %q", i, f.Name, wantNodeFields[i])
		}
	}
	nrows, _ := nodes.RowLen()
	// 2 in-org devices + 1 external (upstream-router) = 3 nodes.
	if nrows != 3 {
		t.Errorf("nodes rows = %d, want 3 (Q-SW-1, Q-AP-1, upstream-router)", nrows)
	}
	if nodes.Meta == nil || nodes.Meta.PreferredVisualization != data.VisTypeNodeGraph {
		t.Errorf("nodes.Meta.PreferredVisualization = %v, want %q", nodes.Meta, data.VisTypeNodeGraph)
	}

	var edges data.Frame
	if err := json.Unmarshal(resp.Frames[1], &edges); err != nil {
		t.Fatalf("decode edges: %v", err)
	}
	wantEdgeFields := []string{"id", "source", "target"}
	for i, f := range edges.Fields {
		if i >= len(wantEdgeFields) {
			t.Fatalf("edges has more fields than expected: %v", edges.Fields)
		}
		if f.Name != wantEdgeFields[i] {
			t.Errorf("edges.Fields[%d].Name = %q, want %q", i, f.Name, wantEdgeFields[i])
		}
	}
	erows, _ := edges.RowLen()
	// CDP+LLDP between Q-SW-1 and Q-AP-1 should collapse to one edge;
	// CDP from Q-SW-1 to upstream-router is the second edge. Total: 2.
	if erows != 2 {
		t.Errorf("edges rows = %d, want 2 (Q-SW-1↔Q-AP-1 deduped, Q-SW-1→upstream-router)", erows)
	}
	if edges.Meta == nil || edges.Meta.PreferredVisualization != data.VisTypeNodeGraph {
		t.Errorf("edges.Meta.PreferredVisualization = %v, want %q", edges.Meta, data.VisTypeNodeGraph)
	}

	// Fan-out actually happened — once per device.
	if got := atomic.LoadInt64(&stub.lldpCalls); got != 2 {
		t.Errorf("lldpCdp fan-out hit %d times, want 2 (one per device)", got)
	}
}

// TestHandle_DeviceLldpCdp_RequiresFilter asserts the org-wide fan-out
// guard fires: a query with no networkIds and no serials must error
// rather than silently fan-out across the whole org.
func TestHandle_DeviceLldpCdp_RequiresFilter(t *testing.T) {
	stub := &topologyStub{
		networksJSON: `[]`,
		devicesJSON:  `[]`,
	}
	srv := httptest.NewServer(stub.handler())
	t.Cleanup(srv.Close)

	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	resp, err := Handle(context.Background(), client, &QueryRequest{
		Queries: []MerakiQuery{{RefID: "A", Kind: KindDeviceLldpCdp, OrgID: "o1"}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle envelope: %v", err)
	}
	if got := len(resp.Frames); got == 0 {
		t.Fatalf("got 0 frames, want 1 with an error notice")
	}

	// The error gets surfaced as a notice on the first frame (per the
	// frame-notice contract in dispatch.go).
	var frame data.Frame
	if err := json.Unmarshal(resp.Frames[0], &frame); err != nil {
		t.Fatalf("decode frame: %v", err)
	}
	if frame.Meta == nil || len(frame.Meta.Notices) == 0 {
		t.Fatalf("expected error notice; got Meta=%+v", frame.Meta)
	}
	if !strings.Contains(frame.Meta.Notices[0].Text, "networkId or serial is required") {
		t.Errorf("notice text = %q, want guard message", frame.Meta.Notices[0].Text)
	}
}
