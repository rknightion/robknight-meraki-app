package query

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// Page A (Clients) handlers — v0.5 §4.4.4.
//
// TTLs match the plan:
//   - clientsList: 1m   (per-network, fan-out across selected networks)
//   - clientLookup: 1m  (org-wide MAC search)
//   - clientSessions: 1m (per-client wireless latency history bucket list)
//
// clientsOverview already has a handler in insights.go (handleClientsOverview);
// Page A reuses it directly via QueryKind.ClientsOverview from the frontend.
const (
	clientsListTTL    = 1 * time.Minute
	clientLookupTTL   = 1 * time.Minute
	clientSessionsTTL = 1 * time.Minute

	clientLatencyHistoryEndpoint = "networks/{networkId}/wireless/clients/{clientId}/latencyHistory"
)

// handleClientsList fans out a /networks/{id}/clients call across every
// network in q.NetworkIDs and emits a single wide table frame. We default to
// a 24h timespan when the panel time range collapses to zero; longer ranges
// get clamped to the endpoint's 31-day max.
//
// q.Metrics[0] (when set) is forwarded as the `mac` filter — handy for the
// "find this MAC anywhere in this network" case from the search tab. For the
// "all clients on selected networks" Top Talkers tab, leave Metrics empty.
//
// Frame columns (one row per client per network):
//
//	ts (firstSeen) | networkId | mac | ip | hostname | vlan | ssid |
//	  status | usageSentKb | usageRecvKb | usageTotalKb | firstSeen | lastSeen |
//	  manufacturer | os | recentDeviceSerial | recentDeviceName
func handleClientsList(ctx context.Context, client *meraki.Client, q MerakiQuery, tr TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		// orgId isn't strictly required by the underlying endpoint — the
		// network ID is the authoritative scope — but every Page A panel ships
		// it so we keep the validation symmetric with the rest of the plugin.
		return nil, fmt.Errorf("clientsList: orgId is required")
	}
	if len(q.NetworkIDs) == 0 {
		return nil, fmt.Errorf("clientsList: at least one networkId is required")
	}

	// Pick a timespan: prefer the panel range (clamped to 31d), fall back to
	// the explicit override on q.TimespanSeconds, then to 24h as the default
	// "Top talkers in the last day" view.
	timespan := time.Duration(0)
	if tr.From > 0 && tr.To > tr.From {
		timespan = time.Duration(tr.To-tr.From) * time.Millisecond
	} else if q.TimespanSeconds > 0 {
		timespan = time.Duration(q.TimespanSeconds) * time.Second
	}
	if timespan <= 0 {
		timespan = 24 * time.Hour
	}
	if timespan > 31*24*time.Hour {
		timespan = 31 * 24 * time.Hour
	}

	opts := meraki.NetworkClientsOptions{
		Timespan: timespan,
		MAC:      firstNonEmpty(q.Metrics),
	}

	var (
		tss                 []time.Time
		networkIDs          []string
		macs                []string
		ips                 []string
		hostnames           []string
		vlans               []string
		ssids               []string
		statuses            []string
		usageSent           []float64
		usageRecv           []float64
		usageTotal          []float64
		firstSeens          []time.Time
		lastSeens           []time.Time
		manufacturers       []string
		oses                []string
		recentDeviceSerials []string
		recentDeviceNames   []string
		clientIDs           []string
	)

	var lastErr error
	for _, networkID := range q.NetworkIDs {
		if networkID == "" {
			continue
		}
		clients, err := client.GetNetworkClients(ctx, networkID, opts, clientsListTTL)
		if err != nil {
			// Surface only the last error — empty-network 404s are common when
			// a network was just deleted, and we still want a partial frame from
			// the other networks rather than blank-screening the panel.
			lastErr = err
			continue
		}
		for _, c := range clients {
			usage := meraki.DecodeClientUsage(c.UsageRaw)
			tssVal := time.Time{}
			if c.LastSeen != nil {
				tssVal = c.LastSeen.UTC()
			} else if c.FirstSeen != nil {
				tssVal = c.FirstSeen.UTC()
			}
			tss = append(tss, tssVal)
			networkIDs = append(networkIDs, networkID)
			macs = append(macs, c.MAC)
			ips = append(ips, c.IP)
			hostnames = append(hostnames, hostnameForClient(c))
			vlans = append(vlans, string(c.VLAN))
			ssids = append(ssids, c.SSID)
			statuses = append(statuses, c.Status)
			usageSent = append(usageSent, usage.Sent)
			usageRecv = append(usageRecv, usage.Recv)
			usageTotal = append(usageTotal, usage.Total)
			if c.FirstSeen != nil {
				firstSeens = append(firstSeens, c.FirstSeen.UTC())
			} else {
				firstSeens = append(firstSeens, time.Time{})
			}
			if c.LastSeen != nil {
				lastSeens = append(lastSeens, c.LastSeen.UTC())
			} else {
				lastSeens = append(lastSeens, time.Time{})
			}
			manufacturers = append(manufacturers, c.Manufacturer)
			oses = append(oses, c.OS)
			recentDeviceSerials = append(recentDeviceSerials, c.RecentDeviceSerial)
			recentDeviceNames = append(recentDeviceNames, c.RecentDeviceName)
			clientIDs = append(clientIDs, c.ID)
		}
	}

	frame := data.NewFrame("clients_list",
		data.NewField("ts", nil, tss),
		data.NewField("networkId", nil, networkIDs),
		data.NewField("clientId", nil, clientIDs),
		data.NewField("mac", nil, macs),
		data.NewField("ip", nil, ips),
		data.NewField("hostname", nil, hostnames),
		data.NewField("vlan", nil, vlans),
		data.NewField("ssid", nil, ssids),
		data.NewField("status", nil, statuses),
		data.NewField("usageSentKb", nil, usageSent),
		data.NewField("usageRecvKb", nil, usageRecv),
		data.NewField("usageTotalKb", nil, usageTotal),
		data.NewField("firstSeen", nil, firstSeens),
		data.NewField("lastSeen", nil, lastSeens),
		data.NewField("manufacturer", nil, manufacturers),
		data.NewField("os", nil, oses),
		data.NewField("recentDeviceSerial", nil, recentDeviceSerials),
		data.NewField("recentDeviceName", nil, recentDeviceNames),
	)

	// If every network errored and we have nothing to show, surface the last
	// error to the dispatcher so it can attach it as a notice.
	if lastErr != nil && len(macs) == 0 {
		return []*data.Frame{frame}, lastErr
	}
	return []*data.Frame{frame}, nil
}

// handleClientLookup wraps /organizations/{id}/clients/search?mac=... and
// emits a single-row frame for each matching record (one network per record).
// When the MAC is unknown, emits a zero-row frame with a `data.Notice`
// `Info` notice so the search-results panel renders a friendly empty state
// instead of an error banner. Mirrors the §G.20 zero-row + notice pattern
// from `configuration_changes_annotation.go`.
//
// Required q.Metrics[0] = MAC. The frontend builds this from the search
// box; an empty MAC short-circuits with an error notice (the API would 400
// anyway).
func handleClientLookup(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("clientLookup: orgId is required")
	}
	mac := firstNonEmpty(q.Metrics)
	if mac == "" {
		// Return an empty frame so the panel binds successfully — a notice on
		// top tells the user what's missing.
		empty := emptyClientLookupFrame()
		empty.AppendNotices(data.Notice{
			Severity: data.NoticeSeverityInfo,
			Text:     "Enter a MAC address (or partial MAC) to search.",
		})
		return []*data.Frame{empty}, nil
	}

	result, err := client.SearchOrganizationClient(ctx, q.OrgID, meraki.ClientSearchOptions{MAC: mac}, clientLookupTTL)
	if err != nil {
		// 404 is expected when the MAC isn't known — emit a zero-row frame with
		// an Info notice so the panel renders a friendly empty state.
		var notFound *meraki.NotFoundError
		if errors.As(err, &notFound) {
			empty := emptyClientLookupFrame()
			empty.AppendNotices(data.Notice{
				Severity: data.NoticeSeverityInfo,
				Text:     fmt.Sprintf("No client found for MAC %q.", mac),
			})
			return []*data.Frame{empty}, nil
		}
		return nil, err
	}

	if result == nil || len(result.Records) == 0 {
		empty := emptyClientLookupFrame()
		empty.AppendNotices(data.Notice{
			Severity: data.NoticeSeverityInfo,
			Text:     fmt.Sprintf("No client found for MAC %q.", mac),
		})
		return []*data.Frame{empty}, nil
	}

	var (
		clientIDs    []string
		macs         []string
		descriptions []string
		networkIDs   []string
		networkNms   []string
		ips          []string
		users        []string
		oses         []string
		ssids        []string
		vlans        []string
		statuses     []string
		usageSent    []float64
		usageRecv    []float64
		usageTotal   []float64
		firstSeens   []time.Time
		lastSeens    []time.Time
		recentSerial []string
		recentName   []string
	)

	for _, r := range result.Records {
		usage := meraki.DecodeClientUsage(r.UsageRaw)
		clientIDs = append(clientIDs, firstNonEmptyStrings(r.ClientID, result.ClientID))
		macs = append(macs, firstNonEmptyStrings(result.MAC, mac))
		descriptions = append(descriptions, firstNonEmptyStrings(r.Description, result.Description))
		networkIDs = append(networkIDs, r.Network.ID)
		networkNms = append(networkNms, r.Network.Name)
		ips = append(ips, r.IP)
		users = append(users, firstNonEmptyStrings(r.User, result.User))
		oses = append(oses, firstNonEmptyStrings(r.OS, result.OS))
		ssids = append(ssids, r.SSID)
		vlans = append(vlans, string(r.VLAN))
		statuses = append(statuses, r.Status)
		usageSent = append(usageSent, usage.Sent)
		usageRecv = append(usageRecv, usage.Recv)
		usageTotal = append(usageTotal, usage.Total)
		if r.FirstSeen != nil {
			firstSeens = append(firstSeens, r.FirstSeen.UTC())
		} else {
			firstSeens = append(firstSeens, time.Time{})
		}
		if r.LastSeen != nil {
			lastSeens = append(lastSeens, r.LastSeen.UTC())
		} else {
			lastSeens = append(lastSeens, time.Time{})
		}
		recentSerial = append(recentSerial, r.RecentDeviceSerial)
		recentName = append(recentName, r.RecentDeviceName)
	}

	return []*data.Frame{
		data.NewFrame("client_lookup",
			data.NewField("clientId", nil, clientIDs),
			data.NewField("mac", nil, macs),
			data.NewField("description", nil, descriptions),
			data.NewField("networkId", nil, networkIDs),
			data.NewField("networkName", nil, networkNms),
			data.NewField("ip", nil, ips),
			data.NewField("user", nil, users),
			data.NewField("os", nil, oses),
			data.NewField("ssid", nil, ssids),
			data.NewField("vlan", nil, vlans),
			data.NewField("status", nil, statuses),
			data.NewField("usageSentKb", nil, usageSent),
			data.NewField("usageRecvKb", nil, usageRecv),
			data.NewField("usageTotalKb", nil, usageTotal),
			data.NewField("firstSeen", nil, firstSeens),
			data.NewField("lastSeen", nil, lastSeens),
			data.NewField("recentDeviceSerial", nil, recentSerial),
			data.NewField("recentDeviceName", nil, recentName),
		),
	}, nil
}

// handleClientSessions emits one frame per traffic category (background,
// bestEffort, video, voice) plus an "overall" series, each labelled with the
// client id. Per-series frames follow the §G.18 convention: long-format frames
// would render as an empty chart.
//
// q.NetworkIDs[0] = network id, q.Metrics[0] = client id (a Meraki client key
// such as `kABC123`, MAC, or IP depending on Track-By config). Both are
// required.
func handleClientSessions(ctx context.Context, client *meraki.Client, q MerakiQuery, tr TimeRange, _ Options) ([]*data.Frame, error) {
	if len(q.NetworkIDs) == 0 || q.NetworkIDs[0] == "" {
		return nil, fmt.Errorf("clientSessions: networkId (q.networkIds[0]) is required")
	}
	clientID := firstNonEmpty(q.Metrics)
	if clientID == "" {
		return nil, fmt.Errorf("clientSessions: clientId (q.metrics[0]) is required")
	}

	from := toRFCTime(tr.From)
	to := toRFCTime(tr.To)
	if from.IsZero() || to.IsZero() {
		// Default to 7 days back when the panel didn't pass a range — matches
		// the per-client drilldown's default lookback hint.
		now := time.Now().UTC()
		to = now
		from = now.Add(-7 * 24 * time.Hour)
	}

	spec, ok := meraki.KnownEndpointRanges[clientLatencyHistoryEndpoint]
	if !ok {
		return nil, fmt.Errorf("clientSessions: missing endpoint spec")
	}
	window, err := spec.Resolve(from, to, tr.MaxDataPoints, nil)
	if err != nil {
		return nil, fmt.Errorf("clientSessions: resolve window: %w", err)
	}

	opts := meraki.ClientLatencyHistoryOptions{
		Window:     &window,
		Resolution: window.Resolution,
	}
	entries, err := client.GetNetworkWirelessClientLatencyHistory(ctx, q.NetworkIDs[0], clientID, opts, clientSessionsTTL)
	if err != nil {
		return nil, err
	}

	// Sort chronologically — the API already returns ordered buckets but
	// re-sorting here matches the rest of the timeseries handlers.
	sort.Slice(entries, func(i, j int) bool { return entries[i].StartTs.Before(entries[j].StartTs) })

	if len(entries) == 0 {
		// Empty placeholder so the panel still binds; the noValue label on the
		// scene viz will surface the empty state.
		return []*data.Frame{data.NewFrame("client_sessions",
			data.NewField("ts", nil, []time.Time{}),
			data.NewField("value", nil, []float64{}),
		)}, nil
	}

	type series struct {
		label  string
		pick   func(meraki.ClientLatencyHistoryEntry) float64
	}
	categories := []series{
		{"overall", func(e meraki.ClientLatencyHistoryEntry) float64 { return e.AvgLatencyMs }},
		{"background", func(e meraki.ClientLatencyHistoryEntry) float64 { return e.BackgroundAvgMs }},
		{"bestEffort", func(e meraki.ClientLatencyHistoryEntry) float64 { return e.BestEffortAvgMs }},
		{"video", func(e meraki.ClientLatencyHistoryEntry) float64 { return e.VideoAvgMs }},
		{"voice", func(e meraki.ClientLatencyHistoryEntry) float64 { return e.VoiceAvgMs }},
	}

	frames := make([]*data.Frame, 0, len(categories))
	for _, cat := range categories {
		ts := make([]time.Time, 0, len(entries))
		vs := make([]float64, 0, len(entries))
		hasAny := false
		for _, e := range entries {
			ts = append(ts, e.StartTs.UTC())
			v := cat.pick(e)
			vs = append(vs, v)
			if v > 0 {
				hasAny = true
			}
		}
		// Elide categories that never observed any latency in the window —
		// mirrors handleApiRequestsByInterval's "skip empty class" rule so the
		// legend stays tidy on quiet clients.
		if !hasAny && cat.label != "overall" {
			continue
		}
		labels := data.Labels{
			"clientId": clientID,
			"category": cat.label,
		}
		valueField := data.NewField("value", labels, vs)
		valueField.Config = &data.FieldConfig{
			DisplayNameFromDS: cat.label,
			Unit:              "ms",
		}
		frames = append(frames, data.NewFrame("client_sessions",
			data.NewField("ts", nil, ts),
			valueField,
		))
	}
	return frames, nil
}

// emptyClientLookupFrame builds the zero-row client_lookup frame with the
// full column set so the panel binds the same fields regardless of which
// branch emitted it.
func emptyClientLookupFrame() *data.Frame {
	return data.NewFrame("client_lookup",
		data.NewField("clientId", nil, []string{}),
		data.NewField("mac", nil, []string{}),
		data.NewField("description", nil, []string{}),
		data.NewField("networkId", nil, []string{}),
		data.NewField("networkName", nil, []string{}),
		data.NewField("ip", nil, []string{}),
		data.NewField("user", nil, []string{}),
		data.NewField("os", nil, []string{}),
		data.NewField("ssid", nil, []string{}),
		data.NewField("vlan", nil, []string{}),
		data.NewField("status", nil, []string{}),
		data.NewField("usageSentKb", nil, []float64{}),
		data.NewField("usageRecvKb", nil, []float64{}),
		data.NewField("usageTotalKb", nil, []float64{}),
		data.NewField("firstSeen", nil, []time.Time{}),
		data.NewField("lastSeen", nil, []time.Time{}),
		data.NewField("recentDeviceSerial", nil, []string{}),
		data.NewField("recentDeviceName", nil, []string{}),
	)
}

// hostnameForClient picks the friendliest available identifier for the
// client. Description is the user-supplied label in the Meraki dashboard;
// `user` is the 802.1x identity; `manufacturer` is a last-resort hint so
// unidentified clients still get a recognisable label rather than a blank
// cell.
func hostnameForClient(c meraki.NetworkClient) string {
	if c.Description != "" {
		return c.Description
	}
	if c.User != "" {
		return c.User
	}
	if c.DeviceTypePrediction != "" {
		return c.DeviceTypePrediction
	}
	if c.Manufacturer != "" {
		return c.Manufacturer
	}
	return ""
}

// firstNonEmptyStrings returns the first non-empty string in the variadic
// list, or "" when every value is empty. Variadic sibling of `firstNonEmpty`
// (which takes a slice). Used for "prefer record-level identity, fall back to
// envelope identity, fall back to query input".
func firstNonEmptyStrings(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
