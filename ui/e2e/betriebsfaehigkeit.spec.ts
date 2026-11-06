// Betriebsfähigkeit: Karte auf der Serverseite mit Ursache, Dauer und Abhilfe.
//
// Die Antworten werden gestellt (page.route): Welche Kategorie gerade
// fehlschlägt, ist Zustand der Datenbank und lässt sich in den Demo-Daten
// nicht auf Zuruf herstellen. Die Seiten selbst laufen gegen das echte Binary.
import { test, expect } from './fixtures';
import { fenster } from './helpers';

const VOR_STUNDEN = (stunden: number) => new Date(Date.now() - stunden * 3600 * 1000).toISOString();

const ALLES_OK = ['connection', 'packages', 'rules', 'cve_scan'].map((category) => ({
  category,
  state: 'ok',
  effective: '',
  failing_since: null,
  consecutive_failures: 0,
  last_success_at: null,
  last_error_class: '',
  last_error_message: '',
  last_job_id: '',
  last_job_name: '',
  warning_class: '',
  warning_message: '',
  stale: '',
  repair: '',
  retryable: false,
}));

// Paketverwaltung seit zwei Tagen an einer dpkg-Sperre gescheitert.
function gesperrtePakete() {
  return ALLES_OK.map((kategorie) =>
    kategorie.category !== 'packages'
      ? kategorie
      : {
          ...kategorie,
          state: 'failed',
          effective: 'critical',
          failing_since: VOR_STUNDEN(49),
          consecutive_failures: 3,
          last_success_at: VOR_STUNDEN(100),
          last_error_class: 'lock_busy',
          last_error_message: 'PID 1234 (unattended-upgr)',
          last_error_params: { pid: '1234', process: 'unattended-upgr' },
          last_job_id: 'job-1',
          last_job_name: 'Alle Pakete aktualisieren @ web01',
          repair: 'dpkg_configure',
          retryable: true,
        },
  );
}

const karte = (page: import('@playwright/test').Page) => page.locator('lcm-card', { hasText: 'Betriebsfähigkeit' });

test.describe('Betriebsfähigkeit', () => {
  test('Karte zeigt Ursache, Dauer und Hinweis; Reparatur braucht Bestätigung', async ({ page }) => {
    await page.route('**/api/v1/servers/1/capabilities', (route) => route.fulfill({ json: gesperrtePakete() }));
    await page.goto('/servers/1');

    await expect(karte(page)).toBeVisible();
    // Jeder der vier Bereiche hat seine Zeile, nur Pakete ist kritisch.
    await expect(karte(page).getByRole('listitem')).toHaveCount(4);
    await expect(karte(page).getByRole('listitem').filter({ hasText: 'Pakete' }).getByRole('img', { name: 'Kritisch' })).toBeVisible();
    await expect(karte(page).getByRole('listitem').filter({ hasText: 'Regeln' }).getByRole('img', { name: 'In Ordnung' })).toBeVisible();

    await expect(karte(page)).toContainText('seit 2 Tagen · 3 Fehlschläge in Folge');
    await expect(karte(page).getByText('dpkg-Sperre durch unattended-upgr (PID 1234)')).toBeVisible();
    await expect(karte(page).getByText('Warte ab, bis er fertig ist')).toBeVisible();

    // Reparatur: erst die Bestätigung, dann (erst nach ihr) die Anfrage.
    let anfrage: unknown = null;
    await page.route('**/api/v1/servers/1/packages/repair', (route) => {
      anfrage = route.request().postDataJSON();
      return route.fulfill({ status: 202, json: { status: 'started', job_id: 'job-2', job_name: 'Paketverwaltung reparieren' } });
    });
    await karte(page).getByRole('button', { name: 'Reparieren' }).click();
    const bestaetigung = fenster(page, 'Reparieren');
    await expect(bestaetigung.getByText('dpkg --configure -a')).toBeVisible();
    expect(anfrage).toBeNull();

    await bestaetigung.getByRole('button', { name: 'Reparieren' }).click();
    await expect.poll(() => anfrage).toEqual({ action: 'dpkg_configure' });
  });

  test('der Job zum Fehlschlag ist nur mit jobs:read verlinkt', async ({ page }) => {
    await page.route('**/api/v1/servers/1/capabilities', (route) => route.fulfill({ json: gesperrtePakete() }));
    await page.goto('/servers/1');
    const verweis = karte(page).getByRole('link', { name: /Job ansehen/ });
    await expect(verweis).toHaveAttribute('href', '/jobs/job-1');

    // Dieselbe Sitzung ohne das Recht: der Verweis entfällt, der Rest bleibt.
    await page.route('**/api/v1/auth/me', async (route) => {
      const antwort = await route.fetch();
      const profil = await antwort.json();
      profil.permissions = profil.permissions.filter((recht: string) => recht !== 'jobs:read');
      await route.fulfill({ response: antwort, json: profil });
    });
    await page.reload();
    await expect(karte(page).getByText('dpkg-Sperre durch unattended-upgr (PID 1234)')).toBeVisible();
    await expect(karte(page).getByRole('link', { name: /Job ansehen/ })).toHaveCount(0);
  });

  test('läuft alles, steht trotzdem je Bereich eine Zeile mit „In Ordnung“', async ({ page }) => {
    await page.route('**/api/v1/servers/1/capabilities', (route) => route.fulfill({ json: ALLES_OK }));
    await page.goto('/servers/1');
    await expect(karte(page)).toBeVisible();
    const zeilen = karte(page).getByRole('listitem');
    await expect(zeilen).toHaveText([/Verbindung\s*In Ordnung/, /Pakete\s*In Ordnung/, /Regeln\s*In Ordnung/, /CVE-Scan\s*In Ordnung/]);
    await expect(karte(page).getByRole('img', { name: 'In Ordnung' })).toHaveCount(4);
    // Nichts zu reparieren, nichts zu wiederholen.
    await expect(karte(page).getByRole('button')).toHaveCount(0);
  });

  test('zeigt veralteten Paketstand und die Warnung einer Paketquelle auch ohne Fehlschlag', async ({ page }) => {
    const stand = ALLES_OK.map((kategorie) =>
      kategorie.category !== 'packages'
        ? kategorie
        : {
            ...kategorie,
            stale: 'warning',
            last_success_at: VOR_STUNDEN(80),
            warning_class: 'repo_error',
            warning_message: 'Die Quelle https://x hat keine Release-Datei',
          },
    );
    await page.route('**/api/v1/servers/1/capabilities', (route) => route.fulfill({ json: stand }));
    await page.goto('/servers/1');

    const pakete = karte(page).getByRole('listitem').filter({ hasText: 'Pakete' });
    await expect(pakete.getByRole('img', { name: 'Warnung' })).toBeVisible();
    await expect(pakete.getByText('Paketquelle fehlerhaft: Die Quelle https://x hat keine Release-Datei')).toBeVisible();
    await expect(pakete.getByText('Paketstand veraltet: Die Paketliste wurde seit 3 Tagen nicht mehr erfolgreich aktualisiert.')).toBeVisible();
  });

  test('die Job-Ausgabe lässt sich in der Karte aufklappen, nur mit jobs:read', async ({ page }) => {
    await page.route('**/api/v1/servers/1/capabilities', (route) => route.fulfill({ json: gesperrtePakete() }));
    await page.route('**/api/v1/jobs/job-1/ssh-output', (route) =>
      route.fulfill({ json: { job_id: 'job-1', name: 'Alle Pakete aktualisieren @ web01', status: 'failed', exit_code: 100, output: 'E: Konnte Sperre nicht erhalten' } }),
    );
    await page.goto('/servers/1');
    await expect(karte(page).getByText('E: Konnte Sperre nicht erhalten')).toHaveCount(0);
    await karte(page).getByRole('button', { name: 'Job-Ausgabe anzeigen' }).click();
    await expect(karte(page).getByText('E: Konnte Sperre nicht erhalten')).toBeVisible();
    await karte(page).getByRole('button', { name: 'Job-Ausgabe ausblenden' }).click();
    await expect(karte(page).getByText('E: Konnte Sperre nicht erhalten')).toHaveCount(0);

    await page.route('**/api/v1/auth/me', async (route) => {
      const antwort = await route.fetch();
      const profil = await antwort.json();
      profil.permissions = profil.permissions.filter((recht: string) => recht !== 'jobs:read');
      await route.fulfill({ response: antwort, json: profil });
    });
    await page.reload();
    await expect(karte(page).getByText('dpkg-Sperre durch unattended-upgr (PID 1234)')).toBeVisible();
    await expect(karte(page).getByRole('button', { name: 'Job-Ausgabe anzeigen' })).toHaveCount(0);
  });

  test('Ladefehler wird angezeigt', async ({ page }) => {
    await page.route('**/api/v1/servers/1/capabilities', (route) => route.fulfill({ status: 500, json: { error: 'kaputt' } }));
    await page.goto('/servers/1');
    await expect(karte(page)).toContainText('Anfrage fehlgeschlagen (500)');
  });
});
