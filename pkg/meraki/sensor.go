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

	// MT40 smart power monitor fields. Each sensor sample populates at most
	// one of these; the union is discriminated by `Metric`.
	RealPower           *RealPowerReading           `json:"realPower,omitempty"`
	ApparentPower       *ApparentPowerReading       `json:"apparentPower,omitempty"`
	Voltage             *VoltageReading             `json:"voltage,omitempty"`
	Current             *CurrentReading             `json:"current,omitempty"`
	Frequency           *FrequencyReading           `json:"frequency,omitempty"`
	PowerFactor         *PowerFactorReading         `json:"powerFactor,omitempty"`
	DownstreamPower     *DownstreamPowerReading     `json:"downstreamPower,omitempty"`
	RemoteLockoutSwitch *RemoteLockoutSwitchReading `json:"remoteLockoutSwitch,omitempty"`
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

// MT40 smart-power-monitor readings. Meraki reports power using a union of
// scalar fields with different key names per metric (`draw` vs `level` vs
// `percentage`) — keep the types narrow to mirror the wire format.
type RealPowerReading struct {
	Draw float64 `json:"draw"`
}

type ApparentPowerReading struct {
	Draw float64 `json:"draw"`
}

type VoltageReading struct {
	Level float64 `json:"level"`
}

type CurrentReading struct {
	Draw float64 `json:"draw"`
}

type FrequencyReading struct {
	Level float64 `json:"level"`
}

type PowerFactorReading struct {
	Percentage float64 `json:"percentage"`
}

type DownstreamPowerReading struct {
	Enabled bool `json:"enabled"`
}

type RemoteLockoutSwitchReading struct {
	Locked bool `json:"locked"`
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

	RealPower           *RealPowerReading           `json:"realPower,omitempty"`
	ApparentPower       *ApparentPowerReading       `json:"apparentPower,omitempty"`
	Voltage             *VoltageReading             `json:"voltage,omitempty"`
	Current             *CurrentReading             `json:"current,omitempty"`
	Frequency           *FrequencyReading           `json:"frequency,omitempty"`
	PowerFactor         *PowerFactorReading         `json:"powerFactor,omitempty"`
	DownstreamPower     *DownstreamPowerReading     `json:"downstreamPower,omitempty"`
	RemoteLockoutSwitch *RemoteLockoutSwitchReading `json:"remoteLockoutSwitch,omitempty"`
}

// SensorReadingsHistoryOptions filters a historical-readings query. When Window is non-nil it
// takes precedence; otherwise Timespan is used with the API's default `t1=now`.
//
// NOTE: the endpoint has no `interval` parameter, so resolution/quantization is
// deliberately absent from this struct. Long-range queries are chunked into
// 7-day windows by the handler rather than bucketed server-side.
type SensorReadingsHistoryOptions struct {
	NetworkIDs []string
	Serials    []string
	Metrics    []string
	Window     *TimeRangeWindow
	Timespan   time.Duration
}

func (o SensorReadingsHistoryOptions) values() url.Values {
	v := url.Values{"perPage": []string{"1000"}}
	// The /organizations/{orgId}/sensor/readings/history endpoint does not
	// accept an `interval` parameter — Meraki returns raw samples at the
	// sensor's native cadence. Sending `interval` against this endpoint is a
	// no-op at best; `t0`/`t1` or `timespan` are the only time-scope knobs.
	if o.Window != nil {
		v.Set("t0", o.Window.T0.UTC().Format(time.RFC3339))
		v.Set("t1", o.Window.T1.UTC().Format(time.RFC3339))
	} else if o.Timespan > 0 {
		v.Set("timespan", strconv.Itoa(int(o.Timespan.Seconds())))
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

// ---------------------------------------------------------------------------
// Floor plans (v0.5 §4.4.3-1e)
// ---------------------------------------------------------------------------

// FloorPlanCorner is a geo anchor on a floor plan. Meraki exposes four
// corners (bottomLeft, bottomRight, topLeft, topRight) plus a center. Each
// corner is a lat/lng pair in WGS84.
type FloorPlanCorner struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// FloorPlanDevice is a device placed on a floor plan. Meraki populates
// `lat` / `lng` when the device has anchor coordinates (either set manually
// or published from an auto-locate job); when the device is assigned to the
// plan but has no anchor, the coordinates are absent / zero.
type FloorPlanDevice struct {
	Serial      string  `json:"serial"`
	Name        string  `json:"name,omitempty"`
	Model       string  `json:"model,omitempty"`
	ProductType string  `json:"productType,omitempty"`
	Lat         float64 `json:"lat,omitempty"`
	Lng         float64 `json:"lng,omitempty"`
}

// FloorPlan mirrors `GET /networks/{networkId}/floorPlans` list entries.
// Fields match the create/update body the Meraki OpenAPI spec documents
// (center + four corners + imageContents metadata) plus the `devices`
// array populated on the list response.
type FloorPlan struct {
	ID                string            `json:"floorPlanId"`
	Name              string            `json:"name"`
	FloorNumber       *int              `json:"floorNumber,omitempty"`
	Center            *FloorPlanCorner  `json:"center,omitempty"`
	BottomLeftCorner  *FloorPlanCorner  `json:"bottomLeftCorner,omitempty"`
	BottomRightCorner *FloorPlanCorner  `json:"bottomRightCorner,omitempty"`
	TopLeftCorner     *FloorPlanCorner  `json:"topLeftCorner,omitempty"`
	TopRightCorner    *FloorPlanCorner  `json:"topRightCorner,omitempty"`
	Devices           []FloorPlanDevice `json:"devices,omitempty"`
}

// GetNetworkFloorPlans returns the floor plans configured for a Meraki
// network. Response is a plain JSON array (no pagination envelope) per the
// `GET /networks/{networkId}/floorPlans` spec; callers can feed a 15 m TTL
// since plan geometry + device anchors change rarely.
func (c *Client) GetNetworkFloorPlans(ctx context.Context, networkID string, ttl time.Duration) ([]FloorPlan, error) {
	if networkID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "networks/{networkId}/floorPlans", Message: "missing network id"}}
	}
	var out []FloorPlan
	// Floor plans are a small, unpaginated list. Use Get (single request) — if
	// Meraki ever adds pagination we'll switch to GetAll without any caller
	// change, since the endpoint path is stable.
	if err := c.Get(ctx,
		"networks/"+url.PathEscape(networkID)+"/floorPlans",
		networkID, nil, ttl, &out); err != nil {
		return nil, err
	}
	return out, nil
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
