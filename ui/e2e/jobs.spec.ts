// Jobverlauf: Filter, Detailfenster mit Ausgabe und die Health-Checks.
import { test, expect } from './fixtures';
import { fenster, setzeSchalter } from './helpers';

test.describe('Jobs', () => {
  test('die Filter sind mit den Werten der Instanz befüllt', async ({ page }) => {
    await page.goto('/jobs');
    await expect(page.getByRole('heading', { level: 1, name: 'Jobs' })).toBeVisible();
    await expect(page.getByRole('combobox', { name: 'Art' }).locator('option', { hasText: 'Updates einspielen' })).toHaveCount(1);
    await expect(page.getByRole('combobox', { name: 'Ausgelöst von' }).locator('option', { hasText: 'scheduler' })).toHaveCount(1);
    await expect(page.getByRole('combobox', { name: 'Server' }).locator('option', { hasText: 'web01' })).toHaveCount(1);
    // Standard: nur beendete Jobs.
    await expect(page.getByRole('combobox', { name: 'Status' })).toContainText('Beendete');
  });

  test('ein Filter grenzt den Verlauf ein und steht in der Adresse', async ({ page }) => {
    await page.goto('/jobs');
    const verlauf = page.getByRole('grid', { name: 'Jobverlauf' });
    await expect(verlauf.getByRole('row').filter({ hasText: 'Updates einspielen' }).first()).toBeVisible();
    await page.getByRole('combobox', { name: 'Art' }).selectOption({ label: 'SSH härten' });
    await expect(page).toHaveURL(/kind=harden-ssh/);
    await expect(verlauf.getByRole('row').filter({ hasText: 'SSH härten' }).first()).toBeVisible();
    await expect(verlauf.getByRole('row').filter({ hasText: 'Updates einspielen' })).toHaveCount(0);
    await expect(page.getByText('Anfrage fehlgeschlagen')).toHaveCount(0);
  });

  test('jede Art, die der Filter anbietet, lässt sich wählen, ohne dass die Liste fehlschlägt', async ({ page }) => {
    await page.goto('/jobs');
    const art = page.getByRole('combobox', { name: 'Art' });
    await expect(art.locator('option', { hasText: 'Updates einspielen' })).toHaveCount(1); // die Arten kommen nachgeladen
    const arten = await art.locator('option').evaluateAll((optionen) => optionen.map((option) => (option as HTMLOptionElement).value).filter(Boolean));
    expect(arten.length).toBeGreaterThan(3);
    for (const wert of arten) {
      const antwort = page.waitForResponse((r) => r.url().includes('/api/v1/jobs?') && r.url().includes(`kind=${encodeURIComponent(wert)}`));
      await art.selectOption(wert);
      expect((await antwort).status(), `Art ${wert}`).toBe(200);
    }
  });

  test('ein Klick auf einen Job öffnet das Detailfenster mit Ausgabe, ein Direktlink ebenso', async ({ page }) => {
    await page.goto('/jobs');
    await page.getByRole('grid', { name: 'Jobverlauf' }).getByRole('row', { name: /SSH-Härtung @ web01/ }).click();
    await expect(page).toHaveURL(/\/jobs\/[0-9a-f-]{36}$/);
    const detail = fenster(page, 'SSH-Härtung @ web01');
    await expect(detail.getByRole('heading', { level: 3, name: 'Ausgabe' })).toBeVisible();
    await expect(detail.getByText('sshd_config.d/60-lcm-hardening.conf geschrieben')).toBeVisible();
    await expect(detail.getByRole('link', { name: 'web01' })).toBeVisible();

    // Der Direktlink öffnet dasselbe Fenster.
    const adresse = page.url();
    await page.goto(adresse);
    await expect(fenster(page, 'SSH-Härtung @ web01').getByRole('heading', { level: 3, name: 'Ausgabe' })).toBeVisible();
  });

  test('Health-Checks sind ausgeblendet und lassen sich einblenden', async ({ page }) => {
    await page.goto('/jobs');
    const verlauf = page.getByRole('grid', { name: 'Jobverlauf' });
    await expect(verlauf.getByRole('row').nth(1)).toBeVisible();
    await expect(verlauf.getByRole('row').filter({ hasText: 'Health-Check' })).toHaveCount(0);
    await setzeSchalter(page.getByRole('switch', { name: 'Health-Checks zeigen' }), true);
    await expect(verlauf.getByRole('row').filter({ hasText: 'Health-Check' }).first()).toBeVisible();
  });
});
