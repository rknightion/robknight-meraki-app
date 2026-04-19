import { test, expect } from './fixtures';
import { testIds } from '../src/components/testIds';

test('saves Meraki plugin configuration', async ({ appConfigPage, page }) => {
  // Wait for the form to finish rendering — the Region combobox is near the
  // bottom of the form, so its presence means the API-key + Reset controls
  // above it are also committed to the DOM.
  const region = page.getByTestId(testIds.appConfig.region);
  await expect(region).toBeVisible();

  const apiKey = page.getByRole('textbox', { name: 'API key' });

  // SecretInput disables the textbox and shows a Reset button when a key is
  // already persisted. In CI the first run is always fresh, so this is a
  // no-op there; locally it cleans up state from a previous run.
  const resetButton = page.getByRole('button', { name: /reset/i });
  if (await resetButton.isVisible()) {
    await resetButton.click();
    await expect(apiKey).toBeEnabled();
  }

  await apiKey.fill('test-api-key');

  // Base URL is disabled unless Region = Custom…. Flip the region first so
  // the input becomes editable, then type a URL.
  await region.click();
  await page.getByRole('option', { name: 'Custom…' }).click();

  const baseUrl = page.getByRole('textbox', { name: 'Base URL' });
  await expect(baseUrl).toBeEnabled();
  await baseUrl.fill('https://api.meraki.com/api/v1');

  const saveResponse = appConfigPage.waitForSettingsResponse();
  await page.getByRole('button', { name: /^Save$/ }).click();
  await expect(saveResponse).toBeOK();
});
