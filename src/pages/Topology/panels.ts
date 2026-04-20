import {
  PanelBuilders,
  SceneQueryRunner,
  VizPanel,
} from '@grafana/scenes';
import { MERAKI_DS_REF } from '../../scene-helpers/datasource';
import { QueryKind } from '../../datasource/types';

// Local copy of the `oneQuery` helper that other v0.5 page directories
// already inline. We deliberately don't import the shared
// `src/scene-helpers/panels.ts` factory because it doesn't expose
// `oneQuery` and we want this directory to be self-contained — adding a
// shared export specifically for Topology would couple two unrelated
// pages.
function oneQuery(params: {
  refId?: string;
  kind: QueryKind;
  orgId?: string;
  networkIds?: string[];
  serials?: string[];
}): SceneQueryRunner {
  const { refId = 'A', kind, orgId, networkIds, serials } = params;
  const query: Record<string, unknown> & { refId: string } = { refId, kind };
  query.orgId = orgId ?? '$org';
  if (networkIds && networkIds.length > 0) {
    query.networkIds = networkIds;
  }
  if (serials && serials.length > 0) {
    query.serials = serials;
  }
  return new SceneQueryRunner({
    datasource: MERAKI_DS_REF,
    queries: [query],
  });
}

/**
 * Per-network device link graph (the full Topology page).
 *
 * Backed by the `deviceLldpCdp` query kind which emits the two-frame
 * Grafana Node Graph contract:
 *   - "nodes" frame: id, title, subtitle, mainstat
 *   - "edges" frame: id, source, target
 *
 * §4.4.4-D explicitly gates the LLDP/CDP fan-out to per-network scope —
 * the panel binds to the single-select `$network` variable. The backend
 * caps the per-network device fan-out at 50 devices (see
 * `deviceLldpCdpFanoutCap` in pkg/plugin/query/topology.go) so a
 * 200-device network won't blow through the rate budget.
 */
export function networkLinkGraphPanel(): VizPanel {
  const runner = oneQuery({
    kind: QueryKind.DeviceLldpCdp,
    networkIds: ['$network'],
  });
  return PanelBuilders.nodegraph()
    .setTitle('Device link graph (LLDP/CDP)')
    .setDescription(
      'Per-network LLDP/CDP topology. Nodes are Meraki devices in the ' +
        'selected network; edges are discovered via LLDP or CDP. External ' +
        'neighbours (e.g. upstream routers) appear as nodes labelled ' +
        '"external".'
    )
    .setData(runner)
    .setNoValue('No LLDP/CDP neighbours discovered for this network.')
    .build();
}
