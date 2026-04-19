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

// orgHealthStubServer serves every downstream endpoint the
// handleOrgHealthSummary fan-out needs. Each matcher is substring-based so a
// single handler can cover the /licenses probe + /licenses list the
// handleLicensesList path walks. Hit counts are tracked per-matcher so the
// "second call is cached" test can assert zero growth on the second call.
type orgHealthStubServer struct {
	srv  *httptest.Server
	hits *orgHealthHits
}

type orgHealthHits struct {
	deviceStatus       int64
	alertsOverview     int64
	licensesOverview   int64
	licensesList       int64
	firmwareUpgrades   int64
	apiRequestsByInt   int64
	applianceUplinks   int64
	networks           int64 // secondary call by applianceUplinkStatuses
}

func newOrgHealthStubServer(t *testing.T) *orgHealthStubServer {
	t.Helper()
	hits := &orgHealthHits{}

	// A single httptest server with a path-matcher dispatcher mirrors the
	// pattern used in insights_test.go and org_change_feed_test.go.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		switch {
		// Device status overview → deviceStatusOverview handler.
		// Two online, one offline, one alerting (alerting is NOT surfaced in
		// the KPI row per the spec).
		case strings.HasSuffix(path, "/devices/statuses/overview"):
			atomic.AddInt64(&hits.deviceStatus, 1)
			_, _ = w.Write([]byte(`{"counts":{"byStatus":{"online":2,"alerting":1,"offline":1,"dormant":0}}}`))

		// Alerts severity overview → alertsOverview handler. The handler now
		// hits /assurance/alerts/overview (NOT .../byType) because that's
		// the endpoint that exposes counts.bySeverity. Match must come
		// BEFORE any generic /assurance/alerts handler.
		case strings.HasSuffix(path, "/assurance/alerts/overview"):
			atomic.AddInt64(&hits.alertsOverview, 1)
			_, _ = w.Write([]byte(`{"counts":{"total":8,"bySeverity":[{"type":"critical","count":4},{"type":"warning","count":3},{"type":"informational","count":1}]}}`))

		// Licenses overview probe → short-circuit on non-coterm shape (must
		// have /licenses/overview matched BEFORE /licenses).
		case strings.HasSuffix(path, "/licenses/overview"):
			atomic.AddInt64(&hits.licensesOverview, 1)
			// Per-device shape — the handler proceeds to /licenses after this.
			_, _ = w.Write([]byte(`{"states":{"active":{"count":3},"expired":{"count":0},"expiring":{"count":0},"recentlyQueued":{"count":0},"unused":{"count":0},"unusedActive":{"count":0}}}`))

		// Licenses list → licensesList handler. Three licenses:
		//   - 60d out   (not counted)
		//   - 20d out   (exp30d)
		//   - 3d  out   (exp30d + exp7d)
		case strings.HasSuffix(path, "/licenses"):
			atomic.AddInt64(&hits.licensesList, 1)
			now := time.Now().UTC()
			exp60 := now.Add(60 * 24 * time.Hour).Format(time.RFC3339)
			exp20 := now.Add(20 * 24 * time.Hour).Format(time.RFC3339)
			exp3 := now.Add(3 * 24 * time.Hour).Format(time.RFC3339)
			body := `[
			  {"id":"L1","licenseType":"ENT","state":"active","deviceSerial":"S1","networkId":"N1","seatCount":1,"activationDate":"2024-01-01T00:00:00Z","expirationDate":"` + exp60 + `"},
			  {"id":"L2","licenseType":"ENT","state":"active","deviceSerial":"S2","networkId":"N1","seatCount":1,"activationDate":"2024-01-01T00:00:00Z","expirationDate":"` + exp20 + `"},
			  {"id":"L3","licenseType":"ENT","state":"active","deviceSerial":"S3","networkId":"N1","seatCount":1,"activationDate":"2024-01-01T00:00:00Z","expirationDate":"` + exp3 + `"}
			]`
			_, _ = w.Write([]byte(body))

		// Firmware pending → two devices with a pending upgrade.
		case strings.Contains(path, "/firmware/upgrades/byDevice"):
			atomic.AddInt64(&hits.firmwareUpgrades, 1)
			_, _ = w.Write([]byte(`[
			  {"serial":"Q1","name":"sw1","model":"MS250","network":{"id":"N1","name":"Lab"},"upgrade":{"upgradeBatchId":"b1","status":"started","fromVersion":{"id":"1","shortName":"MS 15"},"toVersion":{"id":"2","shortName":"MS 16","scheduledFor":"2099-01-01T00:00:00Z"},"staged":{"group":{"name":""}}}},
			  {"serial":"Q2","name":"ap1","model":"MR36","network":{"id":"N1","name":"Lab"},"upgrade":{"upgradeBatchId":"b2","status":"scheduled","fromVersion":{"id":"3","shortName":"MR 29"},"toVersion":{"id":"4","shortName":"MR 30","scheduledFor":"2099-02-01T00:00:00Z"},"staged":{"group":{"name":""}}}}
			]`))

		// API requests byInterval → 1000 total: 950×2xx + 40×4xx + 10×429 (1% 429 rate).
		case strings.Contains(path, "/apiRequests/overview/responseCodes/byInterval"):
			atomic.AddInt64(&hits.apiRequestsByInt, 1)
			_, _ = w.Write([]byte(`[
			  {"startTs":"2026-04-17T09:00:00Z","endTs":"2026-04-17T10:00:00Z","counts":[{"code":200,"count":950},{"code":404,"count":40},{"code":429,"count":10}]}
			]`))

		// Appliance uplink statuses → three uplinks; two "failed", one "active".
		case strings.Contains(path, "/appliance/uplink/statuses"):
			atomic.AddInt64(&hits.applianceUplinks, 1)
			_, _ = w.Write([]byte(`[
			  {"serial":"Q2XX-MX1","model":"MX68","networkId":"N1","uplinks":[
			    {"interface":"wan1","status":"active"},
			    {"interface":"wan2","status":"failed"}
			  ]},
			  {"serial":"Q2XX-MX2","model":"MX68","networkId":"N2","uplinks":[
			    {"interface":"wan1","status":"failed"}
			  ]}
			]`))

		// Networks lookup (best-effort, called by applianceUplinkStatuses).
		case strings.HasSuffix(path, "/networks"):
			atomic.AddInt64(&hits.networks, 1)
			_, _ = w.Write([]byte(`[{"id":"N1","organizationId":"o1","name":"Lab","productTypes":["appliance"]}]`))

		default:
			http.Error(w, "unexpected path: "+path, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	return &orgHealthStubServer{srv: srv, hits: hits}
}

// TestHandle_OrgHealthSummary_EmitsNineFieldWideFrame stubs every downstream
// endpoint and asserts the nine KPI fields are populated from the reduced
// frames. Pins the §4.4.4-E spec shape.
func TestHandle_OrgHealthSummary_EmitsNineFieldWideFrame(t *testing.T) {
	stub := newOrgHealthStubServer(t)
	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: stub.srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	resp, err := Handle(context.Background(), client, &QueryRequest{
		Queries: []MerakiQuery{{RefID: "A", Kind: KindOrgHealthSummary, OrgID: "o1"}},
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

	// Spec shape: nine fields, single row.
	wantFields := []string{
		"devicesOnline",
		"devicesOffline",
		"alertsCritical",
		"alertsWarning",
		"licensesExp30d",
		"licensesExp7d",
		"firmwareDrift",
		"apiErrorPct",
		"uplinksDown",
	}
	if got := len(frame.Fields); got != len(wantFields) {
		t.Fatalf("got %d fields, want %d", got, len(wantFields))
	}
	for i, want := range wantFields {
		if got := frame.Fields[i].Name; got != want {
			t.Errorf("field[%d].Name = %q, want %q", i, got, want)
		}
	}
	rows, err := frame.RowLen()
	if err != nil {
		t.Fatalf("RowLen: %v", err)
	}
	if rows != 1 {
		t.Fatalf("rows = %d, want 1 (wide single-row KPI frame)", rows)
	}

	// Field-level assertions — pins the reducer behaviour for each KPI.
	wantInt := map[string]int64{
		"devicesOnline":  2,
		"devicesOffline": 1,
		"alertsCritical": 4,
		"alertsWarning":  3,
		// Two licenses inside 30d (the 20d and 3d ones).
		"licensesExp30d": 2,
		// One license inside 7d (the 3d one).
		"licensesExp7d":  1,
		"firmwareDrift":  2,
		"uplinksDown":    2,
	}
	for name, want := range wantInt {
		f, _ := frame.FieldByName(name)
		if f == nil {
			t.Fatalf("missing field %q", name)
		}
		got, _ := f.At(0).(int64)
		if got != want {
			t.Errorf("%s = %d, want %d", name, got, want)
		}
	}

	// apiErrorPct: 10 / 1000 * 100 = 1.0 (float comparison with tolerance).
	apiField, _ := frame.FieldByName("apiErrorPct")
	if apiField == nil {
		t.Fatal("missing apiErrorPct")
	}
	gotPct, _ := apiField.At(0).(float64)
	if gotPct < 0.99 || gotPct > 1.01 {
		t.Errorf("apiErrorPct = %v, want ~1.0", gotPct)
	}
}

// TestHandle_OrgHealthSummary_RequiresOrgID guards the handler contract.
func TestHandle_OrgHealthSummary_RequiresOrgID(t *testing.T) {
	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "k", BaseURL: "https://unused"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := handleOrgHealthSummary(context.Background(), client, MerakiQuery{Kind: KindOrgHealthSummary}, TimeRange{}, Options{}); err == nil {
		t.Fatal("expected error for missing orgId")
	}
}

// TestHandle_OrgHealthSummary_CachedSecondCallHitsNoBackend verifies the
// singleflight+TTL contract from §4.4.4-E: after a first call populates the
// underlying meraki.Client cache, a second back-to-back call must not issue
// any downstream HTTP requests. The TTLs on the six downstream calls (30s–
// 15m) all comfortably cover the "back-to-back Home load" window this KPI
// tile serves.
func TestHandle_OrgHealthSummary_CachedSecondCallHitsNoBackend(t *testing.T) {
	stub := newOrgHealthStubServer(t)
	cache, err := meraki.NewTTLCache(64)
	if err != nil {
		t.Fatalf("NewTTLCache: %v", err)
	}
	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: stub.srv.URL, Cache: cache})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ctx := context.Background()
	req := &QueryRequest{
		Queries: []MerakiQuery{{RefID: "A", Kind: KindOrgHealthSummary, OrgID: "o1"}},
	}

	if _, err := Handle(ctx, client, req, Options{}); err != nil {
		t.Fatalf("first Handle: %v", err)
	}

	// Snapshot hit counts after call #1.
	first := *stub.hits

	if _, err := Handle(ctx, client, req, Options{}); err != nil {
		t.Fatalf("second Handle: %v", err)
	}

	second := *stub.hits

	// Every counter must be identical after the second call — the cache
	// absorbs the re-entry so zero new HTTP requests reach the stub.
	diffs := map[string][2]int64{
		"deviceStatus":     {first.deviceStatus, second.deviceStatus},
		"alertsOverview":   {first.alertsOverview, second.alertsOverview},
		"licensesOverview": {first.licensesOverview, second.licensesOverview},
		"licensesList":     {first.licensesList, second.licensesList},
		"firmwareUpgrades": {first.firmwareUpgrades, second.firmwareUpgrades},
		"apiRequestsByInt": {first.apiRequestsByInt, second.apiRequestsByInt},
		"applianceUplinks": {first.applianceUplinks, second.applianceUplinks},
		"networks":         {first.networks, second.networks},
	}
	for endpoint, counts := range diffs {
		if counts[0] != counts[1] {
			t.Errorf("endpoint %s: hits went %d → %d on second Handle (expected cache hit)", endpoint, counts[0], counts[1])
		}
	}

	// Sanity: first call actually populated each counter (so we know the
	// second-call assertion has teeth, rather than silently passing on a
	// broken stub).
	if first.deviceStatus == 0 || first.alertsOverview == 0 || first.licensesOverview == 0 ||
		first.licensesList == 0 || first.firmwareUpgrades == 0 || first.apiRequestsByInt == 0 ||
		first.applianceUplinks == 0 {
		t.Fatalf("first-call hits contain zeros (stub not exercised); first=%+v", first)
	}
}
