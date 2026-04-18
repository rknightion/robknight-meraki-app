import { test, expect } from './fixtures';
import { ROUTES } from '../src/constants';

test.describe('Meraki app navigation', () => {
  test('Home page renders the intro panel', async ({ gotoPage, page }) => {
    await gotoPage(`/${ROUTES.Home}`);
    await expect(page.getByRole('heading', { name: 'Cisco Meraki' })).toBeVisible();
    await expect(page.getByText(/Meraki API key/i)).toBeVisible();
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

  test('Traffic page renders device-type selector', async ({ gotoPage, page }) => {
    await gotoPage(`/${ROUTES.Traffic}`);
    await expect(page.getByRole('heading', { name: 'Traffic' })).toBeVisible();
    await expect(
      page.getByLabel('Device type').or(page.getByText('Device type'))
    ).toBeVisible();
  });
});
