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

// insightsServer is a small test harness that dispatches by substring match
// on the incoming request path. Several insights tests need to serve a single
// endpoint; a few (licensesList) need to serve the overview probe AS WELL as
// the list. Keeping the matcher explicit avoids falling through to 404 when
// a caller adds a second probe in the future.
type insightsServer struct {
	routes map[string]http.HandlerFunc
}

func newInsightsServer() *insightsServer {
	return &insightsServer{routes: map[string]http.HandlerFunc{}}
}

func (s *insightsServer) handle(substr string, h http.HandlerFunc) *insightsServer {
	s.routes[substr] = h
	return s
}

func (s *insightsServer) start(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Route longest-match-first so "/licenses/overview" wins over
		// "/licenses" when both are registered. The stdlib net/http mux
		// would do this on prefix-match; we're matching by `Contains`, so
		// sort by descending length to get the same effect.
		best := ""
		for substr := range s.routes {
			if !strings.Contains(r.URL.Path, substr) {
				continue
			}
			if len(substr) > len(best) {
				best = substr
			}
		}
		if best == "" {
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		s.routes[best](w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newInsightsClient builds a meraki.Client pointed at the test server.
func newInsightsClient(t *testing.T, srv *httptest.Server) *meraki.Client {
	t.Helper()
	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}

// runSingleFrame invokes Handle with one query and returns the decoded
// primary frame. Convenience wrapper — every insights test is single-query
// and single-frame (or takes frame 0 anyway), so factoring this out keeps the
// assert bodies focused on the payload.
func runSingleFrame(t *testing.T, client *meraki.Client, q MerakiQuery, tr TimeRange, opts Options) *data.Frame {
	t.Helper()
	resp, err := Handle(context.Background(), client, &QueryRequest{
		Range:         tr,
		MaxDataPoints: tr.MaxDataPoints,
		Queries:       []MerakiQuery{q},
	}, opts)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(resp.Frames) == 0 {
		t.Fatalf("expected at least one frame, got none")
	}
	var frame data.Frame
	if err := json.Unmarshal(resp.Frames[0], &frame); err != nil {
		t.Fatalf("decode frame 0: %v (body=%s)", err, string(resp.Frames[0]))
	}
	return &frame
}

// fieldInt64 looks up a named field and returns the int64 at row 0. Fails
// the test if the field is missing or the cell is the wrong type — callers
// never want to swallow either mistake silently.
func fieldInt64(t *testing.T, f *data.Frame, name string) int64 {
	t.Helper()
	field, _ := f.FieldByName(name)
	if field == nil {
		t.Fatalf("frame missing %q column; fields=%v", name, f.Fields)
	}
	v, _ := field.ConcreteAt(0)
	got, ok := v.(int64)
	if !ok {
		t.Fatalf("%s = %T %v, want int64", name, v, v)
	}
	return got
}

func fieldFloat64(t *testing.T, f *data.Frame, name string) float64 {
	t.Helper()
	field, _ := f.FieldByName(name)
	if field == nil {
		t.Fatalf("frame missing %q column; fields=%v", name, f.Fields)
	}
	v, _ := field.ConcreteAt(0)
	got, ok := v.(float64)
	if !ok {
		t.Fatalf("%s = %T %v, want float64", name, v, v)
	}
	return got
}

func fieldBool(t *testing.T, f *data.Frame, name string) bool {
	t.Helper()
	field, _ := f.FieldByName(name)
	if field == nil {
		t.Fatalf("frame missing %q column; fields=%v", name, f.Fields)
	}
	v, _ := field.ConcreteAt(0)
	got, ok := v.(bool)
	if !ok {
		t.Fatalf("%s = %T %v, want bool", name, v, v)
	}
	return got
}

func fieldTime(t *testing.T, f *data.Frame, name string) time.Time {
	t.Helper()
	field, _ := f.FieldByName(name)
	if field == nil {
		t.Fatalf("frame missing %q column; fields=%v", name, f.Fields)
	}
	v, _ := field.ConcreteAt(0)
	switch tv := v.(type) {
	case time.Time:
		return tv
	case *time.Time:
		if tv == nil {
			return time.Time{}
		}
		return *tv
	default:
		t.Fatalf("%s = %T %v, want time.Time", name, v, v)
		return time.Time{}
	}
}

// TestHandle_LicensesOverview_CoTerm stubs the co-termination response shape
// (status + licensedDeviceCounts + expirationDate) and confirms the wide
// frame reports the summed count, coterm=true, and the parsed expiration.
func TestHandle_LicensesOverview_CoTerm(t *testing.T) {
	const payload = `{"status":"OK","expirationDate":"2027-03-13T00:00:00Z","licensedDeviceCounts":{"MR":12,"MS":8}}`

	srv := newInsightsServer().
		handle("/licenses/overview", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(payload))
		}).start(t)
	client := newInsightsClient(t, srv)

	frame := runSingleFrame(t, client, MerakiQuery{RefID: "A", Kind: KindLicensesOverview, OrgID: "o1"}, TimeRange{}, Options{})

	if got := fieldInt64(t, frame, "total"); got != 20 {
		t.Fatalf("total = %d, want 20", got)
	}
	if got := fieldInt64(t, frame, "active"); got != 20 {
		t.Fatalf("active = %d, want 20 (OK coterm ⇒ active=total)", got)
	}
	if got := fieldBool(t, frame, "coterm"); !got {
		t.Fatalf("coterm = false, want true")
	}
	wantExp := time.Date(2027, 3, 13, 0, 0, 0, 0, time.UTC)
	if got := fieldTime(t, frame, "cotermExpiration"); !got.Equal(wantExp) {
		t.Fatalf("cotermExpiration = %v, want %v", got, wantExp)
	}
}

// TestHandle_LicensesOverview_PerDevice stubs the per-device bucket shape and
// confirms the wide frame reads the counts straight off `states.*.count`.
func TestHandle_LicensesOverview_PerDevice(t *testing.T) {
	const payload = `{"states":{"active":{"count":5},"expired":{"count":1},"expiring":{"count":2},"recentlyQueued":{"count":0},"unused":{"count":0},"unusedActive":{"count":0}}}`

	srv := newInsightsServer().
		handle("/licenses/overview", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(payload))
		}).start(t)
	client := newInsightsClient(t, srv)

	frame := runSingleFrame(t, client, MerakiQuery{RefID: "A", Kind: KindLicensesOverview, OrgID: "o1"}, TimeRange{}, Options{})

	if got := fieldInt64(t, frame, "active"); got != 5 {
		t.Fatalf("active = %d, want 5", got)
	}
	if got := fieldInt64(t, frame, "expired"); got != 1 {
		t.Fatalf("expired = %d, want 1", got)
	}
	if got := fieldInt64(t, frame, "expiring30"); got != 2 {
		t.Fatalf("expiring30 = %d, want 2", got)
	}
	if got := fieldBool(t, frame, "coterm"); got {
		t.Fatalf("coterm = true, want false on per-device org")
	}
}

// TestHandle_LicensesList_FiltersByState confirms q.Metrics[0] is passed to
// the API as the `state` filter and the daysUntilExpiry column is computed.
// The stub mux serves both the overview probe (returning the per-device
// shape so the handler doesn't short-circuit) and the licenses list.
func TestHandle_LicensesList_FiltersByState(t *testing.T) {
	// One license with an expiration 90 days in the future. The activation
	// date is arbitrary — we only assert on days-until-expiration.
	futureExp := time.Now().UTC().Add(90 * 24 * time.Hour)
	listPayload := `[{"id":"L1","licenseType":"ENT","state":"active","deviceSerial":"Q2XX-1234-5678","networkId":"N1","seatCount":1,"activationDate":"2024-01-01T00:00:00Z","expirationDate":"` + futureExp.Format(time.RFC3339) + `"}]`

	var observedState atomic.Value
	observedState.Store("")

	srv := newInsightsServer().
		handle("/licenses/overview", func(w http.ResponseWriter, _ *http.Request) {
			// Per-device shape so the handler proceeds past the probe.
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"states":{"active":{"count":1},"expired":{"count":0},"expiring":{"count":0},"recentlyQueued":{"count":0},"unused":{"count":0},"unusedActive":{"count":0}}}`))
		}).
		handle("/licenses", func(w http.ResponseWriter, r *http.Request) {
			observedState.Store(r.URL.Query().Get("state"))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(listPayload))
		}).start(t)
	client := newInsightsClient(t, srv)

	frame := runSingleFrame(t, client, MerakiQuery{
		RefID:   "A",
		Kind:    KindLicensesList,
		OrgID:   "o1",
		Metrics: []string{"active"},
	}, TimeRange{}, Options{})

	if got := observedState.Load().(string); got != "active" {
		t.Fatalf("state query param = %q, want 'active'", got)
	}
	if field, _ := frame.FieldByName("daysUntilExpiry"); field == nil {
		t.Fatalf("frame missing daysUntilExpiry column; fields=%v", frame.Fields)
	}
	// Row count sanity — one license in the stub.
	rows, _ := frame.RowLen()
	if rows != 1 {
		t.Fatalf("rows = %d, want 1", rows)
	}
	// daysUntilExpiry should be ~90 (allow ±2 for runtime jitter).
	days := fieldInt64(t, frame, "daysUntilExpiry")
	if days < 88 || days > 91 {
		t.Fatalf("daysUntilExpiry = %d, want ~90", days)
	}
}

// TestHandle_LicensesList_CoTermSynthesisesRows verifies the overview probe
// short-circuits /licenses (which 400s on co-term orgs) and synthesises one
// row per licensedDeviceCounts entry with the shared co-term expiration
// threaded through. Panels that key on `seatCount` / `expirationDate` /
// `daysUntilExpiry` still render meaningful data.
func TestHandle_LicensesList_CoTermSynthesisesRows(t *testing.T) {
	srv := newInsightsServer().
		handle("/licenses/overview", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"OK","expirationDate":"2099-03-13T00:00:00Z","licensedDeviceCounts":{"MR":5,"MS":3}}`))
		}).
		handle("/licenses", func(_ http.ResponseWriter, _ *http.Request) {
			t.Error("licenses list endpoint was called despite co-term probe")
		}).start(t)
	client := newInsightsClient(t, srv)

	frame := runSingleFrame(t, client, MerakiQuery{RefID: "A", Kind: KindLicensesList, OrgID: "o1"}, TimeRange{}, Options{})

	rows, _ := frame.RowLen()
	if rows != 2 {
		t.Fatalf("rows = %d, want 2 (one per model in licensedDeviceCounts)", rows)
	}

	// Sorted-key order: MR then MS.
	licenseType, _ := frame.FieldByName("licenseType")
	if got, _ := licenseType.ConcreteAt(0); got != "MR" {
		t.Fatalf("row 0 licenseType = %v, want MR", got)
	}
	if got, _ := licenseType.ConcreteAt(1); got != "MS" {
		t.Fatalf("row 1 licenseType = %v, want MS", got)
	}

	seat, _ := frame.FieldByName("seatCount")
	if got, _ := seat.ConcreteAt(0); got != int64(5) {
		t.Fatalf("row 0 seatCount = %v, want 5", got)
	}
	if got, _ := seat.ConcreteAt(1); got != int64(3) {
		t.Fatalf("row 1 seatCount = %v, want 3", got)
	}

	// Every row shares the same expiration date pulled from the overview.
	wantExp := time.Date(2099, 3, 13, 0, 0, 0, 0, time.UTC)
	if got := fieldTime(t, frame, "expirationDate"); !got.Equal(wantExp) {
		t.Fatalf("row 0 expirationDate = %v, want %v", got, wantExp)
	}

	// Notice still carries "co-termination" so the UI can badge the table.
	if frame.Meta == nil || len(frame.Meta.Notices) == 0 {
		t.Fatalf("expected at least one notice on co-term frame; meta=%+v", frame.Meta)
	}
	if !strings.Contains(frame.Meta.Notices[0].Text, "Co-termination") {
		t.Fatalf("notice text = %q, want 'Co-termination' substring", frame.Meta.Notices[0].Text)
	}
}

// TestHandle_ApiRequestsOverview_BucketsClasses exercises the class-bucketing
// arithmetic: the handler must split 2xx / 4xx / 429 / 5xx independently and
// compute `total` as the sum of every numeric code encountered.
func TestHandle_ApiRequestsOverview_BucketsClasses(t *testing.T) {
	const payload = `{"responseCodeCounts":{"200":100,"404":3,"429":1,"500":2}}`

	srv := newInsightsServer().
		handle("/apiRequests/overview", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(payload))
		}).start(t)
	client := newInsightsClient(t, srv)

	frame := runSingleFrame(t, client, MerakiQuery{RefID: "A", Kind: KindApiRequestsOverview, OrgID: "o1"}, TimeRange{}, Options{})

	wants := map[string]int64{
		"total":          106,
		"success2xx":     100,
		"clientError4xx": 3,
		"tooMany429":     1,
		"serverError5xx": 2,
	}
	for name, want := range wants {
		if got := fieldInt64(t, frame, name); got != want {
			t.Fatalf("%s = %d, want %d", name, got, want)
		}
	}
}

// TestHandle_ApiRequestsByInterval_EmitsPerClassFrames confirms the handler
// emits one frame per class seen in the stubbed data. The stub has 2xx and
// 4xx only; the other classes should be elided from the response so the
// legend stays focused.
func TestHandle_ApiRequestsByInterval_EmitsPerClassFrames(t *testing.T) {
	const payload = `[
	  {"startTs":"2026-04-17T09:00:00Z","endTs":"2026-04-17T10:00:00Z","counts":[{"code":200,"count":50},{"code":404,"count":2}]},
	  {"startTs":"2026-04-17T10:00:00Z","endTs":"2026-04-17T11:00:00Z","counts":[{"code":200,"count":80},{"code":404,"count":5}]}
	]`

	srv := newInsightsServer().
		handle("/apiRequests/overview/responseCodes/byInterval", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(payload))
		}).start(t)
	client := newInsightsClient(t, srv)

	now := time.Now().UTC()
	resp, err := Handle(context.Background(), client, &QueryRequest{
		Range: TimeRange{
			From: now.Add(-6 * time.Hour).UnixMilli(),
			To:   now.UnixMilli(),
		},
		Queries: []MerakiQuery{{RefID: "A", Kind: KindApiRequestsByInterval, OrgID: "o1"}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	// We should have frames for 2xx and 4xx only; 429/5xx aren't present
	// in the stub so they're elided.
	if got := len(resp.Frames); got != 2 {
		t.Fatalf("got %d frames, want 2 (only 2xx and 4xx have samples)", got)
	}

	seen := map[string]bool{}
	for i, raw := range resp.Frames {
		var frame data.Frame
		if err := json.Unmarshal(raw, &frame); err != nil {
			t.Fatalf("decode frame %d: %v", i, err)
		}
		valueField, _ := frame.FieldByName("value")
		if valueField == nil {
			t.Fatalf("frame %d missing value column; fields=%v", i, frame.Fields)
		}
		class := valueField.Labels["class"]
		if class == "" {
			t.Fatalf("frame %d missing class label", i)
		}
		seen[class] = true
	}
	if !seen["2xx"] {
		t.Fatalf("missing class=2xx frame; seen=%v", seen)
	}
	if !seen["4xx"] {
		t.Fatalf("missing class=4xx frame; seen=%v", seen)
	}
}

// TestHandle_ClientsOverview_KPIWideFrame stubs a representative response
// and confirms the handler produces a single-row wide frame with the
// expected numeric fields populated.
func TestHandle_ClientsOverview_KPIWideFrame(t *testing.T) {
	const payload = `{"counts":{"total":42},"usage":{"overall":{"total":1234,"downstream":800,"upstream":434},"average":{"total":29.38,"downstream":19.04,"upstream":10.33}}}`

	srv := newInsightsServer().
		handle("/clients/overview", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(payload))
		}).start(t)
	client := newInsightsClient(t, srv)

	now := time.Now().UTC()
	frame := runSingleFrame(t, client, MerakiQuery{RefID: "A", Kind: KindClientsOverview, OrgID: "o1"}, TimeRange{
		From: now.Add(-24 * time.Hour).UnixMilli(),
		To:   now.UnixMilli(),
	}, Options{})

	if got := fieldInt64(t, frame, "totalClients"); got != 42 {
		t.Fatalf("totalClients = %d, want 42", got)
	}
	if got := fieldFloat64(t, frame, "usageTotalKb"); got != 1234 {
		t.Fatalf("usageTotalKb = %v, want 1234", got)
	}
	if got := fieldFloat64(t, frame, "usageDownstreamKb"); got != 800 {
		t.Fatalf("usageDownstreamKb = %v, want 800", got)
	}
	if got := fieldFloat64(t, frame, "avgUsageTotalKb"); got < 29 || got > 30 {
		t.Fatalf("avgUsageTotalKb = %v, want ~29.38", got)
	}
}

// TestHandle_TopClients_SetsTimespan confirms q.TimespanSeconds threads
// through to the outgoing request's `timespan` query string parameter.
func TestHandle_TopClients_SetsTimespan(t *testing.T) {
	const payload = `[{"name":"laptop","id":"C1","mac":"00:11:22:33:44:55","network":{"id":"N1","name":"Lab"},"usage":{"sent":100.0,"recv":50.0,"total":150.0}}]`

	var observedTimespan atomic.Value
	observedTimespan.Store("")

	srv := newInsightsServer().
		handle("/summary/top/clients/byUsage", func(w http.ResponseWriter, r *http.Request) {
			observedTimespan.Store(r.URL.Query().Get("timespan"))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(payload))
		}).start(t)
	client := newInsightsClient(t, srv)

	_ = runSingleFrame(t, client, MerakiQuery{
		RefID:           "A",
		Kind:            KindTopClients,
		OrgID:           "o1",
		TimespanSeconds: 3600,
	}, TimeRange{}, Options{})

	if got := observedTimespan.Load().(string); got != "3600" {
		t.Fatalf("timespan = %q, want '3600'", got)
	}
}

// TestHandle_TopSwitchesByEnergy_ConvertsJoulesToKwh confirms the joules→kWh
// conversion (divide by 3,600,000). 7.2MJ → exactly 2.0 kWh.
func TestHandle_TopSwitchesByEnergy_ConvertsJoulesToKwh(t *testing.T) {
	const payload = `[{"name":"SW-1","serial":"Q2XX-1111-2222","model":"MS250-48","network":{"id":"N1","name":"Lab"},"usage":{"total":7200000}}]`

	srv := newInsightsServer().
		handle("/summary/top/switches/byEnergyUsage", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(payload))
		}).start(t)
	client := newInsightsClient(t, srv)

	frame := runSingleFrame(t, client, MerakiQuery{RefID: "A", Kind: KindTopSwitchesByEnergy, OrgID: "o1"}, TimeRange{}, Options{})

	if got := fieldFloat64(t, frame, "energyKwh"); got != 2.0 {
		t.Fatalf("energyKwh = %v, want 2.0 (7.2MJ / 3.6M)", got)
	}
}

// TestHandle_LicensesOverview_FallsBackToSubscription verifies the
// subscription-licensing fallback: when /licenses/overview returns a 400
// with a subscription-model error body, the handler falls through to
// /administered/licensing/subscription/subscriptions and synthesises the
// KPI frame from the subscription list.
func TestHandle_LicensesOverview_FallsBackToSubscription(t *testing.T) {
	// Two active subscriptions, one expiring soon (within 30 days), one with
	// a far-future end date. SummariseSeats should report total=30 across
	// both, active=30, expiring30=10 (just the first).
	soonExp := time.Now().UTC().Add(15 * 24 * time.Hour).Format(time.RFC3339)
	farExp := time.Now().UTC().Add(365 * 24 * time.Hour).Format(time.RFC3339)
	subsPayload := `[` +
		`{"subscriptionId":"S1","name":"Corp","status":"active","endDate":"` + soonExp + `","counts":{"seats":{"limit":10,"assigned":10,"available":0}}},` +
		`{"subscriptionId":"S2","name":"Branch","status":"active","endDate":"` + farExp + `","counts":{"seats":{"limit":20,"assigned":18,"available":2}}}` +
		`]`

	srv := newInsightsServer().
		handle("/licenses/overview", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"errors":["This organization uses subscription licensing and does not support this endpoint."]}`))
		}).
		handle("/administered/licensing/subscription/subscriptions", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(subsPayload))
		}).start(t)
	client := newInsightsClient(t, srv)

	frame := runSingleFrame(t, client, MerakiQuery{RefID: "A", Kind: KindLicensesOverview, OrgID: "o1"}, TimeRange{}, Options{})

	if got := fieldInt64(t, frame, "total"); got != 30 {
		t.Fatalf("total = %d, want 30 (10 + 20)", got)
	}
	if got := fieldInt64(t, frame, "active"); got != 30 {
		t.Fatalf("active = %d, want 30", got)
	}
	if got := fieldInt64(t, frame, "expiring30"); got != 10 {
		t.Fatalf("expiring30 = %d, want 10 (only the 15-day subscription)", got)
	}
	if got := fieldBool(t, frame, "coterm"); got {
		t.Fatalf("coterm = true, want false on subscription org")
	}
	// Diagnostic notice should explain this came from the subscription fallback.
	if len(frame.Meta.Notices) == 0 {
		t.Fatalf("expected diagnostic notice on subscription frame")
	}
	found := false
	for _, n := range frame.Meta.Notices {
		if strings.Contains(n.Text, "/administered/licensing/subscription/subscriptions") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected notice mentioning the administered endpoint; got %+v", frame.Meta.Notices)
	}
}

// TestHandle_LicensesList_FallsBackToSubscriptions verifies the per-row
// fallback: /licenses 400 on a subscription-licensed org falls through to
// /administered/licensing/subscription/subscriptions, and each subscription
// becomes one row in the licenses_list frame.
func TestHandle_LicensesList_FallsBackToSubscriptions(t *testing.T) {
	subsPayload := `[` +
		`{"subscriptionId":"S1","name":"Corp","status":"active","type":"enterpriseAgreement","endDate":"2027-03-01T00:00:00Z","counts":{"seats":{"limit":10,"assigned":10,"available":0}}}` +
		`]`
	srv := newInsightsServer().
		handle("/licenses/overview", func(w http.ResponseWriter, _ *http.Request) {
			// Per-device shape so the overview probe inside handleLicensesList
			// doesn't short-circuit on coterm. The list call is what needs to
			// trigger the 400 fallback.
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"states":{"active":{"count":0},"expired":{"count":0},"expiring":{"count":0},"recentlyQueued":{"count":0},"unused":{"count":0},"unusedActive":{"count":0}}}`))
		}).
		handle("/administered/licensing/subscription/subscriptions", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(subsPayload))
		}).
		handle("/licenses", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"errors":["subscription licensing org - use /administered endpoints"]}`))
		}).start(t)
	client := newInsightsClient(t, srv)

	frame := runSingleFrame(t, client, MerakiQuery{RefID: "A", Kind: KindLicensesList, OrgID: "o1"}, TimeRange{}, Options{})

	rows, _ := frame.RowLen()
	if rows != 1 {
		t.Fatalf("rows = %d, want 1 (one subscription)", rows)
	}
	if field, _ := frame.FieldByName("state"); field == nil {
		t.Fatalf("frame missing state column")
	} else if got, _ := field.ConcreteAt(0); got != "active" {
		t.Fatalf("state row 0 = %v, want 'active'", got)
	}
	if field, _ := frame.FieldByName("licenseType"); field != nil {
		if got, _ := field.ConcreteAt(0); got != "enterpriseAgreement" {
			t.Fatalf("licenseType row 0 = %v, want 'enterpriseAgreement'", got)
		}
	}
	if field, _ := frame.FieldByName("seatCount"); field != nil {
		if got, _ := field.ConcreteAt(0); got != int64(10) {
			t.Fatalf("seatCount row 0 = %v, want 10", got)
		}
	}
}
