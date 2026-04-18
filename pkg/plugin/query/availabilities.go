package query

import (
	"context"
	"fmt"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// deviceAvailabilitiesTTL: availability state updates every ~60s on the Meraki side, so
// 1m keeps panel auto-refreshes cheap without hiding genuine status changes.
const deviceAvailabilitiesTTL = 1 * time.Minute

// handleDeviceAvailabilities emits a single table-shaped frame with one row per device.
// Columns are (serial, name, productType, status, network_id, network_name, lastReportedAt) —
// the name column makes LabelMode irrelevant for this kind.
//
// lastReportedAt is always the zero time.Time right now: Meraki's v1
// `/organizations/{organizationId}/devices/availabilities` endpoint does not expose a
// per-device last-reported timestamp (status is "current"). We keep the column in the
// frame so panels can bind to it; when a future API version starts returning the field
// we'll plumb it through without a frame-shape change.
func handleDeviceAvailabilities(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, opts Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("deviceAvailabilities: orgId is required")
	}

	avails, err := client.GetOrganizationDevicesAvailabilities(ctx, q.OrgID, q.ProductTypes, deviceAvailabilitiesTTL)
	if err != nil {
		return nil, err
	}

	serials := make([]string, 0, len(avails))
	names := make([]string, 0, len(avails))
	productTypes := make([]string, 0, len(avails))
	statuses := make([]string, 0, len(avails))
	netIDs := make([]string, 0, len(avails))
	netNames := make([]string, 0, len(avails))
	lastReported := make([]time.Time, 0, len(avails))
	drilldownURLs := make([]string, 0, len(avails))

	for _, a := range avails {
		serials = append(serials, a.Serial)
		names = append(names, a.Name)
		productTypes = append(productTypes, a.ProductType)
		statuses = append(statuses, a.Status)
		netIDs = append(netIDs, a.Network.ID)
		netNames = append(netNames, a.Network.Name)
		// Zero time is acceptable here — the column stays present so panels can bind.
		lastReported = append(lastReported, time.Time{})
		drilldownURLs = append(drilldownURLs, deviceDrilldownURL(opts.PluginPathPrefix, a.ProductType, a.Serial))
	}

	return []*data.Frame{
		data.NewFrame("device_availabilities",
			data.NewField("serial", nil, serials),
			data.NewField("name", nil, names),
			data.NewField("productType", nil, productTypes),
			data.NewField("status", nil, statuses),
			data.NewField("network_id", nil, netIDs),
			data.NewField("network_name", nil, netNames),
			data.NewField("lastReportedAt", nil, lastReported),
			data.NewField("drilldownUrl", nil, drilldownURLs),
		),
	}, nil
}
