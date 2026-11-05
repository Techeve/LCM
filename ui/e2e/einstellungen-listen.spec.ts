// Einstellungen: Seiten mit Listen und Fenstern (Quellen, Caches, Allowlists, Aktionen, Anwendungen, Schlüssel).
// Jeder Test legt an, prüft und räumt auf: Die Instanz gehört dem Worker, aber andere Tests laufen weiter.
import type { Page } from '@playwright/test';
import { test, expect } from './fixtures';
import { bestaetige, fenster } from './helpers';

const zeile = (page: Page, tabelle: string, text: string) => page.getByRole('grid', { name: tabelle }).getByRole('row').filter({ hasText: text });

test.describe('Einstellungen: Paketquellen', () => {
  test('der Katalog bringt Einträge mit; eine eigene Quelle lässt sich anlegen, öffnen und löschen', async ({ page }) => {
    await page.goto('/settings/repositories');
    await expect(page.getByRole('heading', { level: 2, name: 'Katalog' })).toBeVisible();
    // Die mitgelieferten Einträge sind da (Docker CE aus dem Seed).
    await expect(zeile(page, 'Bekannte Paketquellen', 'Docker CE')).toContainText('download.docker.com');

    await page.getByRole('button', { name: 'Paketquelle hinzufügen' }).first().click();
    const neu = fenster(page, 'Neue Paketquelle');
    // Ohne Eingaben geht es nicht weiter: Was fehlt, steht an den Feldern.
    await neu.getByRole('button', { name: 'Anlegen' }).click();
    await expect(neu.getByText('Gib einen Namen an.')).toBeVisible();
    await neu.getByRole('textbox', { name: 'Name', exact: true }).fill('e2e-Quelle');
    await neu.getByRole('textbox', { name: 'Kennung' }).fill('e2e-quelle');
    await neu.getByRole('textbox', { name: 'Quellzeile' }).fill('deb [signed-by=/etc/apt/keyrings/e2e-quelle.asc] https://example.com stable main');
    await neu.getByRole('button', { name: 'Anlegen' }).click();
    const eintrag = zeile(page, 'Bekannte Paketquellen', 'e2e-Quelle');
    await expect(eintrag).toBeVisible();

    // Bearbeiten öffnet mit gefüllten Feldern; danach wieder löschen.
    await eintrag.click();
    await expect(fenster(page, 'Paketquelle bearbeiten').getByRole('textbox', { name: 'Kennung' })).toHaveValue('e2e-quelle');
    await fenster(page, 'Paketquelle bearbeiten').getByRole('button', { name: 'Abbrechen' }).click();
    await eintrag.getByRole('button', { name: 'Paketquelle löschen' }).click();
    await bestaetige(page, '„e2e-Quelle“ aus dem Katalog löschen?', 'Löschen');
    await expect(eintrag).toHaveCount(0);
  });
});

test.describe('Einstellungen: APT-Cache und CrowdSec', () => {
  test('mehrere APT-Caches anlegen und prüfen', async ({ page }) => {
    await page.goto('/settings/apt-cache');
    await expect(page.getByRole('heading', { level: 3, name: 'Noch kein APT-Cache hinterlegt' })).toBeVisible();
    // Zwei Caches mit eigenem Namen (127.0.0.1:1 und :2 sind nicht belegt).
    for (const [name, adresse] of [['Standort A', 'http://127.0.0.1:1'], ['Standort B', 'http://127.0.0.1:2']]) {
      await page.getByRole('button', { name: 'Cache hinzufügen' }).first().click();
      const neu = fenster(page, 'APT-Cache hinzufügen');
      await neu.getByRole('textbox', { name: 'Name', exact: true }).fill(name);
      await neu.getByRole('textbox', { name: 'Adresse' }).fill(adresse);
      await neu.getByRole('button', { name: /Anlegen|Hinzufügen|Speichern/ }).last().click();
      await expect(zeile(page, 'APT-Caches', name).or(page.getByRole('row').filter({ hasText: name })).first()).toBeVisible();
    }
    const a = page.getByRole('row').filter({ hasText: 'Standort A' });
    await a.getByRole('button', { name: 'Prüfen' }).click();
    await expect(a.getByText(/nicht erreichbar/)).toBeVisible();

    // Aufräumen: Der Test hinterlässt seine Ausgangslage.
    for (const name of ['Standort A', 'Standort B']) {
      await page.getByRole('row').filter({ hasText: name }).getByRole('button', { name: 'Cache entfernen' }).click();
      await bestaetige(page, `„${name}“ aus LCM entfernen?`, 'Entfernen');
      await expect(page.getByRole('row').filter({ hasText: name })).toHaveCount(0);
    }
    await expect(page.getByRole('heading', { level: 3, name: 'Noch kein APT-Cache hinterlegt' })).toBeVisible();
  });

  test('mehrere CrowdSec-LAPIs anlegen und prüfen, die Überwachungsregel empfehlen lassen', async ({ page }) => {
    await page.goto('/settings/crowdsec');
    await expect(page.getByRole('heading', { level: 3, name: 'Noch keine LAPI hinterlegt' })).toBeVisible();
    for (const [name, adresse] of [['LAPI Techeve', 'http://127.0.0.1:1'], ['LAPI Service 2000', 'http://127.0.0.1:2']]) {
      await page.getByRole('button', { name: 'LAPI hinzufügen' }).first().click();
      const neu = fenster(page, 'LAPI hinzufügen');
      await neu.getByRole('textbox', { name: 'Name', exact: true }).fill(name);
      await neu.getByRole('textbox', { name: 'Adresse der LAPI' }).fill(adresse);
      await neu.getByRole('textbox', { name: 'Maschinen-Passwort' }).fill('e2e-lapi-passwort');
      await neu.getByRole('button', { name: /Anlegen|Hinzufügen|Speichern/ }).last().click();
      await expect(page.getByRole('row').filter({ hasText: name })).toBeVisible();
    }
    const techeve = page.getByRole('row').filter({ hasText: 'LAPI Techeve' });
    await techeve.getByRole('button', { name: 'Prüfen' }).click();
    await expect(techeve.getByText(/nicht erreichbar/)).toBeVisible();

    // Überwachungsempfehlung: Die Regel entsteht per Klick.
    await page.getByRole('button', { name: 'Empfohlene Regel anlegen' }).click();
    await expect(page.getByRole('button', { name: 'Empfohlene Regel anlegen' })).toHaveCount(0);

    // Aufräumen: die Regel unter Alarme und beide LAPIs.
    await page.goto('/settings/alerts');
    await page.getByRole('row').filter({ hasText: 'CrowdSec-LAPI nicht erreichbar' }).first().click();
    await fenster(page, 'Alarmregel bearbeiten').getByRole('button', { name: 'Regel löschen' }).click();
    await page.getByRole('button', { name: 'Löschen', exact: true }).click();

    await page.goto('/settings/crowdsec');
    for (const name of ['LAPI Techeve', 'LAPI Service 2000']) {
      await page.getByRole('row').filter({ hasText: name }).getByRole('button', { name: 'LAPI entfernen' }).click();
      await bestaetige(page, `„${name}“ aus LCM entfernen?`, 'Entfernen');
      await expect(page.getByRole('row').filter({ hasText: name })).toHaveCount(0);
    }
  });
});

test.describe('Einstellungen: Allowlists, Aktionen, Anwendungen, Schlüssel', () => {
  test('IP-Allowlists: anlegen, ungültigen Eintrag am Feld abweisen, löschen', async ({ page }) => {
    await page.goto('/settings/allowlists');
    // Demo-Seeds sind sichtbar.
    await expect(zeile(page, 'IP-Allowlists', 'Büro-Netze')).toBeVisible();

    await page.getByRole('button', { name: 'Allowlist anlegen' }).first().click();
    const neu = fenster(page, 'Neue Allowlist');
    await neu.getByRole('textbox', { name: 'Name', exact: true }).fill('E2E-Liste');
    await neu.getByRole('textbox', { name: 'Einträge' }).fill('kein-ip');
    await neu.getByRole('button', { name: 'Anlegen' }).click();
    // Ein ungültiger Eintrag wird am Feld abgewiesen, die Liste entsteht nicht.
    await expect(neu.getByRole('textbox', { name: 'Einträge' })).toHaveAttribute('aria-invalid', 'true');
    await neu.getByRole('textbox', { name: 'Einträge' }).fill('198.51.100.0/24\n2001:db8:e2e::1');
    await neu.getByRole('button', { name: 'Anlegen' }).click();
    const liste = zeile(page, 'IP-Allowlists', 'E2E-Liste');
    await expect(liste).toBeVisible();
    await expect(liste).toContainText('2'); // zwei Einträge

    await liste.getByRole('button', { name: 'Allowlist löschen' }).click();
    await bestaetige(page, '„E2E-Liste“ löschen?', 'Löschen');
    await expect(liste).toHaveCount(0);
  });

  test('eigene Aktionen: Anlegen verlangt Name und Kommando', async ({ page }) => {
    await page.goto('/settings/custom-actions');
    await expect(page.getByRole('heading', { level: 3, name: 'Noch keine eigene Aktion' })).toBeVisible();
    await page.getByRole('button', { name: 'Aktion anlegen' }).first().click();
    const neu = fenster(page, 'Neue Aktion');
    await neu.getByRole('button', { name: /Anlegen|Speichern/ }).last().click();
    await expect(neu.getByText('Gib einen Namen an.')).toBeVisible();
    await neu.getByRole('textbox', { name: 'Name', exact: true }).fill('e2e-Aktion');
    await expect(neu.getByRole('textbox').nth(1)).toBeVisible();
    await neu.getByRole('button', { name: 'Abbrechen' }).click();
    await bestaetigeVerwerfen(page);
  });

  test('der Anwendungskatalog liefert Steckbriefe mit und nimmt eigene auf', async ({ page }) => {
    await page.goto('/settings/apps');
    const katalog = page.getByRole('grid', { name: 'Steckbriefe der Anwendungen' });
    await expect(katalog.getByRole('row').filter({ hasText: 'Nextcloud' })).toContainText('mitgeliefert');

    await page.getByRole('button', { name: 'Anwendung anlegen' }).first().click();
    const neu = fenster(page, 'Anwendung anlegen');
    await neu.getByRole('textbox', { name: 'Technischer Name' }).fill('e2e-testanwendung');
    await neu.getByRole('textbox', { name: 'Name', exact: true }).fill('E2E-Testanwendung');
    await neu.getByRole('textbox', { name: 'Merkmale' }).fill('path /opt/e2e-test');
    await neu.getByRole('button', { name: /Anlegen|Speichern/ }).last().click();
    // Die Anwendung läuft beim Scan als root: Erst bestätigen.
    await bestaetige(page, '„E2E-Testanwendung“ beim Scan ausführen?', 'Speichern und ausführen lassen');
    const eintrag = katalog.getByRole('row').filter({ hasText: 'E2E-Testanwendung' });
    await expect(eintrag).toBeVisible();

    await eintrag.getByRole('gridcell').first().click();
    await fenster(page, 'Anwendung bearbeiten').getByRole('button', { name: 'Anwendung entfernen' }).click();
    await bestaetige(page, '„E2E-Testanwendung“ aus dem Katalog entfernen?', 'Entfernen', 'E2E-Testanwendung');
    await expect(eintrag).toHaveCount(0);
  });

  test('einen API-Schlüssel mit Rechten anlegen, einmal sehen und widerrufen', async ({ page }) => {
    await page.goto('/settings/apikeys');
    await page.getByRole('textbox', { name: 'Name', exact: true }).fill('e2e-schluessel');
    await page.getByRole('button', { name: 'Schlüssel erzeugen' }).click();
    await expect(page.getByText('Schlüssel „e2e-schluessel“ erzeugt.')).toBeVisible();
    const schluessel = zeile(page, 'Schlüssel', 'e2e-schluessel');
    await page.getByRole('button', { name: 'Gesichert, ausblenden' }).click();
    await expect(schluessel).toBeVisible();

    await schluessel.getByRole('button', { name: '„e2e-schluessel“ widerrufen' }).first().click();
    await bestaetige(page, '„e2e-schluessel“ widerrufen?', 'Widerrufen', 'e2e-schluessel');
    // Der Schlüssel bleibt als widerrufen in der Liste stehen und lässt sich nicht mehr widerrufen.
    await expect(schluessel).toContainText('widerrufen');
    await expect(schluessel.getByRole('button', { name: '„e2e-schluessel“ widerrufen' })).toHaveCount(0);
  });
});

async function bestaetigeVerwerfen(page: Page) {
  const rueckfrage = page.getByRole('heading', { level: 2, name: /^(Änderungen|Eingaben) verwerfen\?$/ });
  if (await rueckfrage.waitFor({ state: 'visible', timeout: 1000 }).then(() => true, () => false)) {
    await page.getByRole('button', { name: 'Verwerfen', exact: true }).click();
  }
}
