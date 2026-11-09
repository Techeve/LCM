// Einstellungen → Benutzer: LCM-Konten, Passwortprüfung.
import { test, expect } from './fixtures';
import { bestaetige, fenster } from './helpers';

const konten = (page: import('@playwright/test').Page) => page.getByRole('grid', { name: 'LCM-Konten' });

async function oeffneNeuesKonto(page: import('@playwright/test').Page, name: string) {
  await page.goto('/settings/users');
  await page.getByRole('button', { name: 'Konto anlegen' }).click();
  const neu = fenster(page, 'Neues Konto');
  await neu.getByRole('textbox', { name: 'Benutzername' }).fill(name);
  return neu;
}

test.describe('Einstellungen: Benutzer', () => {
  test('zeigt die Konten des Demo-Bestands', async ({ page }) => {
    await page.goto('/settings/users');
    await expect(konten(page).getByRole('row').filter({ hasText: 'system' }).first()).toBeVisible();
    await expect(konten(page).getByRole('row').filter({ hasText: 'admin' }).first()).toContainText('du');
  });

  test('Passwortwiederholung muss übereinstimmen', async ({ page }) => {
    const neu = await oeffneNeuesKonto(page, 'e2e-mgr');
    await neu.getByRole('textbox', { name: 'Passwort', exact: true }).fill('Anker5-Leuchtturm!Wind');
    await neu.getByRole('textbox', { name: 'Passwort wiederholen' }).fill('passt-nicht');
    await neu.getByRole('button', { name: 'Anlegen' }).click();
    await expect(neu.getByText('Die Passwörter stimmen nicht überein.')).toBeVisible();
    await expect(neu.getByRole('textbox', { name: 'Passwort wiederholen' })).toHaveAttribute('aria-invalid', 'true');
    // Es wurde nichts angelegt.
    await expect(fenster(page, 'Neues Konto')).toBeVisible();
  });

  test('die Oberfläche nennt den Grund eines schwachen Passworts und sagt nicht „Stark“', async ({ page }) => {
    const neu = await oeffneNeuesKonto(page, 'e2e-policy');
    const passwort = neu.getByRole('textbox', { name: 'Passwort', exact: true });
    const wiederholung = neu.getByRole('textbox', { name: 'Passwort wiederholen' });

    // Standard-Passwort: schwach, mit Grund, und das Anlegen wird nicht abgeschickt.
    await passwort.fill('Passwort123!');
    await wiederholung.fill('Passwort123!');
    await expect(neu.getByText('Bekanntes Standard-Passwort')).toBeVisible();
    await expect(neu.getByText('Stark', { exact: true })).toHaveCount(0);
    await expect(neu.getByText('Schwach', { exact: true })).toBeVisible();
    await neu.getByRole('button', { name: 'Anlegen' }).click();
    await expect(neu.getByRole('alert').filter({ hasText: 'erfüllt die Regeln noch nicht' })).toBeVisible();

    // Enthält den eigenen Benutzernamen: ebenfalls schwach, mit Grund.
    await passwort.fill('Nordwind-e2e-policy-9!');
    await expect(neu.getByText('Enthält deinen Benutzernamen')).toBeVisible();
    await expect(neu.getByText('Sehr stark')).toHaveCount(0);
    await expect(neu.getByText('Stark', { exact: true })).toHaveCount(0);
    await expect(fenster(page, 'Neues Konto')).toBeVisible();
  });

  test('die API nennt die verletzten Passwortregeln in den Parametern', async ({ page }) => {
    await page.goto('/settings/users');
    const antwort = await page.evaluate(async () => {
      const csrf = decodeURIComponent(document.cookie.split('; ').find((c) => c.startsWith('__Host-lcm_csrf='))?.slice(16) ?? '');
      const res = await fetch('/api/v1/users', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
        body: JSON.stringify({ username: 'e2e-api-policy', password: 'Passwort123!', roles: [] }),
      });
      return { status: res.status, body: await res.json() };
    });
    expect(antwort.status).toBe(422);
    expect(antwort.body.code).toBe('password.too_weak');
    expect(antwort.body.params.problems).toContain('common_password');
  });

  test('ein starkes Passwort legt das Konto an; Löschen räumt auf', async ({ page }) => {
    const neu = await oeffneNeuesKonto(page, 'e2e-mgr');
    await neu.getByRole('textbox', { name: 'Passwort', exact: true }).fill('Fjord4-Muschel!Wandel');
    await neu.getByRole('textbox', { name: 'Passwort wiederholen' }).fill('Fjord4-Muschel!Wandel');
    await expect(neu.getByText('Sehr stark')).toBeVisible();
    await neu.getByRole('button', { name: 'Anlegen' }).click();
    const zeile = konten(page).getByRole('row').filter({ hasText: 'e2e-mgr' });
    await expect(zeile).toBeVisible();

    // Aufräumen: Der Test hinterlässt seine Ausgangslage.
    await zeile.getByRole('button', { name: 'Weitere Aktionen für e2e-mgr' }).click();
    await page.getByRole('menuitem', { name: 'Löschen' }).click();
    await bestaetige(page, 'e2e-mgr löschen?', 'Endgültig löschen', 'e2e-mgr');
    await expect(zeile).toHaveCount(0);
  });
});
