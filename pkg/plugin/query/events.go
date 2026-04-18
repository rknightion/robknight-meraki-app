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
// productType defaults to the first entry of q.ProductTypes; on networks
// with a single product family the endpoint accepts requests without one, so
// we pass through whatever the caller supplied rather than guessing.
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
	if len(q.ProductTypes) > 0 {
		reqOpts.ProductType = q.ProductTypes[0]
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
		got, err := client.GetNetworkEvents(ctx, networkID, reqOpts, networkEventsTTL)
		if err != nil {
			return nil, err
		}
		events = append(events, got...)
	}

	fallbackPT := ""
	if len(q.ProductTypes) > 0 {
		fallbackPT = q.ProductTypes[0]
	}

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
	for _, e := range events {
		var ts time.Time
		if e.OccurredAt != nil {
			ts = e.OccurredAt.UTC()
		}
		pt := e.ProductType
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
