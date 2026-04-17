import { DataSourceJsonData } from '@grafana/data';
import { DataQuery } from '@grafana/schema';
import { MerakiProductType, SensorMetric } from '../types';

/**
 * QueryKind is the discriminator for the Meraki backend query dispatcher.
 * Keep this list in sync with pkg/plugin/query/dispatch.go.
 */
export enum QueryKind {
  Organizations = 'organizations',
  Networks = 'networks',
  Devices = 'devices',
  DeviceStatusOverview = 'deviceStatusOverview',
  SensorReadingsLatest = 'sensorReadingsLatest',
  SensorReadingsHistory = 'sensorReadingsHistory',
  SensorAlertSummary = 'sensorAlertSummary',
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
