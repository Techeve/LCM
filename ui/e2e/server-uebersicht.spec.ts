// Serverdetail, Kopf und Übersicht: Zustand, Plattform, Aktionen, Einstellungen.
import { test, expect } from './fixtures';
import { hinweis } from './helpers';

const kopf = (page: import('@playwright/test').Page) => page.getByRole('heading', { level: 2 }).first();

test.describe('Serverdetail: Übersicht', () => {
  test('Hardware, Plattform, Support und die Aktionen im Kopf', async ({ page }) => {
    await page.goto('/servers/1');
    await expect(kopf(page)).toHaveText('web01');
    await expect(page.getByRole('heading', { level: 3, name: 'Hardware' })).toBeVisible();

    // web01 ist im Demo eine VM (KVM), Debian 12 (unterstützt, neuere Version verfügbar).
    await expect(page.getByText('Virtuelle Maschine (KVM)')).toBeVisible();
    await expect(page.getByText('unterstützt bis 2028-06', { exact: true })).toBeVisible();
    await expect(page.getByText('Neuere Version Debian 13 verfügbar.')).toBeVisible();

    // web01 ist im Demo gehärtet: der Knopf bietet das Aufheben an.
    await expect(page.getByRole('button', { name: 'Härtung aufheben' })).toBeVisible();

    // Seltener genutzte Aktionen stecken im Menü „Aktionen“.
    await page.getByRole('button', { name: 'Aktionen' }).click();
    await expect(page.getByRole('menuitem', { name: 'Benutzer abgleichen' })).toBeVisible();
    await expect(page.getByRole('menuitem', { name: 'Server neu starten' })).toBeVisible();

    // db01 ist ein LXC-Container.
    await page.goto('/servers/2');
    await expect(page.getByText('Container (LXC)', { exact: true })).toBeVisible();
  });

  test('die Ampel im Kopf nennt den Grund', async ({ page }) => {
    // web01 hat im Demo eine KRITISCHE CVE: die Blase an der Ampel nennt sie.
    await page.goto('/servers/1');
    await page.getByRole('button', { name: 'Kritisch: Befunde zeigen' }).click();
    await expect(page.getByRole('paragraph').filter({ hasText: /kritische Sicherheitslücke/ })).toBeVisible();
  });

  test('Docker-Ports, die ufw umgehen, werden an der Firewall benannt', async ({ page }) => {
    // web01 hat ufw aktiv (nur 22) UND einen Container auf 0.0.0.0:80 - genau
    // die Konstellation, für die LCM früher „Firewall aktiv, nur 22“ meldete,
    // obwohl Port 80 von außen erreichbar war.
    await page.goto('/servers/1');
    await expect(page.getByText('1 Docker-Port umgeht diese Firewall')).toBeVisible();
    await expect(page.getByText(/Keiner ist an eine bestimmte Adresse gebunden/)).toBeVisible();
    // Die einzelnen Ports stehen erst nach dem Aufklappen da - es können Dutzende sein.
    const zeile = page.getByText('webshop-web-1 · 80/tcp');
    await expect(zeile).toBeHidden();
    await page.getByText('Ports anzeigen').click();
    await expect(zeile).toBeVisible();
  });

  test('Post-Quanten-Zustand des Schlüsselaustauschs', async ({ page }) => {
    // web01 verbindet klassisch, der Regelfall auf OpenSSH älter als 9.9.
    await page.goto('/servers/1');
    await expect(page.getByText('curve25519-sha256', { exact: true })).toBeVisible();
    await expect(page.getByText('klassisch', { exact: true })).toBeVisible();
    await expect(page.getByText('quantensicher', { exact: true })).toHaveCount(0);

    // db01 handelt ML-KEM aus: dort keine Beanstandung.
    await page.goto('/servers/2');
    await expect(page.getByText('mlkem768x25519-sha256', { exact: true })).toBeVisible();
    await expect(page.getByText('quantensicher', { exact: true })).toBeVisible();
    await expect(page.getByText('klassisch', { exact: true })).toHaveCount(0);
  });

  test('Befund zum Zustand der Speicher-Verbünde steht als Satz an der Ampel', async ({ page }) => {
    // db01 hat im Demo einen ZFS-Mirror ohne Redundanz; der Befund färbt den Server.
    await page.goto('/servers/2');
    await page.getByRole('button', { name: 'Warnung: Befunde zeigen' }).click();
    const befund = page.getByRole('paragraph').filter({ hasText: 'ZFS-Pool tank' });
    await expect(befund).toBeVisible();
    // Als übersetzter Satz, nicht als roher Schlüssel.
    await expect(befund).not.toContainText('insights.');
  });

  test('Post-Quanten-Befund steht als Hinweis in der Ampel', async ({ page }) => {
    await page.goto('/servers/1');
    await page.getByRole('button', { name: 'Kritisch: Befunde zeigen' }).click();
    const befund = page.getByRole('paragraph').filter({ hasText: 'Post-Quanten' });
    await expect(befund).toBeVisible();
    await expect(befund).not.toContainText('insights.');
  });

  test('ein tolerierter Offline-Server zeigt das Band mit Grund und die Frist in den Einstellungen', async ({ page }) => {
    await page.goto('/servers/7');
    await expect(kopf(page)).toHaveText('edge01');
    await expect(hinweis(page, /Nicht erreichbar/)).toBeVisible();

    await page.getByRole('button', { name: 'Einstellungen' }).click();
    await page.getByRole('tab', { name: 'Betrieb' }).click();
    await expect(page.getByRole('switch', { name: 'Nichterreichbarkeit unkritisch' })).toBeChecked();
    await expect(page.getByRole('textbox', { name: 'Frist bis kritisch (Tage)' })).toBeVisible();
  });

  test('ein ausgefallener Server zeigt das Band auch in der Detailansicht', async ({ page }) => {
    await page.goto('/servers/3');
    await expect(kopf(page)).toHaveText('cache01');
    await expect(hinweis(page, /Nicht erreichbar/)).toBeVisible();
  });

  test('LCM-Host: Karte mit Trivy und apt-cacher-ng, im Container mit Erklärung', async ({ page }) => {
    await page.goto('/servers');
    await page.getByRole('row').filter({ hasText: 'lcm-host' }).getByRole('gridcell', { name: 'lcm-host' }).click();
    await expect(page).toHaveURL(/\/servers\/\d+$/);
    await expect(page.getByText('Auf diesem Server läuft LCM selbst.')).toBeVisible();

    // Die Karte zeigt je nach Betriebsart Verschiedenes, und beides ist richtig:
    // Läuft LCM selbst in einem Container, gibt es dort nichts einzurichten (kein
    // apt, kein Dienst, der den Neustart überlebt) - dann erklärt die Karte das,
    // statt Knöpfe anzubieten, die scheitern müssen. Auf einem gewöhnlichen Host
    // stehen die Einrichtungs-Zustände. Der Test fährt das echte Binary und kann
    // nicht wählen, wo es läuft, deshalb gelten beide Fälle.
    const imContainer = hinweis(page, 'LCM läuft in einem Container');
    const trivy = page.getByText('Trivy CVE-Scanner');
    await expect(imContainer.or(trivy).first()).toBeVisible();
    if (!(await imContainer.isVisible())) {
      await expect(page.getByText('apt-cacher-ng')).toBeVisible();
    }
  });

  test('LCM-Host: eine neuere Trivy-Version steht an Trivy, mit Knopf zum Aktualisieren', async ({ page }) => {
    // Der Stand kommt aus dem Paketbestand des Hosts; hier vorgegeben, damit der Test nicht davon abhängt,
    // ob auf der Testmaschine gerade ein neueres Trivy im Paketarchiv liegt.
    await page.route('**/api/v1/servers/*/lcm-host/status', (route) =>
      route.fulfill({
        json: {
          is_lcm_host: true,
          package_manager: 'apt',
          trivy_installed: true,
          scanner: { available: true, version: '0.67.2', update_version: '0.68.1', freshness: 'fresh', updated_at: new Date().toISOString(), sandboxed: true, sandbox_backend: 'bubblewrap' },
        },
      }),
    );
    await page.goto('/servers');
    await page.getByRole('row').filter({ hasText: 'lcm-host' }).getByRole('gridcell', { name: 'lcm-host' }).click();
    await expect(page.getByText('Version 0.68.1 verfügbar')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Auf Trivy 0.68.1 aktualisieren' })).toBeVisible();
  });

  test('Proxmox: Erkennung mit Typ und Version, gesperrte Aktionen', async ({ page }) => {
    // Die Liste zeigt das Produkt statt des generischen Debian.
    await page.goto('/servers');
    await expect(page.getByRole('row').filter({ hasText: 'pve01' }).getByRole('gridcell', { name: 'Proxmox VE 8.2.4' })).toBeVisible();

    // Detailseite: Kennzeichen mit Produkt und Version.
    await page.goto('/servers/6');
    await expect(page.getByText('Proxmox VE 8.2.4').first()).toBeVisible();

    // Firewall und Benutzerabgleich sind auf Proxmox gesperrt.
    await expect(page.getByRole('button', { name: /^Firewall/ })).toBeVisible();
    await page.getByRole('button', { name: 'Aktionen' }).click();
    await expect(page.getByRole('menuitem', { name: 'Benutzer abgleichen' })).toHaveCount(0);
    await page.keyboard.press('Escape');

    // Repositories: der Sperr-Hinweis statt des Formulars.
    await page.getByRole('tab', { name: 'Repositories' }).click();
    await expect(page.getByText('Proxmox verwaltet seine Paketquellen selbst.')).toBeVisible();
  });
});
