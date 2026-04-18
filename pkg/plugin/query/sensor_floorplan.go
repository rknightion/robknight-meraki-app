package query

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// sensorFloorPlanTTL — floor-plan geometry and device anchors change
// rarely (operators edit them by hand in Dashboard). 15 m matches the
// roadmap (§4.4.3-1e) and is deliberately loose so the layout query adds
// near-zero Meraki load on a busy Sensors page.
const sensorFloorPlanTTL = 15 * time.Minute

// handleSensorFloorPlan joins Meraki floor-plan layout data (per network)
// with the latest sensor readings (per org) into a single wide frame the
// panel factory consumes directly. The shape is:
//
//	floor_plan_id | floor_plan_name | serial | metric | value | x | y
//
// `x` and `y` are nullable:
//
//   - Anchor coordinates present (published from an auto-locate job or set
//     manually) → x = FloorPlanDevice.Lng, y = FloorPlanDevice.Lat. We use
//     (lng, lat) so downstream Geomap panels get standard (x=longitude,
//     y=latitude) orientation without a flip transform.
//   - Device on plan but no anchors → x = nil, y = nil. The frontend
//     branches on this and renders a status-grid / table layout instead of
//     the Geomap.
//   - No floor plan configured at all → zero-row frame + data.Notice so
//     the panel surfaces a friendly message (a blank timeseries would read
//     as "connection failed").
//
// The handler intentionally does NOT use the Device.FloorPlanID field to
// join — the floor-plan list response already includes a devices[] array
// pre-joined by Meraki, which spares us a second fan-out against the
// org-level devices endpoint.
func handleSensorFloorPlan(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("sensorFloorPlan: orgId is required")
	}
	if len(q.NetworkIDs) == 0 {
		return nil, fmt.Errorf("sensorFloorPlan: at least one networkId is required")
	}

	// Fetch the latest readings once for the whole query — filtered to the
	// same network set + serial set the panel scoped, and the same metric
	// filter so we don't fetch readings we won't surface.
	latestOpts := meraki.SensorReadingsLatestOptions{
		NetworkIDs: q.NetworkIDs,
		Serials:    q.Serials,
		Metrics:    q.Metrics,
	}
	sensors, err := client.GetOrganizationSensorReadingsLatest(ctx, q.OrgID, latestOpts, sensorLatestTTL)
	if err != nil {
		return nil, fmt.Errorf("sensorFloorPlan: readings latest: %w", err)
	}

	// readingsBySerial: serial → (metric → value). We fold the union-shape
	// reading into a float64 before emitting; the AQI panel re-composes the
	// weighted score client-side.
	readingsBySerial := make(map[string]map[string]float64)
	for _, s := range sensors {
		row, exists := readingsBySerial[s.Serial]
		if !exists {
			row = make(map[string]float64)
			readingsBySerial[s.Serial] = row
		}
		for _, r := range s.Readings {
			v, ok := sensorValue(r.Metric, r.Temperature, r.Humidity, r.Door, r.Water, r.CO2, r.PM25, r.TVOC, r.Noise, r.Battery, r.IAQ)
			if !ok {
				continue
			}
			row[r.Metric] = v
		}
	}

	// Fan out floor-plan fetches across every requested network. Each list
	// is small + cached so the parallelism budget here is trivial — keep
	// the loop serial for determinism and simplicity.
	type placed struct {
		planID   string
		planName string
		serial   string
		lat      float64
		lng      float64
		hasAnchor bool
	}
	var placements []placed
	anyPlanSeen := false
	for _, netID := range q.NetworkIDs {
		plans, perr := client.GetNetworkFloorPlans(ctx, netID, sensorFloorPlanTTL)
		if perr != nil {
			return nil, fmt.Errorf("sensorFloorPlan: floor plans (network %s): %w", netID, perr)
		}
		for _, p := range plans {
			anyPlanSeen = true
			for _, dev := range p.Devices {
				// Only surface MT sensors. The devices[] list can include MR
				// APs and MS switches placed on the same plan; those are
				// irrelevant to the Sensors page.
				if dev.ProductType != "" && dev.ProductType != "sensor" {
					continue
				}
				placements = append(placements, placed{
					planID:    p.ID,
					planName:  p.Name,
					serial:    dev.Serial,
					lat:       dev.Lat,
					lng:       dev.Lng,
					hasAnchor: dev.Lat != 0 || dev.Lng != 0,
				})
			}
		}
	}

	// Stable order (plan id, serial, metric) keeps panel rendering + tests
	// deterministic.
	sort.Slice(placements, func(i, j int) bool {
		if placements[i].planID != placements[j].planID {
			return placements[i].planID < placements[j].planID
		}
		return placements[i].serial < placements[j].serial
	})

	planIDs := make([]string, 0)
	planNames := make([]string, 0)
	serials := make([]string, 0)
	metrics := make([]string, 0)
	values := make([]float64, 0)
	xs := make([]*float64, 0)
	ys := make([]*float64, 0)

	for _, pl := range placements {
		row := readingsBySerial[pl.serial]
		if row == nil {
			// Sensor is on the plan but hasn't reported — skip silently.
			// The Sensors KPI row already surfaces this as "Sensors
			// reporting", so adding a placeholder row here would double-count.
			continue
		}
		// Sort metric names for determinism.
		keys := make([]string, 0, len(row))
		for k := range row {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, m := range keys {
			planIDs = append(planIDs, pl.planID)
			planNames = append(planNames, pl.planName)
			serials = append(serials, pl.serial)
			metrics = append(metrics, m)
			values = append(values, row[m])
			if pl.hasAnchor {
				lng := pl.lng
				lat := pl.lat
				xs = append(xs, &lng)
				ys = append(ys, &lat)
			} else {
				xs = append(xs, nil)
				ys = append(ys, nil)
			}
		}
	}

	frame := data.NewFrame("sensor_floor_plan",
		data.NewField("floor_plan_id", nil, planIDs),
		data.NewField("floor_plan_name", nil, planNames),
		data.NewField("serial", nil, serials),
		data.NewField("metric", nil, metrics),
		data.NewField("value", nil, values),
		data.NewField("x", nil, xs),
		data.NewField("y", nil, ys),
	)

	if !anyPlanSeen {
		// Frame stays zero-row — the viz branches on the notice, not on
		// "rows == 0", so a sensor that's reporting but un-placed reads
		// differently from "no floor plan configured at all".
		if frame.Meta == nil {
			frame.Meta = &data.FrameMeta{}
		}
		frame.Meta.Notices = append(frame.Meta.Notices, data.Notice{
			Severity: data.NoticeSeverityInfo,
			Text:     "no floor plan configured for the selected network(s); assign a floor plan in Meraki Dashboard to enable the heatmap view",
		})
	}

	return []*data.Frame{frame}, nil
}
