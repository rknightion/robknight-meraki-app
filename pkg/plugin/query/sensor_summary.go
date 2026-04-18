package query

import (
	"context"
	"fmt"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// sensorSummaryTTL matches sensorLatestTTL because the summary is computed
// directly from the latest-readings feed; if that's fresh, so is this.
const sensorSummaryTTL = 30 * time.Second

// lowBatteryThreshold is the percentage at-or-below which we count a sensor
// as "low battery" on the KPI tile. 20% matches the Meraki dashboard's
// default alert threshold for MT10/12 battery warnings.
const lowBatteryThreshold = 20.0

// handleSensorAlertSummary aggregates the latest sensor readings into the
// four scalar counts rendered on the Sensors overview KPI row. Doing this
// server-side avoids a brittle filterByValue + reduce transform chain on
// the client — client-side transforms don't always hit the reducer we
// asked for (see earlier bug where every KPI rendered the same stray value
// because the reducer silently defaulted).
//
// Returned frame shape (single row, four fields):
//
//	sensorsReporting | doorsOpen | waterDetected | lowBattery
//	      10         |     0     |       0       |      1
//
// A sensor counts as "reporting" if it appears in the latest-readings
// response — Meraki drops sensors that have been silent for ~24h, so this
// doubles as a liveness proxy without needing a separate heartbeat call.
func handleSensorAlertSummary(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("sensorAlertSummary: orgId is required")
	}
	opts := meraki.SensorReadingsLatestOptions{
		NetworkIDs: q.NetworkIDs,
		Serials:    q.Serials,
		// metrics is intentionally unset — we need every metric to compute
		// door / water / battery simultaneously.
	}
	sensors, err := client.GetOrganizationSensorReadingsLatest(ctx, q.OrgID, opts, sensorSummaryTTL)
	if err != nil {
		return nil, err
	}

	var (
		sensorsReporting int64
		doorsOpen        int64
		waterDetected    int64
		lowBattery       int64
	)
	reportingSeen := make(map[string]struct{}, len(sensors))

	for _, s := range sensors {
		if _, already := reportingSeen[s.Serial]; !already {
			reportingSeen[s.Serial] = struct{}{}
			sensorsReporting++
		}
		for _, r := range s.Readings {
			switch r.Metric {
			case "door":
				if r.Door != nil && r.Door.Open {
					doorsOpen++
				}
			case "water":
				if r.Water != nil && r.Water.Present {
					waterDetected++
				}
			case "battery":
				if r.Battery != nil && r.Battery.Percentage <= lowBatteryThreshold {
					lowBattery++
				}
			}
		}
	}

	return []*data.Frame{
		data.NewFrame("sensor_alert_summary",
			data.NewField("sensorsReporting", nil, []int64{sensorsReporting}),
			data.NewField("doorsOpen", nil, []int64{doorsOpen}),
			data.NewField("waterDetected", nil, []int64{waterDetected}),
			data.NewField("lowBattery", nil, []int64{lowBattery}),
		),
	}, nil
}
