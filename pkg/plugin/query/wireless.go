package query

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// Wireless cache TTLs — see todos.txt §5.5:
//   - Channel utilisation and usage history refresh at 1m (panel tiles are the
//     main consumer; minute-granularity is plenty and keeps Meraki API load low).
//   - SSIDs are a config snapshot and change rarely; 5m mirrors devicesTTL.
//   - AP clients refresh at 1m — same rationale as other live-ish reads.
const (
	wirelessChannelUtilTTL = 1 * time.Minute
	wirelessUsageTTL       = 1 * time.Minute
	networkSsidsTTL        = 5 * time.Minute
	apClientsTTL           = 1 * time.Minute

	wirelessChannelUtilEndpoint = "organizations/{organizationId}/wireless/devices/channelUtilization/history"
	wirelessUsageEndpoint       = "networks/{networkId}/clients/bandwidthUsageHistory"
)

// handleWirelessChannelUtil emits one frame per (serial, band) with Prometheus-style labels
// on the value field so Grafana's timeseries viz can infer the legend and series grouping
// without a transform chain.
//
// LabelMode: when opts.LabelMode == "name", DisplayNameFromDS is pre-baked to
// `"<AP name> / <band> GHz"` (or just `<serial> / <band> GHz` when the name is blank).
// The `band` label on the value field lets panel overrides colour the series per band.
func handleWirelessChannelUtil(ctx context.Context, client *meraki.Client, q MerakiQuery, tr TimeRange, opts Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("wirelessChannelUtil: orgId is required")
	}

	from := toRFCTime(tr.From)
	to := toRFCTime(tr.To)
	if from.IsZero() || to.IsZero() {
		return nil, fmt.Errorf("wirelessChannelUtil: time range is required")
	}

	spec, ok := meraki.KnownEndpointRanges[wirelessChannelUtilEndpoint]
	if !ok {
		return nil, fmt.Errorf("wirelessChannelUtil: missing endpoint spec")
	}
	window, err := spec.Resolve(from, to, tr.MaxDataPoints, nil)
	if err != nil {
		return nil, fmt.Errorf("wirelessChannelUtil: resolve window: %w", err)
	}

	reqOpts := meraki.WirelessChannelUtilOptions{
		NetworkIDs: q.NetworkIDs,
		Serials:    q.Serials,
		Window:     &window,
	}
	if q.Band != "" {
		reqOpts.Bands = []string{q.Band}
	}
	points, err := client.GetOrganizationWirelessChannelUtilHistory(ctx, q.OrgID, reqOpts, wirelessChannelUtilTTL)
	if err != nil {
		return nil, err
	}

	// Serial → name lookup piggybacks on the /devices cache (5m TTL) and is scoped to
	// productType=wireless so mixed orgs don't return the whole inventory.
	var nameBySerial map[string]string
	if opts.LabelMode == "name" {
		if names, lookupErr := resolveDeviceNames(ctx, client, q.OrgID, "wireless"); lookupErr == nil {
			nameBySerial = names
		}
	}

	type seriesKey struct {
		serial string
		band   string
	}
	type seriesBuf struct {
		ts     []time.Time
		values []float64
	}
	groups := make(map[seriesKey]*seriesBuf)
	for _, p := range points {
		k := seriesKey{serial: p.Serial, band: p.Band}
		buf, exists := groups[k]
		if !exists {
			buf = &seriesBuf{}
			groups[k] = buf
		}
		buf.ts = append(buf.ts, p.StartTs.UTC())
		buf.values = append(buf.values, p.Utilization)
	}

	if len(groups) == 0 {
		empty := data.NewFrame("wireless_channel_util",
			data.NewField("ts", nil, []time.Time{}),
			data.NewField("value", nil, []float64{}),
		)
		return []*data.Frame{empty}, nil
	}

	keys := make([]seriesKey, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].serial != keys[j].serial {
			return keys[i].serial < keys[j].serial
		}
		return keys[i].band < keys[j].band
	})

	frames := make([]*data.Frame, 0, len(keys))
	for _, k := range keys {
		buf := groups[k]
		sortByTime(buf.ts, buf.values)

		labels := data.Labels{
			"serial": k.serial,
			"band":   k.band,
		}

		valueField := data.NewField("value", labels, buf.values)

		// DisplayNameFromDS is a pre-formatted final string — Grafana does NOT template-
		// substitute it, so bake the legend here. We want "<name> / <band> GHz" when a
		// name is resolvable, otherwise "<serial> / <band> GHz".
		displayName := k.serial
		if nameBySerial != nil {
			if name := nameBySerial[k.serial]; name != "" {
				displayName = name
			}
		}
		displayName = fmt.Sprintf("%s / %s GHz", displayName, k.band)

		valueField.Config = &data.FieldConfig{
			DisplayNameFromDS: displayName,
			Unit:              "percent",
		}

		frames = append(frames, data.NewFrame("wireless_channel_util",
			data.NewField("ts", nil, buf.ts),
			valueField,
		))
	}
	return frames, nil
}

// handleWirelessUsage fetches per-network wireless usage history and emits one frame per
// network with `totalKbps` as the value field. When q.NetworkIDs has more than one entry
// we loop (the endpoint is network-scoped) and accumulate frames. The network name is
// resolved via GetOrganizationNetworks for a human-readable legend.
//
// Labels on the value field: {"network_id": ..., "network_name": ..., "metric": "totalKbps"}.
func handleWirelessUsage(ctx context.Context, client *meraki.Client, q MerakiQuery, tr TimeRange, opts Options) ([]*data.Frame, error) {
	_ = opts // usage history is keyed by network, not serial — LabelMode is not relevant.
	if q.OrgID == "" {
		return nil, fmt.Errorf("wirelessUsage: orgId is required")
	}
	if len(q.NetworkIDs) == 0 {
		return nil, fmt.Errorf("wirelessUsage: at least one networkId is required")
	}

	from := toRFCTime(tr.From)
	to := toRFCTime(tr.To)
	if from.IsZero() || to.IsZero() {
		return nil, fmt.Errorf("wirelessUsage: time range is required")
	}

	spec, ok := meraki.KnownEndpointRanges[wirelessUsageEndpoint]
	if !ok {
		return nil, fmt.Errorf("wirelessUsage: missing endpoint spec")
	}
	window, err := spec.Resolve(from, to, tr.MaxDataPoints, nil)
	if err != nil {
		return nil, fmt.Errorf("wirelessUsage: resolve window: %w", err)
	}

	// Preload network names so the legend is human-readable. A failure here is
	// non-fatal — we fall back to using the network ID as the display name.
	nameByID := make(map[string]string)
	if networks, lookupErr := client.GetOrganizationNetworks(ctx, q.OrgID, []string{"wireless"}, networksTTL); lookupErr == nil {
		for _, n := range networks {
			nameByID[n.ID] = n.Name
		}
	}

	// Sort networkIDs so frame order is deterministic (helps tests and stable legends).
	networkIDs := make([]string, len(q.NetworkIDs))
	copy(networkIDs, q.NetworkIDs)
	sort.Strings(networkIDs)

	frames := make([]*data.Frame, 0, len(networkIDs))
	var firstErr error
	for _, networkID := range networkIDs {
		reqOpts := meraki.WirelessUsageOptions{
			Window: &window,
		}
		points, usageErr := client.GetNetworkWirelessUsageHistory(ctx, networkID, reqOpts, wirelessUsageTTL)
		if usageErr != nil {
			if firstErr == nil {
				firstErr = usageErr
			}
			continue
		}
		if len(points) == 0 {
			continue
		}

		ts := make([]time.Time, 0, len(points))
		totals := make([]float64, 0, len(points))
		for _, p := range points {
			ts = append(ts, p.StartTs.UTC())
			totals = append(totals, p.TotalKbps)
		}

		networkName := nameByID[networkID]
		labels := data.Labels{
			"network_id": networkID,
			"metric":     "totalKbps",
		}
		if networkName != "" {
			labels["network_name"] = networkName
		}

		displayName := networkName
		if displayName == "" {
			displayName = networkID
		}

		valueField := data.NewField("value", labels, totals)
		valueField.Config = &data.FieldConfig{
			DisplayNameFromDS: displayName,
			Unit:              "Kbits",
		}

		frames = append(frames, data.NewFrame("wireless_usage_history",
			data.NewField("ts", nil, ts),
			valueField,
		))
	}

	if len(frames) == 0 {
		// Return an empty-but-named frame so the panel surface stays populated and can
		// render a "No data" notice rather than a missing-frame error.
		empty := data.NewFrame("wireless_usage_history",
			data.NewField("ts", nil, []time.Time{}),
			data.NewField("value", nil, []float64{}),
		)
		if firstErr != nil {
			return []*data.Frame{empty}, firstErr
		}
		return []*data.Frame{empty}, nil
	}
	return frames, firstErr
}

// handleNetworkSsids emits one table-shaped frame concatenating SSID rows across every
// network in q.NetworkIDs. No LabelMode needed — the `name` column is already a human
// string.
func handleNetworkSsids(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if len(q.NetworkIDs) == 0 {
		return nil, fmt.Errorf("networkSsids: at least one networkId is required")
	}

	networkIDs := make([]string, len(q.NetworkIDs))
	copy(networkIDs, q.NetworkIDs)
	sort.Strings(networkIDs)

	var (
		numbers     []int64
		names       []string
		enableds    []bool
		splashPages []string
		authModes   []string
		netIDs      []string
		firstErr    error
	)
	for _, networkID := range networkIDs {
		ssids, err := client.GetNetworkWirelessSsids(ctx, networkID, networkSsidsTTL)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, s := range ssids {
			numbers = append(numbers, int64(s.Number))
			names = append(names, s.Name)
			enableds = append(enableds, s.Enabled)
			splashPages = append(splashPages, s.SplashPage)
			authModes = append(authModes, s.AuthMode)
			netIDs = append(netIDs, networkID)
		}
	}

	frame := data.NewFrame("network_ssids",
		data.NewField("number", nil, numbers),
		data.NewField("name", nil, names),
		data.NewField("enabled", nil, enableds),
		data.NewField("splashPage", nil, splashPages),
		data.NewField("authMode", nil, authModes),
		data.NewField("networkId", nil, netIDs),
	)
	return []*data.Frame{frame}, firstErr
}

// handleApClients emits one table-shaped frame with rows concatenated across every serial
// in q.Serials. Each row represents one client currently associated with one AP.
//
// Note: Meraki's `/devices/{serial}/clients` endpoint does NOT return rssi/ssid/status —
// those live on separate wireless-specific endpoints. We surface the fields the endpoint
// actually provides (mac, ip, user, vlan, description, switchport, sent/recv bytes).
func handleApClients(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if len(q.Serials) == 0 {
		return nil, fmt.Errorf("apClients: at least one serial is required")
	}

	timespan := time.Duration(q.TimespanSeconds) * time.Second

	serials := make([]string, len(q.Serials))
	copy(serials, q.Serials)
	sort.Strings(serials)

	var (
		serialCol      []string
		macs           []string
		ips            []string
		users          []string
		vlans          []string
		descriptions   []string
		switchports    []string
		sentKB         []float64
		recvKB         []float64
		firstErr       error
	)
	for _, serial := range serials {
		clients, err := client.GetDeviceWirelessClients(ctx, serial, timespan, apClientsTTL)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, c := range clients {
			serialCol = append(serialCol, serial)
			macs = append(macs, c.MAC)
			ips = append(ips, c.IP)
			users = append(users, c.User)
			vlans = append(vlans, c.VLAN)
			descriptions = append(descriptions, c.Description)
			switchports = append(switchports, c.Switchport)
			sentKB = append(sentKB, c.Usage.Sent)
			recvKB = append(recvKB, c.Usage.Recv)
		}
	}

	frame := data.NewFrame("ap_clients",
		data.NewField("serial", nil, serialCol),
		data.NewField("mac", nil, macs),
		data.NewField("ip", nil, ips),
		data.NewField("user", nil, users),
		data.NewField("vlan", nil, vlans),
		data.NewField("description", nil, descriptions),
		data.NewField("switchport", nil, switchports),
		data.NewField("sentKB", nil, sentKB),
		data.NewField("recvKB", nil, recvKB),
	)
	return []*data.Frame{frame}, firstErr
}

// ---------------------------------------------------------------------------
// §2.1 — Org-level AP client counts
// ---------------------------------------------------------------------------

// wirelessApClientCountsTTL: live-ish read, same as apClientsTTL.
const wirelessApClientCountsTTL = 1 * time.Minute

// handleWirelessApClientCounts emits one flat table frame with one row per
// wireless device, showing the number of currently-associated clients.
//
// The endpoint GET /organizations/{organizationId}/wireless/clients/overview/byDevice
// returns: [{"network":{"id":"N_..."}, "serial":"Q2...", "counts":{"byStatus":{"online":N}}}]
//
// A best-effort GetOrganizationNetworks call is used to resolve network names;
// failures fall back to the raw network ID without failing the query.
func handleWirelessApClientCounts(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("wirelessApClientCounts: orgId is required")
	}

	opts := meraki.WirelessApClientCountsOptions{
		NetworkIDs: q.NetworkIDs,
		Serials:    q.Serials,
	}
	counts, err := client.GetOrganizationWirelessClientsOverviewByDevice(ctx, q.OrgID, opts, wirelessApClientCountsTTL)
	if err != nil {
		return nil, err
	}

	// Best-effort network name lookup.
	nameByNetworkID := make(map[string]string)
	if networks, lookupErr := client.GetOrganizationNetworks(ctx, q.OrgID, []string{"wireless"}, networksTTL); lookupErr == nil {
		for _, n := range networks {
			nameByNetworkID[n.ID] = n.Name
		}
	}

	serials := make([]string, 0, len(counts))
	networkIDs := make([]string, 0, len(counts))
	networkNames := make([]string, 0, len(counts))
	online := make([]int64, 0, len(counts))

	for _, c := range counts {
		serials = append(serials, c.Serial)
		networkIDs = append(networkIDs, c.NetworkID)
		networkNames = append(networkNames, nameByNetworkID[c.NetworkID])
		online = append(online, c.OnlineCount)
	}

	frame := data.NewFrame("wireless_ap_client_counts",
		data.NewField("serial", nil, serials),
		data.NewField("networkId", nil, networkIDs),
		data.NewField("networkName", nil, networkNames),
		data.NewField("online", nil, online),
	)
	return []*data.Frame{frame}, nil
}

// ---------------------------------------------------------------------------
// §3.2 — Wireless packet loss by network
// ---------------------------------------------------------------------------

const wirelessPacketLossTTL = 1 * time.Minute

const wirelessPacketLossEndpoint = "organizations/{organizationId}/wireless/devices/packetLoss/byNetwork"

// handleWirelessPacketLossByNetwork emits one flat table frame with one row per
// network, showing downstream/upstream/total packet-loss percentages.
//
// The endpoint GET /organizations/{organizationId}/wireless/devices/packetLoss/byNetwork
// supports a t0/t1 window with a 90-day MaxTimespan; no resolution parameter.
// A best-effort GetOrganizationNetworks call is used to resolve network names.
func handleWirelessPacketLossByNetwork(ctx context.Context, client *meraki.Client, q MerakiQuery, tr TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("wirelessPacketLossByNetwork: orgId is required")
	}

	from := toRFCTime(tr.From)
	to := toRFCTime(tr.To)

	var window *meraki.TimeRangeWindow
	if !from.IsZero() && !to.IsZero() {
		spec, ok := meraki.KnownEndpointRanges[wirelessPacketLossEndpoint]
		if !ok {
			return nil, fmt.Errorf("wirelessPacketLossByNetwork: missing endpoint spec")
		}
		w, err := spec.Resolve(from, to, tr.MaxDataPoints, nil)
		if err != nil {
			return nil, fmt.Errorf("wirelessPacketLossByNetwork: resolve window: %w", err)
		}
		window = &w
	}

	opts := meraki.WirelessPacketLossOptions{
		NetworkIDs: q.NetworkIDs,
		Serials:    q.Serials,
		Window:     window,
	}
	rows, err := client.GetOrganizationWirelessPacketLossByNetwork(ctx, q.OrgID, opts, wirelessPacketLossTTL)
	if err != nil {
		return nil, err
	}

	// Best-effort network name lookup.
	nameByNetworkID := make(map[string]string)
	if networks, lookupErr := client.GetOrganizationNetworks(ctx, q.OrgID, []string{"wireless"}, networksTTL); lookupErr == nil {
		for _, n := range networks {
			nameByNetworkID[n.ID] = n.Name
		}
	}

	networkIDs := make([]string, 0, len(rows))
	networkNames := make([]string, 0, len(rows))
	dsTotal := make([]int64, 0, len(rows))
	dsLost := make([]int64, 0, len(rows))
	dsLossPct := make([]float64, 0, len(rows))
	usTotal := make([]int64, 0, len(rows))
	usLost := make([]int64, 0, len(rows))
	usLossPct := make([]float64, 0, len(rows))
	totTotal := make([]int64, 0, len(rows))
	totLost := make([]int64, 0, len(rows))
	totLossPct := make([]float64, 0, len(rows))

	for _, r := range rows {
		networkIDs = append(networkIDs, r.NetworkID)
		networkNames = append(networkNames, nameByNetworkID[r.NetworkID])
		if r.Downstream != nil {
			dsTotal = append(dsTotal, r.Downstream.TotalPackets)
			dsLost = append(dsLost, r.Downstream.LostPackets)
			dsLossPct = append(dsLossPct, r.Downstream.LossPercent)
		} else {
			dsTotal = append(dsTotal, 0)
			dsLost = append(dsLost, 0)
			dsLossPct = append(dsLossPct, 0)
		}
		if r.Upstream != nil {
			usTotal = append(usTotal, r.Upstream.TotalPackets)
			usLost = append(usLost, r.Upstream.LostPackets)
			usLossPct = append(usLossPct, r.Upstream.LossPercent)
		} else {
			usTotal = append(usTotal, 0)
			usLost = append(usLost, 0)
			usLossPct = append(usLossPct, 0)
		}
		if r.Total != nil {
			totTotal = append(totTotal, r.Total.TotalPackets)
			totLost = append(totLost, r.Total.LostPackets)
			totLossPct = append(totLossPct, r.Total.LossPercent)
		} else {
			totTotal = append(totTotal, 0)
			totLost = append(totLost, 0)
			totLossPct = append(totLossPct, 0)
		}
	}

	frame := data.NewFrame("wireless_packet_loss_by_network",
		data.NewField("networkId", nil, networkIDs),
		data.NewField("networkName", nil, networkNames),
		data.NewField("downstreamTotal", nil, dsTotal),
		data.NewField("downstreamLost", nil, dsLost),
		data.NewField("downstreamLossPct", nil, dsLossPct),
		data.NewField("upstreamTotal", nil, usTotal),
		data.NewField("upstreamLost", nil, usLost),
		data.NewField("upstreamLossPct", nil, usLossPct),
		data.NewField("totalPackets", nil, totTotal),
		data.NewField("totalLost", nil, totLost),
		data.NewField("totalLossPct", nil, totLossPct),
	)
	return []*data.Frame{frame}, nil
}

// ---------------------------------------------------------------------------
// §3.2 — Wireless ethernet statuses
// ---------------------------------------------------------------------------

const wirelessEthernetStatusesTTL = 1 * time.Minute

// handleWirelessDevicesEthernetStatuses emits one flat table frame with one
// row per wireless device, showing ethernet port speed/duplex/PoE status and
// power source (ac | poe | unknown).
//
// The endpoint GET /organizations/{organizationId}/wireless/devices/ethernet/statuses
// is a snapshot (no time parameters). A best-effort device-name lookup is
// performed to populate a human-readable "name" column.
func handleWirelessDevicesEthernetStatuses(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("wirelessDevicesEthernetStatuses: orgId is required")
	}

	opts := meraki.WirelessDeviceEthernetOptions{
		NetworkIDs: q.NetworkIDs,
		Serials:    q.Serials,
	}
	rows, err := client.GetOrganizationWirelessDevicesEthernetStatuses(ctx, q.OrgID, opts, wirelessEthernetStatusesTTL)
	if err != nil {
		return nil, err
	}

	serials := make([]string, 0, len(rows))
	names := make([]string, 0, len(rows))
	networkIDs := make([]string, 0, len(rows))
	models := make([]string, 0, len(rows))
	powers := make([]string, 0, len(rows))
	primarySpeed := make([]string, 0, len(rows))
	primaryDuplex := make([]string, 0, len(rows))
	primaryPoe := make([]bool, 0, len(rows))

	for _, r := range rows {
		serials = append(serials, r.Serial)
		names = append(names, r.Name)
		networkIDs = append(networkIDs, r.NetworkID)
		models = append(models, r.Model)
		powers = append(powers, r.Power)
		primarySpeed = append(primarySpeed, r.Primary.Speed)
		primaryDuplex = append(primaryDuplex, r.Primary.Duplex)
		primaryPoe = append(primaryPoe, r.Primary.PoeEnabled)
	}

	frame := data.NewFrame("wireless_ethernet_statuses",
		data.NewField("serial", nil, serials),
		data.NewField("name", nil, names),
		data.NewField("networkId", nil, networkIDs),
		data.NewField("model", nil, models),
		data.NewField("power", nil, powers),
		data.NewField("primarySpeed", nil, primarySpeed),
		data.NewField("primaryDuplex", nil, primaryDuplex),
		data.NewField("primaryPoe", nil, primaryPoe),
	)
	return []*data.Frame{frame}, nil
}

// ---------------------------------------------------------------------------
// §3.2 — Wireless AP CPU load history
// ---------------------------------------------------------------------------

const wirelessCpuLoadTTL = 1 * time.Minute

const wirelessCpuLoadEndpoint = "organizations/{organizationId}/wireless/devices/system/cpu/load/history"

// handleWirelessDevicesCpuLoadHistory emits one timeseries frame per AP serial.
// Each frame has a `ts` time field and a `value` float64 field with labels
// {"serial": ...} and a baked DisplayNameFromDS so the legend shows the AP name
// (or serial when the name is unavailable).
//
// The endpoint GET /organizations/{organizationId}/wireless/devices/system/cpu/load/history
// caps the time window to 1 day. As of 2026-04 the endpoint no longer accepts an
// interval parameter and samples are returned at natural cadence (~5 min).
func handleWirelessDevicesCpuLoadHistory(ctx context.Context, client *meraki.Client, q MerakiQuery, tr TimeRange, opts Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("wirelessDevicesCpuLoadHistory: orgId is required")
	}

	from := toRFCTime(tr.From)
	to := toRFCTime(tr.To)
	if from.IsZero() || to.IsZero() {
		return nil, fmt.Errorf("wirelessDevicesCpuLoadHistory: time range is required")
	}

	spec, ok := meraki.KnownEndpointRanges[wirelessCpuLoadEndpoint]
	if !ok {
		return nil, fmt.Errorf("wirelessDevicesCpuLoadHistory: missing endpoint spec")
	}
	window, err := spec.Resolve(from, to, tr.MaxDataPoints, nil)
	if err != nil {
		return nil, fmt.Errorf("wirelessDevicesCpuLoadHistory: resolve window: %w", err)
	}

	reqOpts := meraki.WirelessCpuLoadOptions{
		NetworkIDs: q.NetworkIDs,
		Serials:    q.Serials,
		Window:     &window,
	}
	points, err := client.GetOrganizationWirelessDevicesCpuLoadHistory(ctx, q.OrgID, reqOpts, wirelessCpuLoadTTL)
	if err != nil {
		return nil, err
	}

	// Best-effort name lookup for legend labels.
	var nameBySerial map[string]string
	if opts.LabelMode == "name" {
		if names, lookupErr := resolveDeviceNames(ctx, client, q.OrgID, "wireless"); lookupErr == nil {
			nameBySerial = names
		}
	}

	// Group points by serial.
	type seriesBuf struct {
		ts     []time.Time
		values []float64
	}
	groups := make(map[string]*seriesBuf)
	seriesOrder := make([]string, 0)
	for _, p := range points {
		if _, exists := groups[p.Serial]; !exists {
			groups[p.Serial] = &seriesBuf{}
			seriesOrder = append(seriesOrder, p.Serial)
		}
		buf := groups[p.Serial]
		buf.ts = append(buf.ts, p.StartTs.UTC())
		buf.values = append(buf.values, p.CpuLoad5)
	}

	if len(groups) == 0 {
		empty := data.NewFrame("wireless_cpu_load_history",
			data.NewField("ts", nil, []time.Time{}),
			data.NewField("value", nil, []float64{}),
		)
		return []*data.Frame{empty}, nil
	}

	sort.Strings(seriesOrder)

	frames := make([]*data.Frame, 0, len(seriesOrder))
	for _, serial := range seriesOrder {
		buf := groups[serial]
		sortByTime(buf.ts, buf.values)

		labels := data.Labels{"serial": serial}
		valueField := data.NewField("value", labels, buf.values)

		displayName := serial
		if nameBySerial != nil {
			if name := nameBySerial[serial]; name != "" {
				displayName = name
			}
		}
		valueField.Config = &data.FieldConfig{
			DisplayNameFromDS: displayName,
			Unit:              "percent",
		}

		frames = append(frames, data.NewFrame("wireless_cpu_load_history",
			data.NewField("ts", nil, buf.ts),
			valueField,
		))
	}
	return frames, nil
}
