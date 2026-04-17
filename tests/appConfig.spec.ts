import { test, expect } from './fixtures';

test('saves Meraki plugin configuration', async ({ appConfigPage, page }) => {
  // If a prior run left a key configured, clear it.
  const resetButton = page.getByRole('button', { name: /reset/i });
  if (await resetButton.isVisible().catch(() => false)) {
    await resetButton.click();
  }

  await page.getByRole('textbox', { name: 'API key' }).fill('test-api-key');
  const baseUrl = page.getByRole('textbox', { name: 'Base URL' });
  await baseUrl.fill('https://api.meraki.com/api/v1');

  const saveResponse = appConfigPage.waitForSettingsResponse();
  await page.getByRole('button', { name: /^Save$/ }).click();
  await expect(saveResponse).toBeOK();
});
