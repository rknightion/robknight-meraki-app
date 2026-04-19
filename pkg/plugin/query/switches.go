package query

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// Switch-family cache TTLs — port state and speed flap on a busy LAN so 30s
// keeps panels responsive without hammering Meraki. Port config is long-lived
// (rarely changes outside a maintenance window) so 5 minutes is safe.
const (
	switchPortsTTL         = 30 * time.Second
	switchPortConfigTTL    = 5 * time.Minute
	switchPortPacketsTTL   = 30 * time.Second
	switchPortPacketsSpan  = 5 * time.Minute

	// §4.4.3-1b TTLs — PoE piggybacks on the 30s ports feed; STP config is
	// stable so 1 min; device clients change quickly (30s matches the ports
	// feed); VLAN config is long-lived (5 min matches switchPortConfigTTL).
	switchPoeTTL          = 30 * time.Second
	switchStpTTL          = 1 * time.Minute
	switchMacTableTTL     = 30 * time.Second
	switchMacTableSpan    = 24 * time.Hour
	switchVlansSummaryTTL = 5 * time.Minute
)

// handleSwitchPorts emits one row per port across every switch in scope.
//
// Two endpoint paths:
//
//  1. **Per-switch (detail page)** — when `q.Serials` names specific switches,
//     fan out to the **device-scoped** `/devices/{serial}/switch/ports/statuses`
//     endpoint per serial and merge in the device-scoped `/devices/{serial}/
//     switch/ports` config feed for VLAN columns. The org-level
//     `bySwitch/statuses` endpoint returns a MINIMAL port shape without
//     clientCount / powerUsageInWh / usageInKb / trafficInKbps, which is why
//     the switch-detail Port map rendered zeros + noValue text on every
//     detail row before this branch existed. Verified 2026-04-19 against
//     org 1019781.
//
//  2. **Fleet (no serial filter)** — use the org-level aggregated
//     `/organizations/{orgId}/switch/ports/statuses/bySwitch` feed. Fields
//     we can't populate from that endpoint (clientCount, poePowerW,
//     vlan, allowedVlans) stay zero/empty; the fleet inventory panel
//     doesn't expose those columns so it doesn't matter there.
//
// Stack handling: each SwitchWithPorts entry carries `switchStackId`; a
// 2-member stack produces two entries both sharing the stack id. Output
// frame has a `stackId` column so the UI can group by stack when desired
// — empty string means the device is standalone (the common case).
func handleSwitchPorts(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("switchPorts: orgId is required")
	}

	switches, err := fetchSwitchesForPorts(ctx, client, q)
	if err != nil {
		return nil, err
	}

	var (
		serials      []string
		switchNames  []string
		models       []string
		stackIDs     []string
		networkIDs   []string
		networkNames []string
		portIDs      []string
		enableds     []bool
		statuses     []string
		duplexes     []string
		speedsMbps   []int64
		clientCounts []int64
		poePowerW    []float64
		vlans        []string
		allowedVlans []string
	)

	for _, sw := range switches {
		for _, p := range sw.Ports {
			serials = append(serials, sw.Serial)
			switchNames = append(switchNames, sw.Name)
			models = append(models, sw.Model)
			stackIDs = append(stackIDs, sw.StackID)
			networkIDs = append(networkIDs, sw.Network.ID)
			networkNames = append(networkNames, sw.Network.Name)
			portIDs = append(portIDs, p.PortID)
			enableds = append(enableds, p.Enabled)
			statuses = append(statuses, p.Status)
			duplexes = append(duplexes, p.Duplex)
			speedsMbps = append(speedsMbps, parseSpeedMbps(p.Speed))
			clientCounts = append(clientCounts, p.ClientCount)
			poePowerW = append(poePowerW, p.PowerUsageInWh)
			vlans = append(vlans, vlanString(p.Vlan))
			allowedVlans = append(allowedVlans, p.AllowedVlans)
		}
	}

	return []*data.Frame{
		data.NewFrame("switch_ports",
			data.NewField("serial", nil, serials),
			data.NewField("switchName", nil, switchNames),
			data.NewField("model", nil, models),
			data.NewField("stackId", nil, stackIDs),
			data.NewField("networkId", nil, networkIDs),
			data.NewField("networkName", nil, networkNames),
			data.NewField("portId", nil, portIDs),
			data.NewField("enabled", nil, enableds),
			data.NewField("status", nil, statuses),
			data.NewField("duplex", nil, duplexes),
			data.NewField("speedMbps", nil, speedsMbps),
			data.NewField("clientCount", nil, clientCounts),
			data.NewField("poePowerW", nil, poePowerW),
			data.NewField("vlan", nil, vlans),
			data.NewField("allowedVlans", nil, allowedVlans),
		),
	}, nil
}

// fleetFanoutCap bounds the number of switches we'll fan out per-device
// requests for during fleet-wide aggregation. The fleet KPI row sums
// clientCount + poePowerW across the org, which the org-level
// statuses/bySwitch endpoint no longer populates — so we hit each
// device's /switch/ports/statuses feed individually. At the Meraki
// per-org default of 10 req/s the cap keeps one panel refresh under a
// second even on cold caches; larger estates get a partial sum plus a
// truncation notice so the number isn't silently wrong.
const fleetFanoutCap = 25

// fetchSwitchesForPorts resolves q.Serials / q.NetworkIDs to a unified
// `[]SwitchWithPorts` slice. When specific serials are requested the call
// uses the device-scoped statuses feed (richer shape) plus the device-scoped
// config feed merged by portId so VLAN columns populate. For the fleet
// case (no serials) we use the org-level statuses feed — it has every
// switch's basic port info which is all the fleet inventory / port-map
// panels need. The richer fields (clientCount / powerUsageInWh) only
// populate when callers opt into device fan-out via
// `fetchSwitchesForAggregate`.
func fetchSwitchesForPorts(ctx context.Context, client *meraki.Client, q MerakiQuery) ([]meraki.SwitchWithPorts, error) {
	if len(q.Serials) > 0 {
		return fetchPerSwitchPortStatuses(ctx, client, q)
	}
	// Fleet-wide path: org-level aggregation with optional networkId filter.
	switches, err := client.GetOrganizationSwitchPortStatuses(ctx, q.OrgID, meraki.SwitchPortStatusOptions{}, switchPortsTTL)
	if err != nil {
		return nil, err
	}
	networkFilter := stringSetOrNil(q.NetworkIDs)
	if networkFilter == nil {
		return switches, nil
	}
	filtered := switches[:0:0]
	for _, sw := range switches {
		if _, ok := networkFilter[sw.Network.ID]; ok {
			filtered = append(filtered, sw)
		}
	}
	return filtered, nil
}

// fetchSwitchesForAggregate returns switches with the RICH per-port shape
// (clientCount, powerUsageInWh) even when the caller didn't scope to
// specific serials. Used by the fleet KPI aggregator so "PoE draw total"
// and "Clients" tiles on the Switches page sum across the fleet.
//
// Strategy: pull the org-level statuses feed for the switch list and
// basic per-port enabled/status, then fan out to the device-scoped
// statuses endpoint for up to `fleetFanoutCap` switches to merge in the
// rich fields. For larger estates we skip the fan-out and return the
// org-level shape alone — callers emit a truncation notice.
func fetchSwitchesForAggregate(ctx context.Context, client *meraki.Client, q MerakiQuery) (switches []meraki.SwitchWithPorts, truncated bool, err error) {
	if len(q.Serials) > 0 {
		// Serials supplied — reuse the per-serial path which already fans
		// out to device statuses + config for the full rich shape.
		switches, err = fetchPerSwitchPortStatuses(ctx, client, q)
		return switches, false, err
	}

	orgSwitches, err := client.GetOrganizationSwitchPortStatuses(ctx, q.OrgID, meraki.SwitchPortStatusOptions{}, switchPortsTTL)
	if err != nil {
		return nil, false, err
	}
	networkFilter := stringSetOrNil(q.NetworkIDs)
	candidates := orgSwitches[:0:0]
	for _, sw := range orgSwitches {
		if networkFilter != nil {
			if _, ok := networkFilter[sw.Network.ID]; !ok {
				continue
			}
		}
		candidates = append(candidates, sw)
	}

	if len(candidates) > fleetFanoutCap {
		// Estate too large — caller will attach a truncation notice and
		// use the org-level minimal shape for headline counts.
		return candidates, true, nil
	}

	// Fan out to device statuses. Single-threaded because each request
	// still charges the per-org rate limiter; parallelism without
	// coordination would just burst-fail at the token bucket.
	for i := range candidates {
		got, derr := client.GetDeviceSwitchPortStatuses(ctx, candidates[i].Serial, switchPortsTTL)
		if derr != nil {
			// Tolerate a single switch failing — keep the org-level
			// minimal ports for it so the fleet count still includes it.
			continue
		}
		candidates[i].Ports = got
	}
	return candidates, false, nil
}

// fetchPerSwitchPortStatuses issues per-serial device-scoped calls and
// merges the config feed in so VLAN/allowedVlans columns populate. The
// switch's name/model/networkId/networkName come from the org-level
// statuses feed cache (one call per panel load, shared with the fleet
// query) so the detail page still feels fast. Errors on a single serial
// are tolerated — the frame keeps the other serials' rows.
func fetchPerSwitchPortStatuses(ctx context.Context, client *meraki.Client, q MerakiQuery) ([]meraki.SwitchWithPorts, error) {
	orgSwitches, err := client.GetOrganizationSwitchPortStatuses(ctx, q.OrgID, meraki.SwitchPortStatusOptions{}, switchPortsTTL)
	if err != nil {
		return nil, err
	}
	metaBySerial := make(map[string]meraki.SwitchWithPorts, len(orgSwitches))
	for _, sw := range orgSwitches {
		metaBySerial[sw.Serial] = sw
	}

	networkFilter := stringSetOrNil(q.NetworkIDs)

	out := make([]meraki.SwitchWithPorts, 0, len(q.Serials))
	for _, serial := range q.Serials {
		if serial == "" {
			continue
		}
		meta, haveMeta := metaBySerial[serial]
		if networkFilter != nil && haveMeta {
			if _, ok := networkFilter[meta.Network.ID]; !ok {
				continue
			}
		}

		statuses, err := client.GetDeviceSwitchPortStatuses(ctx, serial, switchPortsTTL)
		if err != nil {
			// Tolerate per-serial failures — one bad serial shouldn't
			// blank the whole panel. The other serials still render.
			continue
		}
		configByPort := map[string]meraki.SwitchPortConfig{}
		if cfgs, cfgErr := client.GetDeviceSwitchPorts(ctx, serial, switchPortConfigTTL); cfgErr == nil {
			for _, c := range cfgs {
				configByPort[c.PortID] = c
			}
		}
		// Merge VLAN config fields (statuses endpoint doesn't echo them).
		for i := range statuses {
			cfg, ok := configByPort[statuses[i].PortID]
			if !ok {
				continue
			}
			if statuses[i].Vlan == 0 {
				statuses[i].Vlan = cfg.Vlan
			}
			if statuses[i].VoiceVlan == 0 {
				statuses[i].VoiceVlan = cfg.VoiceVlan
			}
			if statuses[i].AllowedVlans == "" {
				statuses[i].AllowedVlans = cfg.AllowedVlans
			}
		}

		entry := meraki.SwitchWithPorts{Serial: serial, Ports: statuses}
		if haveMeta {
			entry.Name = meta.Name
			entry.Model = meta.Model
			entry.MAC = meta.MAC
			entry.Network = meta.Network
			entry.StackID = meta.StackID
		}
		out = append(out, entry)
	}
	return out, nil
}

// handleSwitchPortConfig emits one row per port per serial. When the query
// specifies multiple serials we loop — the underlying endpoint is per-device.
// Failures on one serial abort the whole query (matching the one-frame
// contract); a future optimisation could emit per-serial notices instead.
//
// Optional port filter via `q.Metrics[0]`: when set, the handler drops every
// row whose `portId` doesn't match. Keeps the per-port drilldown panel off
// the fragile client-side `filterByValue` transform chain (todos.txt §G.20).
func handleSwitchPortConfig(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if len(q.Serials) == 0 {
		return nil, fmt.Errorf("switchPortConfig: at least one serial is required")
	}

	var portFilter string
	if len(q.Metrics) > 0 {
		portFilter = q.Metrics[0]
	}

	var (
		serials      []string
		portIDs      []string
		names        []string
		enableds     []bool
		types        []string
		vlans        []string
		voiceVlans   []string
		allowedVlans []string
		poeEnabled   []bool
		tags         []string
	)

	for _, serial := range q.Serials {
		ports, err := client.GetDeviceSwitchPorts(ctx, serial, switchPortConfigTTL)
		if err != nil {
			return nil, fmt.Errorf("switchPortConfig: %s: %w", serial, err)
		}
		for _, p := range ports {
			if portFilter != "" && p.PortID != portFilter {
				continue
			}
			serials = append(serials, serial)
			portIDs = append(portIDs, p.PortID)
			names = append(names, p.Name)
			enableds = append(enableds, p.Enabled)
			types = append(types, p.Type)
			vlans = append(vlans, vlanString(p.Vlan))
			voiceVlans = append(voiceVlans, vlanString(p.VoiceVlan))
			allowedVlans = append(allowedVlans, p.AllowedVlans)
			poeEnabled = append(poeEnabled, p.PoeEnabled)
			tags = append(tags, strings.Join(p.Tags, ","))
		}
	}

	return []*data.Frame{
		data.NewFrame("switch_port_config",
			data.NewField("serial", nil, serials),
			data.NewField("portId", nil, portIDs),
			data.NewField("name", nil, names),
			data.NewField("enabled", nil, enableds),
			data.NewField("type", nil, types),
			data.NewField("vlan", nil, vlans),
			data.NewField("voiceVlan", nil, voiceVlans),
			data.NewField("allowedVlans", nil, allowedVlans),
			data.NewField("poeEnabled", nil, poeEnabled),
			data.NewField("tags", nil, tags),
		),
	}, nil
}

// handleSwitchPortPacketCounters emits one row per counter bucket (Total,
// Broadcast, Multicast, CRC align errors, Collisions, …) for a single port.
// Meraki only exposes a device-level endpoint, so we fetch all ports on the
// switch and filter client-side to the requested port id.
//
// Port-id overload: the dispatcher's MerakiQuery has no dedicated port-id
// field, so we read it from `q.Metrics[0]` — this is the only handler that
// repurposes Metrics, and the frontend must set it when emitting a
// switchPortPacketCounters query.
//
// Timespan: `q.TimespanSeconds` passes through to the API's `timespan` param
// (in seconds). When unset we default to 5 minutes — the Meraki endpoint
// snaps any value to one of 5m / 15m / 1h / 1d anyway.
func handleSwitchPortPacketCounters(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if len(q.Serials) == 0 {
		return nil, fmt.Errorf("switchPortPacketCounters: at least one serial is required")
	}
	if len(q.Metrics) == 0 || q.Metrics[0] == "" {
		return nil, fmt.Errorf("switchPortPacketCounters: port id required (pass via metrics[0])")
	}
	serial := q.Serials[0]
	portID := q.Metrics[0]

	span := switchPortPacketsSpan
	if q.TimespanSeconds > 0 {
		span = time.Duration(q.TimespanSeconds) * time.Second
	}

	counters, err := client.GetDeviceSwitchPortPacketCounters(ctx, serial, span, switchPortPacketsTTL)
	if err != nil {
		return nil, err
	}

	var target *meraki.SwitchPortPacketCounter
	for i := range counters {
		if counters[i].PortID == portID {
			target = &counters[i]
			break
		}
	}
	if target == nil {
		// Emit an empty frame with the expected schema so the panel shows "no
		// data" instead of an error. The handler signature contract requires a
		// named frame; the empty fields let the UI bind columns consistently.
		return []*data.Frame{
			data.NewFrame("switch_port_packet_counters",
				data.NewField("desc", nil, []string{}),
				data.NewField("total", nil, []int64{}),
				data.NewField("sent", nil, []int64{}),
				data.NewField("recv", nil, []int64{}),
				data.NewField("ratePerSecTotal", nil, []float64{}),
				data.NewField("ratePerSecSent", nil, []float64{}),
				data.NewField("ratePerSecRecv", nil, []float64{}),
			),
		}, nil
	}

	var (
		descs    []string
		totals   []int64
		sents    []int64
		recvs    []int64
		rateTot  []float64
		rateSent []float64
		rateRecv []float64
	)
	for _, bucket := range target.Packets {
		descs = append(descs, bucket.Desc)
		totals = append(totals, bucket.Total)
		sents = append(sents, bucket.Sent)
		recvs = append(recvs, bucket.Recv)
		if bucket.RatePerSec != nil {
			rateTot = append(rateTot, bucket.RatePerSec.Total)
			rateSent = append(rateSent, bucket.RatePerSec.Sent)
			rateRecv = append(rateRecv, bucket.RatePerSec.Recv)
		} else {
			rateTot = append(rateTot, 0)
			rateSent = append(rateSent, 0)
			rateRecv = append(rateRecv, 0)
		}
	}

	return []*data.Frame{
		data.NewFrame("switch_port_packet_counters",
			data.NewField("desc", nil, descs),
			data.NewField("total", nil, totals),
			data.NewField("sent", nil, sents),
			data.NewField("recv", nil, recvs),
			data.NewField("ratePerSecTotal", nil, rateTot),
			data.NewField("ratePerSecSent", nil, rateSent),
			data.NewField("ratePerSecRecv", nil, rateRecv),
		),
	}, nil
}

// parseSpeedMbps decodes the Meraki `speed` field. Depending on firmware the
// value may be "1 Gbps" / "10 Gbps" / "100 Mbps" / "10 Mbps" / "" (disabled
// ports) — we normalise to Mbps so panels can apply numeric thresholds.
// Unrecognised shapes return 0.
func parseSpeedMbps(speed string) int64 {
	s := strings.TrimSpace(strings.ToLower(speed))
	if s == "" {
		return 0
	}
	// Split once on whitespace to get number + unit.
	idx := strings.IndexFunc(s, func(r rune) bool { return r == ' ' })
	if idx == -1 {
		// Might be a raw integer (numeric Mbps in some responses).
		if n, err := strconv.ParseFloat(s, 64); err == nil {
			return int64(n)
		}
		return 0
	}
	num, err := strconv.ParseFloat(strings.TrimSpace(s[:idx]), 64)
	if err != nil {
		return 0
	}
	unit := strings.TrimSpace(s[idx+1:])
	switch unit {
	case "mbps", "mbit/s", "mb/s", "mbps.":
		return int64(num)
	case "gbps", "gbit/s", "gb/s":
		return int64(num * 1000)
	case "kbps":
		return int64(num / 1000)
	}
	return 0
}

// vlanString returns the string form of a VLAN number, or empty when zero.
// Port configs report unassigned VLANs as 0 so we avoid emitting "0" to the
// frame — it would look like a trunked native VLAN and confuse the panel.
func vlanString(v int) string {
	if v == 0 {
		return ""
	}
	return strconv.Itoa(v)
}

// ---------------------------------------------------------------------------
// §3.1 — Switch ports overview by speed + usage history
// ---------------------------------------------------------------------------

// switchPortsOverviewBySpeedTTL: snapshot; 1m matches other overview kinds.
const switchPortsOverviewBySpeedTTL = 1 * time.Minute

// handleSwitchPortsOverviewBySpeed emits a **wide one-row** frame with one
// numeric field per (media, speed) bucket, labelled with a human-readable
// column name like "1 Gbps (RJ45)". Only non-zero buckets are emitted so
// the bar-gauge panel doesn't render a wall of empty bars for every speed
// the Meraki API knows about.
//
// Why wide (one col per bucket) rather than long (one row per bucket):
// Grafana's bar gauge labels each bar off the field's display name, NOT
// off a sibling string column. A long frame with `media`/`speed`/`active`
// columns requires row-aware templating (`${__data.fields.X}` inside a
// `displayName` override), which doesn't interpolate per-row on bar gauge
// / stat visualisations — every bar ended up with the literal template
// string or a blank label, and Grafana fell through to the panel's
// `noValue` text ("No port speed data available"). One-row wide frame
// sidesteps the template problem: each column becomes one bar with the
// column name as its label.
func handleSwitchPortsOverviewBySpeed(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("switchPortsOverviewBySpeed: orgId is required")
	}
	opts := meraki.SwitchPortsOverviewOptions{
		NetworkIDs: q.NetworkIDs,
		Serials:    q.Serials,
	}
	buckets, err := client.GetOrganizationSwitchPortsOverview(ctx, q.OrgID, opts, switchPortsOverviewBySpeedTTL)
	if err != nil {
		return nil, err
	}

	frame := data.NewFrame("switch_ports_overview_by_speed")
	// Emit in a deterministic order so the bar gauge legend is stable
	// across refreshes and tests compare reliably.
	sort.SliceStable(buckets, func(i, j int) bool {
		if buckets[i].Media != buckets[j].Media {
			return buckets[i].Media < buckets[j].Media
		}
		return speedRank(buckets[i].Speed) < speedRank(buckets[j].Speed)
	})
	for _, b := range buckets {
		if b.Active <= 0 {
			continue
		}
		label := fmt.Sprintf("%s (%s)", formatLinkSpeed(b.Speed), strings.ToUpper(b.Media))
		frame.Fields = append(frame.Fields, data.NewField(label, nil, []int64{b.Active}))
	}
	return []*data.Frame{frame}, nil
}

// formatLinkSpeed turns the numeric string Meraki returns into the
// human-friendly link speed operators expect on a bar label.
// `"inactive"` is pre-filtered by the handler (Active <= 0) so we don't
// need to handle it here — but we do, defensively.
func formatLinkSpeed(raw string) string {
	switch raw {
	case "10":
		return "10 Mbps"
	case "100":
		return "100 Mbps"
	case "1000":
		return "1 Gbps"
	case "2500":
		return "2.5 Gbps"
	case "5000":
		return "5 Gbps"
	case "10000":
		return "10 Gbps"
	case "20000":
		return "20 Gbps"
	case "25000":
		return "25 Gbps"
	case "40000":
		return "40 Gbps"
	case "50000":
		return "50 Gbps"
	case "100000":
		return "100 Gbps"
	case "inactive":
		return "Inactive"
	}
	return raw
}

// speedRank orders link-speed strings numerically so the sort in
// handleSwitchPortsOverviewBySpeed keeps columns in "slowest-first"
// order. Bar gauge renders columns left-to-right in the order emitted.
func speedRank(raw string) int {
	if raw == "inactive" {
		return 1 << 30
	}
	n := 0
	for _, r := range raw {
		if r < '0' || r > '9' {
			return 1 << 29
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// switchPortsUsageHistoryTTL: timeseries; 1m is consistent with other history kinds.
const switchPortsUsageHistoryTTL = 1 * time.Minute

// handleSwitchPortsUsageHistory emits one frame per (serial, metric) with
// labels on the value field for native timeseries rendering in Grafana.
// Metrics emitted: "sent", "recv", "total" (kilobytes per interval).
func handleSwitchPortsUsageHistory(ctx context.Context, client *meraki.Client, q MerakiQuery, tr TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("switchPortsUsageHistory: orgId is required")
	}

	etr, ok := meraki.KnownEndpointRanges["organizations/{organizationId}/switch/ports/usage/history/byDevice/byInterval"]
	if !ok {
		return nil, fmt.Errorf("switchPortsUsageHistory: missing KnownEndpointRanges entry")
	}

	from := toRFCTime(tr.From)
	to := toRFCTime(tr.To)
	w, err := etr.Resolve(from, to, tr.MaxDataPoints, nil)
	if err != nil {
		return nil, fmt.Errorf("switchPortsUsageHistory: resolve time range: %w", err)
	}

	opts := meraki.SwitchPortsUsageHistoryOptions{
		NetworkIDs: q.NetworkIDs,
		Serials:    q.Serials,
		Window:     &w,
		Interval:   w.Resolution,
	}
	points, err := client.GetOrganizationSwitchPortsUsageHistory(ctx, q.OrgID, opts, switchPortsUsageHistoryTTL)
	if err != nil {
		return nil, err
	}

	// Build per-(serial, metric) time series: one frame each for sent/recv/total.
	type seriesKey struct {
		Serial string
		Metric string
	}
	type seriesData struct {
		Times  []time.Time
		Values []int64
	}
	seriesMap := make(map[seriesKey]*seriesData)

	for _, pt := range points {
		for _, pair := range []struct {
			Metric string
			Val    int64
		}{
			{"sent", pt.Sent},
			{"recv", pt.Recv},
			{"total", pt.Total},
		} {
			k := seriesKey{Serial: pt.Serial, Metric: pair.Metric}
			s, ok := seriesMap[k]
			if !ok {
				s = &seriesData{}
				seriesMap[k] = s
			}
			s.Times = append(s.Times, pt.StartTs)
			s.Values = append(s.Values, pair.Val)
		}
	}

	frames := make([]*data.Frame, 0, len(seriesMap))
	for k, s := range seriesMap {
		tsField := data.NewField("ts", nil, s.Times)
		valField := data.NewField("value", data.Labels{
			"serial": k.Serial,
			"metric": k.Metric,
		}, s.Values)
		valField.Config = &data.FieldConfig{
			DisplayNameFromDS: k.Serial + " " + k.Metric,
			Unit:              "kbytes",
		}
		frames = append(frames, data.NewFrame("switch_ports_usage_history", tsField, valField))
	}

	// Attach truncation annotation if applicable.
	if w.Truncated && len(frames) > 0 {
		for _, ann := range w.Annotations {
			frames[0].AppendNotices(data.Notice{Severity: data.NoticeSeverityInfo, Text: ann})
		}
	}

	return frames, nil
}

// ---------------------------------------------------------------------------
// §4.4.3-1b — switchPoe / switchStp / switchMacTable / switchVlansSummary
// ---------------------------------------------------------------------------

// handleSwitchPoe emits one row per port carrying the PoE draw.
//
// Data source: same split as handleSwitchPorts — the **device-scoped**
// `/devices/{serial}/switch/ports/statuses` endpoint when `q.Serials` is
// set (rich shape includes `powerUsageInWh` per port), or the org-level
// `bySwitch` feed for fleet-wide queries. The org-level feed returns a
// MINIMAL port shape that omits `powerUsageInWh`, which is why the
// per-switch PoE-draw tile showed 0W before this patch.
//
// Units: we emit the value under the `poeWatts` column for backward
// compatibility with the existing panel unit, even though the API field
// is literally named `powerUsageInWh`. See the header comment on
// `SwitchPortStatus` for the semantic discussion.
func handleSwitchPoe(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("switchPoe: orgId is required")
	}
	switches, err := fetchSwitchesForPorts(ctx, client, q)
	if err != nil {
		return nil, err
	}

	var (
		serials      []string
		switchNames  []string
		networkIDs   []string
		networkNames []string
		portIDs      []string
		enableds     []bool
		poeWatts     []float64
	)
	for _, sw := range switches {
		for _, p := range sw.Ports {
			serials = append(serials, sw.Serial)
			switchNames = append(switchNames, sw.Name)
			networkIDs = append(networkIDs, sw.Network.ID)
			networkNames = append(networkNames, sw.Network.Name)
			portIDs = append(portIDs, p.PortID)
			enableds = append(enableds, p.Enabled)
			poeWatts = append(poeWatts, p.PowerUsageInWh)
		}
	}

	return []*data.Frame{
		data.NewFrame("switch_poe",
			data.NewField("serial", nil, serials),
			data.NewField("switchName", nil, switchNames),
			data.NewField("networkId", nil, networkIDs),
			data.NewField("networkName", nil, networkNames),
			data.NewField("portId", nil, portIDs),
			data.NewField("enabled", nil, enableds),
			data.NewField("poeWatts", nil, poeWatts),
		),
	}, nil
}

// handleSwitchStp emits one row per bridge-priority entry from the network's
// STP settings. A single frame carries:
//   networkId, rstpEnabled, serial, stackId, stpPriority
// The row-per-entry (rather than row-per-network) shape lets the topology
// table show per-switch priority alongside the network-level rstpEnabled flag
// without a client-side expand transform. Callers must pass at least one
// networkId (the Meraki endpoint is per-network).
func handleSwitchStp(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if len(q.NetworkIDs) == 0 {
		return nil, fmt.Errorf("switchStp: at least one networkId is required")
	}

	var (
		networkIDs   []string
		rstpEnableds []bool
		serials      []string
		stackIDs     []string
		priorities   []int64
	)
	for _, nid := range q.NetworkIDs {
		if nid == "" {
			continue
		}
		stp, err := client.GetNetworkSwitchStp(ctx, nid, switchStpTTL)
		if err != nil {
			return nil, fmt.Errorf("switchStp: %s: %w", nid, err)
		}
		if len(stp.StpBridgePriority) == 0 {
			// Emit one descriptor row so the rstpEnabled flag is still visible
			// when no explicit priorities are set on this network.
			networkIDs = append(networkIDs, nid)
			rstpEnableds = append(rstpEnableds, stp.RstpEnabled)
			serials = append(serials, "")
			stackIDs = append(stackIDs, "")
			priorities = append(priorities, 0)
			continue
		}
		for _, bp := range stp.StpBridgePriority {
			// Expand each bridge-priority bucket across its switches and stacks.
			if len(bp.Switches) == 0 && len(bp.Stacks) == 0 {
				networkIDs = append(networkIDs, nid)
				rstpEnableds = append(rstpEnableds, stp.RstpEnabled)
				serials = append(serials, "")
				stackIDs = append(stackIDs, "")
				priorities = append(priorities, int64(bp.StpPriority))
				continue
			}
			for _, s := range bp.Switches {
				networkIDs = append(networkIDs, nid)
				rstpEnableds = append(rstpEnableds, stp.RstpEnabled)
				serials = append(serials, s)
				stackIDs = append(stackIDs, "")
				priorities = append(priorities, int64(bp.StpPriority))
			}
			for _, st := range bp.Stacks {
				networkIDs = append(networkIDs, nid)
				rstpEnableds = append(rstpEnableds, stp.RstpEnabled)
				serials = append(serials, "")
				stackIDs = append(stackIDs, st)
				priorities = append(priorities, int64(bp.StpPriority))
			}
		}
	}

	return []*data.Frame{
		data.NewFrame("switch_stp",
			data.NewField("networkId", nil, networkIDs),
			data.NewField("rstpEnabled", nil, rstpEnableds),
			data.NewField("serial", nil, serials),
			data.NewField("stackId", nil, stackIDs),
			data.NewField("stpPriority", nil, priorities),
		),
	}, nil
}

// handleSwitchMacTable emits one row per client currently (or recently)
// connected to the given switch. Data comes from
// `GET /devices/{serial}/clients` which returns per-client usage + the
// connected switchport. Default lookback window is 24h (the Meraki default
// is 1d); callers can override via `q.TimespanSeconds`.
func handleSwitchMacTable(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if len(q.Serials) == 0 {
		return nil, fmt.Errorf("switchMacTable: at least one serial is required")
	}

	span := switchMacTableSpan
	if q.TimespanSeconds > 0 {
		span = time.Duration(q.TimespanSeconds) * time.Second
	}

	var (
		serialsCol    []string
		macs          []string
		ips           []string
		vlans         []string
		ports         []string
		descs         []string
		users         []string
		manufacturers []string
		lastSeens     []time.Time
		sentKb        []float64
		recvKb        []float64
	)

	for _, serial := range q.Serials {
		clients, err := client.GetDeviceClients(ctx, serial, span, switchMacTableTTL)
		if err != nil {
			return nil, fmt.Errorf("switchMacTable: %s: %w", serial, err)
		}
		for _, c := range clients {
			serialsCol = append(serialsCol, serial)
			macs = append(macs, c.MAC)
			ips = append(ips, c.IP)
			vlans = append(vlans, vlanString(c.VLAN))
			ports = append(ports, c.SwitchPort)
			descs = append(descs, c.Description)
			users = append(users, c.User)
			manufacturers = append(manufacturers, c.Manufacturer)
			if c.LastSeen > 0 {
				lastSeens = append(lastSeens, time.Unix(c.LastSeen, 0).UTC())
			} else {
				lastSeens = append(lastSeens, time.Time{})
			}
			sentKb = append(sentKb, c.Usage.Sent)
			recvKb = append(recvKb, c.Usage.Recv)
		}
	}

	return []*data.Frame{
		data.NewFrame("switch_mac_table",
			data.NewField("serial", nil, serialsCol),
			data.NewField("mac", nil, macs),
			data.NewField("ip", nil, ips),
			data.NewField("vlan", nil, vlans),
			data.NewField("switchPort", nil, ports),
			data.NewField("description", nil, descs),
			data.NewField("user", nil, users),
			data.NewField("manufacturer", nil, manufacturers),
			data.NewField("lastSeen", nil, lastSeens),
			data.NewField("sentKb", nil, sentKb),
			data.NewField("recvKb", nil, recvKb),
		),
	}, nil
}

// handleSwitchVlansSummary emits a **wide one-row** frame with one numeric
// field per VLAN. Field names are human-readable ("VLAN 25",
// "VLAN 100 (voice)") so the piechart viz can render slices labelled by
// the column name directly — no fragile `${__data.fields.X}` per-row
// templating, no organize-transform gymnastics to hide metadata columns.
// The long-format shape (serial, switchName, networkId, vlan, portCount
// columns × many rows) confused the piechart into "Cannot visualize
// data" because it had multiple string columns to choose from for slice
// labels. Wide format sidesteps that the same way the bar-gauge ports-
// by-speed fix did.
//
// Source: `GET /organizations/{orgId}/switch/ports/bySwitch` (config
// feed, bare array) — same endpoint as before; only the output shape
// changed. Voice VLANs are suffixed `(voice)` so they render as a
// distinct slice from the native VLAN with the same number.
func handleSwitchVlansSummary(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("switchVlansSummary: orgId is required")
	}
	opts := meraki.SwitchBySwitchPortsOptions{
		NetworkIDs: q.NetworkIDs,
		Serials:    q.Serials,
	}
	switches, err := client.GetOrganizationSwitchPortsBySwitch(ctx, q.OrgID, opts, switchVlansSummaryTTL)
	if err != nil {
		return nil, err
	}

	// Aggregate by VLAN label across every switch in scope. When the caller
	// filters to one serial (per-switch detail page) this boils down to
	// per-switch counts; the fleet page without a filter gets org-wide
	// totals under the same column names.
	type vlanKey struct {
		Label string // e.g. "VLAN 25" or "VLAN 100 (voice)"
		Rank  int    // sort-order hint (numeric VLAN, voice flag)
	}
	counts := make(map[vlanKey]int64)
	for _, sw := range switches {
		for _, p := range sw.Ports {
			if !p.Enabled {
				continue
			}
			if p.Vlan != 0 {
				k := vlanKey{Label: "VLAN " + strconv.Itoa(p.Vlan), Rank: p.Vlan*10 + 0}
				counts[k]++
			}
			if p.VoiceVlan != 0 {
				k := vlanKey{Label: "VLAN " + strconv.Itoa(p.VoiceVlan) + " (voice)", Rank: p.VoiceVlan*10 + 1}
				counts[k]++
			}
		}
	}

	keys := make([]vlanKey, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].Rank < keys[j].Rank })

	frame := data.NewFrame("switch_vlans_summary")
	for _, k := range keys {
		frame.Fields = append(frame.Fields, data.NewField(k.Label, nil, []int64{counts[k]}))
	}
	return []*data.Frame{frame}, nil
}

// stringSetOrNil returns a lookup set for non-empty entries or nil when the
// slice is empty/all-empty. Used by the filter helpers above.
func stringSetOrNil(xs []string) map[string]struct{} {
	if len(xs) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(xs))
	for _, s := range xs {
		if s != "" {
			out[s] = struct{}{}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
