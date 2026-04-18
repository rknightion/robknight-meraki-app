import { SceneAppPage } from '@grafana/scenes';
import { PLUGIN_BASE_URL, ROUTES } from '../../constants';
import { appliancesScene } from './appliancesScene';
import { applianceDetailPage } from './applianceDetailPage';

/**
 * Top-level Appliances page — mounted at `/a/<plugin>/appliances`. Hosts
 * the overview scene and a `:serial/*` drilldown that resolves to the
 * tabbed per-appliance detail page (Overview / Uplinks / VPN / Firewall).
 */
export const appliancesPage = new SceneAppPage({
  title: 'Appliances',
  subTitle:
    'Security appliances (MX) — uplinks, VPN peers, and firewall summary.',
  url: `${PLUGIN_BASE_URL}/${ROUTES.Appliances}`,
  routePath: `${ROUTES.Appliances}/*`,
  getScene: () => appliancesScene(),
  drilldowns: [
    {
      routePath: ':serial/*',
      getPage: (match, parent) => {
        const serial = decodeURIComponent(match.params.serial);
        return applianceDetailPage(serial, parent.state.url);
      },
    },
  ],
});
