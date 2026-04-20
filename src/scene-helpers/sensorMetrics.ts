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
  /** When true, the metric is a discrete 0/1 state (door, water, MT40 power switches). Panels render it as state-over-time rather than a numeric line. */
  discrete?: boolean;
  /**
   * Value-to-label mapping for discrete metrics. 0 = `off`, 1 = `on`. State
   * timeline panels use this to colour and label the bars without having to
   * hard-code per-metric branches.
   */
  discreteLabels?: {
    off: { text: string; color: string };
    on: { text: string; color: string };
  };
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
  {
    id: 'door',
    label: 'Door',
    discrete: true,
    discreteLabels: {
      off: { text: 'Closed', color: 'green' },
      on: { text: 'Open', color: 'red' },
    },
  },
  {
    id: 'water',
    label: 'Water',
    discrete: true,
    discreteLabels: {
      off: { text: 'Dry', color: 'green' },
      on: { text: 'Water detected', color: 'red' },
    },
  },
  // MT40 smart power monitor metrics. Meraki's native cadence is ~15s per
  // metric per sensor; treat them the same as environmental timeseries.
  { id: 'realPower', label: 'Real power', unit: 'watt', min: 0 },
  { id: 'apparentPower', label: 'Apparent power', unit: 'voltamp', min: 0 },
  { id: 'voltage', label: 'Voltage', unit: 'volt', min: 0 },
  { id: 'current', label: 'Current', unit: 'amp', min: 0 },
  { id: 'frequency', label: 'Frequency', unit: 'hertz' },
  { id: 'powerFactor', label: 'Power factor', unit: 'percent', min: 0, max: 100 },
  {
    id: 'downstreamPower',
    label: 'Downstream power',
    discrete: true,
    discreteLabels: {
      off: { text: 'Disabled', color: 'red' },
      on: { text: 'Enabled', color: 'green' },
    },
  },
  {
    id: 'remoteLockoutSwitch',
    label: 'Remote lockout',
    discrete: true,
    discreteLabels: {
      off: { text: 'Unlocked', color: 'green' },
      on: { text: 'Locked', color: 'orange' },
    },
  },
];

/**
 * Composite Air-Quality Index weights for the v0.5 §4.4.3-1e AQI tile.
 *
 * The panel renders a single 0-100 score per sensor by combining three
 * independent metrics — CO₂ (ppm), TVOC (ppb / µg/m³) and PM2.5 (µg/m³) —
 * that Meraki MT15 / MT40 sensors report natively. A single composite
 * number is what operators actually want on an overview tile; the per-
 * metric history lives on the detail page.
 *
 * ### Weights
 *
 * ```
 *   CO₂  : 0.30     reflects ventilation / occupancy
 *   TVOC : 0.35     broad chemical-pollutant proxy (paint, cleaners, off-gassing)
 *   PM2.5: 0.35     strongest health-impact correlator per US EPA + WHO AQGs
 * ```
 *
 * Weights sum to 1.0 so the composite stays on the same 0-100 scale as
 * the per-metric sub-scores.
 *
 * ### Per-metric sub-score (piecewise linear, higher = worse)
 *
 * ```
 *   CO₂ (ppm):    <600 → 100       (outdoor-baseline ventilation)
 *                 600-1000 linear  (ASHRAE 62.1 "acceptable" band)
 *                 1000-1500 linear (drowsiness onset, ASHRAE upper)
 *                 >1500 → 0
 *   TVOC (ppb):   <220 → 100       (BAuA "hygienically acceptable")
 *                 220-660 linear   (BAuA level 2)
 *                 660-2200 linear  (BAuA level 3)
 *                 >2200 → 0
 *   PM2.5 (µg/m³):<10 → 100        (WHO 2021 AQG annual)
 *                 10-25 linear     (WHO 24h AQG)
 *                 25-55 linear     (EPA "unhealthy for sensitive groups")
 *                 >55 → 0
 * ```
 *
 * ### Citations
 *
 * The bands chosen are the standards the exporter reference repo (see root
 * CLAUDE.md for the path) also uses; all are drawn from public guidance:
 *
 * - CO₂ thresholds: ASHRAE 62.1-2022 and Harvard T.H. Chan "cognitive
 *   impact" studies (Allen et al., 2016).
 * - TVOC thresholds: German Federal Environment Agency (BAuA / AGÖF)
 *   "Guide values for indoor air volatile organic compounds."
 * - PM2.5 thresholds: WHO Global Air Quality Guidelines 2021 + US EPA
 *   NAAQS 2024 revision (annual 9 µg/m³, 24h 35 µg/m³).
 *
 * ### Why server-side aggregation is NOT used here
 *
 * Unlike the alert KPI row (`sensorAlertSummary`), AQI is computed in the
 * panel layer. The weights above are a product-owner-tunable parameter
 * and would otherwise require a new query kind per tweak. The sub-score
 * functions are small + pure, so client-side evaluation via a standard
 * `reduce` + arithmetic transform is reliable — we are NOT relying on
 * `filterByValue + reduce`, which is the pattern §G.20 calls out as
 * brittle.
 */
export interface AqiWeights {
  co2: number;
  tvoc: number;
  pm25: number;
}

export const AQI_WEIGHTS: AqiWeights = {
  co2: 0.3,
  tvoc: 0.35,
  pm25: 0.35,
};

/**
 * Piecewise linear interpolation: 100 at `good`, 0 at `bad`, linear
 * between, clamped to [0, 100]. Exported for the Jest unit test that
 * asserts the weights sum + sub-score boundaries.
 */
export function aqiSubScore(value: number, good: number, bad: number): number {
  if (!Number.isFinite(value)) {
    return NaN;
  }
  if (value <= good) {
    return 100;
  }
  if (value >= bad) {
    return 0;
  }
  return 100 * (1 - (value - good) / (bad - good));
}

/**
 * Composite AQI score from raw readings. Missing inputs are skipped and
 * the remaining weights re-normalised — a sensor without TVOC still gets
 * a meaningful score from CO₂ + PM2.5 alone.
 */
export function aqiCompositeScore(readings: {
  co2?: number;
  tvoc?: number;
  pm25?: number;
}): number | undefined {
  const parts: Array<{ score: number; weight: number }> = [];
  if (readings.co2 !== undefined) {
    parts.push({ score: aqiSubScore(readings.co2, 600, 1500), weight: AQI_WEIGHTS.co2 });
  }
  if (readings.tvoc !== undefined) {
    parts.push({ score: aqiSubScore(readings.tvoc, 220, 2200), weight: AQI_WEIGHTS.tvoc });
  }
  if (readings.pm25 !== undefined) {
    parts.push({ score: aqiSubScore(readings.pm25, 10, 55), weight: AQI_WEIGHTS.pm25 });
  }
  if (parts.length === 0) {
    return undefined;
  }
  const totalWeight = parts.reduce((a, p) => a + p.weight, 0);
  const weighted = parts.reduce((a, p) => a + p.score * p.weight, 0);
  return weighted / totalWeight;
}

/** Quick-lookup map by metric id. Kept in sync with ALL_SENSOR_METRICS. */
export const SENSOR_METRIC_BY_ID: Record<SensorMetric, SensorMetricMeta> =
  ALL_SENSOR_METRICS.reduce<Record<SensorMetric, SensorMetricMeta>>(
    (acc, m) => ({ ...acc, [m.id]: m }),
    {} as Record<SensorMetric, SensorMetricMeta>
  );
