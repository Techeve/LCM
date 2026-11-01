// Betriebsfähigkeit: Karte auf der Serverseite, Dashboard-Filter und Befundtext.
//
// Die Antworten werden gestellt (page.route): Welche Kategorie gerade
// fehlschlägt, ist Zustand der Datenbank und lässt sich in den Demo-Daten
// nicht auf Zuruf herstellen. Die Seiten selbst laufen gegen das echte Binary.
import { test, expect } from './fixtures.js';

const ISO_AGO = (hours) => new Date(Date.now() - hours * 3600 * 1000).toISOString();

const ALL_OK = ['connection', 'packages', 'rules', 'cve_scan'].map((category) => ({
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
function lockedPackages() {
  return ALL_OK.map((c) =>
    c.category !== 'packages'
      ? c
      : {
          ...c,
          state: 'failed',
          effective: 'critical',
          failing_since: ISO_AGO(49),
          consecutive_failures: 3,
          last_success_at: ISO_AGO(100),
          last_error_class: 'lock_busy',
          last_error_message: 'PID 1234 (unattended-upgr)',
          last_error_params: { pid: '1234', process: 'unattended-upgr' },
          last_job_id: 'job-1',
          last_job_name: 'Alle Pakete aktualisieren @ web01',
          warning_class: 'repo_error',
          warning_message: 'http://old.example/ubuntu 404 Not Found',
          stale: 'warning',
          repair: 'dpkg_configure',
          retryable: true,
        },
  );
}

test.describe('Betriebsfähigkeit', () => {
  test('Karte zeigt Ursache, Dauer und Hinweis; Reparatur braucht Bestätigung', async ({ page }) => {
    await page.route('**/api/v1/servers/1/capabilities', (r) => r.fulfill({ json: lockedPackages() }));
    await page.goto('/#/servers/1');

    const card = page.getByTestId('capability-card');
    await expect(card).toBeVisible();
    await expect(page.getByTestId('capability-overall')).toHaveAttribute('data-level', 'critical');

    const row = page.getByTestId('capability-packages');
    await expect(row).toContainText('seit 2 Tagen');
    await expect(row).toContainText('3 Fehlschläge in Folge');
    await expect(page.getByTestId('capability-packages-cause')).toContainText('dpkg-Sperre durch unattended-upgr (PID 1234)');
    // Bei lock_busy steht der Halter schon in der Ursache - kein doppelter Auszug.
    await expect(page.getByTestId('capability-packages-excerpt')).toHaveCount(0);
    await expect(page.getByTestId('capability-packages-stale')).toContainText('seit 4 Tagen');
    await expect(page.getByTestId('capability-packages-warning')).toContainText('old.example');
    // Gesunde Kategorien bleiben knapp: keine Fehlerfelder.
    await expect(page.getByTestId('capability-rules')).not.toContainText('Letzter Fehler');

    // Hover-Hilfe an den Knöpfen.
    await expect(page.getByTestId('capability-packages-repair')).toHaveAttribute('title', /dpkg --configure -a/);
    await expect(page.getByTestId('capability-packages-retry')).toHaveAttribute('title', /protokollierter Job/);

    // Reparatur: erst Dialog, dann (erst nach Bestätigung) die Anfrage.
    let repairBody = null;
    await page.route('**/api/v1/servers/1/packages/repair', (r) => {
      repairBody = r.request().postDataJSON();
      return r.fulfill({ status: 202, json: { status: 'started', job_id: 'job-2', job_name: 'Paketverwaltung reparieren' } });
    });
    await page.getByTestId('capability-packages-repair').click();
    await expect(page.getByTestId('capability-confirm-text')).toContainText('NICHT automatisch wiederholt');
    expect(repairBody).toBeNull();

    const sent = page.waitForRequest('**/api/v1/servers/1/packages/repair');
    await page.getByTestId('capability-confirm-run').click();
    await sent;
    expect(repairBody).toEqual({ action: 'dpkg_configure' });
  });

  test('Job-Ausgabe und Jobliste gibt es nur mit jobs:read', async ({ page }) => {
    await page.route('**/api/v1/servers/1/capabilities', (r) => r.fulfill({ json: lockedPackages() }));
    await page.goto('/#/servers/1');
    const row = page.getByTestId('capability-packages');
    await expect(row.getByTestId('capability-packages-output-toggle')).toBeVisible();
    await expect(row.getByRole('link', { name: 'Job-Historie' })).toBeVisible();

    // Dieselbe Sitzung ohne das Recht: beide Verweise entfallen, der Rest bleibt.
    await page.addInitScript(() => {
      const user = JSON.parse(localStorage.getItem('lcm.user'));
      user.permissions = user.permissions.filter((p) => p !== 'jobs:read');
      localStorage.setItem('lcm.user', JSON.stringify(user));
    });
    await page.reload();
    await expect(row.getByTestId('capability-packages-cause')).toBeVisible();
    await expect(row.getByTestId('capability-packages-output-toggle')).toHaveCount(0);
    await expect(row.getByRole('link', { name: 'Job-Historie' })).toHaveCount(0);
  });

  test('Alles in Ordnung: eine Zeile je Kategorie, keine Knöpfe', async ({ page }) => {
    await page.route('**/api/v1/servers/1/capabilities', (r) => r.fulfill({ json: ALL_OK }));
    await page.goto('/#/servers/1');
    await expect(page.getByTestId('capability-overall')).toHaveAttribute('data-level', 'ok');
    await expect(page.getByTestId('capability-card').locator('li')).toHaveCount(4);
    await expect(page.getByTestId('capability-card').getByRole('button')).toHaveCount(0);
  });

  test('Ladefehler wird angezeigt', async ({ page }) => {
    await page.route('**/api/v1/servers/1/capabilities', (r) =>
      r.fulfill({ status: 500, json: { error: 'kaputt' } }),
    );
    await page.goto('/#/servers/1');
    await expect(page.getByTestId('capability-error')).toContainText('kaputt');
  });

  test('Dashboard: Filter zeigt nur handlungsunfähige Server', async ({ page }) => {
    await page.route('**/api/v1/servers/statuses', async (r) => {
      const res = await r.fetch();
      const all = await res.json();
      await r.fulfill({ json: all.map((s) => ({ ...s, incapable: s.id === 1 })) });
    });
    await page.goto('/#/');
    await expect(page.locator('tbody tr').nth(1)).toBeVisible();
    const before = await page.locator('tbody tr').count();
    expect(before).toBeGreaterThan(1);

    await page.getByTestId('filter-incapable').check();
    await expect(page.locator('tbody tr')).toHaveCount(1);
    await expect(page.locator('tbody')).toContainText('web01');

    await page.getByRole('button', { name: 'Reset' }).click();
    await expect(page.locator('tbody tr')).toHaveCount(before);
  });

  test('Ampel-Befund nennt Dauer und Ursache', async ({ page }) => {
    await page.route('**/api/v1/servers/statuses', async (r) => {
      const res = await r.fetch();
      const all = await res.json();
      await r.fulfill({
        json: all.map((s) =>
          s.id !== 1
            ? s
            : {
                ...s,
                status: 'red',
                incapable: true,
                insights: [
                  {
                    severity: 'critical',
                    message: 'x',
                    key: 'actionFailed.packages',
                    params: {
                      count: 3,
                      since: ISO_AGO(49),
                      causeKey: 'lock_busy',
                      pid: '1234',
                      process: 'unattended-upgr',
                    },
                  },
                ],
              },
        ),
      });
    });
    await page.goto('/#/');
    const row = page.locator('tbody tr', { hasText: 'web01' });
    // Erst in Sicht holen und warten: Das Popover schließt sich beim Scrollen,
    // und das automatische Scrollen des Klicks liefe sonst noch nach.
    const info = row.getByRole('button', { name: /Warum|ⓘ/ });
    await info.scrollIntoViewIfNeeded();
    await expect(info).toBeInViewport();
    await info.click();
    await expect(page.getByRole('dialog')).toContainText(
      'Updates schlagen seit 2 Tagen fehl (3×): dpkg-Sperre durch unattended-upgr (PID 1234)',
    );
  });
});
