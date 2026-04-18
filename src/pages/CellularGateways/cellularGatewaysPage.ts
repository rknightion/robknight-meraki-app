import { SceneAppPage } from '@grafana/scenes';
import { PLUGIN_BASE_URL, ROUTES } from '../../constants';
import { cellularGatewayDetailPage } from './cellularGatewayDetailPage';
import { cellularGatewaysScene } from './cellularGatewaysScene';

/**
 * Top-level Cellular Gateways page. Hosts the overview scene and a
 * drilldown that resolves to the per-gateway detail page (a tabbed
 * `SceneAppPage`). URL pattern:
 * `/cellular-gateways/:serial/(overview|uplink|port-forwarding)`.
 */
export const cellularGatewaysPage = new SceneAppPage({
  title: 'Cellular Gateways',
  subTitle: 'Cellular gateways (MG) — uplink status, signal strength, and port forwarding.',
  titleIcon: 'signal',
  url: `${PLUGIN_BASE_URL}/${ROUTES.CellularGateways}`,
  routePath: `${ROUTES.CellularGateways}/*`,
  getScene: () => cellularGatewaysScene(),
  drilldowns: [
    {
      routePath: ':serial/*',
      getPage: (match, parent) => {
        const serial = decodeURIComponent(match.params.serial);
        return cellularGatewayDetailPage(serial, parent.state.url);
      },
    },
  ],
});
