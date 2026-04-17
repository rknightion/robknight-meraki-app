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
