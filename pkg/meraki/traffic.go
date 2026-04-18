package meraki

// L7 traffic analytics — wrappers around the four traffic endpoints used by
// the Page C (Traffic Analytics) scene. Paths and response shapes verified
// against the canonical OpenAPI spec at /openapi/api_meraki_api_v1_openapispec
// via ctx7 on 2026-04-18.
//
//   GET /networks/{networkId}/traffic
//        Returns a flat list of L7 application rows over the window
//        (max timespan 30 days). Filterable by deviceType
//        (combined / wireless / switch / appliance). Requires "traffic
//        analysis with hostname visibility" enabled on the network.
//
//   GET /networks/{networkId}/trafficAnalysis
//        Returns the network's traffic-analysis settings:
//          { "mode": "disabled" | "basic" | "detailed",
//            "customPieChartItems": [...] }
//        Used by the TrafficGuard component to surface a banner when a
//        network has traffic analysis turned off (which renders the
//        application/category breakdowns empty for that network).
//
//   GET /organizations/{organizationId}/summary/top/applications/byUsage
//   GET /organizations/{organizationId}/summary/top/applications/categories/byUsage
//        Top-N application + application-category aggregates for the org.
//        Default unit is megabytes. Timespan range: ≥ 25 minutes,
//        ≤ 186 days (default 1 day).

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"time"
)

// NetworkTrafficRow mirrors one row of `GET /networks/{networkId}/traffic`.
// The wire field names follow Meraki's spec; integer rows (`numClients`,
// `flows`, `port`) are int64 to match the rest of the codebase.
type NetworkTrafficRow struct {
	Application string  `json:"application,omitempty"`
	Destination string  `json:"destination,omitempty"`
	Protocol    string  `json:"protocol,omitempty"`
	Port        *int64  `json:"port,omitempty"`
	Sent        float64 `json:"sent,omitempty"`
	Recv        float64 `json:"recv,omitempty"`
	NumClients  int64   `json:"numClients,omitempty"`
	ActiveTime  int64   `json:"activeTime,omitempty"`
	Flows       int64   `json:"flows,omitempty"`
}

// NetworkTrafficOptions filters the per-network traffic call. Either
// Window (preferred — supports t0+timespan derived from the panel range)
// or TimespanSeconds (raw fallback) can be used.
type NetworkTrafficOptions struct {
	DeviceType      string
	Window          *TimeRangeWindow
	TimespanSeconds int
}

func (o NetworkTrafficOptions) values() url.Values {
	v := url.Values{}
	if o.DeviceType != "" {
		v.Set("deviceType", o.DeviceType)
	}
	if o.Window != nil {
		// /networks/{id}/traffic accepts t0 + timespan (NOT t0+t1). The
		// timespan is the window length in seconds.
		v.Set("t0", o.Window.T0.UTC().Format(time.RFC3339))
		v.Set("timespan", strconv.Itoa(int(o.Window.Timespan.Seconds())))
	} else if o.TimespanSeconds > 0 {
		v.Set("timespan", strconv.Itoa(o.TimespanSeconds))
	}
	return v
}

// GetNetworkTraffic returns the L7 application breakdown for a single network.
func (c *Client) GetNetworkTraffic(ctx context.Context, networkID string, opts NetworkTrafficOptions, ttl time.Duration) ([]NetworkTrafficRow, error) {
	if networkID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "networks/{networkId}/traffic", Message: "missing network id"}}
	}
	var rows []NetworkTrafficRow
	if err := c.Get(ctx,
		"networks/"+url.PathEscape(networkID)+"/traffic",
		"", opts.values(), ttl, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// NetworkTrafficAnalysisMode is the decoded shape of
// `GET /networks/{networkId}/trafficAnalysis`. Only the `mode` field is
// load-bearing for the TrafficGuard component; customPieChartItems is
// retained as raw JSON so a future panel can surface user-defined matchers
// without another wire-shape change.
type NetworkTrafficAnalysisMode struct {
	Mode                string          `json:"mode"`
	CustomPieChartItems json.RawMessage `json:"customPieChartItems,omitempty"`
}

// GetNetworkTrafficAnalysis returns the per-network traffic-analysis settings.
// The endpoint is cheap (settings, not metrics); a 5-minute TTL keeps panel
// refreshes responsive without flooding the API.
func (c *Client) GetNetworkTrafficAnalysis(ctx context.Context, networkID string, ttl time.Duration) (*NetworkTrafficAnalysisMode, error) {
	if networkID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "networks/{networkId}/trafficAnalysis", Message: "missing network id"}}
	}
	var out NetworkTrafficAnalysisMode
	if err := c.Get(ctx,
		"networks/"+url.PathEscape(networkID)+"/trafficAnalysis",
		"", nil, ttl, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// TopApplicationRow mirrors one row of
// `GET /organizations/{organizationId}/summary/top/applications/byUsage`.
// Meraki returns usage in megabytes by default — fields are float64 to
// preserve fractional values.
type TopApplicationRow struct {
	Name        string  `json:"name,omitempty"`
	Category    string  `json:"category,omitempty"`
	Total       float64 `json:"total,omitempty"`
	Downstream  float64 `json:"downstream,omitempty"`
	Upstream    float64 `json:"upstream,omitempty"`
	Percentage  float64 `json:"percentage,omitempty"`
	Clients     *TopApplicationClients `json:"clients,omitempty"`
}

// TopApplicationClients is a nested counter on the top-applications response.
type TopApplicationClients struct {
	Counts *TopApplicationClientsCounts `json:"counts,omitempty"`
}

// TopApplicationClientsCounts captures the documented `total` field; we
// retain the wrapper so a future spec evolution (per-uplink counts, etc.)
// can decode without another nested struct.
type TopApplicationClientsCounts struct {
	Total int64 `json:"total,omitempty"`
}

// TopApplicationCategoryRow mirrors one row of
// `GET /organizations/{organizationId}/summary/top/applications/categories/byUsage`.
// Same shape as TopApplicationRow without the `category` discriminator
// (each row IS a category here).
type TopApplicationCategoryRow struct {
	Name       string                 `json:"name,omitempty"`
	Total      float64                `json:"total,omitempty"`
	Downstream float64                `json:"downstream,omitempty"`
	Upstream   float64                `json:"upstream,omitempty"`
	Percentage float64                `json:"percentage,omitempty"`
	Clients    *TopApplicationClients `json:"clients,omitempty"`
}

// TopApplicationsOptions filters the org-level top-N application calls.
// Both byUsage and categories/byUsage share the same shape.
type TopApplicationsOptions struct {
	NetworkTag      string
	DeviceTag       string
	NetworkID       string
	SsidName        string
	UsageUplink     string
	Quantity        int
	TimespanSeconds int
	Window          *TimeRangeWindow
}

func (o TopApplicationsOptions) values() url.Values {
	v := url.Values{}
	if o.NetworkTag != "" {
		v.Set("networkTag", o.NetworkTag)
	}
	// The /summary/top/applications/byUsage endpoint historically accepts
	// `device` (singular) per the spec; categories accepts `deviceTag`.
	// Most operators set neither; we expose `DeviceTag` and emit it as
	// `deviceTag` because that's what the categories shape wants and the
	// applications endpoint silently ignores unknown filters.
	if o.DeviceTag != "" {
		v.Set("deviceTag", o.DeviceTag)
	}
	if o.NetworkID != "" {
		v.Set("networkId", o.NetworkID)
	}
	if o.SsidName != "" {
		v.Set("ssidName", o.SsidName)
	}
	if o.UsageUplink != "" {
		v.Set("usageUplink", o.UsageUplink)
	}
	if o.Quantity > 0 {
		// Meraki caps quantity at 50; clamp here so we never get a 400.
		q := o.Quantity
		if q > 50 {
			q = 50
		}
		v.Set("quantity", strconv.Itoa(q))
	}
	if o.Window != nil {
		v.Set("t0", o.Window.T0.UTC().Format(time.RFC3339))
		v.Set("t1", o.Window.T1.UTC().Format(time.RFC3339))
	} else if o.TimespanSeconds > 0 {
		v.Set("timespan", strconv.Itoa(o.TimespanSeconds))
	}
	return v
}

// GetOrganizationTopApplicationsByUsage returns the org-wide top-N L7
// applications ordered by total usage (default unit MB).
func (c *Client) GetOrganizationTopApplicationsByUsage(ctx context.Context, orgID string, opts TopApplicationsOptions, ttl time.Duration) ([]TopApplicationRow, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "organizations/{organizationId}/summary/top/applications/byUsage", Message: "missing organization id"}}
	}
	var rows []TopApplicationRow
	if err := c.Get(ctx,
		"organizations/"+url.PathEscape(orgID)+"/summary/top/applications/byUsage",
		orgID, opts.values(), ttl, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// GetOrganizationTopApplicationCategoriesByUsage returns the org-wide
// top-N application categories ordered by total usage. NOTE the path
// segment is `/applications/categories/byUsage` (not
// `/applicationsCategories/byUsage`) — verified against the spec on
// 2026-04-18.
func (c *Client) GetOrganizationTopApplicationCategoriesByUsage(ctx context.Context, orgID string, opts TopApplicationsOptions, ttl time.Duration) ([]TopApplicationCategoryRow, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "organizations/{organizationId}/summary/top/applications/categories/byUsage", Message: "missing organization id"}}
	}
	var rows []TopApplicationCategoryRow
	if err := c.Get(ctx,
		"organizations/"+url.PathEscape(orgID)+"/summary/top/applications/categories/byUsage",
		orgID, opts.values(), ttl, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}
