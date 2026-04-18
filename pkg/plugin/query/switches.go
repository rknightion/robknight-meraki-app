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
