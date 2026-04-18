package query

import (
	"context"
	"fmt"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// networkEventsTTL is intentionally short — the events feed is near-live and
// users expect auto-refresh panels to surface new entries quickly. Matching
// the sensor/alerts latest TTLs keeps the UI responsive without melting the
// /events endpoint under auto-refresh bursts.
const networkEventsTTL = 30 * time.Second

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
	if len(q.NetworkIDs) == 0 || q.NetworkIDs[0] == "" {
		return nil, fmt.Errorf("networkEvents: at least one networkId is required")
	}
	networkID := q.NetworkIDs[0]

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

	events, err := client.GetNetworkEvents(ctx, networkID, reqOpts, networkEventsTTL)
	if err != nil {
		return nil, err
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

	return []*data.Frame{
		data.NewFrame("network_events",
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
		),
	}, nil
}
