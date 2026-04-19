package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// TestHandle_NetworkTraffic_FlatTable confirms the per-network handler emits
// a single table frame with one row per (networkId, application) tuple
// across the selected networks. The handler fans out across NetworkIDs
// sequentially and concatenates the results — verified here by stubbing two
// distinct networks with one row each and asserting the frame carries
// rows from both.
func TestHandle_NetworkTraffic_FlatTable(t *testing.T) {
	const n1Payload = `[
	  {"application":"YouTube","destination":"youtube.com","protocol":"tcp","port":443,"sent":12.5,"recv":250.0,"numClients":3,"activeTime":600,"flows":15}
	]`
	const n2Payload = `[
	  {"application":"Spotify","destination":"spotify.com","protocol":"tcp","port":443,"sent":2.0,"recv":40.5,"numClients":1,"activeTime":120,"flows":5}
	]`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/networks/N1/traffic"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(n1Payload))
		case strings.Contains(r.URL.Path, "/networks/N2/traffic"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(n2Payload))
		default:
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	now := time.Now().UTC()
	resp, err := Handle(context.Background(), client, &QueryRequest{
		Range: TimeRange{
			From: now.Add(-1 * time.Hour).UnixMilli(),
			To:   now.UnixMilli(),
		},
		Queries: []MerakiQuery{{
			RefID:      "A",
			Kind:       KindNetworkTraffic,
			NetworkIDs: []string{"N1", "N2"},
		}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got := len(resp.Frames); got != 1 {
		t.Fatalf("got %d frames, want 1 (single table frame)", got)
	}

	var frame data.Frame
	if err := json.Unmarshal(resp.Frames[0], &frame); err != nil {
		t.Fatalf("decode frame: %v", err)
	}
	rows, _ := frame.RowLen()
	if rows != 2 {
		t.Fatalf("got %d rows, want 2 (one per network)", rows)
	}

	for _, name := range []string{
		"networkId", "application", "category", "destination",
		"protocol", "port", "sentMb", "recvMb", "totalMb",
		"numClients", "activeTime", "flows",
	} {
		if f, _ := frame.FieldByName(name); f == nil {
			t.Fatalf("frame missing %q column; got fields=%v", name, frame.Fields)
		}
	}

	netField, _ := frame.FieldByName("networkId")
	appField, _ := frame.FieldByName("application")
	totalField, _ := frame.FieldByName("totalMb")

	seen := map[string]float64{}
	for i := range rows {
		nid, _ := netField.ConcreteAt(i)
		app, _ := appField.ConcreteAt(i)
		total, _ := totalField.ConcreteAt(i)
		seen[nid.(string)+"|"+app.(string)] = total.(float64)
	}
	if got := seen["N1|YouTube"]; got != 262.5 {
		t.Fatalf("N1/YouTube totalMb = %v, want 262.5 (sent+recv)", got)
	}
	if got := seen["N2|Spotify"]; got != 42.5 {
		t.Fatalf("N2/Spotify totalMb = %v, want 42.5 (sent+recv)", got)
	}
}

// TestHandle_NetworkTraffic_RequiresNetworkIDs confirms the handler emits a
// frame notice (via the dispatcher's error-frame wrapper) when no network
// IDs are supplied — single bad query shouldn't blank the panel.
func TestHandle_NetworkTraffic_RequiresNetworkIDs(t *testing.T) {
	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: "http://unused"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	resp, err := Handle(context.Background(), client, &QueryRequest{
		Queries: []MerakiQuery{{RefID: "A", Kind: KindNetworkTraffic}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got := len(resp.Frames); got != 1 {
		t.Fatalf("got %d frames, want 1 (synthetic error frame)", got)
	}

	var frame data.Frame
	if err := json.Unmarshal(resp.Frames[0], &frame); err != nil {
		t.Fatalf("decode frame: %v", err)
	}
	if frame.Meta == nil || len(frame.Meta.Notices) == 0 {
		t.Fatalf("expected error notice on frame; got meta=%v", frame.Meta)
	}
	if !strings.Contains(frame.Meta.Notices[0].Text, "networkIds is required") {
		t.Fatalf("notice text = %q, want it to mention 'networkIds is required'", frame.Meta.Notices[0].Text)
	}
}

// TestHandle_NetworkTraffic_AllNetworksFanout confirms that when the $network
// picker resolves to its "All" sentinel (a single empty-string entry from
// `allValue: ''` on the scene variable), the handler expands to the concrete
// org network list via /organizations/{id}/networks instead of iterating over
// the empty string and producing a blank frame. Previously the per-network
// panel on the Traffic page appeared empty whenever the user hadn't picked a
// specific network.
func TestHandle_NetworkTraffic_AllNetworksFanout(t *testing.T) {
	const orgNetworks = `[
	  {"id":"N1","name":"Site A","productTypes":["wireless"]},
	  {"id":"N2","name":"Site B","productTypes":["switch"]}
	]`
	const n1Payload = `[{"application":"YouTube","destination":"youtube.com","protocol":"tcp","port":443,"sent":1.0,"recv":2.0,"numClients":1,"activeTime":10,"flows":1}]`
	const n2Payload = `[{"application":"Spotify","destination":"spotify.com","protocol":"tcp","port":443,"sent":3.0,"recv":4.0,"numClients":1,"activeTime":10,"flows":1}]`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/organizations/o1/networks"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(orgNetworks))
		case strings.Contains(r.URL.Path, "/networks/N1/traffic"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(n1Payload))
		case strings.Contains(r.URL.Path, "/networks/N2/traffic"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(n2Payload))
		default:
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	now := time.Now().UTC()
	resp, err := Handle(context.Background(), client, &QueryRequest{
		Range: TimeRange{
			From: now.Add(-1 * time.Hour).UnixMilli(),
			To:   now.UnixMilli(),
		},
		Queries: []MerakiQuery{{
			RefID:      "A",
			Kind:       KindNetworkTraffic,
			OrgID:      "o1",
			NetworkIDs: []string{""}, // "All" sentinel from the $network picker
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
	if rows != 2 {
		t.Fatalf("got %d rows, want 2 (one per org network after fanout)", rows)
	}
}

// TestHandle_TopApplicationsByUsage_WideRow asserts the org-wide top-apps
// handler emits a wide table frame with the expected columns and that the
// nested clients counter is flattened into a `clientCount` column.
func TestHandle_TopApplicationsByUsage_WideRow(t *testing.T) {
	// Meraki's byUsage endpoint names the app column `application` (not
	// `name`) and does not include a `category` field on each row — the
	// category breakdown lives on the sibling /categories/byUsage path.
	// Clients are present here as the spec still documents the optional
	// `clients` sub-object on byUsage responses.
	const payload = `[
	  {"application":"YouTube","total":500.5,"downstream":480.0,"upstream":20.5,"percentage":42.1,"clients":{"counts":{"total":12}}},
	  {"application":"Spotify","total":200.0,"downstream":190.0,"upstream":10.0,"percentage":18.4,"clients":{"counts":{"total":4}}}
	]`

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if !strings.Contains(r.URL.Path, "/summary/top/applications/byUsage") {
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		// Confirm quantity override is propagated when q.Metrics[0] is set.
		if got := r.URL.Query().Get("quantity"); got != "25" {
			t.Errorf("quantity query = %q, want 25", got)
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
			Kind:    KindTopApplicationsByUsage,
			OrgID:   "o1",
			Metrics: []string{"25"}, // quantity override
		}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got := len(resp.Frames); got != 1 {
		t.Fatalf("got %d frames, want 1", got)
	}
	if calls.Load() == 0 {
		t.Fatalf("upstream was never called")
	}

	var frame data.Frame
	if err := json.Unmarshal(resp.Frames[0], &frame); err != nil {
		t.Fatalf("decode frame: %v", err)
	}
	rows, _ := frame.RowLen()
	if rows != 2 {
		t.Fatalf("got %d rows, want 2", rows)
	}
	for _, name := range []string{"name", "category", "totalMb", "downstreamMb", "upstreamMb", "percentage", "clientCount"} {
		if f, _ := frame.FieldByName(name); f == nil {
			t.Fatalf("frame missing %q column; got fields=%v", name, frame.Fields)
		}
	}

	// The `application` wire field must surface in the emitted `name` column —
	// a prior decode bug used the wrong JSON tag and returned empty strings
	// here even when the upstream response was populated.
	nameField, _ := frame.FieldByName("name")
	if got, _ := nameField.ConcreteAt(0); got != "YouTube" {
		t.Fatalf("row 0 name = %v, want YouTube (decoded from `application` wire field)", got)
	}

	clientField, _ := frame.FieldByName("clientCount")
	if got, _ := clientField.ConcreteAt(0); got != int64(12) {
		t.Fatalf("row 0 clientCount = %v, want 12 (flattened from clients.counts.total)", got)
	}
}

// TestHandle_TopApplicationCategoriesByUsage_PathIncludesCategoriesSlash
// guards against a path-shape regression: Meraki's spec uses
// `/applications/categories/byUsage` (with a slash separator), NOT the
// camelCase `/applicationsCategories/byUsage`. Verified via ctx7 against the
// canonical OpenAPI spec on 2026-04-18; calling the wrong path would 404.
func TestHandle_TopApplicationCategoriesByUsage_PathIncludesCategoriesSlash(t *testing.T) {
	// Category rows name the discriminator column `category` on the wire.
	const payload = `[
	  {"category":"Video","total":1024.0,"downstream":1000.0,"upstream":24.0,"percentage":52.3,"clients":{"counts":{"total":18}}}
	]`

	var observedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observedPath = r.URL.Path
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
			RefID: "A",
			Kind:  KindTopApplicationCategoriesByUsage,
			OrgID: "o1",
		}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !strings.Contains(observedPath, "/summary/top/applications/categories/byUsage") {
		t.Fatalf("upstream path = %q, want to contain /summary/top/applications/categories/byUsage", observedPath)
	}

	// Confirm the `category` wire field surfaces in the emitted `name`
	// column — same decode-bug guard as the sibling applications test.
	if got := len(resp.Frames); got != 1 {
		t.Fatalf("got %d frames, want 1", got)
	}
	var frame data.Frame
	if err := json.Unmarshal(resp.Frames[0], &frame); err != nil {
		t.Fatalf("decode frame: %v", err)
	}
	nameField, _ := frame.FieldByName("name")
	if nameField == nil {
		t.Fatalf("frame missing `name` column; fields=%v", frame.Fields)
	}
	if got, _ := nameField.ConcreteAt(0); got != "Video" {
		t.Fatalf("row 0 name = %v, want Video (decoded from `category` wire field)", got)
	}
}

// TestHandle_NetworkTrafficAnalysisMode_PerNetworkRows confirms the mode
// lookup handler emits one row per requested network with the decoded mode
// string. De-duplicates and sorts so the frame is deterministic regardless
// of the order Scenes interpolates the variable.
func TestHandle_NetworkTrafficAnalysisMode_PerNetworkRows(t *testing.T) {
	const detailedPayload = `{"mode":"detailed","customPieChartItems":[]}`
	const disabledPayload = `{"mode":"disabled","customPieChartItems":[]}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/networks/N1/trafficAnalysis"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(detailedPayload))
		case strings.Contains(r.URL.Path, "/networks/N2/trafficAnalysis"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(disabledPayload))
		default:
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	// Pass duplicates + reversed order to confirm de-dup + sort.
	resp, err := Handle(context.Background(), client, &QueryRequest{
		Queries: []MerakiQuery{{
			RefID:      "A",
			Kind:       KindNetworkTrafficAnalysisMode,
			NetworkIDs: []string{"N2", "N1", "N2"},
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
	if rows != 2 {
		t.Fatalf("got %d rows, want 2 (de-duped from 3 inputs)", rows)
	}

	netField, _ := frame.FieldByName("networkId")
	modeField, _ := frame.FieldByName("mode")
	if netField == nil || modeField == nil {
		t.Fatalf("frame missing networkId or mode column; fields=%v", frame.Fields)
	}

	// Sorted: N1 (detailed) before N2 (disabled).
	if got, _ := netField.ConcreteAt(0); got != "N1" {
		t.Fatalf("row 0 networkId = %v, want N1 (sort order)", got)
	}
	if got, _ := modeField.ConcreteAt(0); got != "detailed" {
		t.Fatalf("row 0 mode = %v, want detailed", got)
	}
	if got, _ := modeField.ConcreteAt(1); got != "disabled" {
		t.Fatalf("row 1 mode = %v, want disabled", got)
	}
}
