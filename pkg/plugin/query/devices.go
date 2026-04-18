package query

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// devicesTTL: device inventory drifts slowly (adds, swaps, firmware upgrades)
// so a 5-minute TTL is a reasonable balance between staleness and API load.
const devicesTTL = 5 * time.Minute

// handleDevices emits one row per device in the requested org, optionally
// filtered server-side by productType and/or serial. Every row carries a
// `drilldownUrl` column pointing at the right per-family detail page for
// that device's productType, so downstream tables can route serial clicks
// to the correct scene without frontend template branching.
//
// Serial filter is applied client-side after the org-wide fetch so the
// underlying cache entry (keyed on orgID + productTypes) is shared between
// fleet-inventory panels and single-device stat panels — avoids a duplicate
// Meraki round-trip per serial.
func handleDevices(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, opts Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("devices: orgId is required")
	}
	devices, err := client.GetOrganizationDevices(ctx, q.OrgID, q.ProductTypes, devicesTTL)
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

	serials := make([]string, 0, len(devices))
	names := make([]string, 0, len(devices))
	macs := make([]string, 0, len(devices))
	models := make([]string, 0, len(devices))
	networkIDs := make([]string, 0, len(devices))
	firmwares := make([]string, 0, len(devices))
	productTypes := make([]string, 0, len(devices))
	tags := make([]string, 0, len(devices))
	lanIPs := make([]string, 0, len(devices))
	addresses := make([]string, 0, len(devices))
	lats := make([]float64, 0, len(devices))
	lngs := make([]float64, 0, len(devices))
	drilldownURLs := make([]string, 0, len(devices))
	for _, d := range devices {
		if serialFilter != nil {
			if _, ok := serialFilter[d.Serial]; !ok {
				continue
			}
		}
		serials = append(serials, d.Serial)
		names = append(names, d.Name)
		macs = append(macs, d.MAC)
		models = append(models, d.Model)
		networkIDs = append(networkIDs, d.NetworkID)
		firmwares = append(firmwares, d.Firmware)
		productTypes = append(productTypes, d.ProductType)
		tags = append(tags, strings.Join(d.Tags, ","))
		lanIPs = append(lanIPs, d.LanIP)
		addresses = append(addresses, d.Address)
		lats = append(lats, d.Lat)
		lngs = append(lngs, d.Lng)
		drilldownURLs = append(drilldownURLs, deviceDrilldownURL(opts.PluginPathPrefix, d.ProductType, d.Serial))
	}

	return []*data.Frame{
		data.NewFrame("devices",
			data.NewField("serial", nil, serials),
			data.NewField("name", nil, names),
			data.NewField("mac", nil, macs),
			data.NewField("model", nil, models),
			data.NewField("networkId", nil, networkIDs),
			data.NewField("firmware", nil, firmwares),
			data.NewField("productType", nil, productTypes),
			data.NewField("tags", nil, tags),
			data.NewField("lanIp", nil, lanIPs),
			data.NewField("address", nil, addresses),
			data.NewField("lat", nil, lats),
			data.NewField("lng", nil, lngs),
			data.NewField("drilldownUrl", nil, drilldownURLs),
		),
	}, nil
}
