import { test, expect } from './fixtures';
import type { Page, Route } from '@playwright/test';
import pluginJson from '../src/plugin.json';
import { ROUTES } from '../src/constants';

// RecordingsPanel only renders inside the "full" variant of MerakiConfigForm —
// same invariant as AlertRulesPanel (see tests/alerts.spec.ts). The catalog
// page (`/plugins/<id>`) uses the "catalog" variant and does NOT render this
// panel, so these specs navigate to the in-app Configuration page.
const CONFIG_PATH = `/${ROUTES.Configuration}`;

// Test-ids are duplicated here (rather than imported from src/components/testIds)
// so the spec file stays decoupled from the frontend module graph — Playwright
// only needs the string values. Keep in sync with testIds.recordingsPanel.
const TID = {
  container: 'data-testid rrp-container',
  featureToggleBanner: 'data-testid rrp-feature-toggle-banner',
  resultBanner: 'data-testid rrp-result-banner',
  statusPill: 'data-testid rrp-status-pill',
  reconcileButton: 'data-testid rrp-reconcile',
  uninstallButton: 'data-testid rrp-uninstall',
  datasourcePicker: 'data-testid rrp-datasource-picker',
  datasourceHint: 'data-testid rrp-datasource-hint',
  groupInstall: (gid: string) => `data-testid rrp-group-install-${gid}`,
  templateRow: (gid: string, tid: string) => `data-testid rrp-template-${gid}-${tid}`,
};

// Mirror of the live recordings bundle. Fourteen templates across six
// groups, matching pkg/plugin/recordings/templates/. The Playwright spec
// only asserts the presence of the testids the panel renders for each
// group + template, so threshold shape is kept minimal here.
const DEFAULT_RECORDING_TEMPLATES = {
  groups: [
    {
      id: 'availability',
      displayName: 'Availability',
      templates: [
        {
          id: 'device-status-overview',
          groupId: 'availability',
          displayName: 'Device status overview',
          metric: 'meraki_device_status_count',
          thresholds: [{ key: 'interval', type: 'duration', default: '1m', label: 'Interval' }],
        },
      ],
    },
    {
      id: 'wan',
      displayName: 'WAN',
      templates: [
        {
          id: 'appliance-uplink-status',
          groupId: 'wan',
          displayName: 'Appliance uplink status',
          metric: 'meraki_appliance_uplink_up',
          thresholds: [{ key: 'interval', type: 'duration', default: '1m', label: 'Interval' }],
        },
        {
          id: 'appliance-uplinks-overview',
          groupId: 'wan',
          displayName: 'Appliance uplinks overview',
          metric: 'meraki_appliance_uplinks_active_count',
          thresholds: [{ key: 'interval', type: 'duration', default: '1m', label: 'Interval' }],
        },
        {
          id: 'appliance-vpn-summary',
          groupId: 'wan',
          displayName: 'Appliance VPN summary',
          metric: 'meraki_appliance_vpn_tunnels_up_pct',
          thresholds: [{ key: 'interval', type: 'duration', default: '1m', label: 'Interval' }],
        },
        {
          id: 'device-uplinks-loss-latency',
          groupId: 'wan',
          displayName: 'Device uplinks loss + latency',
          metric: 'meraki_wan_uplink_loss_pct',
          thresholds: [{ key: 'interval', type: 'duration', default: '5m', label: 'Interval' }],
        },
      ],
    },
    {
      id: 'wireless',
      displayName: 'Wireless',
      templates: [
        {
          id: 'ap-client-count',
          groupId: 'wireless',
          displayName: 'AP client count',
          metric: 'meraki_ap_client_count',
          thresholds: [{ key: 'interval', type: 'duration', default: '1m', label: 'Interval' }],
        },
        {
          id: 'channel-util-history',
          groupId: 'wireless',
          displayName: 'Channel utilization history',
          metric: 'meraki_wireless_channel_util_pct',
          thresholds: [{ key: 'interval', type: 'duration', default: '5m', label: 'Interval' }],
        },
        {
          id: 'usage-history',
          groupId: 'wireless',
          displayName: 'Usage history',
          metric: 'meraki_wireless_usage_bytes',
          thresholds: [{ key: 'interval', type: 'duration', default: '5m', label: 'Interval' }],
        },
        {
          id: 'packet-loss-by-network',
          groupId: 'wireless',
          displayName: 'Packet loss by network',
          metric: 'meraki_wireless_packet_loss_pct',
          thresholds: [],
        },
      ],
    },
    {
      id: 'cellular',
      displayName: 'Cellular',
      templates: [
        {
          id: 'mg-uplink-signal',
          groupId: 'cellular',
          displayName: 'MG uplink signal',
          metric: 'meraki_mg_rsrp_dbm',
          thresholds: [],
        },
      ],
    },
    {
      id: 'switches',
      displayName: 'Switches',
      templates: [
        {
          id: 'ports-overview',
          groupId: 'switches',
          displayName: 'Ports overview',
          metric: 'meraki_switch_ports_count',
          thresholds: [{ key: 'interval', type: 'duration', default: '1m', label: 'Interval' }],
        },
      ],
    },
    {
      id: 'alerts',
      displayName: 'Alerts',
      templates: [
        {
          id: 'alerts-overview-by-type',
          groupId: 'alerts',
          displayName: 'Alerts overview by type',
          metric: 'meraki_alerts_by_type_count',
          thresholds: [{ key: 'interval', type: 'duration', default: '5m', label: 'Interval' }],
        },
        {
          id: 'alerts-overview-by-network',
          groupId: 'alerts',
          displayName: 'Alerts overview by network',
          metric: 'meraki_alerts_by_network_count',
          thresholds: [{ key: 'interval', type: 'duration', default: '5m', label: 'Interval' }],
        },
        {
          id: 'alerts-history-by-severity',
          groupId: 'alerts',
          displayName: 'Alerts history by severity',
          metric: 'meraki_alerts_history_count',
          thresholds: [],
        },
      ],
    },
  ],
};

type RecordingsStatus = {
  installed: Array<{
    groupId: string;
    templateId: string;
    orgId: string;
    uid: string;
    enabled: boolean;
  }>;
  targetDatasourceUid?: string;
  lastReconciledAt?: string;
  grafanaReady: boolean;
};

/**
 * Installs `page.route()` handlers that short-circuit the recordings
 * bundle's read endpoints with canned JSON. Mirrors `mockAlertsEndpoints`
 * in `fixtures.ts`. Specs that exercise the real Go reconciler should opt
 * into `E2E_MOCK_GRAFANA=1` and NOT call this helper.
 */
async function mockRecordingsEndpoints(
  page: Page,
  state: { templates: typeof DEFAULT_RECORDING_TEMPLATES; status: RecordingsStatus },
): Promise<void> {
  const prefix = `/api/plugins/${pluginJson.id}/resources/recordings`;

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

// E2E_MOCK_GRAFANA=1 is the same opt-in toggle the alerts spec uses. When
// it isn't set we skip the reconcile / uninstall specs that depend on the
// Go-level InMemoryGrafana stub; render + banner specs above still run
// because they mock the read endpoints at the HTTP layer via page.route().
const GO_MOCK_ENABLED = process.env.E2E_MOCK_GRAFANA === '1';

test.describe('Bundled recording rules — config UI (v0.7 §4.6.8)', () => {
  test('renders the panel with all 14 templates across 6 groups', async ({
    gotoPage,
    page,
  }) => {
    await mockRecordingsEndpoints(page, {
      templates: DEFAULT_RECORDING_TEMPLATES,
      status: { installed: [], grafanaReady: true },
    });
    await gotoPage(CONFIG_PATH);

    const panel = page.getByTestId(TID.container);
    await expect(panel).toBeVisible();
    await expect(panel.getByText('Bundled recording rules')).toBeVisible();

    // DataSourcePicker is rendered at the top of the panel — the picker
    // itself is a @grafana/runtime component so we only assert the
    // wrapping data-testid is present (deep picker interactions are
    // covered by the Jest suite in RecordingsPanel.test.tsx).
    await expect(page.getByTestId(TID.datasourcePicker)).toBeVisible();

    // Expand every group so the per-template rows render, then count.
    // GroupRow only renders the template row when `groupState.installed`
    // is true (see src/components/AppConfig/RuleBundlePanel/GroupRow.tsx),
    // so every group in the mock needs its install toggle checked before
    // the per-template assertions below. Driving from the mock keeps the
    // spec in lockstep with DEFAULT_RECORDING_TEMPLATES when rules or
    // groups are added.
    for (const group of DEFAULT_RECORDING_TEMPLATES.groups) {
      const toggle = page.getByTestId(TID.groupInstall(group.id));
      await expect(toggle).toBeVisible();
      await toggle.check({ force: true });
    }

    // Each template contributes one data-testid row. 14 templates total.
    for (const group of DEFAULT_RECORDING_TEMPLATES.groups) {
      for (const tpl of group.templates) {
        await expect(page.getByTestId(TID.templateRow(group.id, tpl.id))).toBeVisible();
      }
    }

    // Status pill is the stable hook for "X of Y groups installed".
    await expect(page.getByTestId(TID.statusPill)).toBeVisible();
  });

  test('reconcile button is disabled until a target datasource is picked', async ({
    gotoPage,
    page,
  }) => {
    await mockRecordingsEndpoints(page, {
      templates: DEFAULT_RECORDING_TEMPLATES,
      status: { installed: [], grafanaReady: true },
    });
    await gotoPage(CONFIG_PATH);

    // Hint text is rendered while no target datasource is set.
    await expect(page.getByTestId(TID.datasourceHint)).toBeVisible();

    // And the reconcile button is disabled — the button STILL renders
    // (so operators see it) but it will not submit until the DS is set.
    await expect(page.getByTestId(TID.reconcileButton)).toBeDisabled();
  });

  test('shows the feature-toggle banner when grafanaReady=false', async ({
    gotoPage,
    page,
  }) => {
    await mockRecordingsEndpoints(page, {
      templates: DEFAULT_RECORDING_TEMPLATES,
      status: { installed: [], grafanaReady: false },
    });
    await gotoPage(CONFIG_PATH);

    const banner = page.getByTestId(TID.featureToggleBanner);
    await expect(banner).toBeVisible();
    await expect(banner).toContainText(/externalServiceAccounts/i);

    // With grafanaReady=false the reconcile button is also disabled —
    // either lock (no DS, or no feature toggle) keeps the user from
    // issuing a call that will fail.
    await expect(page.getByTestId(TID.reconcileButton)).toBeDisabled();
  });

  test.describe('Reconcile + uninstall (requires E2E_MOCK_GRAFANA=1)', () => {
    test.skip(
      !GO_MOCK_ENABLED,
      'Set E2E_MOCK_GRAFANA=1 on the Grafana container before running; see ' +
        '.config/AGENTS/e2e-testing.md',
    );

    const RECONCILE_URL = `**/api/plugins/${pluginJson.id}/resources/recordings/reconcile`;
    const UNINSTALL_URL = `**/api/plugins/${pluginJson.id}/resources/recordings/uninstall-all`;
    const SETTINGS_URL = `**/api/plugins/${pluginJson.id}/settings`;

    // The Go-level InMemoryGrafana stub persists for the process
    // lifetime, so we target a sentinel datasource UID + clear state
    // between scenarios. The DS UID does NOT need to exist in Grafana's
    // real `/api/datasources` table — the stub only looks at the string
    // that ends up on `record.target_datasource_uid`, and the reconcile
    // body carries it directly.
    const TEST_TARGET_DS_UID = 'e2e-prom-target';

    /**
     * Preseed jsonData.recordings.targetDatasourceUid via the plugin
     * settings endpoint so the Reconcile button is enabled immediately
     * on mount. This mirrors what the RecordingsPanel would do on
     * `onChange` of the DataSourcePicker — driving the picker itself
     * via keyboard in a hermetic way is fragile (the DataSourcePicker
     * popover lazy-loads its options from `/api/datasources`), so we
     * short-circuit to the persisted state the panel reads on first
     * paint.
     */
    async function seedTargetDatasourceUid(page: Page): Promise<void> {
      await page
        .request
        .post(`/api/plugins/${pluginJson.id}/settings`, {
          data: {
            jsonData: { recordings: { targetDatasourceUid: TEST_TARGET_DS_UID } },
          },
          failOnStatusCode: false,
        })
        .catch(() => null);
    }

    test('reconcile installs the availability group after picking a target DS', async ({
      gotoPage,
      page,
    }) => {
      // Clean slate — the Go stub persists across tests in the same
      // process, so we uninstall-all first. Ignore errors if nothing is
      // installed yet.
      await page
        .request
        .post(`/api/plugins/${pluginJson.id}/resources/recordings/uninstall-all`, {
          failOnStatusCode: false,
        })
        .catch(() => null);

      // Seed the target DS so the Reconcile button is enabled on mount.
      const settingsResp = page.waitForResponse(SETTINGS_URL).catch(() => null);
      await seedTargetDatasourceUid(page);
      await settingsResp;

      await gotoPage(CONFIG_PATH);

      // Reconcile is disabled until the panel's first-paint effect
      // picks up the persisted target DS; wait for it to enable.
      const reconcileBtn = page.getByTestId(TID.reconcileButton);
      await expect(reconcileBtn).toBeEnabled();

      // Toggle the availability group on and click Reconcile.
      await page.getByTestId(TID.groupInstall('availability')).check({ force: true });

      const reconcileResp = page.waitForResponse(RECONCILE_URL);
      await reconcileBtn.click();
      const resp = await reconcileResp;
      expect(resp.status()).toBe(200);

      const body = await resp.json();
      // 1 template × N mock orgs — exact count depends on the stub's
      // org fan-out, but at least one rule must have been created.
      expect(Array.isArray(body.created)).toBe(true);
      expect(body.created.length).toBeGreaterThan(0);

      const banner = page.getByTestId(TID.resultBanner);
      await expect(banner).toBeVisible();
      await expect(banner).toContainText(/created/i);
    });

    test('uninstall-all empties the managed recording rules', async ({
      gotoPage,
      page,
    }) => {
      // Ensure there's at least one rule installed from the previous
      // scenario; re-seed + re-reconcile if the test runs in isolation.
      await seedTargetDatasourceUid(page);

      await gotoPage(CONFIG_PATH);

      const uninstallResp = page.waitForResponse(UNINSTALL_URL);
      await page.getByTestId(TID.uninstallButton).click();
      // ConfirmModal — click the "Uninstall" confirm button.
      await page.getByRole('button', { name: 'Uninstall' }).click();
      const resp = await uninstallResp;
      expect(resp.status()).toBe(200);

      const body = await resp.json();
      expect(Array.isArray(body.deleted)).toBe(true);

      const banner = page.getByTestId(TID.resultBanner);
      await expect(banner).toBeVisible();
      await expect(banner).toContainText(/uninstall/i);
    });
  });
});
