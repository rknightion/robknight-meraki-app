package query

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// v0.5 §4.4.3-1a — new MR query kinds:
//   - wirelessClientCountHistory  (per-network, per-SSID optional, timeseries)
//   - wirelessFailedConnections   (per-network wide table of events)
//   - wirelessLatencyStats        (per-network latency history timeseries)
//   - deviceRadioStatus           (org-wide radio/band status snapshot)
//
// All four are additive and reuse the existing MerakiQuery wire shape. No new
// transport fields; SSID filter for client count is passed via q.Metrics[0]
// when present (option-overload pattern documented in pkg/plugin/query/CLAUDE.md).

const (
	wirelessClientCountTTL      = 1 * time.Minute
	wirelessFailedConnTTL       = 5 * time.Minute
	wirelessLatencyTTL          = 5 * time.Minute
	wirelessRadioStatusTTL      = 15 * time.Minute

	wirelessClientCountEndpoint = "networks/{networkId}/wireless/clientCountHistory"
	wirelessFailedConnEndpoint  = "networks/{networkId}/wireless/failedConnections"
	wirelessLatencyEndpoint     = "networks/{networkId}/wireless/latencyHistory"
)

// handleWirelessClientCountHistory fetches /networks/{id}/wireless/clientCountHistory
// for each network in q.NetworkIDs and emits one timeseries frame per network,
// with `network_id` and `network_name` labels on the value field. When
// q.Metrics carries a single entry, it is treated as an SSID-number filter —
// the emitted frames additionally carry an `ssid` label.
//
// Endpoint variant: GET /networks/{networkId}/wireless/clientCountHistory
// (documented, 31-day max per spec; plan clamps to 7-day via KnownEndpointRanges).
// The endpoint returns a flat [{startTs,endTs,clientCount}] array; we loop
// per-network in this handler so the frame shape stays one-per-network.
func handleWirelessClientCountHistory(ctx context.Context, client *meraki.Client, q MerakiQuery, tr TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("wirelessClientCountHistory: orgId is required")
	}
	if len(q.NetworkIDs) == 0 {
		return nil, fmt.Errorf("wirelessClientCountHistory: at least one networkId is required")
	}

	from := toRFCTime(tr.From)
	to := toRFCTime(tr.To)
	if from.IsZero() || to.IsZero() {
		return nil, fmt.Errorf("wirelessClientCountHistory: time range is required")
	}
	spec, ok := meraki.KnownEndpointRanges[wirelessClientCountEndpoint]
	if !ok {
		return nil, fmt.Errorf("wirelessClientCountHistory: missing endpoint spec")
	}
	window, err := spec.Resolve(from, to, tr.MaxDataPoints, nil)
	if err != nil {
		return nil, fmt.Errorf("wirelessClientCountHistory: resolve window: %w", err)
	}

	ssidFilter := ""
	if len(q.Metrics) > 0 {
		ssidFilter = q.Metrics[0]
	}

	// Best-effort network-name lookup for legends.
	nameByID := make(map[string]string)
	if networks, lookupErr := client.GetOrganizationNetworks(ctx, q.OrgID, []string{"wireless"}, networksTTL); lookupErr == nil {
		for _, n := range networks {
			nameByID[n.ID] = n.Name
		}
	}

	networkIDs := make([]string, len(q.NetworkIDs))
	copy(networkIDs, q.NetworkIDs)
	sort.Strings(networkIDs)

	frames := make([]*data.Frame, 0, len(networkIDs))
	var firstErr error
	for _, nid := range networkIDs {
		reqOpts := meraki.WirelessClientCountOptions{
			Window: &window,
			SSID:   ssidFilter,
		}
		points, cErr := client.GetNetworkWirelessClientCountHistory(ctx, nid, reqOpts, wirelessClientCountTTL)
		if cErr != nil {
			if firstErr == nil {
				firstErr = cErr
			}
			continue
		}
		if len(points) == 0 {
			continue
		}
		ts := make([]time.Time, 0, len(points))
		vals := make([]float64, 0, len(points))
		for _, p := range points {
			ts = append(ts, p.StartTs.UTC())
			vals = append(vals, float64(p.ClientCount))
		}
		netName := nameByID[nid]
		labels := data.Labels{"network_id": nid}
		if netName != "" {
			labels["network_name"] = netName
		}
		if ssidFilter != "" {
			labels["ssid"] = ssidFilter
		}
		display := netName
		if display == "" {
			display = nid
		}
		if ssidFilter != "" {
			display = fmt.Sprintf("%s / SSID %s", display, ssidFilter)
		}
		valueField := data.NewField("value", labels, vals)
		valueField.Config = &data.FieldConfig{
			DisplayNameFromDS: display,
			Unit:              "short",
		}
		frames = append(frames, data.NewFrame("wireless_client_count_history",
			data.NewField("ts", nil, ts),
			valueField,
		))
	}

	if len(frames) == 0 {
		empty := data.NewFrame("wireless_client_count_history",
			data.NewField("ts", nil, []time.Time{}),
			data.NewField("value", nil, []float64{}),
		)
		if firstErr != nil {
			return []*data.Frame{empty}, firstErr
		}
		return []*data.Frame{empty}, nil
	}
	return frames, firstErr
}

// handleWirelessFailedConnections emits one wide table frame with the
// aggregated failure counts per (serial, ssidNumber, type). Meraki's
// /networks/{id}/wireless/failedConnections returns one row per event — we
// group server-side so the panel sees a compact KPI-style table with a
// `count` column ready for stacking / sorting.
//
// Endpoint variant: GET /networks/{networkId}/wireless/failedConnections
// (documented, 180-day lookback, t1 ≤ t0 + 7d — timerange.go clamps to 7d).
func handleWirelessFailedConnections(ctx context.Context, client *meraki.Client, q MerakiQuery, tr TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("wirelessFailedConnections: orgId is required")
	}
	if len(q.NetworkIDs) == 0 {
		return nil, fmt.Errorf("wirelessFailedConnections: at least one networkId is required")
	}

	from := toRFCTime(tr.From)
	to := toRFCTime(tr.To)
	if from.IsZero() || to.IsZero() {
		return nil, fmt.Errorf("wirelessFailedConnections: time range is required")
	}
	spec, ok := meraki.KnownEndpointRanges[wirelessFailedConnEndpoint]
	if !ok {
		return nil, fmt.Errorf("wirelessFailedConnections: missing endpoint spec")
	}
	window, err := spec.Resolve(from, to, tr.MaxDataPoints, nil)
	if err != nil {
		return nil, fmt.Errorf("wirelessFailedConnections: resolve window: %w", err)
	}

	networkIDs := make([]string, len(q.NetworkIDs))
	copy(networkIDs, q.NetworkIDs)
	sort.Strings(networkIDs)

	type aggKey struct {
		serial string
		ssid   int
		typ    string
	}
	counts := make(map[aggKey]int64)

	var firstErr error
	for _, nid := range networkIDs {
		reqOpts := meraki.WirelessFailedConnectionsOptions{Window: &window}
		rows, cErr := client.GetNetworkWirelessFailedConnections(ctx, nid, reqOpts, wirelessFailedConnTTL)
		if cErr != nil {
			if firstErr == nil {
				firstErr = cErr
			}
			continue
		}
		for _, r := range rows {
			k := aggKey{serial: r.Serial, ssid: r.SsidNumber, typ: r.Type}
			counts[k]++
		}
	}

	keys := make([]aggKey, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].serial != keys[j].serial {
			return keys[i].serial < keys[j].serial
		}
		if keys[i].ssid != keys[j].ssid {
			return keys[i].ssid < keys[j].ssid
		}
		return keys[i].typ < keys[j].typ
	})

	serials := make([]string, 0, len(keys))
	ssids := make([]int64, 0, len(keys))
	types := make([]string, 0, len(keys))
	countsCol := make([]int64, 0, len(keys))
	for _, k := range keys {
		serials = append(serials, k.serial)
		ssids = append(ssids, int64(k.ssid))
		types = append(types, k.typ)
		countsCol = append(countsCol, counts[k])
	}

	frame := data.NewFrame("wireless_failed_connections",
		data.NewField("serial", nil, serials),
		data.NewField("ssid", nil, ssids),
		data.NewField("type", nil, types),
		data.NewField("count", nil, countsCol),
	)
	return []*data.Frame{frame}, firstErr
}

// handleWirelessLatencyStats fetches /networks/{id}/wireless/latencyHistory
// for each network in q.NetworkIDs and emits one timeseries frame per
// (network, access-category). `avgLatencyMs` + the four per-category fields
// are each surfaced as their own series when non-zero, labelled with
// {network_id, category}.
//
// Endpoint variant: GET /networks/{networkId}/wireless/latencyHistory
// (documented, 31-day max per spec; plan clamps to 7d).
func handleWirelessLatencyStats(ctx context.Context, client *meraki.Client, q MerakiQuery, tr TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("wirelessLatencyStats: orgId is required")
	}
	if len(q.NetworkIDs) == 0 {
		return nil, fmt.Errorf("wirelessLatencyStats: at least one networkId is required")
	}
	from := toRFCTime(tr.From)
	to := toRFCTime(tr.To)
	if from.IsZero() || to.IsZero() {
		return nil, fmt.Errorf("wirelessLatencyStats: time range is required")
	}
	spec, ok := meraki.KnownEndpointRanges[wirelessLatencyEndpoint]
	if !ok {
		return nil, fmt.Errorf("wirelessLatencyStats: missing endpoint spec")
	}
	window, err := spec.Resolve(from, to, tr.MaxDataPoints, nil)
	if err != nil {
		return nil, fmt.Errorf("wirelessLatencyStats: resolve window: %w", err)
	}

	nameByID := make(map[string]string)
	if networks, lookupErr := client.GetOrganizationNetworks(ctx, q.OrgID, []string{"wireless"}, networksTTL); lookupErr == nil {
		for _, n := range networks {
			nameByID[n.ID] = n.Name
		}
	}

	networkIDs := make([]string, len(q.NetworkIDs))
	copy(networkIDs, q.NetworkIDs)
	sort.Strings(networkIDs)

	// emitSeries appends one frame for (network, category, values).
	type catSeries struct {
		label  string
		values []float64
	}

	frames := make([]*data.Frame, 0, len(networkIDs))
	var firstErr error
	for _, nid := range networkIDs {
		reqOpts := meraki.WirelessLatencyOptions{Window: &window}
		points, cErr := client.GetNetworkWirelessLatencyHistory(ctx, nid, reqOpts, wirelessLatencyTTL)
		if cErr != nil {
			if firstErr == nil {
				firstErr = cErr
			}
			continue
		}
		if len(points) == 0 {
			continue
		}
		ts := make([]time.Time, 0, len(points))
		avg := make([]float64, 0, len(points))
		bg := make([]float64, 0, len(points))
		be := make([]float64, 0, len(points))
		video := make([]float64, 0, len(points))
		voice := make([]float64, 0, len(points))
		var haveBG, haveBE, haveVideo, haveVoice bool
		for _, p := range points {
			ts = append(ts, p.StartTs.UTC())
			avg = append(avg, p.AvgLatencyMs)
			bg = append(bg, p.BackgroundTrafficMs)
			be = append(be, p.BestEffortTrafficMs)
			video = append(video, p.VideoTrafficMs)
			voice = append(voice, p.VoiceTrafficMs)
			if p.BackgroundTrafficMs != 0 {
				haveBG = true
			}
			if p.BestEffortTrafficMs != 0 {
				haveBE = true
			}
			if p.VideoTrafficMs != 0 {
				haveVideo = true
			}
			if p.VoiceTrafficMs != 0 {
				haveVoice = true
			}
		}

		netName := nameByID[nid]
		display := netName
		if display == "" {
			display = nid
		}

		seriesList := []catSeries{{label: "avg", values: avg}}
		if haveBG {
			seriesList = append(seriesList, catSeries{label: "background", values: bg})
		}
		if haveBE {
			seriesList = append(seriesList, catSeries{label: "bestEffort", values: be})
		}
		if haveVideo {
			seriesList = append(seriesList, catSeries{label: "video", values: video})
		}
		if haveVoice {
			seriesList = append(seriesList, catSeries{label: "voice", values: voice})
		}

		for _, s := range seriesList {
			labels := data.Labels{"network_id": nid, "category": s.label}
			if netName != "" {
				labels["network_name"] = netName
			}
			valueField := data.NewField("value", labels, append([]float64(nil), s.values...))
			valueField.Config = &data.FieldConfig{
				DisplayNameFromDS: fmt.Sprintf("%s / %s", display, s.label),
				Unit:              "ms",
			}
			// ts must be a fresh slice per frame because frames don't share fields.
			tsCopy := append([]time.Time(nil), ts...)
			frames = append(frames, data.NewFrame("wireless_latency_history",
				data.NewField("ts", nil, tsCopy),
				valueField,
			))
		}
	}

	if len(frames) == 0 {
		empty := data.NewFrame("wireless_latency_history",
			data.NewField("ts", nil, []time.Time{}),
			data.NewField("value", nil, []float64{}),
		)
		if firstErr != nil {
			return []*data.Frame{empty}, firstErr
		}
		return []*data.Frame{empty}, nil
	}
	return frames, firstErr
}

// handleDeviceRadioStatus emits one wide table frame with one row per wireless
// AP, showing which radio bands are currently broadcasting plus an overall
// enabled flag.
//
// Endpoint variant: org-wide
// GET /organizations/{organizationId}/wireless/ssids/statuses/byDevice
// is used because Meraki does NOT expose a true
// /organizations/{id}/wireless/devices/radioSettings/bySsid endpoint — the
// only per-device radio-settings GET is /devices/{serial}/wireless/radio/settings
// which would require one call per AP. The ssids/statuses/byDevice response
// contains a `basicServiceSets` array per device whose `radio.band` +
// `radio.isBroadcasting` + `enabled` flags give us the same "is this band
// enabled on this AP?" signal the plan calls for. Per §2.2 we prefer
// org-wide, hence this substitution; see commit body for rationale.
func handleDeviceRadioStatus(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("deviceRadioStatus: orgId is required")
	}

	rows, err := client.GetOrganizationWirelessSsidsStatusesByDevice(ctx, q.OrgID, meraki.WirelessSsidStatusOptions{
		NetworkIDs: q.NetworkIDs,
		Serials:    q.Serials,
	}, wirelessRadioStatusTTL)
	if err != nil {
		return nil, err
	}

	// Best-effort name lookup.
	nameBySerial := make(map[string]string)
	if devs, lookupErr := client.GetOrganizationDevices(ctx, q.OrgID, []string{"wireless"}, devicesTTL); lookupErr == nil {
		for _, d := range devs {
			nameBySerial[d.Serial] = d.Name
		}
	}

	serials := make([]string, 0, len(rows))
	names := make([]string, 0, len(rows))
	band24 := make([]bool, 0, len(rows))
	band5 := make([]bool, 0, len(rows))
	band6 := make([]bool, 0, len(rows))
	enabled := make([]bool, 0, len(rows))

	// Sort deterministically by serial.
	sort.Slice(rows, func(i, j int) bool { return rows[i].Serial < rows[j].Serial })

	for _, r := range rows {
		serials = append(serials, r.Serial)
		names = append(names, nameBySerial[r.Serial])
		band24 = append(band24, r.Band24Active)
		band5 = append(band5, r.Band5Active)
		band6 = append(band6, r.Band6Active)
		enabled = append(enabled, r.AnyEnabled)
	}

	frame := data.NewFrame("device_radio_status",
		data.NewField("serial", nil, serials),
		data.NewField("name", nil, names),
		data.NewField("band2_4", nil, band24),
		data.NewField("band5", nil, band5),
		data.NewField("band6", nil, band6),
		data.NewField("enabled", nil, enabled),
	)
	return []*data.Frame{frame}, nil
}
