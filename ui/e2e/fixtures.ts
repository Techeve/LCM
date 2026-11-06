// Gemeinsame Test-Grundlage: angemeldete Sitzung ohne Formular-Login.
//
// Das serverseitige Passwort-Hashing ist absichtlich langsam; bei jedem Test
// über das Formular anzumelden würde den größten Teil der Testzeit kosten.
// Stattdessen: einmal pro Worker über die API anmelden und die beiden Cookies
// der Sitzung (`__Host-lcm_session`, httpOnly, und `__Host-lcm_csrf`) in jeden
// Browser-Kontext legen. Der Anmeldeweg SELBST bleibt geprüft: Die Tests, die ihn
// zum Gegenstand haben, schalten die Sitzung mit `angemeldet: false` ab.
//
// Die Suite nutzt die Cookie-Variante der Anmeldung: Die Arbeitsspeicher-Variante
// (Standard) meldet bei jedem `page.goto` und `page.reload` ab. Cookie-Sitzungen
// gibt es nur über HTTPS; deshalb gibt sich jede Anfrage als HTTPS aus
// (`X-Forwarded-Proto`, siehe playwright.config.ts), und der Testserver vertraut
// 127.0.0.1 als Proxy (run-server.sh). Die Fixture stellt jede Instanz auf „cookie“
// und bestätigt die Wahl, damit die einmalige Frage nach den Pflichtschritten nicht
// jeden Test unterbricht. `anmeldung-varianten.spec.ts` schaltet um und stellt zurück;
// eine Sitzung, die dabei endet, holt der Kontext sich neu.
import { test as base, expect, type APIRequestContext, type Cookie, type PlaywrightWorkerArgs } from '@playwright/test';

type Playwright = PlaywrightWorkerArgs['playwright'];

export const ADMIN_PASSWORD = 'e2e-admin-passwort';

// Jeder Worker bekommt seine eigene Server-Instanz auf einem eigenen Port
// (siehe webServer in playwright.config.ts). Die Adresse hängt deshalb am
// Worker-Index und nicht an der globalen baseURL.
const BASIS_PORT = Number(process.env.LCM_E2E_PORT || 8090);
const SCHEMA = process.env.LCM_E2E_TLS === '1' ? 'https' : 'http';
export const adresseFuer = (index: number): string => `${SCHEMA}://127.0.0.1:${BASIS_PORT + index}`;

interface Optionen {
  /** `false` für Tests des Anmeldewegs: der Browser kommt ohne Sitzung. */
  angemeldet: boolean;
}

/** Jede Anfrage der Tests gibt sich als HTTPS aus (siehe oben). */
export const ALS_HTTPS = { 'X-Forwarded-Proto': 'https' };

/** Ein API-Zugang ohne Browser; `https: false` lässt den Kopf weg. */
export function apiKontext(playwright: Playwright, baseURL: string, https = true): Promise<APIRequestContext> {
  return playwright.request.newContext({ baseURL, ignoreHTTPSErrors: true, extraHTTPHeaders: https ? ALS_HTTPS : {} });
}

/** Meldet als Admin an. Die Antwort trägt `session_token` (Arbeitsspeicher) oder `csrf_token` (Cookie). */
async function meldeAnPerApi(api: APIRequestContext): Promise<{ session_token?: string; csrf_token?: string }> {
  const antwort = await api.post('/api/v1/auth/login', { data: { username: 'admin', password: ADMIN_PASSWORD } });
  if (!antwort.ok()) throw new Error(`Anmeldung für die Test-Sitzung fehlgeschlagen: HTTP ${antwort.status()}`);
  return antwort.json();
}

/**
 * Stellt die Einstellung der Instanz über die API (als Admin) auf einen Weg der Anmeldung und bestätigt sie.
 * Das beendet alle Sitzungen, wenn der Weg wechselt.
 */
export async function stelleAnmeldungEin(
  playwright: Playwright,
  baseURL: string,
  modus: 'memory' | 'cookie',
  bestaetigt = true,
): Promise<void> {
  const api = await apiKontext(playwright, baseURL);
  const login = await meldeAnPerApi(api);
  const kopf: Record<string, string> = login.session_token ? { 'X-LCM-Session': login.session_token } : { 'X-CSRF-Token': login.csrf_token ?? '' };
  const antwort = await api.patch('/api/v1/settings/global', {
    headers: kopf,
    data: { browser_session_mode: modus, browser_session_mode_confirmed: bestaetigt },
  });
  if (!antwort.ok()) throw new Error(`Anmeldeweg einstellen fehlgeschlagen: HTTP ${antwort.status()}`);
  await api.dispose();
}

/** Eine frische Cookie-Sitzung des Admins; stellt die Instanz dafür bei Bedarf auf „cookie“. */
async function cookieSitzung(playwright: Playwright, baseURL: string): Promise<Cookie[]> {
  await stelleAnmeldungEin(playwright, baseURL, 'cookie');
  const api = await apiKontext(playwright, baseURL);
  const login = await meldeAnPerApi(api);
  if (login.session_token) throw new Error('Die Instanz steht nicht auf „cookie“, obwohl sie dafür eingestellt wurde.');
  const { cookies } = await api.storageState();
  await api.dispose();
  return cookies;
}

/** Lebt die Sitzung dieser Cookies noch? */
async function lebt(playwright: Playwright, baseURL: string, cookies: Cookie[]): Promise<boolean> {
  const api = await playwright.request.newContext({ baseURL, ignoreHTTPSErrors: true, extraHTTPHeaders: ALS_HTTPS, storageState: { cookies, origins: [] } });
  const antwort = await api.get('/api/v1/auth/me');
  await api.dispose();
  return antwort.ok();
}

/** Die Sitzung eines Workers; sie wird ersetzt, wenn ein Test sie beendet hat. */
class Sitzung {
  cookies: Cookie[] = [];
  constructor(
    private readonly playwright: Playwright,
    private readonly baseURL: string,
  ) {}

  async bereit(): Promise<Cookie[]> {
    if (this.cookies.length === 0 || !(await lebt(this.playwright, this.baseURL, this.cookies))) {
      this.cookies = await cookieSitzung(this.playwright, this.baseURL);
    }
    return this.cookies;
  }
}

interface WorkerFixtures {
  sitzung: Sitzung;
}

export const test = base.extend<Optionen, WorkerFixtures>({
  angemeldet: [true, { option: true }],

  // Die Tests navigieren über baseURL, und die muss zum Server DIESES Workers
  // zeigen, nicht zum ersten.
  baseURL: async ({}, use, testInfo) => {
    await use(adresseFuer(testInfo.parallelIndex));
  },

  sitzung: [
    async ({ playwright }, use, workerInfo) => {
      const sitzung = new Sitzung(playwright, adresseFuer(workerInfo.parallelIndex));
      await sitzung.bereit(); // stellt die Instanz auch für die Tests ein, die ohne Sitzung starten
      await use(sitzung);
    },
    { scope: 'worker' },
  ],

  context: async ({ context, sitzung, angemeldet }, use) => {
    if (angemeldet) await context.addCookies(await sitzung.bereit());
    await use(context);
  },
});

export { expect };
