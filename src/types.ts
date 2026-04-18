/**
 * Device label mode — controls how each per-device series is labeled on
 * timeseries panels across every Meraki device family the plugin supports
 * (sensors, access points, switches, appliances, cameras, cellular gateways).
 *  - `serial` (default): raw Meraki device serial, e.g. `Q3CC-HV6P-H5XK`.
 *  - `name`: the human-friendly device name from `/organizations/{id}/devices`
 *    (falls back to the serial when the device has no name set).
 */
export type DeviceLabelMode = 'serial' | 'name';

/**
 * App-level non-secret configuration stored in Grafana plugin settings.
 */
export interface AppJsonData {
  baseUrl?: string;
  sharedFraction?: number;
  isApiKeySet?: boolean;
  labelMode?: DeviceLabelMode;
  /**
   * Opt-in per-source-IP rate limiter. When true, the Go backend adds a
   * secondary 100 rps / 200 burst token bucket keyed on "ip" in front of the
   * per-org bucket. Useful only for multi-tenant deployments where many org
   * keys egress through a single Grafana instance; leave off for the common
   * single-org / single-replica case.
   */
  enableIPLimiter?: boolean;
  /**
   * When true, the app shows every device-family nav page even if the
   * selected org has zero devices of that family. Default (undefined/false)
   * hides Appliances / Access Points / Switches / Cameras / Cellular
   * Gateways / Sensors nav entries with no underlying devices so an org
   * that only runs MR/MS/MT isn't cluttered with empty pages.
   */
  showEmptyFamilies?: boolean;
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
