// Barrierefreiheit: axe-core prüft die wichtigsten Seiten in beiden
// Farbmodi gegen WCAG 2.1 AA - Kontraste, Beschriftungen, Landmarken,
// Überschriften-Reihenfolge, Formularfelder. Ein neuer Verstoß fällt hier
// im Testlauf auf, nicht erst beim Anwender mit Screenreader.
import AxeBuilder from '@axe-core/playwright';
import { test, expect } from './fixtures.js';

const SEITEN = [
  ['/#/', 'Dashboard'],
  ['/#/servers/1', 'Server-Detail'],
  ['/#/servers/join', 'Server aufnehmen'],
  ['/#/groups', 'Gruppen'],
  ['/#/jobs', 'Jobs'],
  ['/#/linux-users', 'Linux-Benutzer'],
  ['/#/security', 'Sicherheit'],
  ['/#/docker', 'Docker'],
  ['/#/account', 'Mein Konto'],
  ['/#/settings/general', 'Einstellungen allgemein'],
  ['/#/settings/users', 'Einstellungen Benutzer'],
  ['/#/settings/backups', 'Einstellungen Backups'],
  ['/#/settings/security', 'Einstellungen Sicherheit'],
  ['/#/settings/alerts', 'Einstellungen Alarme'],
  ['/#/doku', 'Dokumentation'],
];

async function pruefe(page, pfad, thema) {
  await page.addInitScript((t) => localStorage.setItem('lcm.theme', t), thema);
  await page.goto(pfad);
  await expect(page.locator('main h1, main h2, main .card').first()).toBeVisible({ timeout: 15_000 });
  // Die Seiten laden Daten nach - kurz warten, bis Tabellen stehen.
  await page.waitForLoadState('networkidle').catch(() => {});
  const ergebnis = await new AxeBuilder({ page })
    .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'best-practice'])
    .analyze();
  const zeilen = ergebnis.violations.map(
    (v) =>
      `${v.id} [${v.impact}] ${v.help}\n` +
      v.nodes
        .slice(0, 6)
        .map((n) => `    ${n.target.join(' ')}\n      ${n.failureSummary?.split('\n').slice(1, 3).join(' | ')}`)
        .join('\n'),
  );
  expect(zeilen, `${pfad} (${thema})\n${zeilen.join('\n')}`).toEqual([]);
}

for (const [pfad, name] of SEITEN) {
  for (const thema of ['light', 'dark']) {
    test(`Barrierefreiheit: ${name} (${thema})`, async ({ page }) => {
      await pruefe(page, pfad, thema);
    });
  }
}

test.describe('Anmeldung', () => {
  test.use({ angemeldet: false });
  for (const thema of ['light', 'dark']) {
    test(`Barrierefreiheit: Login (${thema})`, async ({ page }) => {
      await pruefe(page, '/#/login', thema);
    });
  }
});
