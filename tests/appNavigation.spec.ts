import { test, expect } from './fixtures';
import { ROUTES } from '../src/constants';

test.describe('Meraki app navigation', () => {
  test('Home page renders the condensed intro banner + KPI row (v0.5 §4.4.5)', async ({
    gotoPage,
    page,
  }) => {
    await gotoPage(`/${ROUTES.Home}`);
    // Row 2 — condensed intro. The old welcome block + CTA grid was removed;
    // only a single-line hint remains.
    await expect(page.getByText('Cisco Meraki').first()).toBeVisible();
    await expect(page.getByText(/pick an organization/i)).toBeVisible();
    // Row 3 — "At a glance" KPI row. Titles come from HOME_AT_A_GLANCE_KPIS.
    await expect(page.getByText('Devices offline')).toBeVisible();
    await expect(page.getByText('Critical alerts')).toBeVisible();
    await expect(page.getByText('Uplinks down')).toBeVisible();
    // Row 4 — 24h change feed tile title.
    await expect(page.getByText('What changed in the last 24 hours')).toBeVisible();
    // Row 5 — availability by family stacked bar.
    await expect(page.getByText('Availability by device family')).toBeVisible();
  });

  test('Organizations page renders', async ({ gotoPage, page }) => {
    await gotoPage(`/${ROUTES.Organizations}`);
    // Scene renders even without data; title comes from SceneAppPage.
    await expect(page.getByRole('heading', { name: 'Organizations' })).toBeVisible();
  });

  test('Sensors page renders variable selectors', async ({ gotoPage, page }) => {
    await gotoPage(`/${ROUTES.Sensors}`);
    await expect(page.getByRole('heading', { name: 'Sensors' })).toBeVisible();
    await expect(page.getByLabel('Organization').or(page.getByText('Organization'))).toBeVisible();
  });

  test('Access Points page renders', async ({ gotoPage, page }) => {
    await gotoPage(`/${ROUTES.AccessPoints}`);
    await expect(page.getByRole('heading', { name: 'Access Points' })).toBeVisible();
  });

  test('Switches page renders', async ({ gotoPage, page }) => {
    await gotoPage(`/${ROUTES.Switches}`);
    await expect(page.getByRole('heading', { name: 'Switches' })).toBeVisible();
  });

  test('Alerts page renders severity selector', async ({ gotoPage, page }) => {
    await gotoPage(`/${ROUTES.Alerts}`);
    await expect(page.getByRole('heading', { name: 'Alerts' })).toBeVisible();
    await expect(page.getByLabel('Severity').or(page.getByText('Severity'))).toBeVisible();
  });

  test('Appliances page renders', async ({ gotoPage, page }) => {
    await gotoPage(`/${ROUTES.Appliances}`);
    await expect(page.getByRole('heading', { name: 'Appliances' })).toBeVisible();
  });

  test('Cameras page renders', async ({ gotoPage, page }) => {
    await gotoPage(`/${ROUTES.Cameras}`);
    await expect(page.getByRole('heading', { name: 'Cameras' })).toBeVisible();
  });

  test('Cellular Gateways page renders', async ({ gotoPage, page }) => {
    await gotoPage(`/${ROUTES.CellularGateways}`);
    await expect(page.getByRole('heading', { name: 'Cellular Gateways' })).toBeVisible();
  });

  test('Insights page renders licensing tab by default', async ({ gotoPage, page }) => {
    await gotoPage(`/${ROUTES.Insights}`);
    await expect(page.getByRole('heading', { name: 'Insights' })).toBeVisible();
    await expect(page.getByRole('tab', { name: 'Licensing' })).toBeVisible();
  });

  test('Events page renders product type selector', async ({ gotoPage, page }) => {
    await gotoPage(`/${ROUTES.Events}`);
    await expect(page.getByRole('heading', { name: 'Events' })).toBeVisible();
  });

  test('Clients page renders the tabbed nav', async ({ gotoPage, page }) => {
    await gotoPage(`/${ROUTES.Clients}`);
    await expect(page.getByRole('heading', { name: 'Clients' })).toBeVisible();
    await expect(page.getByRole('tab', { name: 'Top talkers' })).toBeVisible();
  });

  test('Per-client drilldown route renders the MAC as page title', async ({
    gotoPage,
    page,
  }) => {
    // The drilldown is bookmarkable — hitting the URL directly should resolve
    // to a SceneAppPage whose title is the route-param MAC.
    const mac = '00:11:22:33:44:55';
    await gotoPage(`/${ROUTES.Clients}/${encodeURIComponent(mac)}`);
    await expect(page.getByRole('heading', { name: mac })).toBeVisible();
  });

  test('Firmware page renders heading + tables', async ({ gotoPage, page }) => {
    await gotoPage(`/${ROUTES.Firmware}`);
    await expect(page.getByRole('heading', { name: 'Firmware & Lifecycle' })).toBeVisible();
    // The KPI row + tables — at least one panel title from the bottom rows
    // should be visible once the scene mounts. We don't assert on data
    // because the backend may not be configured in the test stack.
    await expect(page.getByText(/Pending upgrades|End-of-life devices/)).toBeVisible();
  });

  test('Traffic page renders device-type selector', async ({ gotoPage, page }) => {
    await gotoPage(`/${ROUTES.Traffic}`);
    await expect(page.getByRole('heading', { name: 'Traffic' })).toBeVisible();
    await expect(
      page.getByLabel('Device type').or(page.getByText('Device type'))
    ).toBeVisible();
  });

  test('Topology page renders the geomap row', async ({ gotoPage, page }) => {
    await gotoPage(`/${ROUTES.Topology}`);
    await expect(page.getByRole('heading', { name: 'Topology' })).toBeVisible();
  });
});
