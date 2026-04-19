package query

// v0.5 §4.4.4-C — Traffic Analytics handlers.
//
// Four kinds back the new /traffic page:
//
//   - networkTraffic                       table per (network × app) row from
//                                          /networks/{id}/traffic
//   - topApplicationsByUsage               wide top-N table from
//                                          /organizations/{id}/summary/top/applications/byUsage
//   - topApplicationCategoriesByUsage      wide top-N table from
//                                          /organizations/{id}/summary/top/applications/categories/byUsage
//   - networkTrafficAnalysisMode           one-row-per-network mode lookup from
//                                          /networks/{id}/trafficAnalysis. Powers
//                                          the TrafficGuard banner.

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// Per-kind TTLs: traffic data refreshes minute-ish on Meraki's side, and the
// top-N summaries are aggregated server-side. 5 minutes matches the rest of
// the §4.4.4 page-level handlers.
const (
	trafficTTL                  = 5 * time.Minute
	topAppsTTL                  = 5 * time.Minute
	trafficAnalysisModeTTL      = 5 * time.Minute
	networkTrafficEndpoint      = "networks/{networkId}/traffic"
	topAppsByUsageEndpoint      = "organizations/{organizationId}/summary/top/applications/byUsage"
	topAppCategoriesByUsageEnd  = "organizations/{organizationId}/summary/top/applications/categories/byUsage"
	// topAppsMinWindow is the smallest window the /summary/top/applications
	// endpoints consistently return data for. Empirical check against a live
	// org (2026-04-19): 1h/2h/6h → 0 rows; 8h/12h → populated. Meraki
	// pre-aggregates this summary on coarse buckets server-side, so windows
	// shorter than ~8h align to an empty bucket. Clamp to 12h and attach a
	// notice so operators understand why.
	topAppsMinWindow = 12 * time.Hour
)

// handleNetworkTraffic emits a single long-format table frame with one row
// per (networkId, application, destination, port) tuple across the selected
// networks. Frame columns:
//
//	networkId | application | category | destination | protocol | port |
//	  sentMb | recvMb | totalMb | numClients | activeTime | flows
//
// A long table here is intentional: the panel renders as a sortable table
// (no per-series legend), so the wide-frame-per-KPI rule (§G.20) doesn't
// apply. Per-network fan-out is sequential to share the rate-limiter and
// avoid blowing the per-org token bucket on dashboards that select every
// network in a large estate.
func handleNetworkTraffic(ctx context.Context, client *meraki.Client, q MerakiQuery, tr TimeRange, _ Options) ([]*data.Frame, error) {
	if len(q.NetworkIDs) == 0 {
		return nil, fmt.Errorf("networkTraffic: networkIds is required")
	}

	// Expand the all-networks sentinel (network picker set to "All" —
	// `allValue: ''` in scene-helpers/variables.ts, which interpolates
	// to a single empty-string entry) into the concrete org network list.
	// Without this, the loop below skipped every empty id and the panel
	// rendered blank whenever the user didn't manually pick a network.
	targets, truncated, terr := resolveNetworkTrafficTargets(ctx, client, q)
	if terr != nil {
		return nil, terr
	}

	etr, ok := meraki.KnownEndpointRanges[networkTrafficEndpoint]
	if !ok {
		return nil, fmt.Errorf("networkTraffic: missing KnownEndpointRanges entry")
	}
	from := toRFCTime(tr.From)
	to := toRFCTime(tr.To)
	w, err := etr.Resolve(from, to, tr.MaxDataPoints, nil)
	if err != nil {
		return nil, fmt.Errorf("networkTraffic: resolve time range: %w", err)
	}

	// Optional deviceType filter via q.Metrics[0]. Allowed values per the
	// spec: "combined" (default), "wireless", "switch", "appliance".
	opts := meraki.NetworkTrafficOptions{
		Window:     &w,
		DeviceType: firstNonEmpty(q.Metrics),
	}

	var (
		networkIDs   []string
		applications []string
		categories   []string
		destinations []string
		protocols    []string
		ports        []int64
		sent         []float64
		recv         []float64
		total        []float64
		numClients   []int64
		activeTime   []int64
		flows        []int64
	)

	for _, nid := range targets {
		rows, ferr := client.GetNetworkTraffic(ctx, nid, opts, trafficTTL)
		if ferr != nil {
			// Per-network failure: keep going so the table still renders
			// rows from healthy networks. The first per-row error is
			// surfaced as the handler error so it lands as a frame notice.
			if err == nil {
				err = fmt.Errorf("networkTraffic: %s: %w", nid, ferr)
			}
			continue
		}
		for _, r := range rows {
			networkIDs = append(networkIDs, nid)
			applications = append(applications, r.Application)
			// /networks/{id}/traffic doesn't include a category column on
			// each row — the category breakdown is the sibling
			// `topApplicationCategoriesByUsage` kind. Keep the column for
			// schema stability so future Meraki API revisions can populate
			// it without panel churn.
			categories = append(categories, "")
			destinations = append(destinations, r.Destination)
			protocols = append(protocols, r.Protocol)
			if r.Port != nil {
				ports = append(ports, *r.Port)
			} else {
				ports = append(ports, 0)
			}
			sent = append(sent, r.Sent)
			recv = append(recv, r.Recv)
			total = append(total, r.Sent+r.Recv)
			numClients = append(numClients, r.NumClients)
			activeTime = append(activeTime, r.ActiveTime)
			flows = append(flows, r.Flows)
		}
	}

	frame := data.NewFrame("network_traffic",
		data.NewField("networkId", nil, networkIDs),
		data.NewField("application", nil, applications),
		data.NewField("category", nil, categories),
		data.NewField("destination", nil, destinations),
		data.NewField("protocol", nil, protocols),
		data.NewField("port", nil, ports),
		data.NewField("sentMb", nil, sent),
		data.NewField("recvMb", nil, recv),
		data.NewField("totalMb", nil, total),
		data.NewField("numClients", nil, numClients),
		data.NewField("activeTime", nil, activeTime),
		data.NewField("flows", nil, flows),
	)

	if w.Truncated {
		for _, ann := range w.Annotations {
			frame.AppendNotices(data.Notice{Severity: data.NoticeSeverityInfo, Text: ann})
		}
	}
	if truncated {
		frame.AppendNotices(data.Notice{
			Severity: data.NoticeSeverityInfo,
			Text: fmt.Sprintf(
				"Per-network traffic truncated: queried only the first %d networks in this organisation. Pick specific networks for the full breakdown.",
				networkTrafficAllFanoutCap,
			),
		})
	}

	return []*data.Frame{frame}, err
}

// networkTrafficAllFanoutCap bounds the number of networks a single
// networkTraffic request can fan out over when the picker resolves to
// "All". Each target is one Meraki API call; the cap keeps a dashboard
// refresh from draining the per-org token bucket on very large estates.
// Matches the cap used by the network-events handlers.
const networkTrafficAllFanoutCap = 25

// resolveNetworkTrafficTargets expands the "All" sentinel (empty-string
// entries from `allValue: ''` on the $network picker) into a concrete
// list of network IDs for the org. When the caller supplied at least
// one non-empty id we preserve the exact list they gave us (no fanout).
// Returns the target list plus a flag signalling that we truncated the
// org-wide list to `networkTrafficAllFanoutCap` entries.
func resolveNetworkTrafficTargets(ctx context.Context, client *meraki.Client, q MerakiQuery) ([]string, bool, error) {
	concrete := make([]string, 0, len(q.NetworkIDs))
	for _, id := range q.NetworkIDs {
		if id != "" && id != "$__all" && id != "*" {
			concrete = append(concrete, id)
		}
	}
	if len(concrete) > 0 {
		return concrete, false, nil
	}

	if q.OrgID == "" {
		// No orgId to enumerate against — preserve the legacy contract so
		// the handler surfaces the same "networkIds is required" error in
		// this pathological case.
		return nil, false, fmt.Errorf("networkTraffic: networkIds is required (and orgId missing, cannot expand)")
	}
	networks, err := client.GetOrganizationNetworks(ctx, q.OrgID, q.ProductTypes, networksTTL)
	if err != nil {
		return nil, false, fmt.Errorf("networkTraffic: expand all-networks: %w", err)
	}
	ids := make([]string, 0, len(networks))
	for _, n := range networks {
		if n.ID != "" {
			ids = append(ids, n.ID)
		}
	}
	sort.Strings(ids)
	truncated := false
	if len(ids) > networkTrafficAllFanoutCap {
		ids = ids[:networkTrafficAllFanoutCap]
		truncated = true
	}
	return ids, truncated, nil
}

// handleTopApplicationsByUsage emits a wide table frame with one row per
// returned application. Columns:
//
//	name | category | totalMb | downstreamMb | upstreamMb | percentage | clientCount
//
// `q.Metrics[0]` (when set and parseable) overrides the result quantity;
// `q.NetworkIDs[0]` (when set) restricts to a single network so the page can
// reuse the same kind for "top apps in $network" without a separate handler.
// The Meraki endpoint accepts only one networkId — taking the first entry
// matches the existing alerts/alertsOverview convention.
func handleTopApplicationsByUsage(ctx context.Context, client *meraki.Client, q MerakiQuery, tr TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("topApplicationsByUsage: orgId is required")
	}

	opts, extended := buildTopApplicationsOptions(q, tr)
	rows, err := client.GetOrganizationTopApplicationsByUsage(ctx, q.OrgID, opts, topAppsTTL)
	if err != nil {
		return nil, err
	}

	// The /summary/top/applications/byUsage endpoint does NOT return a
	// category column per row — the category breakdown lives on the sibling
	// /applications/categories/byUsage endpoint. We intentionally do not emit
	// a `category` field here; the panel's `setNoValue()` text would otherwise
	// fill every empty cell.
	var (
		names       []string
		totals      []float64
		downstream  []float64
		upstream    []float64
		percentages []float64
		clientCount []int64
	)
	for _, r := range rows {
		names = append(names, r.Application)
		totals = append(totals, r.Total)
		downstream = append(downstream, r.Downstream)
		upstream = append(upstream, r.Upstream)
		percentages = append(percentages, r.Percentage)
		var clients int64
		if r.Clients != nil && r.Clients.Counts != nil {
			clients = r.Clients.Counts.Total
		}
		clientCount = append(clientCount, clients)
	}

	frame := data.NewFrame("top_applications_by_usage",
		data.NewField("name", nil, names),
		data.NewField("totalMb", nil, totals),
		data.NewField("downstreamMb", nil, downstream),
		data.NewField("upstreamMb", nil, upstream),
		data.NewField("percentage", nil, percentages),
		data.NewField("clientCount", nil, clientCount),
	)
	if extended {
		frame.AppendNotices(topAppsExtensionNotice())
	}
	return []*data.Frame{frame}, nil
}

// handleTopApplicationCategoriesByUsage is the categories sibling of
// handleTopApplicationsByUsage. Wire frame is the same shape minus the
// `category` column (each row IS a category here):
//
//	name | totalMb | downstreamMb | upstreamMb | percentage | clientCount
func handleTopApplicationCategoriesByUsage(ctx context.Context, client *meraki.Client, q MerakiQuery, tr TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("topApplicationCategoriesByUsage: orgId is required")
	}

	opts, extended := buildTopApplicationsOptions(q, tr)
	rows, err := client.GetOrganizationTopApplicationCategoriesByUsage(ctx, q.OrgID, opts, topAppsTTL)
	if err != nil {
		return nil, err
	}

	var (
		names       []string
		totals      []float64
		downstream  []float64
		upstream    []float64
		percentages []float64
		clientCount []int64
	)
	for _, r := range rows {
		names = append(names, r.Category)
		totals = append(totals, r.Total)
		downstream = append(downstream, r.Downstream)
		upstream = append(upstream, r.Upstream)
		percentages = append(percentages, r.Percentage)
		var clients int64
		if r.Clients != nil && r.Clients.Counts != nil {
			clients = r.Clients.Counts.Total
		}
		clientCount = append(clientCount, clients)
	}

	frame := data.NewFrame("top_application_categories_by_usage",
		data.NewField("name", nil, names),
		data.NewField("totalMb", nil, totals),
		data.NewField("downstreamMb", nil, downstream),
		data.NewField("upstreamMb", nil, upstream),
		data.NewField("percentage", nil, percentages),
		data.NewField("clientCount", nil, clientCount),
	)
	if extended {
		frame.AppendNotices(topAppsExtensionNotice())
	}
	return []*data.Frame{frame}, nil
}

// handleNetworkTrafficAnalysisMode emits a single table frame with one row
// per requested network: (networkId, mode). Used by the TrafficGuard React
// component on the Page C scene to render a banner when a selected network
// has traffic analysis disabled (which produces empty L7 breakdowns until
// it's turned on).
//
// Frame shape:
//
//	networkId | mode    (string: "disabled" | "basic" | "detailed" | "")
//
// Per-network failures emit a row with mode="" so the frontend treats the
// network as "unknown" rather than silently dropping it from the banner
// audit.
func handleNetworkTrafficAnalysisMode(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if len(q.NetworkIDs) == 0 {
		return nil, fmt.Errorf("networkTrafficAnalysisMode: networkIds is required")
	}

	// De-dup + sort so the frame is deterministic regardless of which order
	// scenes interpolate the multi-value variable.
	seen := make(map[string]struct{}, len(q.NetworkIDs))
	nids := make([]string, 0, len(q.NetworkIDs))
	for _, n := range q.NetworkIDs {
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		nids = append(nids, n)
	}
	sort.Strings(nids)

	var firstErr error
	modes := make([]string, 0, len(nids))
	for _, nid := range nids {
		settings, ferr := client.GetNetworkTrafficAnalysis(ctx, nid, trafficAnalysisModeTTL)
		if ferr != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("networkTrafficAnalysisMode: %s: %w", nid, ferr)
			}
			modes = append(modes, "")
			continue
		}
		if settings != nil {
			modes = append(modes, settings.Mode)
		} else {
			modes = append(modes, "")
		}
	}

	frame := data.NewFrame("network_traffic_analysis_mode",
		data.NewField("networkId", nil, nids),
		data.NewField("mode", nil, modes),
	)
	return []*data.Frame{frame}, firstErr
}

// buildTopApplicationsOptions extracts the shared filter set from a
// MerakiQuery so the application + category handlers stay in lock-step.
//
// Conventions:
//   - q.NetworkIDs[0] is treated as a single-network filter (the API only
//     accepts one networkId per request).
//   - q.Metrics[0], if numeric, sets the result quantity (Meraki cap: 50).
//   - The window is derived from the panel range and clamped via
//     KnownEndpointRanges (186-day cap) AND extended to topAppsMinWindow
//     on the low side so sub-hour picker ranges return data instead of 0.
//     The second return value is true when the window was extended so the
//     handler can surface a panel-level notice.
func buildTopApplicationsOptions(q MerakiQuery, tr TimeRange) (meraki.TopApplicationsOptions, bool) {
	opts := meraki.TopApplicationsOptions{}
	if len(q.NetworkIDs) > 0 {
		opts.NetworkID = q.NetworkIDs[0]
	}
	if q.TimespanSeconds > 0 {
		opts.TimespanSeconds = q.TimespanSeconds
	}
	extended := false
	if etr, ok := meraki.KnownEndpointRanges[topAppsByUsageEndpoint]; ok {
		from := toRFCTime(tr.From)
		to := toRFCTime(tr.To)
		if !from.IsZero() && !to.IsZero() && from.Before(to) {
			// Extend short windows up to topAppsMinWindow BEFORE Resolve so
			// the downstream annotations (e.g. 186-day truncation) still see
			// the adjusted range.
			if to.Sub(from) < topAppsMinWindow {
				from = to.Add(-topAppsMinWindow)
				extended = true
			}
			if w, err := etr.Resolve(from, to, tr.MaxDataPoints, nil); err == nil {
				opts.Window = &w
				// Window wins over TimespanSeconds — clear so the URL
				// doesn't double up t0/t1 + timespan.
				opts.TimespanSeconds = 0
			}
		}
	}
	if qty := parseQuantity(q.Metrics); qty > 0 {
		opts.Quantity = qty
	}
	return opts, extended
}

// topAppsExtensionNotice builds the data.Notice emitted when a panel's time
// range was widened to `topAppsMinWindow` to satisfy Meraki's minimum. Kept as
// a helper so both handlers emit the same wording.
func topAppsExtensionNotice() data.Notice {
	return data.Notice{
		Severity: data.NoticeSeverityInfo,
		Text: fmt.Sprintf(
			"Panel window extended to %s — Meraki's top-applications summary only returns data for windows of at least ~12h.",
			topAppsMinWindow,
		),
	}
}

// parseQuantity reads q.Metrics[0] as a positive integer quantity override.
// Returns 0 (= use server default) when the slot is empty or unparseable.
// Kept tiny and dependency-free so the handlers can stay symmetrical with
// the existing alerts severity-via-Metrics convention.
func parseQuantity(metrics []string) int {
	if len(metrics) == 0 {
		return 0
	}
	v := metrics[0]
	if v == "" {
		return 0
	}
	n := 0
	for _, ch := range v {
		if ch < '0' || ch > '9' {
			return 0
		}
		n = n*10 + int(ch-'0')
		if n > 50 {
			return 50
		}
	}
	return n
}
