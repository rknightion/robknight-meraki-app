package query

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// Phase 8 (MX security appliances + VPN) handlers.
//
// Cache TTLs mirror the conventions set by switch/wireless/alerts handlers:
// live status feeds refresh at 30s–1m; config-style snapshots (settings, port
// forwarding rules) refresh at 5 minutes. The VPN stats endpoint aggregates
// over a window and is keyed by (serial, peer-pair), which makes it safe to
// cache at 1 minute without risking stale chart data inside a panel refresh.
const (
	applianceUplinkStatusesTTL  = 30 * time.Second
	applianceUplinksOverviewTTL = 30 * time.Second
	applianceVpnStatusesTTL     = 30 * time.Second
	applianceVpnStatsTTL        = 1 * time.Minute
	deviceUplinkLossLatencyTTL  = 30 * time.Second
	portForwardingTTL           = 5 * time.Minute
	applianceSettingsTTL        = 5 * time.Minute

	applianceVpnStatsEndpoint       = "organizations/{organizationId}/appliance/vpn/stats"
	deviceUplinkLossLatencyEndpoint = "organizations/{organizationId}/devices/uplinksLossAndLatency"
)

// handleApplianceUplinkStatuses flattens the nested uplinks array so each row
// represents one (serial, interface) pair. The Meraki feed returns one entry
// per appliance with 1–3 uplinks embedded; the dashboard tables want a single
// flat table so the UI can sort by status across all appliances without a
// client-side expand transform.
//
// Network name resolution is best-effort: we warm a serial→name map via
// GetOrganizationNetworks (productTypes=[appliance]) and tolerate errors by
// falling back to an empty string — otherwise a bad /networks fetch would
// silently hide valid uplink rows.
func handleApplianceUplinkStatuses(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, opts Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("applianceUplinkStatuses: orgId is required")
	}

	reqOpts := meraki.ApplianceUplinkOptions{
		NetworkIDs: q.NetworkIDs,
		Serials:    q.Serials,
	}
	entries, err := client.GetOrganizationApplianceUplinkStatuses(ctx, q.OrgID, reqOpts, applianceUplinkStatusesTTL)
	if err != nil {
		return nil, err
	}

	// Best-effort networkID→name map (wireless/usage handler uses the same
	// tolerate-on-error pattern). Limit to the appliance productType so mixed
	// orgs don't pay for the full inventory here.
	networkNameByID := map[string]string{}
	if networks, lookupErr := client.GetOrganizationNetworks(ctx, q.OrgID, []string{"appliance"}, networksTTL); lookupErr == nil {
		for _, n := range networks {
			networkNameByID[n.ID] = n.Name
		}
	}

	var (
		serials        []string
		models         []string
		networkIDs     []string
		networkNames   []string
		lastReporteds  []time.Time
		interfaces     []string
		statuses       []string
		ips            []string
		gateways       []string
		publicIPs      []string
		primaryDns     []string
		secondaryDns   []string
		ipAssignedBys  []string
		iccids         []string
		providers      []string
		signalTypes    []string
		connTypes      []string
		rsrps          []string
		rsrqs          []string
		apns           []string
		drilldowns     []string
	)

	for _, entry := range entries {
		last := time.Time{}
		if entry.LastReportedAt != nil {
			last = entry.LastReportedAt.UTC()
		}
		for _, u := range entry.Uplinks {
			serials = append(serials, entry.Serial)
			models = append(models, entry.Model)
			networkIDs = append(networkIDs, entry.NetworkID)
			networkNames = append(networkNames, networkNameByID[entry.NetworkID])
			lastReporteds = append(lastReporteds, last)
			interfaces = append(interfaces, u.Interface)
			statuses = append(statuses, u.Status)
			ips = append(ips, u.IP)
			gateways = append(gateways, u.Gateway)
			publicIPs = append(publicIPs, u.PublicIP)
			primaryDns = append(primaryDns, u.PrimaryDns)
			secondaryDns = append(secondaryDns, u.SecondaryDns)
			ipAssignedBys = append(ipAssignedBys, u.IPAssignedBy)
			iccids = append(iccids, u.ICCID)
			providers = append(providers, u.Provider)
			signalTypes = append(signalTypes, u.SignalType)
			connTypes = append(connTypes, u.ConnectionType)
			var rsrp, rsrq string
			if u.SignalStat != nil {
				rsrp = u.SignalStat.RSRP
				rsrq = u.SignalStat.RSRQ
			}
			rsrps = append(rsrps, rsrp)
			rsrqs = append(rsrqs, rsrq)
			apns = append(apns, u.APN)
			drilldowns = append(drilldowns, deviceDrilldownURL(opts.PluginPathPrefix, "appliance", entry.Serial))
		}
	}

	return []*data.Frame{
		data.NewFrame("appliance_uplink_statuses",
			data.NewField("serial", nil, serials),
			data.NewField("model", nil, models),
			data.NewField("networkId", nil, networkIDs),
			data.NewField("networkName", nil, networkNames),
			data.NewField("lastReportedAt", nil, lastReporteds),
			data.NewField("interface", nil, interfaces),
			data.NewField("status", nil, statuses),
			data.NewField("ip", nil, ips),
			data.NewField("gateway", nil, gateways),
			data.NewField("publicIp", nil, publicIPs),
			data.NewField("primaryDns", nil, primaryDns),
			data.NewField("secondaryDns", nil, secondaryDns),
			data.NewField("ipAssignedBy", nil, ipAssignedBys),
			data.NewField("iccid", nil, iccids),
			data.NewField("provider", nil, providers),
			data.NewField("signalType", nil, signalTypes),
			data.NewField("connectionType", nil, connTypes),
			data.NewField("rsrp", nil, rsrps),
			data.NewField("rsrq", nil, rsrqs),
			data.NewField("apn", nil, apns),
			data.NewField("drilldownUrl", nil, drilldowns),
		),
	}, nil
}

// handleApplianceUplinksOverview emits the org-wide status-count KPI frame.
// Wide one-row layout mirrors handleAlertsOverview and handleSensorAlertSummary
// — each KPI is its own field so panels bind directly to the column and we
// avoid the filterByValue+reduce transform chain (§G.20).
func handleApplianceUplinksOverview(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("applianceUplinksOverview: orgId is required")
	}
	overview, err := client.GetOrganizationApplianceUplinksOverview(ctx, q.OrgID, applianceUplinksOverviewTTL)
	if err != nil {
		return nil, err
	}
	var active, ready, failed, notConnected int64
	if overview != nil {
		active = overview.Counts.ByStatus.Active
		ready = overview.Counts.ByStatus.Ready
		failed = overview.Counts.ByStatus.Failed
		notConnected = overview.Counts.ByStatus.NotConnected
	}
	total := active + ready + failed + notConnected

	return []*data.Frame{
		data.NewFrame("appliance_uplinks_overview",
			data.NewField("active", nil, []int64{active}),
			data.NewField("ready", nil, []int64{ready}),
			data.NewField("failed", nil, []int64{failed}),
			data.NewField("notConnected", nil, []int64{notConnected}),
			data.NewField("total", nil, []int64{total}),
		),
	}, nil
}

// handleApplianceVpnStatuses flattens the nested peer arrays so each row is
// one peer-pair. Meraki returns one entry per network with two peer arrays
// (merakiVpnPeers, thirdPartyVpnPeers); the UI's VPN status table wants one
// row per peer so operators can see each tunnel's reachability at a glance.
//
// `peerKind` distinguishes the two origin arrays so downstream overrides can
// colour them differently; `peerNetworkId` stays blank for thirdParty peers
// because Meraki doesn't model them as networks — only a name + publicIp.
func handleApplianceVpnStatuses(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("applianceVpnStatuses: orgId is required")
	}
	reqOpts := meraki.ApplianceVpnStatusOptions{NetworkIDs: q.NetworkIDs}
	entries, err := client.GetOrganizationApplianceVpnStatuses(ctx, q.OrgID, reqOpts, applianceVpnStatusesTTL)
	if err != nil {
		return nil, err
	}

	var (
		sourceNetworkIDs   []string
		sourceNetworkNames []string
		sourceSerials      []string
		sourceStatuses     []string
		vpnModes           []string
		peerKinds          []string
		peerNetworkIDs     []string
		peerNetworkNames   []string
		peerPublicIPs      []string
		reachabilities     []string
		sentKB             []int64
		recvKB             []int64
	)

	for _, entry := range entries {
		for _, peer := range entry.MerakiVpnPeers {
			sourceNetworkIDs = append(sourceNetworkIDs, entry.NetworkID)
			sourceNetworkNames = append(sourceNetworkNames, entry.NetworkName)
			sourceSerials = append(sourceSerials, entry.DeviceSerial)
			sourceStatuses = append(sourceStatuses, entry.DeviceStatus)
			vpnModes = append(vpnModes, entry.VpnMode)
			peerKinds = append(peerKinds, "meraki")
			peerNetworkIDs = append(peerNetworkIDs, peer.NetworkID)
			peerNetworkNames = append(peerNetworkNames, peer.NetworkName)
			peerPublicIPs = append(peerPublicIPs, "")
			reachabilities = append(reachabilities, peer.Reachability)
			var sent, recv int64
			if peer.UsageSummary != nil {
				sent = peer.UsageSummary.SentKilobytes
				recv = peer.UsageSummary.ReceivedKilobytes
			}
			sentKB = append(sentKB, sent)
			recvKB = append(recvKB, recv)
		}
		for _, peer := range entry.ThirdPartyVpnPeers {
			sourceNetworkIDs = append(sourceNetworkIDs, entry.NetworkID)
			sourceNetworkNames = append(sourceNetworkNames, entry.NetworkName)
			sourceSerials = append(sourceSerials, entry.DeviceSerial)
			sourceStatuses = append(sourceStatuses, entry.DeviceStatus)
			vpnModes = append(vpnModes, entry.VpnMode)
			peerKinds = append(peerKinds, "thirdParty")
			peerNetworkIDs = append(peerNetworkIDs, "")
			peerNetworkNames = append(peerNetworkNames, peer.Name)
			peerPublicIPs = append(peerPublicIPs, peer.PublicIP)
			reachabilities = append(reachabilities, peer.Reachability)
			sentKB = append(sentKB, 0)
			recvKB = append(recvKB, 0)
		}
	}

	return []*data.Frame{
		data.NewFrame("appliance_vpn_statuses",
			data.NewField("sourceNetworkId", nil, sourceNetworkIDs),
			data.NewField("sourceNetworkName", nil, sourceNetworkNames),
			data.NewField("sourceSerial", nil, sourceSerials),
			data.NewField("sourceDeviceStatus", nil, sourceStatuses),
			data.NewField("vpnMode", nil, vpnModes),
			data.NewField("peerKind", nil, peerKinds),
			data.NewField("peerNetworkId", nil, peerNetworkIDs),
			data.NewField("peerNetworkName", nil, peerNetworkNames),
			data.NewField("peerPublicIp", nil, peerPublicIPs),
			data.NewField("reachability", nil, reachabilities),
			data.NewField("sentKilobytes", nil, sentKB),
			data.NewField("receivedKilobytes", nil, recvKB),
		),
	}, nil
}

// vpnStatsPairKey groups the four per-peer summary arrays by their
// (senderUplink, receiverUplink) tuple so we can merge them into one row per
// uplink-pair.
type vpnStatsPairKey struct {
	sender   string
	receiver string
}

// vpnStatsPairRow is the merged stats row for one peer-pair. Missing summary
// types leave their field at 0 — the table UI uses format:short so zero
// latency renders as "0 ms" rather than "no data", which is consistent with
// the rest of the v0.2 KPI frames.
type vpnStatsPairRow struct {
	sourceNetworkID   string
	sourceNetworkName string
	peerNetworkID     string
	peerNetworkName   string
	sender            string
	receiver          string
	avgLatencyMs      float64
	avgJitter         float64
	avgLossPercentage float64
	avgMos            float64
	sentKB            int64
	recvKB            int64
}

// handleApplianceVpnStats flattens each entry's merakiVpnPeers × summary
// arrays into one row per (source network, peer network, sender uplink,
// receiver uplink). The four summary arrays (latency/jitter/loss/mos) each
// key on (senderUplink, receiverUplink); we merge them client-side so the
// frame keeps a single row per tunnel pair rather than four sparse rows.
//
// Truncation annotation: when the panel's requested window exceeds the 31-day
// endpoint cap, the meraki.EndpointTimeRange.Resolve() marks the window as
// Truncated and returns an annotation; we surface that annotation as a frame
// notice so the UI can show "window truncated" without failing the panel.
func handleApplianceVpnStats(ctx context.Context, client *meraki.Client, q MerakiQuery, tr TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("applianceVpnStats: orgId is required")
	}

	from := toRFCTime(tr.From)
	to := toRFCTime(tr.To)
	if from.IsZero() || to.IsZero() {
		return nil, fmt.Errorf("applianceVpnStats: time range is required")
	}

	spec, ok := meraki.KnownEndpointRanges[applianceVpnStatsEndpoint]
	if !ok {
		return nil, fmt.Errorf("applianceVpnStats: missing endpoint spec")
	}
	window, err := spec.Resolve(from, to, tr.MaxDataPoints, nil)
	if err != nil {
		return nil, fmt.Errorf("applianceVpnStats: resolve window: %w", err)
	}

	reqOpts := meraki.ApplianceVpnStatsOptions{
		NetworkIDs: q.NetworkIDs,
		Window:     &window,
	}
	entries, err := client.GetOrganizationApplianceVpnStats(ctx, q.OrgID, reqOpts, applianceVpnStatsTTL)
	if err != nil {
		return nil, err
	}

	rows := make([]vpnStatsPairRow, 0)
	for _, entry := range entries {
		for _, peer := range entry.MerakiVpnPeers {
			// Merge the four summary arrays by their (sender, receiver) key.
			// Map allocation is small per peer so we do it inline.
			merged := map[vpnStatsPairKey]*vpnStatsPairRow{}
			getRow := func(k vpnStatsPairKey) *vpnStatsPairRow {
				r, exists := merged[k]
				if !exists {
					r = &vpnStatsPairRow{
						sourceNetworkID:   entry.NetworkID,
						sourceNetworkName: entry.NetworkName,
						peerNetworkID:     peer.NetworkID,
						peerNetworkName:   peer.NetworkName,
						sender:            k.sender,
						receiver:          k.receiver,
					}
					if peer.UsageSummary != nil {
						r.sentKB = peer.UsageSummary.SentKilobytes
						r.recvKB = peer.UsageSummary.ReceivedKilobytes
					}
					merged[k] = r
				}
				return r
			}
			for _, s := range peer.LatencySummaries {
				getRow(vpnStatsPairKey{sender: s.SenderUplink, receiver: s.ReceiverUplink}).avgLatencyMs = s.AvgLatencyMs
			}
			for _, s := range peer.JitterSummaries {
				getRow(vpnStatsPairKey{sender: s.SenderUplink, receiver: s.ReceiverUplink}).avgJitter = s.AvgJitter
			}
			for _, s := range peer.LossPercentageSummaries {
				getRow(vpnStatsPairKey{sender: s.SenderUplink, receiver: s.ReceiverUplink}).avgLossPercentage = s.AvgLossPercentage
			}
			for _, s := range peer.MosSummaries {
				getRow(vpnStatsPairKey{sender: s.SenderUplink, receiver: s.ReceiverUplink}).avgMos = s.AvgMos
			}
			// Sort pair keys so frame output is deterministic.
			keys := make([]vpnStatsPairKey, 0, len(merged))
			for k := range merged {
				keys = append(keys, k)
			}
			sort.Slice(keys, func(i, j int) bool {
				if keys[i].sender != keys[j].sender {
					return keys[i].sender < keys[j].sender
				}
				return keys[i].receiver < keys[j].receiver
			})
			for _, k := range keys {
				rows = append(rows, *merged[k])
			}
		}
	}

	var (
		sourceIDs         []string
		sourceNms         []string
		peerIDs           []string
		peerNms           []string
		senders           []string
		receivers         []string
		avgLatency        []float64
		avgJitter         []float64
		avgLoss           []float64
		avgMos            []float64
		sentKilobytes     []int64
		receivedKilobytes []int64
	)
	for _, r := range rows {
		sourceIDs = append(sourceIDs, r.sourceNetworkID)
		sourceNms = append(sourceNms, r.sourceNetworkName)
		peerIDs = append(peerIDs, r.peerNetworkID)
		peerNms = append(peerNms, r.peerNetworkName)
		senders = append(senders, r.sender)
		receivers = append(receivers, r.receiver)
		avgLatency = append(avgLatency, r.avgLatencyMs)
		avgJitter = append(avgJitter, r.avgJitter)
		avgLoss = append(avgLoss, r.avgLossPercentage)
		avgMos = append(avgMos, r.avgMos)
		sentKilobytes = append(sentKilobytes, r.sentKB)
		receivedKilobytes = append(receivedKilobytes, r.recvKB)
	}

	frame := data.NewFrame("appliance_vpn_stats",
		data.NewField("sourceNetworkId", nil, sourceIDs),
		data.NewField("sourceNetworkName", nil, sourceNms),
		data.NewField("peerNetworkId", nil, peerIDs),
		data.NewField("peerNetworkName", nil, peerNms),
		data.NewField("senderUplink", nil, senders),
		data.NewField("receiverUplink", nil, receivers),
		data.NewField("avgLatencyMs", nil, avgLatency),
		data.NewField("avgJitter", nil, avgJitter),
		data.NewField("avgLossPercentage", nil, avgLoss),
		data.NewField("avgMos", nil, avgMos),
		data.NewField("sentKilobytes", nil, sentKilobytes),
		data.NewField("receivedKilobytes", nil, receivedKilobytes),
	)
	if window.Truncated {
		for _, ann := range window.Annotations {
			frame.AppendNotices(data.Notice{
				Severity: data.NoticeSeverityWarning,
				Text:     ann,
			})
		}
	}
	return []*data.Frame{frame}, nil
}

// lossLatencyKey groups nested loss/latency samples into one frame per
// (serial, uplink, ip, metric).
type lossLatencyKey struct {
	serial string
	uplink string
	ip     string
	metric string
}

// handleDeviceUplinksLossLatency fetches the 5-minute uplink probe dataset and
// emits one native timeseries frame per (serial, uplink, ip, metric) — where
// metric ∈ {"lossPercent", "latencyMs"}. Frames carry Prometheus-style labels
// on the value field so Grafana's timeseries viz infers series grouping and
// legend natively (see §G.18 — without labels on the value field the panel
// renders empty).
//
// Null handling: loss/latency values can be nil when the probe failed; we
// emit *float64 so gaps render as gaps, not zeros. Unit is "percent" for loss
// and "ms" for latency.
//
// Serial filter: the endpoint does NOT accept a serial[] filter, so we fetch
// once and apply the filter client-side. This preserves the one-HTTP-call
// budget regardless of how many serials the panel filters to.
func handleDeviceUplinksLossLatency(ctx context.Context, client *meraki.Client, q MerakiQuery, tr TimeRange, opts Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("deviceUplinksLossLatency: orgId is required")
	}

	from := toRFCTime(tr.From)
	to := toRFCTime(tr.To)
	if from.IsZero() || to.IsZero() {
		return nil, fmt.Errorf("deviceUplinksLossLatency: time range is required")
	}

	spec, ok := meraki.KnownEndpointRanges[deviceUplinkLossLatencyEndpoint]
	if !ok {
		return nil, fmt.Errorf("deviceUplinksLossLatency: missing endpoint spec")
	}
	window, err := spec.Resolve(from, to, tr.MaxDataPoints, nil)
	if err != nil {
		return nil, fmt.Errorf("deviceUplinksLossLatency: resolve window: %w", err)
	}

	reqOpts := meraki.UplinkLossLatencyOptions{Window: &window}
	rows, err := client.GetOrganizationDevicesUplinksLossAndLatency(ctx, q.OrgID, reqOpts, deviceUplinkLossLatencyTTL)
	if err != nil {
		return nil, err
	}

	// Apply client-side serial filter if provided (endpoint has no serials[]).
	var serialFilter map[string]struct{}
	if len(q.Serials) > 0 {
		serialFilter = make(map[string]struct{}, len(q.Serials))
		for _, s := range q.Serials {
			serialFilter[s] = struct{}{}
		}
	}

	// Best-effort serial→name resolution for legend display; not fatal.
	var nameBySerial map[string]string
	if names, lookupErr := resolveDeviceNames(ctx, client, q.OrgID, "appliance"); lookupErr == nil {
		nameBySerial = names
	}

	type seriesBuf struct {
		ts     []time.Time
		values []*float64
	}
	groups := make(map[lossLatencyKey]*seriesBuf)
	for _, row := range rows {
		if serialFilter != nil {
			if _, keep := serialFilter[row.Serial]; !keep {
				continue
			}
		}
		for _, pt := range row.TimeSeries {
			ts := pt.Ts.UTC()
			lossKey := lossLatencyKey{serial: row.Serial, uplink: row.Uplink, ip: row.IP, metric: "lossPercent"}
			latencyKey := lossLatencyKey{serial: row.Serial, uplink: row.Uplink, ip: row.IP, metric: "latencyMs"}
			lossBuf, exists := groups[lossKey]
			if !exists {
				lossBuf = &seriesBuf{}
				groups[lossKey] = lossBuf
			}
			latencyBuf, exists := groups[latencyKey]
			if !exists {
				latencyBuf = &seriesBuf{}
				groups[latencyKey] = latencyBuf
			}
			lossBuf.ts = append(lossBuf.ts, ts)
			lossBuf.values = append(lossBuf.values, pt.LossPercent)
			latencyBuf.ts = append(latencyBuf.ts, ts)
			latencyBuf.values = append(latencyBuf.values, pt.LatencyMs)
		}
	}

	if len(groups) == 0 {
		// Empty-but-named frame so the panel shows a "no data" banner rather
		// than a structurally invalid response. Preserve the *float64 type on
		// the value field so downstream panels bind identically.
		empty := data.NewFrame("device_uplinks_loss_latency",
			data.NewField("ts", nil, []time.Time{}),
			data.NewField("value", nil, []*float64{}),
		)
		frames := []*data.Frame{empty}
		if window.Truncated {
			for _, ann := range window.Annotations {
				frames[0].AppendNotices(data.Notice{
					Severity: data.NoticeSeverityWarning,
					Text:     ann,
				})
			}
		}
		return frames, nil
	}

	// Sort keys for deterministic frame order.
	keys := make([]lossLatencyKey, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].serial != keys[j].serial {
			return keys[i].serial < keys[j].serial
		}
		if keys[i].uplink != keys[j].uplink {
			return keys[i].uplink < keys[j].uplink
		}
		if keys[i].ip != keys[j].ip {
			return keys[i].ip < keys[j].ip
		}
		return keys[i].metric < keys[j].metric
	})

	_ = opts // LabelMode already handled via nameBySerial above; reserved for future extensions.
	frames := make([]*data.Frame, 0, len(keys))
	for _, k := range keys {
		buf := groups[k]

		labels := data.Labels{
			"serial": k.serial,
			"uplink": k.uplink,
			"ip":     k.ip,
			"metric": k.metric,
		}
		valueField := data.NewField("value", labels, buf.values)

		// DisplayNameFromDS is baked as a pre-formatted string (§G.17).
		display := k.serial
		if nameBySerial != nil {
			if name := nameBySerial[k.serial]; name != "" {
				display = name
			}
		}
		displayName := fmt.Sprintf("%s / %s / %s / %s", display, k.uplink, k.ip, k.metric)

		unit := "ms"
		if k.metric == "lossPercent" {
			unit = "percent"
		}
		valueField.Config = &data.FieldConfig{
			DisplayNameFromDS: displayName,
			Unit:              unit,
		}

		frame := data.NewFrame("device_uplinks_loss_latency",
			data.NewField("ts", nil, buf.ts),
			valueField,
		)
		frames = append(frames, frame)
	}

	if window.Truncated && len(frames) > 0 {
		for _, ann := range window.Annotations {
			frames[0].AppendNotices(data.Notice{
				Severity: data.NoticeSeverityWarning,
				Text:     ann,
			})
		}
	}

	return frames, nil
}

// handleAppliancePortForwarding concatenates port-forwarding rules across the
// requested networks into a single table-shaped frame. One row per rule per
// network; errors on a single network are swallowed (continue-on-error) so a
// single misconfigured network doesn't blank the whole panel.
func handleAppliancePortForwarding(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if len(q.NetworkIDs) == 0 {
		return nil, fmt.Errorf("appliancePortForwarding: at least one networkId is required")
	}

	networkIDs := make([]string, len(q.NetworkIDs))
	copy(networkIDs, q.NetworkIDs)
	sort.Strings(networkIDs)

	var (
		netIDCol    []string
		names       []string
		protocols   []string
		publicPorts []string
		localPorts  []string
		lanIPs      []string
		uplinks     []string
		allowed     []string
		firstErr    error
	)
	for _, networkID := range networkIDs {
		rules, err := client.GetNetworkAppliancePortForwardingRules(ctx, networkID, portForwardingTTL)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, r := range rules {
			netIDCol = append(netIDCol, networkID)
			names = append(names, r.Name)
			protocols = append(protocols, r.Protocol)
			publicPorts = append(publicPorts, r.PublicPort)
			localPorts = append(localPorts, r.LocalPort)
			lanIPs = append(lanIPs, r.LanIP)
			uplinks = append(uplinks, r.Uplink)
			allowed = append(allowed, strings.Join(r.AllowedIPs, ","))
		}
	}

	return []*data.Frame{
		data.NewFrame("appliance_port_forwarding",
			data.NewField("networkId", nil, netIDCol),
			data.NewField("name", nil, names),
			data.NewField("protocol", nil, protocols),
			data.NewField("publicPort", nil, publicPorts),
			data.NewField("localPort", nil, localPorts),
			data.NewField("lanIp", nil, lanIPs),
			data.NewField("uplink", nil, uplinks),
			data.NewField("allowedIps", nil, allowed),
		),
	}, firstErr
}

// handleApplianceSettings concatenates per-network appliance-settings rows
// into one table frame. `dynamicDnsEnabled` is always a bool (false when the
// block is absent) so panels can filter on it without nil-guards.
func handleApplianceSettings(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if len(q.NetworkIDs) == 0 {
		return nil, fmt.Errorf("applianceSettings: at least one networkId is required")
	}

	networkIDs := make([]string, len(q.NetworkIDs))
	copy(networkIDs, q.NetworkIDs)
	sort.Strings(networkIDs)

	var (
		netIDs      []string
		tracking    []string
		deployment  []string
		ddnsEnabled []bool
		ddnsPrefix  []string
		ddnsURL     []string
		firstErr    error
	)
	for _, networkID := range networkIDs {
		settings, err := client.GetNetworkApplianceSettings(ctx, networkID, applianceSettingsTTL)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		netIDs = append(netIDs, networkID)
		if settings != nil {
			tracking = append(tracking, settings.ClientTrackingMethod)
			deployment = append(deployment, settings.DeploymentMode)
			if settings.DynamicDns != nil {
				ddnsEnabled = append(ddnsEnabled, settings.DynamicDns.Enabled)
				ddnsPrefix = append(ddnsPrefix, settings.DynamicDns.Prefix)
				ddnsURL = append(ddnsURL, settings.DynamicDns.URL)
			} else {
				ddnsEnabled = append(ddnsEnabled, false)
				ddnsPrefix = append(ddnsPrefix, "")
				ddnsURL = append(ddnsURL, "")
			}
		} else {
			tracking = append(tracking, "")
			deployment = append(deployment, "")
			ddnsEnabled = append(ddnsEnabled, false)
			ddnsPrefix = append(ddnsPrefix, "")
			ddnsURL = append(ddnsURL, "")
		}
	}

	return []*data.Frame{
		data.NewFrame("appliance_settings",
			data.NewField("networkId", nil, netIDs),
			data.NewField("clientTrackingMethod", nil, tracking),
			data.NewField("deploymentMode", nil, deployment),
			data.NewField("dynamicDnsEnabled", nil, ddnsEnabled),
			data.NewField("dynamicDnsPrefix", nil, ddnsPrefix),
			data.NewField("dynamicDnsUrl", nil, ddnsURL),
		),
	}, firstErr
}

