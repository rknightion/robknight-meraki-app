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

// TestHandle_SwitchPortsOverview verifies the aggregator emits a single wide
// frame with per-status port counts plus PoE watts summed across every switch.
// Fixture: 2 switches, 5 ports total. Switch A: 2 connected + 1 disabled.
// Switch B: 1 connected + 1 disconnected. PoE draw is summed across every
// port regardless of status so we can expose a single fleet-wide tile.
func TestHandle_SwitchPortsOverview(t *testing.T) {
	// Wire shape matches the Meraki bySwitch endpoint: {items: [...]} wrapper.
	const payload = `{"items":[
	  {"serial":"A","ports":[
	    {"portId":"1","enabled":true,"status":"Connected","clientCount":2,"powerUsageInWatts":5.5},
	    {"portId":"2","enabled":true,"status":"Connected","clientCount":1,"powerUsageInWatts":2.25},
	    {"portId":"3","enabled":false,"status":"Disconnected","clientCount":0,"powerUsageInWatts":0}
	  ]},
	  {"serial":"B","ports":[
	    {"portId":"1","enabled":true,"status":"Connected","clientCount":4,"powerUsageInWatts":7.0},
	    {"portId":"2","enabled":true,"status":"Disconnected","clientCount":0,"powerUsageInWatts":0}
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
		Queries: []MerakiQuery{{RefID: "A", Kind: KindSwitchPortsOverview, OrgID: "o1"}},
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

	wantInt := map[string]int64{
		"portCount":         5,
		"portsConnected":    3,
		"portsDisconnected": 2,
		"portsDisabled":     1,
		"clientCount":       7,
	}
	for name, want := range wantInt {
		f, _ := frame.FieldByName(name)
		if f == nil {
			t.Fatalf("frame missing %s field; fields=%v", name, frame.Fields)
		}
		got, _ := f.ConcreteAt(0)
		if got != want {
			t.Errorf("%s = %v, want %d", name, got, want)
		}
	}

	f, _ := frame.FieldByName("poeTotalWatts")
	if f == nil {
		t.Fatalf("missing poeTotalWatts field")
	}
	got, _ := f.ConcreteAt(0)
	if got != 14.75 {
		t.Errorf("poeTotalWatts = %v, want 14.75", got)
	}
}
