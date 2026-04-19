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
		Severity:  alertsSeverity(q.Metrics),
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
	// Status filter — sentinel read from q.Metrics[1]. Values:
	//   "active" | "resolved" | "dismissed" | "all"
	// Default is "active" (currently firing). See alertsStatusSentinel.
	sentinel := alertsStatusSentinel(q)
	applyAlertsStatus(&reqOpts, sentinel)

	// Only apply the picker window for EXPLICITLY time-scoped requests
	// (resolved or dismissed). Meraki's tsStart/tsEnd filter on
	// `alert.startedAt`, so narrowing the window hides long-running active
	// alerts AND recently-resolved alerts whose `startedAt` is outside the
	// window — that was the "empty table despite KPI=1" bug users reported
	// twice: 2026-04-18 (active) and 2026-04-19 (all, when applied here
	// dropped 36 resolved-in-window alerts that started earlier).
	//
	// - "active": no time filter — users want live state, not "started in
	//   window".
	// - "all":    no time filter — users want the full feed clearly
	//   marked by status (red/green/grey).
	// - "resolved" / "dismissed": apply time filter — explicit historical
	//   scoping is still useful for audit-style views that DO want
	//   "incidents that started in this period".
	if sentinel == "resolved" || sentinel == "dismissed" {
		if from := toRFCTime(tr.From); !from.IsZero() {
			reqOpts.TSStart = &from
		}
		if to := toRFCTime(tr.To); !to.IsZero() {
			reqOpts.TSEnd = &to
		}
	}

	alerts, err := client.GetOrganizationAssuranceAlerts(ctx, q.OrgID, reqOpts, alertsTTL)
	if err != nil {
		return nil, err
	}

	var (
		occurred      []time.Time
		severity      []string
		statusCol     []string
		category      []string
		alertType     []string
		networkID     []string
		networkName   []string
		deviceSerial  []string
		deviceName    []string
		deviceProduct []string
		titles        []string
		descriptions  []string
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

		// Human-readable lifecycle status. Dismissed overrides resolved when
		// both are present (Meraki occasionally marks a post-resolution
		// dismissal); the column is read by operators far more often than the
		// old true/false resolved+dismissed pair so coalescing here is the
		// least-surprising presentation.
		status := "active"
		switch {
		case a.DismissedAt != nil && !a.DismissedAt.IsZero():
			status = "dismissed"
		case a.ResolvedAt != nil && !a.ResolvedAt.IsZero():
			status = "resolved"
		}
		statusCol = append(statusCol, status)

		var nID, nName string
		if a.Network != nil {
			nID = a.Network.ID
			nName = a.Network.Name
		}
		networkID = append(networkID, nID)
		networkName = append(networkName, nName)

		var dSerial, dName, dProduct string
		if d := a.PrimaryDevice(); d != nil {
			dSerial = d.Serial
			dName = d.Name
			dProduct = d.ProductType
		}
		deviceSerial = append(deviceSerial, dSerial)
		deviceName = append(deviceName, dName)
		deviceProduct = append(deviceProduct, dProduct)

		titles = append(titles, a.Title)
		descriptions = append(descriptions, a.Description)
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
			data.NewField("status", nil, statusCol),
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
// Endpoint choice — /overview, NOT /overview/byType. The byType response is
// a flat `items` array keyed by alert type with NO top-level `counts`
// aggregate; summing items[].count over-counts (one row per (type,severity)
// pair). The /overview sibling returns {counts:{total, bySeverity:[...]}}
// which is exactly the shape we need.
//
// Default sentinel is "active" (currently firing), no time filter — KPI
// tiles are a live snapshot, not a "alerts that started in window" view.
// Callers that want the historical rollup pass metrics:['…','all'] and
// supply a real time range; we honour tsStart/tsEnd only in that case.
func handleAlertsOverview(ctx context.Context, client *meraki.Client, q MerakiQuery, tr TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("alertsOverview: orgId is required")
	}

	opts := meraki.AlertsOptions{}
	if len(q.NetworkIDs) > 0 {
		opts.NetworkID = q.NetworkIDs[0]
	}
	opts.Serials = q.Serials
	sentinel := alertsStatusSentinel(q)
	applyAlertsStatus(&opts, sentinel)
	// Apply the picker window only for explicit "resolved" / "dismissed"
	// views (see handleAlerts for the full rationale — same semantics).
	if sentinel == "resolved" || sentinel == "dismissed" {
		if from := toRFCTime(tr.From); !from.IsZero() {
			opts.TSStart = &from
		}
		if to := toRFCTime(tr.To); !to.IsZero() {
			opts.TSEnd = &to
		}
	}

	overview, err := client.GetOrganizationAssuranceAlertsOverview(ctx, q.OrgID, opts, alertsTTL)
	if err != nil {
		return nil, err
	}

	var critical, warning, informational, total int64
	if overview != nil && overview.Counts != nil {
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
//
// NOTE: do NOT call this on q.Metrics from alerts handlers — the alerts
// kind overloads q.Metrics[0] as severity and q.Metrics[1] as the status
// sentinel, so firstNonEmpty over the full slice would pick the sentinel
// ("all", "resolved", …) as the severity value and Meraki rejects those
// with HTTP 500 ("severity" must be one of critical/warning/informational).
// Use `alertsSeverity` below instead for alerts kinds.
func firstNonEmpty(ss []string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// alertsSeverity extracts the severity filter from q.Metrics, reading
// ONLY the first slot. Returns "" when empty or the slot holds only the
// All-sentinel. Critical: unlike firstNonEmpty, this never peeks at
// q.Metrics[1:] because that slot carries the status sentinel and
// Meraki 500s on `severity=all` / `severity=resolved` / etc.
func alertsSeverity(metrics []string) string {
	if len(metrics) == 0 {
		return ""
	}
	return metrics[0]
}

// alertsStatusSentinel picks the alerts status filter from q.AlertStatus,
// falling back to q.Metrics[1] for legacy callers that still use the
// positional overload (kept for wire-format compatibility until every
// call site migrates). Values: "active" | "resolved" | "dismissed" |
// "all". Default "active" — see the Alerts page rationale in handleAlerts.
//
// Historical note: the positional encoding (metrics[0]=severity,
// metrics[1]=status) broke when the frontend's CSV-interpolation dropped
// the empty $severity placeholder and shifted `all` into metrics[0] →
// Meraki rejected `severity=all` with HTTP 500. AlertStatus is the
// replacement field.
func alertsStatusSentinel(q MerakiQuery) string {
	if q.AlertStatus != "" {
		switch strings.ToLower(q.AlertStatus) {
		case "active", "resolved", "dismissed", "all":
			return strings.ToLower(q.AlertStatus)
		}
	}
	if len(q.Metrics) >= 2 {
		switch strings.ToLower(q.Metrics[1]) {
		case "active", "resolved", "dismissed", "all":
			return strings.ToLower(q.Metrics[1])
		}
	}
	return "active"
}

// applyAlertsStatus sets Active/Resolved/Dismissed on opts based on the
// sentinel. The "all" case must set every boolean explicitly because Meraki's
// default when nothing is sent is active=true, which silently hides resolved
// + dismissed rows (todos.txt §G.* — discovered when the org detail Alerts
// tab rendered empty despite active alerts existing).
func applyAlertsStatus(opts *meraki.AlertsOptions, sentinel string) {
	t := true
	switch sentinel {
	case "active":
		opts.Active = &t
	case "resolved":
		opts.Resolved = &t
	case "dismissed":
		opts.Dismissed = &t
	default: // "all"
		opts.Active = &t
		opts.Resolved = &t
		opts.Dismissed = &t
	}
}

// ---------------------------------------------------------------------------
// §3.4 — Alerts overview byNetwork + historical
// ---------------------------------------------------------------------------

// handleAlertsOverviewByNetwork emits a flat table frame with one row per
// network showing severity counts. Snapshot (no time dimension); 30s TTL.
func handleAlertsOverviewByNetwork(ctx context.Context, client *meraki.Client, q MerakiQuery, tr TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("alertsOverviewByNetwork: orgId is required")
	}

	opts := meraki.AlertsOptions{}
	if len(q.NetworkIDs) > 0 {
		opts.NetworkID = q.NetworkIDs[0]
	}
	// Same default-active / skip-time-filter contract as handleAlerts so
	// the "alerts by network" table shows currently-firing counts out of
	// the box and doesn't hide active alerts that started before the
	// picker window.
	sentinel := alertsStatusSentinel(q)
	applyAlertsStatus(&opts, sentinel)
	// See handleAlerts for the "why skip time filter" rationale.
	if sentinel == "resolved" || sentinel == "dismissed" {
		if from := toRFCTime(tr.From); !from.IsZero() {
			opts.TSStart = &from
		}
		if to := toRFCTime(tr.To); !to.IsZero() {
			opts.TSEnd = &to
		}
	}

	rows, err := client.GetOrganizationAssuranceAlertsOverviewByNetwork(ctx, q.OrgID, opts, alertsTTL)
	if err != nil {
		return nil, err
	}

	var (
		networkIDs    []string
		networkNames  []string
		criticals     []int64
		warnings      []int64
		informationals []int64
		totals        []int64
	)
	for _, r := range rows {
		networkIDs = append(networkIDs, r.NetworkID)
		networkNames = append(networkNames, r.NetworkName)
		criticals = append(criticals, r.Critical)
		warnings = append(warnings, r.Warning)
		informationals = append(informationals, r.Informational)
		totals = append(totals, r.Total)
	}

	return []*data.Frame{
		data.NewFrame("alerts_overview_by_network",
			data.NewField("networkId", nil, networkIDs),
			data.NewField("networkName", nil, networkNames),
			data.NewField("critical", nil, criticals),
			data.NewField("warning", nil, warnings),
			data.NewField("informational", nil, informationals),
			data.NewField("total", nil, totals),
		),
	}, nil
}

// handleAlertsOverviewHistorical emits one frame per severity bucket
// (critical / warning / informational) as a native timeseries so panels can
// stack severities. Labels: {"severity": "<name>"}.
func handleAlertsOverviewHistorical(ctx context.Context, client *meraki.Client, q MerakiQuery, tr TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("alertsOverviewHistorical: orgId is required")
	}

	etr, ok := meraki.KnownEndpointRanges["organizations/{organizationId}/assurance/alerts/overview/historical"]
	if !ok {
		return nil, fmt.Errorf("alertsOverviewHistorical: missing KnownEndpointRanges entry")
	}

	from := toRFCTime(tr.From)
	to := toRFCTime(tr.To)
	w, err := etr.Resolve(from, to, tr.MaxDataPoints, nil)
	if err != nil {
		return nil, fmt.Errorf("alertsOverviewHistorical: resolve time range: %w", err)
	}

	opts := meraki.AlertsOverviewHistoricalOptions{
		Window:  &w,
		Segment: w.Resolution,
	}
	if len(q.NetworkIDs) > 0 {
		opts.NetworkID = q.NetworkIDs[0]
	}
	if len(q.Serials) > 0 {
		opts.Serial = q.Serials[0]
	}

	points, err := client.GetOrganizationAssuranceAlertsOverviewHistorical(ctx, q.OrgID, opts, alertsTTL)
	if err != nil {
		return nil, err
	}

	// Build one frame per severity with time + value columns.
	severities := []string{"critical", "warning", "informational"}
	type sevSeries struct {
		Times  []time.Time
		Values []int64
	}
	seriesMap := make(map[string]*sevSeries, len(severities))
	for _, sev := range severities {
		seriesMap[sev] = &sevSeries{}
	}

	for _, pt := range points {
		for _, sev := range severities {
			s := seriesMap[sev]
			s.Times = append(s.Times, pt.StartTs)
			s.Values = append(s.Values, pt.BySeverity[sev])
		}
	}

	frames := make([]*data.Frame, 0, len(severities))
	for _, sev := range severities {
		s := seriesMap[sev]
		tsField := data.NewField("ts", nil, s.Times)
		valField := data.NewField("value", data.Labels{"severity": sev}, s.Values)
		valField.Config = &data.FieldConfig{
			DisplayNameFromDS: sev,
		}
		frames = append(frames, data.NewFrame("alerts_overview_historical", tsField, valField))
	}

	if w.Truncated && len(frames) > 0 {
		for _, ann := range w.Annotations {
			frames[0].AppendNotices(data.Notice{Severity: data.NoticeSeverityInfo, Text: ann})
		}
	}

	return frames, nil
}
