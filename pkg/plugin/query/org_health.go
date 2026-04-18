// Package query — §4.4.4-E Org Health Overview handler.
//
// orgHealthSummary emits a single-row wide KPI frame summarising org-wide
// health across six downstream handlers (deviceStatusOverview,
// alertsOverview, licensesList, firmwarePending, apiRequestsByInterval,
// applianceUplinkStatuses). It backs the §4.4.5 Home merge.
//
// This file is the commit-1 stub — enum + const + empty handler. The
// full fan-out implementation + test lands in commit 2.
package query

import (
	"context"
	"fmt"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// orgHealthSummaryTTL documents the intended cache TTL for the KPI row.
// The underlying handlers each cache at their own TTL via meraki.Client, so
// this constant is advisory today; wired into a caching wrapper when the
// second-call-is-free guarantee needs to be enforced at this layer.
const orgHealthSummaryTTL = 30 * time.Second

// handleOrgHealthSummary — stub. Full implementation arrives in commit 2
// alongside the cache-behaviour test.
func handleOrgHealthSummary(_ context.Context, _ *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("orgHealthSummary: orgId is required")
	}
	// Empty frame with the canonical 9-field shape so downstream consumers
	// can bind columns even before the fan-out logic lands.
	return []*data.Frame{
		data.NewFrame("org_health_summary",
			data.NewField("devicesOnline", nil, []int64{0}),
			data.NewField("devicesOffline", nil, []int64{0}),
			data.NewField("alertsCritical", nil, []int64{0}),
			data.NewField("alertsWarning", nil, []int64{0}),
			data.NewField("licensesExp30d", nil, []int64{0}),
			data.NewField("licensesExp7d", nil, []int64{0}),
			data.NewField("firmwareDrift", nil, []int64{0}),
			data.NewField("apiErrorPct", nil, []float64{0}),
			data.NewField("uplinksDown", nil, []int64{0}),
		),
	}, nil
}
