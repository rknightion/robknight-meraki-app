package query

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"
	"golang.org/x/sync/errgroup"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// orgChangeFeedTTL is short because the feed backs a near-live Home-page tile.
// The two underlying endpoints have their own TTLs (configurationChangesTTL
// 5m, networkEventsTTL 30s); we intentionally don't add another layer — this
// constant is only used when a future caching wrapper is added around the
// handler itself, today both client.GetAll calls pay their own TTLs already.
const orgChangeFeedTTL = 30 * time.Second

// orgChangeFeedNetworkFanoutCap mirrors networkEventsAllFanoutCap — the org
// events side of the union can't talk to the org directly, only to networks.
// 25 networks × 1000 events default perPage dominates the /events budget, so
// the cap protects the rate budget for the Home tile on very large estates.
const orgChangeFeedNetworkFanoutCap = 25

// orgChangeFeedWindow is the lookback the tile always shows — 24 hours per
// §4.4.3-1f. We ignore the panel time range for the data fetch so the Home
// tile is a stable "what just changed" surface regardless of what range the
// dashboard happens to be showing. The row limit (orgChangeFeedRowCap) is
// applied after the union + sort, so if 24 hours produced 50 events the tile
// still only emits the 10 most recent.
const orgChangeFeedWindow = 24 * time.Hour
const orgChangeFeedRowCap = 10

// severityForEvent maps a Meraki NetworkEvent to a synthetic severity bucket.
//
// Meraki's /events feed has NO native severity field — it only carries
// (category, type, description). The §4.4.3-1f plan asks for "severity≥warn
// events" filtering, so we approximate with a category+type allow-list:
//
//   - Anything whose category matches well-known failure words ("fail",
//     "error", "down", "outage", "critical") is treated as "warning" or
//     "critical". Categories typically seen here: "Alerts", "Errors",
//     "Connectivity", "Faults".
//   - Type names that include "fail" / "down" / "disconnect" / "loss" /
//     "critical" are also bucketed as warning.
//   - Everything else ("DHCP", "associations", "roaming", "client_connectivity",
//     routine lifecycle events) is treated as "info" and ELIDED from the feed.
//
// This is intentionally a conservative allow-list so the Home tile shows
// operator-actionable events. It's NOT a 1:1 mirror of Meraki's dashboard
// severity column because no such server-side column exists on /events. Audit
// changes are always included (admins touching config is always interesting).
func severityForEvent(e meraki.NetworkEvent) string {
	haystack := strings.ToLower(e.Category + " " + e.Type + " " + e.Description)
	// Critical markers — hard failures / outages.
	for _, needle := range []string{"critical", "outage", "down", "offline", "loss"} {
		if strings.Contains(haystack, needle) {
			return "critical"
		}
	}
	// Warning markers — degradations / auth failures / reboots.
	for _, needle := range []string{"fail", "error", "fault", "disconnect", "reboot", "warn"} {
		if strings.Contains(haystack, needle) {
			return "warning"
		}
	}
	return "info"
}

// handleOrgChangeFeed unions two feeds into a single table frame:
//
//  1. Organization configuration changes (GetOrganizationConfigurationChanges)
//     — admin-initiated config churn is surfaced with source="audit".
//  2. Network events across every network in the org, filtered to
//     severity≥warn via the severityForEvent heuristic above, with
//     source="event".
//
// Emitted shape (wide table): time, source, title, text, severity. Rows sorted
// by time descending; capped at orgChangeFeedRowCap entries so the Home tile
// stays compact. The two feeds are fetched in parallel via errgroup to keep
// latency close to max(audit, events) rather than audit+events.
//
// The handler IGNORES the panel time range and always looks back 24 hours —
// this tile is the §4.4.3-1f "what changed in 24h" surface, independent of
// the dashboard time picker.
func handleOrgChangeFeed(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("orgChangeFeed: orgId is required")
	}

	end := time.Now().UTC()
	start := end.Add(-orgChangeFeedWindow)

	type row struct {
		t        time.Time
		source   string
		title    string
		text     string
		severity string
	}
	var (
		audit  []meraki.ConfigurationChange
		events []meraki.NetworkEvent
	)

	g, gctx := errgroup.WithContext(ctx)

	// Audit feed — always included; admin-initiated changes are inherently
	// actionable. Severity "info" unless the change carries an obviously
	// destructive keyword (delete / remove / revoke).
	g.Go(func() error {
		changes, err := client.GetOrganizationConfigurationChanges(gctx, q.OrgID, meraki.ConfigurationChangesOptions{
			TSStart: &start,
			TSEnd:   &end,
		}, orgChangeFeedTTL)
		if err != nil {
			return fmt.Errorf("orgChangeFeed: audit: %w", err)
		}
		audit = changes
		return nil
	})

	// Events feed — fan out over networks, same cap as handleNetworkEvents.
	g.Go(func() error {
		networks, err := client.GetOrganizationNetworks(gctx, q.OrgID, nil, networksTTL)
		if err != nil {
			return fmt.Errorf("orgChangeFeed: networks: %w", err)
		}
		ids := make([]string, 0, len(networks))
		for _, n := range networks {
			if n.ID != "" {
				ids = append(ids, n.ID)
			}
		}
		sort.Strings(ids)
		if len(ids) > orgChangeFeedNetworkFanoutCap {
			ids = ids[:orgChangeFeedNetworkFanoutCap]
		}
		evOpts := meraki.NetworkEventsOptions{
			TSStart: &start,
			TSEnd:   &end,
		}
		for _, nid := range ids {
			got, err := client.GetNetworkEvents(gctx, nid, evOpts, orgChangeFeedTTL)
			if err != nil {
				return fmt.Errorf("orgChangeFeed: events(%s): %w", nid, err)
			}
			events = append(events, got...)
		}
		return nil
	})

	if err := g.Wait(); err != nil {
		return nil, err
	}

	rows := make([]row, 0, len(audit)+len(events))
	for _, ch := range audit {
		var t time.Time
		if ch.TS != nil {
			t = ch.TS.UTC()
		}
		actor := ch.AdminName
		if actor == "" {
			actor = ch.AdminEmail
		}
		if actor == "" {
			actor = ch.AdminID
		}
		label := ch.Label
		if label == "" {
			label = ch.Page
		}
		title := label
		if actor != "" {
			title = actor + " — " + label
		}
		body := ch.OldValue + " → " + ch.NewValue
		if len(body) > 400 {
			body = body[:400] + "…"
		}
		sev := "info"
		if lc := strings.ToLower(ch.Label); strings.Contains(lc, "delete") || strings.Contains(lc, "remove") || strings.Contains(lc, "revoke") {
			sev = "warning"
		}
		rows = append(rows, row{t: t, source: "audit", title: title, text: body, severity: sev})
	}
	for _, e := range events {
		sev := severityForEvent(e)
		if sev == "info" {
			// Filter to severity≥warn per §4.4.3-1f spec.
			continue
		}
		var t time.Time
		if e.OccurredAt != nil {
			t = e.OccurredAt.UTC()
		}
		title := e.Type
		if title == "" {
			title = e.Category
		}
		if e.DeviceName != "" {
			title = e.DeviceName + " — " + title
		} else if e.DeviceSerial != "" {
			title = e.DeviceSerial + " — " + title
		}
		rows = append(rows, row{t: t, source: "event", title: title, text: e.Description, severity: sev})
	}

	// Sort by time desc; cap to orgChangeFeedRowCap.
	sort.Slice(rows, func(i, j int) bool { return rows[i].t.After(rows[j].t) })
	if len(rows) > orgChangeFeedRowCap {
		rows = rows[:orgChangeFeedRowCap]
	}

	var (
		times     = make([]time.Time, 0, len(rows))
		sources   = make([]string, 0, len(rows))
		titles    = make([]string, 0, len(rows))
		texts     = make([]string, 0, len(rows))
		severs    = make([]string, 0, len(rows))
	)
	for _, r := range rows {
		times = append(times, r.t)
		sources = append(sources, r.source)
		titles = append(titles, r.title)
		texts = append(texts, r.text)
		severs = append(severs, r.severity)
	}

	return []*data.Frame{
		data.NewFrame("org_change_feed",
			data.NewField("time", nil, times),
			data.NewField("source", nil, sources),
			data.NewField("title", nil, titles),
			data.NewField("text", nil, texts),
			data.NewField("severity", nil, severs),
		),
	}, nil
}
