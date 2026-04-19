package query

import (
	"context"
	"fmt"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// handleDeviceOfflineCount emits a single-row, single-field frame with the
// number of devices in the org currently reporting `offline` status,
// optionally filtered by productTypes. It exists because the device-offline
// alert template needs a one-field input to feed the standard
// `reduce(last) → threshold(gt 0)` SSE chain. The sibling
// `deviceAvailabilityCounts` kind emits five fields and SSE reduce produces
// one labelled output per numeric field, which would cause a `gt 0`
// threshold to fire whenever `online > 0` (i.e. always).
//
// Underlying Meraki call is the same as `deviceAvailabilityCounts` and
// shares the same TTL, so a panel using both pays one round-trip and one
// cache slot per (orgID, productTypes) combination.
func handleDeviceOfflineCount(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("deviceOfflineCount: orgId is required")
	}

	avails, err := client.GetOrganizationDevicesAvailabilities(ctx, q.OrgID, q.ProductTypes, deviceAvailabilitiesTTL)
	if err != nil {
		return nil, err
	}

	var offline int64
	for _, a := range avails {
		if a.Status == "offline" {
			offline++
		}
	}

	return []*data.Frame{
		data.NewFrame("device_offline_count",
			data.NewField("count", nil, []int64{offline}),
		),
	}, nil
}
