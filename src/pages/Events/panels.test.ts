import { SceneQueryRunner, VizPanel } from '@grafana/scenes';
import { eventsTable, eventsTimelineBarChart } from './panels';
import { QueryKind } from '../../datasource/types';

type AnyQuery = {
  refId: string;
  kind?: QueryKind;
  orgId?: string;
  networkIds?: string[];
  productTypes?: string[];
  metrics?: string[];
};

function firstQuery(panel: VizPanel): AnyQuery {
  const data = panel.state.$data as { state: { $data?: unknown; queries?: unknown } } | undefined;
  if (!data) {
    throw new Error('panel has no $data');
  }
  const inner = (data as any).state.$data ?? data;
  const runner = inner as SceneQueryRunner;
  const queries = runner.state.queries as AnyQuery[];
  return queries[0];
}

describe('Events panels', () => {
  it('eventsTable targets NetworkEvents with the documented variable shape', () => {
    const panel = eventsTable();
    expect(panel.state.pluginId).toBe('table');
    const q = firstQuery(panel);
    expect(q.kind).toBe(QueryKind.NetworkEvents);
    expect(q.networkIds).toEqual(['$network']);
    expect(q.productTypes).toEqual(['$productType']);
    expect(q.metrics).toEqual(['$eventType']);
  });

  it('eventsTimelineBarChart is a bar chart backed by NetworkEvents', () => {
    const panel = eventsTimelineBarChart();
    expect(panel.state.pluginId).toBe('barchart');
    const q = firstQuery(panel);
    expect(q.kind).toBe(QueryKind.NetworkEvents);
    expect(q.productTypes).toEqual(['$productType']);
  });
});
