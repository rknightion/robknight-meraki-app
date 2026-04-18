package query

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// alertsTTL: the assurance alerts feed is refreshed every ~minute on the
// Meraki backend; 30s keeps the UI near-live while soaking up panel
// auto-refresh bursts. Matches sensor latest/summary TTLs for consistency.
const alertsTTL = 30 * time.Second

// handleAlerts turns the assurance alerts list into a single table-shaped
// frame suitable for the Alerts scene's table + drilldown. Filters on the
// MerakiQuery are pushed down to Meraki via AlertsOptions — we intentionally
// avoid client-side filterByValue chains (see G.20 in todos.txt).
//
// Filter plumbing: the first entry of q.Metrics is reused as the severity
// filter until a dedicated `Severity` field lands on MerakiQuery. The
// coordinator is expected to add that field in a follow-up; the frontend
// scene builder should populate `metrics: [severity]` for now and switch to
// `severity` once wired. The choice of q.Metrics[0] (rather than the whole
// slice) is deliberate — the Meraki API accepts a single severity value per
// request, not a repeated filter.
func handleAlerts(ctx context.Context, client *meraki.Client, q MerakiQuery, tr TimeRange, opts Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("alerts: orgId is required")
	}

	reqOpts := meraki.AlertsOptions{
		Severity:  firstNonEmpty(q.Metrics),
		Serials:   q.Serials,
		SortOrder: "descending",
	}
	// Single-network filter — Meraki's /assurance/alerts accepts only one
	// networkId per request. We pick the first entry from NetworkIDs to keep
	// the MerakiQuery wire shape consistent with other kinds (e.g. sensor
	// history) that do support the full slice.
	if len(q.NetworkIDs) > 0 {
		reqOpts.NetworkID = q.NetworkIDs[0]
	}
	// Status filter — sentinel read from q.Metrics[1] so the alerts table can
	// include resolved + dismissed rows without adding a new field to the
	// MerakiQuery wire shape. Values: "active" | "resolved" | "dismissed" |
	// "all" (default). Empty / unknown sentinels fall through to "all" so
	// operators see every alert in the window, matching the Meraki dashboard.
	if statusSentinel := alertsStatusSentinel(q.Metrics); statusSentinel != "all" {
		switch statusSentinel {
		case "active":
			t := true
			reqOpts.Active = &t
		case "resolved":
			t := true
			reqOpts.Resolved = &t
		case "dismissed":
			t := true
			reqOpts.Dismissed = &t
		}
	}

	if from := toRFCTime(tr.From); !from.IsZero() {
		reqOpts.TSStart = &from
	}
	if to := toRFCTime(tr.To); !to.IsZero() {
		reqOpts.TSEnd = &to
	}

	alerts, err := client.GetOrganizationAssuranceAlerts(ctx, q.OrgID, reqOpts, alertsTTL)
	if err != nil {
		return nil, err
	}

	var (
		occurred      []time.Time
		severity      []string
		category      []string
		alertType     []string
		networkID     []string
		networkName   []string
		deviceSerial  []string
		deviceName    []string
		deviceProduct []string
		titles        []string
		descriptions  []string
		resolved      []bool
		dismissed     []bool
		drilldown     []string
	)
	for _, a := range alerts {
		// Prefer startedAt for timeline purposes — falling back through
		// occurredAt and resolvedAt so rows always have a usable timestamp.
		ts := time.Time{}
		switch {
		case a.StartedAt != nil && !a.StartedAt.IsZero():
			ts = a.StartedAt.UTC()
		case a.OccurredAt != nil && !a.OccurredAt.IsZero():
			ts = a.OccurredAt.UTC()
		case a.DismissedAt != nil && !a.DismissedAt.IsZero():
			ts = a.DismissedAt.UTC()
		case a.ResolvedAt != nil && !a.ResolvedAt.IsZero():
			ts = a.ResolvedAt.UTC()
		}
		occurred = append(occurred, ts)
		severity = append(severity, a.Severity)
		category = append(category, a.CategoryType)
		alertType = append(alertType, a.AlertType)

		var nID, nName string
		if a.Network != nil {
			nID = a.Network.ID
			nName = a.Network.Name
		}
		networkID = append(networkID, nID)
		networkName = append(networkName, nName)

		var dSerial, dName, dProduct string
		if a.Device != nil {
			dSerial = a.Device.Serial
			dName = a.Device.Name
			dProduct = a.Device.ProductType
		}
		deviceSerial = append(deviceSerial, dSerial)
		deviceName = append(deviceName, dName)
		deviceProduct = append(deviceProduct, dProduct)

		titles = append(titles, a.Title)
		descriptions = append(descriptions, a.Description)
		resolved = append(resolved, a.ResolvedAt != nil && !a.ResolvedAt.IsZero())
		dismissed = append(dismissed, a.DismissedAt != nil && !a.DismissedAt.IsZero())
		// Cross-family drilldown (§1.12): compute per-row based on the device's
		// productType so a single alerts table that spans MR/MS/MX/MV/MG/MT still
		// routes each row to the right detail page. Network-wide alerts with no
		// device attached get the empty-string URL which the viz renders as an
		// inactive link.
		drilldown = append(drilldown, deviceDrilldownURL(opts.PluginPathPrefix, dProduct, dSerial))
	}

	return []*data.Frame{
		data.NewFrame("alerts",
			data.NewField("occurredAt", nil, occurred),
			data.NewField("severity", nil, severity),
			data.NewField("category", nil, category),
			data.NewField("alertType", nil, alertType),
			data.NewField("network_id", nil, networkID),
			data.NewField("network_name", nil, networkName),
			data.NewField("device_serial", nil, deviceSerial),
			data.NewField("device_name", nil, deviceName),
			data.NewField("device_productType", nil, deviceProduct),
			data.NewField("title", nil, titles),
			data.NewField("description", nil, descriptions),
			data.NewField("resolved", nil, resolved),
			data.NewField("dismissed", nil, dismissed),
			data.NewField("drilldownUrl", nil, drilldown),
		),
	}, nil
}

// handleAlertsOverview aggregates the overview endpoint into a single-row
// wide frame shaped (critical, warning, informational, total) — mirroring
// sensor_summary.go's KPI frame shape. Server-side aggregation here avoids
// the filterByValue+reduce transform chain that has silently mis-reduced in
// the past (G.20 in todos.txt).
//
// Severity accounting: Meraki's /overview/byType response returns a flat
// `items` array by alert type (no severity breakdown). Its sibling /overview
// endpoint returns `counts.bySeverity`. The wrapper decodes both shapes; we
// prefer `counts.bySeverity` when populated (more accurate) and fall back to
// summing `items[].count` into a generic `total` when only the per-type
// shape is returned.
func handleAlertsOverview(ctx context.Context, client *meraki.Client, q MerakiQuery, tr TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("alertsOverview: orgId is required")
	}

	opts := meraki.AlertsOptions{}
	if len(q.NetworkIDs) > 0 {
		opts.NetworkID = q.NetworkIDs[0]
	}
	opts.Serials = q.Serials
	// Status sentinel matches handleAlerts — default "all" so KPIs reflect
	// the full feed rather than silently hiding resolved / dismissed rows.
	if statusSentinel := alertsStatusSentinel(q.Metrics); statusSentinel != "all" {
		switch statusSentinel {
		case "active":
			t := true
			opts.Active = &t
		case "resolved":
			t := true
			opts.Resolved = &t
		case "dismissed":
			t := true
			opts.Dismissed = &t
		}
	}
	if from := toRFCTime(tr.From); !from.IsZero() {
		opts.TSStart = &from
	}
	if to := toRFCTime(tr.To); !to.IsZero() {
		opts.TSEnd = &to
	}

	overview, err := client.GetOrganizationAssuranceAlertsOverviewByType(ctx, q.OrgID, opts, alertsTTL)
	if err != nil {
		return nil, err
	}

	var critical, warning, informational, total int64
	if overview != nil {
		if overview.Counts != nil {
			total = overview.Counts.Total
			for _, sc := range overview.Counts.BySeverity {
				switch strings.ToLower(sc.Type) {
				case "critical":
					critical += sc.Count
				case "warning":
					warning += sc.Count
				case "informational", "info":
					informational += sc.Count
				}
			}
			// `total` sometimes 0 on partial responses — reconstruct from
			// the severity breakdown so the KPI row always renders a
			// meaningful number.
			if total == 0 {
				total = critical + warning + informational
			}
		}
		// When the byType shape is what we got back, we can only reliably
		// fill `total`; severity columns stay 0. The UI surfaces this as a
		// single-number KPI on the alerts page which is still useful.
		if overview.Counts == nil {
			for _, it := range overview.Items {
				total += it.Count
			}
		}
	}

	return []*data.Frame{
		data.NewFrame("alerts_overview",
			data.NewField("critical", nil, []int64{critical}),
			data.NewField("warning", nil, []int64{warning}),
			data.NewField("informational", nil, []int64{informational}),
			data.NewField("total", nil, []int64{total}),
		),
	}, nil
}

// firstNonEmpty returns the first non-empty string in the slice, or "".
// Used to reuse the multi-valued q.Metrics field as a single-valued severity
// filter until MerakiQuery grows a dedicated field.
func firstNonEmpty(ss []string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// alertsStatusSentinel picks the alerts status filter out of q.Metrics[1:].
// q.Metrics[0] is the severity; q.Metrics[1] (when present) is one of
// "active" | "resolved" | "dismissed" | "all". Default is "all" so the feed
// matches the Meraki dashboard's default view — operators saw resolved /
// dismissed rows missing when the backend hard-coded active=true.
func alertsStatusSentinel(metrics []string) string {
	if len(metrics) < 2 {
		return "all"
	}
	switch strings.ToLower(metrics[1]) {
	case "active", "resolved", "dismissed":
		return strings.ToLower(metrics[1])
	}
	return "all"
}
