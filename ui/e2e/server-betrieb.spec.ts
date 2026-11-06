// Serverdetail: Benutzer, Speicher, Apps, Jobs, SSH-Sitzungen und die Servereinstellungen.
import { test, expect } from './fixtures';
import { fenster, setzeSchalter } from './helpers';

const zeileVon = (page: import('@playwright/test').Page, tabelle: string, name: string | RegExp) =>
  page.getByRole('grid', { name: tabelle, exact: true }).getByRole('row', { name });

test.describe('Serverdetail: Benutzer', () => {
  test('zeigt Konten mit Anmeldeart, 2FA und Aktionen', async ({ page }) => {
    // web01 hat im Demo vier Konten (root, lcm-svc, anna mit 2FA, legacy-app).
    await page.goto('/servers/1/users');
    const anna = zeileVon(page, 'Linux-Konten', /^anna /);
    await expect(anna).toContainText('nur Schlüssel');
    await expect(anna).toContainText('eingerichtet'); // 2FA
    await expect(zeileVon(page, 'Linux-Konten', /^root /)).toContainText('Passwort');

    // root und der Dienstbenutzer sind geschützt: keine Aktionsknöpfe.
    for (const geschuetzt of [/^root /, /^lcm-svc /]) {
      await expect(zeileVon(page, 'Linux-Konten', geschuetzt).getByRole('button', { name: /Sperren|entfernen/ })).toHaveCount(0);
    }
    // Das Altkonto ist verwaltbar: Sperren und Entfernen.
    const alt = zeileVon(page, 'Linux-Konten', /^legacy-app /);
    await expect(alt.getByRole('button', { name: 'Sperren' })).toBeVisible();
    await expect(alt.getByRole('button', { name: 'legacy-app entfernen' })).toBeVisible();
    // Ein verwaltetes Konto (anna) ist sperrbar, aber NICHT endgültig entfernbar:
    // Das legte der nächste Abgleich ohnehin wieder an.
    await expect(anna.getByRole('button', { name: 'Sperren' })).toBeVisible();
    await expect(anna.getByRole('button', { name: /entfernen/ })).toHaveCount(0);
    // Neu einlesen steht neben dem Zuordnen.
    await expect(page.getByRole('button', { name: 'Neu einlesen' })).toBeVisible();
  });

  test('die Anmeldehistorie nennt Herkunft und Dauer', async ({ page }) => {
    await page.goto('/servers/1/users');
    await zeileVon(page, 'Linux-Konten', /^anna /).getByRole('button', { name: '3 Anmeldungen' }).click();
    const historie = page.getByRole('grid', { name: 'Anmeldungen von anna' });
    await expect(historie.getByRole('row')).toHaveCount(4); // Kopf + 3 Sitzungen
    await expect(historie.getByRole('row', { name: /192\.168\.10\.27/ })).toHaveCount(2);
    // Eine noch laufende Sitzung ist als aktiv gekennzeichnet.
    await expect(historie.getByRole('gridcell', { name: 'aktiv' })).toHaveCount(1);
  });

  test('ein gesperrtes Konto ist markiert und bietet „Entsperren“ an', async ({ page }) => {
    await page.goto('/servers/2/users');
    const praktikant = zeileVon(page, 'Linux-Konten', /^praktikant /);
    await expect(praktikant).toContainText('deaktiviert');
    await expect(praktikant.getByRole('button', { name: 'Entsperren' })).toBeVisible();
  });

  test('der Reiter verlinkt die Anleitung zu Linux-Benutzern', async ({ page }) => {
    await page.goto('/servers/1/users');
    await page.getByRole('link', { name: 'Anleitung' }).click();
    await expect(page).toHaveURL(/\/docs\/linux-benutzer$/);
    await expect(page.getByRole('heading', { level: 1, name: 'Linux-Benutzer einrichten' })).toBeVisible();
  });
});

test.describe('Serverdetail: Speicher', () => {
  test('zeigt Verlauf, Prognose und Laufwerke', async ({ page }) => {
    // web01 hat im Demo 30 Tage Speicher-Verlauf.
    await page.goto('/servers/1/storage');
    await expect(page.getByRole('heading', { level: 3, name: 'Belegung von /' })).toBeVisible();
    await expect(page.getByText('Jetzt 31 % · 12,5 GB von 40 GB')).toBeVisible();
    await expect(page.getByText('Prognose: reicht')).toBeVisible();
    // Das Verlaufsdiagramm ist eine Grafik mit Beschreibung, nicht nur ein Bild.
    await expect(page.getByRole('img', { name: /Belegung von \/ in Prozent über 30 Tage/ })).toBeVisible();

    // Laufwerke: mehrere Dateisysteme, Root mit System-Kennzeichen, Inodes und Überwachung.
    const laufwerke = page.getByRole('grid', { name: 'Laufwerke' });
    await expect(laufwerke.getByRole('columnheader', { name: 'Inodes' })).toBeVisible();
    await expect(laufwerke.getByRole('columnheader', { name: 'Überwachung' })).toBeVisible();
    await expect(laufwerke.getByRole('row', { name: /^\/data / })).toContainText('xfs');
    await expect(laufwerke.getByRole('row', { name: /^\/ System/ })).toContainText('immer überwacht');
  });

  test('die Prognose warnt, wenn der Speicher in absehbarer Zeit voll ist', async ({ page }) => {
    // db01 hat 310 MB/Tag Zuwachs und knapp 11 GB frei: etwa 37 Tage Restlaufzeit.
    await page.goto('/servers/2/storage');
    await expect(page.getByText('Voll in etwa 37 Tagen', { exact: true })).toBeVisible();
    await expect(page.getByText(/linear voll in etwa 37 Tagen/)).toBeVisible();
    await expect(page.getByRole('row', { name: /^\/var\/lib\/mysql / })).toBeVisible();
  });

  test('die Überwachung eines Laufwerks lässt sich ein- und ausschalten, Netzspeicher nicht', async ({ page }) => {
    await page.goto('/servers/1/storage');
    const data = () => zeileVon(page, 'Laufwerke', /^\/data /);
    await expect(data()).toContainText('nur Anzeige');

    // Einschalten: Die Grenze erscheint mit der Vorgabe 85.
    await data().getByRole('button', { name: 'Überwachen' }).click();
    const monitor = fenster(page, 'Überwachung von /data');
    await monitor.getByRole('switch', { name: 'Überwachen' }).click();
    const grenze = monitor.getByRole('textbox');
    await expect(grenze).toHaveValue('85');
    await grenze.fill('90');
    await monitor.getByRole('button', { name: 'Speichern' }).click();
    await expect(page.getByText('Überwachung von /data')).toHaveCount(0);

    // Die Einstellung liegt in einer eigenen Tabelle und überlebt das Neuladen.
    await page.reload();
    await expect(data()).not.toContainText('nur Anzeige');
    await expect(data()).toContainText('90');

    // Aufräumen: wieder ausschalten.
    await data().getByRole('button').click();
    const ausschalten = fenster(page, 'Überwachung von /data');
    await setzeSchalter(ausschalten.getByRole('switch'), false);
    await ausschalten.getByRole('button', { name: 'Speichern' }).click();
    await expect(data()).toContainText('nur Anzeige');

    // Der Netz-Mount trägt die Marke und bietet KEINEN Schalter an.
    const netz = zeileVon(page, 'Laufwerke', /^\/mnt\/backup /);
    await expect(netz).toContainText('Netzspeicher');
    await expect(netz).toContainText('nicht von hier');
    await expect(netz.getByRole('button')).toHaveCount(0);
  });

  test('ein degradierter Speicherverbund zeigt den Zustand und färbt den Server', async ({ page }) => {
    // db01 hat im Demo einen ZFS-Mirror ohne Redundanz.
    await page.goto('/servers/2/storage');
    const pool = zeileVon(page, 'Speicherverbünde', /^DEGRADED /);
    await expect(pool).toContainText('tank');
    await expect(pool).toContainText('ZFS-Pool');
    await expect(pool).toContainText('Prüfsummenfehler');
  });
});

test.describe('Serverdetail: Apps, Jobs und SSH-Sitzungen', () => {
  test('der Reiter Apps sagt, dass nichts gefunden wurde, und verweist auf den Katalog', async ({ page }) => {
    await page.goto('/servers/1/apps');
    await expect(page.getByRole('heading', { level: 3, name: 'Keine Anwendungen erkannt' })).toBeVisible();
    await expect(page.getByRole('heading', { level: 3, name: 'Ohne Zuordnung' })).toBeVisible();
    // Der Weg zum Katalog steht im Reiter und führt auf die Einstellungsseite.
    await page.getByRole('link', { name: 'Anwendungskatalog' }).click();
    await expect(page).toHaveURL(/\/settings\/apps$/);
    await expect(page.getByRole('grid').getByText('AdGuard Home')).toBeVisible();
  });

  test('der Reiter Jobs listet die Jobs des Servers und filtert', async ({ page }) => {
    await page.goto('/servers/1/jobs');
    const jobs = page.getByRole('grid', { name: 'Jobs dieses Servers' });
    await expect(jobs.getByRole('row', { name: /SSH härten/ })).toContainText('admin');
    const status = page.locator('lcm-srv-jobs').getByRole('combobox', { name: 'Status' });
    await status.selectOption({ label: 'Probleme' });
    // Demo-Jobs sind erfolgreich: Der Filter „Probleme“ leert die Liste.
    await expect(jobs.getByRole('row', { name: /SSH härten/ })).toHaveCount(0);
    await status.selectOption({ label: 'Alle' });
    await expect(jobs.getByRole('row', { name: /SSH härten/ })).toBeVisible();
    await setzeSchalter(page.getByRole('switch', { name: 'Verbindungsprüfungen zeigen' }), true);
  });

  test('ein Job öffnet sich als Fenster über dem Server, Schließen führt nicht weg', async ({ page }) => {
    await page.goto('/servers/1/jobs');
    await page.getByRole('grid', { name: 'Jobs dieses Servers' }).getByRole('row', { name: /SSH härten/ }).first().click();
    await expect(page.getByRole('heading', { name: 'Ausgabe' })).toBeVisible();
    await expect(page).toHaveURL(/\/servers\/1\/jobs$/);
    await page.keyboard.press('Escape');
    await expect(page.getByRole('heading', { name: 'Ausgabe' })).toHaveCount(0);
    await expect(page).toHaveURL(/\/servers\/1\/jobs$/);
    await expect(page.getByRole('grid', { name: 'Jobs dieses Servers' })).toBeVisible();
  });

  test('der Reiter SSH-Sitzungen erklärt die Aufzeichnung; Demo-Server haben keine', async ({ page }) => {
    await page.goto('/servers/1/ssh');
    // Demo-Server werden nie per SSH kontaktiert, also gibt es keine Aufzeichnung.
    await expect(page.getByRole('heading', { level: 3, name: 'Keine Sitzungen aufgezeichnet' })).toBeVisible();
    await expect(page.getByText(/Jede Verbindung, die LCM zu diesem Server öffnet, wird mit ihren Befehlen aufgezeichnet/)).toBeVisible();
  });

  test('Hardware und Alles lassen sich neu erfassen', async ({ page }) => {
    await page.goto('/servers/1');
    await expect(page.getByRole('button', { name: 'Hardware aktualisieren' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Alles aktualisieren' })).toBeVisible();
  });
});

test.describe('Serverdetail: Einstellungen', () => {
  test('Hostname anzeigen und Server umbenennen', async ({ page }) => {
    await page.goto('/servers/1');
    // web01 wurde über eine IP aufgenommen und nennt sich selbst anders: Genau dann ist der Hostname eine Information.
    await expect(page.getByText('web01.intern.example').first()).toBeVisible();

    await page.getByRole('button', { name: 'Einstellungen' }).click();
    const einstellungen = fenster(page, 'Servereinstellungen');
    const name = einstellungen.getByRole('textbox', { name: 'Name', exact: true });
    const speichern = einstellungen.getByRole('button', { name: 'Speichern' });

    // Der gemeldete Hostname wird als Vorschlag angeboten: Man muss ihn nicht abtippen.
    await einstellungen.getByRole('button', { name: /Systemnamen übernehmen: web01.intern.example/ }).click();
    await expect(name).toHaveValue('web01.intern.example');

    // Ein bereits vergebener Name wird mit einer verständlichen Meldung abgewiesen.
    await name.fill('db01');
    await speichern.click();
    await expect(einstellungen.getByText(/bereits|schon/i).first()).toBeVisible();
    // Das Getippte bleibt stehen: Ein abgelehnter Name soll korrigierbar sein.
    await expect(name).toHaveValue('db01');

    // Jetzt richtig umbenennen und danach zurückbenennen.
    await name.fill('web01-umbenannt');
    await speichern.click();
    await expect(page.getByRole('heading', { level: 2, name: 'web01-umbenannt' })).toBeVisible();

    await page.getByRole('button', { name: 'Einstellungen' }).click();
    await fenster(page, 'Servereinstellungen').getByRole('textbox', { name: 'Name', exact: true }).fill('web01');
    await fenster(page, 'Servereinstellungen').getByRole('button', { name: 'Speichern' }).click();
    await expect(page.getByRole('heading', { level: 2, name: 'web01', exact: true })).toBeVisible();
  });

  test('die Konsole ist je Server abschaltbar und bleibt nach dem Neuladen so', async ({ page }) => {
    await page.goto('/servers/1');
    await page.getByRole('button', { name: 'Einstellungen' }).click();
    await fenster(page, 'Servereinstellungen').getByRole('tab', { name: 'Zugang' }).click();
    let schalter = fenster(page, 'Servereinstellungen').getByRole('switch', { name: 'Konsole abschalten' });
    // Von einem bekannten Zustand ausgehen: Die Einstellung überlebt absichtlich einen Testlauf.
    if (await setzeSchalter(schalter, false)) await fenster(page, 'Servereinstellungen').getByRole('button', { name: 'Speichern' }).click();

    await setzeSchalter(schalter, true);
    await fenster(page, 'Servereinstellungen').getByRole('button', { name: 'Speichern' }).click();
    await expect(page.getByText('web01 gespeichert')).toBeVisible();

    await page.reload();
    await page.getByRole('button', { name: 'Einstellungen' }).click();
    await fenster(page, 'Servereinstellungen').getByRole('tab', { name: 'Zugang' }).click();
    schalter = fenster(page, 'Servereinstellungen').getByRole('switch', { name: 'Konsole abschalten' });
    await expect(schalter).toBeChecked();

    // Wieder freischalten, damit die folgenden Tests den Ausgangszustand vorfinden.
    await setzeSchalter(schalter, false);
    await fenster(page, 'Servereinstellungen').getByRole('button', { name: 'Speichern' }).click();
  });

  test('Docker-Updates sperren und Docker-CVEs ignorieren wirkt in der Sicherheitsübersicht', async ({ page }) => {
    // Ausgangslage belegen: Der Container-Fund von web01 steht in der Sicherheitsübersicht.
    // Ohne diese Zusage wäre die Abwesenheitsprüfung unten wertlos.
    await page.goto('/security');
    await expect(page.getByRole('gridcell', { name: 'Container nginx:1.25' })).toBeVisible();

    await page.goto('/servers/1');
    await page.getByRole('button', { name: 'Einstellungen' }).click();
    const einstellungen = fenster(page, 'Servereinstellungen');
    await einstellungen.getByRole('tab', { name: 'Betrieb' }).click();
    const updates = einstellungen.getByRole('switch', { name: 'Keine Docker-Updates einspielen' });
    const cves = einstellungen.getByRole('switch', { name: 'Lücken in Container-Images ignorieren' });
    // Ausgangslage HERSTELLEN, nicht voraussetzen.
    await setzeSchalter(updates, false);
    await setzeSchalter(cves, false);
    await einstellungen.getByRole('button', { name: 'Speichern' }).isEnabled().then(async (an) => an && (await einstellungen.getByRole('button', { name: 'Speichern' }).click()));

    await updates.click();
    await cves.click();
    await einstellungen.getByRole('button', { name: 'Speichern' }).click();
    await expect(page.getByText('web01 gespeichert')).toBeVisible();

    // Wirkung: In der Sicherheitsübersicht sind die Container-Funde von web01 verschwunden.
    await page.goto('/security');
    await expect(page.getByRole('grid', { name: /Sicherheitslücken aller Server/ })).toBeVisible();
    await expect(page.getByRole('gridcell', { name: 'Container nginx:1.25' })).toHaveCount(0);

    // Zurücksetzen, damit die folgenden Tests den Normalzustand vorfinden.
    await page.goto('/servers/1');
    await page.getByRole('button', { name: 'Einstellungen' }).click();
    await fenster(page, 'Servereinstellungen').getByRole('tab', { name: 'Betrieb' }).click();
    await setzeSchalter(fenster(page, 'Servereinstellungen').getByRole('switch', { name: 'Keine Docker-Updates einspielen' }), false);
    await setzeSchalter(fenster(page, 'Servereinstellungen').getByRole('switch', { name: 'Lücken in Container-Images ignorieren' }), false);
    await fenster(page, 'Servereinstellungen').getByRole('button', { name: 'Speichern' }).click();
  });
});
