package query

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// TestHandle_NetworkEvents_StartingAfterPagination verifies the wrapper's
// cursor-pagination loop: when a page returns `perPage` events plus a
// `pageEndAt`, the wrapper should issue a follow-up request with
// `startingAfter=pageEndAt`, appending its events to the result until a
// short page arrives.
//
// Our stub serves:
//   - page 1: 1000 events + pageEndAt → wrapper continues
//   - page 2:  2 events               → wrapper terminates (short page)
//
// Expected concatenated frame: 1002 rows.
func TestHandle_NetworkEvents_StartingAfterPagination(t *testing.T) {
	// Build a 1000-event page on the fly.
	page1Events := make([]string, 0, 1000)
	for i := 0; i < 1000; i++ {
		page1Events = append(page1Events, fmt.Sprintf(
			`{"occurredAt":"2026-04-17T10:%02d:%02dZ","networkId":"N1","type":"association","description":"Client associated #%d","category":"wireless","productType":"wireless","deviceSerial":"AP1","deviceName":"AP-1","clientId":"C%d","clientMac":"aa:bb:cc:dd:ee:%02x","clientDescription":"client-%d"}`,
			i/60, i%60, i, i, i%256, i,
		))
	}
	page1 := `{"events":[` + strings.Join(page1Events, ",") + `],"pageStartAt":"2026-04-17T10:00:00Z","pageEndAt":"2026-04-17T10:59:59Z"}`
	const page2 = `{"events":[
	  {"occurredAt":"2026-04-17T11:00:00Z","networkId":"N1","type":"disassociation","description":"Client left","category":"wireless","productType":"wireless","deviceSerial":"AP1","deviceName":"AP-1"},
	  {"occurredAt":"2026-04-17T11:00:05Z","networkId":"N1","type":"dhcp","description":"DHCP lease","category":"wireless","productType":"wireless","deviceSerial":"AP1","deviceName":"AP-1"}
	],"pageStartAt":"2026-04-17T11:00:00Z","pageEndAt":"2026-04-17T11:00:05Z"}`

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/events") {
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		n := atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		switch n {
		case 1:
			// First call: no startingAfter yet.
			if got := r.URL.Query().Get("startingAfter"); got != "" {
				t.Errorf("call 1: startingAfter = %q, want empty", got)
			}
			_, _ = w.Write([]byte(page1))
		case 2:
			// Second call: wrapper should echo back page 1's pageEndAt as
			// startingAfter. Don't pin the exact value — assert non-empty is
			// enough; the subsequent short-page termination is the real test.
			if got := r.URL.Query().Get("startingAfter"); got == "" {
				t.Errorf("call 2: startingAfter was empty; wrapper did not advance cursor")
			}
			_, _ = w.Write([]byte(page2))
		default:
			t.Errorf("wrapper made %d calls; expected 2", n)
			_, _ = w.Write([]byte(`{"events":[]}`))
		}
	}))
	t.Cleanup(srv.Close)

	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	resp, err := Handle(context.Background(), client, &QueryRequest{
		Queries: []MerakiQuery{{
			RefID:        "A",
			Kind:         KindNetworkEvents,
			NetworkIDs:   []string{"N1"},
			ProductTypes: []string{"wireless"},
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
		t.Fatalf("decode frame: %v", err)
	}
	rows, _ := frame.RowLen()
	if rows != 1002 {
		t.Fatalf("got %d rows, want 1002 (page1=1000 + page2=2)", rows)
	}
	if n := atomic.LoadInt32(&calls); n != 2 {
		t.Fatalf("got %d upstream calls, want 2", n)
	}
}

// TestHandle_NetworkEvents_RequiresNetworkID verifies the guard rail: calling
// the handler with no NetworkIDs returns an error frame (notice attached),
// not a successful empty frame.
func TestHandle_NetworkEvents_RequiresNetworkID(t *testing.T) {
	// Server should never be called — the handler must fail fast before any
	// HTTP request. httptest is wired defensively so we'd see the failure in
	// the form of a panic if the wiring regresses.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected upstream call: %s", r.URL.Path)
		http.Error(w, "no call expected", http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)

	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	resp, err := Handle(context.Background(), client, &QueryRequest{
		Queries: []MerakiQuery{{RefID: "A", Kind: KindNetworkEvents}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got := len(resp.Frames); got != 1 {
		t.Fatalf("got %d frames, want 1 (error frame)", got)
	}

	var frame data.Frame
	if err := json.Unmarshal(resp.Frames[0], &frame); err != nil {
		t.Fatalf("decode frame: %v", err)
	}
	// The handler returned (nil, err); Handle manufactures an error frame
	// named "<refId>_error" and attaches the notice on it.
	if !strings.Contains(frame.Name, "error") {
		t.Fatalf("frame name = %q, want *_error (indicating guard rail fired)", frame.Name)
	}
	if frame.Meta == nil || len(frame.Meta.Notices) == 0 {
		t.Fatalf("expected an error notice on the frame; got meta=%v", frame.Meta)
	}
	msg := frame.Meta.Notices[0].Text
	if !strings.Contains(msg, "networkId") {
		t.Fatalf("error notice = %q, want to mention networkId", msg)
	}
}
