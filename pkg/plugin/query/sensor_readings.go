package query

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// Sensor cache TTLs: "latest" is basically a live read (30s keeps panels
// responsive without hammering Meraki); "history" is immutable once written,
// but 1 minute gives us a small buffer for Grafana auto-refreshes on the
// same panel config.
const (
	sensorLatestTTL  = 30 * time.Second
	sensorHistoryTTL = 1 * time.Minute

	sensorHistoryEndpoint = "organizations/{organizationId}/sensor/readings/history"
)

// metricNames is the static list of Meraki sensor metrics the plugin knows
// how to flatten. Kept in one place so the union-field mapping below and the
// metricfind variable stay in sync.
var metricNames = []string{
	"temperature",
	"humidity",
	"door",
	"water",
	"co2",
	"pm25",
	"tvoc",
	"noise",
	"battery",
	"indoorAirQuality",
}

// metricLabel maps Meraki's camelCase metric id to a human-friendly legend
// label. When a metric is not in the table we fall back to the raw id so
// future metrics keep working before we update the map.
var metricLabel = map[string]string{
	"temperature":      "Temperature",
	"humidity":         "Humidity",
	"door":             "Door",
	"water":            "Water",
	"co2":              "CO₂",
	"pm25":             "PM2.5",
	"tvoc":             "TVOC",
	"noise":            "Noise",
	"battery":          "Battery",
	"indoorAirQuality": "IAQ",
}

// handleSensorReadingsLatest emits one row per (serial, metric) from the
// /sensor/readings/latest feed. Long-format is kept here on purpose — the UI
// uses this frame directly as a table (inventory, "last seen" column) and
// for client-side KPI aggregations.
func handleSensorReadingsLatest(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("sensorReadingsLatest: orgId is required")
	}
	opts := meraki.SensorReadingsLatestOptions{
		NetworkIDs: q.NetworkIDs,
		Serials:    q.Serials,
		Metrics:    q.Metrics,
	}
	sensors, err := client.GetOrganizationSensorReadingsLatest(ctx, q.OrgID, opts, sensorLatestTTL)
	if err != nil {
		return nil, err
	}

	var (
		tss        []time.Time
		serials    []string
		networkIDs []string
		networkNms []string
		metrics    []string
		values     []float64
		raws       []json.RawMessage
	)
	for _, s := range sensors {
		for _, r := range s.Readings {
			v, ok := sensorValue(r.Metric, r.Temperature, r.Humidity, r.Door, r.Water, r.CO2, r.PM25, r.TVOC, r.Noise, r.Battery, r.IAQ)
			if !ok {
				// No union field populated for the metric we were told about;
				// skip it rather than emit a misleading 0.
				continue
			}
			tss = append(tss, r.Ts.UTC())
			serials = append(serials, s.Serial)
			networkIDs = append(networkIDs, s.Network.ID)
			networkNms = append(networkNms, s.Network.Name)
			metrics = append(metrics, r.Metric)
			values = append(values, v)
			rawBytes, marshalErr := json.Marshal(r)
			if marshalErr != nil {
				rawBytes = []byte("null")
			}
			raws = append(raws, rawBytes)
		}
	}

	return []*data.Frame{
		data.NewFrame("sensor_readings_latest",
			data.NewField("ts", nil, tss),
			data.NewField("serial", nil, serials),
			data.NewField("network_id", nil, networkIDs),
			data.NewField("network_name", nil, networkNms),
			data.NewField("metric", nil, metrics),
			data.NewField("value", nil, values),
			data.NewField("raw", nil, raws),
		),
	}, nil
}

// sensorHistoryKey groups a stream of readings into one series. Using a
// struct (not a concatenated string) keeps label values clean — serials and
// network names can contain characters that would collide with separators.
type sensorHistoryKey struct {
	serial      string
	metric      string
	networkID   string
	networkName string
}

// handleSensorReadingsHistory fetches a windowed historical series and emits
// one frame per (serial, metric) pair. Each frame carries Prometheus-style
// labels on its value field so Grafana's timeseries viz can infer the legend
// and series grouping natively — without the labels we were emitting a
// single long-format frame and the panel rendered an empty chart.
//
// Time-range resolution: we ask meraki.KnownEndpointRanges to quantize the
// panel's (from, to, maxDataPoints) tuple to the nearest Meraki-allowed
// bucket before sending, otherwise the API rejects with 400.
//
// Label mode: when opts.LabelMode == "name", the `DisplayNameFromDS` on each
// frame resolves to the device name (via a cached /devices lookup) instead
// of the raw serial. Blank names fall back to the serial so we never show
// an empty legend entry.
func handleSensorReadingsHistory(ctx context.Context, client *meraki.Client, q MerakiQuery, tr TimeRange, opts Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("sensorReadingsHistory: orgId is required")
	}

	from := toRFCTime(tr.From)
	to := toRFCTime(tr.To)
	if from.IsZero() || to.IsZero() {
		return nil, fmt.Errorf("sensorReadingsHistory: time range is required")
	}

	spec, ok := meraki.KnownEndpointRanges[sensorHistoryEndpoint]
	if !ok {
		return nil, fmt.Errorf("sensorReadingsHistory: missing endpoint spec")
	}
	// Quantize resolution to panel width (MaxDataPoints).
	window, err := spec.Resolve(from, to, tr.MaxDataPoints, nil)
	if err != nil {
		return nil, fmt.Errorf("sensorReadingsHistory: resolve window: %w", err)
	}

	reqOpts := meraki.SensorReadingsHistoryOptions{
		NetworkIDs: q.NetworkIDs,
		Serials:    q.Serials,
		Metrics:    q.Metrics,
		Window:     &window,
	}
	points, err := client.GetOrganizationSensorReadingsHistory(ctx, q.OrgID, reqOpts, sensorHistoryTTL)
	if err != nil {
		return nil, err
	}

	// If the user opted into name-based labels, resolve serial→name from the
	// already-cached /devices response. A failure here is non-fatal: we log
	// via the returned frame notice path (caller) and fall back to serials.
	var nameBySerial map[string]string
	if opts.LabelMode == "name" {
		if names, lookupErr := resolveDeviceNames(ctx, client, q.OrgID, "sensor"); lookupErr == nil {
			nameBySerial = names
		}
	}

	// Group by (serial, metric) so each series ends up in its own frame.
	// We also remember the associated network so the label set is rich
	// enough for panel overrides without needing a separate lookup.
	type seriesBuf struct {
		ts     []time.Time
		values []float64
	}
	groups := make(map[sensorHistoryKey]*seriesBuf)
	for _, p := range points {
		v, ok := sensorValue(p.Metric, p.Temperature, p.Humidity, p.Door, p.Water, p.CO2, p.PM25, p.TVOC, p.Noise, p.Battery, p.IAQ)
		if !ok {
			continue
		}
		key := sensorHistoryKey{
			serial:      p.Serial,
			metric:      p.Metric,
			networkID:   p.Network.ID,
			networkName: p.Network.Name,
		}
		buf, exists := groups[key]
		if !exists {
			buf = &seriesBuf{}
			groups[key] = buf
		}
		buf.ts = append(buf.ts, p.Ts.UTC())
		buf.values = append(buf.values, v)
	}

	if len(groups) == 0 {
		// Return a single empty frame so the panel has something to bind to —
		// the UI shows "No data" gracefully when the frame has no rows.
		empty := data.NewFrame("sensor_readings_history",
			data.NewField("ts", nil, []time.Time{}),
			data.NewField("value", nil, []float64{}),
		)
		return []*data.Frame{empty}, nil
	}

	// Sort keys so frame order is deterministic — helps tests and makes the
	// legend stable across refreshes.
	keys := make([]sensorHistoryKey, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].serial != keys[j].serial {
			return keys[i].serial < keys[j].serial
		}
		return keys[i].metric < keys[j].metric
	})

	frames := make([]*data.Frame, 0, len(keys))
	for _, k := range keys {
		buf := groups[k]
		// Meraki returns readings in chronological order per sensor, but
		// because we re-grouped across pages we sort again just to be safe.
		sortByTime(buf.ts, buf.values)

		labels := data.Labels{
			"serial":       k.serial,
			"metric":       k.metric,
			"metric_label": prettyMetricName(k.metric),
		}
		if k.networkID != "" {
			labels["network_id"] = k.networkID
		}
		if k.networkName != "" {
			labels["network_name"] = k.networkName
		}

		valueField := data.NewField("value", labels, buf.values)
		// `DisplayNameFromDS` is a pre-formatted final string — Grafana does
		// NOT template-substitute it, so we compose the label here rather than
		// emit `{{serial}}` placeholders. The labels are still attached to the
		// field so panels can add their own override rules if needed.
		displayName := k.serial
		if nameBySerial != nil {
			if name := nameBySerial[k.serial]; name != "" {
				displayName = name
			}
		}
		valueField.Config = &data.FieldConfig{
			DisplayNameFromDS: displayName,
		}

		frames = append(frames, data.NewFrame("sensor_readings_history",
			data.NewField("ts", nil, buf.ts),
			valueField,
		))
	}
	return frames, nil
}

// prettyMetricName returns a human-readable label for a Meraki metric id,
// falling back to the raw id when we haven't seen it before (so new metrics
// added upstream keep working before we update the map).
func prettyMetricName(metric string) string {
	if pretty, ok := metricLabel[metric]; ok {
		return pretty
	}
	return metric
}

// sortByTime sorts two aligned slices (timestamps and their values) by the
// timestamp in ascending order. In-place, no allocation beyond the pair
// struct.
func sortByTime(ts []time.Time, values []float64) {
	if len(ts) != len(values) {
		return
	}
	idx := make([]int, len(ts))
	for i := range idx {
		idx[i] = i
	}
	sort.Slice(idx, func(i, j int) bool { return ts[idx[i]].Before(ts[idx[j]]) })

	sortedTs := make([]time.Time, len(ts))
	sortedVals := make([]float64, len(values))
	for dst, src := range idx {
		sortedTs[dst] = ts[src]
		sortedVals[dst] = values[src]
	}
	copy(ts, sortedTs)
	copy(values, sortedVals)
}

// sensorValue resolves the union-shape SensorReading into a single float64.
// Returns (value, true) when the metric matches a populated field, or
// (0, false) when no corresponding payload is present (upstream bug or
// intentionally empty reading).
//
// Boolean-valued metrics (door, water) are collapsed to 1.0/0.0 so the frame
// stays a pure number series. Use the raw JSON column on /latest if the
// panel needs the original type.
func sensorValue(
	metric string,
	temp *meraki.TemperatureReading,
	hum *meraki.HumidityReading,
	door *meraki.DoorReading,
	water *meraki.WaterReading,
	co2 *meraki.CO2Reading,
	pm25 *meraki.PM25Reading,
	tvoc *meraki.TVOCReading,
	noise *meraki.NoiseReading,
	battery *meraki.BatteryReading,
	iaq *meraki.IAQReading,
) (float64, bool) {
	switch metric {
	case "temperature":
		if temp != nil {
			return temp.Celsius, true
		}
	case "humidity":
		if hum != nil {
			return hum.RelativePercentage, true
		}
	case "door":
		if door != nil {
			if door.Open {
				return 1, true
			}
			return 0, true
		}
	case "water":
		if water != nil {
			if water.Present {
				return 1, true
			}
			return 0, true
		}
	case "co2":
		if co2 != nil {
			return co2.Concentration, true
		}
	case "pm25":
		if pm25 != nil {
			return pm25.Concentration, true
		}
	case "tvoc":
		if tvoc != nil {
			return tvoc.Concentration, true
		}
	case "noise":
		if noise != nil {
			return noise.Ambient.Level, true
		}
	case "battery":
		if battery != nil {
			return battery.Percentage, true
		}
	case "indoorAirQuality":
		if iaq != nil {
			return iaq.Score, true
		}
	}
	return 0, false
}
