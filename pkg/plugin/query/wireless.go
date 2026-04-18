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
	wirelessUsageEndpoint       = "networks/{networkId}/wireless/usageHistory"
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
