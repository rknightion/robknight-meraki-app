import { SceneAppPage } from '@grafana/scenes';
import { apOverviewScene } from './apOverviewScene';
import { apClientsScene } from './apClientsScene';
import { apRfScene } from './apRfScene';

/**
 * Per-AP detail page — a tabbed `SceneAppPage` with three children:
 *   - Overview: top-line device KPIs (status, model, network, firmware).
 *   - Clients: table of currently associated stations.
 *   - RF: channel-utilisation timeseries, one per band.
 *
 * Returned as a factory so the parent drilldown can pass the matched serial
 * plus the parent URL prefix. Mirrors the shape of
 * `organizationDetailPage(orgId, parentUrl)`; Scenes uses the first tab as
 * the default landing page when the user hits the bare detail URL.
 */
export function accessPointDetailPage(serial: string, parentUrl: string): SceneAppPage {
  const encodedSerial = encodeURIComponent(serial);
  const baseUrl = `${parentUrl}/${encodedSerial}`;

  return new SceneAppPage({
    title: serial,
    subTitle: 'Access point detail — status, connected clients, and RF utilisation.',
    titleIcon: 'signal',
    url: baseUrl,
    // Wildcard so tab children and any future nested drilldowns route
    // correctly beneath this page.
    routePath: `${encodedSerial}/*`,
    tabs: [
      new SceneAppPage({
        title: 'Overview',
        url: `${baseUrl}/overview`,
        routePath: 'overview',
        getScene: () => apOverviewScene(serial),
      }),
      new SceneAppPage({
        title: 'Clients',
        url: `${baseUrl}/clients`,
        routePath: 'clients',
        getScene: () => apClientsScene(serial),
      }),
      new SceneAppPage({
        title: 'RF',
        url: `${baseUrl}/rf`,
        routePath: 'rf',
        getScene: () => apRfScene(serial),
      }),
    ],
  });
}
