package meraki

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

// Device mirrors the fields the plugin consumes from `GET /organizations/{orgId}/devices`.
// Meraki returns additional fields; this struct only names the ones the plugin renders.
type Device struct {
	Serial      string   `json:"serial"`
	Name        string   `json:"name,omitempty"`
	MAC         string   `json:"mac,omitempty"`
	Model       string   `json:"model,omitempty"`
	NetworkID   string   `json:"networkId,omitempty"`
	Firmware    string   `json:"firmware,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	ProductType string   `json:"productType,omitempty"`
	LanIP       string   `json:"lanIp,omitempty"`
	WAN1IP      string   `json:"wan1Ip,omitempty"`
	WAN2IP      string   `json:"wan2Ip,omitempty"`
	Lat         float64  `json:"lat,omitempty"`
	Lng         float64  `json:"lng,omitempty"`
	Address     string   `json:"address,omitempty"`
	Notes       string   `json:"notes,omitempty"`
	// FloorPlanID associates the device with a floor plan in its parent
	// network. Meraki exposes this field on getOrganizationDevices (and on
	// getDevice / updateDevice); it is the canonical link used by the
	// `sensorFloorPlan` query kind to map an MT sensor back to its floor
	// plan's anchor coordinates. Empty when the device is not placed.
	FloorPlanID string `json:"floorPlanId,omitempty"`
}

// DeviceStatusOverview is the response shape of `GET /organizations/{orgId}/devices/statuses/overview`.
type DeviceStatusOverview struct {
	Counts DeviceStatusCounts `json:"counts"`
}

type DeviceStatusCounts struct {
	ByStatus DeviceStatusByStatus `json:"byStatus"`
}

type DeviceStatusByStatus struct {
	Online    int `json:"online"`
	Alerting  int `json:"alerting"`
	Offline   int `json:"offline"`
	Dormant   int `json:"dormant"`
}

// GetOrganizationDevices paginates through every device in the given org.
func (c *Client) GetOrganizationDevices(ctx context.Context, orgID string, productTypes []string, ttl time.Duration) ([]Device, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "organizations/{organizationId}/devices", Message: "missing organization id"}}
	}
	params := url.Values{"perPage": []string{"1000"}}
	for _, pt := range productTypes {
		params.Add("productTypes[]", pt)
	}
	var devices []Device
	_, err := c.GetAll(ctx,
		"organizations/"+url.PathEscape(orgID)+"/devices",
		orgID, params, ttl, &devices)
	if err != nil {
		return nil, err
	}
	return devices, nil
}

// GetOrganizationDevicesStatusOverview returns aggregated online/alerting/offline/dormant counts.
func (c *Client) GetOrganizationDevicesStatusOverview(ctx context.Context, orgID string, productTypes []string, ttl time.Duration) (*DeviceStatusOverview, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "organizations/{organizationId}/devices/statuses/overview", Message: "missing organization id"}}
	}
	params := url.Values{}
	for _, pt := range productTypes {
		params.Add("productTypes[]", pt)
	}
	var overview DeviceStatusOverview
	if err := c.Get(ctx,
		"organizations/"+url.PathEscape(orgID)+"/devices/statuses/overview",
		orgID, params, ttl, &overview); err != nil {
		return nil, err
	}
	return &overview, nil
}

// DeviceAvailability is one row from `GET /organizations/{organizationId}/devices/availabilities`.
// Each entry captures a device's current availability (online/alerting/offline/dormant) plus
// identifying metadata and the network it belongs to. The field set mirrors the Meraki wire
// format as of v1 (2026-04).
//
// The Meraki spec does not include a "lastReportedAt" field on this endpoint — availability
// status is "current" per the documentation. Callers that need change timestamps should use
// `/organizations/{organizationId}/devices/availabilities/changeHistory` (not yet wrapped).
type DeviceAvailability struct {
	Serial      string              `json:"serial"`
	Name        string              `json:"name,omitempty"`
	MAC         string              `json:"mac,omitempty"`
	ProductType string              `json:"productType,omitempty"`
	Status      string              `json:"status,omitempty"`
	Tags        []string            `json:"tags,omitempty"`
	Network     DeviceAvailabilityNetworkRef `json:"network"`
}

// DeviceAvailabilityNetworkRef is the nested network object on DeviceAvailability.
type DeviceAvailabilityNetworkRef struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// GetOrganizationDevicesAvailabilities paginates through the org's device availability rows.
// productTypes, when non-empty, limits the response to those product families (wireless,
// switch, appliance, sensor, etc.).
func (c *Client) GetOrganizationDevicesAvailabilities(ctx context.Context, orgID string, productTypes []string, ttl time.Duration) ([]DeviceAvailability, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "organizations/{organizationId}/devices/availabilities", Message: "missing organization id"}}
	}
	params := url.Values{"perPage": []string{"1000"}}
	for _, pt := range productTypes {
		params.Add("productTypes[]", pt)
	}
	var out []DeviceAvailability
	_, err := c.GetAll(ctx,
		"organizations/"+url.PathEscape(orgID)+"/devices/availabilities",
		orgID, params, ttl, &out)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Availability-change-history endpoint wrapper (todos.txt §7.3-D). Path + response shape
// verified via ctx7 on 2026-04-18.
//
// DeviceAvailabilityChange is one state-transition entry from
// GET /organizations/{organizationId}/devices/availabilities/changeHistory. Each row
// captures a before/after status snapshot for a specific device at a specific time.
type DeviceAvailabilityChange struct {
	TS      *time.Time                      `json:"ts,omitempty"`
	Device  DeviceAvailabilityChangeDevice  `json:"device"`
	Details DeviceAvailabilityChangeDetails `json:"details"`
	Network DeviceAvailabilityChangeNetwork `json:"network"`
}

// DeviceAvailabilityChangeDevice is the compact device reference on each change entry.
type DeviceAvailabilityChangeDevice struct {
	Serial      string `json:"serial"`
	Name        string `json:"name,omitempty"`
	ProductType string `json:"productType,omitempty"`
	Model       string `json:"model,omitempty"`
}

// DeviceAvailabilityChangeDetails is the before/after envelope. Meraki encodes the
// transition as a list of (name, value) pairs; the "status" entry is the most common but
// the envelope is extensible on their side so we keep it as a slice.
type DeviceAvailabilityChangeDetails struct {
	Old []DeviceAvailabilityChangeValue `json:"old,omitempty"`
	New []DeviceAvailabilityChangeValue `json:"new,omitempty"`
}

// DeviceAvailabilityChangeValue is one entry inside details.old / details.new.
type DeviceAvailabilityChangeValue struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// DeviceAvailabilityChangeNetwork is the compact network reference on each change entry.
type DeviceAvailabilityChangeNetwork struct {
	ID   string   `json:"id"`
	Name string   `json:"name,omitempty"`
	URL  string   `json:"url,omitempty"`
	Tags []string `json:"tags,omitempty"`
}

// DeviceAvailabilityChangeOptions filters the availabilities/changeHistory call. All
// fields are optional; when left unset Meraki defaults to a 1-day window.
type DeviceAvailabilityChangeOptions struct {
	TSStart      *time.Time
	TSEnd        *time.Time
	Serials      []string
	ProductTypes []string
	NetworkIDs   []string
	Statuses     []string
	PerPage      int
}

func (o DeviceAvailabilityChangeOptions) values() url.Values {
	v := url.Values{}
	per := o.PerPage
	if per <= 0 {
		per = 1000
	}
	if per < 3 {
		per = 3
	}
	if per > 1000 {
		per = 1000
	}
	v.Set("perPage", strconv.Itoa(per))
	if o.TSStart != nil && !o.TSStart.IsZero() {
		v.Set("t0", o.TSStart.UTC().Format(time.RFC3339))
	}
	if o.TSEnd != nil && !o.TSEnd.IsZero() {
		v.Set("t1", o.TSEnd.UTC().Format(time.RFC3339))
	}
	for _, s := range o.Serials {
		v.Add("serials[]", s)
	}
	for _, pt := range o.ProductTypes {
		v.Add("productTypes[]", pt)
	}
	for _, n := range o.NetworkIDs {
		v.Add("networkIds[]", n)
	}
	for _, s := range o.Statuses {
		v.Add("statuses[]", s)
	}
	return v
}

// GetOrganizationDevicesAvailabilitiesChangeHistory walks the Link-header-paginated change
// feed for device availability transitions. Additive to GetOrganizationDevicesAvailabilities;
// do NOT call this as a substitute for current-state queries — each endpoint answers a
// different question.
func (c *Client) GetOrganizationDevicesAvailabilitiesChangeHistory(ctx context.Context, orgID string, opts DeviceAvailabilityChangeOptions, ttl time.Duration) ([]DeviceAvailabilityChange, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "organizations/{organizationId}/devices/availabilities/changeHistory", Message: "missing organization id"}}
	}
	var out []DeviceAvailabilityChange
	_, err := c.GetAll(ctx,
		"organizations/"+url.PathEscape(orgID)+"/devices/availabilities/changeHistory",
		orgID, opts.values(), ttl, &out)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// §3.3 — Device memory usage history
// ---------------------------------------------------------------------------

// DeviceMemoryHistoryPoint is one flattened (device × time interval) row from
// GET /organizations/{organizationId}/devices/system/memory/usage/history/byInterval.
//
// Response shape (verified 2026-04-18):
//
//	{"items":[{"serial":"…","network":{"id":"…","name":"…"},"intervals":[{"startTs":"…","endTs":"…","memory":{"used":{"percentages":{"maximum":N}}}}],...}],"meta":{…}}
//
// We surface the maximum used% per interval as UsagePercent since that is the
// most useful metric for identifying memory pressure. UsedBytes/FreeBytes are
// optional; populated when the spec exposes them.
type DeviceMemoryHistoryPoint struct {
	StartTs      time.Time
	EndTs        time.Time
	Serial       string
	NetworkID    string
	// UsagePercent is the maximum used memory percentage over the interval.
	UsagePercent float64
	// UsedBytes and FreeBytes are in kilobytes when present.
	UsedBytes *int64
	FreeBytes *int64
}

// DeviceMemoryHistoryOptions filters the memory usage history call.
type DeviceMemoryHistoryOptions struct {
	NetworkIDs   []string
	Serials      []string
	ProductTypes []string
	Window       *TimeRangeWindow
	Interval     time.Duration
}

func (o DeviceMemoryHistoryOptions) values() url.Values {
	// perPage: spec says 3–20 with startingAfter. Set to 20 (max) to minimise pages.
	v := url.Values{"perPage": []string{"20"}}
	for _, id := range o.NetworkIDs {
		v.Add("networkIds[]", id)
	}
	for _, s := range o.Serials {
		v.Add("serials[]", s)
	}
	for _, pt := range o.ProductTypes {
		v.Add("productTypes[]", pt)
	}
	if o.Window != nil {
		v.Set("t0", o.Window.T0.UTC().Format(time.RFC3339))
		v.Set("t1", o.Window.T1.UTC().Format(time.RFC3339))
	}
	if o.Interval > 0 {
		v.Set("interval", strconv.Itoa(int(o.Interval.Seconds())))
	}
	return v
}

// deviceMemoryHistoryResponse is the on-wire envelope for the memory endpoint.
// Uses the same items/meta wrapper shape as other org-level history endpoints.
type deviceMemoryHistoryResponse struct {
	Items []deviceMemoryHistoryDevice `json:"items"`
	Meta  struct {
		Counts struct {
			Items struct {
				Total     int `json:"total"`
				Remaining int `json:"remaining"`
			} `json:"items"`
		} `json:"counts"`
	} `json:"meta"`
}

type deviceMemoryHistoryDevice struct {
	Serial    string                    `json:"serial"`
	Network   DeviceAvailabilityNetworkRef `json:"network"`
	Intervals []deviceMemoryHistoryInterval `json:"intervals"`
}

type deviceMemoryHistoryInterval struct {
	StartTs string                   `json:"startTs"`
	EndTs   string                   `json:"endTs"`
	Memory  deviceMemoryIntervalData `json:"memory"`
}

type deviceMemoryIntervalData struct {
	Used deviceMemoryUsage `json:"used"`
	Free deviceMemoryUsage `json:"free"`
}

type deviceMemoryUsage struct {
	Minimum     *int64                    `json:"minimum,omitempty"`
	Maximum     *int64                    `json:"maximum,omitempty"`
	Median      *int64                    `json:"median,omitempty"`
	Percentages *deviceMemoryPercentages  `json:"percentages,omitempty"`
}

type deviceMemoryPercentages struct {
	Maximum float64 `json:"maximum"`
}

// GetOrganizationDevicesMemoryUsageHistoryByInterval paginates through the
// device memory usage history endpoint. The spec uses perPage 3–20 with
// Link-header pagination (confirmed by Meraki docs). Results are flattened to
// one point per (serial × interval).
func (c *Client) GetOrganizationDevicesMemoryUsageHistoryByInterval(ctx context.Context, orgID string, opts DeviceMemoryHistoryOptions, ttl time.Duration) ([]DeviceMemoryHistoryPoint, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "organizations/{organizationId}/devices/system/memory/usage/history/byInterval", Message: "missing organization id"}}
	}

	endpoint := "organizations/" + url.PathEscape(orgID) + "/devices/system/memory/usage/history/byInterval"
	params := opts.values()

	// Paginate manually because items are wrapped in an envelope (not a raw array).
	var allDevices []deviceMemoryHistoryDevice
	path := endpoint
	for pageNum := 0; pageNum < MaxPages; pageNum++ {
		var pageParams url.Values
		if pageNum == 0 {
			pageParams = params
		}
		body, hdr, err := c.Do(ctx, "GET", path, orgID, pageParams, nil)
		if err != nil {
			return nil, err
		}

		var pageResp deviceMemoryHistoryResponse
		if err := decodeJSON(body, path, &pageResp); err != nil {
			return nil, err
		}
		allDevices = append(allDevices, pageResp.Items...)

		next := nextLink(hdr)
		if next == "" {
			break
		}
		path = next
	}

	// Flatten to per-device per-interval points.
	var out []DeviceMemoryHistoryPoint
	for _, dev := range allDevices {
		for _, iv := range dev.Intervals {
			startTs, _ := time.Parse(time.RFC3339, iv.StartTs)
			endTs, _ := time.Parse(time.RFC3339, iv.EndTs)
			pt := DeviceMemoryHistoryPoint{
				StartTs:   startTs,
				EndTs:     endTs,
				Serial:    dev.Serial,
				NetworkID: dev.Network.ID,
			}
			if iv.Memory.Used.Percentages != nil {
				pt.UsagePercent = iv.Memory.Used.Percentages.Maximum
			}
			pt.UsedBytes = iv.Memory.Used.Maximum
			pt.FreeBytes = iv.Memory.Free.Maximum
			out = append(out, pt)
		}
	}
	return out, nil
}

// decodeJSON is a small helper that wraps json.Unmarshal with a descriptive error.
func decodeJSON(body []byte, path string, out any) error {
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("meraki: decode %s: %w", path, err)
	}
	return nil
}
