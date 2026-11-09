// Serverdetail: Reiter Sicherheit, Firewall, Sicherheits-Tools und die Bestätigungen gefährlicher Aktionen.
import { test, expect } from './fixtures';
import { bestaetige, fenster, waehle } from './helpers';

const cveTabelle = (page: import('@playwright/test').Page) => page.getByRole('grid', { name: 'Sicherheitslücken', exact: true });

/** Nur die Sicherheitslücken stehen offen; alles andere klappt man über den Kopf der Karte auf. */
const klappeAuf = (page: import('@playwright/test').Page, karte: string) => page.getByRole('button', { name: karte, exact: true }).click();

test.describe('Serverdetail: Sicherheit', () => {
  test('nur die Sicherheitslücken sind offen, alles andere klappt man auf', async ({ page }) => {
    await page.goto('/servers/1/security');
    await expect(cveTabelle(page)).toBeVisible();
    const schutz = page.getByRole('button', { name: 'Schutz', exact: true });
    await expect(schutz).toHaveAttribute('aria-expanded', 'false');
    await schutz.click();
    await expect(schutz).toHaveAttribute('aria-expanded', 'true');
  });

  test('listet CVEs mit Schweregrad, Quelle und Paket', async ({ page }) => {
    // web01 hat im Demo eine kritische und mehrere hohe CVEs.
    await page.goto('/servers/1/security');
    await expect(page.getByRole('heading', { level: 3, name: 'Sicherheitslücken (CVE)' })).toBeVisible();
    const kritisch = cveTabelle(page).getByRole('row').filter({ hasText: 'CVE-2023-0286' });
    await expect(kritisch).toContainText('Kritisch');
    await expect(kritisch).toContainText('openssl');
    await expect(kritisch).toContainText('System');
    // Eine Spalte „Herkunft“: Einem Fund ist anzusehen, ob er das System oder ein Image betrifft.
    await expect(cveTabelle(page).getByRole('columnheader', { name: 'Herkunft' })).toBeVisible();
  });

  test('der Herkunftsfilter trennt Betriebssystem und Container-Images', async ({ page }) => {
    await page.goto('/servers/1/security');
    const filter = page.getByRole('combobox', { name: 'Herkunft' });
    await filter.selectOption({ label: 'Nur Container-Images' });
    const zeilen = cveTabelle(page).getByRole('row');
    await expect(zeilen.nth(1)).toContainText('Container');
    await expect(cveTabelle(page).getByRole('gridcell', { name: 'System', exact: true })).toHaveCount(0);

    await filter.selectOption({ label: 'Nur Betriebssystem' });
    await expect(cveTabelle(page).getByRole('gridcell', { name: /^Container/ })).toHaveCount(0);
  });

  test('ein CVE-Fund am Paket führt in den Reiter Sicherheit', async ({ page }) => {
    // web01: openssl hat im Demo eine kritische CVE; sie steht in der Fundliste des Reiters.
    await page.goto('/servers/1/packages');
    await expect(page.getByRole('row', { name: /^Paket openssl/ })).toContainText('Sicherheit');
    await page.getByRole('tab', { name: 'Sicherheit' }).click();
    await expect(cveTabelle(page).getByRole('row').filter({ hasText: 'CVE-2023-0286' })).toBeVisible();
  });

  test('SSH-2FA erscheint erst nach der Einrichtung und wird über die Sicherheits-Tools eingerichtet', async ({ page }) => {
    await page.goto('/servers/1/security');
    // Solange 2FA nicht eingerichtet ist, steht sie als „nicht aktiv“ in der Übersicht des Schutzes.
    await klappeAuf(page, 'Schutz');
    await expect(page.getByRole('listitem').filter({ hasText: 'Zwei-Faktor für SSH' }).getByRole('img', { name: 'nicht aktiv' })).toBeVisible();

    await page.getByRole('button', { name: 'Sicherheits-Tools einrichten' }).click();
    const tools = fenster(page, 'Sicherheits-Tools einrichten');
    // Drei Werkzeuge zur Auswahl: fail2ban ist auf web01 schon da.
    await expect(tools.getByRole('radio', { name: /^fail2ban/ })).toBeDisabled();
    await expect(tools.getByRole('radio', { name: /^CrowdSec/ })).toBeEnabled();
    await expect(tools.getByRole('radio', { name: /^SSH-2FA/ })).toBeEnabled();
    await expect(tools.getByRole('button', { name: 'Einrichten' })).toBeDisabled();
    await waehle(page, /^SSH-2FA/);
    await expect(tools.getByRole('button', { name: 'Einrichten' })).toBeEnabled();
    // Bei 2FA gibt es keine Allowlist-Auswahl, dafür die Warnung vor dem Aussperren.
    await expect(tools.getByRole('group', { name: /Allowlist/ })).toHaveCount(0);
    await expect(tools.getByText(/Richte deinen eigenen Code ein/)).toBeVisible();
  });

  test('fail2ban verwalten: Dienst, Allowlist, Sperrliste; Aktionen melden das echte Ergebnis', async ({ page }) => {
    // web01 hat im Demo fail2ban installiert und aktiv.
    await page.goto('/servers/1/security');
    await klappeAuf(page, 'fail2ban verwalten');
    for (const knopf of ['Starten', 'Neu starten', 'Stoppen', 'Autostart an', 'Autostart aus', 'Deinstallieren']) {
      await expect(page.getByRole('button', { name: knopf, exact: true })).toBeVisible();
    }
    await expect(page.getByRole('group', { name: 'IP-Allowlists' }).getByRole('button', { name: 'Büro-Netze' })).toBeVisible();
    // Sperrliste: Demo-Server werden nie per SSH kontaktiert, also leer.
    await expect(page.getByText('Keine aktiven Sperren.')).toBeVisible();

    // Eine Aktion meldet erst „gestartet“ und danach das ECHTE Ergebnis.
    await page.getByRole('button', { name: 'Neu starten', exact: true }).click();
    await expect(page.getByText('Neu starten gestartet auf web01')).toBeVisible();
    await expect(page.getByText('Neu starten auf web01 abgeschlossen')).toBeVisible({ timeout: 25_000 });
  });

  test('jobs:read: der Reiter „Jobs“ und der Link „Job ansehen“ in der Meldung hängen am Recht', async ({ page }) => {
    // Mit dem Recht (Admin): Reiter da, die Meldung führt zum Job.
    await page.goto('/servers/1/security');
    await expect(page.getByRole('tab', { name: /^Jobs/ })).toBeVisible();
    await klappeAuf(page, 'fail2ban verwalten');
    await page.getByRole('button', { name: 'Neu starten', exact: true }).click();
    await expect(page.locator('lcm-toaster .toast').filter({ hasText: 'Neu starten gestartet auf web01' }).getByRole('button', { name: 'Job ansehen' })).toBeVisible();

    // Dieselbe Sitzung ohne das Recht: kein Reiter, die Adresse fällt auf die Übersicht, die Meldung hat keinen Link.
    await page.route('**/api/v1/auth/me', async (route) => {
      const antwort = await route.fetch();
      const profil = await antwort.json();
      profil.permissions = profil.permissions.filter((recht: string) => recht !== 'jobs:read');
      await route.fulfill({ response: antwort, json: profil });
    });
    await page.goto('/servers/1/jobs');
    await expect(page.getByRole('tab', { name: 'Übersicht' })).toHaveAttribute('aria-selected', 'true');
    await expect(page.getByRole('tab', { name: /^Jobs/ })).toHaveCount(0);
    await page.getByRole('tab', { name: 'Sicherheit' }).click();
    await klappeAuf(page, 'fail2ban verwalten');
    await page.getByRole('button', { name: 'Neu starten', exact: true }).click();
    const meldung = page.locator('lcm-toaster .toast').filter({ hasText: 'Neu starten gestartet auf web01' });
    await expect(meldung).toBeVisible();
    await expect(meldung.getByRole('button', { name: 'Job ansehen' })).toHaveCount(0);
  });

  test('db01 nutzt CrowdSec (inaktiv), cache01 hat kein Werkzeug', async ({ page }) => {
    await page.goto('/servers/2/security');
    await expect(page.getByRole('heading', { level: 3, name: 'CrowdSec verwalten' })).toBeVisible();
    await expect(page.getByRole('heading', { level: 3, name: 'fail2ban verwalten' })).toHaveCount(0);

    await klappeAuf(page, 'CrowdSec verwalten');
    await expect(page.getByText('gestoppt', { exact: true })).toBeVisible();

    await page.goto('/servers/3/security');
    await expect(page.getByRole('heading', { level: 3, name: 'Schutz' })).toBeVisible();
    await expect(page.getByRole('heading', { name: /verwalten$/ })).toHaveCount(0);
    await klappeAuf(page, 'Schutz');
    await expect(page.getByRole('listitem').filter({ hasText: 'Fail2ban' }).getByRole('img', { name: 'nicht aktiv' })).toBeVisible();
  });
});

test.describe('Serverdetail: Firewall und gefährliche Aktionen', () => {
  test('die Firewall öffnet ein Fenster mit Regel-Editor, SSH-Zeile und Vorschlägen', async ({ page }) => {
    await page.goto('/servers/1');
    await page.getByRole('button', { name: /^Firewall/ }).click();
    const firewall = fenster(page, 'Firewall von web01');
    // Backend: Debian → nftables, das erkannte Werkzeug der Demo.
    await expect(firewall.getByText('Werkzeug: nftables')).toBeVisible();
    await expect(firewall.getByText('SSH (Port 22) bleibt immer offen')).toBeVisible();

    // Der Regel-Editor zeigt die konfigurierten Regeln (80 + 443); die SSH-Freigabe
    // steht als eigene, nicht löschbare Zeile da.
    await expect(firewall.getByRole('textbox', { name: 'Port' })).toHaveCount(2);
    const ssh = firewall.getByRole('row').filter({ hasText: 'SSH-Zugang' });
    await expect(ssh).toContainText('22');
    await expect(ssh.getByRole('button', { name: /entfernen/ })).toHaveCount(0);
    // web01 ist im Demo firewall-aktiv: neu anwenden und deaktivieren.
    await expect(firewall.getByRole('button', { name: 'Regeln neu anwenden' })).toBeVisible();
    await expect(firewall.getByRole('button', { name: 'Deaktivieren' })).toBeVisible();

    // Lauschende Dienste aus dem Port-Scan sind Vorschläge; ein Klick übernimmt Port, Protokoll und IP-Version als Regel.
    await firewall.getByRole('button', { name: 'Freigabe 9100/tcp hinzufügen' }).click();
    await expect(firewall.getByRole('textbox', { name: 'Port' })).toHaveCount(3);
    // Übernommen heißt: Der Vorschlag verschwindet.
    await expect(firewall.getByRole('button', { name: 'Freigabe 9100/tcp hinzufügen' })).toHaveCount(0);

    // Docker-Ports gehen an der Firewall vorbei: Das steht im Fenster.
    await expect(firewall.getByText(/Von Docker veröffentlicht, an der Firewall vorbei/)).toBeVisible();

    // Entscheidend: Was beim Anwenden tatsächlich gesendet wird. Die Bemerkung ging
    // früher genau hier verloren. Der Job selbst darf gegen den Demo-Server scheitern.
    await firewall.getByRole('textbox', { name: 'Bemerkung' }).last().fill('Prometheus-Export');
    await firewall.getByRole('button', { name: 'Regeln neu anwenden' }).click();
    const rueckfrage = fenster(page, 'Regeln auf web01 neu anwenden?');
    await expect(rueckfrage.getByText('22/tcp, 80/tcp, 443/tcp, 9100/tcp')).toBeVisible();
    const [anfrage] = await Promise.all([
      page.waitForRequest((r) => /\/firewall/.test(r.url()) && r.method() === 'POST'),
      rueckfrage.getByRole('button').last().click(),
    ]);
    const gesendet = anfrage.postDataJSON();
    expect(gesendet.rules.some((regel: { comment?: string }) => regel.comment === 'Prometheus-Export')).toBeTruthy();
    expect(gesendet.rules.some((regel: { port: number }) => regel.port === 9100)).toBeTruthy();
  });

  test('Rechte einschränken erklärt die Folge und lässt sich abbrechen', async ({ page }) => {
    await page.goto('/servers/1');
    await page.getByRole('button', { name: 'Aktionen' }).click();
    await page.getByRole('menuitem', { name: 'Rechte einschränken' }).click();
    const bestaetigung = fenster(page, 'Rechte einschränken: web01');
    await expect(bestaetigung.getByText('LCM kann danach nur noch die Befehle ausführen')).toBeVisible();
    // Sagt, was danach gilt, dass es nicht per Klick zurückgeht und wogegen der Modus nicht schützt (BUG-030).
    await expect(bestaetigung.getByText('Nicht per Klick rückgängig machbar')).toBeVisible();
    await expect(bestaetigung.getByText('Danach hat der LCM-Benutzer selbst nicht mehr die Rechte')).toBeVisible();
    await expect(bestaetigung.getByText('Wogegen das schützt, und wogegen nicht')).toBeVisible();
    await expect(bestaetigung.getByText('Er schützt nicht vor jemandem, der den Service-Schlüssel erlangt', { exact: false })).toBeVisible();
    await bestaetigung.getByRole('button', { name: 'Abbrechen' }).click();
    // Abgebrochen: Der Demo-Zustand bleibt unverändert.
    await expect(page.getByRole('heading', { level: 3, name: 'Hardware' })).toBeVisible();
    await expect(page.getByRole('heading', { name: 'Rechte einschränken: web01' })).toHaveCount(0);
  });

  test('der Neustart löst nach der Bestätigung einen Job aus', async ({ page }) => {
    await page.goto('/servers/4'); // rocky01 - von keinem späteren Test mehr gebraucht
    await page.getByRole('button', { name: 'Aktionen' }).click();
    await page.getByRole('menuitem', { name: 'Server neu starten' }).click();
    await expect(fenster(page, 'Server neu starten: rocky01').getByText('bis der Server wieder hochgefahren ist')).toBeVisible();
    await bestaetige(page, 'Server neu starten: rocky01', 'Server neu starten');
    await expect(page.getByText('Server neu starten auf rocky01 abgeschlossen')).toBeVisible({ timeout: 25_000 });
  });

  test('Server entfernen verlangt den Namen und bietet die Bereinigung an', async ({ page }) => {
    await page.goto('/servers/1');
    await page.getByRole('button', { name: 'Aktionen' }).click();
    await page.getByRole('menuitem', { name: 'Entfernen' }).click();
    const entfernen = fenster(page, 'web01 entfernen?');
    await expect(entfernen.getByText('Jobs, Pakete und Befunde dieses Servers gehen verloren')).toBeVisible();
    // Die Bereinigung auf dem Server ist ein eigener Schalter.
    await expect(entfernen.getByRole('switch', { name: 'LCM-Zugang auf dem Server auch löschen' })).toBeVisible();
    // Ohne den getippten Namen bleibt der Knopf gesperrt.
    await expect(entfernen.getByRole('button', { name: 'Server entfernen' })).toBeDisabled();
    await entfernen.getByRole('textbox').fill('web01');
    await expect(entfernen.getByRole('button', { name: 'Server entfernen' })).toBeEnabled();
    // Abbrechen ohne zu löschen: Die Demo-Daten bleiben erhalten.
    await entfernen.getByRole('button', { name: 'Abbrechen' }).click();
    await expect(page.getByRole('heading', { level: 2, name: 'web01', exact: true })).toBeVisible();
  });
});
