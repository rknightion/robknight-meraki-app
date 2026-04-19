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

// TestHandle_DeviceOfflineCount verifies the handler emits a single-row
// frame with one int64 `count` field equal to the offline-status count.
// One field is load-bearing — Grafana's reduce SSE produces one labelled
// output per numeric field, and the device-offline alert template feeds
// the result into a `gt 0` threshold; if we returned multiple fields the
// alert would fire whenever any non-offline bucket was non-zero.
func TestHandle_DeviceOfflineCount(t *testing.T) {
	const payload = `[
	  {"serial":"A","status":"online","productType":"switch","network":{"id":"N","name":"Lab"}},
	  {"serial":"B","status":"offline","productType":"appliance","network":{"id":"N","name":"Lab"}},
	  {"serial":"C","status":"offline","productType":"wireless","network":{"id":"N","name":"Lab"}},
	  {"serial":"D","status":"alerting","productType":"switch","network":{"id":"N","name":"Lab"}},
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
		Queries: []MerakiQuery{{RefID: "A", Kind: KindDeviceOfflineCount, OrgID: "o1"}},
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
	if got := len(frame.Fields); got != 1 {
		t.Fatalf("got %d fields, want exactly 1 (load-bearing for SSE reduce)", got)
	}
	f, _ := frame.FieldByName("count")
	if f == nil {
		t.Fatalf("missing count field; fields=%v", frame.Fields)
	}
	if got, _ := f.ConcreteAt(0); got != int64(2) {
		t.Errorf("count = %v, want 2", got)
	}
}

// TestHandle_DeviceOfflineCount_MissingOrgID confirms the handler reports a
// validation error when called without an org ID, matching the error-frame
// pattern of every sibling handler.
func TestHandle_DeviceOfflineCount_MissingOrgID(t *testing.T) {
	client, _ := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: "http://unused.invalid"})
	_, err := handleDeviceOfflineCount(context.Background(), client, MerakiQuery{Kind: KindDeviceOfflineCount}, TimeRange{}, Options{})
	if err == nil || !strings.Contains(err.Error(), "orgId is required") {
		t.Fatalf("expected orgId required error, got %v", err)
	}
}
