// Barrierefreiheit: axe-core prüft die wichtigsten Seiten in beiden Farbmodi gegen WCAG 2.1 AA:
// Kontraste, Beschriftungen, Landmarken, Überschriften-Reihenfolge, Formularfelder. Ein neuer
// Verstoß fällt hier im Testlauf auf, nicht erst beim Anwender mit Screenreader.
import AxeBuilder from '@axe-core/playwright';
import type { Page } from '@playwright/test';
import { test, expect } from './fixtures';

const SEITEN: Array<[string, string]> = [
  ['/', 'Dashboard'],
  ['/servers', 'Serverliste'],
  ['/servers/1', 'Serverdetail'],
  ['/servers/1/security', 'Serverdetail Sicherheit'],
  ['/servers/1/packages', 'Serverdetail Pakete'],
  ['/servers/join', 'Server aufnehmen'],
  ['/groups/2', 'Gruppen'],
  ['/jobs', 'Jobs'],
  ['/linux-users', 'Linux-Benutzer'],
  ['/security', 'Sicherheit'],
  ['/security/advisories', 'Sicherheit Frühwarnung'],
  ['/security/caches', 'Sicherheit Zwischenspeicher'],
  ['/containers', 'Container'],
  ['/account', 'Mein Konto'],
  ['/settings/general', 'Einstellungen allgemein'],
  ['/settings/users', 'Einstellungen Benutzer'],
  ['/settings/backups', 'Einstellungen Backups'],
  ['/settings/security', 'Einstellungen Sicherheit'],
  ['/settings/alerts', 'Einstellungen Alarme'],
  ['/settings/events', 'Einstellungen Ereignisse'],
  ['/settings/crowdsec', 'Einstellungen CrowdSec'],
  ['/settings/apt-cache', 'Einstellungen APT-Cache'],
  ['/settings/allowlists', 'Einstellungen Allowlists'],
  ['/settings/notifications', 'Einstellungen Benachrichtigungen'],
  ['/settings/schedules', 'Einstellungen Zeitpläne'],
  ['/settings/repositories', 'Einstellungen Paketquellen'],
  ['/docs', 'Dokumentation'],
];

async function pruefe(page: Page, pfad: string, thema: 'light' | 'dark', abwarten: () => Promise<void>) {
  await page.addInitScript((wahl) => localStorage.setItem('lcm.theme', wahl), thema);
  await page.goto(pfad);
  await abwarten();
  // Die Seiten laden Daten nach: kurz warten, bis Tabellen stehen.
  await page.waitForLoadState('networkidle').catch(() => undefined);
  const ergebnis = await new AxeBuilder({ page })
    .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'best-practice'])
    .analyze();
  const zeilen = ergebnis.violations.map(
    (verstoss) =>
      `${verstoss.id} [${verstoss.impact}] ${verstoss.help}\n` +
      verstoss.nodes
        .slice(0, 6)
        .map((knoten) => `    ${knoten.target.join(' ')}\n      ${knoten.failureSummary?.split('\n').slice(1, 3).join(' | ')}`)
        .join('\n'),
  );
  expect(zeilen, `${pfad} (${thema})`).toEqual([]);
}

for (const [pfad, name] of SEITEN) {
  for (const thema of ['light', 'dark'] as const) {
    test(`Barrierefreiheit: ${name} (${thema})`, async ({ page }) => {
      await pruefe(page, pfad, thema, () => expect(page.getByRole('main')).toBeVisible({ timeout: 15_000 }));
    });
  }
}

test.describe('Anmeldung', () => {
  test.use({ angemeldet: false });
  for (const thema of ['light', 'dark'] as const) {
    test(`Barrierefreiheit: Anmeldung (${thema})`, async ({ page }) => {
      await pruefe(page, '/login', thema, () => expect(page.getByRole('form', { name: 'Anmelden' })).toBeVisible());
    });
  }
});
