package meraki

import (
	"context"
	"net/url"
	"time"
)

// Phase 10 (MV cameras) endpoint wrappers. Paths, pagination modes, and
// response shapes verified via ctx7 against the canonical
// /openapi/api_meraki_api_v1_openapispec dataset on 2026-04-17.
//
// Coverage:
//   - /organizations/{organizationId}/camera/onboarding/statuses (single GET, filters)
//   - /devices/{serial}/camera/analytics/overview (single GET, t0/t1 window)
//   - /devices/{serial}/camera/analytics/live (single GET, snapshot)
//   - /devices/{serial}/camera/analytics/zones (single GET)
//   - /devices/{serial}/camera/analytics/zones/{zoneId}/history (single GET, windowed)
//   - /networks/{networkId}/camera/qualityRetentionProfiles (single GET)

// CameraOnboardingNetworkRef is the compact network reference embedded on every
// onboarding status row. The `Name` field is populated on the dashboard-friendly
// responses; `ID` is always present.
type CameraOnboardingNetworkRef struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// CameraOnboardingStatus is one entry from
// `GET /organizations/{orgId}/camera/onboarding/statuses`. Status values per
// the v1 spec: "complete" | "incomplete" | "unboxed" | "connected".
type CameraOnboardingStatus struct {
	Serial    string                     `json:"serial"`
	Network   CameraOnboardingNetworkRef `json:"network"`
	Status    string                     `json:"status,omitempty"`
	UpdatedAt *time.Time                 `json:"updatedAt,omitempty"`
}

// CameraOnboardingOptions filters the onboarding/statuses call. Both filter
// fields map to `serials[]` and `networkIds[]` query params. The endpoint is
// NOT paginated (returns a single array).
type CameraOnboardingOptions struct {
	Serials    []string
	NetworkIDs []string
}

func (o CameraOnboardingOptions) values() url.Values {
	v := url.Values{}
	for _, s := range o.Serials {
		v.Add("serials[]", s)
	}
	for _, id := range o.NetworkIDs {
		v.Add("networkIds[]", id)
	}
	return v
}

// GetOrganizationCameraOnboardingStatuses returns camera onboarding status
// rows for the org. Not paginated.
func (c *Client) GetOrganizationCameraOnboardingStatuses(ctx context.Context, orgID string, opts CameraOnboardingOptions, ttl time.Duration) ([]CameraOnboardingStatus, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "organizations/{organizationId}/camera/onboarding/statuses", Message: "missing organization id"}}
	}
	var out []CameraOnboardingStatus
	if err := c.Get(ctx,
		"organizations/"+url.PathEscape(orgID)+"/camera/onboarding/statuses",
		orgID, opts.values(), ttl, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CameraAnalyticsOverviewPoint is one interval sample from
// `GET /devices/{serial}/camera/analytics/overview`. Timestamps mark the
// start/end of the aggregation window; `zoneId` "0" is the full camera frame
// per the v1 spec (Meraki calls it the "all" zone).
type CameraAnalyticsOverviewPoint struct {
	StartTs      time.Time `json:"startTs"`
	EndTs        time.Time `json:"endTs"`
	ZoneID       string    `json:"zoneId"`
	Entrances    int64     `json:"entrances"`
	AverageCount float64   `json:"averageCount"`
}

// CameraAnalyticsOptions filters the camera analytics overview + zone-history
// endpoints. Both accept t0/t1 or timespan (default 1h, max 7d) and an
// optional objectType ∈ {"person","vehicle"} (default "person").
type CameraAnalyticsOptions struct {
	Window     *TimeRangeWindow
	ObjectType string
}

func (o CameraAnalyticsOptions) values() url.Values {
	v := url.Values{}
	if o.Window != nil {
		v.Set("t0", o.Window.T0.UTC().Format(time.RFC3339))
		v.Set("t1", o.Window.T1.UTC().Format(time.RFC3339))
	}
	if o.ObjectType != "" {
		v.Set("objectType", o.ObjectType)
	}
	return v
}

// GetDeviceCameraAnalyticsOverview returns aggregated person/vehicle counts
// per zone per interval for one camera. Not paginated.
func (c *Client) GetDeviceCameraAnalyticsOverview(ctx context.Context, serial string, opts CameraAnalyticsOptions, ttl time.Duration) ([]CameraAnalyticsOverviewPoint, error) {
	if serial == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "devices/{serial}/camera/analytics/overview", Message: "missing serial"}}
	}
	var out []CameraAnalyticsOverviewPoint
	if err := c.Get(ctx,
		"devices/"+url.PathEscape(serial)+"/camera/analytics/overview",
		"", opts.values(), ttl, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CameraLiveZone is one zone's per-class count in a live snapshot.
type CameraLiveZone struct {
	Person  int64 `json:"person"`
	Vehicle int64 `json:"vehicle"`
}

// CameraAnalyticsLiveSnapshot is the response shape of
// `GET /devices/{serial}/camera/analytics/live`. The `zones` map is keyed by
// zone id (as a string); the "0" key is the all-frame zone.
type CameraAnalyticsLiveSnapshot struct {
	Ts    time.Time                 `json:"ts"`
	Zones map[string]CameraLiveZone `json:"zones"`
}

// GetDeviceCameraAnalyticsLive returns a single point-in-time snapshot of the
// per-zone person/vehicle counts for one camera. Not paginated.
func (c *Client) GetDeviceCameraAnalyticsLive(ctx context.Context, serial string, ttl time.Duration) (*CameraAnalyticsLiveSnapshot, error) {
	if serial == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "devices/{serial}/camera/analytics/live", Message: "missing serial"}}
	}
	var out CameraAnalyticsLiveSnapshot
	if err := c.Get(ctx,
		"devices/"+url.PathEscape(serial)+"/camera/analytics/live",
		"", nil, ttl, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CameraAnalyticsZone is one row from
// `GET /devices/{serial}/camera/analytics/zones`. `Type` is typically
// "person" or "vehicle" (or empty for the "all" zone); `Label` is the
// user-supplied name.
type CameraAnalyticsZone struct {
	ZoneID string `json:"zoneId"`
	Type   string `json:"type,omitempty"`
	Label  string `json:"label,omitempty"`
}

// GetDeviceCameraAnalyticsZones returns the zone configuration for one
// camera. Not paginated.
func (c *Client) GetDeviceCameraAnalyticsZones(ctx context.Context, serial string, ttl time.Duration) ([]CameraAnalyticsZone, error) {
	if serial == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "devices/{serial}/camera/analytics/zones", Message: "missing serial"}}
	}
	var out []CameraAnalyticsZone
	if err := c.Get(ctx,
		"devices/"+url.PathEscape(serial)+"/camera/analytics/zones",
		"", nil, ttl, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CameraAnalyticsZoneHistoryPoint is one interval sample from
// `GET /devices/{serial}/camera/analytics/zones/{zoneId}/history`. Same shape
// as the overview response minus the per-zone fan-out (since the zone is
// already fixed in the URL path).
type CameraAnalyticsZoneHistoryPoint struct {
	StartTs      time.Time `json:"startTs"`
	EndTs        time.Time `json:"endTs"`
	Entrances    int64     `json:"entrances"`
	AverageCount float64   `json:"averageCount"`
}

// GetDeviceCameraAnalyticsZoneHistory returns the per-interval history for a
// single zone. Not paginated.
func (c *Client) GetDeviceCameraAnalyticsZoneHistory(ctx context.Context, serial, zoneID string, opts CameraAnalyticsOptions, ttl time.Duration) ([]CameraAnalyticsZoneHistoryPoint, error) {
	if serial == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "devices/{serial}/camera/analytics/zones/{zoneId}/history", Message: "missing serial"}}
	}
	if zoneID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "devices/{serial}/camera/analytics/zones/{zoneId}/history", Message: "missing zone id"}}
	}
	var out []CameraAnalyticsZoneHistoryPoint
	if err := c.Get(ctx,
		"devices/"+url.PathEscape(serial)+"/camera/analytics/zones/"+url.PathEscape(zoneID)+"/history",
		"", opts.values(), ttl, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CameraRetentionProfile is one entry from
// `GET /networks/{networkId}/camera/qualityRetentionProfiles`. The
// `VideoSettings` field is preserved as a map so callers can inspect the
// per-model resolution/quality overrides without us hard-depending on every
// camera family's schema.
type CameraRetentionProfile struct {
	ID                              string                 `json:"id"`
	Name                            string                 `json:"name,omitempty"`
	IsDefault                       bool                   `json:"isDefault,omitempty"`
	AudioRecordingEnabled           bool                   `json:"audioRecordingEnabled,omitempty"`
	MotionBasedRetentionEnabled     bool                   `json:"motionBasedRetentionEnabled,omitempty"`
	RestrictedBandwidthModeEnabled  bool                   `json:"restrictedBandwidthModeEnabled,omitempty"`
	ScheduleID                      string                 `json:"scheduleId,omitempty"`
	MaxRetentionDays                int64                  `json:"maxRetentionDays,omitempty"`
	VideoSettings                   map[string]any         `json:"videoSettings,omitempty"`
}

// GetNetworkCameraQualityRetentionProfiles returns the retention profile list
// for one network. Not paginated.
func (c *Client) GetNetworkCameraQualityRetentionProfiles(ctx context.Context, networkID string, ttl time.Duration) ([]CameraRetentionProfile, error) {
	if networkID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "networks/{networkId}/camera/qualityRetentionProfiles", Message: "missing network id"}}
	}
	var out []CameraRetentionProfile
	if err := c.Get(ctx,
		"networks/"+url.PathEscape(networkID)+"/camera/qualityRetentionProfiles",
		"", nil, ttl, &out); err != nil {
		return nil, err
	}
	return out, nil
}
