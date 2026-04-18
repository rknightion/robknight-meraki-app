import { SceneAppPage } from '@grafana/scenes';
import { PLUGIN_BASE_URL, ROUTES } from '../../constants';
import { accessPointsScene } from './accessPointsScene';
import { accessPointDetailPage } from './accessPointDetailPage';
import { familyGateWrap } from '../../scene-helpers/familyGate';

/**
 * Top-level Access Points page. Hosts the overview scene and a drilldown
 * that resolves to the per-AP detail page (a tabbed `SceneAppPage`).
 */
export const accessPointsPage = new SceneAppPage({
  title: 'Access Points',
  subTitle:
    'Wireless (MR) access points — inventory, channel utilisation, SSID usage, and per-AP clients.',
  url: `${PLUGIN_BASE_URL}/${ROUTES.AccessPoints}`,
  routePath: `${ROUTES.AccessPoints}/*`,
  getScene: familyGateWrap('wireless', () => accessPointsScene()),
  drilldowns: [
    {
      routePath: ':serial/*',
      getPage: (match, parent) => {
        const serial = decodeURIComponent(match.params.serial);
        return accessPointDetailPage(serial, parent.state.url);
      },
    },
  ],
});
