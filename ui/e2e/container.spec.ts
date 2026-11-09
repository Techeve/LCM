// Container: Reiter am Server und die Containerseite über alle Server.
import { test, expect } from './fixtures';
import { setzeSchalter } from './helpers';

const tabelle = (page: import('@playwright/test').Page, name: string) => page.getByRole('grid', { name, exact: true });

test.describe('Serverdetail: Container', () => {
  test('zeigt Compose-Projekt, einzelne Container und Images mit Stand und Lücken', async ({ page }) => {
    // web01 betreibt im Demo das Compose-Projekt „webshop“ plus einen einzelnen Container.
    await page.goto('/servers/1');
    await expect(page.getByRole('tab', { name: 'Container' })).toBeVisible();
    await page.getByRole('tab', { name: 'Container' }).click();

    // Compose-Projekt mit Update-Knopf, einzelne Container getrennt.
    await expect(page.getByRole('heading', { level: 3, name: 'Compose-Projekt webshop' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Projekt aktualisieren' })).toBeVisible();
    // Sammelaktion: alle genutzten Registry-Images auf einmal ziehen.
    await expect(page.getByRole('button', { name: 'Alle Images aktualisieren' })).toBeVisible();
    await expect(page.getByRole('heading', { level: 3, name: 'Einzelne Container' })).toBeVisible();
    await expect(tabelle(page, 'Einzelne Container').getByRole('row', { name: /uptime-kuma/ })).toBeVisible();

    // Je Container ein Schalter „In der Ampel“: Docker-Lücken zählen nur für markierte Container.
    const web = tabelle(page, 'Compose-Projekt webshop').getByRole('row', { name: /webshop-web-1/ });
    await expect(web.getByRole('switch')).toBeVisible();
    // CVE-Zähler aus dem Image-Scan (eine hohe Lücke im nginx-Image) an der Container-Zeile.
    await expect(web).toContainText('1 hoch');

    // Images: nginx veraltet (Ziehen), lokales Image gekennzeichnet.
    const nginx = tabelle(page, 'Images').getByRole('row', { name: /^nginx:1\.25/ });
    await expect(nginx).toContainText('Update verfügbar');
    await expect(nginx).toContainText('1 hoch');
    await expect(nginx.getByRole('button', { name: 'Ziehen' })).toBeVisible();
    // Das ungenutzte lokale Image hat „Löschen“, das genutzte nicht.
    const lokal = tabelle(page, 'Images').getByRole('row', { name: /^meinapp:dev/ });
    await expect(lokal).toContainText('lokal gebaut');
    await expect(lokal.getByRole('button', { name: 'Löschen' })).toBeVisible();
    await expect(nginx.getByRole('button', { name: 'Löschen' })).toHaveCount(0);

    // cache01 hat kein Docker: kein Reiter.
    await page.goto('/servers/3');
    await expect(page.getByRole('tab', { name: /^Pakete/ })).toBeVisible();
    await expect(page.getByRole('tab', { name: 'Container' })).toHaveCount(0);
  });
});

test.describe('Containerseite', () => {
  test('Images: eindeutige Images mit Server und Stand; „Nur mit Update“ filtert', async ({ page }) => {
    await page.goto('/containers');
    await expect(page.getByRole('heading', { level: 1, name: 'Container' })).toBeVisible();
    const images = tabelle(page, 'Container-Images aller Server');
    await expect(images.getByRole('row', { name: /^nginx:1\.25/ })).toContainText('Update verfügbar');
    // Die Server stehen mit Namen da, nicht nur als Zahl.
    await expect(images.getByRole('row', { name: /^nginx:1\.25/ }).getByRole('link', { name: 'web01' })).toBeVisible();
    // Ein privates Image (db01) ist anonym nicht prüfbar.
    await expect(images.getByRole('row', { name: /postgres:16/ })).toContainText('Nicht prüfbar (privat)');

    // „Nur mit Update“ reduziert die Liste.
    await setzeSchalter(page.getByRole('switch', { name: 'Nur mit Update' }), true);
    await expect(images.getByRole('row', { name: /^nginx:1\.25/ })).toBeVisible();
    await expect(images.getByRole('row', { name: /^redis:7/ })).toHaveCount(0);
  });

  test('Reiter für Container und Compose, laufende zuerst', async ({ page }) => {
    await page.goto('/containers');
    await page.getByRole('tab', { name: 'Container', exact: true }).click();
    await expect(page).toHaveURL(/\/containers\/instances$/);
    await expect(tabelle(page, 'Container aller Server').getByRole('row', { name: /webshop-web-1/ })).toBeVisible();
    // Standardmäßig nur laufende.
    await expect(page.getByRole('switch', { name: 'Auch beendete und angehaltene zeigen' })).not.toBeChecked();
    await expect(tabelle(page, 'Container aller Server').getByRole('row', { name: /uptime-kuma/ })).toHaveCount(0);

    await page.getByRole('tab', { name: 'Compose' }).click();
    const compose = tabelle(page, 'Compose-Projekte aller Server').getByRole('row', { name: /^webshop/ });
    await expect(compose).toContainText('2 / 2');
    await expect(compose).toContainText('web01');
  });

  test('viele Server je Image werden gekürzt', async ({ page }) => {
    // Die Demo-Daten haben je Image nur einen Server: Mehrere gibt es nur mit einer gestellten Antwort.
    const namen = ['alpha', 'beta', 'gamma', 'delta', 'epsilon'];
    await page.route('**/api/v1/docker/overview', (route) =>
      route.fulfill({
        json: [
          {
            repository: 'nginx',
            tag: '1.25',
            server_count: 5,
            servers: namen.map((name, index) => ({ id: index + 1, name })),
            in_use_count: 5,
            update_available: false,
            unverifiable: false,
            local_only: false,
            critical_vulns: 0,
            high_vulns: 0,
          },
          {
            repository: 'redis',
            tag: '7',
            server_count: 2,
            servers: namen.slice(0, 2).map((name, index) => ({ id: index + 1, name })),
            in_use_count: 2,
            update_available: false,
            unverifiable: false,
            local_only: false,
            critical_vulns: 0,
            high_vulns: 0,
          },
        ],
      }),
    );
    await page.goto('/containers');
    const images = tabelle(page, 'Container-Images aller Server');

    // Bis drei Server stehen als Links in der Zeile; bei mehr heißt es „+2 weitere“.
    const redis = images.getByRole('row', { name: /^redis:7/ });
    await expect(redis.getByRole('link')).toHaveCount(2);
    await expect(redis.getByText('weitere')).toHaveCount(0);
    const nginx = images.getByRole('row', { name: /^nginx:1\.25/ });
    for (const name of namen.slice(0, 3)) await expect(nginx.getByRole('link', { name })).toBeVisible();
    await expect(nginx.getByRole('link', { name: 'delta' })).toHaveCount(0);
    const mehr = nginx.getByRole('button', { name: '+2 weitere' });
    await expect(mehr).toBeVisible();

    // Die Links führen auf die Serverseite.
    await nginx.getByRole('link', { name: 'gamma' }).click();
    await expect(page).toHaveURL(/\/servers\/3$/);
  });

  test('„+2 weitere“ klappt alle Server auf, mit Maus und Tastatur', async ({ page }) => {
    const namen = ['alpha', 'beta', 'gamma', 'delta', 'epsilon'];
    await page.route('**/api/v1/docker/overview', (route) =>
      route.fulfill({
        json: [
          {
            repository: 'nginx',
            tag: '1.25',
            server_count: 5,
            servers: namen.map((name, index) => ({ id: index + 1, name })),
            in_use_count: 5,
            update_available: false,
            unverifiable: false,
            local_only: false,
            critical_vulns: 0,
            high_vulns: 0,
          },
        ],
      }),
    );
    await page.goto('/containers');
    const zeile = tabelle(page, 'Container-Images aller Server').getByRole('row', { name: /^nginx:1\.25/ });
    const mehr = zeile.getByRole('button', { name: '+2 weitere' });
    const liste = page.getByRole('group', { name: 'Alle 5 Server' });
    await expect(liste).toBeHidden();

    // Tastatur: Enter öffnet, der Fokus liegt auf dem ersten Link, Pfeil und Ende springen, Esc schließt.
    await mehr.focus();
    await page.keyboard.press('Enter');
    await expect(liste).toBeVisible();
    for (const name of namen) await expect(liste.getByRole('link', { name })).toBeVisible();
    await expect(liste.getByRole('link', { name: 'alpha' })).toBeFocused();
    await page.keyboard.press('ArrowDown');
    await expect(liste.getByRole('link', { name: 'beta' })).toBeFocused();
    await page.keyboard.press('End');
    await expect(liste.getByRole('link', { name: 'epsilon' })).toBeFocused();
    await page.keyboard.press('Escape');
    await expect(liste).toBeHidden();
    await expect(mehr).toBeFocused();

    // Maus: öffnen, einen Server aus dem Rest anklicken.
    await mehr.click();
    await liste.getByRole('link', { name: 'delta' }).click();
    await expect(page).toHaveURL(/\/servers\/4$/);
  });
});
