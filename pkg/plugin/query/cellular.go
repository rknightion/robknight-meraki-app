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

// Phase 10 (MG cellular gateway) handlers. Cache TTLs mirror the MX family:
// uplink state flips on minute timescales; port-forwarding + LAN + monitoring
// destinations are config snapshots so 15m is safe without hiding intentional
// edits for too long.
const (
	mgUplinksTTL        = 1 * time.Minute
	mgPortForwardingTTL = 15 * time.Minute
	mgLanTTL            = 15 * time.Minute
	mgConnectivityTTL   = 15 * time.Minute
)

// handleMgUplinks flattens the nested uplinks array into one row per
// (serial, interface). The `rsrpDb` / `rsrqDb` columns are parsed float64s
// so panels can apply numeric thresholds — the raw Meraki fields are strings
// with units attached ("-87 dBm").
func handleMgUplinks(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, opts Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("mgUplinks: orgId is required")
	}

	reqOpts := meraki.MgUplinkOptions{
		NetworkIDs: q.NetworkIDs,
		Serials:    q.Serials,
	}
	entries, err := client.GetOrganizationCellularGatewayUplinkStatuses(ctx, q.OrgID, reqOpts, mgUplinksTTL)
	if err != nil {
		return nil, err
	}

	var (
		serials      []string
		models       []string
		networkIDs   []string
		interfaces   []string
		statuses     []string
		iccids       []string
		apns         []string
		providers    []string
		publicIPs    []string
		signalTypes  []string
		connTypes    []string
		rsrpDb       []float64
		rsrqDb       []float64
		dns1         []string
		dns2         []string
		lastReported []time.Time
		drilldown    []string
	)

	for _, e := range entries {
		var last time.Time
		if e.LastReportedAt != nil {
			last = e.LastReportedAt.UTC()
		}
		for _, u := range e.Uplinks {
			serials = append(serials, e.Serial)
			models = append(models, e.Model)
			networkIDs = append(networkIDs, e.NetworkID)
			interfaces = append(interfaces, u.Interface)
			statuses = append(statuses, u.Status)
			iccids = append(iccids, u.ICCID)
			apns = append(apns, u.APN)
			providers = append(providers, u.Provider)
			publicIPs = append(publicIPs, u.PublicIP)
			signalTypes = append(signalTypes, u.SignalType)
			connTypes = append(connTypes, u.ConnectionType)
			rsrp, _ := meraki.ParseSignalDb(u.SignalStat.RSRP)
			rsrq, _ := meraki.ParseSignalDb(u.SignalStat.RSRQ)
			rsrpDb = append(rsrpDb, rsrp)
			rsrqDb = append(rsrqDb, rsrq)
			dns1 = append(dns1, u.DNS1)
			dns2 = append(dns2, u.DNS2)
			lastReported = append(lastReported, last)
			drilldown = append(drilldown, deviceDrilldownURL(opts.PluginPathPrefix, "cellularGateway", e.Serial))
		}
	}

	return []*data.Frame{
		data.NewFrame("mg_uplinks",
			data.NewField("serial", nil, serials),
			data.NewField("model", nil, models),
			data.NewField("networkId", nil, networkIDs),
			data.NewField("interface", nil, interfaces),
			data.NewField("status", nil, statuses),
			data.NewField("iccid", nil, iccids),
			data.NewField("apn", nil, apns),
			data.NewField("provider", nil, providers),
			data.NewField("publicIp", nil, publicIPs),
			data.NewField("signalType", nil, signalTypes),
			data.NewField("connectionType", nil, connTypes),
			data.NewField("rsrpDb", nil, rsrpDb),
			data.NewField("rsrqDb", nil, rsrqDb),
			data.NewField("dns1", nil, dns1),
			data.NewField("dns2", nil, dns2),
			data.NewField("lastReportedAt", nil, lastReported),
			data.NewField("drilldownUrl", nil, drilldown),
		),
	}, nil
}

// handleMgPortForwarding fans out across q.Serials and concatenates the
// per-device rule lists into a single table frame. Per-device failures
// continue-on-error so one misbehaving gateway doesn't blank the panel.
// `allowedIps` is surfaced as a comma-joined string — Grafana's table panel
// handles string arrays poorly, and the flat string is easy to filter on.
func handleMgPortForwarding(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if len(q.Serials) == 0 {
		return nil, fmt.Errorf("mgPortForwarding: at least one serial is required")
	}

	serials := make([]string, len(q.Serials))
	copy(serials, q.Serials)
	sort.Strings(serials)

	var (
		serialCol   []string
		names       []string
		protocols   []string
		publicPorts []string
		localPorts  []string
		lanIPs      []string
		allowedIPs  []string
		access      []string
		firstErr    error
	)
	for _, serial := range serials {
		rules, err := client.GetDeviceCellularGatewayPortForwardingRules(ctx, serial, mgPortForwardingTTL)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, r := range rules {
			serialCol = append(serialCol, serial)
			names = append(names, r.Name)
			protocols = append(protocols, r.Protocol)
			publicPorts = append(publicPorts, r.PublicPort)
			localPorts = append(localPorts, r.LocalPort)
			lanIPs = append(lanIPs, r.LanIP)
			allowedIPs = append(allowedIPs, strings.Join(r.AllowedIPs, ","))
			access = append(access, r.Access)
		}
	}

	return []*data.Frame{
		data.NewFrame("mg_port_forwarding",
			data.NewField("serial", nil, serialCol),
			data.NewField("name", nil, names),
			data.NewField("protocol", nil, protocols),
			data.NewField("publicPort", nil, publicPorts),
			data.NewField("localPort", nil, localPorts),
			data.NewField("lanIp", nil, lanIPs),
			data.NewField("allowedIps", nil, allowedIPs),
			data.NewField("access", nil, access),
		),
	}, firstErr
}

// handleMgLan flattens the fixed-IP assignments and reserved-IP ranges into
// one table with a `kind` discriminator column. This shape lets a single
// panel show both with filters, without needing two separate queries.
func handleMgLan(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if len(q.Serials) == 0 {
		return nil, fmt.Errorf("mgLan: at least one serial is required")
	}

	serials := make([]string, len(q.Serials))
	copy(serials, q.Serials)
	sort.Strings(serials)

	var (
		serialCol     []string
		kinds         []string
		identifiers   []string
		nameCol       []string
		ipCol         []string
		rangeStartCol []string
		rangeEndCol   []string
		commentCol    []string
		firstErr      error
	)
	for _, serial := range serials {
		lan, err := client.GetDeviceCellularGatewayLan(ctx, serial, mgLanTTL)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if lan == nil {
			continue
		}
		for _, fx := range lan.FixedIPAssignments {
			serialCol = append(serialCol, serial)
			kinds = append(kinds, "fixed")
			identifiers = append(identifiers, fx.MAC)
			nameCol = append(nameCol, fx.Name)
			ipCol = append(ipCol, fx.IP)
			rangeStartCol = append(rangeStartCol, "")
			rangeEndCol = append(rangeEndCol, "")
			commentCol = append(commentCol, "")
		}
		for _, rv := range lan.ReservedIPRanges {
			serialCol = append(serialCol, serial)
			kinds = append(kinds, "reserved")
			identifiers = append(identifiers, rv.Start+"-"+rv.End)
			nameCol = append(nameCol, "")
			ipCol = append(ipCol, "")
			rangeStartCol = append(rangeStartCol, rv.Start)
			rangeEndCol = append(rangeEndCol, rv.End)
			commentCol = append(commentCol, rv.Comment)
		}
	}

	return []*data.Frame{
		data.NewFrame("mg_lan",
			data.NewField("serial", nil, serialCol),
			data.NewField("kind", nil, kinds),
			data.NewField("identifier", nil, identifiers),
			data.NewField("name", nil, nameCol),
			data.NewField("ip", nil, ipCol),
			data.NewField("rangeStart", nil, rangeStartCol),
			data.NewField("rangeEnd", nil, rangeEndCol),
			data.NewField("comment", nil, commentCol),
		),
	}, firstErr
}

// handleMgConnectivity emits one row per (network, destination). The table
// surface is small (typically 1-3 destinations per network) so no fan-out
// concerns here.
func handleMgConnectivity(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if len(q.NetworkIDs) == 0 {
		return nil, fmt.Errorf("mgConnectivity: at least one networkId is required")
	}

	networkIDs := make([]string, len(q.NetworkIDs))
	copy(networkIDs, q.NetworkIDs)
	sort.Strings(networkIDs)

	var (
		netCol       []string
		ipCol        []string
		descCol      []string
		isDefaultCol []bool
		firstErr     error
	)
	for _, networkID := range networkIDs {
		dests, err := client.GetNetworkCellularGatewayConnectivityMonitoringDestinations(ctx, networkID, mgConnectivityTTL)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, d := range dests {
			netCol = append(netCol, networkID)
			ipCol = append(ipCol, d.IP)
			descCol = append(descCol, d.Description)
			isDefaultCol = append(isDefaultCol, d.Default)
		}
	}

	return []*data.Frame{
		data.NewFrame("mg_connectivity",
			data.NewField("networkId", nil, netCol),
			data.NewField("ip", nil, ipCol),
			data.NewField("description", nil, descCol),
			data.NewField("isDefault", nil, isDefaultCol),
		),
	}, firstErr
}
