package query

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// Phase 10 (MV cameras) handlers. Cache TTLs are a balance between API load
// and dashboard freshness — onboarding state changes on human timescales so
// 5 minutes is plenty; live snapshots are near-real-time so 30s keeps the
// panel responsive without hammering the API during auto-refresh bursts.
const (
	cameraOnboardingTTL           = 5 * time.Minute
	cameraAnalyticsOverviewTTL    = 1 * time.Minute
	cameraAnalyticsLiveTTL        = 30 * time.Second
	cameraAnalyticsZonesTTL       = 5 * time.Minute
	cameraAnalyticsZoneHistoryTTL = 1 * time.Minute
	cameraRetentionTTL            = 15 * time.Minute

	// cameraAnalyticsEndpoint keys the endpoint time-range spec shared between
	// the overview + zone-history handlers. The 7-day cap comes from Meraki's
	// v1 spec (MaxTimespan=7d) — longer windows 400 upstream.
	cameraAnalyticsEndpoint = "devices/{serial}/camera/analytics/overview"
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

// cameraAnalyticsKey groups per-interval points into one timeseries per
// (serial, zoneId) pair.
type cameraAnalyticsKey struct {
	serial string
	zoneID string
}

// handleCameraAnalyticsOverview emits one frame per (serial, zoneId) pair,
// each a timeseries of entrance counts. The overview endpoint is per-device,
// so multiple serials trigger a fan-out of requests. objectType defaults to
// "person" but may be overridden via q.Metrics[0] ∈ {"person","vehicle"} —
// this is the same pattern the alerts handler uses to smuggle a scalar filter
// through the generic MerakiQuery shape (§G.18).
func handleCameraAnalyticsOverview(ctx context.Context, client *meraki.Client, q MerakiQuery, tr TimeRange, opts Options) ([]*data.Frame, error) {
	if len(q.Serials) == 0 {
		return nil, fmt.Errorf("cameraAnalyticsOverview: at least one serial is required")
	}

	from := toRFCTime(tr.From)
	to := toRFCTime(tr.To)
	if from.IsZero() || to.IsZero() {
		return nil, fmt.Errorf("cameraAnalyticsOverview: time range is required")
	}

	spec, ok := meraki.KnownEndpointRanges[cameraAnalyticsEndpoint]
	if !ok {
		return nil, fmt.Errorf("cameraAnalyticsOverview: missing endpoint spec")
	}
	window, err := spec.Resolve(from, to, tr.MaxDataPoints, nil)
	if err != nil {
		return nil, fmt.Errorf("cameraAnalyticsOverview: resolve window: %w", err)
	}

	objectType := "person"
	if len(q.Metrics) > 0 && q.Metrics[0] != "" {
		objectType = q.Metrics[0]
	}

	// Name lookup is tolerant — a failed /devices call falls back to using
	// raw serials in the display name.
	var nameBySerial map[string]string
	if q.OrgID != "" {
		if names, lookupErr := resolveDeviceNames(ctx, client, q.OrgID, "camera"); lookupErr == nil {
			nameBySerial = names
		}
	}

	serials := make([]string, len(q.Serials))
	copy(serials, q.Serials)
	sort.Strings(serials)

	type seriesBuf struct {
		ts     []time.Time
		values []float64
	}
	groups := make(map[cameraAnalyticsKey]*seriesBuf)
	var firstErr error
	for _, serial := range serials {
		reqOpts := meraki.CameraAnalyticsOptions{
			Window:     &window,
			ObjectType: objectType,
		}
		points, perr := client.GetDeviceCameraAnalyticsOverview(ctx, serial, reqOpts, cameraAnalyticsOverviewTTL)
		if perr != nil {
			if firstErr == nil {
				firstErr = perr
			}
			continue
		}
		for _, p := range points {
			k := cameraAnalyticsKey{serial: serial, zoneID: p.ZoneID}
			buf, exists := groups[k]
			if !exists {
				buf = &seriesBuf{}
				groups[k] = buf
			}
			buf.ts = append(buf.ts, p.StartTs.UTC())
			buf.values = append(buf.values, float64(p.Entrances))
		}
	}

	if len(groups) == 0 {
		empty := data.NewFrame("camera_analytics_overview",
			data.NewField("ts", nil, []time.Time{}),
			data.NewField("value", nil, []float64{}),
		)
		if firstErr != nil {
			return []*data.Frame{empty}, firstErr
		}
		return []*data.Frame{empty}, nil
	}

	keys := make([]cameraAnalyticsKey, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].serial != keys[j].serial {
			return keys[i].serial < keys[j].serial
		}
		return keys[i].zoneID < keys[j].zoneID
	})

	frames := make([]*data.Frame, 0, len(keys))
	for _, k := range keys {
		buf := groups[k]
		sortByTime(buf.ts, buf.values)

		labels := data.Labels{
			"serial":      k.serial,
			"zone_id":     k.zoneID,
			"object_type": objectType,
		}
		displayName := k.serial
		if nameBySerial != nil {
			if name := nameBySerial[k.serial]; name != "" {
				displayName = name
			}
		}
		displayName = fmt.Sprintf("%s / zone %s", displayName, k.zoneID)

		valueField := data.NewField("value", labels, buf.values)
		valueField.Config = &data.FieldConfig{
			DisplayNameFromDS: displayName,
		}
		frames = append(frames, data.NewFrame("camera_analytics_overview",
			data.NewField("ts", nil, buf.ts),
			valueField,
		))
	}
	return frames, firstErr
}

// handleCameraAnalyticsLive emits one wide frame with rows per
// (serial, zoneId). Columns: serial, ts, zone_id, person, vehicle. The
// endpoint is a per-device snapshot so multiple serials trigger fan-out.
func handleCameraAnalyticsLive(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if len(q.Serials) == 0 {
		return nil, fmt.Errorf("cameraAnalyticsLive: at least one serial is required")
	}

	serials := make([]string, len(q.Serials))
	copy(serials, q.Serials)
	sort.Strings(serials)

	var (
		serialCol []string
		tsCol     []time.Time
		zoneCol   []string
		personCol []int64
		vehCol    []int64
		firstErr  error
	)
	for _, serial := range serials {
		snap, err := client.GetDeviceCameraAnalyticsLive(ctx, serial, cameraAnalyticsLiveTTL)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if snap == nil {
			continue
		}
		// Sort zone ids so frame rows are deterministic across refreshes.
		zoneIDs := make([]string, 0, len(snap.Zones))
		for zoneID := range snap.Zones {
			zoneIDs = append(zoneIDs, zoneID)
		}
		sort.Strings(zoneIDs)
		for _, zoneID := range zoneIDs {
			z := snap.Zones[zoneID]
			serialCol = append(serialCol, serial)
			tsCol = append(tsCol, snap.Ts.UTC())
			zoneCol = append(zoneCol, zoneID)
			personCol = append(personCol, z.Person)
			vehCol = append(vehCol, z.Vehicle)
		}
	}

	return []*data.Frame{
		data.NewFrame("camera_analytics_live",
			data.NewField("serial", nil, serialCol),
			data.NewField("ts", nil, tsCol),
			data.NewField("zone_id", nil, zoneCol),
			data.NewField("person", nil, personCol),
			data.NewField("vehicle", nil, vehCol),
		),
	}, firstErr
}

// handleCameraAnalyticsZones emits one flat table frame with one row per
// (serial, zoneId). Used to populate the zone-picker variable and surface
// label/type metadata on zone drill-downs.
func handleCameraAnalyticsZones(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if len(q.Serials) == 0 {
		return nil, fmt.Errorf("cameraAnalyticsZones: at least one serial is required")
	}

	serials := make([]string, len(q.Serials))
	copy(serials, q.Serials)
	sort.Strings(serials)

	var (
		serialCol []string
		zoneIDCol []string
		typeCol   []string
		labelCol  []string
		firstErr  error
	)
	for _, serial := range serials {
		zones, err := client.GetDeviceCameraAnalyticsZones(ctx, serial, cameraAnalyticsZonesTTL)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, z := range zones {
			serialCol = append(serialCol, serial)
			zoneIDCol = append(zoneIDCol, z.ZoneID)
			typeCol = append(typeCol, z.Type)
			labelCol = append(labelCol, z.Label)
		}
	}

	return []*data.Frame{
		data.NewFrame("camera_zones",
			data.NewField("serial", nil, serialCol),
			data.NewField("zoneId", nil, zoneIDCol),
			data.NewField("type", nil, typeCol),
			data.NewField("label", nil, labelCol),
		),
	}, firstErr
}

// handleCameraAnalyticsZoneHistory emits a single timeseries frame for one
// (serial, zoneId). The zoneId is smuggled via q.Metrics[0] (same pattern
// as switchPortPacketCounters); objectType defaults to "person" but may
// be overridden via q.Metrics[1].
func handleCameraAnalyticsZoneHistory(ctx context.Context, client *meraki.Client, q MerakiQuery, tr TimeRange, opts Options) ([]*data.Frame, error) {
	if len(q.Serials) == 0 || q.Serials[0] == "" {
		return nil, fmt.Errorf("cameraAnalyticsZoneHistory: serial is required (serials[0])")
	}
	if len(q.Metrics) == 0 || q.Metrics[0] == "" {
		return nil, fmt.Errorf("cameraAnalyticsZoneHistory: zoneId is required (metrics[0])")
	}
	serial := q.Serials[0]
	zoneID := q.Metrics[0]

	from := toRFCTime(tr.From)
	to := toRFCTime(tr.To)
	if from.IsZero() || to.IsZero() {
		return nil, fmt.Errorf("cameraAnalyticsZoneHistory: time range is required")
	}

	spec, ok := meraki.KnownEndpointRanges[cameraAnalyticsEndpoint]
	if !ok {
		return nil, fmt.Errorf("cameraAnalyticsZoneHistory: missing endpoint spec")
	}
	window, err := spec.Resolve(from, to, tr.MaxDataPoints, nil)
	if err != nil {
		return nil, fmt.Errorf("cameraAnalyticsZoneHistory: resolve window: %w", err)
	}

	objectType := "person"
	if len(q.Metrics) > 1 && q.Metrics[1] != "" {
		objectType = q.Metrics[1]
	}

	reqOpts := meraki.CameraAnalyticsOptions{
		Window:     &window,
		ObjectType: objectType,
	}
	points, err := client.GetDeviceCameraAnalyticsZoneHistory(ctx, serial, zoneID, reqOpts, cameraAnalyticsZoneHistoryTTL)
	if err != nil {
		return nil, err
	}

	ts := make([]time.Time, 0, len(points))
	values := make([]float64, 0, len(points))
	for _, p := range points {
		ts = append(ts, p.StartTs.UTC())
		values = append(values, float64(p.Entrances))
	}
	sortByTime(ts, values)

	// Optional name lookup for the legend — failure is non-fatal.
	displayName := serial
	if q.OrgID != "" {
		if names, lookupErr := resolveDeviceNames(ctx, client, q.OrgID, "camera"); lookupErr == nil {
			if name := names[serial]; name != "" {
				displayName = name
			}
		}
	}
	_ = opts // PluginPathPrefix irrelevant — this frame is a single series.
	displayName = fmt.Sprintf("%s / zone %s", displayName, zoneID)

	labels := data.Labels{
		"serial":      serial,
		"zone_id":     zoneID,
		"object_type": objectType,
	}
	valueField := data.NewField("value", labels, values)
	valueField.Config = &data.FieldConfig{
		DisplayNameFromDS: displayName,
	}

	return []*data.Frame{
		data.NewFrame("camera_zone_history",
			data.NewField("ts", nil, ts),
			valueField,
		),
	}, nil
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
