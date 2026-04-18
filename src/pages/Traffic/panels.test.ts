import { SceneDataTransformer, SceneQueryRunner, VizPanel } from '@grafana/scenes';
import {
  networkTrafficTable,
  topApplicationCategoriesTable,
  topApplicationsBarChart,
  topApplicationsTable,
} from './panels';
import { QueryKind } from '../../datasource/types';

// Query runners expose their queries through `state.queries`; the shape is
// loosely typed so we cast to this for assertions.
type AnyQuery = { refId: string; kind?: QueryKind };

function unwrapRunner(panel: VizPanel): SceneQueryRunner | undefined {
  const data = panel.state.$data;
  if (data instanceof SceneQueryRunner) {
    return data;
  }
  if (data instanceof SceneDataTransformer) {
    const inner = (data as any).state?.$data;
    if (inner instanceof SceneQueryRunner) {
      return inner;
    }
  }
  return undefined;
}

describe('Traffic panels', () => {
  it('topApplicationsTable is backed by topApplicationsByUsage with quantity=25', () => {
    const panel = topApplicationsTable();
    expect(panel).toBeInstanceOf(VizPanel);
    expect(panel.state.pluginId).toBe('table');
    expect(panel.state.title).toBe('Top applications by usage');

    const runner = unwrapRunner(panel);
    expect(runner).toBeInstanceOf(SceneQueryRunner);
    const queries = runner!.state.queries as AnyQuery[];
    expect(queries).toHaveLength(1);
    expect(queries[0].kind).toBe(QueryKind.TopApplicationsByUsage);
    expect((queries[0] as any).metrics).toEqual(['25']);
  });

  it('topApplicationsBarChart is a barchart over the same kind', () => {
    const panel = topApplicationsBarChart();
    expect(panel.state.pluginId).toBe('barchart');
    expect(panel.state.title).toBe('Top applications (volume)');

    const runner = unwrapRunner(panel);
    expect(runner).toBeInstanceOf(SceneQueryRunner);
    const queries = runner!.state.queries as AnyQuery[];
    expect(queries[0].kind).toBe(QueryKind.TopApplicationsByUsage);
  });

  it('topApplicationCategoriesTable is backed by topApplicationCategoriesByUsage', () => {
    const panel = topApplicationCategoriesTable();
    expect(panel.state.pluginId).toBe('table');
    expect(panel.state.title).toBe('Top application categories');

    const runner = unwrapRunner(panel);
    expect(runner).toBeInstanceOf(SceneQueryRunner);
    const queries = runner!.state.queries as AnyQuery[];
    expect(queries[0].kind).toBe(QueryKind.TopApplicationCategoriesByUsage);
  });

  it('networkTrafficTable fans out across $network and applies $deviceType', () => {
    const panel = networkTrafficTable();
    expect(panel.state.pluginId).toBe('table');
    expect(panel.state.title).toBe('Per-network L7 traffic');

    const runner = unwrapRunner(panel);
    expect(runner).toBeInstanceOf(SceneQueryRunner);
    const queries = runner!.state.queries as AnyQuery[];
    expect(queries).toHaveLength(1);
    expect(queries[0].kind).toBe(QueryKind.NetworkTraffic);
    // The networkIds slot is a single-element array carrying the variable
    // template — Scenes interpolates `$network` into the actual id list at
    // query time. Hard-coded networkIds would defeat the multi-select.
    expect((queries[0] as any).networkIds).toEqual(['$network']);
    expect((queries[0] as any).metrics).toEqual(['$deviceType']);
  });
});
