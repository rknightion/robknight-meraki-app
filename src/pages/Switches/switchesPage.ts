import { SceneAppPage } from '@grafana/scenes';
import { prefixRoute } from '../../utils/utils.routing';
import { ROUTES } from '../../constants';
import { switchesScene } from './switchesScene';
import { switchDetailPage } from './switchDetailPage';

/**
 * Top-level Switches page — mounted at `/a/<plugin>/switches`. A single
 * drilldown routes `:serial/*` into the tabbed per-switch detail page; the
 * nested `ports/:portId/*` drilldown is owned by the detail page's Ports
 * tab (see `switchDetailPage.ts`).
 */
export const switchesPage = new SceneAppPage({
  title: 'Switches',
  subTitle: 'Meraki MS switches — fleet inventory and per-port status.',
  url: prefixRoute(ROUTES.Switches),
  // routePath below needs the raw slug (not prefixed).
  routePath: `${ROUTES.Switches}/*`,
  getScene: () => switchesScene(),
  drilldowns: [
    {
      routePath: ':serial/*',
      getPage: (match, parent) => {
        const serial = decodeURIComponent(match.params.serial);
        return switchDetailPage(serial, parent.state.url);
      },
    },
  ],
});
