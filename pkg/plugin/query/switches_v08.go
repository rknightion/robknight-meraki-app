package query

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// v0.8 — new switch query-kinds. All six added in a single pass to keep the
// wire contract (kind → handler → emitted frame shape) visible in one place.

const (
	switchFleetPowerHistoryTTL   = 2 * time.Minute
	switchPortsClientsOverviewTTL = 1 * time.Minute
	switchNeighborsTopologyTTL    = 2 * time.Minute
	networkDhcpServersSeenTTL    = 2 * time.Minute
	networkSwitchStacksTTL       = 5 * time.Minute
	switchRoutingInterfacesTTL   = 5 * time.Minute

	// Default timespan for neighbour / clients-overview when callers don't set one.
	switchNeighborsDefaultTimespan = 24 * time.Hour
	dhcpServersSeenDefaultTimespan = 24 * time.Hour
)

// handleSwitchFleetPowerHistory emits the fleet PoE draw as a single-series
// frame suitable for a Grafana timeseries viz. One frame, two fields:
// `ts` (time.Time), `drawWatts` (float64).
func handleSwitchFleetPowerHistory(ctx context.Context, client *meraki.Client, q MerakiQuery, tr TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("switchFleetPowerHistory: orgId is required")
	}
	etr, ok := meraki.KnownEndpointRanges["organizations/{organizationId}/summary/switch/power/history"]
	if !ok {
		return nil, fmt.Errorf("switchFleetPowerHistory: missing KnownEndpointRanges entry")
	}
	from := toRFCTime(tr.From)
	to := toRFCTime(tr.To)
	w, err := etr.Resolve(from, to, tr.MaxDataPoints, nil)
	if err != nil {
		return nil, fmt.Errorf("switchFleetPowerHistory: resolve time range: %w", err)
	}
	points, err := client.GetOrganizationSummarySwitchPowerHistory(ctx, q.OrgID, w.T0, w.T1, switchFleetPowerHistoryTTL)
	if err != nil {
		return nil, err
	}

	sort.Slice(points, func(i, j int) bool { return points[i].Ts.Before(points[j].Ts) })

	times := make([]time.Time, 0, len(points))
	watts := make([]float64, 0, len(points))
	for _, p := range points {
		times = append(times, p.Ts)
		watts = append(watts, p.DrawWatts)
	}

	tsField := data.NewField("ts", nil, times)
	valField := data.NewField("drawWatts", nil, watts)
	valField.Config = &data.FieldConfig{
		DisplayName: "Fleet PoE draw",
		Unit:        "watt",
	}
	frame := data.NewFrame("switch_fleet_power_history", tsField, valField)

	if w.Truncated {
		for _, ann := range w.Annotations {
			frame.AppendNotices(data.Notice{Severity: data.NoticeSeverityInfo, Text: ann})
		}
	}
	return []*data.Frame{frame}, nil
}

// handleSwitchPortsClientsOverview collapses the per-port clients-overview
// response into one row per switch (clientsOnline summed across ports,
// activePortCount = count of ports with ≥1 online client).
func handleSwitchPortsClientsOverview(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("switchPortsClientsOverview: orgId is required")
	}
	opts := meraki.SwitchPortsClientsOverviewByDeviceOptions{
		NetworkIDs: q.NetworkIDs,
		Serials:    q.Serials,
		Timespan:   time.Duration(q.TimespanSeconds) * time.Second,
	}
	if opts.Timespan == 0 {
		opts.Timespan = switchNeighborsDefaultTimespan
	}
	devices, err := client.GetOrganizationSwitchPortsClientsOverviewByDevice(ctx, q.OrgID, opts, switchPortsClientsOverviewTTL)
	if err != nil {
		return nil, err
	}

	// Stable ordering so legends / table rows are consistent across refreshes.
	sort.Slice(devices, func(i, j int) bool {
		if devices[i].Network.Name != devices[j].Network.Name {
			return devices[i].Network.Name < devices[j].Network.Name
		}
		return devices[i].Name < devices[j].Name
	})

	var (
		serials       []string
		switchNames   []string
		networkIDs    []string
		networkNames  []string
		models        []string
		clientsOnline []int64
		activePorts   []int64
	)
	for _, dev := range devices {
		var onlineSum, active int64
		for _, p := range dev.Ports {
			online := p.Counts.ByStatus["online"]
			onlineSum += online
			if online > 0 {
				active++
			}
		}
		serials = append(serials, dev.Serial)
		switchNames = append(switchNames, dev.Name)
		networkIDs = append(networkIDs, dev.Network.ID)
		networkNames = append(networkNames, dev.Network.Name)
		models = append(models, dev.Model)
		clientsOnline = append(clientsOnline, onlineSum)
		activePorts = append(activePorts, active)
	}

	return []*data.Frame{
		data.NewFrame("switch_ports_clients_overview",
			data.NewField("serial", nil, serials),
			data.NewField("switchName", nil, switchNames),
			data.NewField("networkId", nil, networkIDs),
			data.NewField("networkName", nil, networkNames),
			data.NewField("model", nil, models),
			data.NewField("clientsOnline", nil, clientsOnline),
			data.NewField("activePortCount", nil, activePorts),
		),
	}, nil
}

// handleSwitchNeighborsTopology flattens the LLDP/CDP name-value arrays into
// one row per (serial, portId, source). Source ∈ {"LLDP", "CDP"}; a port
// with both gets two rows. When callers pass `q.Serials` we filter client-
// side (endpoint is org-scoped).
func handleSwitchNeighborsTopology(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("switchNeighborsTopology: orgId is required")
	}

	opts := meraki.SwitchNeighborsTopologyOptions{
		NetworkIDs: q.NetworkIDs,
		Timespan:   time.Duration(q.TimespanSeconds) * time.Second,
	}
	if opts.Timespan == 0 {
		opts.Timespan = switchNeighborsDefaultTimespan
	}
	devices, err := client.GetOrganizationSwitchPortsTopologyDiscoveryByDevice(ctx, q.OrgID, opts, switchNeighborsTopologyTTL)
	if err != nil {
		return nil, err
	}

	serialFilter := stringSetOrNil(q.Serials)

	var (
		serials         []string
		switchNames     []string
		networkIDs      []string
		networkNames    []string
		portIDs         []string
		sources         []string
		peerSystemNames []string
		peerDescs       []string
		peerPortIDs     []string
		peerChassisIDs  []string
		peerAddresses   []string
		peerCaps        []string
		lastUpdated     []time.Time
	)

	appendRow := func(dev meraki.SwitchNeighborsDevice, p meraki.SwitchNeighborsPort, source string, pairs []meraki.SwitchNeighborsKeyPair) {
		serials = append(serials, dev.Serial)
		switchNames = append(switchNames, dev.Name)
		networkIDs = append(networkIDs, dev.Network.ID)
		networkNames = append(networkNames, dev.Network.Name)
		portIDs = append(portIDs, p.PortID)
		sources = append(sources, source)
		peerSystemNames = append(peerSystemNames, pickNeighbor(pairs, "System name", "Device ID"))
		peerDescs = append(peerDescs, pickNeighbor(pairs, "System description", "Platform", "Version"))
		peerPortIDs = append(peerPortIDs, pickNeighbor(pairs, "Port ID", "Port description"))
		peerChassisIDs = append(peerChassisIDs, pickNeighbor(pairs, "Chassis ID"))
		peerAddresses = append(peerAddresses, pickNeighbor(pairs, "Management address", "Address"))
		peerCaps = append(peerCaps, pickNeighbor(pairs, "System capabilities", "Capabilities"))
		if p.LastUpdatedAt != "" {
			if t, err := time.Parse(time.RFC3339, p.LastUpdatedAt); err == nil {
				lastUpdated = append(lastUpdated, t)
				return
			}
		}
		lastUpdated = append(lastUpdated, time.Time{})
	}

	for _, dev := range devices {
		if serialFilter != nil {
			if _, ok := serialFilter[dev.Serial]; !ok {
				continue
			}
		}
		for _, p := range dev.Ports {
			if len(p.LLDP) > 0 {
				appendRow(dev, p, "LLDP", p.LLDP)
			}
			if len(p.CDP) > 0 {
				appendRow(dev, p, "CDP", p.CDP)
			}
		}
	}

	return []*data.Frame{
		data.NewFrame("switch_neighbors",
			data.NewField("serial", nil, serials),
			data.NewField("switchName", nil, switchNames),
			data.NewField("networkId", nil, networkIDs),
			data.NewField("networkName", nil, networkNames),
			data.NewField("portId", nil, portIDs),
			data.NewField("source", nil, sources),
			data.NewField("peerSystemName", nil, peerSystemNames),
			data.NewField("peerDescription", nil, peerDescs),
			data.NewField("peerPortId", nil, peerPortIDs),
			data.NewField("peerChassisId", nil, peerChassisIDs),
			data.NewField("peerAddress", nil, peerAddresses),
			data.NewField("peerCapabilities", nil, peerCaps),
			data.NewField("lastUpdatedAt", nil, lastUpdated),
		),
	}, nil
}

// pickNeighbor returns the first non-empty value for the given key(s) from a
// Meraki LLDP/CDP name/value array. Order matters — earlier keys win.
func pickNeighbor(pairs []meraki.SwitchNeighborsKeyPair, keys ...string) string {
	for _, key := range keys {
		for _, kv := range pairs {
			if kv.Name == key && kv.Value != "" {
				return kv.Value
			}
		}
	}
	return ""
}

// handleNetworkDhcpServersSeen returns the DHCPv4 rogue-detection table.
// Accepts NetworkIDs or Serials; when only Serials given, resolves each
// serial's networkId via cached GetDevice then fans out.
func handleNetworkDhcpServersSeen(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	networks, err := resolveNetworkIDsForDhcp(ctx, client, q)
	if err != nil {
		return nil, err
	}
	if len(networks) == 0 {
		return nil, fmt.Errorf("networkDhcpServersSeen: serials or networkIds required")
	}

	opts := meraki.NetworkDhcpServersSeenOptions{
		Timespan: time.Duration(q.TimespanSeconds) * time.Second,
	}
	if opts.Timespan == 0 {
		opts.Timespan = dhcpServersSeenDefaultTimespan
	}

	var (
		macs         []string
		ipv4s        []string
		vlans        []string
		seenBy       []string
		lastSeens    []time.Time
		lastPacket   []string
		clientIDs    []string
		isAllowed    []bool
	)
	for _, nid := range networks {
		seen, err := client.GetNetworkSwitchDhcpV4ServersSeen(ctx, nid, opts, networkDhcpServersSeenTTL)
		if err != nil {
			// Don't blank the panel on one-network failure — surface via
			// notice attached to the first frame by the dispatcher.
			continue
		}
		for _, s := range seen {
			macs = append(macs, s.MAC)
			ipAddr := ""
			if s.IPv4 != nil {
				ipAddr = s.IPv4.Address
			}
			ipv4s = append(ipv4s, ipAddr)
			vlans = append(vlans, vlanString(s.VLAN))
			seenByStr := ""
			if len(s.SeenBy) > 0 {
				parts := make([]string, 0, len(s.SeenBy))
				for _, d := range s.SeenBy {
					name := d.Name
					if name == "" {
						name = d.Serial
					}
					parts = append(parts, name)
				}
				seenByStr = strings.Join(parts, ", ")
			}
			seenBy = append(seenBy, seenByStr)
			if ts, err := time.Parse(time.RFC3339, s.LastSeenAt); err == nil {
				lastSeens = append(lastSeens, ts)
			} else {
				lastSeens = append(lastSeens, time.Time{})
			}
			pktType := ""
			if s.LastPacket != nil {
				pktType = s.LastPacket.Type
			}
			lastPacket = append(lastPacket, pktType)
			clientIDs = append(clientIDs, s.ClientID)
			isAllowed = append(isAllowed, s.IsAllowed)
		}
	}

	return []*data.Frame{
		data.NewFrame("dhcp_servers_seen",
			data.NewField("mac", nil, macs),
			data.NewField("ipv4", nil, ipv4s),
			data.NewField("vlan", nil, vlans),
			data.NewField("seenBy", nil, seenBy),
			data.NewField("lastSeen", nil, lastSeens),
			data.NewField("lastPacket", nil, lastPacket),
			data.NewField("clientId", nil, clientIDs),
			data.NewField("trusted", nil, isAllowed),
		),
	}, nil
}

// resolveNetworkIDsForDhcp returns the unique network IDs implied by the
// query: explicit NetworkIDs take precedence, otherwise look up the
// network id from each Serial via the cached org-level ports/statuses
// feed (the same map we keep warm for the port-map queries).
func resolveNetworkIDsForDhcp(ctx context.Context, client *meraki.Client, q MerakiQuery) ([]string, error) {
	if len(q.NetworkIDs) > 0 {
		seen := map[string]struct{}{}
		out := make([]string, 0, len(q.NetworkIDs))
		for _, n := range q.NetworkIDs {
			if n == "" {
				continue
			}
			if _, ok := seen[n]; ok {
				continue
			}
			seen[n] = struct{}{}
			out = append(out, n)
		}
		return out, nil
	}
	if len(q.Serials) == 0 || q.OrgID == "" {
		return nil, nil
	}
	orgSwitches, err := client.GetOrganizationSwitchPortStatuses(ctx, q.OrgID, meraki.SwitchPortStatusOptions{}, switchPortsTTL)
	if err != nil {
		return nil, err
	}
	byNetwork := map[string]struct{}{}
	for _, sw := range orgSwitches {
		for _, s := range q.Serials {
			if sw.Serial == s && sw.Network.ID != "" {
				byNetwork[sw.Network.ID] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(byNetwork))
	for n := range byNetwork {
		out = append(out, n)
	}
	sort.Strings(out)
	return out, nil
}

// handleNetworkSwitchStacks lists switch stacks in the given networks.
// When callers pass `q.Serials` only, the handler resolves the serial's
// network via the cached org-level ports feed (same helper the DHCP
// handler uses), so the per-switch Overview tab can pass just
// `serials: [serial]`.
func handleNetworkSwitchStacks(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	networkIDs, err := resolveNetworkIDsForDhcp(ctx, client, q)
	if err != nil {
		return nil, err
	}

	var (
		netIDs   []string
		stackIDs []string
		stackNames []string
		members  []string
	)
	for _, nid := range networkIDs {
		stacks, err := client.GetNetworkSwitchStacks(ctx, nid, networkSwitchStacksTTL)
		if err != nil {
			continue
		}
		for _, st := range stacks {
			// When callers pass serials, only keep stacks that contain one of
			// them — otherwise the panel would show every stack in the whole
			// network.
			if len(q.Serials) > 0 {
				match := false
				for _, want := range q.Serials {
					for _, have := range st.Serials {
						if want == have {
							match = true
							break
						}
					}
					if match {
						break
					}
				}
				if !match {
					continue
				}
			}
			netIDs = append(netIDs, nid)
			stackIDs = append(stackIDs, st.ID)
			stackNames = append(stackNames, st.Name)
			members = append(members, strings.Join(st.Serials, ", "))
		}
	}

	return []*data.Frame{
		data.NewFrame("network_switch_stacks",
			data.NewField("networkId", nil, netIDs),
			data.NewField("stackId", nil, stackIDs),
			data.NewField("stackName", nil, stackNames),
			data.NewField("memberSerials", nil, members),
		),
	}, nil
}

// handleSwitchRoutingInterfaces returns the L3 SVI list for a standalone
// L3 switch or, when the switch is a stack member, for its containing stack.
// Endpoint 404s on L2 switches are caught by the client wrapper and
// returned as an empty slice so the always-visible panel shows its
// no-value text instead of an error banner.
func handleSwitchRoutingInterfaces(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if len(q.Serials) == 0 {
		return nil, fmt.Errorf("switchRoutingInterfaces: at least one serial is required")
	}

	var (
		interfaceIDs    []string
		names           []string
		subnets         []string
		vlanIDs         []int64
		ipv4Addresses   []string
		defaultGateways []string
		multicasts      []string
		sources         []string
	)

	for _, serial := range q.Serials {
		var (
			ifaces []meraki.SwitchRoutingInterface
			source = "device"
			err    error
		)

		// Stack-check: look up stacks by the switch's network and see if any
		// contains this serial. Requires resolving the network first.
		networkID := resolveNetworkIDForSerial(ctx, client, q.OrgID, serial)
		if networkID != "" {
			stacks, _ := client.GetNetworkSwitchStacks(ctx, networkID, networkSwitchStacksTTL)
			for _, st := range stacks {
				for _, s := range st.Serials {
					if s == serial {
						ifaces, err = client.GetNetworkSwitchStackRoutingInterfaces(ctx, networkID, st.ID, switchRoutingInterfacesTTL)
						source = "stack:" + st.ID
						break
					}
				}
				if source != "device" {
					break
				}
			}
		}

		if source == "device" {
			ifaces, err = client.GetDeviceSwitchRoutingInterfaces(ctx, serial, switchRoutingInterfacesTTL)
		}
		if err != nil {
			// L2 switches return NotFoundError which the wrapper catches
			// and returns as nil — other errors bubble to a frame notice.
			var nfe *meraki.NotFoundError
			if errors.As(err, &nfe) {
				continue
			}
			return nil, fmt.Errorf("switchRoutingInterfaces: %s: %w", serial, err)
		}

		for _, iface := range ifaces {
			interfaceIDs = append(interfaceIDs, iface.InterfaceID)
			names = append(names, iface.Name)
			subnets = append(subnets, iface.Subnet)
			vlanIDs = append(vlanIDs, int64(iface.VlanID))
			ipv4Addresses = append(ipv4Addresses, iface.InterfaceIP)
			defaultGateways = append(defaultGateways, iface.DefaultGateway)
			multicasts = append(multicasts, iface.MulticastRouting)
			sources = append(sources, source)
		}
	}

	return []*data.Frame{
		data.NewFrame("switch_routing_interfaces",
			data.NewField("interfaceId", nil, interfaceIDs),
			data.NewField("name", nil, names),
			data.NewField("subnet", nil, subnets),
			data.NewField("vlanId", nil, vlanIDs),
			data.NewField("ipv4Address", nil, ipv4Addresses),
			data.NewField("defaultGateway", nil, defaultGateways),
			data.NewField("multicastRouting", nil, multicasts),
			data.NewField("source", nil, sources),
		),
	}, nil
}

// resolveNetworkIDForSerial returns the network id for a given serial by
// reusing the cached org-level ports statuses map. Returns empty when the
// serial isn't in the org or the org lookup fails (callers fall back to
// the device-scoped endpoint in that case).
func resolveNetworkIDForSerial(ctx context.Context, client *meraki.Client, orgID, serial string) string {
	if orgID == "" || serial == "" {
		return ""
	}
	orgSwitches, err := client.GetOrganizationSwitchPortStatuses(ctx, orgID, meraki.SwitchPortStatusOptions{}, switchPortsTTL)
	if err != nil {
		return ""
	}
	for _, sw := range orgSwitches {
		if sw.Serial == serial {
			return sw.Network.ID
		}
	}
	return ""
}
