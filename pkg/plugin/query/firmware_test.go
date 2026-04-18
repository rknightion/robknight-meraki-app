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

// TestHandle_FirmwareUpgrades_TableShape covers the org-level upgrades feed.
// We expect a single table frame with one row per upgrade event, including
// both completed and scheduled rows (we keep them in a single table — see
// the design comment on handleFirmwareUpgrades).
func TestHandle_FirmwareUpgrades_TableShape(t *testing.T) {
	const payload = `[
	  {
	    "upgradeId": "u1",
	    "upgradeBatchId": "b1",
	    "time": "2026-04-10T03:00:00Z",
	    "status": "completed",
	    "productType": "wireless",
	    "network": {"id": "N1", "name": "Lab"},
	    "fromVersion": {"id": "1000", "shortName": "MR 30.6", "releaseType": "stable"},
	    "toVersion":   {"id": "1001", "shortName": "MR 30.7", "releaseType": "stable"},
	    "staged": false
	  },
	  {
	    "upgradeId": "u2",
	    "upgradeBatchId": "b2",
	    "time": "2026-04-25T03:00:00Z",
	    "status": "scheduled",
	    "productType": "switch",
	    "network": {"id": "N2", "name": "Office"},
	    "fromVersion": {"id": "2000", "shortName": "MS 16.4"},
	    "toVersion":   {"id": "2001", "shortName": "MS 17.0", "releaseType": "beta"},
	    "staged": true
	  }
	]`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/firmware/upgrades") {
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
		Queries: []MerakiQuery{{RefID: "A", Kind: KindFirmwareUpgrades, OrgID: "o1"}},
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
		t.Fatalf("got %d rows, want 2", rows)
	}

	for _, col := range []string{"time", "status", "productType", "networkId", "networkName", "fromVersion", "toVersion", "releaseType", "staged", "upgradeId"} {
		if f, _ := frame.FieldByName(col); f == nil {
			t.Fatalf("frame missing %q column; got fields=%v", col, frame.Fields)
		}
	}

	statusField, _ := frame.FieldByName("status")
	toField, _ := frame.FieldByName("toVersion")
	if got, _ := statusField.ConcreteAt(0); got != "completed" {
		t.Fatalf("row 0 status = %v, want completed", got)
	}
	// `shortName` should be preferred over the numeric id when emitting versions.
	if got, _ := toField.ConcreteAt(1); got != "MS 17.0" {
		t.Fatalf("row 1 toVersion = %v, want MS 17.0", got)
	}
}

// TestHandle_FirmwarePending_TableShape exercises the per-device pending
// upgrades handler. We expect one row per device with a non-empty upgrade
// envelope, including the computed `daysUntil` column.
func TestHandle_FirmwarePending_TableShape(t *testing.T) {
	// Two devices with pending upgrades + one device with an empty upgrade
	// envelope (which the handler should skip).
	const payload = `[
	  {
	    "serial": "Q2XX-MR-AAAA",
	    "name": "AP-1",
	    "model": "MR46",
	    "network": {"id": "N1", "name": "Lab"},
	    "upgrade": {
	      "upgradeBatchId": "b1",
	      "status": "scheduled",
	      "fromVersion": {"id": "1000", "shortName": "MR 30.6"},
	      "toVersion":   {"id": "1001", "shortName": "MR 30.7", "scheduledFor": "2099-01-01T03:00:00Z"},
	      "staged": {"group": {"id": "g1", "name": "Wave 1"}}
	    }
	  },
	  {
	    "serial": "Q2XX-MS-BBBB",
	    "name": "Switch-1",
	    "model": "MS250-48",
	    "network": {"id": "N1", "name": "Lab"},
	    "upgrade": {
	      "upgradeBatchId": "b2",
	      "status": "started",
	      "fromVersion": {"id": "2000", "shortName": "MS 16.4"},
	      "toVersion":   {"id": "2001", "shortName": "MS 17.0", "scheduledFor": "2099-02-01T03:00:00Z"},
	      "staged": {"group": {"id": "", "name": ""}}
	    }
	  },
	  {
	    "serial": "Q2XX-NOPE-CCCC",
	    "name": "Idle",
	    "model": "MR36",
	    "network": {"id": "N1", "name": "Lab"},
	    "upgrade": {
	      "fromVersion": {},
	      "toVersion": {},
	      "staged": {"group": {}}
	    }
	  }
	]`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/firmware/upgrades/byDevice") {
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		// Verify the handler set currentUpgradesOnly=true so cached
		// "completed" rows don't pollute the pending table.
		if got := r.URL.Query().Get("currentUpgradesOnly"); got != "true" {
			http.Error(w, "expected currentUpgradesOnly=true; got "+got, http.StatusBadRequest)
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
		Queries: []MerakiQuery{{RefID: "A", Kind: KindFirmwarePending, OrgID: "o1"}},
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
	// Two devices with active upgrades; the empty-envelope row was skipped.
	if rows != 2 {
		t.Fatalf("got %d rows, want 2 (third row should be skipped)", rows)
	}

	for _, col := range []string{"serial", "name", "model", "networkId", "currentVersion", "targetVersion", "scheduledFor", "daysUntil", "status", "stagedGroup"} {
		if f, _ := frame.FieldByName(col); f == nil {
			t.Fatalf("frame missing %q column; got fields=%v", col, frame.Fields)
		}
	}

	serialField, _ := frame.FieldByName("serial")
	stagedField, _ := frame.FieldByName("stagedGroup")
	daysField, _ := frame.FieldByName("daysUntil")

	if got, _ := serialField.ConcreteAt(0); got != "Q2XX-MR-AAAA" {
		t.Fatalf("row 0 serial = %v, want Q2XX-MR-AAAA", got)
	}
	// AP-1 is in the "Wave 1" staged group; the switch is not staged.
	if got, _ := stagedField.ConcreteAt(0); got != "Wave 1" {
		t.Fatalf("row 0 stagedGroup = %v, want Wave 1", got)
	}
	if got, _ := stagedField.ConcreteAt(1); got != "" {
		t.Fatalf("row 1 stagedGroup = %v, want empty (no staged rollout)", got)
	}
	// daysUntil is computed against now; both scheduled times are in 2099,
	// so we expect a large positive number for both rows.
	if got, _ := daysField.ConcreteAt(0); got.(int64) < 1000 {
		t.Fatalf("row 0 daysUntil = %v, want > 1000 (scheduled in 2099)", got)
	}
}

// TestHandle_DeviceEol_SortedByDaysUntil covers the EOL handler: per-device
// rows decoded from the inventory endpoint, sorted ascending by daysUntil.
// We also verify the handler defaults `eoxStatuses[]` to all three buckets
// when the caller leaves q.Metrics empty.
func TestHandle_DeviceEol_SortedByDaysUntil(t *testing.T) {
	// Three devices: one already past end-of-support (negative days), one
	// near end-of-support (small positive), one with a far-future date.
	// The handler should emit them in ascending daysUntil order.
	const payload = `[
	  {
	    "serial": "Q2XX-NEAR-AAAA",
	    "name": "Soon-EOL",
	    "model": "MR42",
	    "productType": "wireless",
	    "networkId": "N1",
	    "eoxStatus": "nearEndOfSupport",
	    "endOfSaleDate": "2024-01-01T00:00:00Z",
	    "endOfSupportDate": "2099-06-01T00:00:00Z"
	  },
	  {
	    "serial": "Q2XX-PAST-BBBB",
	    "name": "Already-EOL",
	    "model": "MS220-8",
	    "productType": "switch",
	    "networkId": "N1",
	    "eoxStatus": "endOfSupport",
	    "endOfSaleDate": "2018-01-01T00:00:00Z",
	    "endOfSupportDate": "2023-01-01T00:00:00Z"
	  },
	  {
	    "serial": "Q2XX-FAR-CCCC",
	    "name": "Long-EOL",
	    "model": "MR46",
	    "productType": "wireless",
	    "networkId": "N2",
	    "eoxStatus": "endOfSale",
	    "endOfSaleDate": "2099-12-01T00:00:00Z",
	    "endOfSupportDate": "2199-01-01T00:00:00Z"
	  }
	]`

	var capturedQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/inventory/devices") {
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		capturedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
	}))
	t.Cleanup(srv.Close)

	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	resp, err := Handle(context.Background(), client, &QueryRequest{
		Queries: []MerakiQuery{{RefID: "A", Kind: KindDeviceEol, OrgID: "o1"}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got := len(resp.Frames); got != 1 {
		t.Fatalf("got %d frames, want 1", got)
	}

	// The handler should default to filtering on all three EOX buckets.
	for _, want := range []string{"endOfSale", "endOfSupport", "nearEndOfSupport"} {
		if !strings.Contains(capturedQuery, "eoxStatuses%5B%5D="+want) {
			t.Fatalf("expected eoxStatuses[]=%s in query; got %s", want, capturedQuery)
		}
	}

	var frame data.Frame
	if err := json.Unmarshal(resp.Frames[0], &frame); err != nil {
		t.Fatalf("decode frame: %v (body=%s)", err, string(resp.Frames[0]))
	}

	rows, _ := frame.RowLen()
	if rows != 3 {
		t.Fatalf("got %d rows, want 3", rows)
	}

	for _, col := range []string{"serial", "name", "model", "productType", "networkId", "eoxStatus", "endOfSaleDate", "endOfSupportDate", "daysUntil"} {
		if f, _ := frame.FieldByName(col); f == nil {
			t.Fatalf("frame missing %q column; got fields=%v", col, frame.Fields)
		}
	}

	// Rows should be sorted by daysUntil ascending — past < near < far.
	serialField, _ := frame.FieldByName("serial")
	want := []string{"Q2XX-PAST-BBBB", "Q2XX-NEAR-AAAA", "Q2XX-FAR-CCCC"}
	for i, exp := range want {
		got, _ := serialField.ConcreteAt(i)
		if got != exp {
			t.Fatalf("row %d serial = %v, want %v (sort order)", i, got, exp)
		}
	}
}
