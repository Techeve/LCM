// Gemeinsame Test-Grundlage: angemeldete Sitzung ohne Formular-Login.
//
// Warum: 99 der 108 Tests riefen loginAsAdmin() auf und durchliefen dabei
// jedes Mal den vollständigen Anmeldeweg - Seite laden, Formular ausfüllen,
// serverseitiges Passwort-Hashing abwarten, Seitenwechsel. Das Hashing ist
// absichtlich langsam; 99-mal dasselbe Ergebnis zu erzeugen kostete rund die
// Hälfte der Testzeit.
//
// Stattdessen: einmal pro Worker über die API anmelden und das Ergebnis vor
// jedem Seitenaufruf in localStorage legen - dort sucht der ApiClient es
// ohnehin (siehe src/api/client.svelte.js). Der Anmeldeweg SELBST bleibt
// geprüft: Die Tests, die ihn zum Gegenstand haben, melden sich weiterhin
// über das Formular an.
import { test as base, expect } from '@playwright/test';

export const ADMIN_PASSWORD = 'e2e-admin-passwort';

// Muss zu TOKEN_KEY/USER_KEY in src/api/client.svelte.js passen.
const TOKEN_KEY = 'lcm.token';
const USER_KEY = 'lcm.user';

// Jeder Worker bekommt seine eigene Server-Instanz auf einem eigenen Port
// (siehe webServer in playwright.config.js). Die Adresse haengt deshalb am
// Worker-Index und nicht an der globalen baseURL - eine Worker-Fixture darf
// auf letztere ohnehin nicht zugreifen.
export const BASIS_PORT = Number(process.env.LCM_E2E_PORT || 8090);
export const adresseFuer = (index) => `http://127.0.0.1:${BASIS_PORT + index}`;

export const test = base.extend({
  // Tests, die den Anmeldeweg SELBST pruefen, schalten die Sitzung ab:
  //   test.describe('Anmeldung', () => { test.use({ angemeldet: false }); ... })
  angemeldet: [true, { option: true }],

  // Worker-weit: eine Anmeldung je Playwright-Prozess, nicht je Test.
  // Die Tests navigieren ueber baseURL - die muss zum Server DIESES Workers
  // zeigen, nicht zum ersten.
  baseURL: async ({}, use, testInfo) => {
    await use(adresseFuer(testInfo.parallelIndex));
  },

  sitzung: [
    async ({ playwright }, use, workerInfo) => {
      const api = await playwright.request.newContext({
        baseURL: adresseFuer(workerInfo.parallelIndex),
        ignoreHTTPSErrors: true,
      });
      const antwort = await api.post('/api/v1/auth/login', {
        data: { username: 'admin', password: ADMIN_PASSWORD },
      });
      if (!antwort.ok()) {
        throw new Error(`Anmeldung für die Test-Sitzung fehlgeschlagen: HTTP ${antwort.status()}`);
      }
      const { token, user } = await antwort.json();
      await api.dispose();
      await use({ token, user });
    },
    { scope: 'worker' },
  ],

  // addInitScript läuft VOR dem Seiten-Skript - der ApiClient findet die
  // Sitzung also schon beim Hochfahren vor und rendert direkt angemeldet.
  page: async ({ page, sitzung, angemeldet }, use) => {
    if (!angemeldet) {
      await use(page);
      return;
    }
    await page.addInitScript(
      ([schluesselToken, schluesselUser, token, user]) => {
        localStorage.setItem(schluesselToken, token);
        localStorage.setItem(schluesselUser, user);
      },
      [TOKEN_KEY, USER_KEY, sitzung.token, JSON.stringify(sitzung.user)],
    );
    await use(page);
  },
});

export { expect };
