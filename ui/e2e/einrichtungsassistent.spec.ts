// Einrichtungsassistent nach Passwort und 2FA (`/setup`) und die Einführung auf der Seite des LCM-Hosts.
//
// Der Schrittstand (`setup_step`) liegt auf dem Server; das Demo-Binary startet aber nie mit offenem Assistenten und
// lässt ihn nicht von Hand wieder öffnen. Deshalb gibt `richteAssistentEin` den Stand über `/auth/me` und
// `/settings/global` vor und führt ihn bei jedem PATCH weiter; alles andere (Netzbereiche, Let's Encrypt, Serverliste)
// kommt vom echten Binary. Das Backend prüft die Reihenfolge der Schritte in seinen eigenen Tests.
import { test, expect } from './fixtures';
import { hinweis, waehle } from './helpers';
import type { Page } from '@playwright/test';

type Koerper = Record<string, unknown>;

interface Assistent {
  /** Alle PATCH-Körper an `/settings/global`, in der Reihenfolge. */
  patches: Koerper[];
  /** Alle Körper an den Zeitplan der Backups. */
  zeitplaene: Koerper[];
}

/**
 * Stellt den Assistenten auf `start` und führt ihn mit den PATCH-Körpern weiter. `neueAdresse` ist die Adresse der
 * Oberfläche, die der Server meldet; ohne Angabe die der Seite selbst (`localhost`, also ohne Namen). `mitName`: Schritt 2
 * hat diese Adresse als Namen eingetragen (`public_base_url`), nur dann leitet der Assistent dorthin weiter.
 */
async function richteAssistentEin(page: Page, start: string, neueAdresse?: string, mitName = false): Promise<Assistent> {
  const stand: Koerper = {};
  let schritt = start;
  const assistent: Assistent = { patches: [], zeitplaene: [] };

  await page.route('**/api/v1/auth/me', async (route) => {
    const antwort = await route.fetch();
    await route.fulfill({ response: antwort, json: { ...(await antwort.json()), setup_step: schritt } });
  });
  await page.route('**/api/v1/settings/global', async (route) => {
    const anfrage = route.request();
    if (anfrage.method() === 'PATCH') {
      const koerper = anfrage.postDataJSON() as Koerper;
      assistent.patches.push(koerper);
      if ('setup_step' in koerper) schritt = String(koerper.setup_step);
      Object.assign(stand, Object.fromEntries(Object.entries(koerper).filter(([schluessel]) => schluessel !== 'setup_step')));
    }
    // Der echte Stand kommt per GET, auch nach einem PATCH: Dessen Körper bliebe sonst am Aufruf hängen.
    // Endet der Test mitten in der Anfrage, ist die Antwort schon verworfen: Dann gibt es nichts mehr zu liefern.
    try {
      const echt = (await (await page.context().request.get('/api/v1/settings/global')).json()) as Koerper;
      const adresse = neueAdresse ?? new URL(page.url()).origin;
      const name = mitName ? { public_base_url: adresse } : {};
      await route.fulfill({ json: { ...echt, ...stand, ...name, setup_step: schritt, effective_urls: { ui: adresse, agent: '', mcp: '' } } });
    } catch {
      await route.abort().catch(() => undefined);
    }
  });
  await page.route('**/api/v1/system/backups/settings', async (route) => {
    assistent.zeitplaene.push(route.request().postDataJSON() as Koerper);
    await route.fulfill({ json: {} });
  });
  return assistent;
}

const weiter = (page: Page) => page.getByRole('button', { name: 'Speichern und weiter' });
const fortschritt = (page: Page) => page.locator('[data-field="progress"]');

test.describe('Einrichtungsassistent', () => {
  test('führt Schritt für Schritt bis zur Einführung auf der Seite des LCM-Hosts', async ({ page }) => {
    const assistent = await richteAssistentEin(page, 'session');
    await page.goto('/');

    // Jede Seite führt zum Assistenten, solange ein Schritt offen ist; das Fenster zur Anmeldung geht nicht auf.
    await expect(page).toHaveURL(/\/setup$/);
    await expect(fortschritt(page)).toContainText('Schritt 1 von 10');
    await expect(page.getByRole('heading', { level: 1, name: 'Anmeldung im Browser' })).toBeVisible();
    await expect(page.getByRole('dialog')).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Zurück' })).toHaveCount(0);
    await weiter(page).click();

    await expect(fortschritt(page)).toContainText('Schritt 2 von 10');
    await expect(page.getByRole('heading', { level: 1, name: 'Wie ist LCM erreichbar?' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Überspringen' })).toHaveCount(0);
    await expect(page.getByRole('textbox', { name: 'Name der Weboberfläche' })).toBeVisible();
    await weiter(page).click();

    await expect(page.getByRole('heading', { level: 1, name: 'Welche Schnittstellen von LCM willst du verwenden?' })).toBeVisible();
    await expect(page.getByRole('switch', { name: 'Agenten-Schnittstelle' })).toBeVisible();
    await expect(page.getByRole('switch', { name: 'MCP-Schnittstelle' })).toBeVisible();
    await weiter(page).click();

    await expect(page.getByRole('heading', { level: 1, name: 'Wer darf LCM erreichen?' })).toBeVisible();
    await expect(page.locator('lcm-network-group').first()).toBeVisible();
    await weiter(page).click();

    await expect(page.getByRole('heading', { level: 1, name: 'Zertifikat' })).toBeVisible();
    await expect(page.getByText('Ohne Let\'s Encrypt und ohne hochgeladenes Zertifikat liefert LCM sein eigenes')).toBeVisible();
    await weiter(page).click();

    await expect(page.getByRole('heading', { level: 1, name: 'Neue Server aufnehmen' })).toBeVisible();
    await expect(page.getByText('Du kannst das später in den Einstellungen ändern.')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Überspringen' })).toHaveCount(0);
    await weiter(page).click();

    await expect(fortschritt(page)).toContainText('Schritt 7 von 10');
    await expect(page.getByRole('heading', { level: 1, name: 'E-Mail-Versand' })).toBeVisible();
    await expect(page.getByText('Du kannst das später unter Einstellungen → Allgemein einrichten oder ändern.')).toBeVisible();
    await page.getByRole('button', { name: 'Überspringen' }).click();

    await expect(fortschritt(page)).toContainText('Schritt 8 von 10');
    await expect(page.getByRole('heading', { level: 1, name: 'Aufbewahrung' })).toBeVisible();
    await expect(page.getByText('Jeder Job ist eine Verbindung von LCM zu einem Server')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Überspringen' })).toHaveCount(0);
    await weiter(page).click();

    await expect(fortschritt(page)).toContainText('Schritt 9 von 10');
    await expect(page.getByRole('heading', { level: 1, name: 'Automatisches Backup' })).toBeVisible();
    await expect(page.getByText('Bei jedem Update, das du über LCM startest, legt LCM vorher automatisch ein Backup an.')).toBeVisible();
    await page.getByRole('button', { name: 'Überspringen' }).click();

    await expect(fortschritt(page)).toContainText('Schritt 10 von 10');
    await expect(page.getByRole('heading', { level: 1, name: 'Subscription und Paketkanal' })).toBeVisible();
    await expect(page.getByRole('textbox', { name: 'Subscription-Key' })).toBeVisible();
    await expect(page.getByRole('radio', { name: /Beta/ })).toBeVisible();
    await expect(page.getByText('Instanz-Kennung')).toHaveCount(0);
    await page.getByRole('button', { name: 'Überspringen' }).click();

    // Danach wartet die Einführung auf der Seite des LCM-Hosts.
    await expect(page).toHaveURL(/\/servers\/\d+$/);
    await expect(page.getByRole('heading', { name: 'Das ist dein LCM-Host' })).toBeVisible();
    expect(assistent.patches.map((koerper) => koerper.setup_step)).toEqual(['addresses', 'services', 'access', 'certificate', 'onboarding', 'mail', 'retention', 'backup', 'subscription', 'intro']);
    // Die Wahl bleibt, wie sie gespeichert war (hier das Cookie der Test-Konfiguration), und gilt als bestätigt.
    expect(assistent.patches[0]).toEqual({ browser_session_mode: 'cookie', browser_session_mode_confirmed: true, setup_step: 'addresses' });
    expect(assistent.zeitplaene).toEqual([]);
  });

  test('Zurück zeigt den Schritt davor mit den gespeicherten Werten', async ({ page }) => {
    const assistent = await richteAssistentEin(page, 'services');
    await page.goto('/setup');
    await expect(page.getByRole('heading', { level: 1, name: 'Welche Schnittstellen von LCM willst du verwenden?' })).toBeVisible();
    await page.getByRole('button', { name: 'Zurück' }).click();
    await expect(fortschritt(page)).toContainText('Schritt 2 von 10');
    await expect(page.getByRole('heading', { level: 1, name: 'Wie ist LCM erreichbar?' })).toBeVisible();
    await expect(page.getByRole('textbox', { name: 'Port', exact: true }).first()).not.toHaveValue('');
    await page.getByRole('button', { name: 'Zurück' }).click();
    await expect(page.getByRole('heading', { level: 1, name: 'Anmeldung im Browser' })).toBeVisible();
    expect(assistent.patches).toEqual([{ setup_step: 'addresses' }, { setup_step: 'session' }]);
  });

  test('die Hilfe steht rechts neben dem Schritt, klappt zur Seite ein und bleibt es im nächsten Schritt', async ({ page }) => {
    await richteAssistentEin(page, 'addresses');
    await page.goto('/setup');
    const hilfe = page.locator('lcm-explainer');
    await expect(hilfe).toBeVisible();
    const inhalt = await page.locator('lcm-setup-addresses .step, [data-step="addresses"]').first().boundingBox();
    const spalte = await hilfe.boundingBox();
    expect(spalte!.x).toBeGreaterThan(inhalt!.x + inhalt!.width - 1);
    await hilfe.getByRole('button', { name: 'Einklappen' }).click();
    await expect(hilfe).toHaveAttribute('collapsed', '');
    await weiter(page).click(); // Schnittstellen: ohne Hilfe
    await expect(page.getByRole('heading', { level: 1, name: /Welche Schnittstellen/ })).toBeVisible();
    await expect(page.locator('lcm-explainer')).toHaveCount(0);
    await weiter(page).click(); // Zugriff
    await expect(page.getByRole('heading', { level: 1, name: 'Wer darf LCM erreichen?' })).toBeVisible();
    await weiter(page).click(); // Zertifikat: wieder mit Hilfe, noch eingeklappt
    await expect(page.getByRole('heading', { level: 1, name: 'Zertifikat' })).toBeVisible();
    await expect(page.locator('lcm-explainer')).toHaveAttribute('collapsed', '');
  });

  test('schmal steht die Hilfe unter dem Schritt und klappt nach oben ein', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await richteAssistentEin(page, 'addresses');
    await page.goto('/setup');
    const hilfe = page.locator('lcm-explainer');
    await expect(hilfe).toHaveAttribute('stacked', '');
    const inhalt = await page.locator('[data-step="addresses"]').boundingBox();
    const spalte = await hilfe.boundingBox();
    expect(spalte!.y).toBeGreaterThan(inhalt!.y + inhalt!.height - 1);
    const offen = (await hilfe.boundingBox())!.height;
    await hilfe.getByRole('button', { name: 'Einklappen' }).click();
    await expect.poll(async () => (await hilfe.boundingBox())!.height).toBeLessThan(offen / 3);
    await expect(hilfe.getByRole('button', { name: 'Ausklappen' })).toBeVisible();
  });

  test('Modus A nimmt nur den Namen, optional mit Port, und speichert https://name:port', async ({ page }) => {
    const assistent = await richteAssistentEin(page, 'addresses');
    await page.goto('/setup');
    const name = page.getByRole('textbox', { name: 'Name der Weboberfläche' });
    await name.fill('lcm.example.org:8443');
    await weiter(page).click();
    await expect(page.getByRole('heading', { level: 1, name: 'Welche Schnittstellen von LCM willst du verwenden?' })).toBeVisible();
    expect(assistent.patches[0]).toMatchObject({ public_base_url: 'https://lcm.example.org:8443', setup_step: 'services' });
  });

  test('Modus A lehnt einen ungültigen Port hinter dem Namen ab', async ({ page }) => {
    const assistent = await richteAssistentEin(page, 'addresses');
    await page.goto('/setup');
    await page.getByRole('textbox', { name: 'Name der Weboberfläche' }).fill('lcm.example.org:99999');
    await weiter(page).click();
    await expect(page.getByText('Der Port hinter dem Namen muss eine Zahl von 1 bis 65535 sein.')).toBeVisible();
    expect(assistent.patches).toEqual([]);
  });

  test('Modus B: die Vorschau der Adressen hat im Assistenten keine Kopierknöpfe und überlappt nicht', async ({ page }) => {
    await richteAssistentEin(page, 'addresses');
    await page.goto('/setup');
    await waehle(page, /^Eigene Ports, ein Name/);
    await page.getByRole('textbox', { name: 'Name', exact: true }).fill('lcm.example.org');
    await expect(page.locator('ul.derived .url')).toHaveCount(3);
    await expect(page.getByRole('button', { name: 'Kopieren' })).toHaveCount(0);
    const zeilen = await page.locator('ul.derived > li > .service, ul.derived > li > .url').evaluateAll((elemente) => elemente.map((e) => e.getBoundingClientRect()));
    for (let i = 1; i < zeilen.length; i++) {
      const a = zeilen[i - 1]!;
      const b = zeilen[i]!;
      const ueberlappt = a.left < b.right && b.left < a.right && a.top < b.bottom && b.top < a.bottom;
      expect(ueberlappt).toBe(false);
    }
  });

  test('Zertifikat: mit Let\'s Encrypt bleibt der Knopf unten gesperrt, die Karte hat „Zertifikat anfordern“', async ({ page }) => {
    await richteAssistentEin(page, 'certificate', 'https://lcm.example.org');
    await page.goto('/setup');
    await waehle(page, /^Let's Encrypt/);
    await expect(page.getByText('Let\'s Encrypt ist noch nicht eingeschaltet')).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Zertifikat anfordern' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Weiter', exact: true })).toBeDisabled();
    await expect(page.getByRole('button', { name: 'Speichern und weiter' })).toHaveCount(0);
  });

  test('das Backup speichert den Zeitplan, mit Passphrase, wenn LCM noch keinen Schlüssel hat', async ({ page }) => {
    const assistent = await richteAssistentEin(page, 'backup');
    await page.goto('/setup');
    await page.getByRole('switch', { name: 'Automatische Backups' }).click();
    await weiter(page).click();
    await expect(page.getByText('Vergib eine Passphrase: Ein geplantes Backup ist verschlüsselt.')).toBeVisible();
    expect(assistent.zeitplaene).toEqual([]);

    await page.getByLabel('Passphrase', { exact: false }).first().fill('eine-ausreichend-lange-Passphrase-2026');
    await weiter(page).click();
    await expect(page.getByRole('heading', { level: 1, name: 'Subscription und Paketkanal' })).toBeVisible();
    expect(assistent.zeitplaene).toHaveLength(1);
    expect(assistent.zeitplaene[0]).toMatchObject({ enabled: true, interval_hours: 24 });
  });

  test('E-Mail: die Testnachricht speichert die Angaben zuerst, Weiter schaltet den Versand ein', async ({ page }) => {
    const assistent = await richteAssistentEin(page, 'mail');
    const testmails: number[] = [];
    await page.route('**/api/v1/settings/global/test-mail', (route) => {
      testmails.push(assistent.patches.length);
      return route.fulfill({ json: {} });
    });
    await page.goto('/setup');
    await expect(page.getByRole('heading', { level: 1, name: 'E-Mail-Versand' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Testnachricht senden' })).toBeDisabled();
    await page.getByRole('textbox', { name: 'Server' }).first().fill('smtp.example.com');
    await page.getByRole('textbox', { name: /Absender/ }).fill('lcm@example.com');
    await page.getByRole('button', { name: 'Testnachricht senden' }).click();
    await expect.poll(() => testmails).toEqual([1]);
    expect(assistent.patches[0]).toEqual({ mail_enabled: true, mail_host: 'smtp.example.com', mail_from: 'lcm@example.com' });
    await weiter(page).click();
    await expect(page.getByRole('heading', { level: 1, name: 'Aufbewahrung' })).toBeVisible();
    expect(assistent.patches[1]).toEqual({ setup_step: 'retention' });
  });

  test('Aufbewahrung: Vorgaben vorbelegt, geänderte Tage gehen mit zum Server', async ({ page }) => {
    const assistent = await richteAssistentEin(page, 'retention');
    await page.goto('/setup');
    const jobs = page.getByRole('spinbutton', { name: /Job-Verläufe/ });
    await expect(jobs).not.toHaveValue('');
    await jobs.fill('30');
    await weiter(page).click();
    await expect(page.getByRole('heading', { level: 1, name: 'Automatisches Backup' })).toBeVisible();
    expect(assistent.patches).toEqual([{ log_retention_days: 30, setup_step: 'backup' }]);
  });

  test('Zertifikat: die Erfolgsmeldung von Let\'s Encrypt steht in voller Breite, nicht Buchstabe für Buchstabe', async ({ page }) => {
    await richteAssistentEin(page, 'certificate', 'https://lcm.example.org');
    await page.route('**/api/v1/system/acme', async (route) => {
      if (route.request().method() !== 'GET') return route.fallback();
      const antwort = await route.fetch();
      const koerper = await antwort.json();
      const zertifikat = { id: '1', names: ['lcm.example.org'], issuer: 'R11', not_after: '2999-01-01T00:00:00Z', fingerprint: 'ab:cd', renew_at: '2998-12-01T00:00:00Z' };
      await route.fulfill({
        response: antwort,
        json: { ...koerper, enabled: true, names: ['lcm.example.org'], terms_accepted: true, running: false, last_run_at: '2026-11-22T10:00:00Z', last_error: '', certificates: [zertifikat] },
      });
    });
    await page.goto('/setup');
    await expect(page.getByRole('heading', { level: 1, name: 'Zertifikat' })).toBeVisible();
    await waehle(page, /^Let's Encrypt/);
    const fertig = page.locator('[data-field="acme-done"]');
    await expect(fertig).toContainText('Das Zertifikat für lcm.example.org ist da und gilt bis');
    for (const breite of [1440, 390]) {
      await page.setViewportSize({ width: breite, height: 900 });
      const karte = (await page.locator('lcm-settings-acme').boundingBox())!;
      const meldung = (await fertig.boundingBox())!;
      // Die Meldung füllt die Zeile des Karten-Fußes (bis auf Innenabstand), statt auf die Breite eines Zeichens zu schrumpfen.
      expect(meldung.width, `${breite} px`).toBeGreaterThan(karte.width * 0.8);
      expect(meldung.height, `${breite} px`).toBeLessThan(120);
    }
  });

  test('Zertifikat: ein Reverse-Proxy braucht gültige Adressen und schaltet den Header ein', async ({ page }) => {
    // Mit Namen, aber ohne dass Schritt 2 ihn eingetragen hätte: Der Assistent bleibt nach dem Speichern auf der Adresse.
    const assistent = await richteAssistentEin(page, 'certificate', 'https://lcm.example.org');
    await page.goto('/setup');
    // Die config.json der Tests schaltet trust_proxy_header ein: Vorgewählt ist darum der Proxy.
    await waehle(page, /^Let's Encrypt/);
    await expect(page.getByRole('heading', { name: 'Let\'s Encrypt', level: 3 })).toBeVisible();

    await waehle(page, /Reverse-Proxy davor/);
    const feld = page.getByRole('textbox', { name: 'Adressen des Proxys' });
    await feld.fill('');
    await weiter(page).click();
    await expect(page.getByText('Trage mindestens eine Adresse ein.')).toBeVisible();

    await feld.fill('0.0.0.0/0');
    await weiter(page).click();
    await expect(page.getByText('Ein Netzbereich über alle Adressen ist nicht erlaubt: 0.0.0.0/0')).toBeVisible();

    await feld.fill('proxy.example.com');
    await weiter(page).click();
    await expect(page.getByText('Keine gültige Adresse oder kein gültiger Netzbereich: proxy.example.com')).toBeVisible();
    expect(assistent.patches).toEqual([]);

    await feld.fill('10.0.0.1\n192.168.0.0/24');
    await weiter(page).click();
    await expect(page.getByRole('heading', { level: 1, name: 'Neue Server aufnehmen' })).toBeVisible();
    expect(assistent.patches).toEqual([{ trust_proxy_header: true, trusted_proxies: ['10.0.0.1', '192.168.0.0/24'], setup_step: 'onboarding' }]);
  });

  test('Zertifikat: liegt die Oberfläche danach auf einer anderen Adresse, kündigt der Schritt den Wechsel an', async ({ page }) => {
    await richteAssistentEin(page, 'certificate', 'https://lcm.example.org', true);
    await page.goto('/setup');
    await weiter(page).click();
    const meldung = hinweis(page, /Die Oberfläche ist jetzt unter https:\/\/lcm\.example\.org erreichbar/);
    await expect(meldung).toBeVisible();
    await expect(meldung).toContainText('neue Anmeldung');
    await expect(page.getByRole('button', { name: 'Jetzt wechseln' })).toBeVisible();
  });

  test('Zertifikat ohne Namen: Let\'s Encrypt zeigt nur den Hinweis, „Zu Schritt 2“ führt zurück', async ({ page }) => {
    const assistent = await richteAssistentEin(page, 'certificate');
    await page.goto('/setup');
    // Die Adresse der Seite ist `localhost`: Es gibt keinen Namen für Let's Encrypt.
    await waehle(page, /^Let's Encrypt/);
    const hinweisText = hinweis(page, /Für Let's Encrypt braucht LCM einen Namen/);
    await expect(hinweisText).toContainText('Trag ihn in Schritt 2 (Wie ist LCM erreichbar?) ein.');
    await expect(page.locator('lcm-settings-acme')).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Zertifikat anfordern' })).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Weiter', exact: true })).toBeDisabled();

    await page.getByRole('button', { name: 'Zu Schritt 2' }).click();
    await expect(page.getByRole('heading', { level: 1, name: 'Wie ist LCM erreichbar?' })).toBeVisible();
    expect(assistent.patches).toEqual([{ setup_step: 'addresses' }]);
  });

  test('Zertifikat ohne Namen: der Proxy ist vorgewählt, der Browser bleibt auf der Adresse und es geht weiter', async ({ page }) => {
    const assistent = await richteAssistentEin(page, 'certificate');
    await page.goto('/setup');
    await expect(page.getByRole('radio', { name: /Reverse-Proxy davor/ })).toBeChecked();
    await weiter(page).click();
    await expect(page.getByRole('heading', { level: 1, name: 'Neue Server aufnehmen' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Jetzt wechseln' })).toHaveCount(0);
    expect(assistent.patches.map((koerper) => koerper.setup_step)).toEqual(['onboarding']);
  });

  test('Zertifikat: eine IP-Adresse als Rückfall ist kein Name, auch wenn sie von der Seite abweicht: keine Weiterleitung', async ({ page }) => {
    await richteAssistentEin(page, 'certificate', 'https://10.20.30.40:9310', true);
    await page.goto('/setup');
    await weiter(page).click();
    await expect(page.getByRole('heading', { level: 1, name: 'Neue Server aufnehmen' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Jetzt wechseln' })).toHaveCount(0);
  });

  /** Subscription und Kanäle der Seite; nach der Aktivierung gilt der Vertrag und Enterprise ist frei. */
  async function richteSubscriptionEin(page: Page): Promise<{ umstellungen: Koerper[]; aktivierungen: Koerper[] }> {
    const stand = { aktiv: false };
    const protokoll = { umstellungen: [] as Koerper[], aktivierungen: [] as Koerper[] };
    await page.route('**/api/v1/subscription', (route) =>
      route.fulfill({
        json: { configured: stand.aktiv, status: stand.aktiv ? 'active' : '', key_prefix: 'LCM-E-AAAAA', apt_available: true, apt_channel: 'community', instance_id: 'x' },
      }),
    );
    await page.route('**/api/v1/subscription/apt/channels', (route) =>
      route.fulfill({
        json: {
          installed: '2.0.0',
          channels: [
            { channel: 'community', latest: '2.0.0', lock: '' },
            { channel: 'beta', latest: '2.0.1~beta.1', lock: '' },
            { channel: 'enterprise', latest: '2.0.0', lock: stand.aktiv ? '' : 'subscription' },
          ],
        },
      }),
    );
    await page.route('**/api/v1/subscription/apt', (route) => {
      protokoll.umstellungen.push(route.request().postDataJSON() as Koerper);
      return route.fulfill({ json: { id: 'job-1' } });
    });
    await page.route('**/api/v1/subscription/activate', (route) => {
      protokoll.aktivierungen.push(route.request().postDataJSON() as Koerper);
      stand.aktiv = true;
      return route.fulfill({ json: {} });
    });
    return protokoll;
  }

  test('Subscription: die Kanalwahl wirkt erst mit Weiter, ohne Sofort-Umstellung und ohne „Einrichtung erneut anwenden“', async ({ page }) => {
    const assistent = await richteAssistentEin(page, 'subscription');
    const subscription = await richteSubscriptionEin(page);
    await page.goto('/setup');
    await expect(page.getByRole('heading', { level: 1, name: 'Subscription und Paketkanal' })).toBeVisible();
    await expect(page.getByRole('button', { name: /Einrichtung erneut anwenden|umstellen/ })).toHaveCount(0);

    await page.getByRole('radio', { name: /^Beta-Kanal/ }).check();
    await expect(page.getByRole('radio', { name: /^Beta-Kanal/ })).toBeChecked();
    expect(subscription.umstellungen).toEqual([]);

    await page.getByRole('button', { name: 'Weiter', exact: true }).click();
    await expect(page).toHaveURL(/\/servers\/\d+$/);
    expect(subscription.umstellungen).toEqual([{ channel: 'beta' }]);
    expect(assistent.patches.map((koerper) => koerper.setup_step)).toEqual(['intro']);
  });

  test('Subscription: eine unveränderte Wahl stellt nichts um', async ({ page }) => {
    const assistent = await richteAssistentEin(page, 'subscription');
    const subscription = await richteSubscriptionEin(page);
    await page.goto('/setup');
    await page.getByRole('button', { name: 'Weiter', exact: true }).click();
    await expect(page).toHaveURL(/\/servers\/\d+$/);
    expect(subscription.umstellungen).toEqual([]);
    expect(assistent.patches.map((koerper) => koerper.setup_step)).toEqual(['intro']);
  });

  test('Subscription: nach der Aktivierung empfiehlt der Schritt Enterprise, Weiter stellt um', async ({ page }) => {
    await richteAssistentEin(page, 'subscription');
    const subscription = await richteSubscriptionEin(page);
    await page.goto('/setup');
    await page.getByRole('textbox', { name: 'Subscription-Key' }).fill('LCM-E-AAAAA-BBBBB-CCCCC-DDDDD');
    await page.getByRole('button', { name: 'Aktivieren' }).click();
    await expect(hinweis(page, /Subscription aktiv\. Wähle unten den Enterprise-Kanal; mit „Weiter“ stellt LCM um\./)).toBeVisible();
    await expect(page.getByRole('radio', { name: /^Enterprise-Kanal/ })).toBeChecked();
    expect(subscription.aktivierungen).toEqual([{ subscription_key: 'LCM-E-AAAAA-BBBBB-CCCCC-DDDDD' }]);
    expect(subscription.umstellungen).toEqual([]);

    await page.getByRole('button', { name: 'Weiter', exact: true }).click();
    await expect(page).toHaveURL(/\/servers\/\d+$/);
    expect(subscription.umstellungen).toEqual([{ channel: 'enterprise' }]);
  });

  test('Subscription: Überspringen nach der Aktivierung lässt den Kanal', async ({ page }) => {
    await richteAssistentEin(page, 'subscription');
    const subscription = await richteSubscriptionEin(page);
    await page.goto('/setup');
    await page.getByRole('textbox', { name: 'Subscription-Key' }).fill('LCM-E-AAAAA-BBBBB-CCCCC-DDDDD');
    await page.getByRole('button', { name: 'Aktivieren' }).click();
    await expect(page.getByRole('radio', { name: /^Enterprise-Kanal/ })).toBeChecked();
    await page.getByRole('button', { name: 'Überspringen' }).click();
    await expect(page).toHaveURL(/\/servers\/\d+$/);
    expect(subscription.umstellungen).toEqual([]);
  });

  test('Zugriff: ungespeicherte Netzbereiche warnen einmal vor dem Weitergehen', async ({ page }) => {
    const assistent = await richteAssistentEin(page, 'access');
    await page.goto('/setup');
    await page.getByRole('textbox', { name: 'Erlaubte Netze' }).first().fill('10.0.0.0/8');
    await weiter(page).click();
    await expect(hinweis(page, /noch nicht gespeichert/)).toBeVisible();
    expect(assistent.patches).toEqual([]);

    await page.getByRole('button', { name: 'Ohne diese Änderungen weiter' }).click();
    await expect(page.getByRole('heading', { level: 1, name: 'Zertifikat' })).toBeVisible();
    expect(assistent.patches).toEqual([{ setup_step: 'certificate' }]);
  });
});

test.describe('Einführung auf der Seite des LCM-Hosts', () => {
  const FENSTER = 'Einführung in den LCM-Host';

  /** Ohne Container und mit apt, Trivy ohne Sandbox installiert: Die Karte zeigt ihre Knöpfe. */
  async function richteHostEin(page: Page): Promise<void> {
    await page.route('**/api/v1/servers/*/lcm-host/status', (route) =>
      route.fulfill({
        json: {
          is_lcm_host: true,
          package_manager: 'apt',
          in_container: false,
          trivy_installed: true,
          sandbox_retrofit: true,
          apt_cacher_installed: false,
          crowdsec_lapi_installed: false,
          scanner: { available: true, version: '0.60.0', freshness: 'fresh', sandboxed: false, sandbox_backend: '', sandbox_note: '' },
        },
      }),
    );
  }

  async function oeffneHost(page: Page): Promise<void> {
    await page.goto('/servers');
    await page.getByRole('row').filter({ hasText: 'lcm-host' }).getByRole('gridcell', { name: 'lcm-host' }).click();
  }

  test('erklärt und navigiert in 18 Schritten, ohne Aktionsknöpfe, lässt sich schließen und kommt nicht wieder', async ({ page }) => {
    await richteHostEin(page);
    const assistent = await richteAssistentEin(page, 'intro');
    await oeffneHost(page);

    const fenster = page.getByRole('region', { name: FENSTER });
    const titel = fenster.locator('[data-field="tour-title"]');
    const fortschritt = fenster.locator('[data-field="tour-progress"]');
    const weiter = fenster.getByRole('button', { name: 'Weiter' });
    await expect(titel).toContainText('Das ist dein LCM-Host');
    await expect(fortschritt).toHaveText('1 von 18');
    await expect(fenster.getByRole('button', { name: 'Zurück' })).toHaveCount(0);
    const box = (await fenster.boundingBox())!;
    expect(box.x).toBeGreaterThanOrEqual(0);
    expect(box.x + box.width).toBeLessThanOrEqual(page.viewportSize()!.width);
    // Die Knöpfe zum Einrichten stehen auf der Karte, auch während der Einführung, und nie im Fenster.
    await expect(page.getByRole('button', { name: 'Sandbox nachrüsten' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'apt-cacher-ng einrichten' })).toBeVisible();
    await expect(fenster.getByRole('button', { name: /einrichten|installieren|nachrüsten/ })).toHaveCount(0);

    await weiter.click();
    await expect(titel).toContainText('Trivy');
    await expect(page.locator('[data-field="tour-ring"]')).toBeVisible();
    await weiter.click();
    await expect(titel).toContainText('apt-cacher-ng');
    await expect(fenster).not.toContainText('CrowdSec');
    await weiter.click();
    await expect(titel).toContainText('SSH härten und Firewall');

    // Schritte 5 bis 9: Das Einstellungsfenster geht auf und blättert seine Seiten durch.
    await weiter.click();
    await expect(titel).toContainText('Einstellungen: Betrieb');
    await expect(fortschritt).toHaveText('5 von 18');
    await expect(page).toHaveURL(/\?modal=settings&section=operation$/);
    const einstellungen = page.getByRole('dialog', { name: 'Servereinstellungen' });
    await expect(page.getByRole('tab', { name: 'Betrieb', selected: true })).toBeVisible();
    // Das Fenster ist nicht modal: Das Einführungsfenster bleibt bedienbar.
    await expect(fenster).toBeVisible();
    const teile: [string, RegExp][] = [
      ['Einstellungen: Betrieb', /section=operation$/],
      ['Einstellungen: Zugang', /section=access$/],
      ['Einstellungen: DNS', /section=dns$/],
      ['Einstellungen: Zeit', /section=time$/],
    ];
    await weiter.click();
    await expect(page).toHaveURL(teile[0]![1]);
    for (const [ueberschrift, adresse] of teile.slice(1)) {
      await weiter.click();
      await expect(titel).toContainText(ueberschrift);
      await expect(page).toHaveURL(adresse);
    }

    // Schritte 10 bis 16: Die Reiter des Servers; das Einstellungsfenster ist wieder zu.
    const reiter = ['Pakete', 'Apps', 'Benutzer', 'Sicherheit', 'Deep-Scan', 'Speicher', 'Jobs'];
    for (const [index, name] of reiter.entries()) {
      await weiter.click();
      await expect(titel).toHaveText(new RegExp(`^${name}`));
      await expect(fortschritt).toHaveText(`${10 + index} von 18`);
      await expect(einstellungen).toHaveCount(0);
      await expect(page.getByRole('tab', { name: new RegExp(`^${name}`), selected: true })).toBeVisible();
    }
    await expect(page).toHaveURL(/\/servers\/\d+\/jobs$/);

    // Schritt 17: zurück auf der Hauptseite, bei den Gruppen.
    await weiter.click();
    await expect(titel).toContainText('Gruppen');
    await expect(page).toHaveURL(/\/servers\/\d+$/);
    await expect(page.locator('lcm-srv-groups').getByRole('heading', { name: 'Gruppen' })).toBeVisible();

    // Schritt 18
    await weiter.click();
    await expect(titel).toContainText('Einrichtung abgeschlossen');
    await expect(fortschritt).toHaveText('18 von 18');
    await expect(weiter).toHaveCount(0);
    await expect(fenster.getByRole('button', { name: 'Einführung schließen' })).toHaveCount(0);
    await fenster.getByRole('button', { name: 'Zurück' }).click();
    await expect(titel).toContainText('Gruppen');
    await weiter.click();

    await fenster.getByRole('button', { name: 'Fertig' }).click();
    await expect(fenster).toHaveCount(0);
    expect(assistent.patches).toEqual([{ setup_step: '' }]);

    await page.reload();
    await expect(page.getByRole('heading', { name: 'LCM-Host', exact: true })).toBeVisible();
    await expect(page.getByRole('region', { name: FENSTER })).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Sandbox nachrüsten' })).toBeVisible();
  });

  test('Einführung schließen geht auf jeder Station, auch bei offenem Einstellungsfenster', async ({ page }) => {
    await richteHostEin(page);
    const assistent = await richteAssistentEin(page, 'intro');
    await oeffneHost(page);
    const fenster = page.getByRole('region', { name: FENSTER });
    for (let i = 0; i < 4; i += 1) await fenster.getByRole('button', { name: 'Weiter' }).click();
    await expect(page.getByRole('dialog', { name: 'Servereinstellungen' })).toBeVisible();
    await fenster.getByRole('button', { name: 'Einführung schließen' }).click();
    await expect(fenster).toHaveCount(0);
    await expect(page.getByRole('dialog', { name: 'Servereinstellungen' })).toHaveCount(0);
    expect(assistent.patches).toEqual([{ setup_step: '' }]);
  });

  test('Trivy ohne Sandbox bekommt „Sandbox nachrüsten“, auch wenn LCM es nicht installiert hat', async ({ page }) => {
    await richteHostEin(page);
    await richteAssistentEin(page, '');
    await oeffneHost(page);
    const karte = page.locator('lcm-srv-lcm-host');
    await expect(karte.getByText('ohne Sandbox')).toBeVisible();
    await expect(karte.getByRole('button', { name: 'Sandbox nachrüsten' })).toBeVisible();
  });
});
