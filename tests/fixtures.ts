import { AppConfigPage, AppPage, test as base } from '@grafana/plugin-e2e';
import type { Page, Route } from '@playwright/test';
import pluginJson from '../src/plugin.json';

type AppTestFixture = {
  appConfigPage: AppConfigPage;
  gotoPage: (path?: string) => Promise<AppPage>;
};

export const test = base.extend<AppTestFixture>({
  appConfigPage: async ({ gotoAppConfigPage }, use) => {
    const configPage = await gotoAppConfigPage({
      pluginId: pluginJson.id,
    });
    await use(configPage);
  },
  gotoPage: async ({ gotoAppPage }, use) => {
    await use((path) =>
      gotoAppPage({
        path,
        pluginId: pluginJson.id,
      })
    );
  },
});

export { expect } from '@grafana/plugin-e2e';

// -----------------------------------------------------------------------------
// Alerts-spec helpers
// -----------------------------------------------------------------------------

/**
 * Payload returned from `GET /resources/alerts/templates`. Kept in sync with
 * the Go DTOs in `pkg/plugin/resources.go` and the frontend mirror in
 * `src/components/AppConfig/alertsTypes.ts`. Duplicated minimally here
 * (rather than imported) because the Playwright fixture deliberately does
 * not pull frontend module code — reduces the spec's coupling to build
 * output.
 */
export type AlertsMockState = {
  templates: {
    groups: Array<{
      id: string;
      displayName: string;
      templates: Array<{
        id: string;
        groupId: string;
        displayName: string;
        severity: string;
        thresholds: Array<{
          key: string;
          type: string;
          default?: unknown;
          label?: string;
          help?: string;
          options?: string[];
        }>;
      }>;
    }>;
  };
  status: {
    installed: Array<{
      groupId: string;
      templateId: string;
      orgId: string;
      uid: string;
      enabled: boolean;
    }>;
    lastReconciledAt?: string;
    lastReconcileSummary?: { created: number; updated: number; deleted: number; failed: number };
    grafanaReady: boolean;
  };
};

/**
 * A small, known-shape templates fixture covering two groups so the spec can
 * assert both `availability` (2 rules) and `wan` (1 rule) end-to-end. These
 * IDs are real entries in `pkg/plugin/alerts/templates/` so the data-testids
 * the panel renders match what a live `/alerts/templates` call would return.
 */
export const DEFAULT_ALERT_TEMPLATES: AlertsMockState['templates'] = {
  groups: [
    {
      id: 'availability',
      displayName: 'Availability',
      templates: [
        {
          id: 'device-offline',
          groupId: 'availability',
          displayName: 'Device offline',
          severity: 'warning',
          thresholds: [
            { key: 'for_duration', type: 'duration', default: '5m', label: 'For duration' },
          ],
        },
        {
          id: 'meraki-critical',
          groupId: 'availability',
          displayName: 'Meraki critical alert',
          severity: 'critical',
          thresholds: [],
        },
      ],
    },
    {
      id: 'wan',
      displayName: 'WAN',
      templates: [
        {
          id: 'uplink-down',
          groupId: 'wan',
          displayName: 'Uplink down',
          severity: 'warning',
          thresholds: [
            { key: 'for_duration', type: 'duration', default: '5m', label: 'For duration' },
          ],
        },
      ],
    },
  ],
};

/**
 * Installs `page.route()` handlers that short-circuit the alerts bundle's
 * read endpoints (`/resources/alerts/templates` + `/resources/alerts/status`)
 * with canned JSON. Used by the hermetic render + banner specs — tests that
 * need to exercise the real Go reconciler should opt into E2E_MOCK_GRAFANA=1
 * instead and NOT call this helper.
 *
 * The route matches the fully-qualified plugin resource path so it doesn't
 * accidentally swallow other Grafana alerting API calls (which the panel's
 * "View in Grafana Alerting" link button points at).
 */
export async function mockAlertsEndpoints(page: Page, state: AlertsMockState): Promise<void> {
  const prefix = `/api/plugins/${pluginJson.id}/resources/alerts`;

  await page.route(`**${prefix}/templates`, (route: Route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(state.templates),
    }),
  );

  await page.route(`**${prefix}/status`, (route: Route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(state.status),
    }),
  );
}
