// Serverdetail: Pakete, Pins, Kernel, Snaps, Repositories, Deep-Scan.
import { test, expect } from './fixtures';
import { bestaetige, fenster, setzeSchalter } from './helpers';

const paketTabelle = (page: import('@playwright/test').Page) => page.getByRole('grid', { name: 'Pakete', exact: true });

test.describe('Serverdetail: Pakete', () => {
  test('bietet Update-Aktionen oben und je Paket Version und Schutz', async ({ page }) => {
    await page.goto('/servers/1/packages');
    // Ad-hoc-Aktionen oben.
    for (const aktion of ['Paketliste aktualisieren', 'Alle Updates einspielen', 'Nur Sicherheitsupdates', 'Nicht benötigte Pakete entfernen']) {
      await expect(page.getByRole('button', { name: aktion })).toBeVisible();
    }
    // Standardmäßig nur Pakete mit Update; je Paket Version und Schutz vor dem Aufräumen.
    const openssl = paketTabelle(page).getByRole('row').filter({ hasText: 'openssl' });
    await expect(openssl).toContainText('3.0.14-1');
    await expect(openssl).toContainText('Sicherheit');
    await expect(openssl.getByRole('button', { name: 'Version …' })).toBeVisible();
    await expect(openssl.getByRole('button', { name: 'openssl vor dem Aufräumen schützen' })).toBeVisible();
  });

  test('alle Pakete zeigen und nach Namen suchen', async ({ page }) => {
    await page.goto('/servers/1/packages');
    // web01-Seed: openssl hat ein Update, curl und nginx nicht.
    await expect(paketTabelle(page).getByRole('row', { name: /^Paket openssl/ })).toBeVisible();
    await expect(paketTabelle(page).getByRole('row', { name: /^Paket nginx/ })).toHaveCount(0);
    await setzeSchalter(page.getByRole('switch', { name: 'Alle Pakete zeigen' }), true);
    await expect(paketTabelle(page).getByRole('row', { name: /^Paket nginx/ })).toBeVisible();
    // Die Liste filtert schon beim Tippen, ohne Enter und ohne das Feld zu verlassen.
    await page.getByRole('searchbox', { name: 'Paket suchen' }).pressSequentially('ssl');
    await expect(paketTabelle(page).getByRole('row').filter({ hasText: 'openssl' })).toBeVisible();
    await expect(paketTabelle(page).getByRole('row').filter({ hasText: 'nginx' })).toHaveCount(0);
  });

  test('Sortierung über die Tabellenköpfe holt Updates nach oben', async ({ page }) => {
    await page.goto('/servers/1/packages');
    await setzeSchalter(page.getByRole('switch', { name: 'Alle Pakete zeigen' }), true);
    const zeilen = paketTabelle(page).getByRole('row');
    await expect(zeilen.nth(1)).toBeVisible();
    // Nach „Verfügbar“ absteigend steht oben, was ein Update hat.
    await paketTabelle(page).getByRole('button', { name: 'Verfügbar' }).click();
    const erste = (await zeilen.nth(1).innerText()).trim();
    await paketTabelle(page).getByRole('button', { name: 'Verfügbar' }).click();
    // Der zweite Klick dreht die Richtung um.
    await expect(zeilen.nth(1)).not.toHaveText(erste);
  });

  test('Rocky und openSUSE: Paketverwaltung und verfügbare Updates', async ({ page }) => {
    // rocky01 (Rocky Linux, dnf): Paketverwaltung wird erkannt, das Update steht da.
    await page.goto('/servers/4');
    await expect(page.getByText('DNF (RPM)')).toBeVisible();
    await page.getByRole('tab', { name: /^Pakete/ }).click();
    const openssl = paketTabelle(page).getByRole('row', { name: /^Paket openssl/ });
    await expect(openssl).toContainText('3.0.7-27.el9');
    // suse01 (openSUSE Leap, zypper).
    await page.goto('/servers/5');
    await expect(page.getByText('Zypper (RPM)')).toBeVisible();
  });

  test('Snaps gibt es nur bei Snap-Servern, mit Version, Kanal und Update', async ({ page }) => {
    // db01 (Ubuntu) hat ZWEI Paketverwaltungen, apt UND snap: Beide werden getrennt ausgewiesen.
    await page.goto('/servers/2');
    await expect(page.getByText('APT (dpkg)')).toBeVisible();
    await page.getByRole('tab', { name: 'Snaps' }).click();
    const snaps = page.getByRole('grid', { name: 'Snaps' });
    await expect(snaps.getByRole('columnheader', { name: 'Kanal' })).toBeVisible();
    const lxd = snaps.getByRole('row').filter({ hasText: 'lxd' });
    await expect(lxd).toContainText('5.0/stable');
    await expect(lxd).toContainText('5.0.3'); // verfügbares Update

    // web01 (Debian) hat keine Snaps: kein Reiter.
    await page.goto('/servers/1');
    await expect(page.getByRole('tab', { name: /^Pakete/ })).toBeVisible();
    await expect(page.getByRole('tab', { name: 'Snaps' })).toHaveCount(0);
  });
});

test.describe('Serverdetail: Paket-Pins', () => {
  test('Kernel schützen, Paket pinnen, Entfernen gesperrt, Pins wieder entfernen', async ({ page }) => {
    // web01 (apt): Dort greift der Kernel-Vorschlag linux-image-*/linux-headers-*.
    await page.goto('/servers/1/packages');
    await page.getByRole('button', { name: /^Pins/ }).click();
    const pins = fenster(page, 'Paket-Pins');
    await expect(pins.getByText('Noch keine Pins')).toBeVisible();

    // Der Ein-Klick-Kernelschutz legt die Muster als „nicht entfernen“ an.
    await pins.getByRole('button', { name: 'Kernel schützen' }).click();
    const tabelle = pins.getByRole('grid', { name: 'Paket-Pins' });
    await expect(tabelle.getByRole('row').filter({ hasText: 'linux-image-*' })).toContainText('nicht entfernen');
    await expect(tabelle.getByRole('row').filter({ hasText: 'linux-headers-*' })).toBeVisible();
    // Bewusst KEIN Einfrieren: Das nähme dem Kernel die Sicherheitsupdates.
    await expect(tabelle).not.toContainText('Version einfrieren');

    // Eigenes Muster anlegen und wieder entfernen.
    await pins.getByRole('textbox', { name: 'Paket oder Muster' }).fill('nginx');
    await pins.getByRole('button', { name: 'Pin anlegen' }).click();
    await expect(tabelle.getByRole('row', { name: /^nginx / })).toBeVisible();

    // Aufräumen: Der Test hinterlässt seine Ausgangslage.
    for (const muster of ['nginx', 'linux-headers-*', 'linux-image-*']) {
      await tabelle.getByRole('button', { name: `Pin ${muster} entfernen` }).click();
      await bestaetige(page, `Pin „${muster}“ entfernen?`, 'Pin entfernen');
      await expect(tabelle.getByRole('button', { name: `Pin ${muster} entfernen` })).toHaveCount(0);
    }
    await expect(pins.getByText('Noch keine Pins')).toBeVisible();
  });

  test('ein gepinntes Paket ist in der Liste gekennzeichnet', async ({ page }) => {
    await page.goto('/servers/1/packages');
    await page.getByRole('button', { name: 'openssl vor dem Aufräumen schützen' }).click();
    const zeile = paketTabelle(page).getByRole('row').filter({ hasText: 'openssl' });
    await expect(zeile).toContainText('gepinnt');

    // Aufräumen: Pin im Fenster wieder entfernen.
    await page.getByRole('button', { name: /^Pins/ }).click();
    const pins = fenster(page, 'Paket-Pins');
    await pins.getByRole('button', { name: 'Pin openssl entfernen' }).click();
    await bestaetige(page, 'Pin „openssl“ entfernen?', 'Pin entfernen');
    await expect(pins.getByText('Noch keine Pins')).toBeVisible();
  });

  test('auf Proxmox sind Pins ausgenommen', async ({ page }) => {
    await page.goto('/servers/6/packages');
    await page.getByRole('button', { name: /^Pins/ }).click();
    await expect(fenster(page, 'Paket-Pins').getByText(/Auf Proxmox ausgenommen/)).toBeVisible();
    await expect(fenster(page, 'Paket-Pins').getByRole('button', { name: 'Kernel schützen' })).toHaveCount(0);
    await expect(fenster(page, 'Paket-Pins').getByRole('button', { name: 'Pin anlegen' })).toHaveCount(0);
  });
});

test.describe('Serverdetail: Kernel', () => {
  test('Proxmox listet installierte Kernel und markiert den laufenden', async ({ page }) => {
    await page.goto('/servers/6');
    // Der LAUFENDE Kernel steht in der Übersicht (aus uname -r, nicht aus der
    // Paketliste). Er ist hier bewusst NICHT der neueste installierte.
    await expect(page.getByText('6.8.12-1-pve', { exact: true }).first()).toBeVisible();
    const tabelle = page.locator('lcm-card').filter({ has: page.getByRole('heading', { level: 3, name: 'Installierte Kernel' }) });
    const zeilen = tabelle.getByRole('row');
    // Kopf + 4 Kernel.
    await expect(zeilen).toHaveCount(5);
    // Neueste Fassung zuerst; sie wartet auf den Neustart und ist keine
    // „Rückfallebene“: Dieser Unterschied ist der Punkt der Anzeige.
    await expect(zeilen.nth(1)).toContainText('6.8.12-4-pve');
    await expect(zeilen.nth(1)).toContainText('wartet auf Neustart');
    await expect(zeilen.nth(2)).toContainText('läuft');
    await expect(zeilen.nth(3)).toContainText('Rückfallebene');
    // Nur der ÄLTESTE ist Ballast: laufender Kernel, neuerer und die nächstältere Rückfallebene bleiben stehen.
    await expect(zeilen.nth(4)).toContainText('entfernbar');
    await expect(tabelle.getByRole('button', { name: '1 alten Kernel entfernen' })).toBeVisible();
    // Proxmox-Namensgebung, nicht linux-image.
    await expect(tabelle).toContainText('proxmox-kernel-6.8.12-4-pve');
    await expect(tabelle).toContainText('pve-kernel-6.5.13-5-pve');
  });

  test('im LXC-Container nur die Version, keine Liste', async ({ page }) => {
    await page.goto('/servers/2'); // db01 läuft als LXC
    // Die Version bleibt sichtbar, aber sie gehört dem Host, und das steht auch da.
    await expect(page.getByText('5.15.0-91-generic', { exact: true }).first()).toBeVisible();
    await expect(page.getByText(/Der Kernel kommt vom Host/)).toBeVisible();
    // Keine Kernel-Liste: Von innen lässt sich am Kernel eines Containers nichts ändern.
    await expect(page.getByRole('heading', { level: 3, name: 'Installierte Kernel' })).toHaveCount(0);
  });
});

test.describe('Serverdetail: Repositories', () => {
  test('bietet die HTTPS-Umstellung, den Katalog bekannter Quellen und den APT-Cache', async ({ page }) => {
    await page.goto('/servers/1/repositories');
    // Demo-Server 1 hat eine http-Quelle: Umstell-Knopf und Kennzeichen.
    await expect(page.getByText('1 unsichere Quelle (HTTP)')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Auf HTTPS umstellen' })).toBeVisible();
    // Katalog bekannter Repositories (Docker & Co).
    const katalog = page.getByRole('combobox', { name: 'Quelle aus dem Katalog' });
    await expect(katalog.locator('option', { hasText: 'Docker CE' })).toHaveCount(1);
    await katalog.selectOption({ label: 'Docker CE' });
    await expect(page.getByText('Offizielle Docker-Engine-Pakete')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Hinzufügen' })).toBeEnabled();
    // APT-Cache-Anbindung: Der Demo-Server ist nicht angebunden.
    await expect(page.getByText('Kein APT-Cache eingerichtet')).toBeVisible();
  });
});

test.describe('Serverdetail: Deep-Scan', () => {
  test('datierte Berichte mit Fortschritt statt flacher Befundliste', async ({ page }) => {
    await page.goto('/servers/1/deep-scan'); // web01, zwei Demo-Läufe
    await expect(page.getByRole('heading', { level: 3, name: 'Verlauf' })).toBeVisible();
    // Zwei Läufe, neueste zuerst: Die Datierung IST die Identität des Berichts.
    const laeufe = page.getByRole('list').filter({ hasText: 'Härtung 61/100' }).getByRole('button');
    await expect(laeufe).toHaveCount(2);
    await expect(laeufe.nth(0)).toContainText('aktuell');
    // Der Fortschritt gegenüber dem Vorlauf steht am Eintrag: Befunde neu und behoben.
    await expect(laeufe.nth(0)).toContainText('+1 neu');
    await expect(laeufe.nth(0)).toContainText('−2 behoben');
    // Der älteste Lauf hat keinen Vorgänger: Dort gibt es nichts zu vergleichen.
    await expect(laeufe.nth(1)).not.toContainText('neu');

    // Der neue Befund ist als solcher markiert.
    const befunde = page.getByRole('grid', { name: 'Befunde des tiefen Scans' });
    await expect(befunde.getByRole('row').filter({ hasText: 'Dienst nutzt veraltete Bibliotheken: nginx.service' })).toContainText('neu');
  });
});

