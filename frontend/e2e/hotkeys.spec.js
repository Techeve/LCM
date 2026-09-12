// Tastaturbedienung: Sprunglink, Tastenkürzel, Hilfe-Overlay, Abzeichen,
// Fokus nach Seitenwechsel, Pfeiltasten in Menü und Tabelle.
import { test, expect } from './fixtures.js';

test.describe('Tastatur', () => {
  test('Sprunglink ist das erste Element und führt in den Inhalt', async ({ page }) => {
    await page.goto('/#/');
    await expect(page.getByTestId('current-user')).toBeVisible();
    await page.keyboard.press('Tab');
    await expect(page.getByTestId('skip-link')).toBeFocused();
    await page.keyboard.press('Enter');
    await expect(page.locator('main')).toBeFocused();
  });

  test('? öffnet die Hilfe mit Sprüngen und Seitenkürzeln, Esc schließt', async ({ page }) => {
    await page.goto('/#/');
    await expect(page.locator('#f-name')).toBeVisible(); // Serverliste samt Filterleiste steht
    await page.keyboard.press('?');
    const hilfe = page.getByTestId('hotkey-help');
    await expect(hilfe).toBeVisible();
    await expect(hilfe.getByRole('heading', { name: 'Tastenkürzel' })).toBeVisible();
    await expect(hilfe).toContainText('Jobs');
    // Das Dashboard trägt „n" (Server aufnehmen) und „/" (Suche).
    const seite = page.getByTestId('hotkey-page-actions');
    await expect(seite).toContainText('Server hinzufügen');
    await expect(seite.locator('kbd', { hasText: '/' })).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(hilfe).toBeHidden();
  });

  test('g j springt zu den Jobs, Titel und Fokus folgen', async ({ page }) => {
    await page.goto('/#/');
    await expect(page.getByTestId('current-user')).toBeVisible();
    await page.keyboard.press('g');
    await page.keyboard.press('j');
    await expect(page).toHaveURL(/#\/jobs$/);
    await expect(page).toHaveTitle(/^Job-Historie · LCM$/);
    await expect(page.locator('main')).toBeFocused();
    await expect(page.getByTestId('route-announcer')).toHaveText('Job-Historie');
  });

  test('/ springt ins Suchfeld, n löst die Hauptaktion aus, Abzeichen sichtbar', async ({ page }) => {
    await page.goto('/#/');
    const aufnehmen = page.getByRole('link', { name: /Server hinzufügen/ });
    await expect(aufnehmen).toBeVisible();
    await expect(aufnehmen).toHaveAttribute('data-hotkey-label', 'N');
    await expect(page.locator('#f-name')).toBeVisible();
    await page.keyboard.press('/');
    await expect(page.locator('#f-name')).toBeFocused();
    // Im Feld gelten keine Buchstabenkürzel.
    await page.keyboard.type('n');
    await expect(page.locator('#f-name')).toHaveValue('n');
    await expect(page).toHaveURL(/#\/$/);
    await page.keyboard.press('Escape');
    await page.locator('main').focus();
    await page.keyboard.press('n');
    await expect(page).toHaveURL(/#\/servers\/join$/);
  });

  test('Pfeiltasten wandern durch die Hauptnavigation', async ({ page }) => {
    await page.goto('/#/');
    const dashboard = page.getByRole('link', { name: 'Dashboard' });
    await dashboard.focus();
    await page.keyboard.press('ArrowRight');
    await expect(page.getByRole('link', { name: 'Gruppen' })).toBeFocused();
    await page.keyboard.press('End');
    await expect(page.getByRole('link', { name: 'Doku' })).toBeFocused();
    await page.keyboard.press('ArrowRight');
    await expect(dashboard).toBeFocused();
  });

  test('Pfeiltasten wechseln in der Tabelle die Zeile', async ({ page }) => {
    await page.goto('/#/');
    const zeilen = page.locator('main table tbody tr');
    await expect(zeilen.nth(1)).toBeVisible();
    const erster = zeilen.nth(0).locator('a').first();
    await erster.focus();
    await page.keyboard.press('ArrowDown');
    await expect(zeilen.nth(1).locator('a').first()).toBeFocused();
    await page.keyboard.press('ArrowUp');
    await expect(erster).toBeFocused();
  });

  test('Abzeichen lassen sich abschalten', async ({ page }) => {
    await page.goto('/#/');
    await expect(page.getByTestId('current-user')).toBeVisible();
    await page.getByTestId('hotkey-help-button').click();
    await page.getByLabel('Kürzel als Abzeichen an Schaltflächen anzeigen').uncheck();
    await expect(page.locator('html')).toHaveAttribute('data-hotkey-badges', 'off');
    await page.getByLabel('Kürzel als Abzeichen an Schaltflächen anzeigen').check();
    await expect(page.locator('html')).toHaveAttribute('data-hotkey-badges', 'on');
  });
});
