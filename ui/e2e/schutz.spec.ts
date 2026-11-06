// Schutz vor Fehlbedienung und Angriffen: Rückfrage bei ungespeicherten Änderungen, strenge CSP
// und Feldfehler aus der Eingabeprüfung der API.
import { test, expect } from './fixtures';
import { fenster, speicherleiste } from './helpers';

const nameserver = (page: import('@playwright/test').Page) => page.getByRole('textbox', { name: 'Nameserver' });

test.describe('Ungespeicherte Änderungen', () => {
  test('ein Link in andere Einstellungen fragt nach; „Weiter bearbeiten“ behält die Eingabe', async ({ page }) => {
    await page.goto('/settings/dns');
    await nameserver(page).fill('Test = 192.0.2.53');
    await page.getByRole('link', { name: 'Zeit', exact: true }).click();

    const rueckfrage = fenster(page, 'Änderungen verwerfen?');
    await expect(rueckfrage.getByText('Diese Seite hat ungespeicherte Änderungen')).toBeVisible();
    await rueckfrage.getByRole('button', { name: 'Weiter bearbeiten' }).click();
    // Man bleibt auf der Seite, und die Eingabe ist noch da.
    await expect(page).toHaveURL(/\/settings\/dns$/);
    await expect(nameserver(page)).toHaveValue('Test = 192.0.2.53');
  });

  test('„Verwerfen“ geht zum Ziel und lässt die Eingabe verfallen', async ({ page }) => {
    await page.goto('/settings/dns');
    const vorher = await nameserver(page).inputValue();
    await nameserver(page).fill('Test = 192.0.2.53');
    await page.getByRole('link', { name: 'Zeit', exact: true }).click();
    await fenster(page, 'Änderungen verwerfen?').getByRole('button', { name: 'Verwerfen' }).click();
    await expect(page).toHaveURL(/\/settings\/time$/);

    // Beim Wiederkommen steht wieder der gespeicherte Stand.
    await page.getByRole('link', { name: 'DNS', exact: true }).click();
    await expect(nameserver(page)).toHaveValue(vorher);
  });

  test('„Zurück“ im Browser fragt ebenfalls nach', async ({ page }) => {
    await page.goto('/settings/general');
    await page.getByRole('link', { name: 'DNS', exact: true }).click();
    await expect(page).toHaveURL(/\/settings\/dns$/);
    await nameserver(page).fill('Test = 192.0.2.53');

    // Der Browser hat die Adresse beim Zurückgehen schon gewechselt; „Weiter bearbeiten“ stellt sie wieder her.
    await page.goBack();
    await fenster(page, 'Änderungen verwerfen?').getByRole('button', { name: 'Weiter bearbeiten' }).click();
    await expect(page).toHaveURL(/\/settings\/dns$/);
    await expect(nameserver(page)).toHaveValue('Test = 192.0.2.53');

    // Noch einmal zurück und diesmal verwerfen.
    await page.goBack();
    await fenster(page, 'Änderungen verwerfen?').getByRole('button', { name: 'Verwerfen' }).click();
    await expect(page).toHaveURL(/\/settings\/general$/);
  });

  test('ohne Änderung wechselt man ohne Rückfrage', async ({ page }) => {
    await page.goto('/settings/dns');
    await expect(nameserver(page)).toBeVisible();
    await page.getByRole('link', { name: 'Zeit', exact: true }).click();
    await expect(page).toHaveURL(/\/settings\/time$/);
    await expect(page.getByRole('heading', { level: 2, name: 'Änderungen verwerfen?' })).toHaveCount(0);
  });

  test('„Verwerfen“ in der Speicherleiste nimmt die Änderung zurück, danach gibt es keine Rückfrage', async ({ page }) => {
    await page.goto('/settings/dns');
    const vorher = await nameserver(page).inputValue();
    await nameserver(page).fill('Test = 192.0.2.53');
    await speicherleiste(page).getByRole('button', { name: 'Verwerfen' }).click();
    await expect(nameserver(page)).toHaveValue(vorher);
    await page.getByRole('link', { name: 'Zeit', exact: true }).click();
    await expect(page).toHaveURL(/\/settings\/time$/);
  });
});

test.describe('Eingabeprüfung der API', () => {
  test('eine ungültige IP wird vom Server abgewiesen und am Feld gezeigt', async ({ page }) => {
    await page.goto('/settings/dns');
    await expect(nameserver(page)).toBeVisible();
    const vorher = await nameserver(page).inputValue();
    await nameserver(page).fill('Test = 999.1.1.1');
    const antwort = page.waitForResponse((r) => r.url().includes('/api/v1/settings') && r.request().method() !== 'GET');
    await speicherleiste(page).getByRole('button', { name: 'Speichern' }).click();
    // Die Prüfung gehört dem Server: Er antwortet mit einem Eingabefehler, nicht mit einem Absturz.
    expect((await antwort).status()).toBe(422);

    // Die Oberfläche ordnet den Fehler dem Feld zu und nennt es in der Speicherleiste.
    await expect(nameserver(page)).toHaveAttribute('aria-invalid', 'true');
    await expect(speicherleiste(page).getByRole('alert')).toContainText('„Nameserver“ prüfen');
    await speicherleiste(page).getByRole('button', { name: 'Zum Feld' }).click();
    await expect(nameserver(page)).toBeFocused();

    // Mit einer gültigen Adresse geht es durch, und der Zustand ist wieder sauber.
    await nameserver(page).fill('Test = 192.0.2.53');
    await speicherleiste(page).getByRole('button', { name: 'Speichern' }).click();
    await expect(page.getByText('Gespeichert', { exact: true })).toBeVisible();
    // Aufräumen: Der Test hinterlässt den vorherigen Stand.
    await page.reload();
    await nameserver(page).fill(vorher);
    if (await speicherleiste(page).getByRole('button', { name: 'Speichern' }).isEnabled()) {
      await speicherleiste(page).getByRole('button', { name: 'Speichern' }).click();
    }
  });
});

test.describe('Ungespeicherte Änderungen, während der nächste Abschnitt lädt', () => {
  // Der Service Worker hält die Module vor; das Laden soll hier aber dauern.
  test.use({ serviceWorkers: 'block' });

  test('nach „Verwerfen“ ist der alte Abschnitt sofort weg, auch solange der neue noch lädt', async ({ page }) => {
    let freigeben = (): void => undefined;
    const freigegeben = new Promise<void>((fertig) => (freigeben = fertig));
    await page.route('**/assets/time-*.js', async (route) => {
      await freigegeben;
      await route.continue();
    });
    await page.goto('/settings/dns');
    const vorher = await nameserver(page).inputValue();
    await nameserver(page).fill('Test = 192.0.2.53');
    await page.getByRole('link', { name: 'Zeit', exact: true }).click();
    await fenster(page, 'Änderungen verwerfen?').getByRole('button', { name: 'Verwerfen' }).click();
    await expect(nameserver(page)).toHaveCount(0);

    // Noch während des Ladens: keine zweite Rückfrage, und die verworfene Eingabe kommt nicht zurück.
    await page.getByRole('link', { name: 'DNS', exact: true }).click();
    await expect(page).toHaveURL(/\/settings\/dns$/);
    await expect(nameserver(page)).toHaveValue(vorher);
    freigeben();
  });
});

test.describe('Content-Security-Policy', () => {
  const SEITEN = ['/', '/servers', '/servers/1', '/servers/1/security', '/servers/1/storage', '/groups/2', '/linux-users', '/security', '/containers', '/jobs', '/account/security', '/settings/general', '/settings/alerts', '/docs'];

  test('der Server setzt eine strenge Richtlinie mit Trusted Types', async ({ page }) => {
    const antwort = await page.goto('/');
    const richtlinie = antwort!.headers()['content-security-policy'] ?? '';
    expect(richtlinie).toContain("style-src 'self'");
    expect(richtlinie).toContain("script-src 'self'");
    expect(richtlinie).toContain('require-trusted-types-for');
    expect(richtlinie).not.toContain("'unsafe-inline'");
    expect(richtlinie).not.toContain("'unsafe-eval'");
  });

  test('Seitenbesuche und offene Fenster lösen keine CSP-Verletzung aus', async ({ page, browserName }) => {
    test.slow(); // geht viele Seiten durch und reißt unter Last sonst die Zeitgrenze
    // WebKit meldet jedes <select> als `style-src-attr`, auch auf einer leeren Seite.
    const zaehlt = (text: string) => browserName !== 'webkit' || !/style-src-attr|apply a stylesheet/.test(text);
    const meldungen: string[] = [];
    page.on('console', (nachricht) => {
      if (/content security policy|trusted type|refused to/i.test(nachricht.text()) && zaehlt(nachricht.text())) meldungen.push(nachricht.text());
    });
    page.on('pageerror', (fehler) => meldungen.push(fehler.message));
    await page.addInitScript(() => {
      document.addEventListener('securitypolicyviolation', (e) => {
        (window as unknown as { __csp: string[] }).__csp ??= [];
        (window as unknown as { __csp: string[] }).__csp.push(`${e.violatedDirective}: ${e.blockedURI}`);
      });
    });

    for (const pfad of SEITEN) {
      await page.goto(pfad);
      await expect(page.getByRole('main')).toBeVisible();
      await page.waitForLoadState('networkidle').catch(() => undefined);
      const verletzungen = await page.evaluate(() => (window as unknown as { __csp?: string[] }).__csp ?? []);
      expect(verletzungen.filter(zaehlt), `Verletzung auf ${pfad}`).toEqual([]);
    }

    // Fenster, Auswahlen und Menüs sind die Stellen, an denen Stile gern inline entstehen.
    await page.goto('/servers/1');
    await page.getByRole('button', { name: /^Firewall/ }).click();
    await expect(page.getByRole('heading', { level: 2, name: 'Firewall von web01' })).toBeVisible();
    await page.keyboard.press('Escape');
    await page.getByRole('button', { name: 'Aktionen' }).click();
    await page.keyboard.press('Escape');
    await page.getByRole('button', { name: 'Darstellung' }).click();
    await page.keyboard.press('Escape');
    await page.keyboard.press('ControlOrMeta+k');
    await expect(page.getByRole('dialog', { name: 'Befehlspalette' })).toBeVisible();
    await page.keyboard.press('Escape');
    expect((await page.evaluate(() => (window as unknown as { __csp?: string[] }).__csp ?? [])).filter(zaehlt)).toEqual([]);
    expect(meldungen).toEqual([]);
  });
});
