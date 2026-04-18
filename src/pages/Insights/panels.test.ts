import { SceneQueryRunner, VizPanel } from '@grafana/scenes';
import {
  apiRequestsKpiRow,
  clientsOverviewKpiRow,
  licensingKpiRow,
  topTable,
} from './panels';
import { QueryKind } from '../../datasource/types';

// Query runners expose their queries through `state.queries`; the shape is
// loosely typed so we cast to this for assertions.
type AnyQuery = { refId: string; kind?: QueryKind };

describe('licensingKpiRow', () => {
  it('returns four stat panels with the expected titles', () => {
    const panels = licensingKpiRow();

    expect(panels).toHaveLength(4);
    const titles = panels.map((p) => p.state.title);
    expect(titles).toEqual([
      'Active licenses',
      'Expiring (≤30d)',
      'Expired',
      'Total licenses',
    ]);
    for (const p of panels) {
      expect(p).toBeInstanceOf(VizPanel);
      expect(p.state.pluginId).toBe('stat');
    }
  });
});

describe('apiRequestsKpiRow', () => {
  it('returns five stat panels backed by the ApiRequestsOverview kind', () => {
    const panels = apiRequestsKpiRow();

    expect(panels).toHaveLength(5);
    const titles = panels.map((p) => p.state.title);
    expect(titles).toEqual([
      'Total requests',
      '2xx success',
      '4xx errors',
      '429 rate limited',
      '5xx server errors',
    ]);
    for (const p of panels) {
      expect(p.state.pluginId).toBe('stat');
    }
  });
});

describe('clientsOverviewKpiRow', () => {
  it('returns four stat panels backed by the ClientsOverview kind', () => {
    const panels = clientsOverviewKpiRow();

    expect(panels).toHaveLength(4);
    const titles = panels.map((p) => p.state.title);
    expect(titles).toEqual(['Total clients', 'Total usage', 'Downstream', 'Upstream']);
  });
});

describe('topTable', () => {
  it('builds a table panel backed by the supplied query kind', () => {
    const panel = topTable({
      title: 'Top clients',
      kind: QueryKind.TopClients,
    });

    expect(panel).toBeInstanceOf(VizPanel);
    expect(panel.state.pluginId).toBe('table');
    expect(panel.state.title).toBe('Top clients');

    // Walk through the SceneDataTransformer (or raw runner) to find the
    // underlying SceneQueryRunner and assert the query kind.
    const data = panel.state.$data;
    let runner: SceneQueryRunner | undefined;
    if (data instanceof SceneQueryRunner) {
      runner = data;
    } else if (data && 'state' in data && (data as any).state?.$data instanceof SceneQueryRunner) {
      runner = (data as any).state.$data as SceneQueryRunner;
    }
    expect(runner).toBeInstanceOf(SceneQueryRunner);

    const queries = (runner!.state.queries as AnyQuery[]);
    expect(queries).toHaveLength(1);
    expect(queries[0].kind).toBe(QueryKind.TopClients);
  });
});
