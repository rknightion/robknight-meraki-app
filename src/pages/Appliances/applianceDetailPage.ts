import { SceneAppPage } from '@grafana/scenes';
import { applianceFirewallScene } from './applianceFirewallScene';
import { applianceOverviewScene } from './applianceOverviewScene';
import { applianceUplinksScene } from './applianceUplinksScene';
import { applianceVpnScene } from './applianceVpnScene';
import { applyDeviceNameTitle } from '../../scene-helpers/device-name-title';

/**
 * Per-appliance detail page — a tabbed `SceneAppPage` with four children:
 *   - Overview: KPI tiles + per-serial uplink status.
 *   - Uplinks: loss / latency timeseries.
 *   - VPN: peer matrix + aggregated stats.
 *   - Firewall: port forwarding rules + appliance settings.
 *
 * Returned as a factory so the parent drilldown can pass the matched
 * serial plus the parent URL prefix. Mirrors the shape of
 * `accessPointDetailPage(serial, parentUrl)` and
 * `switchDetailPage(serial, parentUrl)`; Scenes uses the first tab as the
 * default landing page when the user hits the bare detail URL.
 */
export function applianceDetailPage(serial: string, parentUrl: string): SceneAppPage {
  const encodedSerial = encodeURIComponent(serial);
  const baseUrl = `${parentUrl}/${encodedSerial}`;

  const page = new SceneAppPage({
    title: serial,
    subTitle:
      'Appliance detail — uplink status, VPN peers, loss/latency, and firewall summary.',
    titleIcon: 'shield',
    url: baseUrl,
    // Wildcard so tab children route correctly beneath this page.
    routePath: `${encodedSerial}/*`,
    tabs: [
      new SceneAppPage({
        title: 'Overview',
        url: `${baseUrl}/overview`,
        routePath: 'overview',
        getScene: () => applianceOverviewScene(serial),
      }),
      new SceneAppPage({
        title: 'Uplinks',
        url: `${baseUrl}/uplinks`,
        routePath: 'uplinks',
        getScene: () => applianceUplinksScene(serial),
      }),
      new SceneAppPage({
        title: 'VPN',
        url: `${baseUrl}/vpn`,
        routePath: 'vpn',
        getScene: () => applianceVpnScene(serial),
      }),
      new SceneAppPage({
        title: 'Firewall',
        url: `${baseUrl}/firewall`,
        routePath: 'firewall',
        getScene: () => applianceFirewallScene(serial),
      }),
    ],
  });
  applyDeviceNameTitle(page, serial, 'appliance');
  return page;
}
