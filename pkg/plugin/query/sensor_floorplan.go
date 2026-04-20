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
			v, ok := sensorValue(r.Metric, r.Temperature, r.Humidity, r.Door, r.Water, r.CO2, r.PM25, r.TVOC, r.Noise, r.Battery, r.IAQ, r.RealPower, r.ApparentPower, r.Voltage, r.Current, r.Frequency, r.PowerFactor, r.DownstreamPower, r.RemoteLockoutSwitch)
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
	selectedNetworks := make(map[string]struct{}, len(q.NetworkIDs))
	for _, netID := range q.NetworkIDs {
		selectedNetworks[netID] = struct{}{}
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

	// Fallback: when Meraki Dashboard has no floor plans for the selected
	// network(s), the operator still expects to see "latest reading per
	// sensor" somewhere on the overview. The inventory table at device-level
	// already carries `lat`/`lng`/`address` for each MT unit; synthesize
	// placements from those so the panel becomes a no-floor-plan-required
	// latest-readings grid instead of silently reading "no sensors placed".
	// When a real floor plan IS configured, this fallback is skipped — we
	// never want to mix plan-relative and device-level coordinates on the
	// same panel (the anchor frame-of-reference is different).
	if !anyPlanSeen {
		devices, derr := client.GetOrganizationDevices(ctx, q.OrgID, []string{"sensor"}, devicesTTL)
		if derr != nil {
			return nil, fmt.Errorf("sensorFloorPlan: devices fallback: %w", derr)
		}
		for _, d := range devices {
			// Narrow to the panel's network scope. An empty networkIds filter
			// resolves to "all networks" earlier in the query pipeline, but
			// callers always pass at least one network here via `$network`.
			if len(selectedNetworks) > 0 {
				if _, ok := selectedNetworks[d.NetworkID]; !ok {
					continue
				}
			}
			placements = append(placements, placed{
				planID:    "",
				planName:  "(no floor plan)",
				serial:    d.Serial,
				lat:       d.Lat,
				lng:       d.Lng,
				hasAnchor: d.Lat != 0 || d.Lng != 0,
			})
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
		// Rows now come from the device-level fallback (if any sensors
		// reported). The info notice tells operators that assigning a floor
		// plan in Meraki Dashboard upgrades this tile into a spatial
		// heatmap — without dropping useful data in the meantime.
		if frame.Meta == nil {
			frame.Meta = &data.FrameMeta{}
		}
		frame.Meta.Notices = append(frame.Meta.Notices, data.Notice{
			Severity: data.NoticeSeverityInfo,
			Text:     "No Meraki Dashboard floor plan configured for this network — showing latest sensor readings by device. Create a floor plan under Network-wide → Map & floor plans to enable the spatial heatmap view.",
		})
	}

	return []*data.Frame{frame}, nil
}
