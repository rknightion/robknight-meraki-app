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

// TestHandle_DeviceStatusByFamily verifies the handler rolls the
// availabilities feed up by productType into a stable wide frame — one row
// per family, one field per status bucket, plus a total.
func TestHandle_DeviceStatusByFamily(t *testing.T) {
	const payload = `[
	  {"serial":"A","status":"online","productType":"switch","network":{"id":"N","name":"Lab"}},
	  {"serial":"B","status":"online","productType":"switch","network":{"id":"N","name":"Lab"}},
	  {"serial":"C","status":"alerting","productType":"wireless","network":{"id":"N","name":"Lab"}},
	  {"serial":"D","status":"offline","productType":"appliance","network":{"id":"N","name":"Lab"}},
	  {"serial":"E","status":"dormant","productType":"switch","network":{"id":"N","name":"Lab"}},
	  {"serial":"F","status":"online","productType":"wireless","network":{"id":"N","name":"Lab"}}
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
		Queries: []MerakiQuery{{RefID: "A", Kind: KindDeviceStatusByFamily, OrgID: "o1"}},
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

	rows, _ := frame.RowLen()
	if rows != 3 {
		t.Fatalf("got %d rows, want 3 (appliance, switch, wireless)", rows)
	}

	// Rows are sorted alphabetically by productType.
	pt, _ := frame.FieldByName("productType")
	if pt == nil {
		t.Fatalf("missing productType field")
	}
	if got, _ := pt.ConcreteAt(0); got != "appliance" {
		t.Errorf("row 0 productType = %v, want appliance", got)
	}
	if got, _ := pt.ConcreteAt(1); got != "switch" {
		t.Errorf("row 1 productType = %v, want switch", got)
	}
	if got, _ := pt.ConcreteAt(2); got != "wireless" {
		t.Errorf("row 2 productType = %v, want wireless", got)
	}

	// switch: 2 online, 0 alerting, 0 offline, 1 dormant, total 3.
	assertRow(t, &frame, 1, map[string]int64{
		"online": 2, "alerting": 0, "offline": 0, "dormant": 1, "total": 3,
	})
	// wireless: 1 online, 1 alerting, 0 offline, 0 dormant, total 2.
	assertRow(t, &frame, 2, map[string]int64{
		"online": 1, "alerting": 1, "offline": 0, "dormant": 0, "total": 2,
	})
	// appliance: 0 online, 0 alerting, 1 offline, 0 dormant, total 1.
	assertRow(t, &frame, 0, map[string]int64{
		"online": 0, "alerting": 0, "offline": 1, "dormant": 0, "total": 1,
	})
}

// TestHandle_DeviceStatusByFamily_RequiresOrgID pins the usual "orgId required"
// invariant so the handler doesn't silently fan out to an empty URL.
func TestHandle_DeviceStatusByFamily_RequiresOrgID(t *testing.T) {
	client, _ := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: "http://127.0.0.1:1"})

	resp, err := Handle(context.Background(), client, &QueryRequest{
		Queries: []MerakiQuery{{RefID: "A", Kind: KindDeviceStatusByFamily}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(resp.Frames) != 1 {
		t.Fatalf("want 1 frame (error notice), got %d", len(resp.Frames))
	}
	// The handler returns an error; the dispatcher wraps it as a notice on the
	// synthetic error frame. Just ensure we didn't get a successful data frame.
	var frame data.Frame
	_ = json.Unmarshal(resp.Frames[0], &frame)
	if len(frame.Meta.Notices) == 0 {
		t.Fatalf("expected an error notice, got none")
	}
}

func assertRow(t *testing.T, f *data.Frame, row int, want map[string]int64) {
	t.Helper()
	for name, v := range want {
		field, _ := f.FieldByName(name)
		if field == nil {
			t.Fatalf("frame missing %s field", name)
			return
		}
		got, _ := field.ConcreteAt(row)
		if got != v {
			t.Errorf("row %d %s = %v, want %d", row, name, got, v)
		}
	}
}
