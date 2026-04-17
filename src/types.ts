/**
 * App-level non-secret configuration stored in Grafana plugin settings.
 */
export interface AppJsonData {
  baseUrl?: string;
  sharedFraction?: number;
  isApiKeySet?: boolean;
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
