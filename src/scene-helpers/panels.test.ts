import { SceneQueryRunner, VizPanel } from '@grafana/scenes';
import { makeStatPanel, sensorMetricCard } from './panels';
import { SENSOR_METRIC_BY_ID } from './sensorMetrics';
import { QueryKind } from '../datasource/types';

// The SceneQueryRunner builders don't expose queries publicly beyond
// `state.queries`; cast to the loosely-typed shape we assert on.
type AnyQuery = { refId: string; kind?: QueryKind; orgId?: string };

describe('makeStatPanel', () => {
  it('builds a stat panel with a query runner and the right title', () => {
    const panel = makeStatPanel('Organizations', QueryKind.Organizations);

    expect(panel).toBeInstanceOf(VizPanel);
    expect(panel.state.title).toBe('Organizations');

    const runner = panel.state.$data;
    expect(runner).toBeInstanceOf(SceneQueryRunner);
  });

  it('omits orgId for the Organizations kind (server walks the whole tenant)', () => {
    const panel = makeStatPanel('Organizations', QueryKind.Organizations);
    const runner = panel.state.$data as SceneQueryRunner;
    const queries = runner.state.queries as AnyQuery[];

    expect(queries).toHaveLength(1);
    expect(queries[0].refId).toBe('A');
    expect(queries[0].kind).toBe(QueryKind.Organizations);
    // Org-scoped kinds must not receive an orgId from the helper — this
    // is the branch we assert is hit in panels.ts' oneQuery() factory.
    expect('orgId' in queries[0]).toBe(false);
  });

  it('threads the orgId through for non-Organizations kinds', () => {
    const panel = makeStatPanel('Count', QueryKind.Networks, 'o1');
    const runner = panel.state.$data as SceneQueryRunner;
    const queries = runner.state.queries as AnyQuery[];

    expect(queries[0].kind).toBe(QueryKind.Networks);
    expect(queries[0].orgId).toBe('o1');
  });
});

describe('sensorMetricCard', () => {
  it('uses the timeseries viz for continuous metrics', () => {
    const panel = sensorMetricCard(SENSOR_METRIC_BY_ID.temperature);

    expect(panel).toBeInstanceOf(VizPanel);
    expect(panel.state.pluginId).toBe('timeseries');
  });

  it('uses the state-timeline viz for discrete metrics', () => {
    const panel = sensorMetricCard(SENSOR_METRIC_BY_ID.door);

    expect(panel).toBeInstanceOf(VizPanel);
    expect(panel.state.pluginId).toBe('state-timeline');
  });
});
