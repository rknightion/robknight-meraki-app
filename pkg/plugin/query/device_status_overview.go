package query

import (
	"context"
	"fmt"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// deviceStatusTTL: the overview endpoint is near-real-time but the Meraki
// backend only refreshes every ~60s anyway. A 1-minute cache soaks up panel
// auto-refresh bursts without hiding genuine changes for long.
const deviceStatusTTL = 1 * time.Minute

// handleDeviceStatusOverview emits one row per status bucket (online,
// alerting, offline, dormant). The panel side typically renders this as a
// stat/bar panel keyed on status.
func handleDeviceStatusOverview(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("deviceStatusOverview: orgId is required")
	}
	overview, err := client.GetOrganizationDevicesStatusOverview(ctx, q.OrgID, q.ProductTypes, deviceStatusTTL)
	if err != nil {
		return nil, err
	}

	statuses := []string{"online", "alerting", "offline", "dormant"}
	counts := []int64{
		int64(overview.Counts.ByStatus.Online),
		int64(overview.Counts.ByStatus.Alerting),
		int64(overview.Counts.ByStatus.Offline),
		int64(overview.Counts.ByStatus.Dormant),
	}

	return []*data.Frame{
		data.NewFrame("device_status",
			data.NewField("status", nil, statuses),
			data.NewField("count", nil, counts),
		),
	}, nil
}
