package query

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// Phase 10 (MV cameras) handlers. Cache TTLs are a balance between API load
// and dashboard freshness — onboarding state changes on human timescales so
// 5 minutes is plenty; boundary configuration changes even more rarely;
// detection counts refresh every few minutes on Meraki's side so 1m is a
// good balance between freshness and API budget.
const (
	cameraOnboardingTTL      = 5 * time.Minute
	cameraBoundariesTTL      = 15 * time.Minute
	cameraDetectionsTTL      = 1 * time.Minute
	cameraRetentionTTL       = 15 * time.Minute
	cameraDetectionsPerPage  = 1000
	cameraDetectionsMinDwell = 60 * time.Second
)

// handleCameraOnboarding emits one table-shaped frame with one row per
// camera, filtered server-side by serials[] / networkIds[]. The drilldownUrl
// column is hard-coded to productType="camera" since the endpoint is
// camera-specific.
func handleCameraOnboarding(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, opts Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("cameraOnboarding: orgId is required")
	}

	reqOpts := meraki.CameraOnboardingOptions{
		Serials:    q.Serials,
		NetworkIDs: q.NetworkIDs,
	}
	rows, err := client.GetOrganizationCameraOnboardingStatuses(ctx, q.OrgID, reqOpts, cameraOnboardingTTL)
	if err != nil {
		return nil, err
	}

	serials := make([]string, 0, len(rows))
	networkIDs := make([]string, 0, len(rows))
	networkNames := make([]string, 0, len(rows))
	statuses := make([]string, 0, len(rows))
	updatedAt := make([]time.Time, 0, len(rows))
	drilldownURLs := make([]string, 0, len(rows))
	for _, r := range rows {
		serials = append(serials, r.Serial)
		networkIDs = append(networkIDs, r.Network.ID)
		networkNames = append(networkNames, r.Network.Name)
		statuses = append(statuses, r.Status)
		var ts time.Time
		if r.UpdatedAt != nil {
			ts = r.UpdatedAt.UTC()
		}
		updatedAt = append(updatedAt, ts)
		drilldownURLs = append(drilldownURLs, deviceDrilldownURL(opts.PluginPathPrefix, "camera", r.Serial))
	}

	return []*data.Frame{
		data.NewFrame("camera_onboarding",
			data.NewField("serial", nil, serials),
			data.NewField("network_id", nil, networkIDs),
			data.NewField("network_name", nil, networkNames),
			data.NewField("status", nil, statuses),
			data.NewField("updatedAt", nil, updatedAt),
			data.NewField("drilldownUrl", nil, drilldownURLs),
		),
	}, nil
}

// handleCameraBoundaryAreas emits one table-shaped frame listing every
// configured area boundary, optionally filtered to one or more serials via
// `q.Serials`. The wrapper flattens the nested `{serial, networkId, boundaries}`
// response into one row per (serial, boundaryId) before reaching this handler.
func handleCameraBoundaryAreas(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("cameraBoundaryAreas: orgId is required")
	}
	rows, err := client.GetOrganizationCameraBoundariesAreasByDevice(ctx, q.OrgID,
		meraki.CameraBoundariesOptions{Serials: q.Serials}, cameraBoundariesTTL)
	if err != nil {
		return nil, err
	}
	return []*data.Frame{boundariesFrame("camera_boundary_areas", rows)}, nil
}

// handleCameraBoundaryLines mirrors handleCameraBoundaryAreas against the
// /lines/byDevice feed. Each boundary may carry a `directionVertex` that the
// handler preserves in the `directionVertex_x / _y` columns for panels that
// want to render crossing direction.
func handleCameraBoundaryLines(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("cameraBoundaryLines: orgId is required")
	}
	rows, err := client.GetOrganizationCameraBoundariesLinesByDevice(ctx, q.OrgID,
		meraki.CameraBoundariesOptions{Serials: q.Serials}, cameraBoundariesTTL)
	if err != nil {
		return nil, err
	}
	return []*data.Frame{boundariesFrame("camera_boundary_lines", rows)}, nil
}

// boundariesFrame is the shared table-shaped frame builder for both the
// /areas and /lines endpoints. `directionVertex_x/y` are nullable — they stay
// at nil for area boundaries where Meraki doesn't populate the field.
func boundariesFrame(name string, rows []meraki.CameraBoundary) *data.Frame {
	serials := make([]string, 0, len(rows))
	networkIDs := make([]string, 0, len(rows))
	boundaryIDs := make([]string, 0, len(rows))
	names := make([]string, 0, len(rows))
	types := make([]string, 0, len(rows))
	kinds := make([]string, 0, len(rows))
	dvX := make([]*float64, 0, len(rows))
	dvY := make([]*float64, 0, len(rows))
	for _, r := range rows {
		serials = append(serials, r.Serial)
		networkIDs = append(networkIDs, r.NetworkID)
		boundaryIDs = append(boundaryIDs, r.BoundaryID)
		names = append(names, r.Name)
		types = append(types, r.Type)
		kinds = append(kinds, r.Kind)
		if r.DirectionVertex != nil {
			x, y := r.DirectionVertex.X, r.DirectionVertex.Y
			dvX = append(dvX, &x)
			dvY = append(dvY, &y)
		} else {
			dvX = append(dvX, nil)
			dvY = append(dvY, nil)
		}
	}
	return data.NewFrame(name,
		data.NewField("serial", nil, serials),
		data.NewField("networkId", nil, networkIDs),
		data.NewField("boundaryId", nil, boundaryIDs),
		data.NewField("name", nil, names),
		data.NewField("type", nil, types),
		data.NewField("kind", nil, kinds),
		data.NewField("directionVertex_x", nil, dvX),
		data.NewField("directionVertex_y", nil, dvY),
	)
}

// detectionsKey groups samples for one chart series by
// (boundaryId, boundaryType, objectType, direction).
type detectionsKey struct {
	boundaryID   string
	boundaryKind string
	objectType   string
	direction    string // "in" | "out"
}

// handleCameraDetectionsHistory fetches detection counts per boundary per
// objectType and emits one timeseries frame per (boundaryId, boundaryType,
// objectType, direction) tuple with Prometheus-style labels on the value
// field — the standard shape for Grafana timeseries panels (§G.18).
//
// Input conventions:
//   - `q.Serials[0]` (optional) narrows to one camera; when set, the handler
//     resolves areas+lines for that serial first and passes the resulting
//     boundary IDs to the history call.
//   - `q.Metrics[0]` (optional) lets callers pass explicit boundary IDs as
//     a comma-joined list, bypassing the serial→boundaries resolution.
//   - `q.Metrics[1]` (optional) filters objectType to "person" or "vehicle";
//     when unset, both are requested so stacked charts render in one call.
//
// The handler emits an info-notice empty frame when no boundary IDs are
// resolvable — this is the common case for cameras with no boundaries
// configured, and keeps panels from flashing an error banner.
func handleCameraDetectionsHistory(ctx context.Context, client *meraki.Client, q MerakiQuery, tr TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("cameraDetectionsHistory: orgId is required")
	}

	var boundaryIDs []string
	if len(q.Metrics) > 0 && q.Metrics[0] != "" {
		for _, id := range strings.Split(q.Metrics[0], ",") {
			id = strings.TrimSpace(id)
			if id != "" {
				boundaryIDs = append(boundaryIDs, id)
			}
		}
	} else if len(q.Serials) > 0 && q.Serials[0] != "" {
		serialOpts := meraki.CameraBoundariesOptions{Serials: []string{q.Serials[0]}}
		if areas, err := client.GetOrganizationCameraBoundariesAreasByDevice(ctx, q.OrgID, serialOpts, cameraBoundariesTTL); err == nil {
			for _, a := range areas {
				boundaryIDs = append(boundaryIDs, a.BoundaryID)
			}
		}
		if lines, err := client.GetOrganizationCameraBoundariesLinesByDevice(ctx, q.OrgID, serialOpts, cameraBoundariesTTL); err == nil {
			for _, l := range lines {
				boundaryIDs = append(boundaryIDs, l.BoundaryID)
			}
		}
	}
	if len(boundaryIDs) == 0 {
		empty := data.NewFrame("camera_detections_history",
			data.NewField("ts", nil, []time.Time{}),
			data.NewField("value", nil, []int64{}),
		)
		empty.AppendNotices(data.Notice{
			Severity: data.NoticeSeverityInfo,
			Text:     "No boundaries configured for the selected camera. Configure area or line crossings in the Meraki Dashboard to populate this panel.",
		})
		return []*data.Frame{empty}, nil
	}

	var objectTypes []string
	if len(q.Metrics) > 1 && q.Metrics[1] != "" {
		objectTypes = []string{q.Metrics[1]}
	} else {
		objectTypes = []string{"person", "vehicle"}
	}

	reqOpts := meraki.CameraDetectionsHistoryOptions{
		BoundaryIDs:   boundaryIDs,
		BoundaryTypes: objectTypes,
		Duration:      cameraDetectionsMinDwell,
		PerPage:       cameraDetectionsPerPage,
	}
	if from := toRFCTime(tr.From); !from.IsZero() {
		if to := toRFCTime(tr.To); !to.IsZero() {
			reqOpts.Window = &meraki.TimeRangeWindow{T0: from.UTC(), T1: to.UTC()}
		}
	}

	samples, err := client.GetOrganizationCameraDetectionsHistoryByBoundaryByInterval(ctx, q.OrgID, reqOpts, cameraDetectionsTTL)
	if err != nil {
		return nil, err
	}

	type seriesBuf struct {
		ts     []time.Time
		values []int64
	}
	groups := make(map[detectionsKey]*seriesBuf)
	for _, s := range samples {
		startTs := s.StartTime.UTC()
		if startTs.IsZero() {
			startTs = s.EndTime.UTC()
		}
		for _, direction := range []string{"in", "out"} {
			k := detectionsKey{
				boundaryID:   s.BoundaryID,
				boundaryKind: s.BoundaryType,
				objectType:   s.ObjectType,
				direction:    direction,
			}
			buf, ok := groups[k]
			if !ok {
				buf = &seriesBuf{}
				groups[k] = buf
			}
			buf.ts = append(buf.ts, startTs)
			if direction == "in" {
				buf.values = append(buf.values, s.In)
			} else {
				buf.values = append(buf.values, s.Out)
			}
		}
	}

	if len(groups) == 0 {
		empty := data.NewFrame("camera_detections_history",
			data.NewField("ts", nil, []time.Time{}),
			data.NewField("value", nil, []int64{}),
		)
		return []*data.Frame{empty}, nil
	}

	keys := make([]detectionsKey, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].boundaryID != keys[j].boundaryID {
			return keys[i].boundaryID < keys[j].boundaryID
		}
		if keys[i].objectType != keys[j].objectType {
			return keys[i].objectType < keys[j].objectType
		}
		return keys[i].direction < keys[j].direction
	})

	frames := make([]*data.Frame, 0, len(keys))
	for _, k := range keys {
		buf := groups[k]
		sortByTimeInt64(buf.ts, buf.values)

		labels := data.Labels{
			"boundary_id":   k.boundaryID,
			"boundary_type": k.boundaryKind,
			"object_type":   k.objectType,
			"direction":     k.direction,
		}
		display := fmt.Sprintf("%s / %s / %s", k.boundaryID, k.objectType, k.direction)
		valueField := data.NewField("value", labels, buf.values)
		valueField.Config = &data.FieldConfig{DisplayNameFromDS: display}
		frames = append(frames, data.NewFrame("camera_detections_history",
			data.NewField("ts", nil, buf.ts),
			valueField,
		))
	}
	return frames, nil
}

// handleCameraRetentionProfiles emits one table-shaped frame with one row per
// (network, profile). The endpoint is per-network so multiple networks fan
// out — per-network failures continue-on-error so one stale network doesn't
// blank the whole panel.
func handleCameraRetentionProfiles(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if len(q.NetworkIDs) == 0 {
		return nil, fmt.Errorf("cameraRetentionProfiles: at least one networkId is required")
	}

	networkIDs := make([]string, len(q.NetworkIDs))
	copy(networkIDs, q.NetworkIDs)
	sort.Strings(networkIDs)

	var (
		netCol           []string
		idCol            []string
		nameCol          []string
		isDefault        []bool
		audio            []bool
		motion           []bool
		restricted       []bool
		scheduleIDCol    []string
		maxRetentionDays []int64
		firstErr         error
	)
	for _, networkID := range networkIDs {
		profiles, err := client.GetNetworkCameraQualityRetentionProfiles(ctx, networkID, cameraRetentionTTL)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, p := range profiles {
			netCol = append(netCol, networkID)
			idCol = append(idCol, p.ID)
			nameCol = append(nameCol, p.Name)
			isDefault = append(isDefault, p.IsDefault)
			audio = append(audio, p.AudioRecordingEnabled)
			motion = append(motion, p.MotionBasedRetentionEnabled)
			restricted = append(restricted, p.RestrictedBandwidthModeEnabled)
			scheduleIDCol = append(scheduleIDCol, p.ScheduleID)
			maxRetentionDays = append(maxRetentionDays, p.MaxRetentionDays)
		}
	}

	return []*data.Frame{
		data.NewFrame("camera_retention_profiles",
			data.NewField("networkId", nil, netCol),
			data.NewField("id", nil, idCol),
			data.NewField("name", nil, nameCol),
			data.NewField("isDefault", nil, isDefault),
			data.NewField("audioRecordingEnabled", nil, audio),
			data.NewField("motionBasedRetentionEnabled", nil, motion),
			data.NewField("restrictedBandwidthModeEnabled", nil, restricted),
			data.NewField("scheduleId", nil, scheduleIDCol),
			data.NewField("maxRetentionDays", nil, maxRetentionDays),
		),
	}, firstErr
}
