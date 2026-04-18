import { SceneQueryRunner } from '@grafana/scenes';
import { QueryKind } from '../../datasource/types';
import { MERAKI_DS_UID } from '../../scene-helpers/datasource';
import { networkGeomapPanel, networkLinkGraphPanel } from './panels';

// v0.5 §4.4.4-D Topology panel-factory smoke tests. Each factory must
// emit a VizPanel whose data-provider is a SceneQueryRunner pointed at
// the Meraki DS and carrying the expected query kind.

function findQueryRunner(panel: any): SceneQueryRunner | null {
  let data = panel.state?.$data;
  while (data) {
    if (data instanceof SceneQueryRunner) {
      return data;
    }
    data = data.state?.$data;
  }
  return null;
}

describe('Topology v0.5 §4.4.4-D panel factories', () => {
  it('networkGeomapPanel wires the networkGeo query kind', () => {
    const panel = networkGeomapPanel();
    expect(panel.state.pluginId).toBe('geomap');

    const runner = findQueryRunner(panel);
    expect(runner).not.toBeNull();
    expect(runner!.state.datasource?.uid).toBe(MERAKI_DS_UID);

    const q = runner!.state.queries[0] as unknown as { kind: QueryKind; orgId: string };
    expect(q.kind).toBe(QueryKind.NetworkGeo);
    expect(q.orgId).toBe('$org');
  });

  it('networkLinkGraphPanel wires deviceLldpCdp scoped to $network', () => {
    const panel = networkLinkGraphPanel();
    expect(panel.state.pluginId).toBe('nodeGraph');

    const runner = findQueryRunner(panel);
    expect(runner).not.toBeNull();
    expect(runner!.state.datasource?.uid).toBe(MERAKI_DS_UID);

    const q = runner!.state.queries[0] as unknown as {
      kind: QueryKind;
      orgId: string;
      networkIds: string[];
    };
    expect(q.kind).toBe(QueryKind.DeviceLldpCdp);
    expect(q.orgId).toBe('$org');
    expect(q.networkIds).toEqual(['$network']);
  });
});
