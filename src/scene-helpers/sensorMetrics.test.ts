import {
  ALL_SENSOR_METRICS,
  AQI_WEIGHTS,
  aqiCompositeScore,
  aqiSubScore,
  SENSOR_METRIC_BY_ID,
} from './sensorMetrics';

describe('sensor metric metadata', () => {
  it('exposes all ten Meraki MT metrics we support', () => {
    expect(ALL_SENSOR_METRICS).toHaveLength(10);
  });

  it('maps temperature to celsius for Grafana unit formatting', () => {
    const temp = SENSOR_METRIC_BY_ID.temperature;

    expect(temp.label).toBe('Temperature');
    expect(temp.unit).toBe('celsius');
  });

  it('flags discrete metrics (door, water) so panels render them as state timelines', () => {
    expect(SENSOR_METRIC_BY_ID.door.discrete).toBe(true);
    expect(SENSOR_METRIC_BY_ID.water.discrete).toBe(true);
    // Continuous metrics must not be marked discrete — panel selection branches on this.
    expect(SENSOR_METRIC_BY_ID.temperature.discrete).toBeFalsy();
  });
});

describe('AQI composite score (v0.5 §4.4.3-1e)', () => {
  it('has weights that sum to exactly 1.0', () => {
    const total = AQI_WEIGHTS.co2 + AQI_WEIGHTS.tvoc + AQI_WEIGHTS.pm25;
    // Floating-point tolerance — the weights are defined to 2 decimal
    // places so any drift would be intentional and should break the test.
    expect(total).toBeCloseTo(1, 5);
  });

  it('returns 100 at or below the "good" threshold and 0 at or above the "bad" threshold', () => {
    expect(aqiSubScore(400, 600, 1500)).toBe(100);
    expect(aqiSubScore(600, 600, 1500)).toBe(100);
    expect(aqiSubScore(1500, 600, 1500)).toBe(0);
    expect(aqiSubScore(2000, 600, 1500)).toBe(0);
  });

  it('interpolates linearly between the two thresholds', () => {
    // Midpoint of [600, 1500] is 1050 — expect score 50.
    expect(aqiSubScore(1050, 600, 1500)).toBeCloseTo(50, 5);
  });

  it('produces a blended composite from all three sub-scores', () => {
    // All at "good" → 100. Weights irrelevant when every component is equal.
    expect(aqiCompositeScore({ co2: 400, tvoc: 100, pm25: 5 })).toBeCloseTo(100, 5);
    // All at "bad" → 0.
    expect(aqiCompositeScore({ co2: 2000, tvoc: 3000, pm25: 100 })).toBeCloseTo(0, 5);
  });

  it('re-normalises when a sensor only reports a subset of the three metrics', () => {
    // CO₂ only at "bad" → composite should be 0 (single component).
    expect(aqiCompositeScore({ co2: 2000 })).toBeCloseTo(0, 5);
    // CO₂ at good, PM2.5 at bad → weighted to (good * 0.3 + bad * 0.35) / (0.3 + 0.35)
    // = (100 * 0.3 + 0 * 0.35) / 0.65 = 46.1538...
    const partial = aqiCompositeScore({ co2: 400, pm25: 100 });
    expect(partial).toBeCloseTo((100 * AQI_WEIGHTS.co2) / (AQI_WEIGHTS.co2 + AQI_WEIGHTS.pm25), 5);
  });

  it('returns undefined when no air-quality readings are present', () => {
    expect(aqiCompositeScore({})).toBeUndefined();
  });
});
