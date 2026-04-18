import pluginJson from './plugin.json';

export const PLUGIN_ID = pluginJson.id;
export const PLUGIN_BASE_URL = `/a/${pluginJson.id}`;

export enum ROUTES {
  Home = 'home',
  Organizations = 'organizations',
  Appliances = 'appliances',
  AccessPoints = 'access-points',
  Switches = 'switches',
  Cameras = 'cameras',
  CellularGateways = 'cellular-gateways',
  Sensors = 'sensors',
  Insights = 'insights',
  Events = 'events',
  Alerts = 'alerts',
  Topology = 'topology',
  AuditLog = 'audit-log',
  Configuration = 'configuration',
}

export const DEFAULT_MERAKI_BASE_URL = 'https://api.meraki.com/api/v1';

/**
 * Known Meraki Dashboard API regional endpoints. The list is exposed in the
 * app config form as a Region picker so operators don't have to remember the
 * per-region base URLs. Picking `Custom…` leaves the Base URL input editable
 * for air-gapped or sandbox deployments that don't match any public region.
 */
export const MERAKI_REGIONS: Array<{ label: string; url: string }> = [
  { label: 'Global / US', url: 'https://api.meraki.com/api/v1' },
  { label: 'Canada', url: 'https://api.meraki.ca/api/v1' },
  { label: 'China', url: 'https://api.meraki.cn/api/v1' },
  { label: 'India', url: 'https://api.meraki.in/api/v1' },
  { label: 'US Federal', url: 'https://api.gov-meraki.com/api/v1' },
  { label: 'Custom…', url: '' },
];
