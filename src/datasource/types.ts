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

  /* §4.4.2 — v0.5 Phase 0 plumbing.
   * ConfigurationChangesAnnotation reshapes the existing configurationChanges
   * feed into a Grafana annotation frame (time/title/text/tags) for data-layer
   * overlay. AlertsMttrSummary aggregates resolvedAt-startedAt across alerts
   * into mean/p50/p95 + counts — shared between the MTTR chart (§4.4.3 1f)
   * and the new Org Health page (§4.4.4). */
  ConfigurationChangesAnnotation = 'configurationChangesAnnotation',
  AlertsMttrSummary = 'alertsMttrSummary',

  /* §4.4.3-1b — MS (switches) panels: PoE draw, STP topology, MAC table,
   * VLAN distribution. Port-error timeline reshapes the existing
   * switchPortPacketCounters kind (no new kind). */
  SwitchPoe = 'switchPoe',
  SwitchStp = 'switchStp',
  SwitchMacTable = 'switchMacTable',
  SwitchVlansSummary = 'switchVlansSummary',
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
