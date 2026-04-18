package meraki

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"time"
)

// Phase 10 (MV cameras) endpoint wrappers. Paths, pagination modes, and
// response shapes verified via the canonical
// /openapi/api_meraki_api_v1_openapispec dataset on 2026-04-18.
//
// Coverage:
//   - /organizations/{organizationId}/camera/onboarding/statuses (single GET, filters)
//   - /organizations/{organizationId}/camera/boundaries/areas/byDevice (single GET)
//   - /organizations/{organizationId}/camera/boundaries/lines/byDevice (single GET)
//   - /organizations/{organizationId}/camera/detections/history/byBoundary/byInterval (single GET)
//   - /networks/{networkId}/camera/qualityRetentionProfiles (single GET)
//
// The four legacy `/camera/analytics/*` endpoints (overview, live, zones,
// zones/{id}/history) were deprecated by Meraki in March 2024 and have been
// replaced by the "boundaries" model below — boundaries are user-configured
// areas or lines on a camera, and detection counts (in/out per objectType) are
// returned per boundary by the org-level history endpoint.

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

// CameraBoundaryVertex is one (x, y) coordinate in a boundary's polygon. Both
// axes are floats in the range [0, 1] per Meraki's spec — normalised to the
// camera frame.
type CameraBoundaryVertex struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// cameraBoundaryDetail is the nested "boundaries" object embedded on the
// areas/byDevice and lines/byDevice response rows. One entry per configured
// boundary on the camera — Meraki emits N rows per camera when the camera has
// N boundaries.
type cameraBoundaryDetail struct {
	ID              string                 `json:"id"`
	Type            string                 `json:"type,omitempty"`
	Name            string                 `json:"name,omitempty"`
	Vertices        []CameraBoundaryVertex `json:"vertices,omitempty"`
	DirectionVertex *CameraBoundaryVertex  `json:"directionVertex,omitempty"`
}

// cameraBoundaryRawEntry matches the wire shape for the /areas and /lines
// responses. The outer row carries (serial, networkId) and embeds the single
// `boundaries` object — NOT an array. Meraki fans out one row per boundary so
// a camera with 3 areas produces 3 entries in the response.
type cameraBoundaryRawEntry struct {
	NetworkID  string               `json:"networkId"`
	Serial     string               `json:"serial"`
	Boundaries cameraBoundaryDetail `json:"boundaries"`
}

// CameraBoundary is one area OR line boundary flattened from the per-camera
// response. `Kind` distinguishes "area" vs "line" so a panel that combines
// both feeds can tell which endpoint surfaced it.
type CameraBoundary struct {
	Serial          string                 `json:"serial"`
	NetworkID       string                 `json:"networkId,omitempty"`
	BoundaryID      string                 `json:"boundaryId"`
	Name            string                 `json:"name,omitempty"`
	Type            string                 `json:"type,omitempty"`
	Kind            string                 `json:"kind"` // "area" | "line" — set by the wrapper
	Vertices        []CameraBoundaryVertex `json:"vertices,omitempty"`
	DirectionVertex *CameraBoundaryVertex  `json:"directionVertex,omitempty"`
}

// CameraBoundariesOptions filters the /boundaries/{areas,lines}/byDevice feeds.
// Only `serials[]` is supported by the API — the endpoint is not paginated.
type CameraBoundariesOptions struct {
	Serials []string
}

func (o CameraBoundariesOptions) values() url.Values {
	v := url.Values{}
	for _, s := range o.Serials {
		v.Add("serials[]", s)
	}
	return v
}

// GetOrganizationCameraBoundariesAreasByDevice returns every configured area
// boundary across cameras in the org. Each returned CameraBoundary has
// Kind="area". Not paginated.
func (c *Client) GetOrganizationCameraBoundariesAreasByDevice(ctx context.Context, orgID string, opts CameraBoundariesOptions, ttl time.Duration) ([]CameraBoundary, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "organizations/{organizationId}/camera/boundaries/areas/byDevice", Message: "missing organization id"}}
	}
	var raw []cameraBoundaryRawEntry
	if err := c.Get(ctx,
		"organizations/"+url.PathEscape(orgID)+"/camera/boundaries/areas/byDevice",
		orgID, opts.values(), ttl, &raw); err != nil {
		return nil, err
	}
	out := make([]CameraBoundary, 0, len(raw))
	for _, r := range raw {
		out = append(out, CameraBoundary{
			Serial:          r.Serial,
			NetworkID:       r.NetworkID,
			BoundaryID:      r.Boundaries.ID,
			Name:            r.Boundaries.Name,
			Type:            r.Boundaries.Type,
			Kind:            "area",
			Vertices:        r.Boundaries.Vertices,
			DirectionVertex: r.Boundaries.DirectionVertex,
		})
	}
	return out, nil
}

// GetOrganizationCameraBoundariesLinesByDevice returns every configured line
// boundary across cameras in the org. Each returned CameraBoundary has
// Kind="line". Not paginated.
func (c *Client) GetOrganizationCameraBoundariesLinesByDevice(ctx context.Context, orgID string, opts CameraBoundariesOptions, ttl time.Duration) ([]CameraBoundary, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "organizations/{organizationId}/camera/boundaries/lines/byDevice", Message: "missing organization id"}}
	}
	var raw []cameraBoundaryRawEntry
	if err := c.Get(ctx,
		"organizations/"+url.PathEscape(orgID)+"/camera/boundaries/lines/byDevice",
		orgID, opts.values(), ttl, &raw); err != nil {
		return nil, err
	}
	out := make([]CameraBoundary, 0, len(raw))
	for _, r := range raw {
		out = append(out, CameraBoundary{
			Serial:          r.Serial,
			NetworkID:       r.NetworkID,
			BoundaryID:      r.Boundaries.ID,
			Name:            r.Boundaries.Name,
			Type:            r.Boundaries.Type,
			Kind:            "line",
			Vertices:        r.Boundaries.Vertices,
			DirectionVertex: r.Boundaries.DirectionVertex,
		})
	}
	return out, nil
}

// CameraDetectionSample is one (boundary, objectType, interval) detection
// count. The Meraki response wraps the per-interval tuple in a `results`
// block — we decode flexibly so `results` may be either a single object or
// an array of interval samples (the spec is ambiguous and we've seen both
// patterns in adjacent endpoints).
type CameraDetectionSample struct {
	BoundaryID   string    `json:"boundaryId"`
	BoundaryType string    `json:"type,omitempty"`
	StartTime    time.Time `json:"startTime"`
	EndTime      time.Time `json:"endTime"`
	ObjectType   string    `json:"objectType,omitempty"`
	In           int64     `json:"in"`
	Out          int64     `json:"out"`
}

type cameraDetectionsRawEntry struct {
	BoundaryID   string          `json:"boundaryId"`
	BoundaryType string          `json:"type"`
	Results      json.RawMessage `json:"results"`
}

type cameraDetectionsRawResult struct {
	StartTime  time.Time `json:"startTime"`
	EndTime    time.Time `json:"endTime"`
	ObjectType string    `json:"objectType,omitempty"`
	In         int64     `json:"in"`
	Out        int64     `json:"out"`
}

// CameraDetectionsHistoryOptions filters the /detections/history/byBoundary/byInterval call.
// BoundaryIDs is required by Meraki (up to 100 ids per request). BoundaryTypes
// defaults to "person" on the server side; pass both "person" and "vehicle" to
// fan out per-objectType counts in one request.
type CameraDetectionsHistoryOptions struct {
	BoundaryIDs   []string
	BoundaryTypes []string
	Duration      time.Duration // minimum dwell time in seconds
	PerPage       int           // 1-1000, defaults to 1000
	// Window is plumbed as t0/t1 where Meraki accepts it — the documented
	// parameter set doesn't include time ranges, but adjacent camera endpoints
	// honour t0/t1/timespan so we send them defensively. Meraki ignores unknown
	// parameters without erroring.
	Window *TimeRangeWindow
}

func (o CameraDetectionsHistoryOptions) values() url.Values {
	v := url.Values{}
	for _, id := range o.BoundaryIDs {
		v.Add("boundaryIds[]", id)
	}
	for _, t := range o.BoundaryTypes {
		v.Add("boundaryTypes[]", t)
	}
	if o.Duration > 0 {
		v.Set("duration", strconv.Itoa(int(o.Duration.Seconds())))
	}
	if o.PerPage > 0 {
		v.Set("perPage", strconv.Itoa(o.PerPage))
	}
	if o.Window != nil {
		v.Set("t0", o.Window.T0.UTC().Format(time.RFC3339))
		v.Set("t1", o.Window.T1.UTC().Format(time.RFC3339))
	}
	return v
}

// GetOrganizationCameraDetectionsHistoryByBoundaryByInterval returns object-
// detection counts (in/out) per boundary per interval. The spec documents
// `results` as a single object per entry, but we decode it flexibly in case
// Meraki returns an array of per-interval results (the URL path `byInterval`
// implies timeseries shape).
//
// Boundary filtering is REQUIRED by the API — callers must pass at least one
// boundaryId. The handler upstream resolves boundaries for the camera's serial
// via the /boundaries/{areas,lines}/byDevice endpoints first.
func (c *Client) GetOrganizationCameraDetectionsHistoryByBoundaryByInterval(ctx context.Context, orgID string, opts CameraDetectionsHistoryOptions, ttl time.Duration) ([]CameraDetectionSample, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "organizations/{organizationId}/camera/detections/history/byBoundary/byInterval", Message: "missing organization id"}}
	}
	var raw []cameraDetectionsRawEntry
	if err := c.Get(ctx,
		"organizations/"+url.PathEscape(orgID)+"/camera/detections/history/byBoundary/byInterval",
		orgID, opts.values(), ttl, &raw); err != nil {
		return nil, err
	}
	out := make([]CameraDetectionSample, 0, len(raw))
	for _, r := range raw {
		if len(r.Results) == 0 || string(r.Results) == "null" {
			continue
		}
		// Try array first, fall back to single object.
		var batch []cameraDetectionsRawResult
		if err := json.Unmarshal(r.Results, &batch); err == nil {
			for _, b := range batch {
				out = append(out, CameraDetectionSample{
					BoundaryID:   r.BoundaryID,
					BoundaryType: r.BoundaryType,
					StartTime:    b.StartTime,
					EndTime:      b.EndTime,
					ObjectType:   b.ObjectType,
					In:           b.In,
					Out:          b.Out,
				})
			}
			continue
		}
		var single cameraDetectionsRawResult
		if err := json.Unmarshal(r.Results, &single); err == nil {
			out = append(out, CameraDetectionSample{
				BoundaryID:   r.BoundaryID,
				BoundaryType: r.BoundaryType,
				StartTime:    single.StartTime,
				EndTime:      single.EndTime,
				ObjectType:   single.ObjectType,
				In:           single.In,
				Out:          single.Out,
			})
		}
	}
	return out, nil
}

// CameraRetentionProfile is one entry from
// `GET /networks/{networkId}/camera/qualityRetentionProfiles`. The
// `VideoSettings` field is preserved as a map so callers can inspect the
// per-model resolution/quality overrides without us hard-depending on every
// camera family's schema.
type CameraRetentionProfile struct {
	ID                             string         `json:"id"`
	Name                           string         `json:"name,omitempty"`
	IsDefault                      bool           `json:"isDefault,omitempty"`
	AudioRecordingEnabled          bool           `json:"audioRecordingEnabled,omitempty"`
	MotionBasedRetentionEnabled    bool           `json:"motionBasedRetentionEnabled,omitempty"`
	RestrictedBandwidthModeEnabled bool           `json:"restrictedBandwidthModeEnabled,omitempty"`
	ScheduleID                     string         `json:"scheduleId,omitempty"`
	MaxRetentionDays               int64          `json:"maxRetentionDays,omitempty"`
	VideoSettings                  map[string]any `json:"videoSettings,omitempty"`
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
