/**
 * Sensor label mode — controls how individual sensor series are labeled on
 * timeseries panels.
 *  - `serial` (default): raw Meraki device serial, e.g. `Q3CC-HV6P-H5XK`.
 *  - `name`: the human-friendly device name from `/organizations/{id}/devices`
 *    (falls back to the serial when the device has no name set).
 */
export type SensorLabelMode = 'serial' | 'name';

/**
 * App-level non-secret configuration stored in Grafana plugin settings.
 */
export interface AppJsonData {
  baseUrl?: string;
  sharedFraction?: number;
  isApiKeySet?: boolean;
  labelMode?: SensorLabelMode;
}

/**
 * App-level secret configuration. Stored encrypted by Grafana; only ever read server-side.
 */
export interface AppSecureJsonData {
  merakiApiKey?: string;
}

export type MerakiProductType =
  | 'wireless'
  | 'switch'
  | 'appliance'
  | 'sensor'
  | 'camera'
  | 'cellularGateway';

export type SensorMetric =
  | 'temperature'
  | 'humidity'
  | 'door'
  | 'water'
  | 'co2'
  | 'pm25'
  | 'tvoc'
  | 'noise'
  | 'battery'
  | 'indoorAirQuality';
