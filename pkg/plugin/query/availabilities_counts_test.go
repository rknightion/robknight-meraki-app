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

// TestHandle_DeviceAvailabilityCounts verifies the handler emits a single
// wide frame with per-status counts plus a total, so stat panels can bind
// via an organize+reduce chain instead of a fragile filterByValue chain.
func TestHandle_DeviceAvailabilityCounts(t *testing.T) {
	const payload = `[
	  {"serial":"A","status":"online","productType":"switch","network":{"id":"N","name":"Lab"}},
	  {"serial":"B","status":"online","productType":"switch","network":{"id":"N","name":"Lab"}},
	  {"serial":"C","status":"alerting","productType":"wireless","network":{"id":"N","name":"Lab"}},
	  {"serial":"D","status":"offline","productType":"appliance","network":{"id":"N","name":"Lab"}},
	  {"serial":"E","status":"dormant","productType":"switch","network":{"id":"N","name":"Lab"}}
	]`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/devices/availabilities") {
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
		Queries: []MerakiQuery{{RefID: "A", Kind: KindDeviceAvailabilityCounts, OrgID: "o1"}},
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

	want := map[string]int64{"online": 2, "alerting": 1, "offline": 1, "dormant": 1, "total": 5}
	for name, v := range want {
		f, _ := frame.FieldByName(name)
		if f == nil {
			t.Fatalf("frame missing %s field; fields=%v", name, frame.Fields)
		}
		got, _ := f.ConcreteAt(0)
		if got != v {
			t.Errorf("%s = %v, want %d", name, got, v)
		}
	}
}

func TestHandle_OrganizationsCount(t *testing.T) {
	const payload = `[{"id":"o1","name":"A"},{"id":"o2","name":"B"},{"id":"o3","name":"C"}]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/organizations") {
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
	}))
	t.Cleanup(srv.Close)

	client, _ := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	resp, err := Handle(context.Background(), client, &QueryRequest{
		Queries: []MerakiQuery{{RefID: "A", Kind: KindOrganizationsCount}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	var frame data.Frame
	if err := json.Unmarshal(resp.Frames[0], &frame); err != nil {
		t.Fatalf("decode: %v", err)
	}
	f, _ := frame.FieldByName("count")
	if f == nil {
		t.Fatalf("missing count field")
	}
	if got, _ := f.ConcreteAt(0); got != int64(3) {
		t.Fatalf("count = %v, want 3", got)
	}
}

func TestHandle_NetworksCount(t *testing.T) {
	const payload = `[
	  {"id":"N1","organizationId":"o1","name":"A"},
	  {"id":"N2","organizationId":"o1","name":"B"}
	]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/organizations/o1/networks") {
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
	}))
	t.Cleanup(srv.Close)

	client, _ := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	resp, err := Handle(context.Background(), client, &QueryRequest{
		Queries: []MerakiQuery{{RefID: "A", Kind: KindNetworksCount, OrgID: "o1"}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	var frame data.Frame
	if err := json.Unmarshal(resp.Frames[0], &frame); err != nil {
		t.Fatalf("decode: %v", err)
	}
	f, _ := frame.FieldByName("count")
	if got, _ := f.ConcreteAt(0); got != int64(2) {
		t.Fatalf("count = %v, want 2", got)
	}
}
