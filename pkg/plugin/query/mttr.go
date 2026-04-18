package query

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// alertsMttrSummaryTTL: MTTR is recomputed from the assurance alerts feed, so
// the TTL tracks alertsTTL (30s) plus a cushion — we don't need sub-minute
// freshness on a moving average.
const alertsMttrSummaryTTL = 1 * time.Minute

// handleAlertsMttrSummary computes mean / p50 / p95 of (resolvedAt - startedAt)
// across resolved alerts in the panel time range, plus simple resolved / open
// counts. Emitted as a single-row wide frame matching the §G.20 KPI pattern
// shared by sensorAlertSummary and alertsOverview — downstream panels render
// each field as its own stat tile.
//
// Frame shape (one row):
//
//	mttrMeanSeconds | mttrP50Seconds | mttrP95Seconds | resolvedCount | openCount
//
// Open alerts (resolvedAt absent) are counted but contribute nothing to the
// MTTR aggregates — including their "so far" duration would skew the statistic
// and make "fewer incidents" look worse than "more quickly-resolved incidents".
// p50 / p95 use the nearest-rank method on a sorted slice; no dependencies
// beyond stdlib sort.
func handleAlertsMttrSummary(ctx context.Context, client *meraki.Client, q MerakiQuery, tr TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("alertsMttrSummary: orgId is required")
	}

	// Pull the superset of alerts; we need resolved + open so the ratio column
	// is meaningful on its own. handleAlerts' severity/status sentinel overloads
	// are unused here — the summary is a time-window aggregation, not a filter.
	reqOpts := meraki.AlertsOptions{
		SortOrder: "descending",
	}
	if len(q.NetworkIDs) > 0 {
		reqOpts.NetworkID = q.NetworkIDs[0]
	}
	reqOpts.Serials = q.Serials
	if from := toRFCTime(tr.From); !from.IsZero() {
		reqOpts.TSStart = &from
	}
	if to := toRFCTime(tr.To); !to.IsZero() {
		reqOpts.TSEnd = &to
	}

	alerts, err := client.GetOrganizationAssuranceAlerts(ctx, q.OrgID, reqOpts, alertsMttrSummaryTTL)
	if err != nil {
		return nil, err
	}

	var (
		durations     []float64
		resolvedCount int64
		openCount     int64
	)
	for _, a := range alerts {
		// Open = no resolvedAt. DismissedAt is a different state from resolved
		// (operator acknowledged, but underlying condition wasn't fixed) and
		// is intentionally not treated as "resolved" here.
		if a.ResolvedAt == nil || a.ResolvedAt.IsZero() {
			openCount++
			continue
		}
		// A resolved alert without a startedAt is degenerate (shouldn't happen
		// in v1 responses but we've seen partial payloads) — count it as
		// resolved but skip the duration sample.
		if a.StartedAt == nil || a.StartedAt.IsZero() {
			resolvedCount++
			continue
		}
		d := a.ResolvedAt.Sub(*a.StartedAt).Seconds()
		if d < 0 {
			// Out-of-order timestamps — skip rather than pollute the histogram.
			resolvedCount++
			continue
		}
		durations = append(durations, d)
		resolvedCount++
	}

	mean, p50, p95 := mttrAggregates(durations)

	return []*data.Frame{
		data.NewFrame("alerts_mttr_summary",
			data.NewField("mttrMeanSeconds", nil, []float64{mean}),
			data.NewField("mttrP50Seconds", nil, []float64{p50}),
			data.NewField("mttrP95Seconds", nil, []float64{p95}),
			data.NewField("resolvedCount", nil, []int64{resolvedCount}),
			data.NewField("openCount", nil, []int64{openCount}),
		),
	}, nil
}

// mttrAggregates returns (mean, p50, p95) for the input durations. Empty input
// yields zeroes so the handler always emits a valid single-row frame — stat
// panels render 0 as "no data" with a display override, and an empty frame
// suppresses the panel entirely which is a worse UX.
//
// Percentiles use the nearest-rank method: p = values[ceil(p*N/100) - 1].
// Simple, correct for small N, no interpolation needed for a KPI tile.
func mttrAggregates(durations []float64) (mean, p50, p95 float64) {
	n := len(durations)
	if n == 0 {
		return 0, 0, 0
	}
	sorted := make([]float64, n)
	copy(sorted, durations)
	sort.Float64s(sorted)

	var sum float64
	for _, v := range sorted {
		sum += v
	}
	mean = sum / float64(n)
	p50 = nearestRank(sorted, 50)
	p95 = nearestRank(sorted, 95)
	return mean, p50, p95
}

func nearestRank(sorted []float64, pct int) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	// ceil(pct*n/100) using integer math; 1-indexed rank then shifted down.
	rank := (pct*n + 99) / 100
	if rank < 1 {
		rank = 1
	}
	if rank > n {
		rank = n
	}
	return sorted[rank-1]
}
