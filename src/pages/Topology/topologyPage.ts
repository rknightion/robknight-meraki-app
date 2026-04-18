import { SceneAppPage } from '@grafana/scenes';
import { PLUGIN_BASE_URL, ROUTES } from '../../constants';
import { topologyScene } from './topologyScene';

/**
 * Top-level Topology page (v0.5 §4.4.4-D).
 *
 * Two rows: a network geomap and a per-network LLDP/CDP link graph.
 * No drilldowns yet — Row 2 is already scoped to a single network via
 * the page's $network variable. A future iteration could add a
 * per-device detail drilldown if operators ask for one, but the link
 * graph itself is interactive enough that the page is useful as-is.
 */
export const topologyPage = new SceneAppPage({
  title: 'Topology',
  subTitle:
    'Network geomap + per-network LLDP/CDP link graph. Locations are ' +
    'derived from device geo-tags; links are discovered via LLDP and CDP.',
  url: `${PLUGIN_BASE_URL}/${ROUTES.Topology}`,
  routePath: `${ROUTES.Topology}/*`,
  getScene: () => topologyScene(),
});
