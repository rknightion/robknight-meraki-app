// Package query — §4.4.5 "availability by family" handler.
//
// handleDeviceStatusByFamily reshapes the existing
// `GetOrganizationDevicesAvailabilities` call into a wide frame with one row
// per productType — the shape the Home "availability by family" stacked-bar
// panel consumes. We add a dedicated kind rather than reshaping in the
// frontend because:
//
//   1. The existing `deviceStatusOverview` handler only exposes the
//      `(status, count)` roll-up; productType is absent from that frame.
//   2. Building the per-family matrix client-side from
//      `deviceAvailabilities` would mean a `filterByValue + reduce` (or a
//      `groupingToMatrix`) chain on every Home render — §G.20 captures why
//      we keep that kind of aggregation server-side.
//   3. The underlying Meraki call is already cached (`deviceAvailabilitiesTTL`
//      = 1m) and shared with `handleDeviceAvailabilityCounts` + the
//      `deviceAvailabilities` table, so the Home tile is effectively free on
//      re-render.
//
// Frame shape (one row per observed productType):
//
//	productType | online | alerting | offline | dormant | total
//
// Product types that Meraki reports but whose total is zero in this org are
// omitted so the bar chart doesn't carry dead columns. If the org has no
// devices at all the frame is emitted empty-but-named so Grafana can still
// display a "no value" banner without the panel erroring.
package query

import (
	"context"
	"fmt"
	"sort"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// handleDeviceStatusByFamily fans one availabilities call into a per-
// productType roll-up. Uses the same TTL as `handleDeviceAvailabilityCounts`
// so the Meraki client's LRU+singleflight dedups across Home renders.
func handleDeviceStatusByFamily(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("deviceStatusByFamily: orgId is required")
	}

	avails, err := client.GetOrganizationDevicesAvailabilities(ctx, q.OrgID, q.ProductTypes, deviceAvailabilitiesTTL)
	if err != nil {
		return nil, err
	}

	// bucket: productType -> [online, alerting, offline, dormant]
	type counts struct {
		online, alerting, offline, dormant int64
	}
	buckets := make(map[string]*counts)

	for _, a := range avails {
		pt := a.ProductType
		if pt == "" {
			pt = "unknown"
		}
		b, ok := buckets[pt]
		if !ok {
			b = &counts{}
			buckets[pt] = b
		}
		switch a.Status {
		case "online":
			b.online++
		case "alerting":
			b.alerting++
		case "offline":
			b.offline++
		case "dormant":
			b.dormant++
		}
	}

	// Stable output order for deterministic legends.
	families := make([]string, 0, len(buckets))
	for pt := range buckets {
		families = append(families, pt)
	}
	sort.Strings(families)

	productTypes := make([]string, 0, len(families))
	online := make([]int64, 0, len(families))
	alerting := make([]int64, 0, len(families))
	offline := make([]int64, 0, len(families))
	dormant := make([]int64, 0, len(families))
	total := make([]int64, 0, len(families))

	for _, pt := range families {
		b := buckets[pt]
		productTypes = append(productTypes, pt)
		online = append(online, b.online)
		alerting = append(alerting, b.alerting)
		offline = append(offline, b.offline)
		dormant = append(dormant, b.dormant)
		total = append(total, b.online+b.alerting+b.offline+b.dormant)
	}

	return []*data.Frame{
		data.NewFrame("device_status_by_family",
			data.NewField("productType", nil, productTypes),
			data.NewField("online", nil, online),
			data.NewField("alerting", nil, alerting),
			data.NewField("offline", nil, offline),
			data.NewField("dormant", nil, dormant),
			data.NewField("total", nil, total),
		),
	}, nil
}
