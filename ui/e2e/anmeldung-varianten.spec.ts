// Die zwei Wege der Browser-Anmeldung: Arbeitsspeicher (Standard) und Sitzungs-Cookie. Die übrige Suite
// läuft auf der Cookie-Variante (siehe fixtures.ts); hier wird je Test auf „memory“ gestellt und danach
// zurück auf „cookie“, damit die Instanz für die nächsten Tests wieder stimmt.
import type { Page, Response } from '@playwright/test';
import { test, expect, ADMIN_PASSWORD, apiKontext, stelleAnmeldungEin } from './fixtures';
import { fenster, hinweis } from './helpers';

test.use({ angemeldet: false });

// `sitzung` steht in jedem Haken, damit die Fixture des Workers die Instanz einstellt, BEVOR ein Haken sie umstellt.
test.afterEach(async ({ sitzung: _sitzung, playwright, baseURL }) => {
  await stelleAnmeldungEin(playwright, baseURL!, 'cookie');
});

const istLogin = (antwort: Response) => antwort.url().endsWith('/api/v1/auth/login') && antwort.request().method() === 'POST';

/** Meldet über das Formular an und liefert den Sitzungswert aus der Antwort (nur Arbeitsspeicher-Variante). */
async function meldeAn(page: Page): Promise<string> {
  const antwort = page.waitForResponse(istLogin);
  await page.getByRole('textbox', { name: 'Benutzername' }).fill('admin');
  await page.getByRole('textbox', { name: 'Passwort' }).fill(ADMIN_PASSWORD);
  await page.getByRole('button', { name: 'Anmelden' }).click();
  const body = await (await antwort).json();
  return body.session_token ?? '';
}

/** Öffnet die Seite von außen (`goto`, in der Arbeitsspeicher-Variante also ohne Sitzung) und meldet an. */
async function oeffneUndMeldeAn(page: Page, pfad: string): Promise<string> {
  await page.goto(pfad);
  await expect(page.getByRole('form', { name: 'Anmelden' })).toBeVisible();
  return meldeAn(page);
}

const konto = (page: Page) => page.getByRole('button', { name: 'Konto von Administrator' });

/** Läuft die Sitzung mit diesem Wert auf dem Server noch? */
async function sitzungLebt(playwright: Parameters<typeof apiKontext>[0], baseURL: string, wert: string): Promise<boolean> {
  const api = await apiKontext(playwright, baseURL);
  const antwort = await api.get('/api/v1/auth/me', { headers: { 'X-LCM-Session': wert } });
  await api.dispose();
  return antwort.ok();
}

test.describe('Arbeitsspeicher-Variante', () => {
  test.beforeEach(async ({ sitzung: _sitzung, playwright, baseURL }) => {
    await stelleAnmeldungEin(playwright, baseURL!, 'memory');
  });

  test('Anmelden setzt keine Cookies, legt nichts im Browser ab und verbindet den Ereigniskanal', async ({ page, context }) => {
    await page.goto('/login');
    // Von Anfang an mitlesen: Das Hallo des Servers kann vor dem ersten await eintreffen.
    const adressen: string[] = [];
    const rahmen: string[] = [];
    page.on('websocket', (ws) => {
      if (!ws.url().includes('/api/v1/events')) return;
      adressen.push(ws.url());
      ws.on('framereceived', (frame) => rahmen.push(String(frame.payload)));
    });
    const wert = await meldeAn(page);
    await expect(konto(page)).toBeVisible();
    expect(wert).not.toBe('');

    // Verbunden heißt: Der Server hat das Hallo geschickt, mit einer Fahrkarte statt eines Sitzungswerts in der Adresse.
    await expect.poll(() => rahmen.some((frame) => frame.includes('"hello"'))).toBe(true);
    expect(adressen[0]).toMatch(/\/api\/v1\/events\?ticket=/);
    expect(adressen[0]).not.toContain(wert);

    expect(await context.cookies()).toEqual([]);
    const abgelegt = await page.evaluate(async () => ({
      local: JSON.stringify({ ...localStorage }),
      session: JSON.stringify({ ...sessionStorage }),
      datenbanken: JSON.stringify((await indexedDB.databases?.()) ?? []),
      caches: JSON.stringify(await caches.keys()),
      cookie: document.cookie,
    }));
    for (const [ort, inhalt] of Object.entries(abgelegt)) expect(inhalt, ort).not.toContain(wert);
  });

  test('Neuladen führt zur Anmeldung und danach zurück zur Adresse', async ({ page }) => {
    await oeffneUndMeldeAn(page, '/servers');
    await expect(page.getByRole('grid', { name: 'Serverliste' })).toBeVisible();

    await page.reload();
    await expect(page.getByRole('form', { name: 'Anmelden' })).toBeVisible();
    await expect(page).toHaveURL(/\/login\?next=%2Fservers/);
    await meldeAn(page);
    await expect(page).toHaveURL(/\/servers$/);
    await expect(page.getByRole('grid', { name: 'Serverliste' })).toBeVisible();
  });

  test('ein neuer Tab ist abgemeldet, der erste bleibt angemeldet', async ({ page, context }) => {
    await oeffneUndMeldeAn(page, '/');
    await expect(konto(page)).toBeVisible();

    const zweiter = await context.newPage();
    await zweiter.goto('/servers');
    await expect(zweiter.getByRole('form', { name: 'Anmelden' })).toBeVisible();

    // Auch nach der Anmeldung im zweiten Tab behalten beide ihre eigene Sitzung.
    await meldeAn(zweiter);
    await expect(zweiter.getByRole('grid', { name: 'Serverliste' })).toBeVisible();
    await zweiter.close();

    await page.getByRole('link', { name: 'Server', exact: true }).click();
    await expect(page.getByRole('grid', { name: 'Serverliste' })).toBeVisible();
    await expect(konto(page)).toBeVisible();
  });

  test('Abmelden in einem Tab lässt den anderen angemeldet', async ({ page, context }) => {
    await oeffneUndMeldeAn(page, '/');
    const zweiter = await context.newPage();
    await zweiter.goto('/');
    await meldeAn(zweiter);
    await expect(konto(zweiter)).toBeVisible();

    await konto(zweiter).click();
    await zweiter.getByRole('menuitem', { name: 'Abmelden' }).click();
    await expect(zweiter.getByRole('form', { name: 'Anmelden' })).toBeVisible();

    await page.getByRole('link', { name: 'Server', exact: true }).click();
    await expect(page.getByRole('grid', { name: 'Serverliste' })).toBeVisible();
  });

  test('Schließen des Tabs beendet die Sitzung auf dem Server', async ({ page, playwright, baseURL }) => {
    const wert = await oeffneUndMeldeAn(page, '/');
    await expect(konto(page)).toBeVisible();
    expect(await sitzungLebt(playwright, baseURL!, wert)).toBe(true);

    await page.close();
    await expect.poll(() => sitzungLebt(playwright, baseURL!, wert), { timeout: 10_000 }).toBe(false);
  });

  test('Abmelden über das Menü beendet die Sitzung auf dem Server', async ({ page, playwright, baseURL }) => {
    const wert = await oeffneUndMeldeAn(page, '/');
    await konto(page).click();
    await page.getByRole('menuitem', { name: 'Abmelden' }).click();
    await expect(page.getByRole('form', { name: 'Anmelden' })).toBeVisible();
    expect(await sitzungLebt(playwright, baseURL!, wert)).toBe(false);
  });

  test('der Sitzungswert steht in keiner Adresse, der Kanal verbindet mit einer Fahrkarte', async ({ page }) => {
    const adressen: string[] = [];
    page.on('request', (anfrage) => adressen.push(anfrage.url()));
    page.on('websocket', (ws) => adressen.push(ws.url()));
    const wert = await oeffneUndMeldeAn(page, '/');
    await expect(konto(page)).toBeVisible();
    await expect.poll(() => adressen.some((url) => url.includes('/api/v1/events?ticket='))).toBe(true);
    expect(adressen.filter((url) => url.includes(wert))).toEqual([]);
  });
});

test.describe('Cookie-Variante', () => {
  test('Anmelden setzt __Host-Cookies mit den vorgeschriebenen Attributen; das Neuladen bleibt angemeldet', async ({ page, context }) => {
    await page.goto('/login');
    await meldeAn(page);
    await expect(konto(page)).toBeVisible();

    const cookies = await context.cookies();
    const sitzung = cookies.find((c) => c.name === '__Host-lcm_session');
    const csrf = cookies.find((c) => c.name === '__Host-lcm_csrf');
    expect(sitzung).toMatchObject({ httpOnly: true, secure: true, sameSite: 'Strict', path: '/' });
    expect(csrf).toMatchObject({ httpOnly: false, secure: true, sameSite: 'Strict', path: '/' });
    expect(cookies.map((c) => c.name).sort()).toEqual(['__Host-lcm_csrf', '__Host-lcm_session']);

    await page.reload();
    await expect(konto(page)).toBeVisible();
    const zweiter = await context.newPage();
    await zweiter.goto('/');
    await expect(konto(zweiter)).toBeVisible();
  });
});

test.describe('Wahl der Variante', () => {
  test.beforeEach(async ({ sitzung: _sitzung, playwright, baseURL }) => {
    await stelleAnmeldungEin(playwright, baseURL!, 'memory');
  });

  async function zuAllgemein(page: Page): Promise<void> {
    await page.getByRole('link', { name: 'Einstellungen' }).click();
    await expect(page).toHaveURL(/\/settings\/general$/);
  }

  test('die Karte erklärt beide Wege und der Wechsel meldet alle ab', async ({ page, context }) => {
    await oeffneUndMeldeAn(page, '/');
    await zuAllgemein(page);

    const karte = page.locator('main lcm-card', { has: page.getByRole('heading', { level: 2, name: 'Anmeldung im Browser' }) });
    // Die Karte steht bei der öffentlichen Adresse und trägt auch die Sitzungsdauer.
    await expect(karte.getByRole('heading', { level: 2 })).toBeVisible();
    const titel = await page.locator('main').getByRole('heading', { level: 2 }).allTextContents();
    expect(titel.indexOf('Anmeldung im Browser')).toBe(titel.indexOf('Öffentliche Adresse') + 1);
    await expect(karte.getByLabel(/Sitzungsdauer/)).toBeVisible();
    const arbeitsspeicher = karte.getByRole('radio', { name: /Nur im Arbeitsspeicher/ });
    const cookie = karte.getByRole('radio', { name: /Sitzungs-Cookie/ });
    await expect(arbeitsspeicher).toBeChecked();
    await expect(cookie).toBeEnabled();
    await expect(karte).toContainText('Die Anmeldung gilt nur für diesen einen Tab');
    await expect(karte).toContainText('Die Anmeldung gilt für alle Tabs dieses Browsers');
    await expect(karte).toContainText('Beim Wechsel werden alle Benutzer abgemeldet.');
    await expect(karte).not.toContainText('Nur verfügbar, wenn LCM über HTTPS aufgerufen wird.');

    await cookie.check({ force: true });
    await page.getByRole('region', { name: 'Änderungen speichern' }).getByRole('button', { name: 'Speichern' }).click();

    // Alle Sitzungen enden, auch diese: Anmeldeseite mit Erklärung, danach geht es mit dem Cookie weiter.
    await expect(page.getByRole('form', { name: 'Anmelden' })).toBeVisible();
    await expect(hinweis(page, 'Die Anmeldung im Browser wurde umgestellt')).toBeVisible();
    await expect(page).toHaveURL(/\/login\?next=%2Fsettings%2Fgeneral&reason=session-mode/);
    await meldeAn(page);
    await expect(page).toHaveURL(/\/settings\/general$/);
    expect((await context.cookies()).map((c) => c.name)).toContain('__Host-lcm_session');
  });

  test('über HTTP ist die Cookie-Option gesperrt und sagt warum', async ({ browser, baseURL }) => {
    const kontext = await browser.newContext({ baseURL, locale: 'de-DE', extraHTTPHeaders: { 'X-Forwarded-Proto': 'http' } });
    const seite = await kontext.newPage();
    await oeffneUndMeldeAn(seite, '/');
    await zuAllgemein(seite);

    const karte = seite.locator('main lcm-card', { has: seite.getByRole('heading', { level: 2, name: 'Anmeldung im Browser' }) });
    await expect(karte.getByRole('radio', { name: /Sitzungs-Cookie/ })).toBeDisabled();
    await expect(karte).toContainText('Nur verfügbar, wenn LCM über HTTPS aufgerufen wird.');
    await expect(karte.getByRole('radio', { name: /Nur im Arbeitsspeicher/ })).toBeChecked();
    await kontext.close();
  });

  test('die einmalige Frage erscheint nach der Anmeldung, bleibt der Weg gleich, bleibt die Sitzung', async ({ page, playwright, baseURL }) => {
    await stelleAnmeldungEin(playwright, baseURL!, 'memory', false);
    await oeffneUndMeldeAn(page, '/');

    const frage = fenster(page, 'Wie soll die Anmeldung im Browser funktionieren?');
    await expect(frage).toBeVisible();
    await expect(frage.getByRole('radio', { name: /Nur im Arbeitsspeicher/ })).toBeChecked();
    await frage.getByRole('button', { name: 'Übernehmen' }).click();
    await expect(frage).toBeHidden();
    // Kein Abmelden: Die Sitzung läuft weiter und die Oberfläche bleibt benutzbar.
    await expect(konto(page)).toBeVisible();
    await page.getByRole('link', { name: 'Server', exact: true }).click();
    await expect(page.getByRole('grid', { name: 'Serverliste' })).toBeVisible();

    // Bestätigt gespeichert: Die nächste Anmeldung fragt nicht mehr.
    const api = await apiKontext(playwright, baseURL!);
    const login = await (await api.post('/api/v1/auth/login', { data: { username: 'admin', password: ADMIN_PASSWORD } })).json();
    const einstellungen = await (await api.get('/api/v1/settings/global', { headers: { 'X-LCM-Session': login.session_token } })).json();
    expect(einstellungen.browser_session_mode_confirmed).toBe(true);
    expect(einstellungen.browser_session_mode).toBe('memory');
    await api.dispose();
  });

  test('wer die Frage ohne Wahl schließt, wird bei der nächsten Anmeldung wieder gefragt', async ({ page, playwright, baseURL }) => {
    await stelleAnmeldungEin(playwright, baseURL!, 'memory', false);
    await oeffneUndMeldeAn(page, '/');
    const frage = fenster(page, 'Wie soll die Anmeldung im Browser funktionieren?');
    await expect(frage).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(frage).toBeHidden();
    await expect(konto(page)).toBeVisible();

    await konto(page).click();
    await page.getByRole('menuitem', { name: 'Abmelden' }).click();
    await expect(page.getByRole('form', { name: 'Anmelden' })).toBeVisible();
    await meldeAn(page);
    await expect(frage).toBeVisible();
  });

  test('die Wahl „Sitzungs-Cookie“ in der Frage meldet ab und gilt danach', async ({ page, playwright, baseURL, context }) => {
    await stelleAnmeldungEin(playwright, baseURL!, 'memory', false);
    await oeffneUndMeldeAn(page, '/');
    const frage = fenster(page, 'Wie soll die Anmeldung im Browser funktionieren?');
    await frage.getByRole('radio', { name: /Sitzungs-Cookie/ }).check({ force: true });
    await frage.getByRole('button', { name: 'Übernehmen' }).click();

    await expect(page.getByRole('form', { name: 'Anmelden' })).toBeVisible();
    await expect(hinweis(page, 'Die Anmeldung im Browser wurde umgestellt')).toBeVisible();
    await meldeAn(page);
    await expect(konto(page)).toBeVisible();
    expect((await context.cookies()).map((c) => c.name)).toContain('__Host-lcm_session');
    await page.reload();
    await expect(konto(page)).toBeVisible();
  });
});
