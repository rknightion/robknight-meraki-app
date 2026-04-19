package query

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// networkEventsTTL is intentionally short — the events feed is near-live and
// users expect auto-refresh panels to surface new entries quickly. Matching
// the sensor/alerts latest TTLs keeps the UI responsive without melting the
// /events endpoint under auto-refresh bursts.
const networkEventsTTL = 30 * time.Second

// networkEventsAllFanoutCap bounds the number of networks a single
// all-networks request fans out over. The Meraki /networks/{id}/events
// endpoint has no multi-network mode, so "All" requires one request per
// network — we stop at this cap and attach a notice rather than melting
// a large estate's rate budget for a single panel load.
const networkEventsAllFanoutCap = 25

// handleNetworkEvents emits a single table frame with one row per event.
// productType defaults to the first entry of q.ProductTypes; when empty
// (the All sentinel from the $productType picker) the handler fans out one
// call per family the target network actually has — the Meraki /events
// endpoint requires productType on multi-family networks, so "All" is
// expanded server-side.
//
// q.Metrics is reused as the includedEventTypes[] filter — the same pattern
// handleAlerts uses to smuggle extra filter slots through MerakiQuery. The
// Meraki API accepts a list here (unlike alert severity which is single-valued),
// so we pass the whole slice.
//
// drilldownUrl is computed per-row from the event's own productType when
// populated, falling back to q.ProductTypes[0]. This matters for mixed-product
// networks where the caller may pass productType=wireless but an event may
// belong to productType=switch — the column should still route correctly.
func handleNetworkEvents(ctx context.Context, client *meraki.Client, q MerakiQuery, tr TimeRange, opts Options) ([]*data.Frame, error) {
	// Resolve target network IDs. Empty / blank entries (e.g. the All sentinel
	// from a multi-select scene variable) fan out across every network in the
	// org — the Meraki /events endpoint is single-network only, so "All"
	// means issuing one request per network. Cap at networkEventsAllFanoutCap.
	networkIDs, truncated, err := resolveNetworkEventsTargets(ctx, client, q)
	if err != nil {
		return nil, err
	}
	if len(networkIDs) == 0 {
		return nil, fmt.Errorf("networkEvents: at least one networkId is required")
	}

	reqOpts := meraki.NetworkEventsOptions{
		IncludedEventTypes: q.Metrics,
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

	// Resolve product types per target network. Empty q.ProductTypes means
	// the user picked "All" — we need to iterate over the actual families
	// Meraki knows about for each network, otherwise /events 400s on
	// multi-family networks.
	productTypesByNetwork, err := resolveNetworkEventsProductTypes(ctx, client, q, networkIDs)
	if err != nil {
		return nil, err
	}

	// eventWithFamily pairs a fetched event with the productType used for the
	// fan-out call that returned it. Meraki sometimes omits productType on
	// individual event payloads, so the call-time family is the only reliable
	// source for the column when the user picked "All" (q.ProductTypes empty)
	// and we fanned out over multiple families.
	type eventWithFamily struct {
		event  meraki.NetworkEvent
		family string
	}
	var (
		events   []eventWithFamily
		firstErr error
	)
	for _, networkID := range networkIDs {
		for _, pt := range productTypesByNetwork[networkID] {
			reqOpts.ProductType = pt
			got, err := client.GetNetworkEvents(ctx, networkID, reqOpts, networkEventsTTL)
			if err != nil {
				// Per-family failures (e.g. a Meraki productType we think is
				// valid but the endpoint rejects under a specific org's
				// licensing) must not zero out the whole panel — keep the
				// first error to surface as a notice and keep merging what
				// did come back. Only surfaces when productType came from
				// the "All" fan-out, where losing one family is tolerable.
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			for _, e := range got {
				events = append(events, eventWithFamily{event: e, family: pt})
			}
		}
	}
	if firstErr != nil && len(events) == 0 {
		// Every family failed — preserve the original hard-fail contract
		// so the scene surfaces a proper error notice instead of silently
		// rendering an empty frame.
		return nil, firstErr
	}

	fallbackPT := firstNonEmpty(q.ProductTypes)

	var (
		occurredAt  []time.Time
		productType []string
		category    []string
		typeCol     []string
		description []string
		deviceSN    []string
		deviceName  []string
		clientID    []string
		clientMac   []string
		clientDesc  []string
		netIDCol    []string
		drilldown   []string
	)
	for _, wrapped := range events {
		e := wrapped.event
		var ts time.Time
		if e.OccurredAt != nil {
			ts = e.OccurredAt.UTC()
		}
		// Prefer the event's own productType; fall back to the family used
		// for the fan-out call (when "All" fans out multiple families); fall
		// back again to whatever the caller supplied (single-family case).
		pt := e.ProductType
		if pt == "" {
			pt = wrapped.family
		}
		if pt == "" {
			pt = fallbackPT
		}
		occurredAt = append(occurredAt, ts)
		productType = append(productType, pt)
		category = append(category, e.Category)
		typeCol = append(typeCol, e.Type)
		description = append(description, e.Description)
		deviceSN = append(deviceSN, e.DeviceSerial)
		deviceName = append(deviceName, e.DeviceName)
		clientID = append(clientID, e.ClientID)
		clientMac = append(clientMac, e.ClientMac)
		clientDesc = append(clientDesc, e.ClientDescription)
		netIDCol = append(netIDCol, e.NetworkID)
		drilldown = append(drilldown, deviceDrilldownURL(opts.PluginPathPrefix, pt, e.DeviceSerial))
	}

	frame := data.NewFrame("network_events",
		data.NewField("occurredAt", nil, occurredAt),
		data.NewField("productType", nil, productType),
		data.NewField("category", nil, category),
		data.NewField("type", nil, typeCol),
		data.NewField("description", nil, description),
		data.NewField("device_serial", nil, deviceSN),
		data.NewField("device_name", nil, deviceName),
		data.NewField("client_id", nil, clientID),
		data.NewField("client_mac", nil, clientMac),
		data.NewField("client_description", nil, clientDesc),
		data.NewField("network_id", nil, netIDCol),
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

// networkEventsProductTypes is the set of product-type values Meraki's
// /networks/{id}/events endpoint accepts. Kept as a whitelist because
// `sensor` is a legitimate productType on networks (MT devices) BUT the
// events endpoint rejects it with a 400, so we must not pass it through
// during the "All" fan-out. Verified against the 400 body:
//
//	"'productType' must be one of: 'appliance', 'camera', 'campusGateway',
//	 'cellularGateway', 'secureConnect', 'switch', 'systemsManager',
//	 'wireless' or 'wirelessController'"
var networkEventsProductTypes = map[string]struct{}{
	"appliance":         {},
	"camera":            {},
	"campusGateway":     {},
	"cellularGateway":   {},
	"secureConnect":     {},
	"switch":            {},
	"systemsManager":    {},
	"wireless":          {},
	"wirelessController": {},
}

// resolveNetworkEventsProductTypes returns the list of product types to
// query for each target network ID. When the caller supplied a concrete
// productType (q.ProductTypes[0] non-empty) every network gets that single
// value; when the caller passed empty (the "All" sentinel from the scene
// picker) we look up each network's actual productTypes from the org
// networks list so we only fan out over families the network has, filtered
// against the events-endpoint whitelist so MT-bearing networks don't 400
// on the `sensor` family.
//
// Returns an error only when the org networks fetch itself fails; missing
// networks just get an empty slice so the caller's outer loop skips them.
// When productTypes is empty on the caller AND orgId is unset we degrade to
// a nil map — the caller treats that as "no fan-out" and each network
// yields zero event calls (the legacy behaviour preserves the previous
// "productType required" contract).
func resolveNetworkEventsProductTypes(ctx context.Context, client *meraki.Client, q MerakiQuery, networkIDs []string) (map[string][]string, error) {
	out := make(map[string][]string, len(networkIDs))
	if pt := firstNonEmpty(q.ProductTypes); pt != "" {
		for _, id := range networkIDs {
			out[id] = []string{pt}
		}
		return out, nil
	}
	if q.OrgID == "" {
		return out, nil
	}
	networks, err := client.GetOrganizationNetworks(ctx, q.OrgID, nil, networksTTL)
	if err != nil {
		return nil, fmt.Errorf("networkEvents: resolve productTypes: %w", err)
	}
	byID := make(map[string][]string, len(networks))
	for _, n := range networks {
		byID[n.ID] = n.ProductTypes
	}
	for _, id := range networkIDs {
		raw := byID[id]
		filtered := raw[:0:0]
		for _, pt := range raw {
			if _, ok := networkEventsProductTypes[pt]; ok {
				filtered = append(filtered, pt)
			}
		}
		if len(filtered) == 0 {
			// Network has no event-bearing families (e.g. sensor-only) or
			// we've never seen it in the org networks list. Fall back to
			// `wireless` — the most common single-family case — so
			// single-family wireless networks still render events when the
			// picker is on All. The per-call 400 guard below tolerates a
			// miss if this default doesn't apply.
			filtered = []string{"wireless"}
		}
		out[id] = filtered
	}
	return out, nil
}

// resolveNetworkEventsTargets expands the all-networks sentinel (empty /
// blank NetworkIDs) to the concrete network list for the org. Returns the
// final target list plus a truncated flag when the org has more networks
// than `networkEventsAllFanoutCap`. Preserves caller-provided IDs when at
// least one non-empty entry is present — no expansion needed in that case.
func resolveNetworkEventsTargets(ctx context.Context, client *meraki.Client, q MerakiQuery) ([]string, bool, error) {
	// Caller provided concrete IDs — keep them, dropping any blank entries
	// that Grafana's interpolation left behind (e.g. ['N1', '', 'N2']).
	concrete := make([]string, 0, len(q.NetworkIDs))
	for _, id := range q.NetworkIDs {
		if id != "" && id != "$__all" && id != "*" {
			concrete = append(concrete, id)
		}
	}
	if len(concrete) > 0 {
		return concrete, false, nil
	}

	if q.OrgID == "" {
		// Preserve the legacy "networkId required" error text when we can't
		// fall back to the org-wide fanout either. This keeps the original
		// guard-rail contract observable to callers / tests.
		return nil, false, fmt.Errorf("networkEvents: at least one networkId is required")
	}
	networks, err := client.GetOrganizationNetworks(ctx, q.OrgID, q.ProductTypes, networksTTL)
	if err != nil {
		return nil, false, err
	}
	ids := make([]string, 0, len(networks))
	for _, n := range networks {
		if n.ID != "" {
			ids = append(ids, n.ID)
		}
	}
	// Deterministic order so caching + tests are stable.
	sort.Strings(ids)
	truncated := false
	if len(ids) > networkEventsAllFanoutCap {
		ids = ids[:networkEventsAllFanoutCap]
		truncated = true
	}
	return ids, truncated, nil
}
