// Seite „Sicherheit“: CVE-Funde über alle Server, Stand der Schwachstellen-
// Datenbank, Frühwarnung und Zwischenspeicher, dazu die zugehörigen Schalter
// unter Einstellungen → Sicherheit.
import { test, expect } from './fixtures';
import { hinweis, setzeSchalter, speichere, speicherleiste } from './helpers';

// Die Tests eines Workers laufen gegen EINE Instanz: Ein Schalter, den ein
// Test umlegt, gilt auch im nächsten. Jeder Test setzt den Zustand, den er
// braucht, deshalb ausdrücklich selbst.
async function setzeFruehwarnung(page: import('@playwright/test').Page, { ein, lokal = false }: { ein: boolean; lokal?: boolean }) {
  await page.goto('/settings/security');
  const fruehwarnung = page.getByRole('switch', { name: 'Frühwarnung', exact: true });
  await expect(fruehwarnung).toBeVisible();
  const umgelegt = [await setzeSchalter(fruehwarnung, ein)];
  if (ein) umgelegt.push(await setzeSchalter(page.getByRole('switch', { name: 'Lokale Kopie statt Online-Abfrage' }), lokal));
  if (umgelegt.includes(true)) await speichere(page);
}

test.describe('CVE-Funde', () => {
  test('listet CVEs über alle Server, kritischste zuerst', async ({ page }) => {
    await page.goto('/security');
    await expect(page.getByRole('heading', { level: 1, name: 'Sicherheit' })).toBeVisible();
    const funde = page.getByRole('grid', { name: /Sicherheitslücken aller Server/ });
    await expect(funde.getByRole('row').filter({ hasText: 'CVE-2023-0286' })).toContainText('web01');
    await expect(funde.getByRole('row').filter({ hasText: 'rocky01' })).toBeVisible();
    // Der erste Treffer ist der kritische.
    await expect(funde.getByRole('row').nth(1)).toContainText('Kritisch');
    // Das Sammel-Update gehört zur Seite.
    await expect(page.getByRole('button', { name: 'Alle Server aktualisieren' })).toBeVisible();
  });

  test('weist die Herkunft aus und blendet Container-Funde auf Wunsch aus', async ({ page }) => {
    await page.goto('/security');
    const funde = page.getByRole('grid', { name: /Sicherheitslücken aller Server/ });
    // Der Demo-Bestand enthält einen Fund aus einem Container-Image (nginx:1.25).
    await expect(funde.getByRole('columnheader', { name: 'Herkunft' })).toBeVisible();
    await expect(funde.getByRole('gridcell', { name: 'Container nginx:1.25' })).toBeVisible();

    await page.getByRole('combobox', { name: 'Herkunft' }).selectOption({ label: 'Nur Betriebssystem' });
    await expect(funde.getByRole('gridcell', { name: /^Container/ })).toHaveCount(0);
    await expect(funde.getByRole('gridcell', { name: 'System' }).first()).toBeVisible();
  });

  test('nennt den Stand der Schwachstellen-Datenbank über der Fundliste', async ({ page }) => {
    await page.goto('/security');
    // Die e2e-Instanz bekommt ein frisches Datenverzeichnis und damit einen
    // leeren Trivy-Cache: Je nach Maschine ist Trivy gar nicht installiert oder
    // installiert, hat aber noch nie eine Datenbank geladen. Beide Fälle sind
    // ehrlich zu melden, und beide sagen dasselbe: Diese Liste ist keine
    // Entwarnung. Genau darauf prüfen wir.
    await expect(page.getByRole('img', { name: /^Stand der Datenbank/ })).toBeVisible();
    await expect(page.getByText(/Schwachstellen-Datenbank|Datenbank/).first()).toBeVisible();
  });
});

test.describe('Frühwarnung', () => {
  test('ist ein eigener Reiter und sagt, wenn sie aus ist', async ({ page }) => {
    await setzeFruehwarnung(page, { ein: false });
    await page.goto('/security');
    // Standardansicht bleibt der CVE-Scan.
    await expect(page.getByRole('tab', { name: 'CVE-Funde' })).toHaveAttribute('aria-selected', 'true');

    await page.getByRole('tab', { name: 'Frühwarnung' }).click();
    await expect(page).toHaveURL(/\/security\/advisories$/);
    // Ohne eingeschaltete Frühwarnung MUSS das dastehen: Eine leere Liste
    // wäre sonst nicht von „alles sauber“ zu unterscheiden.
    await expect(hinweis(page, 'Es wurde nicht nachgesehen')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Jetzt prüfen' })).toBeDisabled();
  });

  test('der Hinweis verlinkt auf eine Seite, die es gibt', async ({ page }) => {
    await setzeFruehwarnung(page, { ein: false });
    await page.goto('/security/advisories');
    await page.getByRole('link', { name: 'In den Einstellungen einschalten' }).click();
    await expect(page).toHaveURL(/\/settings\/security$/);
    // Beide Blöcke liegen dort: CVE-Scan und Frühwarnung.
    await expect(page.getByRole('switch', { name: 'CVE-Scan' })).toBeVisible();
    await expect(page.getByRole('switch', { name: 'Frühwarnung', exact: true })).toBeVisible();
  });

  test('Zustand, Filter und Durchgang auf Knopfdruck', async ({ page }) => {
    await setzeFruehwarnung(page, { ein: true });
    // Der Zustand steht direkt bei den Einstellungen - ohne ihn wüsste
    // niemand, ob und wann zuletzt geprüft wurde.
    await expect(page.getByRole('definition').filter({ hasText: 'an, Online-Abfrage' })).toBeVisible();

    await page.goto('/security/advisories');
    await expect(hinweis(page, 'ausgeschaltet')).toHaveCount(0);
    // Ohne diesen Knopf passiert nach dem Einschalten bis zu 15 Minuten
    // sichtbar nichts.
    await expect(page.getByRole('button', { name: 'Jetzt prüfen' })).toBeEnabled();
    await expect(page.getByText('Noch nie geprüft')).toBeVisible();
    await expect(page.getByRole('combobox', { name: 'Ab Schwere' })).toBeVisible();
  });

  test('lokale Kopie nennt in den Einstellungen, dass noch nichts gespiegelt wurde', async ({ page }) => {
    await setzeFruehwarnung(page, { ein: true, lokal: true });
    // Ohne Spiegellauf wurde NICHTS geprüft: Die Einstellungen müssen das
    // sagen, sonst wirkte die leere Liste wie ein sauberes Ergebnis.
    await expect(page.getByRole('definition').filter({ hasText: /^noch nie$/ }).first()).toBeVisible();
    await expect(page.getByText('Keine. Erst wenn ein Server seinen Paketbestand gemeldet hat')).toBeVisible();
    await setzeFruehwarnung(page, { ein: false });
  });

  test('lokale Kopie ohne Spiegellauf heißt „noch nie gespiegelt“, nicht „ausgeschaltet“', async ({ page }) => {
    await setzeFruehwarnung(page, { ein: true, lokal: true });
    await page.goto('/security/advisories');
    // Der Server hält die Frühwarnung bis zum ersten Spiegellauf für aus; die Seite nennt den wahren Grund.
    await expect(hinweis(page, 'noch nie gespiegelt')).toBeVisible();
    await expect(hinweis(page, 'ausgeschaltet')).toHaveCount(0);
    await expect(page.getByText('Es wurde noch nichts gespiegelt, also wurde nichts geprüft.')).toBeVisible();
    await setzeFruehwarnung(page, { ein: false });
  });
});

test.describe('Zwischenspeicher', () => {
  test('beide Trefferquoten stehen getrennt, ohne Messwerte als Strich', async ({ page }) => {
    await page.goto('/security/caches');
    // Beide messen Verschiedenes und dürfen nicht zu einer Zahl verschmelzen.
    await expect(page.getByRole('heading', { level: 2, name: 'Scan-Ergebnisse (Trivy)' })).toBeVisible();
    await expect(page.getByRole('heading', { level: 2, name: 'Frühwarn-Abfragen (OSV)' })).toBeVisible();
    // Ohne Messwerte steht ein Strich, NICHT „0 %“: null Zugriffe sind keine
    // schlechte Quote, sondern gar keine.
    await expect(page.getByText('–', { exact: true }).first()).toBeVisible();
    await expect(page.getByText('Noch keine Messwerte').first()).toBeVisible();
    await expect(page.getByText('0 %')).toHaveCount(0);
  });
});

test.describe('Einstellungen → Sicherheit', () => {
  test('zeigt CVE-Scan mit Zeitplan, Frühwarnung mit Hinweis auf die Preisgabe', async ({ page }) => {
    await page.goto('/settings/security');
    await expect(page.getByRole('heading', { level: 2, name: 'CVE-Scan' })).toBeVisible();
    await expect(page.getByRole('switch', { name: 'CVE-Scan' })).toBeVisible();
    await expect(page.getByRole('textbox', { name: 'Zeitplan (Cron)' })).toBeVisible();
    // Die Preisgabe des Paketbestands steht im Klartext über dem Schalter.
    await expect(page.getByText(/geht dein Paketbestand .* an den Dienst OSV/)).toBeVisible();
  });

  test('Frühwarnung einschalten: ein zu kleiner Zwischenspeicher wird am Feld abgewiesen', async ({ page }) => {
    await page.goto('/settings/security');
    await setzeSchalter(page.getByRole('switch', { name: 'Frühwarnung', exact: true }), true);
    // 5 Minuten liegen unter dem Poll-Takt von 15 - ein solcher Eintrag wäre
    // beim nächsten Durchgang längst abgelaufen und würde nur geschrieben, nie
    // gelesen. Die Prüfung der API weist ihn deshalb ab, und die Speicherleiste
    // nennt das Feld.
    const feld = page.getByRole('spinbutton', { name: 'Zwischenspeicher in Minuten' });
    await feld.fill('5');
    await speicherleiste(page).getByRole('button', { name: 'Speichern' }).click();
    await expect(speicherleiste(page).getByRole('alert')).toContainText('„Zwischenspeicher in Minuten“ prüfen');
    await speicherleiste(page).getByRole('button', { name: 'Zum Feld' }).click();
    await expect(feld).toBeFocused();

    await feld.fill('15');
    await speichere(page);
    await page.reload();
    await expect(page.getByRole('switch', { name: 'Frühwarnung', exact: true })).toBeChecked();
    await expect(feld).toHaveValue('15');

    // Der Hinweis auf der Sicherheitsseite verschwindet damit.
    await page.goto('/security/advisories');
    await expect(hinweis(page, 'ausgeschaltet')).toHaveCount(0);
    await setzeFruehwarnung(page, { ein: false });
  });

  test('das Formular ist speicherbar: kein Feld blockiert den Speichern-Knopf', async ({ page }) => {
    await page.goto('/settings/general');
    // Ein einziges ungültiges Feld hätte früher das GESAMTE Formular blockiert,
    // ohne Meldung. Jetzt: Änderung machen, Speichern geht durch.
    const feld = page.getByRole('spinbutton').first();
    await expect(feld).toBeVisible();
    const wert = await feld.inputValue();
    await feld.fill(wert === '30' ? '31' : '30');
    await speichere(page);
    await feld.fill(wert);
    await speichere(page);
  });
});
