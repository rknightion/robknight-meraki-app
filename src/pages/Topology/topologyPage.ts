import { SceneAppPage } from '@grafana/scenes';
import { PLUGIN_BASE_URL, ROUTES } from '../../constants';
import { topologyScene } from './topologyScene';

/**
 * Top-level Topology page (v0.5 §4.4.4-D).
 *
 * A single full-page LLDP/CDP device link graph for the selected
 * network. No drilldowns yet — the link graph itself is interactive
 * enough that the page is useful as-is.
 */
export const topologyPage = new SceneAppPage({
  title: 'Topology',
  subTitle:
    'Per-network LLDP/CDP device link graph. Links are discovered via ' +
    'LLDP and CDP; external neighbours appear as nodes labelled "external".',
  url: `${PLUGIN_BASE_URL}/${ROUTES.Topology}`,
  routePath: `${ROUTES.Topology}/*`,
  getScene: () => topologyScene(),
});
