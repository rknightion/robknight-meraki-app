package query

import (
	"context"
	"fmt"
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

// handleSwitchPorts emits one row per port across every switch in the org.
// The source endpoint (`/organizations/{orgId}/switch/ports/statuses/bySwitch`)
// already returns one entry per switch with a nested `ports` array; we flatten
// that into a single table-shaped frame so the UI's port map panel can render
// it directly without a client-side expand transform.
//
// Stack handling: the endpoint returns one entry per stack-member device, so
// a 2-member stack produces two `SwitchWithPorts` entries both carrying the
// same `switchStackId`. The emitted frame has a `stackId` column so the UI
// can group by stack when grouping is desired — empty string means the device
// is standalone.
func handleSwitchPorts(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("switchPorts: orgId is required")
	}
	// NOTE: we intentionally DON'T forward q.Serials / q.NetworkIDs to the
	// Meraki API — pushing them server-side shards the cache key per serial,
	// so every per-switch detail panel pays a fresh round-trip. Fetching the
	// whole-fleet payload once and filtering client-side lets the detail page
	// reuse the fleet query's cache entry. The `bySwitch` endpoint also has
	// historically inconsistent behaviour when mixing `serials` filters with
	// pagination (empty results despite valid serials), so filtering
	// client-side sidesteps that too.
	opts := meraki.SwitchPortStatusOptions{}
	switches, err := client.GetOrganizationSwitchPortStatuses(ctx, q.OrgID, opts, switchPortsTTL)
	if err != nil {
		return nil, err
	}

	var serialFilter map[string]struct{}
	if len(q.Serials) > 0 {
		serialFilter = make(map[string]struct{}, len(q.Serials))
		for _, s := range q.Serials {
			if s != "" {
				serialFilter[s] = struct{}{}
			}
		}
	}
	var networkFilter map[string]struct{}
	if len(q.NetworkIDs) > 0 {
		networkFilter = make(map[string]struct{}, len(q.NetworkIDs))
		for _, n := range q.NetworkIDs {
			if n != "" {
				networkFilter[n] = struct{}{}
			}
		}
	}
	if serialFilter != nil || networkFilter != nil {
		filtered := switches[:0:0]
		for _, sw := range switches {
			if serialFilter != nil {
				if _, ok := serialFilter[sw.Serial]; !ok {
					continue
				}
			}
			if networkFilter != nil {
				if _, ok := networkFilter[sw.Network.ID]; !ok {
					continue
				}
			}
			filtered = append(filtered, sw)
		}
		switches = filtered
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
			poePowerW = append(poePowerW, p.PowerUsageInWatts)
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

// handleSwitchPortsOverviewBySpeed emits a table frame with one row per
// (media × speed) bucket showing the active port count at that speed.
// Distinct from handleSwitchPortsOverview (the KPI row) — this handler is
// for bar-chart / stat visualisations showing the speed distribution.
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

	var (
		mediaCol  []string
		speedCol  []string
		activeCol []int64
	)
	for _, b := range buckets {
		mediaCol = append(mediaCol, b.Media)
		speedCol = append(speedCol, b.Speed)
		activeCol = append(activeCol, b.Active)
	}

	return []*data.Frame{
		data.NewFrame("switch_ports_overview_by_speed",
			data.NewField("media", nil, mediaCol),
			data.NewField("speed", nil, speedCol),
			data.NewField("active", nil, activeCol),
		),
	}, nil
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

// handleSwitchPoe emits one row per port carrying the PoE draw in watts. We
// piggyback on the org-level `statuses/bySwitch` endpoint (same source as
// handleSwitchPorts) — it already surfaces `powerUsageInWatts` per port, so
// fetching a separate per-device statuses feed would just duplicate work and
// shard the cache per serial. Serials/networkIds filter applies client-side
// (see handleSwitchPorts for the rationale on not pushing those server-side).
func handleSwitchPoe(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("switchPoe: orgId is required")
	}
	switches, err := client.GetOrganizationSwitchPortStatuses(ctx, q.OrgID, meraki.SwitchPortStatusOptions{}, switchPoeTTL)
	if err != nil {
		return nil, err
	}

	serialFilter := stringSetOrNil(q.Serials)
	networkFilter := stringSetOrNil(q.NetworkIDs)

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
		if serialFilter != nil {
			if _, ok := serialFilter[sw.Serial]; !ok {
				continue
			}
		}
		if networkFilter != nil {
			if _, ok := networkFilter[sw.Network.ID]; !ok {
				continue
			}
		}
		for _, p := range sw.Ports {
			serials = append(serials, sw.Serial)
			switchNames = append(switchNames, sw.Name)
			networkIDs = append(networkIDs, sw.Network.ID)
			networkNames = append(networkNames, sw.Network.Name)
			portIDs = append(portIDs, p.PortID)
			enableds = append(enableds, p.Enabled)
			poeWatts = append(poeWatts, p.PowerUsageInWatts)
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

// handleSwitchVlansSummary emits one row per (switch, vlan) giving the count
// of configured ports in that VLAN on that switch. Sourced from
// `GET /organizations/{orgId}/switch/ports/bySwitch` (the config-feed variant,
// not the live statuses endpoint) so the port `type` field is authoritative.
// We include both native and voice VLANs — voice is marked via a synthetic
// "voice:<n>" prefix to distinguish from untagged VLANs.
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

	// Aggregate: (serial, vlan) → port count. Use a stable key so test order is
	// deterministic-ish (we still sort serials/vlans before emission).
	type key struct {
		Serial string
		Name   string
		Net    string
		Vlan   string
	}
	counts := make(map[key]int64)
	for _, sw := range switches {
		for _, p := range sw.Ports {
			if !p.Enabled {
				continue
			}
			if p.Vlan != 0 {
				k := key{sw.Serial, sw.Name, sw.Network.ID, strconv.Itoa(p.Vlan)}
				counts[k]++
			}
			if p.VoiceVlan != 0 {
				k := key{sw.Serial, sw.Name, sw.Network.ID, "voice:" + strconv.Itoa(p.VoiceVlan)}
				counts[k]++
			}
		}
	}

	var (
		serialsCol  []string
		switchNames []string
		networkIDs  []string
		vlans       []string
		portCounts  []int64
	)
	for k, v := range counts {
		serialsCol = append(serialsCol, k.Serial)
		switchNames = append(switchNames, k.Name)
		networkIDs = append(networkIDs, k.Net)
		vlans = append(vlans, k.Vlan)
		portCounts = append(portCounts, v)
	}

	return []*data.Frame{
		data.NewFrame("switch_vlans_summary",
			data.NewField("serial", nil, serialsCol),
			data.NewField("switchName", nil, switchNames),
			data.NewField("networkId", nil, networkIDs),
			data.NewField("vlan", nil, vlans),
			data.NewField("portCount", nil, portCounts),
		),
	}, nil
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
