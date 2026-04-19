import { DataSourceJsonData } from '@grafana/data';
import { DataQuery } from '@grafana/schema';
import { MerakiProductType, SensorMetric } from '../types';

/**
 * QueryKind is the discriminator for the Meraki backend query dispatcher.
 * Keep this list in sync with pkg/plugin/query/dispatch.go.
 */
export enum QueryKind {
  Organizations = 'organizations',
  OrganizationsCount = 'organizationsCount',
  Networks = 'networks',
  NetworksCount = 'networksCount',
  Devices = 'devices',
  DeviceStatusOverview = 'deviceStatusOverview',
  DeviceAvailabilities = 'deviceAvailabilities',
  DeviceAvailabilityCounts = 'deviceAvailabilityCounts',
  OrgProductTypes = 'orgProductTypes',
  SensorReadingsLatest = 'sensorReadingsLatest',
  SensorReadingsHistory = 'sensorReadingsHistory',
  SensorAlertSummary = 'sensorAlertSummary',
  /* §4.4.3-1e — floor-plan layout + latest readings per MT sensor.
   * Wide frame; lat/lng are nullable so the panel falls back to a grid
   * layout when a floor plan has no anchor coordinates configured. */
  SensorFloorPlan = 'sensorFloorPlan',

  /* Wireless (MR) — phase 5. */
  WirelessChannelUtil = 'wirelessChannelUtil',
  WirelessUsage = 'wirelessUsage',
  NetworkSsids = 'networkSsids',
  ApClients = 'apClients',

  /* Alerts (assurance) — phase 6. */
  Alerts = 'alerts',
  AlertsOverview = 'alertsOverview',

  /* Switch (MS) — phase 7. */
  SwitchPorts = 'switchPorts',
  SwitchPortConfig = 'switchPortConfig',
  SwitchPortPacketCounters = 'switchPortPacketCounters',
  SwitchPortsOverview = 'switchPortsOverview',

  /* Appliance (MX) — phase 8. */
  ApplianceUplinkStatuses = 'applianceUplinkStatuses',
  ApplianceUplinksOverview = 'applianceUplinksOverview',
  ApplianceVpnStatuses = 'applianceVpnStatuses',
  ApplianceVpnStats = 'applianceVpnStats',
  DeviceUplinksLossLatency = 'deviceUplinksLossLatency',
  AppliancePortForwarding = 'appliancePortForwarding',
  ApplianceSettings = 'applianceSettings',

  /* Insights (licensing / API usage / clients) — phase 9. */
  LicensesOverview = 'licensesOverview',
  LicensesList = 'licensesList',
  ApiRequestsOverview = 'apiRequestsOverview',
  ApiRequestsByInterval = 'apiRequestsByInterval',
  ClientsOverview = 'clientsOverview',
  TopClients = 'topClients',
  TopDevices = 'topDevices',
  TopDeviceModels = 'topDeviceModels',
  TopSsids = 'topSsids',
  TopSwitchesByEnergy = 'topSwitchesByEnergy',
  TopNetworksByStatus = 'topNetworksByStatus',

  /* Camera (MV) — phase 10. The legacy `analytics/*` endpoints were
   * deprecated by Meraki in March 2024; the boundaries model replaces them. */
  CameraOnboarding = 'cameraOnboarding',
  CameraBoundaryAreas = 'cameraBoundaryAreas',
  CameraBoundaryLines = 'cameraBoundaryLines',
  CameraDetectionsHistory = 'cameraDetectionsHistory',
  CameraRetentionProfiles = 'cameraRetentionProfiles',

  /* Cellular Gateway (MG) — phase 10. */
  MgUplinks = 'mgUplinks',
  MgPortForwarding = 'mgPortForwarding',
  MgLan = 'mgLan',
  MgConnectivity = 'mgConnectivity',

  /* Network events — phase 11. */
  NetworkEvents = 'networkEvents',
  NetworkEventsTimeline = 'networkEventsTimeline',

  /* API optimisation — §7.3 (phase 12). */
  ConfigurationChanges = 'configurationChanges',
  DeviceAvailabilityChanges = 'deviceAvailabilityChanges',

  /* §2.2 — per-device uplink loss/latency history (31-day window). */
  DeviceUplinksLossLatencyHistory = 'deviceUplinksLossLatencyHistory',

  /* §3.5 — MX uplinks usage history + org-wide usage by network. */
  ApplianceUplinksUsageHistory = 'applianceUplinksUsageHistory',
  ApplianceUplinksUsageByNetwork = 'applianceUplinksUsageByNetwork',

  /* §2.1 — org-level AP client counts (replaces N per-AP fan-out on overview). */
  WirelessApClientCounts = 'wirelessApClientCounts',

  /* §3.2 — additional wireless kinds. */
  WirelessPacketLossByNetwork = 'wirelessPacketLossByNetwork',
  WirelessDevicesEthernetStatuses = 'wirelessDevicesEthernetStatuses',
  WirelessDevicesCpuLoadHistory = 'wirelessDevicesCpuLoadHistory',

  /* §3.1 — Switch ports overview by speed + usage history. */
  SwitchPortsOverviewBySpeed = 'switchPortsOverviewBySpeed',
  SwitchPortsUsageHistory = 'switchPortsUsageHistory',

  /* §3.3 — Device memory usage history. */
  DeviceMemoryHistory = 'deviceMemoryHistory',

  /* §3.4 — Alerts overview byNetwork + historical. */
  AlertsOverviewByNetwork = 'alertsOverviewByNetwork',
  AlertsOverviewHistorical = 'alertsOverviewHistorical',

  /* §4.4.3-1c — MX panels: traffic shaping snapshot + uplink-failover event
   * timeline + VPN heatmap reshape that REPLACES the legacy peer-matrix panel
   * on the Appliances / VPN tab. applianceVpnHeatmap is a new kind (not a
   * reshape of applianceVpnStatuses) because the existing kind is still used
   * by the flattened peer-status table. */
  ApplianceTrafficShaping = 'applianceTrafficShaping',
  ApplianceFailoverEvents = 'applianceFailoverEvents',
  ApplianceVpnHeatmap = 'applianceVpnHeatmap',

  /* §4.4.2 — v0.5 Phase 0 plumbing.
   * ConfigurationChangesAnnotation reshapes the existing configurationChanges
   * feed into a Grafana annotation frame (time/title/text/tags) for data-layer
   * overlay. AlertsMttrSummary aggregates resolvedAt-startedAt across alerts
   * into mean/p50/p95 + counts — shared between the MTTR chart (§4.4.3 1f)
   * and the new Org Health page (§4.4.4). */
  ConfigurationChangesAnnotation = 'configurationChangesAnnotation',
  AlertsMttrSummary = 'alertsMttrSummary',

  /* §4.4.3-1a — MR panels: per-network client-count timeseries, failed-
   * connection wide table, per-network latency timeseries, and org-wide
   * radio/band-status snapshot. */
  WirelessClientCountHistory = 'wirelessClientCountHistory',
  WirelessFailedConnections = 'wirelessFailedConnections',
  WirelessLatencyStats = 'wirelessLatencyStats',
  DeviceRadioStatus = 'deviceRadioStatus',

  /* §4.4.3-1b — MS (switches) panels: PoE draw, STP topology, MAC table,
   * VLAN distribution. Port-error timeline reshapes the existing
   * switchPortPacketCounters kind (no new kind). */
  SwitchPoe = 'switchPoe',
  SwitchStp = 'switchStp',
  SwitchMacTable = 'switchMacTable',
  SwitchVlansSummary = 'switchVlansSummary',

  /* §4.4.3-1f — cross-cutting. Server-side union of configurationChanges +
   * networkEvents for the Home "what just changed in 24h" tile. Always a
   * fixed 24h lookback regardless of dashboard time range. */
  OrgChangeFeed = 'orgChangeFeed',

  /* §4.4.4-A — Clients page kinds. ClientsOverview already exists (phase 9
   * — `clientsOverview` enum value above). The three new kinds:
   *  - ClientsList:    fan-out /networks/{id}/clients across selected networks.
   *  - ClientLookup:   /organizations/{id}/clients/search?mac=...
   *                    (zero-row + Info notice when MAC not found).
   *  - ClientSessions: per-client wireless latency history; one frame per
   *                    traffic category with labels on the value field. */
  ClientsList = 'clientsList',
  ClientLookup = 'clientLookup',
  ClientSessions = 'clientSessions',

  /* §4.4.4-B — Firmware & Lifecycle page.
   *  - FirmwareUpgrades: org-wide past + scheduled upgrade events table.
   *  - FirmwarePending:  per-device pending/in-progress upgrades
   *                      (currentUpgradesOnly=true; MS+MR only per Meraki's
   *                      documented limitation as of 2026-04).
   *  - DeviceEol:        inventory devices with EOX status sourced from
   *                      /inventory/devices?eoxStatuses[]=. Defaults to all
   *                      three buckets (endOfSale, endOfSupport,
   *                      nearEndOfSupport) when q.metrics is empty. */
  FirmwareUpgrades = 'firmwareUpgrades',
  FirmwarePending = 'firmwarePending',
  DeviceEol = 'deviceEol',

  /* §4.4.4-C — Traffic Analytics page (L7 application breakdown).
   *  - NetworkTraffic:                 per-network L7 row table from
   *                                    /networks/{id}/traffic.
   *  - TopApplicationsByUsage:         org-wide top-N applications.
   *  - TopApplicationCategoriesByUsage: org-wide top-N categories.
   *  - NetworkTrafficAnalysisMode:     per-network analysis mode lookup,
   *                                    feeding the TrafficGuard banner. */
  NetworkTraffic = 'networkTraffic',
  TopApplicationsByUsage = 'topApplicationsByUsage',
  TopApplicationCategoriesByUsage = 'topApplicationCategoriesByUsage',
  NetworkTrafficAnalysisMode = 'networkTrafficAnalysisMode',

  /* §4.4.4-D — Topology page. networkGeo aggregates per-network centroid
   * coordinates (derived from device lat/lng because Meraki's networks
   * endpoint does not carry coordinates). deviceLldpCdp emits the two-
   * frame Grafana Node Graph contract (nodes + edges) for the per-network
   * link graph; org-wide fan-out is intentionally disabled. */
  NetworkGeo = 'networkGeo',
  DeviceLldpCdp = 'deviceLldpCdp',

  /* §4.4.4-E — Org Health Overview. Single-row wide KPI frame fanned out in
   * parallel across 6 existing handlers (deviceStatusOverview,
   * alertsOverview, licensesList, firmwarePending, apiRequestsByInterval,
   * applianceUplinkStatuses). 30s TTL on the underlying calls +
   * singleflight makes re-entry from the §4.4.5 Home merge effectively
   * free. No dedicated page ships in this phase. */
  OrgHealthSummary = 'orgHealthSummary',

  /* §4.4.5 — "availability by family" roll-up. One row per productType with
   * online/alerting/offline/dormant/total counts. Feeds the Home stacked-bar
   * panel that replaces the org-wide donut as a family-level breakdown. */
  DeviceStatusByFamily = 'deviceStatusByFamily',

  /* Single-field offline count used by the device-offline alert template.
   * deviceAvailabilityCounts emits five fields and Grafana's reduce SSE
   * produces one labelled output per numeric field, so a `gt 0` threshold
   * against it would always fire (online > 0 in healthy fleets). This kind
   * narrows to one int64 `count` field so the standard reduce → threshold
   * chain works. */
  DeviceOfflineCount = 'deviceOfflineCount',
}

export interface MerakiQuery extends DataQuery {
  kind: QueryKind;
  orgId?: string;
  networkIds?: string[];
  serials?: string[];
  productTypes?: MerakiProductType[];
  /** Sensor metrics selector. Only meaningful for SensorReadings* kinds. */
  metrics?: SensorMetric[];
  /** Optional override in seconds; defaults to the panel time range. */
  timespanSeconds?: number;
  /**
   * Alerts-only lifecycle filter. One of "active" (currently firing, the
   * default when omitted), "resolved", "dismissed", or "all" (show every
   * status clearly marked in the status column). Separate from `metrics`
   * so the CSV-split template interpolation on $severity can't clobber
   * it — the old positional encoding `metrics: ['$severity', status]`
   * broke when $severity resolved to empty and the `all` value shifted
   * into the severity slot, triggering Meraki HTTP 500.
   */
  alertStatus?: 'active' | 'resolved' | 'dismissed' | 'all';
}

export interface MerakiDSOptions extends DataSourceJsonData {
  // Intentionally empty — the DS reads the API key and base URL from the app plugin instance
  // via resource calls on the app. No DS-level config needed.
  _placeholder?: never;
}

export interface MerakiSecureDSOptions {
  _placeholder?: never;
}

export const DEFAULT_MERAKI_QUERY: Partial<MerakiQuery> = {
  kind: QueryKind.Organizations,
};

/** Result shape of the frontend-facing /metricFind endpoint. Kept small by design. */
export interface MerakiMetricFindValue {
  text: string;
  value?: string | number;
}
