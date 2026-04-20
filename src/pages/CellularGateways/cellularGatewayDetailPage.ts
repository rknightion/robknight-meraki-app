import { SceneAppPage } from '@grafana/scenes';
import { cellularOverviewScene } from './cellularOverviewScene';
import { cellularPortForwardingScene } from './cellularPortForwardingScene';
import { cellularUplinkScene } from './cellularUplinkScene';
import { applyDeviceNameTitle } from '../../scene-helpers/device-name-title';

/**
 * Per-gateway detail page — a tabbed `SceneAppPage` with three children:
 *   - Overview: top-line device KPIs + uplink detail table.
 *   - Uplink: RSRP/RSRQ gauges + uplink detail table.
 *   - Port Forwarding: inbound port-forwarding rules table.
 *
 * Factory-shaped so the parent drilldown can pass the matched serial plus
 * the parent URL prefix. Mirrors {@link accessPointDetailPage} /
 * {@link cameraDetailPage}.
 */
export function cellularGatewayDetailPage(serial: string, parentUrl: string): SceneAppPage {
  const encodedSerial = encodeURIComponent(serial);
  const baseUrl = `${parentUrl}/${encodedSerial}`;

  const page = new SceneAppPage({
    title: serial,
    subTitle: 'Cellular gateway detail — status, uplink, and port forwarding.',
    titleIcon: 'signal',
    url: baseUrl,
    routePath: `${encodedSerial}/*`,
    tabs: [
      new SceneAppPage({
        title: 'Overview',
        url: `${baseUrl}/overview`,
        routePath: 'overview',
        getScene: () => cellularOverviewScene(serial),
      }),
      new SceneAppPage({
        title: 'Uplink',
        url: `${baseUrl}/uplink`,
        routePath: 'uplink',
        getScene: () => cellularUplinkScene(serial),
      }),
      new SceneAppPage({
        title: 'Port Forwarding',
        url: `${baseUrl}/port-forwarding`,
        routePath: 'port-forwarding',
        getScene: () => cellularPortForwardingScene(serial),
      }),
    ],
  });
  applyDeviceNameTitle(page, serial, 'cellularGateway');
  return page;
}
