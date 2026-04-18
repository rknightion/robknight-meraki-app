import { SceneAppPage } from '@grafana/scenes';
import { PLUGIN_BASE_URL, ROUTES } from '../../constants';
import { cameraDetailPage } from './cameraDetailPage';
import { camerasScene } from './camerasScene';
import { familyGateWrap } from '../../scene-helpers/familyGate';

/**
 * Top-level Cameras page. Hosts the overview scene and a drilldown that
 * resolves to the per-camera detail page (a tabbed `SceneAppPage`). URL
 * pattern mirrors the AP page: `/cameras/:serial/(overview|analytics|zones)`.
 */
export const camerasPage = new SceneAppPage({
  title: 'Cameras',
  subTitle: 'Security cameras (MV) — onboarding, inventory, and analytics.',
  titleIcon: 'camera',
  url: `${PLUGIN_BASE_URL}/${ROUTES.Cameras}`,
  routePath: `${ROUTES.Cameras}/*`,
  getScene: familyGateWrap('camera', () => camerasScene()),
  drilldowns: [
    {
      routePath: ':serial/*',
      getPage: (match, parent) => {
        const serial = decodeURIComponent(match.params.serial);
        return cameraDetailPage(serial, parent.state.url);
      },
    },
  ],
});
