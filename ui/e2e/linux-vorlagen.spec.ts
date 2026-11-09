// Linux-Benutzer: Regelbausteine und Berechtigungsprofile.
import type { Page } from '@playwright/test';
import { test, expect } from './fixtures';
import { bricheAb, fenster } from './helpers';

const bausteine = (page: Page) => page.getByRole('grid').first();
const profile = (page: Page) => page.getByRole('grid').first();

/** Löscht, was die Suche gerade zeigt, Zeile für Zeile über den Löschknopf der Zeile. */
async function loescheAlleTreffer(page: Page): Promise<void> {
  const zeilen = page.getByRole('grid').first().getByRole('row').filter({ has: page.getByRole('button', { name: /löschen$/ }) });
  for (let anzahl = await zeilen.count(); anzahl > 0; anzahl = await zeilen.count()) {
    const zeile = zeilen.first();
    const name = (await zeile.getByRole('gridcell').first().innerText()).split('\n')[0].trim();
    await zeile.getByRole('button', { name: /löschen$/ }).click();
    // Zerstörende Aktionen verlangen den getippten Namen.
    const rueckfrage = page.locator('[open][heading]').last();
    const eingabe = rueckfrage.getByRole('textbox');
    if (await eingabe.waitFor({ state: 'visible', timeout: 1500 }).then(() => true, () => false)) await eingabe.fill(name);
    await rueckfrage.getByRole('button', { name: /löschen$/i }).click();
    await expect(zeilen).toHaveCount(anzahl - 1);
  }
}

const maskiert = (text: string) => text.replace(/[()]/g, '\\$&');

/** Die Zeile mit diesem Namen (nicht die „(Kopie)“ davon); die Zelle trägt zusätzlich „mitgeliefert“, wenn der Eintrag mitgeliefert ist. */
const zeileMit = (tabelle: ReturnType<typeof bausteine>, name: string) =>
  tabelle.getByRole('row').filter({ has: tabelle.page().getByRole('gridcell', { name: new RegExp(`^${maskiert(name)}(?!\\s*[(\\d])`) }) });

test.describe('Regelbausteine', () => {
  test('mitgelieferte sind schreibgeschützt und klonbar, die Kopie zählt durch', async ({ page }) => {
    await page.goto('/linux-users/profile-blocks');
    // Mitgelieferte Bausteine sind da und lassen sich nur über eine Kopie anpassen.
    const apache = zeileMit(bausteine(page), 'Apache betreiben');
    await expect(apache).toContainText('mitgeliefert');
    await expect(apache.getByRole('button', { name: 'Kopie anlegen' })).toBeVisible();

    // Zweimal denselben Baustein kopieren: Der Name muss beim zweiten Mal durchzählen,
    // sonst scheitert genau der Knopf, der der vorgesehene Weg ist, einen Baustein anzupassen.
    await apache.getByRole('button', { name: 'Kopie anlegen' }).click();
    await expect(fenster(page, 'Apache betreiben (Kopie)')).toBeVisible();
    await bricheAb(page, fenster(page, 'Apache betreiben (Kopie)'));
    await apache.getByRole('button', { name: 'Kopie anlegen' }).click();
    await expect(fenster(page, 'Apache betreiben (Kopie) 2')).toBeVisible();
    await bricheAb(page, fenster(page, 'Apache betreiben (Kopie) 2'));

    // Aufräumen: Der Test hinterlässt seine Ausgangslage. Die Suche hält die Zeilen auf einer Seite.
    await page.getByRole('textbox', { name: 'Suche' }).fill('Kopie');
    await loescheAlleTreffer(page);
  });

  test('die Suche grenzt den Katalog ein', async ({ page }) => {
    await page.goto('/linux-users/profile-blocks');
    // Der Katalog deckt die verbreiteten Dienste ab, je Dienst mit den Rollen „betreiben“ und „verwalten“.
    await page.getByRole('textbox', { name: 'Suche' }).fill('nginx');
    await expect(zeileMit(bausteine(page), 'nginx betreiben')).toBeVisible();
    await expect(zeileMit(bausteine(page), 'nginx verwalten')).toBeVisible();
    await page.getByRole('textbox', { name: 'Suche' }).fill('adguard');
    await expect(zeileMit(bausteine(page), 'nginx betreiben')).toHaveCount(0);
    await expect(zeileMit(bausteine(page), 'AdGuard Home betreiben')).toBeVisible();
  });

  test('ein nacktes systemctl wird abgewiesen, mit Unteraktion geht es durch', async ({ page }) => {
    await page.goto('/linux-users/profile-blocks');
    await page.getByRole('button', { name: 'Baustein anlegen' }).click();
    const neu = fenster(page, 'Neuer Regelbaustein');
    const name = `e2e-Baustein ${Date.now().toString(36)}`;
    await neu.getByRole('textbox', { name: 'Name', exact: true }).fill(name);
    const befehle = neu.getByRole('textbox', { name: 'Befehle als root, einer je Zeile' });
    await befehle.fill('/usr/bin/systemctl');
    await neu.getByRole('button', { name: 'Anlegen' }).click();
    await expect(neu.getByText(/feste Argumente/i)).toBeVisible();

    await befehle.fill('/usr/bin/systemctl restart nginx');
    await neu.getByRole('button', { name: 'Anlegen' }).click();
    await expect(fenster(page)).toBeHidden();
    await page.getByRole('textbox', { name: 'Suche' }).fill('e2e-Baustein');
    await expect(page).toHaveURL(/q=e2e-Baustein/);
    await expect(bausteine(page).getByRole('row').filter({ hasText: name })).toBeVisible();

    // Aufräumen.
    await loescheAlleTreffer(page);
  });
});

test.describe('Berechtigungsprofile', () => {
  test('eingebaute sind geschützt, ein eigenes Profil bekommt geprüfte Regeln', async ({ page }) => {
    await page.goto('/linux-users/profiles');
    // Die mitgelieferten Profile bilden den bisherigen Zustand ab und sind schreibgeschützt.
    await expect(zeileMit(profile(page), 'Voll-Administrator')).toContainText('mitgeliefert');
    await expect(zeileMit(profile(page), 'Standardbenutzer')).toContainText('mitgeliefert');

    // Eigenes Profil mit einem Kommando, das nur ohne Argumente gefährlich wäre.
    await page.getByRole('button', { name: 'Profil anlegen' }).click();
    const neu = fenster(page, 'Neues Berechtigungsprofil');
    const name = `e2e-Webserver ${Date.now().toString(36)}`;
    await neu.getByRole('textbox', { name: 'Name', exact: true }).fill(name);
    await neu.getByRole('button', { name: 'Regel hinzufügen' }).first().click();
    const kommando = neu.getByRole('textbox', { name: 'Befehl' }).first();
    await kommando.fill('/usr/bin/systemctl status nginx');
    await neu.getByRole('button', { name: 'Anlegen' }).click();
    const zeile = zeileMit(profile(page), name);
    await expect(zeile).toBeVisible();

    // Der Pager-Schutz wurde beim Speichern ergänzt: Sonst liefe less als root.
    await zeile.getByRole('gridcell').first().click();
    const bearbeiten = fenster(page, name);
    await expect(bearbeiten.getByRole('textbox', { name: 'Befehl' }).first()).toHaveValue('/usr/bin/systemctl --no-pager status nginx');

    // Ein nacktes systemctl wäre Vollzugriff und wird abgewiesen.
    await bearbeiten.getByRole('textbox', { name: 'Befehl' }).first().fill('/usr/bin/systemctl');
    await bearbeiten.getByRole('button', { name: 'Speichern' }).click();
    await expect(bearbeiten.getByText(/feste Argumente/i)).toBeVisible();

    // Aufräumen.
    await bricheAb(page, bearbeiten);
    await loescheAlleTreffer(page);
  });
});
