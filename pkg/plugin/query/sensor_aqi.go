package query

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// sensorAqiTTL mirrors sensorSummaryTTL because the AQI score is a
// derived view on the same latest-readings feed — the Go cache coalesces
// both requests into one Meraki HTTP call via singleflight.
const sensorAqiTTL = 30 * time.Second

// AQI weights + piecewise-linear bands. Must stay in sync with
// src/scene-helpers/sensorMetrics.ts (AQI_WEIGHTS + aqiSubScore). The
// sensorMetrics.ts file carries the full citations (WHO 2021 AQGs,
// ASHRAE 62.1, BAuA); keep this block as a thin mirror and prefer
// editing the weights there so the test + docs stay authoritative.
const (
	aqiWeightCO2  = 0.30
	aqiWeightTVOC = 0.35
	aqiWeightPM25 = 0.35
)

// aqiSubScore implements the piecewise-linear scoring — 100 at good,
// 0 at bad, linear in between, clamped. Mirrors aqiSubScore in
// sensorMetrics.ts line-for-line so the server and client view of the
// score stay aligned when only a subset of metrics is reporting.
func aqiSubScore(value, good, bad float64) float64 {
	if math.IsNaN(value) {
		return math.NaN()
	}
	if value <= good {
		return 100
	}
	if value >= bad {
		return 0
	}
	return 100 * (1 - (value-good)/(bad-good))
}

// handleSensorAqiComposite emits one row per sensor with `serial`, `name`,
// and the composite `score`. Sensors that don't report any of CO₂, TVOC,
// or PM2.5 are skipped entirely — a scoreless row would render as a
// 0-percent bar and incorrectly signal "hazardous".
//
// Missing inputs are tolerated: the weighted mean re-normalises over
// whatever inputs are present, matching the client-side behavior so a
// sensor reporting only CO₂ + PM2.5 still gets a meaningful score.
//
// Frame shape (long format, one row per sensor):
//
//	serial   | name     | score
//	Q3CG-... | Lounge   |   73.4
//	Q3CJ-... | Office   |   88.1
//
// `name` falls back to the serial when the device record has no
// configured name — a blank name would cause the bar gauge to render an
// unlabelled row which is useless when multiple sensors report.
func handleSensorAqiComposite(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("sensorAqiComposite: orgId is required")
	}
	opts := meraki.SensorReadingsLatestOptions{
		NetworkIDs: q.NetworkIDs,
		Serials:    q.Serials,
		Metrics:    []string{"co2", "tvoc", "pm25"},
	}
	sensors, err := client.GetOrganizationSensorReadingsLatest(ctx, q.OrgID, opts, sensorAqiTTL)
	if err != nil {
		return nil, err
	}

	// Name lookup: the /readings/latest response only carries serial and
	// network, no device name. Fall through the shared device-name cache
	// so the bar gauge always has a friendly label. Failure is non-fatal;
	// we fall back to the raw serial per-sensor.
	nameBySerial, _ := resolveDeviceNames(ctx, client, q.OrgID, "sensor")

	type row struct {
		serial string
		name   string
		score  float64
	}
	var rows []row

	for _, s := range sensors {
		var (
			co2, tvoc, pm25       float64
			haveCO2, haveTVOC, haveP25 bool
		)
		for _, r := range s.Readings {
			switch r.Metric {
			case "co2":
				if r.CO2 != nil {
					co2 = r.CO2.Concentration
					haveCO2 = true
				}
			case "tvoc":
				if r.TVOC != nil {
					tvoc = r.TVOC.Concentration
					haveTVOC = true
				}
			case "pm25":
				if r.PM25 != nil {
					pm25 = r.PM25.Concentration
					haveP25 = true
				}
			}
		}
		if !haveCO2 && !haveTVOC && !haveP25 {
			continue
		}

		var (
			weighted    float64
			totalWeight float64
		)
		if haveCO2 {
			weighted += aqiSubScore(co2, 600, 1500) * aqiWeightCO2
			totalWeight += aqiWeightCO2
		}
		if haveTVOC {
			weighted += aqiSubScore(tvoc, 220, 2200) * aqiWeightTVOC
			totalWeight += aqiWeightTVOC
		}
		if haveP25 {
			weighted += aqiSubScore(pm25, 10, 55) * aqiWeightPM25
			totalWeight += aqiWeightPM25
		}
		if totalWeight == 0 {
			continue
		}
		name := nameBySerial[s.Serial]
		if name == "" {
			name = s.Serial
		}
		rows = append(rows, row{
			serial: s.Serial,
			name:   name,
			score:  weighted / totalWeight,
		})
	}

	// Sort by score ascending so the "worst" sensors surface at the top
	// of the bar gauge — operators skim downward looking for low scores.
	sort.Slice(rows, func(i, j int) bool { return rows[i].score < rows[j].score })

	serials := make([]string, 0, len(rows))
	names := make([]string, 0, len(rows))
	scores := make([]float64, 0, len(rows))
	for _, r := range rows {
		serials = append(serials, r.serial)
		names = append(names, r.name)
		scores = append(scores, r.score)
	}

	return []*data.Frame{
		data.NewFrame("sensor_aqi_composite",
			data.NewField("serial", nil, serials),
			data.NewField("name", nil, names),
			data.NewField("score", nil, scores),
		),
	}, nil
}
