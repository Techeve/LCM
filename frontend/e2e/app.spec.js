// End-to-End-Tests der LCM-Web-UI gegen das echte Go-Binary (Demo-Modus).
// Admin-Zugang kommt aus e2e/run-server.sh (admin_initial_password).
import { test, expect, ADMIN_PASSWORD } from './fixtures.js';

// Die Sitzung bringt die Fixture mit (einmal je Worker über die API, danach
// in localStorage) - hier wird nur noch die App geöffnet. Der Anmeldeweg über
// das Formular bleibt in den Tests geprüft, die genau ihn zum Gegenstand
// haben (siehe „Login mit falschem Passwort" und „Admin kann sich anmelden").
async function loginAsAdmin(page) {
  await page.goto('/#/');
  // Normalfall: Die Fixture hat die Sitzung schon hinterlegt, die Pille oben
  // rechts steht sofort da. Selten kommt sie nicht an - beobachtet unter Last
  // mit zwei Workern, einmal in fünf Läufen. Statt die ganze Suite an diesem
  // Wackler scheitern zu lassen, wird dann regulär angemeldet: langsamer,
  // aber verlässlich. Träte es häufig auf, wäre das ein Hinweis auf ein
  // echtes Sitzungsproblem - die Ausweichanmeldung verdeckt es nicht, sie
  // macht den Lauf nur belastbar.
  const angemeldet = await page
    .getByTestId('current-user')
    .isVisible({ timeout: 2_000 })
    .catch(() => false);
  if (!angemeldet) {
    await loginUeberFormular(page);
  }
  await expect(page.getByTestId('current-user')).toContainText('Administrator', { timeout: 15_000 });
}

// Vollständige Anmeldung über das Formular - nur für die Tests, die den
// Anmeldeweg selbst prüfen.
async function loginUeberFormular(page, passwort = ADMIN_PASSWORD) {
  await page.goto('/#/login');
  await page.fill('#username', 'admin');
  await page.fill('#password', passwort);
  await page.click('button[type="submit"]');
}

// Legt in der Gruppe „Produktion" einen Wegwerf-Schedule an (die Demo-Gruppe
// bringt seit v0.10.0 keinen mehr mit) und liefert seine Tabellenzeile.
// Aufräumen am Testende: Zeile per „×" löschen (entfernt auch ihre Rules).
async function createProduktionSchedule(page, name) {
  // Löschen von Schedules/Rules ist mit einem confirm()-Dialog abgesichert -
  // im Test automatisch bestätigen.
  page.on('dialog', (d) => d.accept());
  await page.goto('/#/groups');
  await page.getByRole('button', { name: 'Produktion' }).click();
  await page.locator('button[title="Neuer Schedule"]').click();
  await page.locator('#sf-name').fill(name);
  await page.locator('#sf-cron').fill('0 2 * * *');
  await page.getByRole('button', { name: 'Speichern' }).click();
  const row = page.locator('table').filter({ hasText: 'Zeitplan' }).locator('tr', { hasText: name });
  await expect(row).toBeVisible();
  return row;
}

// Stellt einen Schalter auf den gewuenschten Zustand - statt ihn
// vorauszusetzen. Ein Test, der auf „ab Werk aus" baut, scheitert sonst
// endgueltig, sobald ein vorheriger Durchlauf mittendrin abgebrochen ist:
// Der Schalter bleibt an, und schon die erste Zusicherung faellt. Die
// Wiederholung findet denselben Zustand vor und faellt wieder.
async function setzeSchalter(schalter, an) {
  if ((await schalter.isChecked()) !== an) {
    await schalter.click();
    await expect(schalter).toBeChecked({ checked: an });
  }
}

test.describe('LCM', () => {
  // Der Anmeldeweg selbst - diese Tests starten bewusst OHNE vorbereitete
  // Sitzung, sonst pruefen sie nichts mehr.
  test.describe('Anmeldung', () => {
    test.use({ angemeldet: false });

    test('ohne Login erscheint die Anmeldemaske', async ({ page }) => {
      await page.goto('/');
      await expect(page.locator('h1')).toContainText('Anmelden');
    });

    test('Login mit falschem Passwort zeigt Fehler', async ({ page }) => {
      await loginUeberFormular(page, 'definitiv-falsch');
      await expect(page.getByTestId('login-error')).toBeVisible();
    });

    test('Admin kann sich anmelden und abmelden', async ({ page }) => {
      await loginUeberFormular(page);
      await expect(page.getByTestId('current-user')).toContainText('Administrator', { timeout: 15_000 });
      // Abmelden liegt jetzt im Konto-Dropdown (Pille oben rechts).
      await page.getByTestId('current-user').click();
      await page.getByRole('button', { name: 'Abmelden' }).click();
      await expect(page.getByTestId('nav-login')).toBeVisible();
    });
  });

  test('Dashboard zeigt die Demo-Server mit Ampel-Status', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/');
    await expect(page.locator('h1')).toContainText('Dashboard');
    // Die drei Demo-Server erscheinen in der Tabelle.
    await expect(page.locator('table')).toContainText('web01');
    await expect(page.locator('table')).toContainText('db01');
    await expect(page.locator('table')).toContainText('cache01');
    // Mindestens ein kritischer (roter) und ein Warn-Status.
    await expect(page.locator('.badge.bg-danger').first()).toBeVisible();
    await expect(page.locator('.badge.bg-warning').first()).toBeVisible();
  });

  test('Dashboard: Filter (Name, OS, Status) und Pagination-Anzeige', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/');
    await expect(page.locator('h1')).toContainText('Dashboard');
    // Zähler über der Tabelle (die Seitennavigation darunter erscheint erst ab Seite 2).
    await expect(page.locator('body')).toContainText('11 Server');

    // OS-Dropdown ist mit den echten Werten befüllt.
    await expect(page.locator('#f-os option')).toContainText(['alle', 'Ubuntu']);

    // Namenssuche grenzt auf web01 ein.
    await page.fill('#f-name', 'web');
    await expect(page.locator('tbody')).toContainText('web01');
    await expect(page.locator('tbody')).not.toContainText('db01');
    await expect(page.locator('body')).toContainText('1 Server');

    // Reset stellt alle Server wieder her.
    await page.getByRole('button', { name: 'Reset' }).click();
    await expect(page.locator('body')).toContainText('11 Server');

    // Status-Filter „Kritisch“ zeigt nur den roten Server (cache01).
    await page.selectOption('#f-status', 'red');
    await expect(page.locator('tbody')).toContainText('cache01');
    await expect(page.locator('tbody')).not.toContainText('db01');

    // OS-Filter „Ubuntu“ (nach Reset) zeigt nur db01.
    await page.getByRole('button', { name: 'Reset' }).click();
    await page.selectOption('#f-os', 'Ubuntu');
    await expect(page.locator('tbody')).toContainText('db01');
    await expect(page.locator('tbody')).not.toContainText('web01');
  });

  test('Docker-Ports, die ufw umgehen, werden an der Firewall benannt', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/servers/1');
    // web01 hat ufw aktiv (nur 22) UND einen Container auf 0.0.0.0:80 -
    // genau die Konstellation, für die LCM zuvor "Firewall aktiv, nur 22"
    // meldete, obwohl Port 80 von außen erreichbar war (BUG-023).
    const box = page.getByTestId('docker-firewall-bypass');
    await expect(box).toBeVisible();
    await expect(box).toContainText('80/tcp');
    await expect(box).toContainText('webshop-web-1');
    // LCM sagt ausdrücklich, dass es NICHT eingreift, und verweist auf die Doku.
    await expect(box).toContainText('verändert daran nichts');
    await expect(box.getByRole('link')).toHaveAttribute('href', /guides\/docker/);
  });

  test('Nichterreichbarkeit unkritisch: edge01 offline, aber ausgegraut statt rot', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/');
    // edge01 ist offline, aber als unkritisch toleriert → „Offline"-Badge und
    // KEIN roter Kritisch-Badge (behält seinen Status).
    const row = page.locator('tbody tr', { hasText: 'edge01' });
    await expect(row).toContainText('Offline');
    await expect(row.locator('.badge.bg-danger')).toHaveCount(0);
    // Der Status-Filter „Kritisch" listet edge01 NICHT (nur cache01 ist rot).
    await page.selectOption('#f-status', 'red');
    await expect(page.locator('tbody')).toContainText('cache01');
    await expect(page.locator('tbody')).not.toContainText('edge01');
    // Server-Detail: große Offline-Pille neben dem Status.
    await page.goto('/#/servers/7');
    await expect(page.getByTestId('offline-pill')).toBeVisible();
    await expect(page.getByTestId('offline-pill')).toContainText('Offline');
    // Die Einstellung ist an, das Frist-Feld sichtbar.
    await page.getByTestId('open-settings').click();
    await expect(page.getByTestId('unreachable-uncritical-toggle')).toBeChecked();
    await expect(page.locator('#grace-days')).toBeVisible();
  });

  test('Offline-Kennzeichen: auch bei ganz normal ausgefallenen Servern', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/');
    // cache01 ist schlicht ausgefallen (rot, NICHT als unkritisch markiert).
    // Genau dieser Fall hatte vorher gar kein Kennzeichen.
    const cacheRow = page.locator('tbody tr', { hasText: 'cache01' });
    await expect(cacheRow.getByTestId('offline-badge')).toBeVisible();
    await expect(cacheRow.getByTestId('offline-badge')).toHaveAttribute('title', /aufeinanderfolgenden/);
    // edge01 ist als unkritisch markiert: Kennzeichen ja, Zeile ausgegraut.
    const edgeRow = page.locator('tbody tr', { hasText: 'edge01' });
    await expect(edgeRow.getByTestId('offline-badge')).toBeVisible();
    await expect(edgeRow.getByTestId('offline-badge')).toHaveAttribute('title', /unkritisch/);
    // Erreichbare Server tragen es nicht.
    await expect(page.locator('tbody tr', { hasText: 'web01' }).getByTestId('offline-badge')).toHaveCount(0);

    // Dasselbe Kennzeichen in der Detailansicht.
    await page.goto('/#/servers/3');
    await expect(page.getByTestId('offline-badge')).toBeVisible();
  });

  test('Join-Wizard warnt sofort, wenn der Host bereits angelegt ist', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/servers/join');
    await page.fill('#name', 'web01-nochmal');
    // web01 hat im Demo den Host 10.10.0.11 → sofortige Duplikat-Warnung.
    await page.fill('#host', '10.10.0.11');
    await expect(page.getByTestId('join-dup-host')).toBeVisible();
    await expect(page.getByRole('button', { name: /Fingerprint auslesen/ })).toBeDisabled();
    // Anderer Host → keine Warnung mehr.
    await page.fill('#host', '10.10.9.250');
    await expect(page.getByTestId('join-dup-host')).toHaveCount(0);
  });

  test('LCM-Host (localhost): LCM-Logo und Einrichtungs-Karte (Trivy/apt-cacher-ng)', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/');
    // lcm-self trägt das LCM-Logo (aria-label "LCM-Host") statt eines OS-Logos.
    const row = page.locator('tbody tr', { hasText: 'lcm-self' });
    await expect(row.getByRole('img', { name: 'LCM-Host' })).toBeVisible();
    // Detailseite: LCM-Host-Karte mit Status für Trivy und apt-cacher-ng.
    await row.getByRole('link', { name: 'lcm-self' }).click();
    await expect(page.getByTestId('lcm-host-card')).toBeVisible();

    // Die Karte zeigt je nach Betriebsart Verschiedenes, und beides ist
    // richtig: Läuft LCM selbst in einem Container, gibt es dort nichts
    // einzurichten (kein apt, kein Dienst, der den Neustart überlebt) - dann
    // erklärt die Karte das, statt Schaltflächen anzubieten, die scheitern
    // müssen. Auf einem gewöhnlichen Host stehen die Einrichtungs-Zustände.
    //
    // Vorher prüfte der Test nur den Host-Fall. Damit hing er daran, WO er
    // läuft: auf einem Entwicklerrechner grün, im CI-Container zwangsläufig
    // rot. lcm_host.go weist im Kommentar zu inContainer() ausdrücklich auf
    // diese Falle hin - die Go-Tests umgehen sie über containerCheck, der
    // E2E-Test fährt das echte Binary und kann das nicht.
    const imContainer = page.getByTestId('lcm-host-container');
    // Erst abwarten, dass sich die Karte überhaupt entschieden hat - sonst
    // liefe die Abfrage unten ins Leere, solange noch nichts gerendert ist.
    await expect(imContainer.or(page.getByTestId('trivy-status')).first()).toBeVisible();
    if (await imContainer.isVisible()) {
      await expect(imContainer).toContainText('Container');
    } else {
      await expect(page.getByTestId('trivy-status')).toBeVisible();
      await expect(page.getByTestId('apt-cacher-status')).toBeVisible();
    }
  });

  test('Proxmox-Server: Erkennung mit Typ/Version, gesperrte Aktionen', async ({ page }) => {
    await loginAsAdmin(page);
    // Dashboard zeigt das Produkt statt des generischen Debian.
    await page.goto('/#/');
    await expect(page.locator('tbody')).toContainText('Proxmox VE 8.2.4');

    // Detailseite: Badge mit Produkt + Version, Logo trägt das aria-Label.
    await page.goto('/#/servers/6');
    await expect(page.getByTestId('proxmox-badge')).toContainText('Proxmox VE 8.2.4');
    await expect(page.getByRole('img', { name: 'Proxmox VE' }).first()).toBeVisible();

    // Gesperrte Aktionen: Firewall und User-Sync (im Aktionen-Menü) sind deaktiviert.
    await expect(page.getByRole('button', { name: /^Firewall/ })).toBeDisabled();
    await page.getByTestId('server-actions-toggle').click();
    await expect(page.getByRole('button', { name: /User synchronisieren/ })).toBeDisabled();

    // Repositories-Tab: kein „Hinzufügen“-Formular, stattdessen der Sperr-Hinweis.
    await page.getByRole('button', { name: 'Repositories' }).click();
    await expect(page.locator('body')).toContainText('Paketquellen werden von Proxmox verwaltet');
    await expect(page.locator('select', { hasText: 'Bekanntes Repository hinzufügen' })).toHaveCount(0);
  });

  test('Server-Detail zeigt Hardware und Aktionen', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/servers/1');
    await expect(page.locator('h1')).toContainText('web01');
    await expect(page.locator('body')).toContainText('Hardware');
    // web01 ist im Demo gehärtet -> SSH-Button bietet das Aufheben an.
    await expect(page.getByRole('button', { name: /SSH gehärtet .* aufheben/ })).toBeVisible();
    // Seltener genutzte Aktionen stecken im "Aktionen"-Dropdown.
    await page.getByTestId('server-actions-toggle').click();
    await expect(page.getByRole('button', { name: 'User synchronisieren' })).toBeVisible();
    await expect(page.getByRole('button', { name: /Neustart/ })).toBeVisible();
  });

  test('Server-Detail: Plattform (Virtualisierung), OS-Support und Status-Popover', async ({ page }) => {
    await loginAsAdmin(page);

    // web01 ist im Demo eine VM (KVM), Debian 12 (unterstützt, neuere Version verfügbar).
    await page.goto('/#/servers/1');
    await expect(page.locator('body')).toContainText('Plattform');
    await expect(page.locator('body')).toContainText('Virtuelle Maschine (KVM)');
    await expect(page.locator('body')).toContainText('Unterstützt bis 2028-06');
    await expect(page.locator('body')).toContainText('Neuere Version Debian 13');

    // web01 hat im Demo eine KRITISCHE CVE → Ampel rot. Das Status-Popover (ⓘ)
    // nennt die kritische Sicherheitslücke als Grund. Das Popover schließt
    // sich bei jedem Scroll-Ereignis - nachladende Inhalte können es direkt
    // nach dem Klick wieder zuklappen, daher Klick + Sichtprüfung mit Retry.
    await expect(async () => {
      await page.getByRole('button', { name: /^Warum/ }).click();
      await expect(page.getByRole('dialog')).toBeVisible({ timeout: 1000 });
    }).toPass();
    await expect(page.getByRole('dialog')).toContainText('Warum Kritisch?');
    await expect(page.getByRole('dialog')).toContainText('kritische Sicherheitslücke');

    // db01 ist ein LXC-Container mit Ubuntu 22.04 LTS.
    await page.goto('/#/servers/2');
    await expect(page.locator('body')).toContainText('Container (LXC)');
    await expect(page.locator('body')).toContainText('Ubuntu 24.04');
  });

  test('Server-Detail: Snaps-Tab nur bei Snap-Servern, mit Version/Kanal/Update', async ({ page }) => {
    await loginAsAdmin(page);

    // db01 (Ubuntu) hat ZWEI Paketverwaltungen: apt UND snap - beide werden
    // getrennt ausgewiesen (Badge in der Übersicht + eigener Snaps-Tab).
    await page.goto('/#/servers/2');
    await expect(page.locator('body')).toContainText('APT (dpkg)');
    await expect(page.locator('body')).toContainText('Snap (3)');
    const snapsTab = page.getByRole('button', { name: /^Snaps/ });
    await expect(snapsTab).toBeVisible();
    await snapsTab.click();
    await expect(page.locator('thead')).toContainText('Kanal');
    await expect(page.locator('tbody')).toContainText('lxd');
    await expect(page.locator('tbody')).toContainText('latest/stable');
    await expect(page.locator('tbody')).toContainText('5.0.3'); // verfügbares Update

    // web01 (Debian) hat keine Snaps -> kein Snaps-Tab.
    await page.goto('/#/servers/1');
    await expect(page.getByRole('button', { name: 'Pakete' })).toBeVisible();
    await expect(page.getByRole('button', { name: /^Snaps/ })).toHaveCount(0);
  });

  test('Server-Detail: RPM-Distributionen (dnf/zypper) mit Paketverwaltung und Paketen', async ({ page }) => {
    await loginAsAdmin(page);

    // rocky01 (Rocky Linux, dnf) - Paketverwaltung wird erkannt und angezeigt.
    await page.goto('/#/servers/4');
    await expect(page.locator('h1')).toContainText('rocky01');
    await expect(page.locator('body')).toContainText('DNF (RPM)');
    await page.getByRole('button', { name: 'Pakete' }).click();
    await expect(page.getByTestId('pkg-table')).toContainText('nginx');
    await expect(page.getByTestId('pkg-table')).toContainText('3.0.7-27.el9'); // verfügbares Update

    // suse01 (openSUSE Leap, zypper).
    await page.goto('/#/servers/5');
    await expect(page.locator('h1')).toContainText('suse01');
    await expect(page.locator('body')).toContainText('Zypper (RPM)');
  });

  test('Server-Detail: Sicherheit-Tab listet CVEs mit Schweregrad', async ({ page }) => {
    await loginAsAdmin(page);
    // web01 hat im Demo eine kritische + eine hohe CVE.
    await page.goto('/#/servers/1');
    const secTab = page.getByRole('button', { name: /^\s*Sicherheit/ });
    await expect(secTab).toBeVisible();
    await secTab.click();
    await expect(page.locator('body')).toContainText('Sicherheitslücken (CVE)');
    // Der Tab enthält jetzt auch die Werkzeug-Verwaltung mit eigener Tabelle -
    // deshalb gezielt die CVE-Tabelle prüfen.
    const vulnTable = page.getByTestId('vuln-table');
    await expect(vulnTable).toContainText('CVE-2023-0286');
    await expect(vulnTable).toContainText('openssl');
    // Kritischer Fund als roter Badge.
    await expect(vulnTable.locator('tbody .badge.bg-danger', { hasText: 'Kritisch' }).first()).toBeVisible();
  });

  test('Server-Detail: Sicherheits-Karten sind eingeklappt, SSH-2FA erst nach Einrichtung', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/servers/1');
    await page.getByRole('button', { name: /^\s*Sicherheit/ }).click();

    // Solange 2FA nicht eingerichtet ist, gibt es keine Karte dafuer - eine
    // Karte, die nur „nicht aktiv" meldet, kostet Platz und sagt nichts.
    await expect(page.getByTestId('ssh-2fa-card')).toHaveCount(0);

    // Die Werkzeug-Karte ist zu: Zustand sichtbar, Knoepfe nicht.
    const karte = page.getByTestId('sec-manage-fail2ban');
    await expect(karte).toBeVisible();
    await expect(page.getByTestId('sec-state-fail2ban')).toBeVisible();
    await expect(page.getByTestId('sec-start-fail2ban')).toHaveCount(0);

    // Aufklappen bringt die Bedienung.
    await page.getByTestId('sec-manage-fail2ban-toggle').click();
    await expect(page.getByTestId('sec-start-fail2ban')).toBeVisible();

    // Und wieder zu.
    await page.getByTestId('sec-manage-fail2ban-toggle').click();
    await expect(page.getByTestId('sec-start-fail2ban')).toHaveCount(0);
  });

  test('Server-Detail: SSH-2FA steht bei den Sicherheits-Tools zur Einrichtung', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/servers/1');
    await page.getByTestId('server-actions-toggle').click();
    await page.getByTestId('server-action-security-tools').click();

    // Drei Werkzeuge zur Auswahl - 2FA wird hier eingerichtet, nicht auf
    // der Seite selbst.
    const auswahl = page.locator('#sec-tool option');
    await expect(auswahl).toContainText(['- wählen -', 'fail2ban', 'CrowdSec', /SSH-2FA/]);

    // Bei 2FA gibt es keine Allowlist, dafuer den Warnhinweis.
    await page.locator('#sec-tool').selectOption('ssh-2fa');
    await expect(page.getByTestId('sec-ssh2fa-intro')).toBeVisible();
    await expect(page.getByTestId('sec-allowlist')).toHaveCount(0);
    await expect(page.getByTestId('sec-install')).toBeEnabled();
  });

  test('Server-Detail: CVE-Liste zeigt die Quelle und laesst sich danach filtern', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/servers/1');
    await page.getByRole('button', { name: /^\s*Sicherheit/ }).click();
    const vulnTable = page.getByTestId('vuln-table');

    // Die Quelle steht jetzt in einer eigenen Spalte - vorher war einem
    // Fund nicht anzusehen, ob er das Betriebssystem oder ein Image betrifft.
    await expect(vulnTable).toContainText('Quelle');
    await expect(vulnTable).toContainText('System');

    // Filter auf Docker: Es bleiben nur Container-Funde stehen.
    await page.getByTestId('vuln-source-filter').selectOption('docker');
    const lines = vulnTable.locator('tbody tr');
    const count = await lines.count();
    for (let i = 0; i < count; i++) {
      const line = lines.nth(i);
      if ((await line.locator('td').count()) < 2) continue; // Leerzeile
      await expect(line).toContainText('Docker');
    }

    // Filter auf System: kein einziger Docker-Badge mehr.
    await page.getByTestId('vuln-source-filter').selectOption('os');
    await expect(vulnTable.locator('tbody .badge.text-bg-info')).toHaveCount(0);
  });

  test('Sicherheit-Tab: fail2ban verwalten (Dienst, Allowlist, Sperrliste)', async ({ page }) => {
    await loginAsAdmin(page);
    // web01 hat im Demo fail2ban installiert und aktiv, db01 CrowdSec (inaktiv).
    await page.goto('/#/servers/1');
    await page.getByRole('button', { name: /^\s*Sicherheit/ }).click();
    const card = page.getByTestId('sec-manage-fail2ban');
    await expect(card).toBeVisible();
    // Der Zustand steht in der Kopfzeile und ist auch zugeklappt sichtbar;
    // die Bedienung kommt erst beim Aufklappen (seit v1.23).
    await expect(page.getByTestId('sec-state-fail2ban')).toContainText('aktiv');
    await page.getByTestId('sec-manage-fail2ban-toggle').click();
    // Dienst-Knöpfe, Deinstallieren und die Allowlist-Mehrfachauswahl.
    for (const act of ['start', 'stop', 'restart', 'enable', 'disable', 'uninstall']) {
      await expect(page.getByTestId(`sec-${act}-fail2ban`)).toBeVisible();
    }
    await expect(page.getByTestId('sec-allowlist-lists-fail2ban')).toContainText('Büro-Netze');
    // Sperrliste: Demo-Server werden nie per SSH kontaktiert => leer.
    await expect(page.getByTestId('sec-bans-empty-fail2ban')).toContainText('Keine aktiven Sperren');

    // Eine Aktion meldet erst „gestartet" und danach das ECHTE Ergebnis -
    // die Meldung steht in der fixierten Toast-Region, nicht am Seitenanfang.
    await page.getByTestId('sec-restart-fail2ban').click();
    const toasts = page.getByTestId('toast-region');
    await expect(toasts).toContainText('gestartet');
    await expect(toasts.locator('.alert-success')).toContainText('abgeschlossen', { timeout: 25_000 });

    // db01: dieselbe Karte für CrowdSec, dort inaktiv.
    await page.goto('/#/servers/2');
    await page.getByRole('button', { name: /^\s*Sicherheit/ }).click();
    await expect(page.getByTestId('sec-manage-crowdsec')).toBeVisible();
    await expect(page.getByTestId('sec-state-crowdsec')).toContainText('inaktiv');
    await expect(page.getByTestId('sec-manage-fail2ban')).toHaveCount(0);

    // cache01 hat kein Werkzeug installiert => Hinweis statt Karte.
    await page.goto('/#/servers/3');
    await page.getByRole('button', { name: /^\s*Sicherheit/ }).click();
    await expect(page.getByTestId('sec-manage-none')).toContainText('kein Sicherheits-Tool installiert');
  });

  test('Server-Detail: Anwendungs-Reiter benennt beide Hälften und verweist auf den Katalog', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/servers/1');
    await page.getByTestId('tab-apps').click();
    // Ohne Scan ist noch nichts gefunden - der Reiter sagt das, statt leer zu
    // bleiben, und erklärt beide Hälften.
    await expect(page.getByTestId('apps-none')).toContainText('keine Anwendung aus dem Katalog');
    await expect(page.getByRole('heading', { name: 'Ohne Zuordnung' })).toBeVisible();
    // Der Weg zum Katalog steht im Reiter und führt auf die Einstellungsseite.
    await page.getByRole('link', { name: 'Anwendungskatalog' }).click();
    await expect(page.getByTestId('app-catalog')).toContainText('AdGuard Home');
  });

  test('Regelbausteine: die Kopie zählt durch, statt am Namen zu scheitern', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/linux-users/profile-blocks');
    const table = page.locator('table');

    // Zweimal denselben Baustein kopieren: Der Name muss beim zweiten Mal
    // durchzählen - sonst scheitert genau der Knopf, der der vorgesehene Weg
    // ist, einen mitgelieferten Baustein anzupassen.
    const row = page.getByTestId('block-apache-betreiben');
    await row.getByRole('button', { name: 'Klonen' }).click();
    await expect(table).toContainText('Apache betreiben (Kopie)');
    await page.getByTestId('block-apache-betreiben').getByRole('button', { name: 'Klonen' }).click();
    await expect(table).toContainText('Apache betreiben (Kopie) 2');

    // Wieder aufräumen - der Test soll seine Ausgangslage hinterlassen.
    for (const name of ['Apache betreiben (Kopie) 2', 'Apache betreiben (Kopie)']) {
      page.once('dialog', (d) => d.accept());
      await table.locator('tr', { hasText: name }).first().getByRole('button', { name: 'Löschen' }).click();
      await expect(table).not.toContainText(name);
    }
  });

  test('Einstellungen: der Anwendungskatalog liefert Steckbriefe mit und nimmt eigene auf', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/settings/apps');
    const tabelle = page.getByTestId('app-catalog');
    await expect(tabelle).toContainText('Nextcloud');
    await expect(tabelle.locator('tr', { hasText: 'Nextcloud' })).toContainText('mitgeliefert');

    await page.getByTestId('app-add').click();
    await page.locator('#app-slug').fill('e2e-testanwendung');
    await page.locator('#app-name').fill('E2E-Testanwendung');
    await page.locator('#app-markers').fill('path /opt/e2e-test');
    await page.getByTestId('app-save').click();
    await expect(tabelle).toContainText('E2E-Testanwendung');

    // Wieder aufräumen - der Test soll seine Ausgangslage hinterlassen.
    page.once('dialog', (d) => d.accept());
    await tabelle.locator('tr', { hasText: 'E2E-Testanwendung' }).getByRole('button', { name: 'Löschen' }).click();
    await expect(tabelle).not.toContainText('E2E-Testanwendung');
  });

  test('Server-Detail: Benutzer-Tab zeigt Konten mit Login-Art, 2FA und Aktionen', async ({ page }) => {
    await loginAsAdmin(page);
    // web01 hat im Demo vier Konten (root, lcm-svc, anna mit 2FA, legacy-app).
    await page.goto('/#/servers/1');
    await page.getByTestId('tab-users').click();
    const table = page.getByTestId('users-table');
    await expect(table).toContainText('root');
    await expect(table).toContainText('anna');
    // Login-Art: root mit Passwort (Warn-Badge), anna nur mit SSH-Key.
    const annaRow = table.locator('tr', { hasText: 'anna' });
    await expect(annaRow).toContainText('Nur SSH-Key');
    await expect(annaRow.getByTestId('user-2fa-anna')).toContainText('aktiv');
    const rootRow = table.locator('tr', { hasText: 'root' });
    await expect(rootRow).toContainText('Passwort');
    // root und der Service-User sind geschützt: keine Aktions-Knöpfe. Der
    // Aufklapper für die Anmelde-Historie ist etwas anderes und darf da sein -
    // deshalb gezielt auf die Aktionen prüfen statt auf „gar keine Knöpfe".
    for (const name of ['root', 'lcm-svc']) {
      await expect(page.getByTestId(`user-toggle-${name}`)).toHaveCount(0);
      await expect(page.getByTestId(`user-remove-${name}`)).toHaveCount(0);
    }
    // Das Altkonto ist verwaltbar: Deaktivieren + Entfernen sichtbar.
    await expect(page.getByTestId('user-toggle-legacy-app')).toContainText('Deaktivieren');
    await expect(page.getByTestId('user-remove-legacy-app')).toBeVisible();
    // Verwaltetes Konto (anna): sperrbar, aber NICHT endgültig entfernbar -
    // das legte der nächste Sync ohnehin wieder an.
    await expect(page.getByTestId('user-toggle-anna')).toContainText('Deaktivieren');
    await expect(page.getByTestId('user-remove-anna')).toHaveCount(0);
    // Sync-Knopf neben dem Neu-Erheben.
    await expect(page.getByTestId('users-sync')).toBeVisible();
    // Anmelde-Historie aufklappen: mehrere Sitzungen mit Herkunft und Dauer.
    await page.getByTestId('user-logins-anna').click();
    const hist = page.getByTestId('user-logins-row-anna');
    await expect(hist).toContainText('192.168.10.27');
    await expect(hist).toContainText('läuft noch');
    await expect(hist.locator('tbody tr')).toHaveCount(3);

    // db01: deaktiviertes Konto ist markiert und bietet „Aktivieren" an.
    await page.goto('/#/servers/2');
    await page.getByTestId('tab-users').click();
    await expect(page.getByTestId('user-disabled-praktikant')).toContainText('deaktiviert');
    await expect(page.getByTestId('user-toggle-praktikant')).toContainText('Aktivieren');
  });

  test('Sicherheit-Tab: SSH-2FA erscheint erst nach der Einrichtung', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/servers/1');
    await page.getByRole('button', { name: /^\s*Sicherheit/ }).click();
    // Seit v1.23 keine Karte fuer etwas, das nicht eingerichtet ist - die
    // Einrichtung laeuft ueber „Sicherheits-Tools" im Aktionsmenue.
    await expect(page.getByTestId('ssh-2fa-card')).toHaveCount(0);
    await expect(page.getByTestId('ssh-2fa-enable')).toHaveCount(0);
  });

  test('Server-Detail: Festplatten-Tab zeigt Verlauf, Volumes und Chart-Hover', async ({ page }) => {
    await loginAsAdmin(page);
    // web01 hat im Demo 30 Tage Speicher-Verlauf.
    await page.goto('/#/servers/1');
    const storageTab = page.getByRole('button', { name: 'Festplatten' });
    await expect(storageTab).toBeVisible();
    await storageTab.click();
    await expect(page.locator('body')).toContainText('Festplattenbelegung');
    // Aktueller Belegungs-Badge (Prozent + Größe).
    await expect(page.locator('body')).toContainText('Aktuell:');
    // Das Verlaufsdiagramm wird als Inline-SVG gerendert.
    const chart = page.locator('svg[aria-label="Verlauf der Festplattenbelegung in Prozent"]');
    await expect(chart).toBeVisible();
    await expect(page.locator('body')).toContainText('30 Tag(e) erfasst');
    // Prognose-Badge: web01 wächst langsam → Unbegrenzt.
    await expect(page.locator('body')).toContainText('Prognose: Unbegrenzt');

    // Hover über das Diagramm blendet ein Tooltip mit Prozentwert ein.
    await chart.hover({ position: { x: 300, y: 120 } });
    await expect(page.getByTestId('chart-hover-label')).toBeVisible();
    await expect(page.getByTestId('chart-hover-label')).toContainText('%');

    // Volume-Tabelle: web01 hat mehrere Dateisysteme, Root „/" mit System-Badge.
    const volumes = page.getByTestId('volumes-table');
    await expect(volumes).toBeVisible();
    await expect(volumes).toContainText('/data');
    await expect(volumes).toContainText('System'); // Badge am Root-Volume
    await expect(volumes).toContainText('GiB');
  });

  test('Server-Detail: Volume-Überwachung ein-/ausschalten, Netz-Mount gesperrt', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/servers/1');
    await page.getByRole('button', { name: 'Festplatten' }).click();
    const volumes = page.getByTestId('volumes-table');
    await expect(volumes).toBeVisible();

    // Inode-Spalte und Überwachungs-Spalte sind da.
    await expect(volumes).toContainText('Inodes');
    await expect(volumes).toContainText('Überwachung');

    // /data ist lokal (xfs) und überwachbar: Schalter vorhanden, aus.
    const data = volumes.locator('[data-testid="volume-row"][data-mount="/data"]');
    const schalter = data.getByTestId('volume-monitor-toggle');
    await expect(schalter).toBeVisible();

    // Von einem bekannten Zustand ausgehen: Die Überwachung ist eine
    // Einstellung, die absichtlich bestehen bleibt - auch über einen
    // abgebrochenen Testlauf hinweg. Ein Wiederholungslauf fände sie sonst
    // eingeschaltet vor und scheiterte an der ersten Zusicherung, ohne dass
    // an der Anwendung etwas wäre.
    if (await schalter.isChecked()) {
      await schalter.click();
    }
    await expect(schalter).not.toBeChecked();
    await expect(data.getByTestId('volume-monitor-threshold')).toHaveCount(0);

    // Einschalten: Grenze erscheint mit der Vorgabe 85.
    await schalter.click();
    await expect(schalter).toBeChecked();
    const grenze = data.getByTestId('volume-monitor-threshold');
    await expect(grenze).toBeVisible();
    await expect(grenze).toHaveValue('85');

    // Grenze ändern und prüfen, dass sie den Neuaufbau der Seite überlebt -
    // die Einstellung liegt in einer eigenen Tabelle, nicht im Scan-Ergebnis.
    await grenze.fill('90');
    await grenze.dispatchEvent('change');
    await expect(grenze).toHaveValue('90');
    await page.reload();
    await page.getByRole('button', { name: 'Festplatten' }).click();
    const dataNeu = page.getByTestId('volumes-table').locator('[data-testid="volume-row"][data-mount="/data"]');
    await expect(dataNeu.getByTestId('volume-monitor-toggle')).toBeChecked();
    await expect(dataNeu.getByTestId('volume-monitor-threshold')).toHaveValue('90');

    // Ausschalten: Grenze verschwindet.
    await dataNeu.getByTestId('volume-monitor-toggle').click();
    await expect(dataNeu.getByTestId('volume-monitor-toggle')).not.toBeChecked();
    await expect(dataNeu.getByTestId('volume-monitor-threshold')).toHaveCount(0);

    // Der Netz-Mount trägt die Marke und bietet KEINEN Schalter an.
    const netz = page.getByTestId('volumes-table').locator('[data-testid="volume-row"][data-mount="/mnt/backup"]');
    await expect(netz).toContainText('Netzspeicher');
    await expect(netz).toContainText('nicht überwachbar');
    await expect(netz.getByTestId('volume-monitor-toggle')).toHaveCount(0);
  });

  test('Server-Detail: Zustand der Speicher-Verbünde zeigt degradierten Pool', async ({ page }) => {
    await loginAsAdmin(page);
    // db01 hat im Demo einen ZFS-Mirror ohne Redundanz.
    await page.goto('/#/servers/2');
    await page.getByRole('button', { name: 'Festplatten' }).click();
    await expect(page.locator('body')).toContainText('Zustand der Speicher-Verbünde');
    const health = page.getByTestId('storage-health-table');
    await expect(health).toBeVisible();
    const zeile = health.getByTestId('storage-health-row').first();
    await expect(zeile).toContainText('ZFS-Pool');
    await expect(zeile).toContainText('tank');
    await expect(zeile).toContainText('DEGRADED');
    await expect(zeile).toContainText('Prüfsummenfehler');
    // Der Befund färbt den Server: Kopfzeile auf „Warnung", und hinter dem
    // ⓘ-Knopf steht er als übersetzter Satz - nicht als roher Schlüssel.
    await expect(page.locator('body')).toContainText('Warnung');
    // Das Popover schließt sich bei jedem scroll- oder resize-Ereignis von
    // selbst (StatusBadge.svelte) - das ist gewollt, macht es für einen Test
    // aber zu einem flüchtigen Ziel. Deshalb Öffnen und Ablesen zusammen
    // wiederholen, statt sich darauf zu verlassen, dass es offen bleibt.
    const knopf = page.locator('button[aria-label]', { hasText: 'ⓘ' }).first();
    await expect(async () => {
      await knopf.click();
      const inhalt = await page.getByRole('dialog').innerText();
      expect(inhalt).toContain('ZFS-Pool tank');
      // Der Befund muss als übersetzter Satz erscheinen, nicht als roher
      // i18n-Schlüssel - fehlte die Übersetzung, stünde hier "insights.…".
      expect(inhalt).not.toContain('insights.');
    }).toPass({ timeout: 15000 });
  });

  test('Server-Detail: Festplatten-Tab zeigt Prognose-Warnung bei db01', async ({ page }) => {
    await loginAsAdmin(page);
    // db01 hat 310 MB/Tag Zuwachs und ca. 11 GB frei → ~37 Tage Restlaufzeit.
    await page.goto('/#/servers/2');
    const storageTab = page.getByRole('button', { name: 'Festplatten' });
    await expect(storageTab).toBeVisible();
    await storageTab.click();
    await expect(page.locator('body')).toContainText('Prognose: noch ca.');
    await expect(page.locator('body')).toContainText('Tag(e)');
    // Fußzeile zeigt Trendtext.
    await expect(page.locator('body')).toContainText('Linearer Trend');
    // Zusätzliches Volume /var/lib/mysql wird erfasst.
    await expect(page.getByTestId('volumes-table')).toContainText('/var/lib/mysql');
  });

  test('Globale Sicherheitsseite listet CVEs über alle Server', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/security');
    await expect(page.locator('h1')).toContainText('Sicherheit');
    // Betroffene Server + CVE-Kennungen erscheinen, kritischste zuerst.
    await expect(page.locator('tbody')).toContainText('web01');
    await expect(page.locator('tbody')).toContainText('CVE-2023-0286');
    await expect(page.locator('tbody')).toContainText('rocky01');
    // Der erste Treffer ist der kritische.
    await expect(page.locator('tbody tr').first()).toContainText('Kritisch');
    // Sammel-Update-Button + Docker-Filter sind vorhanden.
    await expect(page.getByTestId('bulk-update-all')).toBeVisible();
    // Der Demo-Bestand enthält einen Docker-Image-Fund (nginx:1.25).
    await expect(page.locator('tbody')).toContainText('Docker');
    const hideDocker = page.locator('#hide-docker');
    await expect(hideDocker).toBeVisible();
    // Docker-CVEs ausblenden → die Tabelle enthält danach keine Docker-Badges.
    await hideDocker.check();
    await expect(page.locator('tbody')).not.toContainText('Docker');
  });

  test('Sicherheitsseite nennt den Stand der Schwachstellen-Datenbank', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/security');
    // Der Stand steht ÜBER der Fundliste: Wie belastbar die Liste ist, haengt
    // genau daran.
    //
    // Die e2e-Instanz bekommt ein frisches Datenverzeichnis und damit einen
    // leeren Trivy-Cache - je nach Maschine ist Trivy gar nicht installiert
    // oder installiert, hat aber noch nie eine Datenbank geladen. Beide Faelle
    // sind ehrlich zu melden, und beide sagen dasselbe: Diese Liste ist keine
    // Entwarnung. Genau darauf pruefen wir.
    const db = page.getByTestId('cve-db-status');
    await expect(db).toBeVisible();
    await expect(db).toContainText('CVE-Bewertung');
    // Kein stiller Durchmarsch: Die Zeile ist als Hinweis erkennbar.
    await expect(db).toHaveClass(/alert/);

    // Die Abschottung des Scanners gehoert hierher und nicht nur auf die
    // LCM-Host-Karte: Die gibt es nur, wenn der eigene Rechner als Server
    // aufgenommen ist - im Container ist er das bewusst nicht. Ausgerechnet
    // beim Schutz des Master-Keys darf „nicht sichtbar" nicht die Antwort
    // sein. Ob Trivy auf dieser Maschine ueberhaupt vorhanden ist, haengt vom
    // Runner ab; ist es das, MUSS der Zustand dastehen.
    if (await page.getByTestId('cve-db-update').count()) {
      await expect(db.getByTestId('trivy-sandbox')).toBeVisible();
    }
  });

  test('Alarme: Regel-Typ „CVE-Datenbank veraltet" ist waehlbar', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/settings/alerts');
    await page.getByRole('button', { name: '+ Alarmregel' }).click();
    const typeSelect = page.locator('#al-type');
    await expect(typeSelect).toBeVisible();
    await typeSelect.selectOption('cve_db_stale');
    // Der Hinweis erklaert, warum es diesen Alarm ueberhaupt braucht.
    await expect(page.locator('.modal-body')).toContainText('KEINEN Fehler');
  });

  test('Einstellungen: CVE-Scan-Schalter vorhanden', async ({ page }) => {
    await loginAsAdmin(page);
    // Seit v1.23 unter „Sicherheit" statt „Allgemein": Der Block stand dort
    // zwischen Mail und Log-Aufbewahrung, wer ihn suchte, fand ihn nicht.
    await page.goto('/#/settings/security');
    await expect(page.locator('body')).toContainText('CVE-Scan (Trivy)');
    await expect(page.locator('#cve-en')).toBeVisible();
    await expect(page.locator('#cve-cron')).toBeVisible();
  });

  test('Pakete-Tab zeigt CVE-Badges direkt am betroffenen Paket', async ({ page }) => {
    await loginAsAdmin(page);
    // web01: openssl hat im Demo eine kritische CVE, nginx eine hohe,
    // curl eine mittlere - jeweils als Badge direkt an der Paketzeile.
    await page.goto('/#/servers/1');
    await page.getByRole('button', { name: 'Pakete' }).click();
    const opensslRow = page.getByTestId('pkg-table').locator('tbody tr', { hasText: 'openssl' }).first();
    await expect(opensslRow).toContainText('1 CVE');
    // Kritische Lücke → roter Badge; Klick springt in den Sicherheit-Tab.
    await opensslRow.locator('button.badge.bg-danger', { hasText: 'CVE' }).click();
    await expect(page.locator('body')).toContainText('Sicherheitslücken (CVE)');
    await expect(page.getByTestId('vuln-table')).toContainText('CVE-2023-0286');
  });

  test('Server-Einstellungen: Docker-Updates sperren und Docker-CVEs ignorieren', async ({ page }) => {
    await loginAsAdmin(page);
    // Ausgangslage belegen: Der Container-Fund von web01 steht in der
    // Sicherheitsübersicht. Ohne diese Zusage wäre die Abwesenheitsprüfung
    // unten wertlos - sie wäre auch grün, wenn dort nie etwas gestanden hätte.
    await page.goto('/#/security');
    await expect(page.locator('body')).toContainText('nginx:1.25');

    await page.goto('/#/servers/1'); // web01 betreibt Docker
    await page.getByTestId('open-settings').click();

    const settings = page.getByTestId('docker-settings');
    await expect(settings).toBeVisible();
    const updatesToggle = page.getByTestId('docker-updates-toggle');
    const cvesToggle = page.getByTestId('docker-cves-toggle');
    // Ausgangslage HERSTELLEN, nicht voraussetzen: Beide sind ab Werk aus,
    // aber ein zuvor abgebrochener Durchlauf kann sie angelassen haben.
    await setzeSchalter(updatesToggle, false);
    await setzeSchalter(cvesToggle, false);

    // Docker-Updates sperren: Der Schalter bleibt nach dem Neuladen gesetzt.
    await updatesToggle.click();
    await expect(updatesToggle).toBeChecked();
    await page.reload();
    await page.getByTestId('open-settings').click();
    await expect(page.getByTestId('docker-updates-toggle')).toBeChecked();

    // CVEs ignorieren: Der Hinweis auf die übersteuerten Ausnahmen erscheint.
    await page.getByTestId('docker-cves-toggle').click();
    await expect(page.getByTestId('docker-cves-toggle')).toBeChecked();
    await expect(settings).toContainText('CVE-relevant');

    // Wirkung: In der Sicherheitsübersicht sind die Container-Funde von web01
    // verschwunden - auch mit ausdrücklichem Docker-Filter.
    await page.goto('/#/security');
    await expect(page.locator('body')).not.toContainText('nginx:1.25');

    // Zurücksetzen, damit die folgenden Tests den Normalzustand vorfinden.
    await page.goto('/#/servers/1');
    await page.getByTestId('open-settings').click();
    await setzeSchalter(page.getByTestId('docker-updates-toggle'), false);
    await setzeSchalter(page.getByTestId('docker-cves-toggle'), false);
  });

  test('Server-Detail: Docker-Tab mit Compose-Projekt und Images', async ({ page }) => {
    await loginAsAdmin(page);
    // web01 betreibt im Demo das Compose-Projekt "webshop" + Standalone.
    await page.goto('/#/servers/1');
    const dockerTab = page.getByRole('button', { name: /^Docker/ });
    await expect(dockerTab).toBeVisible();
    // Tab-Badge: 3 Container + 1 verfügbares Update.
    await expect(dockerTab).toContainText('3');
    await expect(dockerTab).toContainText('1 Update');
    await dockerTab.click();
    // Compose-Projekt mit Update-Button, Standalone-Gruppe getrennt.
    await expect(page.locator('body')).toContainText('Compose-Projekt');
    await expect(page.locator('body')).toContainText('webshop');
    await expect(page.getByRole('button', { name: 'Projekt aktualisieren' })).toBeVisible();
    // Sammel-Aktion: alle genutzten Registry-Images auf einmal ziehen.
    await expect(page.getByTestId('docker-pull-all')).toBeVisible();
    // Pro Container: Umschalter „CVE-relevant" (Docker-CVEs zählen nur für
    // markierte Container in die Ampel).
    await expect(page.getByTestId('cve-relevance-toggle').first()).toBeVisible();
    await expect(page.locator('body')).toContainText('Standalone-Container');
    await expect(page.locator('body')).toContainText('uptime-kuma');
    // Images-Tabelle: nginx veraltet (Pull-Button), lokales Image gekennzeichnet.
    await expect(page.locator('body')).toContainText('nginx:1.25');
    await expect(page.locator('body')).toContainText('Update verfügbar');
    await expect(page.locator('body')).toContainText('lokal gebaut');
    await expect(page.getByRole('button', { name: 'Pull' })).toBeVisible();
    // CVE-Zähler aus dem Image-Scan (1 hohe Lücke im nginx-Image) - sowohl
    // in der Images-Tabelle als auch als Badge an der Container-Zeile.
    await expect(page.locator('body')).toContainText('1 hoch');
    const webRow = page.locator('tbody tr', { hasText: 'webshop-web-1' });
    await expect(webRow).toContainText('1 hoch');
    // Ungenutztes lokales Image (meinapp:dev) hat einen Löschen-Button;
    // genutzte Images nicht.
    const localRow = page.locator('tbody tr', { hasText: 'meinapp:dev' });
    await expect(localRow.getByRole('button', { name: 'Löschen' })).toBeVisible();
    const nginxRow = page.locator('tbody tr', { hasText: 'nginx:1.25' });
    await expect(nginxRow.getByRole('button', { name: 'Löschen' })).toHaveCount(0);

    // cache01 hat kein Docker → kein Tab.
    await page.goto('/#/servers/3');
    await expect(page.getByRole('button', { name: 'Pakete' })).toBeVisible();
    await expect(page.getByRole('button', { name: /^Docker/ })).toHaveCount(0);
  });

  test('Globale Docker-Seite listet unique Images mit Status', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/docker');
    await expect(page.locator('h1')).toContainText('Docker');
    await expect(page.locator('tbody')).toContainText('nginx:1.25');
    await expect(page.locator('tbody')).toContainText('Update verfügbar');
    // Privates Image (db01) ist anonym nicht prüfbar.
    await expect(page.locator('tbody')).toContainText('registry.firma.intern/db/postgres:16');
    await expect(page.locator('tbody')).toContainText('nicht prüfbar (privat)');
    // Filter "Nur mit Update" reduziert die Liste.
    await page.locator('#only-updates').click();
    await expect(page.locator('tbody')).toContainText('nginx:1.25');
    await expect(page.locator('tbody')).not.toContainText('redis:7');
  });

  test('Sicherheitsseite weist die Quelle (OS/Docker) aus', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/security');
    await expect(page.locator('thead')).toContainText('Quelle');
    await expect(page.locator('tbody')).toContainText('Docker');
    await expect(page.locator('tbody')).toContainText('nginx:1.25');
  });

  test('Doku: /doku ist ohne Anmeldung erreichbar und rendert Markdown', async ({ page }) => {
    // Bewusst OHNE Login: die Schlüssel-Anleitung wird gebraucht, bevor man
    // einen Zugang hat (z.B. aus der Aktivierungs-Mail heraus).
    await page.goto('/#/doku');
    const content = page.getByTestId('docs-content');
    await expect(content).toBeVisible();
    await expect(content.locator('h1')).toContainText('SSH-Schlüssel');
    // Die drei Systeme und der PuTTY-Weg müssen drin sein.
    for (const section of ['Linux und macOS', 'Windows mit OpenSSH', 'Windows mit PuTTY']) {
      await expect(content.locator('h2', { hasText: section })).toBeVisible();
    }
    // Markdown wurde wirklich gerendert (nicht als Rohtext ausgegeben).
    await expect(content.locator('pre code').first()).toContainText('ssh-keygen');
    await expect(content.locator('table').first()).toContainText('id_ed25519');
    await expect(content.locator('blockquote').first()).toBeVisible();
    // Kein Roh-HTML aus der Quelle.
    await expect(content.locator('script')).toHaveCount(0);
    // Der Verweis im PuTTY-Abschnitt zeigt auf eine vorhandene Überschrift.
    await expect(page.locator('h2#windows-mit-openssh')).toHaveCount(1);
  });

  test('Doku: zweite Seite (SSH-2FA) mit Seitenliste und Querverweis', async ({ page }) => {
    await page.goto('/#/doku');
    // Ab zwei Seiten erscheint die Seitenliste.
    const nav = page.getByTestId('docs-nav');
    await expect(nav).toBeVisible();
    await nav.getByRole('button', { name: /Zwei-Faktor/ }).click();
    const content = page.getByTestId('docs-content');
    await expect(content.locator('h1')).toContainText('Zwei-Faktor-Anmeldung');
    // Die Anleitung muss den Weg vollständig zeigen - inklusive der Probe in
    // einer zweiten Sitzung und dem Rückweg, sonst sperrt man sich aus.
    await expect(content).toContainText('google-authenticator');
    await expect(content).toContainText('Notfallcodes');
    await expect(content.locator('h2', { hasText: 'Vorher prüfen' })).toBeVisible();
    await expect(content).toContainText('rm ~/.google_authenticator');
    // Querverweis auf die Schlüssel-Seite.
    await expect(content.locator('a[href="/#/doku/ssh-schluessel"]').first()).toBeVisible();
  });

  test('Doku: Geräte-Anleitungen (Agent, RouterOS, Synology)', async ({ page }) => {
    await page.goto('/#/doku');
    const nav = page.getByTestId('docs-nav');
    const content = page.getByTestId('docs-content');

    // Agent: der Port ist die Stelle, an der es sonst hakt.
    await nav.getByRole('button', { name: /Agent/ }).click();
    await expect(content.locator('h1')).toContainText('LCM-Agent');
    await expect(content).toContainText('lcm-agent enroll');
    await expect(content).toContainText('9320');

    // RouterOS: Nur-Lese-Benutzer und Schlüssel-Import.
    await nav.getByRole('button', { name: /MikroTik/ }).click();
    await expect(content.locator('h1')).toContainText('MikroTik');
    await expect(content).toContainText('group=read');
    await expect(content).toContainText('ssh-keys import');

    // Synology: die erzwungene 2FA ist der klassische Stolperstein.
    await nav.getByRole('button', { name: /Synology/ }).click();
    await expect(content.locator('h1')).toContainText('Synology');
    await expect(content).toContainText('administrators');
    await expect(content).toContainText('5001');
    await expect(content).toContainText('Zwei-Faktor');
  });

  test('Doku: über die Navigation erreichbar, auch angemeldet', async ({ page }) => {
    await loginAsAdmin(page);
    await page.getByRole('link', { name: 'Doku' }).click();
    await expect(page.getByTestId('docs-content')).toBeVisible();
  });

  test('Onboarding: Join-Wizard bietet Passwort- und System-Key-Anmeldung', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/servers/join');
    // Standard: Passwort-Feld sichtbar.
    await expect(page.locator('#auth-pw')).toBeChecked();
    await expect(page.locator('#pw')).toBeVisible();
    // Auf System-SSH-Key umschalten → Passwortfeld weg, Key-Hinweis da.
    await page.locator('label[for="auth-key"]').click();
    await expect(page.locator('#pw')).toHaveCount(0);
    await expect(page.locator('body')).toContainText('System-SSH-Key');
    // Aufklappbare Anleitung „Root-SSH öffnen": zu erst zugeklappt, nach dem
    // Aufklappen stehen die Befehle drin.
    const guide = page.getByTestId('root-ssh-guide');
    await expect(guide).toContainText('Root-SSH ist auf dem Server deaktiviert?');
    await expect(guide.locator('ol')).not.toBeVisible();
    await guide.locator('summary').click();
    await expect(guide).toContainText('PermitRootLogin yes');
    await expect(guide).toContainText('sudo passwd root');
    await expect(guide).toContainText('SSH-Root-Login deaktivieren');
  });

  test('Join-Wizard: Synology DSM als eigener Gerätetyp mit Zertifikats-Bestätigung', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/servers/join');
    await page.locator('label[for="mode-dsm"]').click();
    // Eigenes Formular: DSM-Port statt SSH-Port, Konto statt Service-User -
    // und der Hinweis, warum kein Konto mit erzwungener 2FA taugt.
    await expect(page.getByTestId('dsm-host')).toBeVisible();
    await expect(page.getByTestId('dsm-account')).toBeVisible();
    await expect(page.locator('#dsm-port')).toHaveValue('5001');
    await expect(page.locator('body')).toContainText('Zwei-Faktor');
    // Ohne vollständige Eingabe bleibt der Weiter-Knopf gesperrt: der
    // Fingerprint wird geholt, BEVOR Zugangsdaten übertragen werden.
    await expect(page.getByTestId('dsm-probe')).toBeDisabled();
    await page.getByTestId('dsm-name').fill('nas-e2e');
    await page.getByTestId('dsm-host').fill('192.0.2.10');
    await page.getByTestId('dsm-account').fill('lcm');
    await page.getByTestId('dsm-password').fill('geheim');
    await expect(page.getByTestId('dsm-probe')).toBeEnabled();
  });

  test('Einstellungen: Zeit & NTP speichert und bleibt nach dem Neuladen stehen', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/settings/time');
    // Die Felder des PATCH-Rumpfs fehlten im Backend: Speichern antwortete mit
    // 200, die Werte kamen aber nie an und das Formular stand danach wieder leer.
    await page.getByTestId('ntp-presets').fill('NTP-Pool = 0.pool.ntp.org');
    await page.getByTestId('default-timezone').fill('Europe/Berlin');
    await page.getByTestId('time-save').click();
    await expect(page.locator('body')).toContainText('Einstellungen gespeichert');
    // Direkt nach dem Speichern stehen die Werte noch im Formular …
    await expect(page.getByTestId('ntp-presets')).toHaveValue('NTP-Pool = 0.pool.ntp.org');
    await expect(page.getByTestId('default-timezone')).toHaveValue('Europe/Berlin');
    // … und sie überleben das Neuladen (kommen also wirklich aus der Datenbank).
    await page.reload();
    await page.goto('/#/settings/time');
    await expect(page.getByTestId('ntp-presets')).toHaveValue('NTP-Pool = 0.pool.ntp.org');
    await expect(page.getByTestId('default-timezone')).toHaveValue('Europe/Berlin');
  });

  test('Einstellungen: Onboarding-Public-Key wird angezeigt', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/settings/general');
    await expect(page.locator('body')).toContainText('Onboarding-SSH-Key');
    // Der beim ersten Start erzeugte Public Key ist sichtbar (ssh-ed25519 …).
    await expect(page.getByRole('button', { name: 'Kopieren' })).toBeVisible();
    await expect(page.locator('input.font-monospace[readonly]')).toHaveValue(/^ssh-ed25519 /);
  });

  test('Server-Detail: Refresh-Buttons (Hardware / Alles) sind vorhanden', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/servers/1');
    await expect(page.getByRole('button', { name: /Hardware aktualisieren/ })).toBeVisible();
    await expect(page.getByRole('button', { name: /Alles aktualisieren/ })).toBeVisible();
  });

  test('Alarme: Regeln und Kanal-Dropdown sind sofort beim Öffnen gefüllt', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/settings/alerts');
    // Bug 5: Die Regel-Tabelle ist direkt beim ersten Laden gefüllt (Demo
    // seedet zwei Regeln) - ohne dass man erst etwas hinzufügen muss.
    const rulesTable = page.locator('table').first();
    await expect(rulesTable).toContainText('Kritische CVEs melden');
    await expect(rulesTable).toContainText('Ops-Team (E-Mail)');
    // Bug 6: Beim ANLEGEN einer neuen Regel ist der Kanal bereits wählbar.
    await page.getByRole('button', { name: '+ Alarmregel' }).click();
    await expect(page.locator('#al-channel option', { hasText: 'Ops-Team (E-Mail)' })).toHaveCount(1);
  });

  test('Alarme: Hinweis, wenn Regeln niemanden benachrichtigen', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/settings/alerts');
    // Alle Demo-Regeln haben einen Kanal → kein Hinweis (der Normalfall).
    await expect(page.getByTestId('alerts-no-channel')).toHaveCount(0);
    // Eine Regel OHNE Kanal anlegen (Default „nur protokollieren").
    await page.getByRole('button', { name: '+ Alarmregel' }).click();
    await page.locator('#al-name').fill('Nur-Log-Regel');
    // channel_id bleibt auf dem leeren Default.
    await page.getByRole('button', { name: 'Erstellen' }).click();
    // Jetzt warnt LCM, dass diese Regel niemanden erreicht.
    await expect(page.getByTestId('alerts-no-channel')).toBeVisible();
    await expect(page.getByTestId('alerts-no-channel')).toContainText('benachrichtigen niemanden');
  });

  // Dieselbe Schwelle gilt meist fuer mehrere Infrastruktur-Gruppen. Vorher
  // brauchte es dafuer je Gruppe eine eigene, ansonsten identische Regel.
  // Die Auswahl muss zudem mit vielen Gruppen benutzbar bleiben: Suche statt
  // Ankreuzliste, Gewaehltes als Pille mit Kreuz.
  test('Alarme: Regel gilt fuer mehrere Servergruppen', async ({ page }) => {
    await loginAsAdmin(page);
    page.on('dialog', (d) => d.accept());
    await page.goto('/#/settings/alerts');
    await page.getByRole('button', { name: '+ Alarmregel' }).click();
    await page.locator('#al-name').fill('e2e-Mehrfachgruppen');

    // Voreinstellung ist die ausdrueckliche Wahl „alle Server".
    await expect(page.getByTestId('group-scope-all')).toHaveAttribute('aria-pressed', 'true');
    await expect(page.getByTestId('group-search')).toHaveCount(0);

    // Auf Gruppen umstellen und ueber die Suche zwei Gruppen waehlen.
    await page.getByTestId('group-scope-selected').click();
    await expect(page.getByTestId('group-picker-empty')).toBeVisible();
    await page.getByTestId('group-search').fill('Produkt');
    await page.getByTestId('group-options').getByRole('button').first().click();
    await page.getByTestId('group-search').fill('Sys');
    await page.getByTestId('group-options').getByRole('button').first().click();
    await expect(page.getByTestId('group-chips').locator('.badge')).toHaveCount(2);
    await expect(page.getByTestId('group-picker-empty')).toHaveCount(0);
    await page.getByRole('button', { name: 'Erstellen' }).click();

    // Beide Gruppen stehen in der Zeile - und bleiben beim Wiederoeffnen gesetzt.
    const row = page.locator('tr', { hasText: 'e2e-Mehrfachgruppen' });
    await expect(row).toBeVisible();
    await expect(row.locator('td').nth(3)).toContainText(',');
    await row.getByRole('button', { name: 'Bearbeiten' }).click();
    await expect(page.getByTestId('group-scope-selected')).toHaveAttribute('aria-pressed', 'true');
    const chips = page.getByTestId('group-chips').locator('.badge');
    await expect(chips).toHaveCount(2);

    // Kreuz an der Pille nimmt eine Gruppe wieder heraus.
    await chips.first().getByRole('button').click();
    await expect(chips).toHaveCount(1);

    // Zurueck auf „alle Server" verwirft die Auswahl sichtbar.
    await page.getByTestId('group-scope-all').click();
    await expect(page.getByTestId('group-chips')).toHaveCount(0);

    await page.getByRole('button', { name: 'Abbrechen' }).click();
    await row.getByRole('button', { name: 'Löschen' }).click();
    await expect(row).toHaveCount(0);
  });

  test('Einstellungen Allgemein: logische Karten-Reihenfolge, kein Docker-Schalter', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/settings/general');
    // Onboarding-Themen stehen zusammen oben, danach Sicherheit,
    // E-Mail-Versand, Aufbewahrung. Die Scan-Einstellungen sind seit v1.23
    // eine eigene Seite (Einstellungen → Sicherheit).
    const headings = page.locator('form .card h3');
    await expect(headings).toHaveText([
      'Standard-SSH-Zugang (Onboarding)',
      'Onboarding-SSH-Key',
      'Zwei-Faktor-Pflicht',
      'Öffentliche Adresse',
      'Anmeldesession',
      'Job-Überwachung',
      'Standard-E-Mail-Versand',
      'Log-Bereinigung',
      'Speicher-Verlauf',
    ]);
    // Der Docker-Check hat keine eigenen Einstellungen mehr - er läuft als
    // Rule des System-Sync-Schedules.
    await expect(page.locator('#docker-en')).toHaveCount(0);
    await expect(page.locator('#docker-cron')).toHaveCount(0);
  });

  test('Login: Passwort-vergessen-Flow antwortet generisch (keine User-Enumeration)', async ({ page }) => {
    await page.goto('/#/login');
    await page.getByTestId('forgot-password').click();
    await page.locator('#reset-email').fill('unbekannt@example.com');
    await page.getByRole('button', { name: 'Link anfordern' }).click();
    // Auch für unbekannte Adressen dieselbe Bestätigung.
    await expect(page.locator('.alert-success')).toContainText('Wenn ein Konto');
    await page.getByRole('button', { name: 'Zurück zur Anmeldung' }).click();
    await expect(page.locator('#username')).toBeVisible();
  });

  test('Aktivierungsseite ohne Token zeigt Hinweis', async ({ page }) => {
    await page.goto('/#/aktivierung');
    await expect(page.locator('.alert-warning')).toContainText('Kein Aktivierungs-Token');
  });

  test('Benutzerverwaltung (unter Einstellungen) zeigt Seed-User', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/settings/users');
    await expect(page.locator('table')).toContainText('system');
    await expect(page.locator('table')).toContainText('admin');
  });

  test('Linux-Benutzer zeigt die Demo-Accounts', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/linux-users');
    await expect(page.locator('h1')).toContainText('Linux-Benutzer');
    await expect(page.locator('body')).toContainText('deploy');
  });

  test('Linux-Benutzer: Reiter führen zu Profilen und Bausteinen', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/linux-users');
    const tabs = page.getByTestId('linux-users-tabs');
    await expect(tabs.getByRole('link', { name: 'Benutzer' })).toHaveClass(/active/);

    await tabs.getByRole('link', { name: 'Berechtigungsprofile' }).click();
    await expect(page).toHaveURL(/#\/linux-users\/profiles/);
    await expect(page.locator('table')).toContainText('Voll-Administrator');

    await tabs.getByRole('link', { name: 'Regelbausteine' }).click();
    await expect(page).toHaveURL(/#\/linux-users\/profile-blocks/);
    await expect(page.locator('table')).toContainText('Systemd-Dienst betreiben');

    // Alte Lesezeichen aus den Einstellungen landen auf dem neuen Reiter.
    await page.goto('/#/settings/profiles');
    await expect(page).toHaveURL(/#\/linux-users\/profiles/);
  });

  test('Linux-Benutzer anlegen läuft über ein Popup und erscheint in der Tabelle', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/linux-users');
    await page.getByRole('button', { name: '+ Linux-Benutzer' }).click();
    await expect(page.locator('.modal-title')).toContainText('Linux-Benutzer anlegen');
    await page.fill('#lu-name', 'e2e-linux');
    await page.getByRole('button', { name: 'Anlegen', exact: true }).click();
    // Popup schließt, der neue Benutzer steht in der Tabelle.
    await expect(page.getByRole('row', { name: /e2e-linux/ })).toBeVisible();
  });

  test('Linux-Benutzer löschen ist gesperrt, solange er Servern zugeordnet ist', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/linux-users');
    // deploy ist über die Demo-Gruppe "Produktion" auf Servern provisioniert.
    await page.getByRole('row', { name: /deploy/ }).getByRole('button', { name: 'Verwalten' }).click();
    await expect(page.locator('.modal-title')).toContainText('Benutzer: deploy');
    await expect(page.getByRole('button', { name: 'Von allen Servern entfernen' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Benutzer löschen' })).toBeDisabled();
  });

  test('Aktivierungsseite ist ohne Login erreichbar', async ({ page }) => {
    // Ohne Login: die öffentliche Self-Service-Aktivierung zeigt das Formular
    // (nicht die Anmeldemaske).
    await page.goto('/#/linux-aktivierung?token=egal');
    await expect(page.locator('h1')).toContainText('Zugang einrichten');
    await expect(page.locator('body')).toContainText('SSH-Schlüssel');
    // Die Generierungs-Option ist vorhanden und macht das Absenden möglich.
    await page.click('#km-generate');
    await expect(page.locator('body')).toContainText('ed25519-Schlüsselpaar');
    await expect(page.getByRole('button', { name: 'Zugang einrichten' })).toBeEnabled();
  });

  test('SSH-Schlüsselpaar generieren liefert den privaten Schlüssel einmalig', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/linux-users');
    // In der Tabelle die deploy-Zeile öffnen (Verwalten-Popup).
    await page.getByRole('row', { name: /deploy/ }).getByRole('button', { name: 'Verwalten' }).click();
    await page.getByRole('button', { name: 'Schlüsselpaar generieren' }).click();
    // Einmal-Download-Popup mit dem privaten Schlüssel erscheint (über dem Verwalten-Popup).
    await expect(page.getByRole('heading', { name: 'Privater SSH-Schlüssel' })).toBeVisible();
    const download = page.waitForEvent('download');
    await page.getByRole('button', { name: 'Privaten Schlüssel herunterladen' }).click();
    expect((await download).suggestedFilename()).toBe('id_ed25519_deploy');
    // Windows-Anleitung richtet den Schlüssel als Standardschlüssel ein
    // (Host *-Eintrag in der config), sodass „ssh deploy@<server>" ohne -i genügt.
    const dialog = page.getByRole('dialog').filter({ hasText: 'Privater SSH-Schlüssel' });
    await expect(dialog).toContainText('Host *');
    await expect(dialog).toContainText('ssh deploy@<server>');
    // Der neue Public Key steht jetzt in der Key-Liste des Benutzers.
    await expect(page.locator('body')).toContainText('Generiert');
  });

  test('Benutzer anlegen läuft über ein Modal mit Passwortbestätigung', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/settings/users');
    await page.getByRole('button', { name: '+ Benutzer anlegen' }).click();
    // Modal offen: zwei Passwortfelder, Anlegen erst bei Übereinstimmung aktiv.
    await expect(page.locator('.modal-title')).toContainText('Benutzer anlegen');
    await page.fill('#new-username', 'e2e-mgr');
    const pw = page.locator('.modal input[type="password"]');
    // Passwort muss die Policy erfüllen (12+ Zeichen, 3 Zeichenklassen,
    // kein Standard-Passwort, kein Bezug zum Benutzernamen).
    await pw.nth(0).fill('Anker5-Leuchtturm!Wind');
    await pw.nth(1).fill('passt-nicht');
    await expect(page.getByRole('button', { name: 'Anlegen', exact: true })).toBeDisabled();
    await pw.nth(1).fill('Anker5-Leuchtturm!Wind');
    await expect(page.getByRole('button', { name: 'Anlegen', exact: true })).toBeEnabled();
  });

  test('Passwort-Stärkeprüfung lehnt schwache Passwörter mit Begründung ab', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/settings/users');
    await page.getByRole('button', { name: '+ Benutzer anlegen' }).click();
    await page.fill('#new-username', 'e2e-policy');
    const pw = page.locator('.modal input[type="password"]');
    const anlegen = page.getByRole('button', { name: 'Anlegen', exact: true });

    // Standard-Passwort: abgelehnt, mit konkreter Begründung.
    await pw.nth(0).fill('Passwort123!');
    await pw.nth(1).fill('Passwort123!');
    await expect(page.getByTestId('pw-problems')).toContainText('Standard-Passwort');
    await expect(anlegen).toBeDisabled();

    // Enthält den eigenen Benutzernamen: ebenfalls abgelehnt.
    await pw.nth(0).fill('Nordwind-e2e-policy-9!');
    await pw.nth(1).fill('Nordwind-e2e-policy-9!');
    await expect(page.getByTestId('pw-problems')).toContainText('Benutzernamen');
    await expect(anlegen).toBeDisabled();

    // Starkes Passwort: Anzeige wird grün, Anlegen ist möglich.
    await pw.nth(0).fill('Fjord4-Muschel!Wandel');
    await pw.nth(1).fill('Fjord4-Muschel!Wandel');
    await expect(page.getByTestId('pw-strength')).toContainText(/Stark|Sehr stark/);
    await expect(anlegen).toBeEnabled();
  });

  test('Repositories-Tab bietet HTTPS-Umstellung und Repo-Katalog', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/servers/1');
    await page.getByRole('button', { name: 'Repositories' }).click();
    // Demo-Server 1 hat eine http-Quelle => Umstell-Button mit Zähler.
    await expect(page.getByRole('button', { name: /http-Quelle.*auf HTTPS umstellen/ })).toBeVisible();
    await expect(page.locator('.badge.bg-danger', { hasText: 'unverschlüsselt' })).toBeVisible();
    // Katalog bekannter Repositories (Docker & Co) ist wählbar.
    const select = page.locator('select.form-select');
    await expect(select.locator('option', { hasText: 'Docker CE' })).toHaveCount(1);
    await select.selectOption('docker');
    await expect(page.locator('body')).toContainText('download.docker.com');
    await expect(page.getByRole('button', { name: 'Hinzufügen' })).toBeEnabled();
    // APT-Cache-Anbindung: Demo-Server ist nicht angebunden → Verwenden-Button.
    await expect(page.getByRole('button', { name: /APT-Cache verwenden/ })).toBeVisible();
  });

  test('Server neu verbinden öffnet den Reconnect-Wizard mit vorbelegten Daten', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/servers/1');
    await page.getByTestId('server-actions-toggle').click();
    await page.getByRole('button', { name: 'Neu verbinden' }).click();
    // Modal offen, Host aus dem Server vorbelegt, Erklärtext vorhanden.
    await expect(page.locator('.modal-title')).toContainText('Server neu verbinden');
    await expect(page.locator('body')).toContainText('überschreibt die gespeicherten Credentials');
    await expect(page.locator('#rc-host')).not.toHaveValue('');
    await expect(page.locator('#rc-user')).toHaveValue('root');
  });

  test('Neustart-Aktion im Aktionen-Menü löst einen Job aus (mit Bestätigung)', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/servers/4'); // rocky01 - von keinem späteren Test mehr referenziert.
    page.once('dialog', (d) => d.accept());
    await page.getByTestId('server-actions-toggle').click();
    await page.getByTestId('server-action-reboot').click();
    await expect(page.locator('.alert-success')).toContainText('Neustart abgeschlossen.');
  });

  test('Rechte einschränken: Aktion öffnet ein erklärendes Bestätigungs-Modal', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/servers/1');
    await page.getByTestId('server-actions-toggle').click();
    await page.getByTestId('server-action-restrict').click();
    // Modal offen, erklärt die Konsequenz und warnt vor der Unumkehrbarkeit.
    await expect(page.locator('.modal-title')).toContainText('Rechte des LCM-Benutzers einschränken');
    await expect(page.locator('.modal-body')).toContainText('Nicht per Klick rückgängig machbar');
    // ... und sagt ehrlich, wogegen der Modus NICHT schützt (BUG-030):
    // Paketverwaltung und Docker führen prinzipbedingt Code als root aus.
    await expect(page.locator('.modal-body')).toContainText('Wogegen das schützt');
    await expect(page.getByTestId('restrict-confirm')).toBeVisible();
    // Ohne zu bestätigen wieder schließen (Demo-Zustand unverändert).
    await page.getByRole('button', { name: 'Abbrechen' }).click();
    await expect(page.locator('.modal-title')).toHaveCount(0);
  });

  test('Firewall-Konfiguration öffnet ein Popup mit Regel-Editor', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/servers/1');
    await page.locator('.btn-group button', { hasText: 'Firewall' }).click();
    await expect(page.locator('.modal-title')).toContainText('Firewall konfigurieren');
    // Backend-Badge: Debian → nftables (erkanntes Werkzeug der Demo).
    await expect(page.getByTestId('fw-backend')).toContainText('nftables');
    await expect(page.locator('body')).toContainText('SSH-Port');
    // Regel-Editor zeigt die konfigurierten Regeln (80 + 443).
    const editor = page.getByTestId('firewall-rules-editor');
    await expect(editor.getByTestId('fw-rule-port')).toHaveCount(2);
    // Die SSH-Freigabe wird als eigene, nicht löschbare Zeile angezeigt: der
    // SSH-Port (22) ist schreibgeschützt, einschränken lässt sie sich über die
    // erlaubten Quellen, und es gibt keinen Löschen-Knopf.
    const sshRow = page.getByTestId('fw-ssh-row');
    await expect(sshRow).toBeVisible();
    await expect(sshRow.locator('input[type="number"]')).toHaveValue('22');
    await expect(sshRow.getByTestId('fw-ssh-sources')).toBeVisible();
    await expect(sshRow.getByRole('button', { name: 'Regel entfernen' })).toHaveCount(0);
    // Die entfernte Bind-/Zieladresse darf nirgends mehr auftauchen - sie
    // stand für dasselbe wie die erlaubten Quellen und hat nur verwirrt.
    await expect(editor.getByTestId('fw-ssh-bind')).toHaveCount(0);
    await expect(page.locator('.modal-body')).not.toContainText('Bind-');
    // web01 ist im Demo firewall-aktiv -> Neu-anwenden + Deaktivieren.
    await expect(page.getByRole('button', { name: 'Regeln neu anwenden' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Deaktivieren' })).toBeVisible();
    // Lauschende Dienste aus dem Port-Scan werden als Vorschlag angeboten;
    // Klick übernimmt Port, Protokoll und IP-Version als neue Regel.
    await expect(page.locator('.modal-body')).toContainText('Lauschende Dienste');
    await page.getByTestId('fw-suggest-listening').filter({ hasText: '9100/tcp' }).click();
    await expect(editor.getByTestId('fw-rule-port')).toHaveCount(3);
    // Übernommener Vorschlag verschwindet aus den Chips.
    await expect(page.getByTestId('fw-suggest-listening').filter({ hasText: '9100/tcp' })).toHaveCount(0);
    // Da im Demo Allowlists existieren, hat jede Regel eine Allowlist-Auswahl
    // (Quell-Einschränkung).
    await expect(editor.getByTestId('fw-rule-allowlist').first()).toBeVisible();
    // Das Quellen-Dropdown enthält zusätzlich ein Feld für eigene IPs/Netze;
    // Einträge erscheinen zusammengefasst im Summary ("2 IP(s)").
    await editor.getByTestId('fw-rule-allowlist').first().click();
    const srcInput = editor.getByTestId('fw-rule-source-ips').first();
    await expect(srcInput).toBeVisible();
    await srcInput.fill('203.0.113.7, 10.0.0.0/24');
    await srcInput.blur();
    await expect(editor.getByTestId('fw-rule-allowlist').first()).toContainText('2 IP(s)');

    // Bemerkung je Regel: der übernommene Vorschlag bringt den Dienstnamen
    // gleich als Notiz mit, das Feld ist frei überschreibbar.
    const comment = editor.getByTestId('fw-rule-comment').last();
    await expect(comment).toBeVisible();
    await comment.fill('Prometheus-Export');
    await expect(comment).toHaveValue('Prometheus-Export');

    // Auch die SSH-Freigabe lässt sich auf Quellen einschränken - mit der
    // Adresse, über die LCM den Server erreicht, als Vorlage.
    await sshRow.getByTestId('fw-ssh-sources').click();
    const lcmBtn = editor.getByTestId('fw-ssh-add-lcm-ip');
    await expect(lcmBtn).toBeVisible();
    await lcmBtn.click();
    await expect(sshRow.getByTestId('fw-ssh-sources')).toContainText('1 IP(s)');
    // Eine Einschränkung OHNE die LCM-Adresse warnt vor dem Aussperren.
    await editor.getByTestId('fw-ssh-source-ips').fill('198.51.100.9');
    await editor.getByTestId('fw-ssh-source-ips').blur();
    await expect(editor.getByTestId('fw-ssh-lockout-warning')).toBeVisible();

    // Lauschende Dienste lassen sich direkt vom Server nachladen.
    await expect(editor.getByTestId('fw-rescan-ports')).toBeVisible();

    // Und das Entscheidende: was beim Anwenden tatsächlich gesendet wird.
    // Die Bemerkung ging genau hier verloren - die Oberfläche baute die
    // Nutzlast Feld für Feld zusammen und ließ sie aus. Der Job selbst darf
    // gegen den Demo-Server scheitern; geprüft wird die Anfrage.
    const [req] = await Promise.all([
      page.waitForRequest((r) => r.url().includes('/firewall') && r.method() === 'POST'),
      page.getByTestId('fw-apply').click(),
    ]);
    const sent = req.postDataJSON();
    expect(sent.rules.some((r) => r.comment === 'Prometheus-Export')).toBeTruthy();
    // Quellen fahren mit, eine Bind-/Zieladresse gibt es nicht mehr.
    expect(sent.rules.some((r) => (r.source_ips ?? []).includes('203.0.113.7'))).toBeTruthy();
    expect(sent.rules.every((r) => r.bind === undefined)).toBeTruthy();
    expect(sent.ssh_bind).toBeUndefined();
  });

  test('IP-Allowlists: Verwaltungsseite mit Anlegen/Bearbeiten/Löschen', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/settings/allowlists');
    // Demo-Seeds sind sichtbar.
    await expect(page.locator('body')).toContainText('Büro-Netze');
    // Neue Liste anlegen.
    await page.getByTestId('allowlist-new').click();
    await page.getByTestId('allowlist-name').fill('E2E-Liste');
    await page.getByTestId('allowlist-entries').fill('198.51.100.0/24\n2001:db8:e2e::1');
    await page.getByTestId('allowlist-save').click();
    const row = page.getByTestId('allowlist-row').filter({ hasText: 'E2E-Liste' });
    await expect(row).toBeVisible();
    await expect(row).toContainText('2'); // zwei Einträge
    // Ungültiger Eintrag wird clientseitig blockiert (Speichern deaktiviert).
    await row.getByRole('button', { name: 'Bearbeiten' }).click();
    await page.getByTestId('allowlist-entries').fill('kein-ip');
    await expect(page.getByTestId('allowlist-save')).toBeDisabled();
    await page.getByRole('button', { name: 'Abbrechen' }).click();
    // Aufräumen: löschen (bestätigt automatisch).
    page.on('dialog', (d) => d.accept());
    await row.getByRole('button', { name: 'Löschen' }).click();
    await expect(page.getByTestId('allowlist-row').filter({ hasText: 'E2E-Liste' })).toHaveCount(0);
  });

  test('Servergruppen zeigen Schedules- und Rules-Tabellen', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/groups');
    // Produktion bringt keinen vorgefertigten Wartungs-Schedule mehr mit -
    // nur die Firewall-Grundsatz-Regel.
    await page.getByRole('button', { name: 'Produktion' }).click();
    await expect(page.locator('body')).toContainText('Noch keine Schedules');
    const rulesTable = page.locator('table').filter({ hasText: 'Konfiguration' });
    const fwRow = rulesTable.locator('tr', { hasText: 'Firewall Webserver' });
    await expect(fwRow).toContainText('Grundsatz');
    await expect(fwRow).toContainText('Ports: 80/tcp, 443/tcp');
    // Die Basis-Schedules liegen in der System-Gruppe: Health-Check und
    // System-Sync (inkl. zentralem Docker-Check als Rule).
    await page.getByRole('button', { name: /^System/ }).click();
    const schedTable = page.locator('table').filter({ hasText: 'Zeitplan' });
    await expect(schedTable.locator('tr', { hasText: 'Health-Check' })).toContainText('*/15 * * * *');
    await expect(schedTable.locator('tr', { hasText: 'System-Sync' })).toContainText('0 4 * * *');
    const sysRules = page.locator('table').filter({ hasText: 'Konfiguration' });
    await expect(sysRules.locator('tr', { hasText: 'Docker-Check' })).toContainText('System-Sync');
  });

  test('Servergruppen: Verwaltungs-User zuweisen und entfernen', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/groups');

    // Produktion bringt den Demo-Manager bereits mit - ohne diese Zuordnung
    // saehe ein Benutzer der Manager-Rolle ueberhaupt keine Server.
    await page.getByRole('button', { name: 'Produktion' }).click();
    const managers = page.getByTestId('group-managers-table');
    await expect(managers).toContainText('ops.manager');

    // Zuweisen in einer zweiten Gruppe und wieder entfernen.
    await page.getByRole('button', { name: 'Staging' }).click();
    await expect(managers).toContainText('Kein Verwaltungs-User zugeordnet');
    await page.locator('select').filter({ hasText: 'Verwaltungs-User hinzufügen' })
      .selectOption({ label: 'ops.manager (Olivia Ops)' });
    await page.getByRole('button', { name: 'Verwaltungs-User hinzufügen' }).click();
    await expect(managers).toContainText('ops.manager');

    await page.getByRole('button', { name: 'Verwaltungs-User entfernen' }).click();
    await expect(managers).toContainText('Kein Verwaltungs-User zugeordnet');
  });

  test('Servergruppe: Name und Beschreibung nachträglich bearbeitbar', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/groups');
    await page.getByRole('button', { name: 'Produktion' }).click();
    // Bearbeiten-Stift öffnet das vorbefüllte Popup.
    await page.locator('button[title="Gruppe bearbeiten"]').click();
    await expect(page.getByRole('heading', { name: 'Gruppe bearbeiten' })).toBeVisible();
    await expect(page.locator('#gf-name')).toHaveValue('Produktion');
    await expect(page.locator('#gf-desc')).toHaveValue('Produktive Server (Demo)');
    // Der Vorrang ist vorbefüllt: Er entscheidet, welche Grundsatz-Regel sich
    // durchsetzt, wenn mehrere Gruppen denselben Server bespielen.
    await expect(page.locator('#gf-priority')).toHaveValue('100');
    // Abbrechen lässt die Demo-Daten unverändert (shared state).
    await page.getByRole('button', { name: 'Abbrechen' }).click();
    // Ohne Popup steht der Vorrang an der Gruppe selbst.
    await expect(page.locator('body')).toContainText('Vorrang: 100');
    // Die geschützte System-Gruppe hat KEINEN Bearbeiten-Stift.
    await page.getByRole('button', { name: /^System/ }).click();
    await expect(page.locator('button[title="Gruppe bearbeiten"]')).toHaveCount(0);
  });

  test('Servergruppen: Rule-Popup öffnet sich und schränkt Typen ein', async ({ page }) => {
    await loginAsAdmin(page);
    const schedRow = await createProduktionSchedule(page, 'e2e-Typen');
    // Erfassungsmaske ist NICHT dauerhaft sichtbar, sondern öffnet per „+".
    await expect(page.locator('.modal-title')).toHaveCount(0);
    // „+" der Rules-Tabelle (per title) öffnet das Popup.
    await page.locator('button[title="Neue Rule"]').click();
    await expect(page.getByRole('heading', { name: 'Neue Rule' })).toBeVisible();
    // Am Schedule stehen die Paket-Typen zur Verfügung.
    const typeSelect = page.locator('#rf-type');
    await expect(typeSelect.locator('option', { hasText: 'Paket-Updates (benannt)' })).toHaveCount(1);
    await expect(typeSelect.locator('option', { hasText: 'Security/Bugfix' })).toHaveCount(1);
    await typeSelect.selectOption('packages');
    await expect(page.getByPlaceholder('z.B. htop, unzip, openssh-server')).toBeVisible();
    // Als Grundsatz-Regel stehen nur Typen mit Soll-Zustand zur Verfügung:
    // Firewall, APT-Cache, ACL einrichten und Rechte-Soll. „Skript" ist hier
    // bewusst NICHT dabei - ein Kommando hat keinen Soll-Zustand und lief
    // unprotokolliert alle 15 Minuten (R2-087); dafür ist ein Zeitplan der
    // richtige Ort.
    await page.locator('#rf-target').selectOption('enforce');
    await expect(page.locator('#rf-type option')).toHaveCount(4);
    await expect(page.locator('#rf-type option', { hasText: 'APT-Cache erzwingen' })).toHaveCount(1);
    await expect(page.locator('#rf-type option', { hasText: 'ACL einrichten' })).toHaveCount(1);
    await expect(page.locator('#rf-type option', { hasText: 'Rechte-Soll halten' })).toHaveCount(1);
    await expect(page.locator('#rf-type option', { hasText: 'Skript' })).toHaveCount(0);
    // Umgekehrt gehören sie NICHT an einen Zeitplan: Dort gäbe es für sie
    // keinen Ausführungspfad.
    await page.locator('#rf-target').selectOption({ index: 0 });
    await expect(page.locator('#rf-type option', { hasText: 'Rechte-Soll halten' })).toHaveCount(0);
    // Aufräumen (shared Demo-State).
    await page.getByRole('button', { name: 'Abbrechen' }).click();
    await schedRow.getByRole('button', { name: 'Schedule löschen' }).click();
  });

  test('Servergruppen: Paket-Rule ohne Paketnamen ist nicht speicherbar', async ({ page }) => {
    await loginAsAdmin(page);
    const schedRow = await createProduktionSchedule(page, 'e2e-Pakete');
    await page.locator('button[title="Neue Rule"]').click();
    await page.locator('#rf-name').fill('LeerePakete');
    await page.locator('#rf-type').selectOption('packages');
    const save = page.getByRole('button', { name: 'Speichern' });
    // Ohne Paketnamen bleibt Speichern gesperrt (kein 500 mehr auslösbar).
    await expect(save).toBeDisabled();
    await page.getByPlaceholder('z.B. htop, unzip, openssh-server').fill('htop, unzip');
    await expect(save).toBeEnabled();
    await page.getByRole('button', { name: 'Abbrechen' }).click();
    await schedRow.getByRole('button', { name: 'Schedule löschen' }).click();
  });

  // Der Cron-Ausdruck bleibt die Wahrheit - der Baukasten schreibt ihn nur und
  // liest ihn zurück. Beide Richtungen werden hier geprüft.
  test('Servergruppen: Zeitplan über den Baukasten zusammenklicken', async ({ page }) => {
    await loginAsAdmin(page);
    page.on('dialog', (d) => d.accept());
    await page.goto('/#/groups');
    await page.getByRole('button', { name: 'Produktion' }).click();
    await page.locator('button[title="Neuer Schedule"]').click();

    // Voreinstellung 0 3 * * * liest der Baukasten als „täglich".
    await expect(page.getByTestId('cron-mode')).toHaveValue('daily');
    await expect(page.getByTestId('cron-time')).toHaveValue('03:00');

    // Baukasten -> Ausdruck: dienstags und donnerstags um 06:30.
    await page.getByTestId('cron-mode').selectOption('weekly');
    await page.getByTestId('cron-time').fill('06:30');
    await page.getByTestId('cron-day-4').click();
    await expect(page.locator('#sf-cron')).toHaveValue('30 6 * * 2,4');

    // Ausdruck -> Baukasten: getippter Minutentakt stellt die Auswahl um.
    await page.locator('#sf-cron').fill('*/15 * * * *');
    await expect(page.getByTestId('cron-mode')).toHaveValue('minutes');
    await expect(page.getByTestId('cron-every')).toHaveValue('15');

    // Was der Baukasten nicht abbildet, bleibt „eigener Ausdruck" - und
    // unverändert stehen.
    await page.locator('#sf-cron').fill('0 3 1 */2 *');
    await expect(page.getByTestId('cron-mode')).toHaveValue('custom');
    await expect(page.locator('#sf-cron')).toHaveValue('0 3 1 */2 *');

    // Gespeichert wird der Ausdruck, den der Baukasten zuletzt geschrieben hat.
    await page.getByTestId('cron-mode').selectOption('monthly');
    await expect(page.locator('#sf-cron')).toHaveValue('30 6 1 * *');
    await page.locator('#sf-name').fill('e2e-Baukasten');
    await page.getByRole('button', { name: 'Speichern' }).click();
    const row = page.locator('table').filter({ hasText: 'Zeitplan' }).locator('tr', { hasText: 'e2e-Baukasten' });
    await expect(row).toContainText('30 6 1 * *');
    await row.getByRole('button', { name: 'Schedule löschen' }).click();
    await expect(row).toHaveCount(0);
  });

  test('Servergruppen: Rule an einem Schedule anlegen (Regression schedule_id)', async ({ page }) => {
    await loginAsAdmin(page);
    // Schedule-Löschen ist mit confirm() abgesichert - automatisch bestätigen.
    page.on('dialog', (d) => d.accept());
    await page.goto('/#/groups');
    await page.getByRole('button', { name: 'Produktion' }).click();
    // Erst einen Schedule anlegen (die Gruppe startet ohne Zeitpläne).
    await page.locator('button[title="Neuer Schedule"]').click();
    await page.locator('#sf-name').fill('e2e-Wartung');
    await page.locator('#sf-cron').fill('0 2 * * *');
    await page.getByRole('button', { name: 'Speichern' }).click();
    const schedTable = page.locator('table').filter({ hasText: 'Zeitplan' });
    const schedRow = schedTable.locator('tr', { hasText: 'e2e-Wartung' });
    await expect(schedRow).toContainText('0 2 * * *');
    // Dann die Rule: Ziel bleibt der erste Schedule (Default), Typ update.
    await page.locator('button[title="Neue Rule"]').click();
    await page.locator('#rf-name').fill('e2e-Update-Rule');
    await page.getByRole('button', { name: 'Speichern' }).click();
    // Keine Fehlermeldung „Schedule auswählen …", sondern Rule erscheint mit
    // dem Schedule als Ziel (schedule_id wurde korrekt gesendet).
    await expect(page.locator('.alert-danger')).toHaveCount(0);
    const rulesTable = page.locator('table').filter({ hasText: 'Konfiguration' });
    const newRow = rulesTable.locator('tr', { hasText: 'e2e-Update-Rule' });
    await expect(newRow).toContainText('e2e-Wartung');
    // Wieder entfernen (shared Demo-State sauber halten) - der Schedule
    // löscht seine Rules mit.
    await schedRow.getByRole('button', { name: 'Schedule löschen' }).click();
    await expect(rulesTable.locator('tr', { hasText: 'e2e-Update-Rule' })).toHaveCount(0);
  });

  test('Server entfernen bietet Bereinigung oder einfaches Löschen', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/servers/1');
    await page.getByTestId('server-actions-toggle').click();
    await page.locator('.dropdown-item.text-danger', { hasText: 'Entfernen' }).click();
    await expect(page.locator('.modal-title')).toContainText('Server entfernen');
    // Purge-Checkbox steuert die Button-Beschriftung.
    const purge = page.locator('#rm-purge');
    await expect(page.locator('body')).toContainText('restlos gelöscht');
    if (await purge.isChecked()) {
      await expect(page.getByRole('button', { name: 'Bereinigen & entfernen' })).toBeVisible();
    }
    await purge.uncheck();
    await expect(page.getByRole('button', { name: 'Nur aus LCM entfernen' })).toBeVisible();
    // Abbrechen ohne zu löschen (Demo-Daten bleiben erhalten).
    await page.getByRole('button', { name: 'Abbrechen' }).click();
    await expect(page.locator('.modal-title')).toHaveCount(0);
  });

  test('Server-Detail hat einen Protokolle-Tab für die SSH-Aufzeichnung', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/servers/1');
    await page.getByRole('button', { name: 'Protokolle' }).click();
    // Erklärtext + Health-Check-Toggle sind vorhanden.
    await expect(page.locator('body')).toContainText('Lückenlose Aufzeichnung jeder SSH-Verbindung');
    await expect(page.locator('#hide-health')).toBeVisible();
    // Demo-Server werden nie per SSH kontaktiert => keine Protokolle.
    await expect(page.locator('body')).toContainText('Keine SSH-Protokolle vorhanden');
  });

  test('Pakete-Tab bietet Update-Schaltflächen und pro Paket eine Aktion', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/servers/1');
    await page.getByRole('button', { name: 'Pakete' }).click();
    // Ad-hoc-Aktionen oben.
    await expect(page.getByRole('button', { name: 'Paketliste aktualisieren' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Alle aktualisieren' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Nur Security/Bugfix' })).toBeVisible();
    // Aufräumen (autoremove) in der Toolbar.
    await expect(page.getByRole('button', { name: 'Aufräumen', exact: true })).toBeVisible();
    // Pro Paket eine Versions-Auswahl (Demo-Server hat Pakete im Seed) und ein
    // gezieltes Entfernen.
    const pkgTable = page.getByTestId('pkg-table');
    await expect(pkgTable.getByRole('button', { name: 'Version…' }).first()).toBeVisible();
    // Der Entfernen-Button trägt einen paket-spezifischen aria-label
    // („Paket … entfernen"), daher per Muster suchen.
    await expect(pkgTable.getByRole('button', { name: /paket .* entfernen/i }).first()).toBeVisible();
    // Paketnamen-Suche filtert die Liste (web01-Seed: curl, nginx, openssl).
    await expect(pkgTable).toContainText('nginx');
    await page.getByTestId('pkg-search').fill('ssl');
    await expect(pkgTable).toContainText('openssl');
    await expect(pkgTable).not.toContainText('nginx');
  });

  test('Pakete: Sortierung über die Tabellenköpfe holt Updates nach oben', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/servers/1');
    await page.getByRole('button', { name: 'Pakete' }).click();
    const lines = page.getByTestId('pkg-table').locator('tbody tr');

    // Nach Namen ist die Reihenfolge alphabetisch - ein Paket mit Update kann
    // dabei irgendwo stehen.
    await page.getByTestId('pkg-sort-update').click();
    // Erster Klick auf „Update" sortiert absteigend: Was ein Update hat, steht
    // oben. Genau dafür drückt man den Kopf.
    await expect(lines.first()).toHaveClass(/table-warning/);

    // Zweiter Klick dreht die Richtung um.
    await page.getByTestId('pkg-sort-update').click();
    await expect(lines.first()).not.toHaveClass(/table-warning/);
  });

  test('Paket-Pins: Kernel schützen, Paket pinnen, Entfernen gesperrt', async ({ page }) => {
    await loginAsAdmin(page);
    // web01 (apt) - dort greift der Kernel-Vorschlag linux-image-*/linux-headers-*.
    await page.goto('/#/servers/1');
    await page.getByRole('button', { name: 'Pakete' }).click();
    const card = page.getByTestId('pin-card');
    await expect(card).toBeVisible();
    // Die Karte ist zugeklappt - sie nimmt sonst den Platz weg, den die
    // Paketliste braucht. Der Zählerstand steht trotzdem in der Kopfzeile.
    await expect(page.getByTestId('pin-count')).toHaveText('0');
    await page.getByTestId('pin-card-toggle').click();
    await expect(page.getByTestId('pin-empty')).toBeVisible();

    // Ein-Klick-Kernelschutz legt die Muster als „nicht entfernen" an.
    await page.getByTestId('pin-kernel').click();
    await expect(card).toContainText('linux-image-*');
    await expect(card).toContainText('linux-headers-*');
    await expect(card).toContainText('nicht entfernen');
    // Bewusst KEIN Einfrieren - das nähme dem Kernel die Sicherheitsupdates.
    await expect(card.locator('tbody')).not.toContainText('Version einfrieren');

    // Ein Paket aus der Liste pinnen: Badge erscheint, Entfernen ist gesperrt.
    const pkgTable = page.getByTestId('pkg-table');
    await pkgTable.getByRole('button', { name: 'nginx pinnen' }).click();
    const nginxRow = pkgTable.locator('tbody tr', { hasText: 'nginx' }).first();
    await expect(nginxRow).toContainText('gepinnt');
    await expect(nginxRow.getByRole('button', { name: 'Paket nginx entfernen' })).toBeDisabled();

    // Anwenden läuft als Job und meldet erst nach dessen Abschluss Erfolg.
    await page.getByTestId('pin-apply').click();
    await expect(page.getByTestId('toast-region').locator('.alert-success'))
      .toContainText('abgeschlossen', { timeout: 25_000 });

    // Aufräumen (shared Demo-State): alle angelegten Pins wieder entfernen.
    page.on('dialog', (d) => d.accept());
    for (let i = 0; i < 3; i++) {
      await card.locator('tbody button', { hasText: '×' }).first().click();
      await expect(card.locator('tbody tr')).toHaveCount(3 - i - 1 || 1);
    }
    await expect(page.getByTestId('pin-empty')).toBeVisible();
  });

  test('Deep Scan: datierte Berichte mit Fortschritt statt flacher Befundliste', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/servers/1'); // web01, zwei Demo-Laeufe
    await page.getByRole('button', { name: /^Deep Scan/ }).click();

    // Zwei Laeufe, neueste zuerst - die Datierung IST die Identitaet des
    // Berichts; genau die fehlte in der frueheren Befundliste.
    const toggles = page.getByTestId('deep-scan-report-toggle');
    await expect(toggles).toHaveCount(2);
    await expect(toggles.nth(0)).toContainText('aktuell');

    // Der Fortschritt gegenueber dem Vorlauf steht am Eintrag: eine
    // Haertungsluecke geschlossen, ein Dienst-Befund neu dazugekommen.
    await expect(page.getByTestId('deep-scan-new').first()).toContainText('1');
    await expect(page.getByTestId('deep-scan-resolved').first()).toContainText('2');

    // Aufklappen laedt den Lauf nach: was behoben wurde, steht namentlich da.
    await toggles.nth(0).click();
    const resolved = page.getByTestId('deep-scan-resolved-list');
    await expect(resolved).toContainText('SSH: Passwort-Anmeldung ist erlaubt');
    // Und der neue Befund ist als solcher markiert.
    await expect(page.getByText('Dienst nutzt veraltete Bibliotheken: nginx.service')).toBeVisible();

    // Der aelteste Lauf hat keinen Vorgaenger - dort gibt es nichts zu
    // vergleichen, also auch keine Kennzeichen.
    await expect(toggles.nth(1)).not.toContainText('neu');
  });

  test('Kernel: Proxmox listet installierte Kernel und markiert den laufenden', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/servers/6'); // pve01
    // Der LAUFENDE Kernel steht in der Uebersicht - aus uname -r, nicht aus
    // der Paketliste. Er ist hier bewusst NICHT der neueste installierte.
    await expect(page.getByTestId('kernel-running')).toHaveText('6.8.12-1-pve');
    await expect(page.getByTestId('kernel-reboot-badge')).toBeVisible();

    const table = page.getByTestId('kernel-table');
    await expect(table).toBeVisible();
    await expect(page.getByTestId('kernel-count')).toContainText('4');
    const rows = table.locator('tbody tr');
    await expect(rows).toHaveCount(4);
    // Neueste Fassung zuerst; sie wartet auf den Neustart und ist keine
    // „Rueckfallebene" - dieser Unterschied ist der Punkt der Anzeige.
    await expect(rows.nth(0)).toContainText('6.8.12-4-pve');
    await expect(rows.nth(0)).toContainText('wartet auf Neustart');
    await expect(rows.nth(1)).toContainText('läuft');
    await expect(rows.nth(2)).toContainText('Rückfallebene');
    // Nur der ÄLTESTE ist Ballast: laufender Kernel, neuerer und die
    // nächstältere Rückfallebene bleiben stehen.
    await expect(rows.nth(3)).toContainText('entfernbar');
    await expect(page.getByTestId('kernel-cleanup')).toContainText('(1)');
    // Proxmox-Namensgebung, nicht linux-image.
    await expect(table).toContainText('proxmox-kernel-6.8.12-4-pve');
    await expect(table).toContainText('pve-kernel-6.5.13-5-pve');
  });

  test('Kernel: im LXC-Container nur die Version, keine Liste', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/servers/2'); // db01 laeuft als LXC
    // Die Version bleibt sichtbar …
    await expect(page.getByTestId('kernel-running')).toHaveText('5.15.0-91-generic');
    // … aber sie gehoert dem Host, und das steht auch da.
    await expect(page.getByTestId('kernel-container-note')).toContainText('kommt vom Host');
    // Keine Kernel-Liste und kein Neustart-Befund: Von innen laesst sich am
    // Kernel eines Containers nichts aendern.
    await expect(page.getByTestId('kernel-table')).toHaveCount(0);
    await expect(page.getByTestId('kernel-reboot-badge')).toHaveCount(0);
  });

  test('Paket-Pins sind auf Proxmox ausgenommen', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/servers/6'); // pve01
    await page.getByRole('button', { name: 'Pakete' }).click();
    await page.getByTestId('pin-card-toggle').click();
    await expect(page.getByTestId('pin-unavailable')).toContainText('Proxmox VE');
    // Kein Anlegen, kein Kernel-Knopf, kein Anwenden.
    await expect(page.getByTestId('pin-kernel')).toHaveCount(0);
    await expect(page.getByTestId('pin-add')).toHaveCount(0);
  });

  test('Custom-Aktionen: Seite + Anlege-Popup unter Einstellungen', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/settings/custom-actions');
    await expect(page.getByRole('heading', { name: 'Custom-Aktionen' })).toBeVisible();
    await page.getByRole('button', { name: '+ Custom-Aktion' }).click();
    await expect(page.getByRole('heading', { name: 'Neue Custom-Aktion' })).toBeVisible();
    // Kommando-Liste als Textarea; Speichern erst mit Name + Kommando aktiv.
    await expect(page.locator('#ca-cmds')).toBeVisible();
    const save = page.getByRole('button', { name: 'Erstellen' });
    await expect(save).toBeDisabled();
    await page.locator('#ca-name').fill('e2e-Aktion');
    await page.locator('#ca-cmds').fill('uptime\nwhoami');
    await expect(save).toBeEnabled();
    await page.getByRole('button', { name: 'Abbrechen' }).click();
  });

  test('Repository-Katalog: Seed-Einträge sichtbar + Anlege-Popup', async ({ page }) => {
    await loginAsAdmin(page);
    // Löschen eines Katalog-Eintrags ist mit confirm() abgesichert.
    page.on('dialog', (d) => d.accept());
    await page.goto('/#/settings/repositories');
    await expect(page.getByRole('heading', { name: 'Katalog bekannter Paketquellen' })).toBeVisible();
    // Die mitgelieferten Einträge sind da (Docker CE aus dem Seed).
    await expect(page.locator('table')).toContainText('Docker CE');
    await expect(page.locator('table')).toContainText('download.docker.com');
    // Anlegen: Speichern erst mit Key + Name + deb-Zeile aktiv.
    await page.getByRole('button', { name: '+ Paketquelle' }).click();
    await expect(page.getByRole('heading', { name: 'Neue Paketquelle' })).toBeVisible();
    const save = page.getByRole('button', { name: 'Erstellen' });
    await expect(save).toBeDisabled();
    await page.locator('#kr-name').fill('e2e-Quelle');
    await page.locator('#kr-key').fill('e2e-quelle');
    await page.locator('#kr-line').fill('deb [signed-by=/etc/apt/keyrings/e2e-quelle.asc] https://example.com stable main');
    await expect(save).toBeEnabled();
    await save.click();
    await expect(page.locator('table')).toContainText('e2e-Quelle');
    // Bearbeiten öffnet mit gefüllten Feldern; danach wieder löschen.
    const row = page.locator('tr', { hasText: 'e2e-Quelle' });
    await row.getByRole('button', { name: 'Bearbeiten' }).click();
    await expect(page.locator('#kr-key')).toHaveValue('e2e-quelle');
    await page.getByRole('button', { name: 'Abbrechen' }).click();
    await row.getByRole('button', { name: 'Löschen' }).click();
    await expect(page.locator('table')).not.toContainText('e2e-Quelle');
  });

  test('Regelbausteine: mitgelieferte klonbar, Varianten je Distribution', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/linux-users/profile-blocks');
    const table = page.locator('table');
    // Mitgelieferte Bausteine sind da und schreibgeschützt - anpassen geht
    // ueber das Klonen.
    await expect(table).toContainText('Systemd-Dienst betreiben');
    const apache = page.getByTestId('block-apache-betreiben');
    await expect(apache).toContainText('mitgeliefert');
    // Der Apache-Baustein traegt je Distribution eine eigene Variante.
    await expect(apache).toContainText('apt');
    await expect(apache).toContainText('dnf');
    await expect(apache.getByRole('button', { name: 'Klonen' })).toBeVisible();

    // Der Katalog deckt die verbreiteten Dienste ab, je Dienst mit den beiden
    // Rollen „betreiben" und „verwalten".
    await expect(page.getByTestId('block-nginx-betreiben')).toBeVisible();
    await expect(page.getByTestId('block-nginx-verwalten')).toBeVisible();
    await expect(page.getByTestId('block-adguard-verwalten')).toBeVisible();

    // Suchfeld: Bei diesem Umfang ist Scrollen keine Antwort mehr.
    await page.getByTestId('block-filter').fill('adguard');
    await expect(page.getByTestId('block-nginx-betreiben')).toHaveCount(0);
    await expect(page.getByTestId('block-adguard-betreiben')).toBeVisible();
    await page.getByTestId('block-filter').fill('');

    // Eigener Baustein: ein nacktes systemctl wird schon hier abgewiesen.
    await page.getByRole('button', { name: 'Baustein anlegen' }).click();
    await page.locator('#bl-name').fill('e2e-Baustein');
    await page.locator('#bl-cmds-0').fill('/usr/bin/systemctl');
    await page.getByRole('button', { name: 'Erstellen' }).click();
    await expect(page.locator('.alert-danger')).toContainText('feste argumente');

    // Mit Unteraktion geht es durch.
    await page.locator('#bl-cmds-0').fill('/usr/bin/systemctl restart nginx');
    await page.getByRole('button', { name: 'Erstellen' }).click();
    const row = table.locator('tr', { hasText: 'e2e-Baustein' });
    await expect(row).toBeVisible();

    // Aufraeumen (shared state).
    page.once('dialog', (d) => d.accept());
    await row.getByRole('button', { name: 'Löschen' }).click();
    await expect(table).not.toContainText('e2e-Baustein');
  });

  test('Berechtigungsprofile: eingebaute geschützt, eigenes Profil mit geprüften Regeln', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/linux-users/profiles');
    // Die mitgelieferten Profile bilden den bisherigen Zustand ab und sind
    // schreibgeschützt.
    const table = page.locator('table');
    await expect(table).toContainText('Voll-Administrator');
    await expect(table).toContainText('Standardbenutzer');
    await expect(table.locator('tr', { hasText: 'Voll-Administrator' })).toContainText('geschützt');

    // Eigenes Profil anlegen, mit einem Kommando das nur ohne Argumente
    // gefährlich wäre.
    await page.getByRole('button', { name: 'Profil anlegen' }).click();
    await page.locator('#pf-name').fill('e2e-Webserver');
    await page.getByRole('button', { name: 'Regel hinzufügen' }).first().click();
    const command = page.locator('input[placeholder="/usr/bin/systemctl restart nginx"]');
    await command.fill('/usr/bin/systemctl status nginx');
    await page.getByRole('button', { name: 'Erstellen' }).click();
    const row = table.locator('tr', { hasText: 'e2e-Webserver' });
    await expect(row).toBeVisible();
    await expect(row).toContainText('e2e-webserver');

    // Der Pager-Schutz wurde beim Speichern ergänzt: sonst liefe less als root.
    await row.getByRole('button', { name: 'Bearbeiten' }).click();
    await expect(page.locator('input[placeholder="/usr/bin/systemctl restart nginx"]'))
      .toHaveValue('/usr/bin/systemctl --no-pager status nginx');

    // Ein nacktes systemctl waere Vollzugriff und wird abgewiesen.
    await page.locator('input[placeholder="/usr/bin/systemctl restart nginx"]').fill('/usr/bin/systemctl');
    await page.getByRole('button', { name: 'Speichern' }).click();
    await expect(page.locator('.alert-danger')).toContainText('feste argumente');

    // Aufräumen (shared state).
    await page.getByRole('button', { name: 'Abbrechen' }).click();
    page.once('dialog', (d) => d.accept());
    await row.getByRole('button', { name: 'Löschen' }).click();
    await expect(table).not.toContainText('e2e-Webserver');
  });

  test('APT-Cache: eigene Einstellungsseite mit URL-Feld und Prüf-Button', async ({ page }) => {
    await loginAsAdmin(page);
    // Der APT-Cache hat eine eigene Einstellungsseite (die Repositories-Seite
    // verweist nur noch dorthin).
    await page.goto('/#/settings/apt-cache');
    await expect(page.getByTestId('apt-cache-url')).toBeVisible();
    // Ohne konfigurierte URL: Badge „nicht konfiguriert", Prüf-Button deaktiviert.
    await expect(page.getByTestId('apt-cache-badge')).toContainText('nicht konfiguriert');
    const check = page.getByTestId('apt-cache-recheck');
    await expect(check).toBeDisabled();
    await page.getByTestId('apt-cache-url').fill('http://127.0.0.1:3142');
    await expect(check).toBeEnabled();
    // Speichern validiert serverseitig und prüft direkt - 127.0.0.1:3142 ist
    // im Test nicht belegt, also „nicht erreichbar".
    await page.getByRole('button', { name: 'Speichern' }).click();
    await expect(page.getByTestId('apt-cache-badge')).toContainText('nicht erreichbar');
    // Aufräumen: URL wieder leeren.
    await page.getByTestId('apt-cache-url').fill('');
    await page.getByRole('button', { name: 'Speichern' }).click();
    await expect(page.getByTestId('apt-cache-badge')).toContainText('nicht konfiguriert');
  });

  test('Subscription: Einstellungsseite mit Key-Eingabe, Instanz-ID und ehrlicher Fehlermeldung', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/settings/subscription');
    // Ohne Subscription: Aktivierungs-Karte mit Key-Feld.
    await expect(page.getByTestId('subscription-activate-card')).toBeVisible();
    const keyInput = page.locator('#sub-key');
    await expect(keyInput).toBeVisible();
    // Die Instanz-Kennung existiert unabhängig vom Vertrag (UUID, beim
    // ersten Start erzeugt) und wird angezeigt.
    await expect(page.getByTestId('subscription-instance-card').locator('code')).toContainText(/[0-9a-f]{8}-[0-9a-f]{4}/);
    // Aktivieren-Button erst mit Eingabe aktiv (Doppelklick-/Leer-Schutz).
    await expect(page.getByRole('button', { name: 'Aktivieren' })).toBeDisabled();
    await keyInput.fill('LCM-E-AAAAA-BBBBB-CCCCC-DDDDD');
    await expect(page.getByRole('button', { name: 'Aktivieren' })).toBeEnabled();
    // Erweitert: Dienst-Adresse auf einen toten Port zeigen lassen - die
    // Aktivierung muss EHRLICH scheitern (Fehler-Toast), nichts speichern.
    await page.getByRole('button', { name: /Erweitert/ }).click();
    await page.locator('#sub-url').fill('http://127.0.0.1:1');
    await page.getByRole('button', { name: 'Aktivieren' }).click();
    await expect(page.getByTestId('toast-region').locator('.alert-danger')).toContainText('nicht erreichbar');
    // Weiterhin keine Subscription - die Aktivierungs-Karte bleibt.
    await expect(page.getByTestId('subscription-activate-card')).toBeVisible();
  });

  test('Subscription: Kanalauswahl ohne Vertrag sichtbar, Enterprise gesperrt', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/settings/subscription');
    // Die Kanalauswahl hängt NICHT am Vertrag: Community und Beta sind frei.
    const card = page.getByTestId('subscription-apt-card');
    await expect(card).toBeVisible();
    if (await page.getByTestId('subscription-apt-community').count()) {
      await expect(page.getByTestId('subscription-apt-community')).toBeEnabled();
      await expect(page.getByTestId('subscription-apt-beta')).toBeEnabled();
      // Ohne hinterlegte Subscription bleibt allein der Enterprise-Kanal zu.
      await expect(page.getByTestId('subscription-apt-enterprise')).toBeDisabled();
      await expect(card).toContainText('Subscription');
    } else {
      // Kein selbst-registrierter apt-Host in dieser Umgebung - dann sagt die
      // Karte genau das, statt Schaltflächen anzubieten, die ins Leere gehen.
      await expect(card).toContainText('Nicht verfügbar');
    }
  });

  test('CrowdSec: LAPI-Check, Überwachungs-Empfehlung und Server-Liste', async ({ page }) => {
    await loginAsAdmin(page);
    // Regel-Löschen ist mit confirm() abgesichert - automatisch bestätigen.
    page.on('dialog', (d) => d.accept());
    await page.goto('/#/settings/crowdsec');
    // LAPI konfigurieren (127.0.0.1:1 ist nicht belegt → Check scheitert schnell).
    await page.getByTestId('cs-lapi-url').fill('http://127.0.0.1:1');
    await page.locator('#cs-login').fill('lcm-managed');
    await page.getByTestId('cs-lapi-pw').fill('e2e-lapi-passwort');
    await page.getByTestId('cs-save').click();
    // Nach dem Speichern erscheinen Status-Check, Überwachung und Server-Liste
    // sofort (die Seite lädt die abgeleiteten Flags frisch nach).
    await expect(page.getByTestId('cs-check')).toBeVisible();
    await page.getByTestId('cs-check').click();
    await expect(page.getByTestId('cs-status-badge')).toContainText('nicht erreichbar');
    // Überwachungs-Empfehlung: Regel per Klick anlegen → Status „nur Historie".
    await page.getByTestId('cs-create-rule').click();
    await expect(page.locator('.card', { hasText: 'Überwachung' })).toContainText('nur Historie');
    // Angebundene Server: in der Demo meldet kein Server an diese LAPI.
    await expect(page.getByTestId('cs-none-connected')).toBeVisible();
    // Aufräumen (shared Demo-State): Regel löschen, LAPI-Zugang leeren.
    await page.goto('/#/settings/alerts');
    const rulesTable = page.locator('table').filter({ hasText: 'Schwelle' });
    const rrow = rulesTable.locator('tr', { hasText: 'CrowdSec-LAPI nicht erreichbar' });
    await rrow.getByRole('button', { name: 'Löschen' }).click();
    await expect(rulesTable).not.toContainText('CrowdSec-LAPI nicht erreichbar');
    await page.goto('/#/settings/crowdsec');
    await page.getByTestId('cs-lapi-url').fill('');
    await page.locator('#cs-login').fill('');
    await page.getByTestId('cs-save').click();
    await expect(page.getByTestId('cs-check')).toHaveCount(0);
  });

  test('Servergruppen: Rule-Typ Custom-Aktion mit Auswahl', async ({ page }) => {
    await loginAsAdmin(page);
    const schedRow = await createProduktionSchedule(page, 'e2e-Custom');
    await page.locator('button[title="Neue Rule"]').click();
    // "Custom-Aktion" ist als Typ wählbar.
    await expect(page.locator('#rf-type option', { hasText: 'Custom-Aktion' })).toHaveCount(1);
    await page.locator('#rf-type').selectOption('custom');
    // Ohne angelegte Custom-Aktion erscheint der Hinweis auf die Einstellungen.
    await expect(page.locator('.modal-body')).toContainText('Custom-Aktion');
    // APT-Cache ist als geplanter Regel-Typ wählbar (mit Erklärtext).
    await expect(page.locator('#rf-type option', { hasText: 'APT-Cache anbinden' })).toHaveCount(1);
    await page.locator('#rf-type').selectOption('apt-proxy');
    await expect(page.locator('.modal-body')).toContainText('zentralen APT-Cache');
    // Aufräumen (shared Demo-State).
    await page.getByRole('button', { name: 'Abbrechen' }).click();
    await schedRow.getByRole('button', { name: 'Schedule löschen' }).click();
  });

  test('Mein Konto: über den Usernamen erreichbar, mit Profil und 2FA (QR)', async ({ page }) => {
    await loginAsAdmin(page);
    // „Mein Konto" liegt im Konto-Dropdown (Pille oben rechts).
    await page.getByTestId('current-user').click();
    await page.getByTestId('account-link').click();
    await expect(page.locator('h1')).toContainText('Mein Konto');
    await expect(page.locator('body')).toContainText('Profil');
    await expect(page.locator('body')).toContainText('Passwort ändern');
    // 2FA wird hier eingerichtet (nicht mehr unter Einstellungen).
    await expect(page.locator('body')).toContainText('Zwei-Faktor-Authentifizierung');
    await page.getByRole('button', { name: '2FA einrichten' }).click();
    // QR-Code-Bild (PNG data-URI) erscheint.
    await expect(page.locator('img[alt="2FA QR-Code"]')).toBeVisible();
  });

  test('Dark Mode: Umschalter wechselt System → Dunkel → Hell und merkt sich die Wahl', async ({ page }) => {
    await loginAsAdmin(page);
    const html = page.locator('html');
    // EINE Schaltfläche, die beim Klick durchschaltet: Automatik → Dunkel →
    // Hell → … Default ist Automatik (Playwright emuliert System = hell).
    const toggle = page.getByTestId('theme-toggle');
    await expect(html).toHaveAttribute('data-bs-theme', 'light');
    await toggle.click(); // auto → dark
    await expect(html).toHaveAttribute('data-bs-theme', 'dark');
    // Die Wahl überlebt einen Reload (localStorage).
    await page.reload();
    await expect(html).toHaveAttribute('data-bs-theme', 'dark');
    await page.getByTestId('theme-toggle').click(); // dark → light
    await expect(html).toHaveAttribute('data-bs-theme', 'light');
    await page.getByTestId('theme-toggle').click(); // light → auto (= hell)
    await expect(html).toHaveAttribute('data-bs-theme', 'light');
  });

  test('Sprachumschalter: Flaggen wechseln die Oberfläche zwischen DE und EN', async ({ page }) => {
    await loginAsAdmin(page);
    const html = page.locator('html');
    // Default ist Deutsch.
    await expect(html).toHaveAttribute('lang', 'de');
    await expect(page.getByRole('link', { name: 'Gruppen' })).toBeVisible();
    // EINE Flaggen-Schaltfläche, die beim Klick die Sprache durchschaltet.
    await page.getByTestId('lang-toggle').click(); // de → en
    await expect(html).toHaveAttribute('lang', 'en');
    await expect(page.getByRole('link', { name: 'Groups' })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Settings' })).toBeVisible();
    // Zurück auf Deutsch (Aufräumen für nachfolgende Tests).
    await page.getByTestId('lang-toggle').click(); // en → de
    await expect(html).toHaveAttribute('lang', 'de');
    await expect(page.getByRole('link', { name: 'Gruppen' })).toBeVisible();
  });

  test('Einstellungen: kein „Mein Konto" mehr, /settings leitet weiter', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/settings');
    // Weiterleitung auf die erste sichtbare Unterseite (admin: Benutzer).
    await expect(page).toHaveURL(/#\/settings\/users$/);
    const nav = page.locator('.list-group');
    await expect(nav).not.toContainText('Mein Konto');
    await expect(nav).not.toContainText('Zwei-Faktor');
  });

  test('Footer zeigt Version und Build-Nummer', async ({ page }) => {
    await page.goto('/');
    const version = page.getByTestId('app-version');
    await expect(version).toBeVisible();
    await expect(version).toContainText('Build');
  });

  test('Info-Fenster: Version prüfen fragt den eingestellten Paketkanal ab', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/');

    // Der Server antwortet auf die Sofort-Prüfung mit einer neueren Version
    // aus dem Beta-Kanal - so sieht die Antwort nach einem Kanalwechsel aus.
    await page.route('**/api/v1/system/update-check', async (r) => {
      await r.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          current_version: '1.0.0',
          latest_version: '99.9.9-beta.1',
          update_available: true,
          channel: 'beta',
          checked_at: new Date().toISOString(),
        }),
      });
    });

    await page.getByTestId('imprint-toggle').click();
    const check = page.getByTestId('update-check-now');
    await expect(check).toBeVisible();
    await check.click();

    // Version UND Kanal stehen da - die Zahl allein wäre nicht einzuordnen.
    const dialog = page.getByTestId('imprint-popover');
    await expect(dialog).toContainText('99.9.9-beta.1');
    await expect(dialog).toContainText('Beta-Kanal');
  });

  test('Update-Balken: „Jetzt updaten" wartet auf laufende Jobs', async ({ page }) => {
    // Der Server meldet eine neuere Version und ein mögliches Selbst-Update.
    await page.route('**/api/v1/system/update-info', async (r) => {
      await r.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          current_version: '1.0.0',
          latest_version: '99.9.9',
          update_available: true,
          channel: 'beta',
        }),
      });
    });
    let angefordert = false;
    await page.route('**/api/v1/system/self-update', async (r) => {
      if (r.request().method() === 'POST') angefordert = true;
      await r.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(
          angefordert
            ? {
                supported: true,
                phase: 'waiting',
                target_version: '99.9.9',
                waiting_for: ['Alle Pakete aktualisieren @ web-01'],
              }
            : { supported: true, phase: 'idle' },
        ),
      });
    });

    await loginAsAdmin(page);
    await page.goto('/#/');
    const banner = page.getByTestId('update-banner');
    await expect(banner).toContainText('99.9.9');

    await page.getByTestId('update-now').click();
    // Die Ansage nennt den Job, auf den gewartet wird - „es dauert noch"
    // ohne das Wofür wäre keine Auskunft.
    await expect(page.getByTestId('update-waiting')).toContainText('Alle Pakete aktualisieren @ web-01');
  });

  test('Nach einem Update lädt die Oberfläche sich selbst neu', async ({ page }) => {
    await page.goto('/');
    await expect(page.getByTestId('app-version')).toBeVisible();
    // Kein Banner, solange der Server dieselbe Version meldet.
    await expect(page.getByTestId('reload-banner')).toHaveCount(0);

    // Ab jetzt antwortet der Server mit einer anderen Build-Nummer - genau
    // das, was nach einem Update passiert.
    const route = '**/api/v1/system/info';
    await page.route(route, async (r) => {
      const res = await r.fetch();
      const body = await res.json();
      body.build = String(Number(body.build || 0) + 1);
      await r.fulfill({ response: res, json: body });
    });
    // Rückkehr zum Tab auslösen, statt den Minutentakt abzuwarten.
    await page.evaluate(() => document.dispatchEvent(new Event('visibilitychange')));

    await expect(page.getByTestId('reload-banner')).toBeVisible();
    // Abfangen wieder abschalten: Sonst fände die neu geladene Seite erneut
    // eine „neue" Version vor und liefe im Kreis.
    await page.unroute(route);
  });

  test('Job-Historie: Filter, Inline-Output und Health-Toggle', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/jobs');
    // Filter-Dropdowns sind mit distinkten Werten befüllt.
    await expect(page.locator('#f-type option', { hasText: /^update$/ })).toHaveCount(1);
    await expect(page.locator('#f-by option', { hasText: 'scheduler' })).toHaveCount(1);
    // Zähler über der Tabelle vorhanden (Seitennavigation erst ab Seite 2).
    await expect(page.locator('body')).toContainText('Job(s)');
    // Output öffnet inline unter der angeklickten Zeile.
    await page.locator('table tbody tr').first().getByRole('button', { name: 'Output' }).click();
    await expect(page.locator('table tbody pre')).toBeVisible();
    // Health standardmäßig ausgeblendet; Toggle blendet ihn ein.
    const toggle = page.locator('#hide-health-jobs');
    await expect(toggle).toBeChecked();
    await expect(page.locator('table tbody')).not.toContainText('Health-Check');
    await toggle.uncheck();
    await expect(page.locator('table tbody')).toContainText('Health-Check');
  });

  test('API-Key mit Scope anlegen', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/settings/apikeys');
    await page.fill('input[placeholder*="ci-pipeline"]', 'e2e-monitor');
    await page.click('label[for="scope-r"]');
    await page.click('button[type="submit"]');
    // Klartext-Key wird einmalig angezeigt, Tabelle zeigt Scope-Badge.
    await expect(page.locator('.alert-warning code')).toContainText('lcm_');
    await expect(page.locator('table')).toContainText('nur lesen');
  });

  // Die E2E-Tests laufen gegen EINE Instanz - ein Schalter, den ein Test
  // umlegt, gilt auch im nächsten. Deshalb setzt jeder Test den Zustand, den
  // er braucht, ausdrücklich selbst.
  async function setzeFruehwarnung(page, { ein, lokal = false }) {
    await page.goto('/#/settings/security');
    await page.locator('form #adv-en').waitFor();
    await page.getByTestId('advisory-enable').setChecked(ein);
    await page.getByTestId('advisory-local').setChecked(lokal);
    await page.getByRole('button', { name: 'Speichern' }).first().click();
    await expect(page.locator('.alert-success')).toBeVisible();
  }

  test('Docker: Reiter fuer Images, Container und Compose', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/docker');
    await expect(page.getByTestId('docker-images-table')).toBeVisible();
    // Die Server stehen jetzt mit Namen da, nicht nur als Zahl.
    await expect(page.getByTestId('docker-images-table')).toContainText('web01');

    await page.getByTestId('tab-containers').click();
    await expect(page.getByTestId('docker-containers-table')).toContainText('webshop-web-1');
    // Standardmaessig nur laufende - danach hat Tony gefragt.
    await expect(page.locator('#only-running')).toBeChecked();

    await page.getByTestId('tab-compose').click();
    await expect(page.getByTestId('docker-compose-table')).toContainText('webshop');
    await expect(page.getByTestId('docker-compose-table')).toContainText('2 Dienst(e)');
  });

  test('Docker: viele Server werden gekuerzt und lassen sich aufklappen', async ({ page }) => {
    await loginAsAdmin(page);
    // Die Demo-Daten haben je Image nur einen Server - die Kuerzung laesst
    // sich nur mit einer gestellten Antwort pruefen.
    await page.route('**/api/v1/docker/overview', async (route) => {
      await route.fulfill({
        json: [
          {
            repository: 'nginx',
            tag: '1.25',
            server_count: 5,
            servers: [
              { id: 1, name: 'alpha' },
              { id: 2, name: 'beta' },
              { id: 3, name: 'gamma' },
              { id: 4, name: 'delta' },
              { id: 5, name: 'epsilon' },
            ],
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
            servers: [
              { id: 1, name: 'alpha' },
              { id: 2, name: 'beta' },
            ],
            in_use_count: 2,
            update_available: false,
            unverifiable: false,
            local_only: false,
            critical_vulns: 0,
            high_vulns: 0,
          },
        ],
      });
    });
    await page.goto('/#/docker');

    // Bis drei Server: alle im Klartext, keine Schaltflaeche.
    const redisRow = page.locator('tbody tr', { hasText: 'redis:7' });
    await expect(redisRow).toContainText('alpha');
    await expect(redisRow).toContainText('beta');
    await expect(redisRow.getByTestId('server-links-more')).toHaveCount(0);

    // Mehr als drei: die ersten zwei plus eine Schaltflaeche.
    const nginxRow = page.locator('tbody tr', { hasText: 'nginx:1.25' });
    await expect(nginxRow).toContainText('alpha');
    await expect(nginxRow).toContainText('beta');
    await expect(nginxRow).not.toContainText('gamma');
    const more = nginxRow.getByTestId('server-links-more');
    await expect(more).toContainText('+3');

    // Aufgeklappt stehen alle fuenf da - und sind Links, kein Text.
    await more.click();
    const popover = nginxRow.getByTestId('server-links-popover');
    await expect(popover).toBeVisible();
    await expect(popover.getByRole('link')).toHaveCount(5);
    await expect(popover).toContainText('epsilon');
    // Und sie fuehren auf die Server-Seite.
    await popover.getByRole('link', { name: 'gamma' }).click();
    await expect(page).toHaveURL(/#\/servers\/3$/);
  });

  test('Sicherheit: Zwischenspeicher-Reiter zeigt beide Trefferquoten', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/security');
    await page.getByTestId('tab-caches').click();

    // Beide Karten stehen nebeneinander - sie messen Verschiedenes und
    // duerfen nicht zu einer Zahl verschmelzen.
    await expect(page.getByTestId('cache-scan')).toBeVisible();
    await expect(page.getByTestId('cache-advisory')).toBeVisible();

    // Ohne Messwerte steht ein Strich, NICHT „0 %" - null Zugriffe sind
    // keine schlechte Quote, sondern gar keine.
    await expect(page.getByTestId('cache-advisory-rate')).toContainText('-');
    await expect(page.locator('body')).toContainText('Noch keine Messwerte');
  });

  test('Sicherheit: der Hinweis verlinkt auf eine Seite, die es gibt', async ({ page }) => {
    await loginAsAdmin(page);
    await setzeFruehwarnung(page, { ein: false });
    await page.goto('/#/security');
    await page.getByTestId('tab-advisories').click();
    // Der Link zeigte auf eine Route, die es nicht gab - Klick landete im 404.
    await page.getByRole('link', { name: /Einstellungen einschalten/ }).click();
    await expect(page).toHaveURL(/#\/settings\/security$/);
    await expect(page.locator('body')).not.toContainText('404');
    // Beide Blöcke liegen jetzt dort: CVE-Scan und Frühwarnung.
    await expect(page.getByTestId('cve-enable')).toBeVisible();
    await expect(page.getByTestId('advisory-enable')).toBeVisible();
  });

  test('Fruehwarnung: Zustand, Filter und Durchgang auf Knopfdruck', async ({ page }) => {
    await loginAsAdmin(page);
    await setzeFruehwarnung(page, { ein: true });
    // Der Zustand steht direkt bei den Einstellungen - ohne ihn wüsste
    // niemand, ob und wann zuletzt geprüft wurde.
    await expect(page.getByTestId('advisory-status')).toContainText('eingeschaltet');

    await page.goto('/#/security');
    await page.getByTestId('tab-advisories').click();
    await expect(page.getByTestId('advisories-disabled')).toHaveCount(0);
    // Ohne diesen Knopf passiert nach dem Einschalten bis zu 15 Minuten
    // sichtbar nichts.
    await expect(page.getByTestId('advisory-poll-now')).toBeEnabled();
    await expect(page.getByTestId('advisories-last-poll')).toContainText('Noch nie geprüft');
    await expect(page.getByTestId('advisory-severity-filter')).toBeVisible();
  });

  test('Sicherheit: Fruehwarnung ist ein eigener Reiter und sagt, wenn sie aus ist', async ({ page }) => {
    await loginAsAdmin(page);
    await setzeFruehwarnung(page, { ein: false });
    await page.goto('/#/security');
    // Standardansicht bleibt der CVE-Scan.
    await expect(page.getByTestId('tab-cve')).toHaveClass(/active/);

    await page.getByTestId('tab-advisories').click();
    await expect(page.getByTestId('advisories-table')).toBeVisible();
    // Ohne eingeschaltete Fruehwarnung MUSS das dastehen: Eine leere Liste
    // waere sonst nicht von „alles sauber" zu unterscheiden.
    await expect(page.getByTestId('advisories-disabled')).toBeVisible();
    await expect(page.locator('body')).toContainText('nicht nachgesehen');
  });

  test('Einstellungen: das Formular „Allgemein" ist speicherbar (kein blockierendes Feld)', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/settings/general');
    await page.locator('form #sttl').waitFor();
    // Ein einziges ungueltiges Feld blockiert den Submit des GESAMTEN
    // Formulars - der Speichern-Knopf reagiert dann scheinbar gar nicht,
    // ohne Fehlermeldung. Genau so war die Seite mit der Voreinstellung
    // session_ttl_minutes=0 gegen min="5" unbenutzbar.
    const invalid = await page.evaluate(() => {
      const form = document.querySelector('form');
      return [...form.elements]
        .filter((el) => el.willValidate && !el.checkValidity())
        .map((el) => `${el.id || el.name}: ${el.validationMessage}`);
    });
    expect(invalid).toEqual([]);
  });

  test('Einstellungen: Fruehwarnung einschalten und Cache-Gueltigkeit setzen', async ({ page }) => {
    await loginAsAdmin(page);
    await page.goto('/#/settings/security');
    // Die Preisgabe des Paketbestands steht im Klartext ueber dem Schalter.
    await expect(page.locator('body')).toContainText('api.osv.dev');

    await page.getByTestId('advisory-enable').check();
    // 5 Minuten liegen unter dem Poll-Takt von 15 - ein solcher Eintrag waere
    // beim naechsten Durchgang laengst abgelaufen und wuerde nur geschrieben,
    // nie gelesen. Der Wert wird deshalb auf den Takt angehoben.
    await page.getByTestId('advisory-ttl').fill('5');
    await page.getByRole('button', { name: 'Speichern' }).first().click();

    await page.reload();
    await expect(page.getByTestId('advisory-enable')).toBeChecked();
    await expect(page.getByTestId('advisory-ttl')).toHaveValue('15');

    // Der Hinweis auf der Sicherheitsseite verschwindet damit.
    await page.goto('/#/security');
    await page.getByTestId('tab-advisories').click();
    await expect(page.getByTestId('advisories-disabled')).toHaveCount(0);
  });

  test('Fruehwarnung: lokale Kopie sagt, dass noch nichts gespiegelt wurde', async ({ page }) => {
    await loginAsAdmin(page);
    await setzeFruehwarnung(page, { ein: true, lokal: true });
    await page.reload();
    await expect(page.getByTestId('advisory-local')).toBeChecked();

    await page.goto('/#/security');
    await page.getByTestId('tab-advisories').click();
    // Ohne Spiegellauf wurde NICHTS geprüft - die Ansicht muss das sagen,
    // sonst sähe die leere Liste wie ein sauberes Ergebnis aus.
    await expect(page.getByTestId('advisories-local')).toBeVisible();
    await expect(page.locator('body')).toContainText('noch nie gespiegelt');
  });

  // Deep-Links: das Routing ist hash-basiert, das Backend liefert aber für
  // jeden Pfad die index.html. Ein PFAD-Aufruf (Bookmark, geteilter Link,
  // Reload einer kopierten URL ohne #) muss beim App-Start in die Hash-Route
  // übersetzt werden - vorher landete man immer auf dem Dashboard (BUG:
  // /security zeigte „Dashboard").
  test.describe('Deep-Links per Pfad-URL', () => {
    test('/security rendert die Sicherheitsseite', async ({ page }) => {
      await loginAsAdmin(page);
      await page.goto('/security');
      await expect(page.locator('h1')).toContainText('Sicherheit');
      await expect(page).toHaveURL(/#\/security$/);
    });

    test('/servers/1 rendert die Server-Detailseite', async ({ page }) => {
      await loginAsAdmin(page);
      await page.goto('/servers/1');
      await expect(page.locator('h1')).toContainText('web01');
      await expect(page).toHaveURL(/#\/servers\/1$/);
    });

    test('/settings/alerts rendert die Alarm-Einstellungen', async ({ page }) => {
      await loginAsAdmin(page);
      await page.goto('/settings/alerts');
      await expect(page.locator('h1')).toContainText('Einstellungen');
      await expect(page.locator('h2')).toContainText('Alarme');
      await expect(page).toHaveURL(/#\/settings\/alerts$/);
    });

    test('Trailing Slash wird toleriert (/docker/)', async ({ page }) => {
      await loginAsAdmin(page);
      await page.goto('/docker/');
      await expect(page.locator('h1')).toContainText('Docker');
      await expect(page).toHaveURL(/#\/docker$/);
    });

    test('vorhandene Hash-Route gewinnt gegen den Pfad', async ({ page }) => {
      await loginAsAdmin(page);
      await page.goto('/security#/jobs');
      await expect(page.locator('h1')).toContainText('Job-Historie');
      await expect(page).toHaveURL(/\/#\/jobs$/);
    });
  });
});
