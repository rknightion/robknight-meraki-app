// Package query implements the Meraki-specific query handlers that translate
// MerakiQuery objects (sent by the nested datasource) into Grafana data.Frames.
//
// The dispatcher is deliberately shallow: each QueryKind maps to one handler
// function that talks to the Meraki API via the shared meraki.Client owned by
// the app plugin. Per-query errors are captured as frame notices so a single
// misconfigured panel doesn't kill every other panel in the request batch.
package query

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// QueryKind enumerates the Meraki-backed operations the dispatcher knows how
// to run. The string values are the wire-format discriminator shared with
// src/datasource/types.ts (MerakiQuery.kind).
type QueryKind string

const (
	KindOrganizations         QueryKind = "organizations"
	KindOrganizationsCount    QueryKind = "organizationsCount"
	KindNetworks              QueryKind = "networks"
	KindNetworksCount         QueryKind = "networksCount"
	KindDevices               QueryKind = "devices"
	KindDeviceStatusOverview  QueryKind = "deviceStatusOverview"
	KindDeviceAvailabilities      QueryKind = "deviceAvailabilities"
	KindDeviceAvailabilityCounts  QueryKind = "deviceAvailabilityCounts"
	KindOrgProductTypes           QueryKind = "orgProductTypes"
	KindSensorReadingsLatest  QueryKind = "sensorReadingsLatest"
	KindSensorReadingsHistory QueryKind = "sensorReadingsHistory"
	KindSensorAlertSummary    QueryKind = "sensorAlertSummary"
	// §4.4.3-1e — floor-plan layout + latest readings per MT sensor. Emits a
	// wide frame with nullable lat/lng so the panel can switch to a grid
	// layout when no anchor coordinates are configured.
	KindSensorFloorPlan QueryKind = "sensorFloorPlan"

	// Wireless (MR) — phase 5.
	KindWirelessChannelUtil QueryKind = "wirelessChannelUtil"
	KindWirelessUsage       QueryKind = "wirelessUsage"
	KindNetworkSsids        QueryKind = "networkSsids"
	KindApClients           QueryKind = "apClients"

	// Alerts (assurance) — phase 6.
	KindAlerts         QueryKind = "alerts"
	KindAlertsOverview QueryKind = "alertsOverview"

	// Switch (MS) — phase 7.
	KindSwitchPorts              QueryKind = "switchPorts"
	KindSwitchPortConfig         QueryKind = "switchPortConfig"
	KindSwitchPortPacketCounters QueryKind = "switchPortPacketCounters"
	KindSwitchPortsOverview      QueryKind = "switchPortsOverview"

	// Appliance (MX) — phase 8.
	KindApplianceUplinkStatuses  QueryKind = "applianceUplinkStatuses"
	KindApplianceUplinksOverview QueryKind = "applianceUplinksOverview"
	KindApplianceVpnStatuses     QueryKind = "applianceVpnStatuses"
	KindApplianceVpnStats        QueryKind = "applianceVpnStats"
	KindDeviceUplinksLossLatency QueryKind = "deviceUplinksLossLatency"
	KindAppliancePortForwarding  QueryKind = "appliancePortForwarding"
	KindApplianceSettings        QueryKind = "applianceSettings"

	// §2.2 — per-device uplink loss/latency history (31-day window).
	KindDeviceUplinksLossLatencyHistory QueryKind = "deviceUplinksLossLatencyHistory"

	// §3.5 — MX uplinks usage history + org-wide usage by network.
	KindApplianceUplinksUsageHistory    QueryKind = "applianceUplinksUsageHistory"
	KindApplianceUplinksUsageByNetwork  QueryKind = "applianceUplinksUsageByNetwork"

	// Insights (licensing / API usage / clients) — phase 9.
	KindLicensesOverview      QueryKind = "licensesOverview"
	KindLicensesList          QueryKind = "licensesList"
	KindApiRequestsOverview   QueryKind = "apiRequestsOverview"
	KindApiRequestsByInterval QueryKind = "apiRequestsByInterval"
	KindClientsOverview       QueryKind = "clientsOverview"
	KindTopClients            QueryKind = "topClients"
	KindTopDevices            QueryKind = "topDevices"
	KindTopDeviceModels       QueryKind = "topDeviceModels"
	KindTopSsids              QueryKind = "topSsids"
	KindTopSwitchesByEnergy   QueryKind = "topSwitchesByEnergy"
	KindTopNetworksByStatus   QueryKind = "topNetworksByStatus"

	// Camera (MV) — phase 10. The legacy `analytics/*` endpoints were
	// deprecated by Meraki in March 2024 and replaced by the boundaries model
	// (areas + lines + per-boundary detection counts).
	KindCameraOnboarding        QueryKind = "cameraOnboarding"
	KindCameraBoundaryAreas     QueryKind = "cameraBoundaryAreas"
	KindCameraBoundaryLines     QueryKind = "cameraBoundaryLines"
	KindCameraDetectionsHistory QueryKind = "cameraDetectionsHistory"
	KindCameraRetentionProfiles QueryKind = "cameraRetentionProfiles"

	// Cellular Gateway (MG) — phase 10.
	KindMgUplinks        QueryKind = "mgUplinks"
	KindMgPortForwarding QueryKind = "mgPortForwarding"
	KindMgLan            QueryKind = "mgLan"
	KindMgConnectivity   QueryKind = "mgConnectivity"

	// Network events — phase 11.
	KindNetworkEvents         QueryKind = "networkEvents"
	KindNetworkEventsTimeline QueryKind = "networkEventsTimeline"

	// API optimisation — §7.3 (phase 12).
	KindConfigurationChanges        QueryKind = "configurationChanges"
	KindDeviceAvailabilityChanges   QueryKind = "deviceAvailabilityChanges"

	// §2.1 — org-level AP client counts (replaces N per-AP fan-out on the overview page).
	KindWirelessApClientCounts QueryKind = "wirelessApClientCounts"

	// §3.2 — additional wireless kinds: packet loss, ethernet statuses, CPU load history.
	KindWirelessPacketLossByNetwork     QueryKind = "wirelessPacketLossByNetwork"
	KindWirelessDevicesEthernetStatuses QueryKind = "wirelessDevicesEthernetStatuses"
	KindWirelessDevicesCpuLoadHistory   QueryKind = "wirelessDevicesCpuLoadHistory"

	// §3.1 — Switch ports overview by speed + usage history.
	// Note: KindSwitchPortsOverview (= "switchPortsOverview") already exists as the KPI row.
	KindSwitchPortsOverviewBySpeed QueryKind = "switchPortsOverviewBySpeed"
	KindSwitchPortsUsageHistory    QueryKind = "switchPortsUsageHistory"

	// §3.3 — Device memory usage history.
	KindDeviceMemoryHistory QueryKind = "deviceMemoryHistory"

	// §3.4 — Alerts overview byNetwork + historical.
	KindAlertsOverviewByNetwork  QueryKind = "alertsOverviewByNetwork"
	KindAlertsOverviewHistorical QueryKind = "alertsOverviewHistorical"

	// §4.4.3-1c — MX traffic shaping snapshot + uplink failover event timeline.
	// applianceVpnHeatmap is a reshape of the existing applianceVpnStatuses
	// feed into a (peerNetworkName, sourceNetworkName, value) long-format
	// table the Grafana heatmap viz can consume; kept as a new kind rather
	// than reshaping applianceVpnStatuses in place because the existing
	// flattened table is also used by a tests / a future follow-up panel.
	KindApplianceTrafficShaping  QueryKind = "applianceTrafficShaping"
	KindApplianceFailoverEvents  QueryKind = "applianceFailoverEvents"
	KindApplianceVpnHeatmap      QueryKind = "applianceVpnHeatmap"

	// §4.4.2 — v0.5 Phase 0 plumbing. configurationChangesAnnotation reshapes
	// the configurationChanges feed into a Grafana annotation frame for
	// data-layer overlay. alertsMttrSummary aggregates alert resolution times
	// into a single wide KPI frame shared by the MTTR chart (§4.4.3 1f) and
	// the new Org Health page (§4.4.4).
	KindConfigurationChangesAnnotation QueryKind = "configurationChangesAnnotation"
	KindAlertsMttrSummary              QueryKind = "alertsMttrSummary"

	// §4.4.3-1a — MR panels: per-network client-count timeseries, per-network
	// failed-connection wide table, per-network latency timeseries, and an
	// org-wide radio/band-status snapshot.
	KindWirelessClientCountHistory QueryKind = "wirelessClientCountHistory"
	KindWirelessFailedConnections  QueryKind = "wirelessFailedConnections"
	KindWirelessLatencyStats       QueryKind = "wirelessLatencyStats"
	KindDeviceRadioStatus          QueryKind = "deviceRadioStatus"

	// §4.4.3-1b — MS (switches) panels: PoE draw, STP topology, MAC table,
	// VLAN distribution. All snapshot kinds. Port-error timeline reshapes the
	// existing switchPortPacketCounters kind (no new kind).
	KindSwitchPoe          QueryKind = "switchPoe"
	KindSwitchStp          QueryKind = "switchStp"
	KindSwitchMacTable     QueryKind = "switchMacTable"
	KindSwitchVlansSummary QueryKind = "switchVlansSummary"

	// §4.4.3-1f — cross-cutting. Union of configurationChanges + networkEvents
	// over the last 24 hours for the Home "what just changed" tile. Always a
	// fixed 24h lookback; panel time range is ignored so the Home tile is
	// stable regardless of dashboard picker.
	KindOrgChangeFeed QueryKind = "orgChangeFeed"

	// §4.4.4-A — Clients page kinds. clientsOverview already exists (kind
	// `clientsOverview` from phase 9); these three are net-new.
	//   - clientsList:    fan-out /networks/{id}/clients across q.NetworkIDs.
	//   - clientLookup:   org-wide /clients/search?mac=q.Metrics[0].
	//   - clientSessions: per-client /networks/{id}/wireless/clients/{id}/latencyHistory.
	KindClientsList    QueryKind = "clientsList"
	KindClientLookup   QueryKind = "clientLookup"
	KindClientSessions QueryKind = "clientSessions"

	// §4.4.4-B — Firmware & Lifecycle page. Three new kinds:
	//   - firmwareUpgrades: org-wide past + scheduled upgrade events.
	//   - firmwarePending:  per-device pending/in-progress upgrades
	//                       (currentUpgradesOnly=true; MS+MR only per
	//                       Meraki's documented limitation as of 2026-04).
	//   - deviceEol:        inventory devices with EOX status (sourced from
	//                       /inventory/devices?eoxStatuses[]= — Meraki
	//                       exposes EOL data via the API; no hand-maintained
	//                       table required).
	KindFirmwareUpgrades QueryKind = "firmwareUpgrades"
	KindFirmwarePending  QueryKind = "firmwarePending"
	KindDeviceEol        QueryKind = "deviceEol"

	// §4.4.4-C — Traffic Analytics page. Three primary kinds (per-network L7
	// rows + org-wide top apps + top categories) plus a settings lookup the
	// TrafficGuard React component uses to render a banner when traffic
	// analysis is disabled on a selected network. Modelled as a separate
	// `networkTrafficAnalysisMode` kind rather than piggy-backing the mode on
	// `networkTraffic` because the guard needs to inspect every selected
	// network independently of whether the user is currently looking at the
	// per-network table panel (and the guard polls on a longer cadence).
	KindNetworkTraffic                  QueryKind = "networkTraffic"
	KindTopApplicationsByUsage          QueryKind = "topApplicationsByUsage"
	KindTopApplicationCategoriesByUsage QueryKind = "topApplicationCategoriesByUsage"
	KindNetworkTrafficAnalysisMode      QueryKind = "networkTrafficAnalysisMode"

	// §4.4.4-D — Topology / Network Map page. networkGeo aggregates per-
	// network centroid lat/lng (derived from device coordinates because the
	// Meraki networks endpoint does not carry geo). deviceLldpCdp emits the
	// two-frame Grafana Node Graph contract (nodes + edges) for the per-
	// network device link graph; org-wide fan-out is intentionally disabled.
	KindNetworkGeo    QueryKind = "networkGeo"
	KindDeviceLldpCdp QueryKind = "deviceLldpCdp"

	// §4.4.4-E — Org Health Overview. Cross-family single-row wide KPI frame
	// fanned out in parallel over 6 existing handlers (deviceStatusOverview,
	// alertsOverview, licensesList, firmwarePending, apiRequestsByInterval,
	// applianceUplinkStatuses). Backs the Home merge in §4.4.5; no dedicated
	// page ships in this phase.
	KindOrgHealthSummary QueryKind = "orgHealthSummary"

	// §4.4.5 — availability-by-family stacked-bar reshape. Reuses the same
	// underlying Meraki call as KindDeviceAvailabilityCounts but emits one
	// row per productType so the Home "availability by family" bar can stack
	// the status buckets. See device_status_by_family.go for the shape.
	KindDeviceStatusByFamily QueryKind = "deviceStatusByFamily"

	// Single-field offline count, used by the device-offline alert template.
	// deviceAvailabilityCounts emits five fields and SSE reduce produces one
	// labelled sample per field, so a `gt 0` threshold against it would always
	// fire (online > 0 in any healthy fleet). This kind narrows to one int64
	// `count` field so the standard reduce → threshold chain works.
	KindDeviceOfflineCount QueryKind = "deviceOfflineCount"
)

// MerakiQuery mirrors the TypeScript MerakiQuery shape. It is the per-panel
// payload inside QueryRequest.Queries.
type MerakiQuery struct {
	RefID           string    `json:"refId"`
	Kind            QueryKind `json:"kind"`
	OrgID           string    `json:"orgId,omitempty"`
	NetworkIDs      []string  `json:"networkIds,omitempty"`
	Serials         []string  `json:"serials,omitempty"`
	ProductTypes    []string  `json:"productTypes,omitempty"`
	Metrics         []string  `json:"metrics,omitempty"`
	TimespanSeconds int       `json:"timespanSeconds,omitempty"`
	Hide            bool      `json:"hide,omitempty"`
}

// TimeRange is Grafana's panel time range in unix milliseconds (same encoding
// as backend.DataQuery.TimeRange once JSON-serialized by the datasource).
type TimeRange struct {
	From int64 `json:"from"` // unix ms
	To   int64 `json:"to"`   // unix ms
	// MaxDataPoints mirrors QueryRequest.MaxDataPoints so handlers can
	// quantize resolution to the panel width.
	MaxDataPoints int64 `json:"maxDataPoints,omitempty"`
}

// QueryRequest is the POST body sent to /resources/query.
type QueryRequest struct {
	Range         TimeRange     `json:"range"`
	MaxDataPoints int64         `json:"maxDataPoints"`
	IntervalMs    int64         `json:"intervalMs"`
	Queries       []MerakiQuery `json:"queries"`
}

// QueryResponse is the wire shape returned to the datasource. Each frame has
// already been serialized via data.FrameToJSON so the datasource can forward
// it to Grafana without re-parsing.
type QueryResponse struct {
	Frames []json.RawMessage `json:"frames"`
}

// MetricFindRequest is the POST body sent to /resources/metricFind. Variable
// queries always carry a single MerakiQuery.
type MetricFindRequest struct {
	Query MerakiQuery `json:"query"`
}

// MetricFindValue is one {text, value} pair returned by a variable query.
// Value is any so we can return strings (most common) or numbers.
type MetricFindValue struct {
	Text  string `json:"text"`
	Value any    `json:"value,omitempty"`
}

// MetricFindResponse is the wire shape returned to the datasource.
type MetricFindResponse struct {
	Values []MetricFindValue `json:"values"`
}

// Options are the non-per-query settings the dispatcher needs — mostly
// plugin-level preferences (like device label mode) that don't belong on the
// MerakiQuery wire shape but still influence how frames are rendered.
type Options struct {
	// LabelMode selects how per-device series are labeled across every
	// device-family timeseries handler (sensor, wireless, appliance, camera).
	// Values match pkg/plugin.LabelMode; unknown values fall back to "serial".
	LabelMode string
	// PluginPathPrefix is the full `/a/<plugin-id>` prefix used when
	// handlers compute cross-family device-detail drilldown URLs. Populated
	// from plugin settings at request time so the Go code doesn't hard-code
	// the plugin ID (the `robknight-*` rename is a real future possibility).
	PluginPathPrefix string
}

// handlerFn is the common signature every per-kind handler implements. Handlers return one or
// more frames so that long-format data (e.g. sensor history) can be split into per-series
// frames with labels — Grafana's timeseries panel infers series from labeled value fields, so
// a single long-format frame renders as a flat table instead of a chart.
type handlerFn func(ctx context.Context, client *meraki.Client, q MerakiQuery, tr TimeRange, opts Options) ([]*data.Frame, error)

// handlers maps a QueryKind to its implementation. Kept in one place so the
// dispatcher logic stays tiny.
var handlers = map[QueryKind]handlerFn{
	KindOrganizations:            handleOrganizations,
	KindOrganizationsCount:       handleOrganizationsCount,
	KindNetworks:                 handleNetworks,
	KindNetworksCount:            handleNetworksCount,
	KindDevices:                  handleDevices,
	KindDeviceStatusOverview:     handleDeviceStatusOverview,
	KindDeviceAvailabilities:     handleDeviceAvailabilities,
	KindDeviceAvailabilityCounts: handleDeviceAvailabilityCounts,
	KindOrgProductTypes:          handleOrgProductTypes,
	KindSensorReadingsLatest:  handleSensorReadingsLatest,
	KindSensorReadingsHistory: handleSensorReadingsHistory,
	KindSensorAlertSummary:    handleSensorAlertSummary,
	KindSensorFloorPlan:       handleSensorFloorPlan,

	KindWirelessChannelUtil: handleWirelessChannelUtil,
	KindWirelessUsage:       handleWirelessUsage,
	KindNetworkSsids:        handleNetworkSsids,
	KindApClients:           handleApClients,

	KindAlerts:         handleAlerts,
	KindAlertsOverview: handleAlertsOverview,

	KindSwitchPorts:              handleSwitchPorts,
	KindSwitchPortConfig:         handleSwitchPortConfig,
	KindSwitchPortPacketCounters: handleSwitchPortPacketCounters,
	KindSwitchPortsOverview:      handleSwitchPortsOverview,

	KindApplianceUplinkStatuses:  handleApplianceUplinkStatuses,
	KindApplianceUplinksOverview: handleApplianceUplinksOverview,
	KindApplianceVpnStatuses:     handleApplianceVpnStatuses,
	KindApplianceVpnStats:        handleApplianceVpnStats,
	KindDeviceUplinksLossLatency: handleDeviceUplinksLossLatency,
	KindAppliancePortForwarding:  handleAppliancePortForwarding,
	KindApplianceSettings:        handleApplianceSettings,

	KindDeviceUplinksLossLatencyHistory: handleDeviceUplinksLossLatencyHistory,
	KindApplianceUplinksUsageHistory:    handleApplianceUplinksUsageHistory,
	KindApplianceUplinksUsageByNetwork:  handleApplianceUplinksUsageByNetwork,

	KindLicensesOverview:      handleLicensesOverview,
	KindLicensesList:          handleLicensesList,
	KindApiRequestsOverview:   handleApiRequestsOverview,
	KindApiRequestsByInterval: handleApiRequestsByInterval,
	KindClientsOverview:       handleClientsOverview,
	KindTopClients:            handleTopClients,
	KindTopDevices:            handleTopDevices,
	KindTopDeviceModels:       handleTopDeviceModels,
	KindTopSsids:              handleTopSsids,
	KindTopSwitchesByEnergy:   handleTopSwitchesByEnergy,
	KindTopNetworksByStatus:   handleTopNetworksByStatus,

	KindCameraOnboarding:        handleCameraOnboarding,
	KindCameraBoundaryAreas:     handleCameraBoundaryAreas,
	KindCameraBoundaryLines:     handleCameraBoundaryLines,
	KindCameraDetectionsHistory: handleCameraDetectionsHistory,
	KindCameraRetentionProfiles: handleCameraRetentionProfiles,

	KindMgUplinks:        handleMgUplinks,
	KindMgPortForwarding: handleMgPortForwarding,
	KindMgLan:            handleMgLan,
	KindMgConnectivity:   handleMgConnectivity,

	KindNetworkEvents:         handleNetworkEvents,
	KindNetworkEventsTimeline: handleNetworkEventsTimeline,

	KindConfigurationChanges:      handleConfigurationChanges,
	KindDeviceAvailabilityChanges: handleDeviceAvailabilitiesChangeHistory,

	KindWirelessApClientCounts:          handleWirelessApClientCounts,
	KindWirelessPacketLossByNetwork:     handleWirelessPacketLossByNetwork,
	KindWirelessDevicesEthernetStatuses: handleWirelessDevicesEthernetStatuses,
	KindWirelessDevicesCpuLoadHistory:   handleWirelessDevicesCpuLoadHistory,

	// §3.1 — Switch ports overview by speed + usage history.
	KindSwitchPortsOverviewBySpeed: handleSwitchPortsOverviewBySpeed,
	KindSwitchPortsUsageHistory:    handleSwitchPortsUsageHistory,

	// §3.3 — Device memory usage history.
	KindDeviceMemoryHistory: handleDeviceMemoryHistory,

	// §3.4 — Alerts overview byNetwork + historical.
	KindAlertsOverviewByNetwork:  handleAlertsOverviewByNetwork,
	KindAlertsOverviewHistorical: handleAlertsOverviewHistorical,

	// §4.4.2 — v0.5 Phase 0 plumbing.
	KindConfigurationChangesAnnotation: handleConfigurationChangesAnnotation,
	KindAlertsMttrSummary:              handleAlertsMttrSummary,

	// §4.4.3-1c — MX panels.
	KindApplianceTrafficShaping: handleApplianceTrafficShaping,
	KindApplianceFailoverEvents: handleApplianceFailoverEvents,
	KindApplianceVpnHeatmap:     handleApplianceVpnHeatmap,

	// §4.4.3-1a — MR panels.
	KindWirelessClientCountHistory: handleWirelessClientCountHistory,
	KindWirelessFailedConnections:  handleWirelessFailedConnections,
	KindWirelessLatencyStats:       handleWirelessLatencyStats,
	KindDeviceRadioStatus:          handleDeviceRadioStatus,

	// §4.4.3-1b — MS (switches) panels.
	KindSwitchPoe:          handleSwitchPoe,
	KindSwitchStp:          handleSwitchStp,
	KindSwitchMacTable:     handleSwitchMacTable,
	KindSwitchVlansSummary: handleSwitchVlansSummary,

	// §4.4.3-1f — Home "what just changed" tile.
	KindOrgChangeFeed: handleOrgChangeFeed,

	// §4.4.4-A — Clients page (top talkers / new clients / search / sessions).
	KindClientsList:    handleClientsList,
	KindClientLookup:   handleClientLookup,
	KindClientSessions: handleClientSessions,

	// §4.4.4-B — Firmware & Lifecycle page.
	KindFirmwareUpgrades: handleFirmwareUpgrades,
	KindFirmwarePending:  handleFirmwarePending,
	KindDeviceEol:        handleDeviceEol,

	// §4.4.4-C — Traffic Analytics page.
	KindNetworkTraffic:                  handleNetworkTraffic,
	KindTopApplicationsByUsage:          handleTopApplicationsByUsage,
	KindTopApplicationCategoriesByUsage: handleTopApplicationCategoriesByUsage,
	KindNetworkTrafficAnalysisMode:      handleNetworkTrafficAnalysisMode,

	// §4.4.4-D — Topology page.
	KindNetworkGeo:    handleNetworkGeo,
	KindDeviceLldpCdp: handleDeviceLldpCdp,

	// §4.4.4-E — Org Health Overview (single wide KPI frame).
	KindOrgHealthSummary: handleOrgHealthSummary,

	// §4.4.5 — availability-by-family reshape for the Home stacked bar.
	KindDeviceStatusByFamily: handleDeviceStatusByFamily,

	// Single-field offline count consumed by the device-offline alert template.
	KindDeviceOfflineCount: handleDeviceOfflineCount,
}

// Handle dispatches each MerakiQuery in req.Queries to its handler and
// aggregates the serialized frames. A per-query failure is captured as a
// notice on a synthetic error frame (named "<refId>_error") so one bad query
// does not blank the whole panel. Returns an error only when the request
// envelope itself is malformed (nil req, unknown kind, etc.).
func Handle(ctx context.Context, client *meraki.Client, req *QueryRequest, opts Options) (*QueryResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("query: nil request")
	}
	if client == nil {
		return nil, fmt.Errorf("query: meraki client not configured")
	}
	resp := &QueryResponse{Frames: make([]json.RawMessage, 0, len(req.Queries))}
	// Propagate the panel's MaxDataPoints into the per-query TimeRange so handlers
	// that quantize resolution (e.g. sensor history) can honour the panel width.
	tr := TimeRange{
		From:          req.Range.From,
		To:            req.Range.To,
		MaxDataPoints: req.MaxDataPoints,
	}
	for _, q := range req.Queries {
		if q.Hide {
			continue
		}
		frames, err := runOne(ctx, client, q, tr, opts)
		if len(frames) == 0 {
			// Handler returned (nil/empty, err) — manufacture an error frame so
			// the panel still gets a visible notice rather than a blank
			// response. When err is also nil we still emit an empty-but-named
			// frame so consumers can key by RefID.
			frames = []*data.Frame{data.NewFrame(errorFrameName(q.RefID))}
		}
		if err != nil {
			// Attach the error as a notice on the first frame only — repeating
			// it on every frame would clutter the UI. First frame wins because
			// Grafana surfaces notices from the primary frame by default.
			frames[0].AppendNotices(data.Notice{
				Severity: data.NoticeSeverityError,
				Text:     err.Error(),
			})
		}
		for _, frame := range frames {
			frame.RefID = q.RefID
			raw, marshalErr := data.FrameToJSON(frame, data.IncludeAll)
			if marshalErr != nil {
				// Extremely unlikely — fall back to a stub error frame so the
				// response is still structurally valid.
				stub := data.NewFrame(errorFrameName(q.RefID))
				stub.RefID = q.RefID
				stub.AppendNotices(data.Notice{
					Severity: data.NoticeSeverityError,
					Text:     fmt.Sprintf("failed to serialize frame: %v", marshalErr),
				})
				raw, _ = data.FrameToJSON(stub, data.IncludeAll)
			}
			resp.Frames = append(resp.Frames, raw)
		}
	}
	return resp, nil
}

// runOne looks up the handler for q.Kind and invokes it. Unknown kinds become
// errors so the caller can turn them into frame notices.
func runOne(ctx context.Context, client *meraki.Client, q MerakiQuery, tr TimeRange, opts Options) ([]*data.Frame, error) {
	h, ok := handlers[q.Kind]
	if !ok {
		return nil, fmt.Errorf("unknown query kind %q", q.Kind)
	}
	return h(ctx, client, q, tr, opts)
}

// HandleMetricFind runs a single variable-hydration query. Unlike Handle,
// failures bubble up as plain errors because variable queries have no frame
// concept to attach notices to.
func HandleMetricFind(ctx context.Context, client *meraki.Client, req *MetricFindRequest) (*MetricFindResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("metricFind: nil request")
	}
	if client == nil {
		return nil, fmt.Errorf("metricFind: meraki client not configured")
	}
	return runMetricFind(ctx, client, req.Query)
}

func errorFrameName(refID string) string {
	if refID == "" {
		return "error"
	}
	return refID + "_error"
}

// toRFCTime converts a unix-ms epoch to a UTC time.Time. Zero input returns
// the zero time so callers can detect "not set".
func toRFCTime(ms int64) time.Time {
	if ms == 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}
