// Tastaturbedienung: Sprunglink, Kürzel, Hilfe, Fokus nach Seitenwechsel, Pfeiltasten im Menü.
import type { Page } from '@playwright/test';
import { test, expect } from './fixtures';

/** Wo der Fokus gerade steht, auch tief in Shadow-DOMs. */
const fokus = (page: Page) =>
  page.evaluate(() => {
    let element: Element | null = document.activeElement;
    while (element?.shadowRoot?.activeElement) element = element.shadowRoot.activeElement;
    return { tag: element?.tagName ?? '', name: element?.getAttribute('aria-label') ?? (element?.textContent ?? '').trim().slice(0, 40), id: element?.id ?? '' };
  });

test.describe('Tastatur', () => {
  test('der Sprunglink ist das erste Element und führt in den Inhalt', async ({ page }) => {
    await page.goto('/servers');
    await expect(page.getByRole('heading', { level: 1, name: 'Server' })).toBeVisible();
    await page.keyboard.press('Tab');
    expect((await fokus(page)).name).toBe('Zum Inhalt springen');
    await page.keyboard.press('Enter');
    expect((await fokus(page)).id).toBe('main');
  });

  test('? öffnet die Hilfe mit den Kürzeln, Esc schließt sie', async ({ page }) => {
    await page.goto('/servers');
    await expect(page.getByRole('searchbox', { name: 'Name oder Host' })).toBeVisible(); // Serverliste samt Filterleiste steht
    await page.keyboard.press('?');
    const hilfe = page.getByRole('dialog', { name: 'Tastenkürzel' });
    await expect(hilfe.getByRole('heading', { level: 2, name: 'Tastenkürzel' })).toBeVisible();
    await expect(hilfe.getByText(/Kürzel gelten, solange kein Eingabefeld den Fokus hat/)).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(hilfe).toBeHidden();
  });

  test('g j springt zu den Jobs, Titel, Fokus und Ansage folgen', async ({ page }) => {
    await page.goto('/');
    await expect(page.getByRole('heading', { level: 1, name: 'Dashboard' })).toBeVisible();
    await page.keyboard.press('g');
    await page.keyboard.press('j');
    await expect(page).toHaveURL(/\/jobs$/);
    await expect(page).toHaveTitle('Jobs · LCM');
    await expect(page.getByRole('heading', { level: 1, name: 'Jobs' })).toBeVisible();
    expect((await fokus(page)).id).toBe('main');
    // Screenreader hören den neuen Seitentitel.
    await expect(page.getByRole('status').filter({ hasText: 'Jobs · LCM' })).toHaveCount(1);
  });

  test('/ springt ins Suchfeld, im Feld gelten keine Buchstabenkürzel, n löst die Hauptaktion aus', async ({ page }) => {
    await page.goto('/servers');
    const suche = page.getByRole('searchbox', { name: 'Name oder Host' });
    await expect(suche).toBeVisible();
    await page.keyboard.press('/');
    await expect(suche).toBeFocused();
    // Im Feld gelten keine Buchstabenkürzel.
    await page.keyboard.type('n');
    await expect(suche).toHaveValue('n');
    await expect(page).toHaveURL(/\/servers\?q=n$/);
    await page.keyboard.press('Escape');
    await page.locator('main').focus();
    await page.keyboard.press('n');
    await expect(page).toHaveURL(/\/servers\/join$/);
  });

  test('Pfeiltasten wandern durch die Hauptnavigation', async ({ page }) => {
    await page.goto('/');
    const menue = page.getByRole('navigation', { name: 'Hauptmenü' });
    const dashboard = menue.getByRole('link', { name: 'Dashboard', exact: true });
    await dashboard.focus();
    await page.keyboard.press('ArrowRight');
    await expect(menue.getByRole('link', { name: 'Server', exact: true })).toBeFocused();
    await page.keyboard.press('ArrowRight');
    await expect(menue.getByRole('link', { name: 'Gruppen' })).toBeFocused();
    await page.keyboard.press('ArrowLeft');
    await expect(menue.getByRole('link', { name: 'Server', exact: true })).toBeFocused();
  });

  test('Pos1 und Ende springen in der Hauptnavigation zum ersten und letzten Eintrag', async ({ page }) => {
    await page.goto('/');
    const menue = page.getByRole('navigation', { name: 'Hauptmenü' });
    await menue.getByRole('link', { name: 'Gruppen' }).focus();
    await page.keyboard.press('End');
    // Letzter Eintrag der Leiste ist das Kontomenü (rechts außen), erster das Logo.
    await expect(menue.getByRole('button', { name: /^Konto von/ })).toBeFocused();
    await page.keyboard.press('Home');
    await expect(menue.getByRole('link', { name: /^LCM/ })).toBeFocused();
    await page.keyboard.press('ArrowRight');
    await expect(menue.getByRole('link', { name: 'Dashboard', exact: true })).toBeFocused();
  });

  test('Pfeiltasten wechseln in der Serverliste die Zeile', async ({ page }) => {
    await page.goto('/servers');
    const zeilen = page.getByRole('grid', { name: 'Serverliste' }).getByRole('row');
    await expect(zeilen.nth(2)).toBeVisible();
    await zeilen.nth(1).focus();
    await page.keyboard.press('ArrowDown');
    await expect(zeilen.nth(2)).toBeFocused();
    await page.keyboard.press('ArrowUp');
    await expect(zeilen.nth(1)).toBeFocused();
  });

  test('Cmd/Strg+K öffnet die Befehlspalette und springt zu einem Server', async ({ page }) => {
    await page.goto('/');
    await expect(page.getByRole('heading', { level: 1, name: 'Dashboard' })).toBeVisible();
    await page.keyboard.press('ControlOrMeta+k');
    const palette = page.getByRole('dialog', { name: 'Befehlspalette' });
    await expect(palette).toBeVisible();
    await page.keyboard.type('web01');
    // Die Treffer kommen aus der Serverliste und stehen erst nach dem Laden da.
    const treffer = palette.getByRole('option', { name: /web01/ });
    await expect(treffer).toBeVisible();
    await expect(treffer).toHaveAttribute('aria-selected', 'true');
    await page.keyboard.press('Enter');
    await expect(page).toHaveURL(/\/servers\/1$/);
  });
});

test.describe('Ohne Anmeldung', () => {
  test.use({ angemeldet: false });

  // Die Kürzel gehören zur angemeldeten Oberfläche: Auf dem Anmeldebildschirm
  // darf die Taste nichts öffnen, sonst stünde dort eine Hilfe mit fast leerer Sprungliste.
  test('? und g j tun auf der Anmeldeseite nichts', async ({ page }) => {
    await page.goto('/login');
    await expect(page.getByRole('textbox', { name: 'Benutzername' })).toBeVisible();
    await page.locator('main').click({ position: { x: 5, y: 5 } });
    await page.keyboard.press('?');
    await expect(page.getByRole('dialog', { name: 'Tastenkürzel' })).toHaveCount(0);
    await page.keyboard.press('g');
    await page.keyboard.press('j');
    await expect(page).toHaveURL(/\/login$/);
  });
});
