package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// TestHandle_MgUplinks_ParsesSignalDb verifies that the MG uplink handler
// decodes Meraki's string-shaped signal strength fields (`"-87 dBm"`) into
// numeric `rsrpDb`/`rsrqDb` columns. The shared ParseSignalDb helper does the
// conversion; this test exercises the end-to-end wiring.
func TestHandle_MgUplinks_ParsesSignalDb(t *testing.T) {
	const payload = `[
	  {
	    "serial": "Q2MG-AAAA-AAAA",
	    "model": "MG41",
	    "networkId": "N1",
	    "lastReportedAt": "2026-04-17T10:00:00Z",
	    "uplinks": [
	      {
	        "interface": "cellular",
	        "status": "active",
	        "iccid": "8944500",
	        "apn": "internet",
	        "provider": "Verizon",
	        "publicIp": "1.2.3.4",
	        "signalType": "4G",
	        "connectionType": "4g",
	        "dns1": "8.8.8.8",
	        "dns2": "1.1.1.1",
	        "signalStat": {"rsrp": "-87 dBm", "rsrq": "-12 dBm"}
	      }
	    ]
	  }
	]`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/cellularGateway/uplink/statuses") {
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
		Queries: []MerakiQuery{{RefID: "A", Kind: KindMgUplinks, OrgID: "o1"}},
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
	rows, _ := frame.RowLen()
	if rows != 1 {
		t.Fatalf("got %d rows, want 1", rows)
	}

	rsrp, _ := frame.FieldByName("rsrpDb")
	rsrq, _ := frame.FieldByName("rsrqDb")
	if rsrp == nil || rsrq == nil {
		t.Fatalf("frame missing rsrpDb/rsrqDb; fields=%v", frame.Fields)
	}
	if got, _ := rsrp.ConcreteAt(0); got != float64(-87) {
		t.Fatalf("rsrpDb = %v, want -87", got)
	}
	if got, _ := rsrq.ConcreteAt(0); got != float64(-12) {
		t.Fatalf("rsrqDb = %v, want -12", got)
	}

	// Sanity: drilldownUrl is the cellular-gateways family route.
	ddField, _ := frame.FieldByName("drilldownUrl")
	if got, _ := ddField.ConcreteAt(0); got != "/a/rknightion-meraki-app/cellular-gateways/Q2MG-AAAA-AAAA" {
		t.Fatalf("drilldownUrl = %v, want /a/rknightion-meraki-app/cellular-gateways/Q2MG-AAAA-AAAA", got)
	}
}

// TestHandle_MgPortForwarding_DecodesRulesWrapper verifies that the
// port-forwarding wrapper strips the `{"rules":[...]}` envelope correctly
// and the handler emits one row per rule per serial.
func TestHandle_MgPortForwarding_DecodesRulesWrapper(t *testing.T) {
	const payload = `{
	  "rules": [
	    {"name":"http","protocol":"tcp","publicPort":"8080","localPort":"80","lanIp":"192.168.1.10","allowedIps":["10.0.0.0/24","8.8.8.8/32"],"access":"restricted"},
	    {"name":"dns","protocol":"udp","publicPort":"5353","localPort":"53","lanIp":"192.168.1.1","allowedIps":["any"],"access":"any"}
	  ]
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/cellularGateway/portForwardingRules") {
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
		Queries: []MerakiQuery{{RefID: "A", Kind: KindMgPortForwarding, Serials: []string{"Q2MG-AAAA-AAAA"}}},
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
	rows, _ := frame.RowLen()
	if rows != 2 {
		t.Fatalf("got %d rows, want 2 (2 rules)", rows)
	}
	for _, col := range []string{"serial", "name", "protocol", "publicPort", "localPort", "lanIp", "allowedIps", "access"} {
		if f, _ := frame.FieldByName(col); f == nil {
			t.Fatalf("frame missing %q column; fields=%v", col, frame.Fields)
		}
	}

	// Spot-check: allowedIps is comma-joined on emit.
	allowedField, _ := frame.FieldByName("allowedIps")
	if got, _ := allowedField.ConcreteAt(0); got != "10.0.0.0/24,8.8.8.8/32" {
		t.Fatalf("row 0 allowedIps = %v, want 10.0.0.0/24,8.8.8.8/32", got)
	}

	// Spot-check: the second rule is the dns entry, serial carries through.
	nameField, _ := frame.FieldByName("name")
	if got, _ := nameField.ConcreteAt(1); got != "dns" {
		t.Fatalf("row 1 name = %v, want dns", got)
	}
	serialField, _ := frame.FieldByName("serial")
	if got, _ := serialField.ConcreteAt(1); got != "Q2MG-AAAA-AAAA" {
		t.Fatalf("row 1 serial = %v, want Q2MG-AAAA-AAAA", got)
	}
}
