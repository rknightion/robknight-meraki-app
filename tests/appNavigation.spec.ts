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
});
