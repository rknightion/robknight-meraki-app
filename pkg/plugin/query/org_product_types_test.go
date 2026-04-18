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

// TestHandle_OrgProductTypes verifies counts per productType and the
// hard-coded field order. The frame is used by the frontend to decide which
// device-family nav pages to show, so the field-name contract is important.
func TestHandle_OrgProductTypes(t *testing.T) {
	const payload = `[
	  {"serial":"A","productType":"switch"},
	  {"serial":"B","productType":"switch"},
	  {"serial":"C","productType":"wireless"},
	  {"serial":"D","productType":"sensor"}
	]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/organizations/o1/devices") {
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
	}))
	t.Cleanup(srv.Close)

	client, _ := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	resp, err := Handle(context.Background(), client, &QueryRequest{
		Queries: []MerakiQuery{{RefID: "A", Kind: KindOrgProductTypes, OrgID: "o1"}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	var frame data.Frame
	if err := json.Unmarshal(resp.Frames[0], &frame); err != nil {
		t.Fatalf("decode: %v", err)
	}

	want := map[string]int64{
		"appliance":       0,
		"wireless":        1,
		"switch":          2,
		"camera":          0,
		"cellularGateway": 0,
		"sensor":          1,
		"systemsManager":  0,
	}
	for name, v := range want {
		f, _ := frame.FieldByName(name)
		if f == nil {
			t.Errorf("frame missing %s field", name)
			continue
		}
		got, _ := f.ConcreteAt(0)
		if got != v {
			t.Errorf("%s = %v, want %d", name, got, v)
		}
	}
}
