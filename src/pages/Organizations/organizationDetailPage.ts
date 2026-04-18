import { SceneAppPage } from '@grafana/scenes';
import { organizationOverviewScene } from './organizationOverviewScene';
import { organizationDevicesScene } from './organizationDevicesScene';
import { organizationAlertsScene } from './organizationAlertsScene';

/**
 * Per-organization detail page — a tabbed `SceneAppPage` with three
 * children: Overview (KPIs + donut + networks), Devices (inventory
 * table), and Alerts (placeholder until the Alerts query kind lands).
 *
 * Returned as a factory so the parent drilldown can pass the matched
 * `orgId` plus the parent URL prefix; Scenes uses the first tab as the
 * default landing page when the user hits the bare detail URL.
 */
export function organizationDetailPage(orgId: string, parentUrl: string): SceneAppPage {
  const encodedOrgId = encodeURIComponent(orgId);
  const baseUrl = `${parentUrl}/${encodedOrgId}`;

  return new SceneAppPage({
    // TODO: resolve this to the actual org name once the detail scene
    // surfaces it (the KPI row has it, but the page title is set before
    // those queries complete).
    title: orgId,
    subTitle: 'Organization detail — networks, devices, and alerts.',
    titleIcon: 'building',
    url: baseUrl,
    // Wildcard so the tab children and future drilldowns route correctly
    // beneath this page.
    routePath: `${encodedOrgId}/*`,
    tabs: [
      new SceneAppPage({
        title: 'Overview',
        url: `${baseUrl}/overview`,
        routePath: 'overview',
        getScene: () => organizationOverviewScene(orgId),
      }),
      new SceneAppPage({
        title: 'Devices',
        url: `${baseUrl}/devices`,
        routePath: 'devices',
        getScene: () => organizationDevicesScene(orgId),
      }),
      new SceneAppPage({
        title: 'Alerts',
        url: `${baseUrl}/alerts`,
        routePath: 'alerts',
        getScene: () => organizationAlertsScene(orgId),
      }),
    ],
  });
}
