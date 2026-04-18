import { ALL_SENSOR_METRICS, SENSOR_METRIC_BY_ID } from './sensorMetrics';

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
