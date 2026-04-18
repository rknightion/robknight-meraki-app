package meraki

import (
	"context"
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
