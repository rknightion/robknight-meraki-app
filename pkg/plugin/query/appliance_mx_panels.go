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

// v0.5 §4.4.3-1c handlers — MX traffic shaping snapshot, MX uplink failover
// event timeline, and the VPN peer heatmap reshape that replaces the legacy
// peer matrix panel on the Appliances / VPN tab.
//
// TTLs:
//   - trafficShaping: 5m (admin config rarely changes; aligns with
//     applianceSettings + portForwarding).
//   - failoverEvents: 30s (live timeline; matches networkEventsTTL so the
//     two feeds share the /events endpoint cache key when callers reuse the
//     same (networkId, filter) tuple).

const (
	applianceTrafficShapingTTL = 5 * time.Minute
	applianceFailoverEventsTTL = 30 * time.Second
)

// handleApplianceTrafficShaping concatenates the two trafficShaping snapshots
// per network into one flat one-row-per-network table: default-rules + global
// bandwidth caps from /appliance/trafficShaping, then the uplink-selection
// policy (defaultUplink, loadBalancing, active-active AutoVPN, immediate
// failover) from /appliance/trafficShaping/uplinkSelection.
//
// Per-network errors are swallowed (continue-on-error) so one misconfigured
// network doesn't blank the whole panel — matches handleAppliancePortForwarding
// and handleApplianceSettings.
func handleApplianceTrafficShaping(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if len(q.NetworkIDs) == 0 {
		return nil, fmt.Errorf("applianceTrafficShaping: at least one networkId is required")
	}

	networkIDs := make([]string, len(q.NetworkIDs))
	copy(networkIDs, q.NetworkIDs)
	sort.Strings(networkIDs)

	var (
		netIDs                []string
		defaultRulesEnabled   []bool
		globalLimitUpKbps     []int64
		globalLimitDownKbps   []int64
		defaultUplink         []string
		loadBalancingEnabled  []bool
		activeActiveAutoVpn   []bool
		immediateFailover     []bool
		wanPreferenceCount    []int64
		vpnPreferenceCount    []int64
		firstErr              error
	)

	for _, networkID := range networkIDs {
		ts, err := client.GetNetworkApplianceTrafficShaping(ctx, networkID, applianceTrafficShapingTTL)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		us, err := client.GetNetworkApplianceTrafficShapingUplinkSelection(ctx, networkID, applianceTrafficShapingTTL)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		netIDs = append(netIDs, networkID)
		var enabled bool
		if ts != nil && ts.DefaultRulesEnabled != nil {
			enabled = *ts.DefaultRulesEnabled
		}
		defaultRulesEnabled = append(defaultRulesEnabled, enabled)
		var up, down int64
		if ts != nil && ts.GlobalBandwidthLimits != nil {
			if ts.GlobalBandwidthLimits.LimitUp != nil {
				up = *ts.GlobalBandwidthLimits.LimitUp
			}
			if ts.GlobalBandwidthLimits.LimitDown != nil {
				down = *ts.GlobalBandwidthLimits.LimitDown
			}
		}
		globalLimitUpKbps = append(globalLimitUpKbps, up)
		globalLimitDownKbps = append(globalLimitDownKbps, down)
		if us != nil {
			defaultUplink = append(defaultUplink, us.DefaultUplink)
			loadBalancingEnabled = append(loadBalancingEnabled, us.LoadBalancingEnabled)
			activeActiveAutoVpn = append(activeActiveAutoVpn, us.ActiveActiveAutoVpnEnabled)
			var immediate bool
			if us.FailoverAndFailback != nil && us.FailoverAndFailback.Immediate != nil {
				immediate = us.FailoverAndFailback.Immediate.Enabled
			}
			immediateFailover = append(immediateFailover, immediate)
			wanPreferenceCount = append(wanPreferenceCount, int64(len(us.WanTrafficUplinkPrefs)))
			vpnPreferenceCount = append(vpnPreferenceCount, int64(len(us.VpnTrafficUplinkPrefs)))
		} else {
			defaultUplink = append(defaultUplink, "")
			loadBalancingEnabled = append(loadBalancingEnabled, false)
			activeActiveAutoVpn = append(activeActiveAutoVpn, false)
			immediateFailover = append(immediateFailover, false)
			wanPreferenceCount = append(wanPreferenceCount, 0)
			vpnPreferenceCount = append(vpnPreferenceCount, 0)
		}
	}

	return []*data.Frame{
		data.NewFrame("appliance_traffic_shaping",
			data.NewField("networkId", nil, netIDs),
			data.NewField("defaultRulesEnabled", nil, defaultRulesEnabled),
			data.NewField("globalLimitUpKbps", nil, globalLimitUpKbps),
			data.NewField("globalLimitDownKbps", nil, globalLimitDownKbps),
			data.NewField("defaultUplink", nil, defaultUplink),
			data.NewField("loadBalancingEnabled", nil, loadBalancingEnabled),
			data.NewField("activeActiveAutoVpn", nil, activeActiveAutoVpn),
			data.NewField("immediateFailover", nil, immediateFailover),
			data.NewField("wanTrafficPreferences", nil, wanPreferenceCount),
			data.NewField("vpnTrafficPreferences", nil, vpnPreferenceCount),
		),
	}, firstErr
}

// applianceFailoverEventTypes is the defensible default filter applied when
// the caller does not supply q.Metrics. Chosen from the public Meraki MX
// event vocabulary as the event types that represent a WAN uplink state
// change (and therefore belong on a "failover timeline" panel):
//
//   - uplink_change     MX wired uplink state transition (up/down)
//   - cellular_up       MX cellular uplink came up
//   - cellular_down     MX cellular uplink went down
//   - failover          Generic failover marker emitted by some MX firmware
//   - wan_failover      Explicit WAN failover event (MX firmware variant)
//
// This list is conservative — we only include event types that describe a
// WAN uplink state change. Callers that want the full MX event stream
// (including non-failover noise) should use the existing networkEvents kind
// instead. If Meraki adds new uplink-change event types the caller can pass
// q.Metrics to override this default.
var applianceFailoverEventTypes = []string{
	"uplink_change",
	"cellular_up",
	"cellular_down",
	"failover",
	"wan_failover",
}

// handleApplianceFailoverEvents fetches MX uplink-change events from the
// existing /networks/{id}/events endpoint and emits a long-format table frame
// suitable for a state-timeline / barchart panel. No new Meraki endpoint is
// called — this is purely a filtered view on top of client.GetNetworkEvents.
//
// Filter behaviour:
//   - productType is always set to "appliance" so we never fan into MR/MS
//     events even when the caller left productType empty.
//   - includedEventTypes[] defaults to applianceFailoverEventTypes; caller
//     can override by populating q.Metrics (same override pattern the existing
//     networkEvents handler uses).
//   - network fan-out honours the networkEvents "all networks" expansion so
//     empty q.NetworkIDs hit every appliance network in the org (capped at
//     networkEventsAllFanoutCap, with a truncation notice).
//
// Frame shape: one row per event with time + serial + uplink + event type +
// description columns. The uplink is extracted from the event's `eventData`
// map when present — Meraki typically populates `uplink` or `interface`
// there; both keys are probed.
func handleApplianceFailoverEvents(ctx context.Context, client *meraki.Client, q MerakiQuery, tr TimeRange, opts Options) ([]*data.Frame, error) {
	networkIDs, truncated, err := resolveNetworkEventsTargets(ctx, client, q)
	if err != nil {
		return nil, err
	}
	if len(networkIDs) == 0 {
		return nil, fmt.Errorf("applianceFailoverEvents: at least one networkId is required")
	}

	eventTypes := q.Metrics
	if len(eventTypes) == 0 {
		eventTypes = applianceFailoverEventTypes
	}

	reqOpts := meraki.NetworkEventsOptions{
		ProductType:        "appliance",
		IncludedEventTypes: eventTypes,
	}
	if len(q.Serials) > 0 {
		reqOpts.DeviceSerial = q.Serials[0]
	}
	if from := toRFCTime(tr.From); !from.IsZero() {
		reqOpts.TSStart = &from
	}
	if to := toRFCTime(tr.To); !to.IsZero() {
		reqOpts.TSEnd = &to
	}

	var events []meraki.NetworkEvent
	for _, networkID := range networkIDs {
		got, err := client.GetNetworkEvents(ctx, networkID, reqOpts, applianceFailoverEventsTTL)
		if err != nil {
			return nil, err
		}
		events = append(events, got...)
	}

	// Stable ordering by occurredAt so the state-timeline viz draws bands
	// left-to-right without relying on Grafana's client-side sort.
	sort.SliceStable(events, func(i, j int) bool {
		var ti, tj time.Time
		if events[i].OccurredAt != nil {
			ti = *events[i].OccurredAt
		}
		if events[j].OccurredAt != nil {
			tj = *events[j].OccurredAt
		}
		return ti.Before(tj)
	})

	var (
		occurredAt  []time.Time
		netIDCol    []string
		deviceSN    []string
		deviceName  []string
		typeCol     []string
		uplinkCol   []string
		description []string
		drilldown   []string
	)
	for _, e := range events {
		var ts time.Time
		if e.OccurredAt != nil {
			ts = e.OccurredAt.UTC()
		}
		occurredAt = append(occurredAt, ts)
		netIDCol = append(netIDCol, e.NetworkID)
		deviceSN = append(deviceSN, e.DeviceSerial)
		deviceName = append(deviceName, e.DeviceName)
		typeCol = append(typeCol, e.Type)
		uplinkCol = append(uplinkCol, extractEventUplink(e.EventData))
		description = append(description, e.Description)
		drilldown = append(drilldown, deviceDrilldownURL(opts.PluginPathPrefix, "appliance", e.DeviceSerial))
	}

	frame := data.NewFrame("appliance_failover_events",
		data.NewField("occurredAt", nil, occurredAt),
		data.NewField("networkId", nil, netIDCol),
		data.NewField("deviceSerial", nil, deviceSN),
		data.NewField("deviceName", nil, deviceName),
		data.NewField("type", nil, typeCol),
		data.NewField("uplink", nil, uplinkCol),
		data.NewField("description", nil, description),
		data.NewField("drilldownUrl", nil, drilldown),
	)
	if truncated {
		frame.AppendNotices(data.Notice{
			Severity: data.NoticeSeverityWarning,
			Text:     fmt.Sprintf("Events truncated: queried only the first %d networks in this organisation. Pick a specific network for the full feed.", networkEventsAllFanoutCap),
		})
	}
	return []*data.Frame{frame}, nil
}

// extractEventUplink plucks an uplink identifier out of the free-form
// eventData map. Meraki uses at least two keys depending on firmware —
// `uplink` on modern builds, `interface` on some legacy payloads — so we
// probe both. Returns "" when neither is present (e.g. a generic failover
// event with no interface context).
func extractEventUplink(eventData map[string]any) string {
	if eventData == nil {
		return ""
	}
	for _, key := range []string{"uplink", "interface"} {
		if v, ok := eventData[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

// handleApplianceVpnHeatmap reshapes the existing applianceVpnStatuses feed
// into a long-format frame tuned for Grafana's heatmap viz (rows × columns ×
// value). One row per meraki peer-pair (thirdParty peers are skipped — they
// don't fit a symmetric peer×peer grid because thirdParty peers have no
// sourceNetwork identity on the reverse leg). Each row carries:
//
//   - sourceNetworkName  — the network whose MX we fetched statuses for
//   - peerNetworkName    — the peer on the other side of the tunnel
//   - value              — 1 when reachable, 0 otherwise (heatmap cell colour)
//   - reachability       — raw string so hover overlays can show text
//
// Why a new kind instead of reshaping applianceVpnStatuses in place:
// applianceVpnStatuses emits a wide flattened table that's already bound to
// the existing VPN peer-matrix table test (see appliance_test.go
// TestHandle_ApplianceVpnStatuses_FlattensPeerKinds) and the panel factory
// vpnPeerMatrixTable(). Reshaping would either break that test or require
// splitting it, which buys nothing since the heatmap is a tighter subset of
// the data. The new kind keeps both views addressable side-by-side during
// the v0.5 transition and can be unified once the matrix panel is removed
// from the scene (done in this same commit on the frontend).
func handleApplianceVpnHeatmap(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("applianceVpnHeatmap: orgId is required")
	}
	reqOpts := meraki.ApplianceVpnStatusOptions{NetworkIDs: q.NetworkIDs}
	entries, err := client.GetOrganizationApplianceVpnStatuses(ctx, q.OrgID, reqOpts, applianceVpnStatusesTTL)
	if err != nil {
		return nil, err
	}

	var (
		sourceNames []string
		peerNames   []string
		values      []float64
		reach       []string
	)

	for _, entry := range entries {
		sourceName := entry.NetworkName
		if sourceName == "" {
			sourceName = entry.NetworkID
		}
		for _, peer := range entry.MerakiVpnPeers {
			peerName := peer.NetworkName
			if peerName == "" {
				peerName = peer.NetworkID
			}
			sourceNames = append(sourceNames, sourceName)
			peerNames = append(peerNames, peerName)
			r := strings.ToLower(peer.Reachability)
			reach = append(reach, peer.Reachability)
			if r == "reachable" {
				values = append(values, 1)
			} else {
				values = append(values, 0)
			}
		}
	}

	return []*data.Frame{
		data.NewFrame("appliance_vpn_heatmap",
			data.NewField("sourceNetworkName", nil, sourceNames),
			data.NewField("peerNetworkName", nil, peerNames),
			data.NewField("value", nil, values),
			data.NewField("reachability", nil, reach),
		),
	}, nil
}
