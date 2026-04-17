package meraki

import (
	"context"
	"net/url"
	"strconv"
	"time"
)

// SensorReadingLatest corresponds to `GET /organizations/{orgId}/sensor/readings/latest`.
// Each element represents the latest reading for one sensor device; `readings` holds the
// per-metric samples (temperature, humidity, door, water, co2, pm25, tvoc, noise, etc.).
type SensorReadingLatest struct {
	Serial    string          `json:"serial"`
	Network   SensorNetworkRef `json:"network"`
	Readings  []SensorReading `json:"readings"`
}

type SensorNetworkRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// SensorReading is one metric sample from a sensor. Only the field matching the reading's Metric
// will be populated — Meraki uses a union shape here.
type SensorReading struct {
	Ts     time.Time             `json:"ts"`
	Metric string                `json:"metric"`

	Temperature *TemperatureReading `json:"temperature,omitempty"`
	Humidity    *HumidityReading    `json:"humidity,omitempty"`
	Door        *DoorReading        `json:"door,omitempty"`
	Water       *WaterReading       `json:"water,omitempty"`
	CO2         *CO2Reading         `json:"co2,omitempty"`
	PM25        *PM25Reading        `json:"pm25,omitempty"`
	TVOC        *TVOCReading        `json:"tvoc,omitempty"`
	Noise       *NoiseReading       `json:"noise,omitempty"`
	Battery     *BatteryReading     `json:"battery,omitempty"`
	IAQ         *IAQReading         `json:"indoorAirQuality,omitempty"`
}

type TemperatureReading struct {
	Celsius    float64 `json:"celsius"`
	Fahrenheit float64 `json:"fahrenheit"`
}

type HumidityReading struct {
	RelativePercentage float64 `json:"relativePercentage"`
}

type DoorReading struct {
	Open bool `json:"open"`
}

type WaterReading struct {
	Present bool `json:"present"`
}

type CO2Reading struct {
	Concentration float64 `json:"concentration"`
}

type PM25Reading struct {
	Concentration float64 `json:"concentration"`
}

type TVOCReading struct {
	Concentration float64 `json:"concentration"`
}

type NoiseReading struct {
	Ambient NoiseAmbient `json:"ambient"`
}

type NoiseAmbient struct {
	Level float64 `json:"level"`
}

type BatteryReading struct {
	Percentage float64 `json:"percentage"`
}

type IAQReading struct {
	Score float64 `json:"score"`
}

// SensorReadingsLatestOptions filters the latest-readings response.
type SensorReadingsLatestOptions struct {
	NetworkIDs []string
	Serials    []string
	Metrics    []string
}

func (o SensorReadingsLatestOptions) values() url.Values {
	v := url.Values{"perPage": []string{"100"}}
	for _, id := range o.NetworkIDs {
		v.Add("networkIds[]", id)
	}
	for _, s := range o.Serials {
		v.Add("serials[]", s)
	}
	for _, m := range o.Metrics {
		v.Add("metrics[]", m)
	}
	return v
}

// GetOrganizationSensorReadingsLatest returns the most recent reading per sensor.
func (c *Client) GetOrganizationSensorReadingsLatest(ctx context.Context, orgID string, opts SensorReadingsLatestOptions, ttl time.Duration) ([]SensorReadingLatest, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "organizations/{organizationId}/sensor/readings/latest", Message: "missing organization id"}}
	}
	var out []SensorReadingLatest
	_, err := c.GetAll(ctx,
		"organizations/"+url.PathEscape(orgID)+"/sensor/readings/latest",
		orgID, opts.values(), ttl, &out)
	return out, err
}

// SensorReadingHistoryPoint is one point in a historical timeseries. Meraki mirrors the
// "union" shape from /latest: only the field matching Metric is populated.
type SensorReadingHistoryPoint struct {
	Ts     time.Time `json:"ts"`
	Serial string    `json:"serial"`
	Metric string    `json:"metric"`
	Network SensorNetworkRef `json:"network"`

	Temperature *TemperatureReading `json:"temperature,omitempty"`
	Humidity    *HumidityReading    `json:"humidity,omitempty"`
	Door        *DoorReading        `json:"door,omitempty"`
	Water       *WaterReading       `json:"water,omitempty"`
	CO2         *CO2Reading         `json:"co2,omitempty"`
	PM25        *PM25Reading        `json:"pm25,omitempty"`
	TVOC        *TVOCReading        `json:"tvoc,omitempty"`
	Noise       *NoiseReading       `json:"noise,omitempty"`
	Battery     *BatteryReading     `json:"battery,omitempty"`
	IAQ         *IAQReading         `json:"indoorAirQuality,omitempty"`
}

// SensorReadingsHistoryOptions filters a historical-readings query. When Window is non-nil it
// takes precedence; otherwise Timespan is used with the API's default `t1=now`.
type SensorReadingsHistoryOptions struct {
	NetworkIDs []string
	Serials    []string
	Metrics    []string
	Window     *TimeRangeWindow
	Timespan   time.Duration
	Resolution time.Duration
}

func (o SensorReadingsHistoryOptions) values() url.Values {
	v := url.Values{"perPage": []string{"1000"}}
	if o.Window != nil {
		v.Set("t0", o.Window.T0.UTC().Format(time.RFC3339))
		v.Set("t1", o.Window.T1.UTC().Format(time.RFC3339))
		if o.Window.Resolution > 0 {
			v.Set("interval", strconv.Itoa(int(o.Window.Resolution.Seconds())))
		}
	} else if o.Timespan > 0 {
		v.Set("timespan", strconv.Itoa(int(o.Timespan.Seconds())))
		if o.Resolution > 0 {
			v.Set("interval", strconv.Itoa(int(o.Resolution.Seconds())))
		}
	}
	for _, id := range o.NetworkIDs {
		v.Add("networkIds[]", id)
	}
	for _, s := range o.Serials {
		v.Add("serials[]", s)
	}
	for _, m := range o.Metrics {
		v.Add("metrics[]", m)
	}
	return v
}

// GetOrganizationSensorReadingsHistory returns native-timeseries sensor samples. Pagination is
// handled transparently (Link header).
func (c *Client) GetOrganizationSensorReadingsHistory(ctx context.Context, orgID string, opts SensorReadingsHistoryOptions, ttl time.Duration) ([]SensorReadingHistoryPoint, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "organizations/{organizationId}/sensor/readings/history", Message: "missing organization id"}}
	}
	var out []SensorReadingHistoryPoint
	_, err := c.GetAll(ctx,
		"organizations/"+url.PathEscape(orgID)+"/sensor/readings/history",
		orgID, opts.values(), ttl, &out)
	return out, err
}
