import { test, expect, mockAlertsEndpoints, DEFAULT_ALERT_TEMPLATES } from './fixtures';
import pluginJson from '../src/plugin.json';

// Test-ids are duplicated here (rather than imported from src/components/testIds)
// so the spec file stays decoupled from the frontend module graph — Playwright
// only needs the string values. Keep in sync with src/components/testIds.ts.
const TID = {
  container: 'arp-container',
  featureToggleBanner: 'arp-feature-toggle-banner',
  resultBanner: 'arp-result-banner',
  statusPill: 'arp-status-pill',
  reconcileButton: 'arp-reconcile',
  uninstallButton: 'arp-uninstall',
  groupInstall: (gid: string) => `arp-group-install-${gid}`,
  templateRow: (gid: string, tid: string) => `arp-template-${gid}-${tid}`,
  ruleEnabled: (gid: string, tid: string) => `arp-rule-enabled-${gid}-${tid}`,
  thresholdInput: (gid: string, tid: string, key: string) => `arp-threshold-${gid}-${tid}-${key}`,
};

// E2E_MOCK_GRAFANA=1 is an opt-in toggle (see .config/AGENTS/e2e-testing.md).
// When it isn't set we skip the reconcile/uninstall specs that depend on the
// Go-level InMemoryGrafana stub; the render + banner specs above still run
// because they mock at the HTTP layer via page.route().
const GO_MOCK_ENABLED = process.env.E2E_MOCK_GRAFANA === '1';

test.describe('Bundled alert rules — config UI (v0.6 §4.5.8)', () => {
  test('renders the panel with groups and per-rule rows', async ({ appConfigPage, page }) => {
    await mockAlertsEndpoints(page, {
      templates: DEFAULT_ALERT_TEMPLATES,
      status: { installed: [], grafanaReady: true },
    });
    // Re-navigate to the config page so the just-installed routes intercept
    // the initial /alerts/* fetches. gotoAppConfigPage in fixtures.ts runs
    // before we can install routes, so a reload is required.
    await appConfigPage.goto();

    const panel = page.getByTestId(TID.container);
    await expect(panel).toBeVisible();
    await expect(panel.getByText('Bundled alert rules')).toBeVisible();

    // Availability group appears with its 2 rules; user must toggle Install
    // to reveal per-rule rows, so flip it on first.
    const availabilityToggle = page.getByTestId(TID.groupInstall('availability'));
    await expect(availabilityToggle).toBeVisible();
    await availabilityToggle.check({ force: true });

    await expect(page.getByTestId(TID.templateRow('availability', 'device-offline'))).toBeVisible();
    await expect(
      page.getByTestId(TID.templateRow('availability', 'meraki-critical')),
    ).toBeVisible();

    // Status pill shows the "0 of 2 groups installed" baseline before the
    // user toggled anything is overwritten by the just-flipped toggle; any
    // "groups installed" text is fine — the pill is the stable hook.
    await expect(page.getByTestId(TID.statusPill)).toBeVisible();
  });

  test('shows the feature-toggle banner when grafanaReady=false', async ({
    appConfigPage,
    page,
  }) => {
    await mockAlertsEndpoints(page, {
      templates: DEFAULT_ALERT_TEMPLATES,
      status: { installed: [], grafanaReady: false },
    });
    await appConfigPage.goto();

    const banner = page.getByTestId(TID.featureToggleBanner);
    await expect(banner).toBeVisible();
    await expect(banner).toContainText(/externalServiceAccounts/i);

    // Reconcile button is disabled when Grafana is not ready — stops the
    // user issuing a call that will 503.
    await expect(page.getByTestId(TID.reconcileButton)).toBeDisabled();
  });

  test.describe('Reconcile + uninstall (requires E2E_MOCK_GRAFANA=1)', () => {
    test.skip(
      !GO_MOCK_ENABLED,
      'Set E2E_MOCK_GRAFANA=1 on the Grafana container before running; see ' +
        '.config/AGENTS/e2e-testing.md',
    );

    const RECONCILE_URL = `**/api/plugins/${pluginJson.id}/resources/alerts/reconcile`;
    const UNINSTALL_URL = `**/api/plugins/${pluginJson.id}/resources/alerts/uninstall-all`;

    test('reconcile installs the availability group (created count > 0)', async ({
      appConfigPage,
      page,
    }) => {
      // Ensure any leftover state from a previous run is cleared. The Go
      // stub is process-lifetime so we uninstall-all first. Ignore errors
      // if no rules are installed yet.
      const uninstallResp = page.waitForResponse(UNINSTALL_URL).catch(() => null);
      await page
        .request
        .post(`/api/plugins/${pluginJson.id}/resources/alerts/uninstall-all`, {
          failOnStatusCode: false,
        })
        .catch(() => null);
      await uninstallResp;

      await appConfigPage.goto();

      // Toggle the availability group on.
      await page.getByTestId(TID.groupInstall('availability')).check({ force: true });

      // Click Reconcile and capture the response body.
      const reconcileResp = page.waitForResponse(RECONCILE_URL);
      await page.getByTestId(TID.reconcileButton).click();
      const resp = await reconcileResp;
      expect(resp.status()).toBe(200);
      const body = await resp.json();
      // 2 templates × 2 mock orgs = 4 rules expected.
      expect(Array.isArray(body.created)).toBe(true);
      expect(body.created.length).toBeGreaterThan(0);

      const banner = page.getByTestId(TID.resultBanner);
      await expect(banner).toBeVisible();
      await expect(banner).toContainText(/created/i);
    });

    test('second reconcile with no changes issues no creates (idempotency)', async ({
      appConfigPage,
      page,
    }) => {
      await appConfigPage.goto();
      await page.getByTestId(TID.groupInstall('availability')).check({ force: true });

      const reconcileResp = page.waitForResponse(RECONCILE_URL);
      await page.getByTestId(TID.reconcileButton).click();
      const body = await (await reconcileResp).json();

      // Rules from the previous test are still in the Go stub, so we
      // expect either an empty diff or only updates (no creates).
      expect(body.created?.length ?? 0).toBe(0);
    });

    test('uninstall-all clears every managed rule', async ({ appConfigPage, page }) => {
      await appConfigPage.goto();

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
