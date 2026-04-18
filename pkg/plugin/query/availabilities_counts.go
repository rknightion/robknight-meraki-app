package query

import (
	"context"
	"fmt"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// handleDeviceAvailabilityCounts emits a single wide frame with int64 counts
// per status bucket across the org's devices. Optionally filtered by
// productTypes (e.g. ["switch"]) so each device-family page can have a KPI
// row sourced from a single backend call instead of a fragile client-side
// filterByValue+reduce chain (todos.txt §G.20).
//
// Frame shape: one row, fields `online`, `alerting`, `offline`, `dormant`,
// `total`. Matches the pattern used by `alertsOverview` so the frontend can
// drive a stat panel with an organize transform to pick exactly one field.
func handleDeviceAvailabilityCounts(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("deviceAvailabilityCounts: orgId is required")
	}

	avails, err := client.GetOrganizationDevicesAvailabilities(ctx, q.OrgID, q.ProductTypes, deviceAvailabilitiesTTL)
	if err != nil {
		return nil, err
	}

	var online, alerting, offline, dormant int64
	for _, a := range avails {
		switch a.Status {
		case "online":
			online++
		case "alerting":
			alerting++
		case "offline":
			offline++
		case "dormant":
			dormant++
		}
	}
	total := int64(len(avails))

	return []*data.Frame{
		data.NewFrame("device_availability_counts",
			data.NewField("online", nil, []int64{online}),
			data.NewField("alerting", nil, []int64{alerting}),
			data.NewField("offline", nil, []int64{offline}),
			data.NewField("dormant", nil, []int64{dormant}),
			data.NewField("total", nil, []int64{total}),
		),
	}, nil
}
