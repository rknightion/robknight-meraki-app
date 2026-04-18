import { SceneAppPage } from '@grafana/scenes';
import { switchOverviewScene } from './switchOverviewScene';
import { switchPortsScene } from './switchPortsScene';
import { portDetailPage } from './portDetailPage';

/**
 * Per-switch detail page — a tabbed `SceneAppPage` with two children:
 * Overview (KPI tiles) and Ports (the colour-coded port map).
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

  return new SceneAppPage({
    // TODO: resolve to the real switch name once the KPI row surfaces it.
    // The model column of the Devices frame would be a good fallback.
    title: serial,
    subTitle: 'Switch detail — overview and per-port status.',
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
    ],
  });
}
