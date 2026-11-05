// Dashboard und Serverliste: Zustand der Flotte, Filter in der Adresse,
// Kennzeichen für nicht erreichbare Server.
import { test, expect } from './fixtures';
import { veraendereServer } from './helpers';

const zeile = (page: import('@playwright/test').Page, name: string) =>
  page.getByRole('row').filter({ has: page.getByRole('gridcell', { name, exact: false }) });

test.describe('Dashboard', () => {
  test('zeigt den Zustand der Flotte und was Aufmerksamkeit braucht', async ({ page }) => {
    await page.goto('/');
    await expect(page.getByRole('heading', { level: 1, name: 'Dashboard' })).toBeVisible();
    await expect(page.getByText('Zustand der Flotte · 12 Server')).toBeVisible();

    // Mindestens ein kritischer und ein Warn-Server, jeweils mit Weg zur Ursache.
    const aufmerksamkeit = page.getByRole('list').filter({ has: page.getByRole('link', { name: 'cache01' }) });
    await expect(aufmerksamkeit.getByRole('img', { name: 'kritisch' }).first()).toBeVisible();
    await expect(aufmerksamkeit.getByRole('img', { name: 'Warnung' }).first()).toBeVisible();
    await expect(aufmerksamkeit.getByRole('link', { name: 'web01' })).toBeVisible();
    await expect(aufmerksamkeit.getByRole('link', { name: 'Sicherheit öffnen' }).first()).toBeVisible();
  });

  test('die Kacheln führen in die gefilterte Serverliste', async ({ page }) => {
    await page.goto('/');
    await page.getByRole('link', { name: /kritisch Jetzt handeln/ }).click();
    await expect(page).toHaveURL(/\/servers\?status=red$/);
    await expect(page.getByRole('combobox', { name: 'Status' })).toHaveValue('red');
    await expect(page.getByRole('row').filter({ hasText: 'cache01' })).toBeVisible();
    await expect(page.getByRole('row').filter({ hasText: 'db01' })).toHaveCount(0);
  });
});

test.describe('Serverliste', () => {
  test('Filter nach Name, Status und System stehen in der Adresse', async ({ page }) => {
    await page.goto('/servers');
    await expect(page.getByText('12 Server')).toBeVisible();

    // Namenssuche grenzt auf web01 ein.
    await page.getByRole('searchbox', { name: 'Name oder Host' }).fill('web');
    await expect(page).toHaveURL(/[?&]q=web/);
    await expect(zeile(page, 'web01')).toBeVisible();
    await expect(zeile(page, 'db01')).toHaveCount(0);
    await expect(page.getByText('1 Server', { exact: true })).toBeVisible();

    // Wer die Adresse kopiert, sieht dieselbe Ansicht.
    await page.reload();
    await expect(page.getByRole('searchbox', { name: 'Name oder Host' })).toHaveValue('web');
    await expect(zeile(page, 'db01')).toHaveCount(0);

    // Status „Kritisch“ zeigt die roten Server, nicht die gelben.
    await page.goto('/servers');
    await page.getByRole('combobox', { name: 'Status' }).selectOption({ label: 'Kritisch' });
    await expect(zeile(page, 'cache01')).toBeVisible();
    await expect(zeile(page, 'db01')).toHaveCount(0);

    // System „Ubuntu“ (nach dem Zurücksetzen) zeigt db01, nicht web01.
    await page.goto('/servers');
    await page.getByRole('combobox', { name: 'System' }).selectOption({ label: 'Ubuntu' });
    await expect(zeile(page, 'db01')).toBeVisible();
    await expect(zeile(page, 'web01')).toHaveCount(0);
  });

  test('ohne Treffer bietet die Liste das Zurücksetzen an', async ({ page }) => {
    await page.goto('/servers?q=gibt-es-nicht');
    await expect(page.getByText('Kein Server passt zu diesen Filtern.')).toBeVisible();
    await page.getByRole('button', { name: 'Filter zurücksetzen' }).click();
    await expect(page.getByText('12 Server')).toBeVisible();
    await expect(page).toHaveURL(/\/servers$/);
  });

  test('Filter „LCM handlungsunfähig“ zeigt nur diese Server', async ({ page }) => {
    // Welcher Server handlungsunfähig ist, lässt sich in den Demo-Daten nicht
    // herstellen: Die Antwort für incapable=1 kommt deshalb gestellt.
    await page.route(/\/api\/v1\/servers\?.*incapable=1/, async (route) => {
      const url = new URL(route.request().url());
      url.searchParams.delete('incapable');
      const antwort = await route.fetch({ url: url.toString() });
      const koerper = await antwort.json();
      const web01 = koerper.items.find((server: { name: string }) => server.name === 'web01');
      await route.fulfill({ json: { ...koerper, items: [{ ...web01, incapable: true }], total: 1 } });
    });
    await page.goto('/servers');
    await page.getByRole('combobox', { name: 'Status' }).selectOption({ label: 'LCM handlungsunfähig' });
    await expect(page).toHaveURL(/incapable=1/);
    await expect(page.getByRole('grid', { name: 'Serverliste' }).getByRole('row')).toHaveCount(2); // Kopf + web01
    await expect(zeile(page, 'web01')).toBeVisible();
  });

  test('ein tolerierter Ausfall dämpft den Server, ein echter zeigt rot', async ({ page }) => {
    await page.goto('/servers');
    // edge01 ist offline, aber als unkritisch toleriert: Kennzeichen, Ampel unverändert.
    const edge = zeile(page, 'edge01');
    await expect(edge.getByRole('img', { name: 'Offline' })).toBeVisible();
    await expect(edge.getByRole('gridcell', { name: 'Hervorragend' })).toBeVisible();
    await edge.getByRole('img', { name: 'Offline' }).hover();
    await expect(page.getByText(/als unkritisch markiert/)).toBeVisible();

    // cache01 ist schlicht ausgefallen: Kennzeichen UND roter Status.
    const cache = zeile(page, 'cache01');
    await expect(cache.getByRole('img', { name: 'Offline' })).toBeVisible();
    await expect(cache.getByRole('gridcell', { name: 'Kritisch' })).toBeVisible();
    await cache.getByRole('img', { name: 'Offline' }).hover();
    await expect(page.getByText(/in Folge fehlgeschlagen/)).toBeVisible();

    // Erreichbare Server tragen es nicht.
    await expect(zeile(page, 'web01').getByRole('img', { name: 'Offline' })).toHaveCount(0);

    // Der Filter „Kritisch“ listet edge01 nicht.
    await page.getByRole('combobox', { name: 'Status' }).selectOption({ label: 'Kritisch' });
    await expect(zeile(page, 'cache01')).toBeVisible();
    await expect(zeile(page, 'edge01')).toHaveCount(0);
  });

  test('Befund nennt Dauer und Ursache des Ausfalls', async ({ page }) => {
    const vorStunden = (stunden: number) => new Date(Date.now() - stunden * 3600 * 1000).toISOString();
    await veraendereServer(page, 1, (server) => ({
      ...server,
      status: 'red',
      incapable: true,
      insights: [
        {
          severity: 'critical',
          message: 'x',
          key: 'actionFailed.packages',
          params: { count: 3, since: vorStunden(49), causeKey: 'lock_busy', pid: '1234', process: 'unattended-upgr' },
        },
      ],
    }));
    await page.goto('/servers/1');
    await page.getByRole('button', { name: 'Kritisch: Befunde zeigen' }).click();
    // Die Liste nennt den Befund auch im Hinweis an der Ampel; hier zählt die geöffnete Blase.
    await expect(
      page.getByRole('paragraph').filter({
        hasText: 'Updates schlagen seit 2 Tagen fehl (3×): dpkg-Sperre durch unattended-upgr (PID 1234)',
      }),
    ).toBeVisible();
  });
});
