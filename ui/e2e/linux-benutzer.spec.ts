// Linux-Benutzer: Liste, Reiter, Anlegen, Verwalten, Schlüsselpaar.
import { test, expect } from './fixtures';
import { bestaetige, fenster } from './helpers';

test.describe('Linux-Benutzer', () => {
  test('zeigt die Demo-Konten', async ({ page }) => {
    await page.goto('/linux-users');
    await expect(page.getByRole('heading', { level: 1, name: 'Linux-Benutzer' })).toBeVisible();
    const tabelle = page.getByRole('grid', { name: 'Linux-Benutzer' });
    await expect(tabelle.getByRole('row').filter({ hasText: 'deploy' })).toContainText('Voll-Administrator');
    await expect(tabelle.getByRole('row').filter({ hasText: 'mmuster' })).toContainText('Produktion');
  });

  test('die Suche grenzt die Liste ein', async ({ page }) => {
    await page.goto('/linux-users');
    await page.getByRole('textbox', { name: 'Suche' }).fill('deploy');
    const tabelle = page.getByRole('grid', { name: 'Linux-Benutzer' });
    await expect(tabelle.getByRole('row').filter({ hasText: 'deploy' })).toBeVisible();
    await expect(tabelle.getByRole('row').filter({ hasText: 'mmuster' })).toHaveCount(0);
  });

  test('Reiter führen zu Profilen und Bausteinen, alte Lesezeichen landen dort', async ({ page }) => {
    await page.goto('/linux-users');
    await expect(page.getByRole('tab', { name: 'Benutzer' })).toHaveAttribute('aria-selected', 'true');

    await page.getByRole('tab', { name: 'Berechtigungsprofile' }).click();
    await expect(page).toHaveURL(/\/linux-users\/profiles$/);
    await expect(page.getByRole('grid').filter({ hasText: 'Voll-Administrator' })).toBeVisible();

    await page.getByRole('tab', { name: 'Regelbausteine' }).click();
    await expect(page).toHaveURL(/\/linux-users\/profile-blocks$/);
    await page.getByRole('textbox', { name: 'Suche' }).fill('Systemd');
    await expect(page.getByRole('grid', { name: 'Regelbausteine' }).getByText('Systemd-Dienst betreiben', { exact: true })).toBeVisible();

    // Alte Lesezeichen aus den Einstellungen landen auf dem neuen Reiter.
    await page.goto('/settings/profiles');
    await expect(page).toHaveURL(/\/linux-users\/profiles$/);
  });

  test('Anlegen läuft über ein Fenster, der Benutzer steht danach in der Tabelle', async ({ page }) => {
    await page.goto('/linux-users');
    await page.getByRole('button', { name: 'Linux-Benutzer anlegen' }).click();
    const fensterNeu = fenster(page, 'Neuer Linux-Benutzer');
    await fensterNeu.getByRole('textbox', { name: 'Benutzername' }).fill('e2e-linux');
    await fensterNeu.getByRole('button', { name: 'Anlegen' }).click();
    // Der neue Benutzer steht in der Tabelle; das Fenster bleibt für Zugang und Gruppen offen.
    const zeile = page.getByRole('row', { name: /^e2e-linux –/ });
    await expect(zeile).toBeVisible();
    const verwalten = fenster(page, 'e2e-linux');
    await expect(verwalten).toBeVisible();

    // Aufräumen: Der Test hinterlässt seine Ausgangslage.
    await verwalten.getByRole('button', { name: 'Benutzer löschen' }).click();
    await bestaetige(page, '„e2e-linux“ löschen?', 'Endgültig löschen', 'e2e-linux');
    await expect(zeile).toHaveCount(0);
  });

  test('Löschen ist gesperrt, solange der Benutzer Servern zugeordnet ist', async ({ page }) => {
    await page.goto('/linux-users');
    // deploy ist über die Demo-Gruppe „Produktion“ auf Servern eingerichtet.
    await page.getByRole('row', { name: /deploy/ }).click();
    const verwalten = fenster(page, 'deploy');
    await expect(verwalten.getByRole('button', { name: 'Von allen Servern nehmen' })).toBeVisible();
    await expect(verwalten.getByRole('button', { name: 'Benutzer löschen' })).toBeDisabled();
  });

  test('ein erzeugtes Schlüsselpaar zeigt den privaten Schlüssel einmalig zum Herunterladen', async ({ page }) => {
    await page.goto('/linux-users');
    await page.getByRole('row', { name: /deploy/ }).click();
    await fenster(page, 'deploy').getByRole('button', { name: 'Schlüsselpaar erzeugen' }).click();
    await fenster(page, 'Schlüsselpaar erzeugen').getByRole('button', { name: 'Erzeugen' }).click();

    // Das Fenster mit dem privaten Schlüssel erscheint über dem Verwalten-Fenster.
    const schluessel = fenster(page, 'Privater Schlüssel für deploy');
    await expect(schluessel).toBeVisible();
    const download = page.waitForEvent('download');
    await schluessel.getByRole('button', { name: 'Herunterladen' }).click();
    expect((await download).suggestedFilename()).toBe('id_ed25519_deploy');
    // Die Windows-Anleitung richtet den Schlüssel als Standardschlüssel ein, sodass
    // „ssh deploy@<server>“ ohne -i genügt.
    await expect(schluessel.getByText('Host *')).toBeVisible();
  });
});
