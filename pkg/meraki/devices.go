package meraki

import (
	"context"
	"net/url"
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
