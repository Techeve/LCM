// Einstellungen → Alarme: Regeln, Kanäle, Geltungsbereich.
import { test, expect } from './fixtures';
import { fenster } from './helpers';

const tabelle = (page: import('@playwright/test').Page) => page.getByRole('grid', { name: 'Alarmregeln' });

/** Löscht die Regel über ihr Fenster; die Tests hinterlassen ihre Ausgangslage. */
async function loescheRegel(page: import('@playwright/test').Page, name: string) {
  const zeile = tabelle(page).getByRole('row').filter({ hasText: name });
  await zeile.click();
  await fenster(page, 'Alarmregel bearbeiten').getByRole('button', { name: 'Regel löschen' }).click();
  await fenster(page, `„${name}“ löschen?`).getByRole('button', { name: 'Löschen' }).click();
  await expect(zeile).toHaveCount(0);
}

async function neueRegel(page: import('@playwright/test').Page, name: string) {
  await page.getByRole('button', { name: 'Regel anlegen' }).click();
  const regel = fenster(page, 'Neue Alarmregel');
  await regel.getByRole('textbox', { name: 'Name', exact: true }).fill(name);
  return regel;
}

test.describe('Alarme', () => {
  test('Regeln und Kanalauswahl sind sofort beim Öffnen gefüllt', async ({ page }) => {
    await page.goto('/settings/alerts');
    // Die Demo seedet drei Regeln: Sie stehen direkt da, ohne dass man erst etwas anlegt.
    await expect(tabelle(page).getByRole('row').filter({ hasText: 'Kritische CVEs melden' })).toContainText('Ops-Team (E-Mail)');
    // Beim Anlegen ist der Kanal bereits wählbar.
    await page.getByRole('button', { name: 'Regel anlegen' }).click();
    await expect(fenster(page, 'Neue Alarmregel').getByRole('combobox', { name: 'Benachrichtigungskanal' })).toContainText('Ops-Team (E-Mail)');
  });

  test('der Regeltyp „CVE-Datenbank veraltet“ ist wählbar und erklärt sich', async ({ page }) => {
    await page.goto('/settings/alerts');
    await page.getByRole('button', { name: 'Regel anlegen' }).click();
    const regel = fenster(page, 'Neue Alarmregel');
    await regel.getByRole('combobox', { name: 'Was wird überwacht?' }).selectOption({ label: 'CVE-Datenbank veraltet' });
    // Der Hinweis erklärt, warum es diesen Alarm überhaupt braucht.
    await expect(regel.getByText(/sieht aus wie „alles sicher“/)).toBeVisible();
  });

  test('Regel ohne Kanal protokolliert nur und wird so angelegt und gelöscht', async ({ page }) => {
    await page.goto('/settings/alerts');
    const regel = await neueRegel(page, 'e2e-Nur-Log-Regel');
    // Ohne Kanal landen Alarme nur in der Historie; der Hinweis steht im Formular.
    await expect(regel.getByText('Ohne Kanal landen Alarme nur in der Historie.')).toBeVisible();
    await regel.getByRole('button', { name: 'Anlegen' }).click();
    const zeile = tabelle(page).getByRole('row').filter({ hasText: 'e2e-Nur-Log-Regel' });
    await expect(zeile).toBeVisible();

    // Die Seite warnt, dass diese Regel niemanden erreicht, und nennt die Zahl.
    const warnung = page.getByRole('status').filter({ hasText: 'benachrichtigt niemanden' });
    await expect(warnung).toContainText('1 aktive Regel benachrichtigt niemanden');

    await loescheRegel(page, 'e2e-Nur-Log-Regel');
    await expect(warnung).toHaveCount(0);
  });

  test('eine Regel gilt für mehrere Servergruppen', async ({ page }) => {
    await page.goto('/settings/alerts');
    const regel = await neueRegel(page, 'e2e-Mehrfachgruppen');

    // Voreinstellung ist „alle Server“.
    await expect(regel.getByText('Keine gewählt: Die Regel gilt für alle Server.')).toBeVisible();
    // Zwei Gruppen wählen.
    await regel.getByRole('group', { name: 'Servergruppen' }).getByRole('button', { name: 'Produktion' }).click();
    await regel.getByRole('group', { name: 'Servergruppen' }).getByRole('button', { name: 'System' }).click();
    await expect(regel.getByText('Keine gewählt')).toHaveCount(0);
    await regel.getByRole('button', { name: 'Anlegen' }).click();

    // Beide Gruppen stehen in der Zeile.
    const zeile = tabelle(page).getByRole('row').filter({ hasText: 'e2e-Mehrfachgruppen' });
    await expect(zeile).toContainText('Produktion');
    await expect(zeile).toContainText('System');
    await loescheRegel(page, 'e2e-Mehrfachgruppen');
  });
});
