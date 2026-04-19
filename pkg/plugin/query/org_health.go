// Package query — §4.4.4-E Org Health Overview handler.
//
// orgHealthSummary fans out to six existing per-kind handlers in parallel
// (deviceStatusOverview, alertsOverview, licensesList, firmwarePending,
// apiRequestsByInterval, applianceUplinkStatuses) and reduces each result
// into a single KPI. The emitted frame is the canonical §G.20 wide shape:
// one row, one field per KPI.
//
// Implementation choice (§4.4.4-E.P step 4): we call the existing handlers
// internally via `runOne` rather than reaching past the dispatcher to
// meraki.Client directly. Rationale — downstream handlers already own the
// data-shaping + frame-emission contract (device-status buckets, alerts
// severity accounting, firmware byDevice filtering, etc.); duplicating any
// of that here would mean two places to fix when Meraki shifts a payload
// shape. The cost is a tiny amount of frame-parsing re-work to pull scalars
// back out of the emitted frames, which is cheap and explicit.
//
// Caching: every downstream handler is already cached at the meraki.Client
// layer with its own TTL + singleflight dedup. A second concurrent Home
// load therefore issues zero HTTP calls; the test
// TestHandle_OrgHealthSummary_CachedSecondCallHitsNoBackend pins this.
package query

import (
	"context"
	"fmt"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"
	"golang.org/x/sync/errgroup"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// runOneFunc indirects the call to runOne so the handlers-map initializer
// cycle check doesn't flag handleOrgHealthSummary → runOne → handlers.
// Assignment happens in init() below; call sites use this var so the
// compile-time initialization order isn't tainted by a direct reference.
var runOneFunc func(ctx context.Context, client *meraki.Client, q MerakiQuery, tr TimeRange, opts Options) ([]*data.Frame, error)

func init() {
	runOneFunc = runOne
}

// orgHealthSummaryTTL — placeholder constant for the documented 30s TTL.
// Currently unused because every downstream handler applies its own cache
// TTL at the meraki.Client layer; left declared so a future caching wrapper
// around this handler has a single constant to key off.
const orgHealthSummaryTTL = 30 * time.Second //nolint:unused // reserved for a future caching wrapper around the aggregated Home tile handler

// orgHealthApiErrorWindow is the fixed lookback we use to compute
// apiErrorPct regardless of the panel time range. The Home tile must be
// stable — it should not swing wildly when a user switches the dashboard
// time picker from "last 1h" to "last 30d". Per the §4.4.4-E spec.
const orgHealthApiErrorWindow = 1 * time.Hour

// orgHealthLicensesExp30dThreshold / orgHealthLicensesExp7dThreshold are
// the day cutoffs for "licenses expiring soon" KPI buckets per the spec.
const (
	orgHealthLicensesExp30dThreshold = 30
	orgHealthLicensesExp7dThreshold  = 7
)

// handleOrgHealthSummary is the §4.4.4-E Org Health handler. It fans out six
// downstream handlers in parallel via errgroup, reduces each emitted frame
// into a single KPI, and returns a single wide frame shaped:
//
//	devicesOnline | devicesOffline | alertsCritical | alertsWarning |
//	licensesExp30d | licensesExp7d | firmwareDrift | apiErrorPct |
//	uplinksDown
//
// Partial-failure semantics: a single downstream failure is captured on the
// returned frame as a data.Notice listing which KPI(s) could not be
// computed. The other KPIs still render so the Home tile degrades
// gracefully. Empty results (e.g. firmwarePending on an MX-only org) are
// treated as 0 counts — this is correct: "no devices pending upgrade" is
// meaningfully different from "handler errored".
func handleOrgHealthSummary(ctx context.Context, client *meraki.Client, q MerakiQuery, tr TimeRange, opts Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("orgHealthSummary: orgId is required")
	}

	now := time.Now().UTC()
	// apiRequestsByInterval needs a non-zero time range; we hard-code 1h per
	// the spec so the KPI is stable regardless of the panel range. The
	// underlying endpoint honours KnownEndpointRanges and quantizes to the
	// matching resolution.
	apiTR := TimeRange{
		From: now.Add(-orgHealthApiErrorWindow).UnixMilli(),
		To:   now.UnixMilli(),
	}

	// kpiResult captures the scalar outputs plus per-KPI error state so we
	// can surface partial-failure notices without failing the whole handler.
	type kpiResult struct {
		// device status counts
		devicesOnline, devicesOffline int64
		deviceErr                     error
		// alerts severity counts
		alertsCritical, alertsWarning int64
		alertsErr                     error
		// licenses expiring soon
		licensesExp30d, licensesExp7d int64
		licensesErr                   error
		// firmware drift (proxy = current pending count)
		firmwareDrift int64
		firmwareErr   error
		// api error percentage (429 / total over last 1h)
		apiErrorPct float64
		apiErr      error
		// uplinks currently failed
		uplinksDown int64
		uplinkErr   error
	}
	var res kpiResult

	g, gctx := errgroup.WithContext(ctx)

	// 1. Device status counts ------------------------------------------------
	g.Go(func() error {
		frames, err := runOneFunc(gctx, client, MerakiQuery{
			Kind:         KindDeviceStatusOverview,
			OrgID:        q.OrgID,
			NetworkIDs:   q.NetworkIDs,
			ProductTypes: q.ProductTypes,
		}, tr, opts)
		if err != nil {
			res.deviceErr = err
			return nil
		}
		// deviceStatusOverview emits one frame shaped (status, count). Scan
		// for the online + offline rows; alerting/dormant are intentionally
		// not surfaced as Home KPIs (they're visible on the full page).
		if len(frames) > 0 {
			statuses := stringColumn(frames[0], "status")
			counts := int64Column(frames[0], "count")
			for i, s := range statuses {
				if i >= len(counts) {
					break
				}
				switch s {
				case "online":
					res.devicesOnline = counts[i]
				case "offline":
					res.devicesOffline = counts[i]
				}
			}
		}
		return nil
	})

	// 2. Alerts severity counts ----------------------------------------------
	g.Go(func() error {
		frames, err := runOneFunc(gctx, client, MerakiQuery{
			Kind:       KindAlertsOverview,
			OrgID:      q.OrgID,
			NetworkIDs: q.NetworkIDs,
		}, tr, opts)
		if err != nil {
			res.alertsErr = err
			return nil
		}
		if len(frames) > 0 {
			if vals := int64Column(frames[0], "critical"); len(vals) > 0 {
				res.alertsCritical = vals[0]
			}
			if vals := int64Column(frames[0], "warning"); len(vals) > 0 {
				res.alertsWarning = vals[0]
			}
		}
		return nil
	})

	// 3. Licenses expiring soon ---------------------------------------------
	g.Go(func() error {
		frames, err := runOneFunc(gctx, client, MerakiQuery{
			Kind:  KindLicensesList,
			OrgID: q.OrgID,
		}, tr, opts)
		if err != nil {
			res.licensesErr = err
			return nil
		}
		// licenses_list carries a `daysUntilExpiry` column (-1 sentinel for
		// permanent). Count positive values <= 30 / 7 respectively. Rows
		// with -1 (permanent / unknown) are ignored — they're not expiring.
		// Rows with 0 or negative (already expired) also count toward the
		// 7d bucket because they're even more urgent than "<7 days left".
		if len(frames) > 0 {
			days := int64Column(frames[0], "daysUntilExpiry")
			for _, d := range days {
				if d == -1 {
					continue
				}
				if d <= orgHealthLicensesExp30dThreshold {
					res.licensesExp30d++
				}
				if d <= orgHealthLicensesExp7dThreshold {
					res.licensesExp7d++
				}
			}
		}
		return nil
	})

	// 4. Firmware drift (pending upgrade count) -----------------------------
	g.Go(func() error {
		frames, err := runOneFunc(gctx, client, MerakiQuery{
			Kind:  KindFirmwarePending,
			OrgID: q.OrgID,
		}, tr, opts)
		if err != nil {
			res.firmwareErr = err
			return nil
		}
		// firmwarePending: one row per device with a pending/in-progress
		// upgrade. MX-only orgs get zero rows (Meraki limitation — MS+MR
		// only on this endpoint); treat that as drift=0, which is correct.
		if len(frames) > 0 {
			if rows, _ := frames[0].RowLen(); rows > 0 {
				res.firmwareDrift = int64(rows)
			}
		}
		return nil
	})

	// 5. API error % (429 rate over last 1h) --------------------------------
	g.Go(func() error {
		frames, err := runOneFunc(gctx, client, MerakiQuery{
			Kind:  KindApiRequestsByInterval,
			OrgID: q.OrgID,
		}, apiTR, opts)
		if err != nil {
			res.apiErr = err
			return nil
		}
		// apiRequestsByInterval emits one frame per class seen (2xx, 4xx,
		// 429, 5xx). Sum the value column per frame and compute 429/total.
		var total, tooMany int64
		for _, f := range frames {
			valueField, _ := f.FieldByName("value")
			if valueField == nil {
				continue
			}
			class := valueField.Labels["class"]
			vals := int64FieldValues(valueField)
			var sum int64
			for _, v := range vals {
				sum += v
			}
			total += sum
			if class == "429" {
				tooMany = sum
			}
		}
		if total > 0 {
			res.apiErrorPct = 100.0 * float64(tooMany) / float64(total)
		}
		return nil
	})

	// 6. Appliance uplinks currently failed ---------------------------------
	g.Go(func() error {
		frames, err := runOneFunc(gctx, client, MerakiQuery{
			Kind:       KindApplianceUplinkStatuses,
			OrgID:      q.OrgID,
			NetworkIDs: q.NetworkIDs,
		}, tr, opts)
		if err != nil {
			res.uplinkErr = err
			return nil
		}
		if len(frames) > 0 {
			statuses := stringColumn(frames[0], "status")
			for _, s := range statuses {
				if s == "failed" {
					res.uplinksDown++
				}
			}
		}
		return nil
	})

	// errgroup.Wait returns nil because every goroutine swallows its error
	// into the result struct. We intentionally don't propagate individual
	// failures as a handler-level error — the KPI row should render even
	// when one downstream is unavailable.
	_ = g.Wait()

	frame := data.NewFrame("org_health_summary",
		data.NewField("devicesOnline", nil, []int64{res.devicesOnline}),
		data.NewField("devicesOffline", nil, []int64{res.devicesOffline}),
		data.NewField("alertsCritical", nil, []int64{res.alertsCritical}),
		data.NewField("alertsWarning", nil, []int64{res.alertsWarning}),
		data.NewField("licensesExp30d", nil, []int64{res.licensesExp30d}),
		data.NewField("licensesExp7d", nil, []int64{res.licensesExp7d}),
		data.NewField("firmwareDrift", nil, []int64{res.firmwareDrift}),
		data.NewField("apiErrorPct", nil, []float64{res.apiErrorPct}),
		data.NewField("uplinksDown", nil, []int64{res.uplinksDown}),
	)

	// Per-KPI partial-failure notices so the operator can see which tile is
	// stale without losing the rest of the row.
	failures := map[string]error{
		"devicesOnline/devicesOffline": res.deviceErr,
		"alertsCritical/alertsWarning": res.alertsErr,
		"licensesExp30d/licensesExp7d": res.licensesErr,
		"firmwareDrift":                res.firmwareErr,
		"apiErrorPct":                  res.apiErr,
		"uplinksDown":                  res.uplinkErr,
	}
	for kpi, err := range failures {
		if err == nil {
			continue
		}
		frame.AppendNotices(data.Notice{
			Severity: data.NoticeSeverityWarning,
			Text:     fmt.Sprintf("%s unavailable: %v", kpi, err),
		})
	}

	return []*data.Frame{frame}, nil
}

// stringColumn returns the string values of a named field, or nil if the
// field is missing or not a string field. Tolerates frames built by
// downstream handlers where the column set is well-known but an older
// Meraki payload might leave some fields empty.
func stringColumn(f *data.Frame, name string) []string {
	if f == nil {
		return nil
	}
	field, _ := f.FieldByName(name)
	if field == nil {
		return nil
	}
	out := make([]string, field.Len())
	for i := 0; i < field.Len(); i++ {
		if v, ok := field.At(i).(string); ok {
			out[i] = v
		}
	}
	return out
}

// int64Column returns the int64 values of a named field. Same tolerance as
// stringColumn.
func int64Column(f *data.Frame, name string) []int64 {
	if f == nil {
		return nil
	}
	field, _ := f.FieldByName(name)
	if field == nil {
		return nil
	}
	return int64FieldValues(field)
}

// int64FieldValues flattens an int64 data.Field to a []int64. Kept separate
// from int64Column so the apiErrorPct path can reuse it after finding the
// value field by label rather than by name.
func int64FieldValues(field *data.Field) []int64 {
	if field == nil {
		return nil
	}
	out := make([]int64, field.Len())
	for i := 0; i < field.Len(); i++ {
		if v, ok := field.At(i).(int64); ok {
			out[i] = v
		}
	}
	return out
}
