import type { SensorMetric } from '../types';

/**
 * Display metadata for a single Meraki sensor metric. Mirrors the `metricLabel`
 * map in `pkg/plugin/query/sensor_readings.go`; if you add a new metric
 * backend-side, extend this table too so overview cards and detail panels
 * pick up the right unit + legend label.
 *
 * `unit` follows Grafana's unit id set (`celsius`, `percent`, `ppm`, `decdb`,
 * `conppm`, etc.). Leave undefined when the unit is unitless (IAQ score, door
 * 0/1). `min`/`max` are defaults for bounded metrics — the panel builder
 * still honours per-panel overrides.
 */
export interface SensorMetricMeta {
  id: SensorMetric;
  label: string;
  unit?: string;
  min?: number;
  max?: number;
  /** When true, the metric is a discrete 0/1 state (door, water). Panels render it as state-over-time rather than a numeric line. */
  discrete?: boolean;
}

/**
 * Ordered list of sensor metrics the plugin surfaces in the UI. Order here
 * dictates the order of the overview grid and the detail page's panel stack
 * — most useful / most common metrics first.
 */
export const ALL_SENSOR_METRICS: SensorMetricMeta[] = [
  { id: 'temperature', label: 'Temperature', unit: 'celsius' },
  { id: 'humidity', label: 'Humidity', unit: 'humidity', min: 0, max: 100 },
  { id: 'co2', label: 'CO₂', unit: 'ppm' },
  { id: 'pm25', label: 'PM2.5', unit: 'conppm' }, // µg/m³ — Grafana ships a "conppm" unit that renders ppm; PM2.5 is commonly graphed on the same axis so this is fine.
  { id: 'tvoc', label: 'TVOC', unit: 'ppb' },
  { id: 'noise', label: 'Noise', unit: 'decdb' },
  { id: 'indoorAirQuality', label: 'IAQ score', min: 0, max: 500 },
  { id: 'battery', label: 'Battery', unit: 'percent', min: 0, max: 100 },
  { id: 'door', label: 'Door', discrete: true },
  { id: 'water', label: 'Water', discrete: true },
];

/** Quick-lookup map by metric id. Kept in sync with ALL_SENSOR_METRICS. */
export const SENSOR_METRIC_BY_ID: Record<SensorMetric, SensorMetricMeta> =
  ALL_SENSOR_METRICS.reduce<Record<SensorMetric, SensorMetricMeta>>(
    (acc, m) => ({ ...acc, [m.id]: m }),
    {} as Record<SensorMetric, SensorMetricMeta>
  );
