import { SceneAppPage } from '@grafana/scenes';
import { switchOverviewScene } from './switchOverviewScene';
import { switchPortsScene } from './switchPortsScene';
import { switchAlertsScene } from './switchAlertsScene';
import { portDetailPage } from './portDetailPage';
import { applyDeviceNameTitle } from '../../scene-helpers/device-name-title';

/**
 * Per-switch detail page — a tabbed `SceneAppPage` with three children:
 * Overview (KPI tiles + stack/L3/DHCP-seen context), Ports (the colour-
 * coded port map + neighbours + MAC table), and Alerts (assurance-alerts
 * feed filtered to this serial).
 *
 * The Ports tab additionally owns a nested drilldown for per-port detail
 * pages. We set the tab's `routePath` to `'ports/*'` (trailing wildcard)
 * because the tab has drilldowns underneath; the drilldown's `routePath`
 * matches the path suffix after the tab base, i.e. `ports/:portId/*`.
 *
 * The Overview tab lands by default when the user hits the bare detail URL
 * (Scenes 7 picks the first tab as the default).
 */
export function switchDetailPage(serial: string, parentUrl: string): SceneAppPage {
  const encodedSerial = encodeURIComponent(serial);
  const baseUrl = `${parentUrl}/${encodedSerial}`;
  const portsTabUrl = `${baseUrl}/ports`;
  const alertsTabUrl = `${baseUrl}/alerts`;

  const page = new SceneAppPage({
    title: serial,
    subTitle: 'Switch detail — overview, per-port status, and alerts.',
    titleIcon: 'sitemap',
    url: baseUrl,
    // Wildcard so the tabs and nested `ports/:portId/*` drilldown route
    // correctly beneath this page.
    routePath: `${encodedSerial}/*`,
    tabs: [
      new SceneAppPage({
        title: 'Overview',
        url: `${baseUrl}/overview`,
        routePath: 'overview',
        getScene: () => switchOverviewScene(serial),
      }),
      new SceneAppPage({
        title: 'Ports',
        url: portsTabUrl,
        // Trailing wildcard because this tab owns the ports/:portId/*
        // drilldown. Without it the nested drilldown wouldn't match.
        routePath: 'ports/*',
        getScene: () => switchPortsScene(serial),
        drilldowns: [
          {
            routePath: ':portId/*',
            getPage: (match, parent) => {
              const portId = decodeURIComponent(match.params.portId);
              // `parent.state.url` here is the Ports tab URL, so the port
              // detail page appends the port slug directly to it —
              // producing `/switches/:serial/ports/:portId`.
              return portDetailPage(serial, portId, parent.state.url);
            },
          },
        ],
      }),
      new SceneAppPage({
        title: 'Alerts',
        url: alertsTabUrl,
        routePath: 'alerts',
        getScene: () => switchAlertsScene(serial),
      }),
    ],
  });
  applyDeviceNameTitle(page, serial, 'switch');
  return page;
}
