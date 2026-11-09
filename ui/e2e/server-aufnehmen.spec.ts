// Server aufnehmen: Assistent für SSH, Agent, RouterOS und Synology DSM.
import { test, expect } from './fixtures';
import { hinweis, waehle } from './helpers';

const name = (page: import('@playwright/test').Page) => page.getByRole('textbox', { name: 'Name', exact: true });

test.describe('Server aufnehmen', () => {
  test('warnt sofort, wenn Adresse und Port bereits angelegt sind', async ({ page }) => {
    await page.goto('/servers/join');
    await name(page).fill('web01-nochmal');
    // web01 hat im Demo den Host 10.10.0.11 auf Port 22: sofortige Warnung.
    await page.getByRole('textbox', { name: 'Hostname oder IP' }).fill('10.10.0.11');
    await expect(hinweis(page, 'Diese Adresse mit diesem Port ist schon angelegt')).toContainText('web01');
    await expect(page.getByRole('button', { name: /Weiter: Host-Schlüssel prüfen/ })).toBeDisabled();

    // Anderer Host: keine Warnung mehr.
    await page.getByRole('textbox', { name: 'Hostname oder IP' }).fill('10.10.9.250');
    await expect(hinweis(page, 'schon angelegt')).toHaveCount(0);
  });

  test('bietet Passwort- und System-Schlüssel-Anmeldung und die Anleitung für den Root-Login', async ({ page }) => {
    await page.goto('/servers/join');
    // Standard: Passwortfeld sichtbar.
    await expect(page.getByRole('radio', { name: 'Passwort' })).toBeChecked();
    await expect(page.getByRole('textbox', { name: 'Passwort', exact: true })).toBeVisible();

    // Auf den System-SSH-Schlüssel umschalten: Passwortfeld weg.
    await waehle(page, 'System-SSH-Schlüssel');
    await expect(page.getByRole('textbox', { name: 'Passwort', exact: true })).toHaveCount(0);

    // Aufklappbare Anleitung „Root-Login“: erst zu, nach dem Aufklappen stehen die Befehle drin.
    const anleitung = page.locator('details').filter({ hasText: 'Root-Login per SSH ist gesperrt?' });
    const befehl = anleitung.locator('code', { hasText: 'PermitRootLogin yes' });
    await expect(befehl).toBeHidden();
    await anleitung.locator('summary').click();
    await expect(befehl).toBeVisible();
    await expect(anleitung.getByText(/Root-Login sperren/)).toBeVisible();
  });

  test('Synology DSM: eigenes Formular, Weiter erst bei vollständiger Eingabe', async ({ page }) => {
    await page.goto('/servers/join');
    await waehle(page, /Synology DSM/);
    // Eigenes Formular: DSM-Port statt SSH-Port, Konto statt Dienstbenutzer, und
    // der Hinweis, warum kein Konto mit erzwungener 2FA taugt.
    await expect(page.getByRole('textbox', { name: 'DSM-Port' })).toHaveValue('5001');
    await expect(page.getByRole('textbox', { name: 'DSM-Konto' })).toBeVisible();
    await expect(page.getByText(/ohne erzwungene Zwei-Faktor-Anmeldung/)).toBeVisible();

    // Ohne vollständige Eingabe geht es nicht weiter: Das Zertifikat wird geholt,
    // BEVOR Zugangsdaten übertragen werden. Was fehlt, steht an den Feldern.
    const weiter = page.getByRole('button', { name: /Weiter: Zertifikat prüfen/ });
    await weiter.click();
    await expect(page.getByText('Bitte ausfüllen.').first()).toBeVisible();
    await expect(page.getByRole('textbox', { name: 'DSM-Konto' })).toHaveAttribute('aria-invalid', 'true');
    await expect(weiter).toBeVisible();
  });

  test('Neu verbinden öffnet den Assistenten mit vorbelegten Daten', async ({ page }) => {
    await page.goto('/servers/1');
    await page.getByRole('button', { name: 'Aktionen' }).click();
    await page.getByRole('menuitem', { name: 'Neu verbinden' }).click();
    await expect(page).toHaveURL(/\/servers\/join\?reconnect=1/);
    await expect(page.getByRole('heading', { level: 1, name: 'Server neu verbinden' })).toBeVisible();
    await expect(page.getByText('Stellt die Verbindung zu web01 neu her')).toBeVisible();
    await expect(page.getByRole('textbox', { name: 'Hostname oder IP' })).toHaveValue('10.10.0.11');
    await expect(page.getByRole('textbox', { name: 'SSH-Port' })).toHaveValue('22');
  });
});
