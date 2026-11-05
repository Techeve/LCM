# Release B: Backend-Probleme aus dem Frontend-Neubau

Stand: 01.11.2026 · laufend ergänzt

Was beim Neubau des Frontends im Backend auffällt und **nicht** sofort
behoben wurde. Kleinigkeiten (Format, Struktur, umständlich angesprochene
Endpunkte) werden direkt im Neubau korrigiert und stehen hier nicht.

## Offen

- **`terminal_enabled` ohne Oberfläche.** Der Schalter steckt in Datenbank und
  Service, aber weder im Formular noch im Update-Endpunkt. Die Konsole lässt sich
  nur je Server abschalten. Entscheiden: Schalter anbieten oder entfernen.
- **`GET /servers/:id/outdated-packages`** nutzte nur die alte Oberfläche. Bleibt
  als dokumentierte REST-API, kann aber weg, wenn niemand sie braucht.
- **Eigene TLS-Felder** (`TLSCertPEM`, `TLSKeyPEMEnc`, `TLSCustomUsed`) sind nie
  verdrahtet worden, nur eine Spalte in `rotate.go`.
- **Nur von Tests benutzt:** `BuildKernelInfo`, `Server.IsOffline`,
  `JobService.QueuedForServer`, `LinuxUserService.RemoveFromAllServers`,
  `ProvisioningService.DrainServer`, `Codes`, `GroupService.List`,
  `PackageService.Vulnerabilities`. Liste aus `deadcode`, im Aufräumen von
  Release B bewusst nicht angefasst.

- **Sitzungen überleben keinen Neustart.** Browser-Sitzungen liegen im
  Speicher (`services/auth_session.go`), wie der Signaturschlüssel der
  2FA-Challenge.
  Nach jedem Selbst-Update muss man sich neu anmelden. Entscheidung offen:
  Sitzungstabelle in der DB, damit ein Update niemanden abmeldet.
- **Cookie-Variante nur über HTTPS.** Hinter einem TLS-Proxy ohne
  `trust_proxy_header` und `trusted_proxies` erkennt LCM HTTPS nicht; dort lässt
  sich „cookie“ nicht einstellen. Die Doku zur Proxy-Einrichtung muss das sagen.
- **Eigener Passwortwechsel beendet die eigene Sitzung** (bisheriges
  Verhalten). Die neue Oberfläche führt danach zur Anmeldung.
- **Freitext aus Services als `detail`.** Diese Fehler haben zwar einen Code,
  tragen aber deutschen Freitext im Parameter `detail`. Die Services sollten
  stattdessen typisierte Fehler bzw. Sentinels mit Parametern liefern:
  - `ops_controller.go`: `TriggerSystem` (`schedule.trigger_failed`), `SetMCP`
    und `ErrSettingInvalid` (`settings.invalid_input`), Selbst-Update
    (`selfupdate.rejected`), Testmail inkl. `ErrMailer*` mit
    unterschiedlichem Status (`settings.mail_test_failed`).
  - `*.invalid_input` für Known-Repo, IP-Allowlist, CrowdSec-LAPI, APT-Cache.
  - `subscription_controller.go`: Aktivierung, Service-URL, Kanalwechsel (Text
    des Abo-Dienstes wird durchgereicht).
  - `profile_block_controller.go`: Vorschau eines Regelbausteins
    (`block.preview_failed`), Variantenfehler.
  - `notification_controller.go`: Testversand.
  - Fehler vom Zielsystem (`server.action_failed`, `server.probe_failed`,
    Provisioning): prüfen, ob SSH-Fehler Details der LCM-Seite enthalten.
- `mapServerError` macht aus jedem `repositories.ErrNotFound` ein
  `server.not_found`, auch bei fehlenden Jobs.
- **Erfolgsantworten mit deutschem Text** (z. B. `respondStaged` in
  `ops_controller.go`) und deutsche MCP-Tool-Ergebnisse. Sie sollten wie die
  Fehler Code und Parameter bekommen.
- **Jobs ohne Ereignis:** Onboarding-Jobs werden direkt ins Repository
  geschrieben (`server_service.go:609`, `:763`, `dsm_service.go:265`,
  `agent_service.go:161`, `routeros_service.go:173`) und umgehen den
  JobService, also auch Ereignisse und Statuscache.
- `job_service.go` `abort()`: ohne Laufzeit-Handle wird `onFinished` nicht
  aufgerufen, der Statuscache bleibt stehen.
- `terminal.go` `OpenConsoles` lädt alle Server, um nach Sichtbereich zu
  filtern; `VisibleServerIDs` reicht.
- Die Konsolen-Anzeige im Serverdetail nutzt `server.status` als „auf dem
  Server hat sich etwas geändert“. Ein eigenes Ereignis wäre klarer.
- Dienst-Suite `services` unter `-race`: zwei Tests schwanken
  (`TestPaketlisteMitEinerKaputtenQuelleGiltAlsErfolg/ad_hoc`,
  `TestPaketlisteMitExit0UndAllenQuellenKaputtIstFehlschlag`), auch ohne
  diese Änderungen.
- Befehlsvorschau: `services` (welche Dienste neu starten) fehlt, weil LCM
  vor einem Lauf keine needrestart-Daten hat. Die Liste der bei `autoremove`
  entfernten Pakete steht erst zur Laufzeit fest.
- **Kein `GET /jobs/:id`:** Ein Direktlink auf einen Job außerhalb der
  geladenen Liste bekommt nur Name, Status und Exit-Code aus `ssh-output`.
- **Job-Ausgabe erst am Ende:** `job.Output` wird erst beim Abschluss
  geschrieben (`job_service.go:555`). Die Live-Ansicht baut sich deshalb aus
  den Befehlen der SSH-Sitzung.
- `GET /servers` liefert je Eintrag alle Serverfelder, auch Hostschlüssel-
  Fingerprint und Zugangsdetails. Die Liste sollte nur die Spalten liefern,
  die eine Übersicht braucht.
- Einladungs- und Aktivierungsmails liefern deutschen `err.Error()` als
  `mail_error` (`provisioning_controller.go:58`, `linux_user_controller.go:156`).
- `bulk.progress` meldet globale Zähler auch an Benutzer mit kleinerem
  Sichtbereich (`bulk_update.go` `publishBulk`).
- Dashboard `recent_jobs` blendet nur Health-Checks aus; `alert-check` füllt
  die Liste.
- **Deutsche Freitexte in Daten:** Alarm-`description` und manche Insight-
  Parameter (z. B. „Verbindung abgelehnt (Host offline)“) sind deutscher
  Freitext und nicht übersetzbar. Sie sollten Code und Parameter bekommen.
- **Gruppenliste zu schwer, zu wenig Zahlen:** `group_repository.go:44`
  `FindAll` lädt alle Server vollständig mit, liefert aber keine Anzahl von
  Verwaltern und Zeitplänen. Die Seite holt deshalb Detail und Zeitpläne je
  Gruppe nach. Besser: `server_count`, `manager_count`, `schedule_count` in
  der Liste und für die Konfliktanzeige nur die Server-IDs.
- **Zeitzone der Zeitpläne unbekannt:** Cron läuft in der Zeitzone des
  LCM-Hosts (`scheduler.go:158`), die API nennt sie nicht. Die Oberfläche kann
  deshalb keine verlässlichen nächsten Termine anzeigen.
- Zeitpläne und Regeln einer Gruppe brauchen `rules:manage`, auch zum Lesen.
- Seed-Regeln haben oft `position` 0; die Reihenfolge ist dann unbestimmt.
- `GET /servers/:id` (`server_controller.go:381`) liefert weder `status`,
  `insights` noch `updates`; die Oberfläche holt den Server deshalb über
  `GET /servers?ids=<id>`. Detail und Liste sollten dieselben Felder haben.
- `POST /servers/:id/vulnerabilities/scan` und `/deep-scan`
  (`ops_controller.go:452`, `:467`) antworten ohne `job_id`; der gestartete
  Job lässt sich nicht verlinken.
- `privilege_profile_service.go:169`: Löschen eines Profils prüft nicht, ob
  Benutzer es noch als Standardprofil oder in Gruppen-Zuordnungen nutzen; die
  Verweise zeigen danach ins Leere.
- `profile_block_service.go:219`: Die Vorschau eines Regelbausteins enthält
  das feste deutsche Wort „Verzeichnisrecht“.
- Die feste Passwort-Richtlinie liefert kein Endpunkt; die Oberfläche spiegelt
  sie in `pages/account/password-policy.ts` und muss bei einer Änderung
  nachgezogen werden.
- MCP-Fehlerdetails nennen kein Feld (`settings_service.go:78`, `:81`).
- **Hostschlüssel ohne Bestätigung (sicherheitsrelevant, Entscheidung offen):**
  Ein leeres `confirmed_fingerprint` überspringt die Prüfung
  (`server_service.go:556`, `:724`, `routeros_service.go:89`,
  `dsm_service.go:86`), jeder Hostschlüssel wird beim ersten Kontakt
  vertraut. Die Oberfläche schickt immer einen bestätigten Fingerprint; über
  API und MCP geht es ohne.
- `activation_service.go:177`: Passwort-Reset-Mails nutzen denselben
  Aktivierungslink (`/#/aktivierung`); die Seite kann keinen reset-eigenen
  Text zeigen.
- `appdocs/markdown.go` `slugify`: gleiche Überschriften bekommen dieselbe
  ID (die Oberfläche hängt `-2`, `-3` an).
- `system/schedules/overview` verlangt `settings:manage`, die Seite
  Zeitpläne ist aber für `rules:manage` sichtbar (`router.go:648`).
- Namen der System-Zeitpläne sind feste deutsche Texte (`scheduler.go:439-466`).
- `allowlist.in_use` verliert die Namen der nutzenden Regeln
  (`ops_controller.go:563`); `notification.invalid_config` den Grund
  (`errors_notifications.go:35`).
- Demo-Mailkanal nutzt `to` statt `recipients` (`storage/seed_demo.go:538`).
- Keine Befehlsvorschau für APT-Cache-Neustart, dauerhaftes Caching,
  APT-Proxy trennen und das Auslösen von Zeitplänen; die Erklärblasen zeigen
  dort nur festen Text.
- Serverliste lässt sich nicht nach APT-Proxy oder CrowdSec filtern; die
  Seiten laden `/servers?limit=1000`.
- **WebSocket-Ablehnung vor dem Handshake** (401 per HTTP) sieht der Browser
  nur als Close-Code 1006, nie als 4401. Der Client fragt deshalb bei
  abnormalem Abbruch `/auth/me`, um abgelaufene Sitzungen zu erkennen.
- **CSRF-Fehler ohne Selbstheilung:** Nach 403 `auth.csrf_invalid` holt der
  Client `/auth/me` neu (frisches Token) und wiederholt einmal.
- `docs/en/static/openapi.yaml` hinkt `docs/static/openapi.yaml` hinterher
  (neue Route `PATCH /auth/me/preferences` nur in der deutschen Fassung).

- **Ereigniskanal im Pflichtschritt gesperrt.** `/api/v1/events` fehlt in
  `remediationAllowedExact` (`middlewares/remediation.go`): Ein Benutzer mit
  `must_change_password` oder `must_setup_2fa` bekommt beim Verbindungsaufbau
  403. Der Client verbindet in dieser Zeit gar nicht erst; das Backend sollte
  den Kanal im Pflichtschritt bewusst erlauben oder bewusst weiter sperren.
- **`config.dev.json` mit `demo_mode: true` wirkt nicht.** `DemoMode` ist
  `json:"-"` (`config.go`), Demo-Daten und das Aufheben des Passwortwechsels
  gibt es nur mit `--demo`; der Vite-Proxy braucht außerdem `--dev` (sonst
  spricht das Backend nur HTTPS). Entweder den Schlüssel aus der Datei nehmen
  oder in `Makefile`/README `--dev --demo` nennen.
- **Härtungsvorschläge ohne Verbindung: 500 ohne Aussage.**
  `GET /servers/:id/harden-suggestions` (`path_hardening.go:273`) gibt den
  Verbindungsfehler roh weiter; die Oberfläche kann nur „Suche
  fehlgeschlagen“ sagen. Besser ein Code wie `server.unreachable` (auch für
  andere Lesezugriffe per SSH). Im Demo-Modus antwortet die Simulation hier
  nicht, Demo-Server zeigen deshalb immer den Fehler.
- **Demo-Jobs ändern keinen Zustand.** Installationen (fail2ban, CrowdSec,
  Sperren/Entsperren von Benutzern) laufen als Job durch, der Server bleibt
  aber unverändert. Für Vorführungen wäre ein simulierter Zustandswechsel
  hilfreich.

## Direkt korrigiert (zur Nachvollziehbarkeit)

- API-Fehler haben jetzt `code` und `params`, Text in `error` englisch
  (`internal/api/apierr`).
- **Listen mit Filter, Sortierung, Blättern im Backend:** `GET /servers`,
  `/jobs` (Alias `/jobs/history`), `/alert-events` und `/server-groups`
  antworten mit `{items, total}`; `GET /servers` kennt `q`, `name`, `status`,
  `incapable`, `os`, `kernel`, `group_id`, `ids`, `sort`, `limit`, `offset`.
- **ETag/If-None-Match** für alle JSON-GET-Antworten unter `/api/v1`
  (`middlewares/etag.go`): unverändert kostet nur ein 304.
- **Mandantentrennung geschlossen:** `GET /alert-events` filtert nach dem
  Sichtbereich (Ereignisse ohne Server nur für Admins), `GET
  /security/update-all` zeigt Namen und Zähler nur Admins und dem, dessen
  Sichtbereich der Lauf betrifft; `/system/self-update` verlangt
  `settings:manage` (Test ergänzt).
- **Nie zugeordnete Sentinels** haben jetzt 4xx-Codes (`block.invalid_run_as`,
  `block.invalid_path_line`, `security_tool.not_installed`,
  `security_tool.invalid_unban_ip`, `backup.invalid_recipient`,
  `settings.mailer_disabled`, `settings.mailer_no_recipients`); die Testmail
  unterscheidet fehlende Konfiguration vom Versandfehler.
- `Reboot` (und seine Befehlsvorschau) lehnt RouterOS/DSM ab.
- **Rechteprofile verloren beim Speichern ihre Regelbausteine** (auch im
  ausgelieferten Stand auf `develop`): Der Request kannte `block_uses` nicht.
  Behoben in `privilege_profile_controller.go`, Test ergänzt. Kandidat für
  einen Hotfix der laufenden Version.
- Zugriffsprotokoll schrieb API-Fehler (401, 404 …) als 500 ins Log;
  `apierr.StatusOf` liefert jetzt denselben Status wie der ErrorHandler.
- Ungültiger `cve_scan_cron` antwortete mit 500; jetzt Eingabefehler.
- Router-Tests: `buildTestApp` setzt `Deps.CustomActions` und `Deps.Alerts`.
- **Serverdetail (LCM-Host):** `GET /servers` und `GET /servers/:id` liefern
  `is_lcm_host` aus `Server.IsLcmHost` (Test `TestServerListeMeldetLcmHost`);
  die Oberfläche baut die Regel nicht mehr nach. `GET /servers/:id/lcm-host/status`
  liefert den Scanner-Stand (`scanner`) mit, die Karte braucht keinen zweiten
  Aufruf. Die Demo meldet für ihren LCM-Host (jetzt `lcm-host`) einen
  eingerichteten Trivy statt des Zustands des Demo-Rechners; simulierte
  Skript-Jobs (Einrichtungen) spielten vorher nebenbei alle ausstehenden
  Paket-Updates ein (`demoPackageJobOutput`, Tests in
  `lcm_host_container_test.go`).
- **Serverliste (Parität):** Der Kernel-Filter vergleicht wieder Stelle für
  Stelle wie im alten Dashboard („6" trifft 6.x, nicht 16.x oder 5.10.0-26),
  vorher Teilstring (`kernelMatches` in `services/server_list.go`).
  `GET /servers` liefert zusätzlich `os_names`, die im Sichtbereich
  vorkommenden Betriebssysteme vor den Textfiltern, für die Auswahl des
  OS-Filters (Tests `TestServerListeFiltertNachText`,
  `TestServerListeNenntVorkommendeBetriebssysteme`).
- **APT-Proxy auf RouterOS/DSM:** `planAptProxy` (`aptproxy.go`) wies API-Geräte
  nicht ab wie alle anderen Skript-Aktionen; jetzt `ensureNotRouterOS`, Test in
  `TestPreviewSSHAndFirewallRefusedLikeRun`.

## Eingabeprüfung (Zweig `feat/b-valid`, Details in `doc/eingabepruefung.md`)

- **Doppelte Namen** von Gruppen, Eigenen Aktionen und Benachrichtigungskanälen
  endeten als `500 Internal server error` (nur der Unique-Index fing sie).
  Jetzt Vorabprüfung mit 409 (`group.name_taken`, `custom_action.name_taken`,
  `notification.name_taken`) und ein Auffangnetz in `jsonErrorHandler`
  (`conflict.duplicate`) für jede übrige Unique-Verletzung. Tests in
  `group_service_test.go`, `custom_action_test.go`, `unique_violation_test.go`.
- **Einstellungen, Bausteine, Alarmregeln, Anmeldung, E-Mail-Kanal** hatten keine
  Längen-, Zeichen- oder Bereichsprüfung (Details im Dokument oben). Neu:
  `checkGlobalSettings`, `checkBlockRequest`, `checkAlertRequest`, `checkAppRequest`,
  `checkRoleNames`, `checkNames`, dazu `checkCount`, `checkRange`, `checkEmailList`.
  Kandidat für einen Hotfix auf `develop`: SMTP-Kopfzeilen (`buildMessage`) und die
  Obergrenze für `expires_in_days`/`ttl_hours`.
- **Firewall:** Bemerkung wurde still auf 120 *Byte* gekürzt (Mehrbyte-Zeichen
  zerrissen); Regel-, Quellen- und Allowlist-Zahl unbegrenzt. Jetzt abgelehnt
  (`firewallMaxRules` 500, `firewallMaxSources` 100, Bemerkung 120 Zeichen).
- **Offen:** Bestehende `mail_from`-Werte mit Anzeigenamen lassen sich erst nach
  Korrektur wieder speichern (sie waren schon vorher nicht versandfähig).
  Datenbank-Spaltenlängen gibt es nicht (SQLite); bei PostgreSQL nachziehen.

## Funktionstest auf lcm-test (04.11.2026, Build 9018)

Neue Oberfläche gegen echte Server (Debian 13, Ubuntu 26.04, Rocky 10, openSUSE
Leap 16; SSH und Agent). Behoben sind unter anderem: sshd-Absturz bei
Socket-Aktivierung (`195428a`), sudo einschränken unter sudo-rs (`3b87060`),
Paketnamen in Großschreibung (`137576d`), fail2ban ohne Paket (`2ad2516`),
Portwechsel im eingeschränkten Modus (`bd93b62`).

**Nachgezogen am 05.11.2026 (Build 9021, auf den echten Systemen geprüft):**
- Die Update-Prüfung meldet Fehler: `update_check_error` am Server, Hinweis
  `updateCheckFailed`, Paketansicht „Update-Stand unsicher“. dnf endet dabei mit
  Exit 1 ohne Liste. zypper überspringt die Quelle mit Exit 0, deshalb prüft der
  Scan zusätzlich dessen Ausgabe. Hat zypper eine kaputte Quelle noch im Cache,
  meldet es als Nicht-root-Benutzer nur „out-of-date“; das fällt erst beim
  nächsten Update-Job auf.
- „LCM-Zugang auch löschen“ läuft im eingeschränkten Modus über den neuen
  Helper-Befehl `self-remove`, der Exit-Code zählt. Ein Server mit älterem Helper
  scheitert daran mit einer Meldung, statt Erfolg vorzutäuschen. Bleibt das Konto
  bestehen, bleibt auch der Server in LCM.
- fail2ban richtet EPEL ein, wenn das Paket fehlt. Beim Entfernen von fail2ban
  bleibt EPEL eingerichtet.
- Adressen von Container-Netzen fehlen in `ip_addresses`. Gefiltert wird nach
  Schnittstelle, echte Netze in 172.16.0.0/12 bleiben. Das wirkt auch in den
  Serverdaten und bei `firstNonLoopbackIP`.
- Ungespeicherte Änderungen: Alles im Register `lib/unsaved.ts` führt zur Rückfrage,
  wenn ein Benutzer die Seite wechselt (Link, Menü, Zurück/Vor), auch Fenster und
  Assistenten. Code, der nach dem Speichern weiterleitet, fragt nicht. Der Browser
  warnt beim Neuladen.

**Entschieden:** Browser-Sitzungen bleiben im RAM, jedes Update meldet alle ab
(Tony, 05.11.2026).

**Offen, niedrig:**
- `cve_scan_error` bleibt bis zum nächsten Scan stehen. Nach der
  Trivy-Einrichtung meldeten Server darum weiter „CVE-Bewertung nicht möglich“.
- Ausgabe im Job wächst nur je Kommando, Einzelskripte zeigen bis zum Ende
  „Noch keine Ausgabe“.
- Nach „Server entfernen“ und „Gruppe auflösen“ kommen einige 404, weil noch
  einmal geladen wird, bevor die Seite wechselt.
- `/security` und Paketliste: Die rechten Spalten passen bei 1440 px nicht in die
  Breite.
- openSUSE Leap ohne Lebenszyklus. „DNS-Server unbekannt“ bis zum ersten DNS-Test.
  `dns_current` zeigt sortiert statt in der echten Reihenfolge.
- Der Agent-Assistent zeigt `apt install` auch für Nicht-apt-Systeme.
- Beim Abbrechen läuft der Prozess auf dem Ziel weiter; der Dialog sagt das.
- Ubuntu 26.04 im Lebenszyklus: 2031-05 ist aus fünf Jahren LTS abgeleitet, nicht
  nachgeschlagen.

**Nicht geprüft:** Terminal auf Rocky und SUSE. CrowdSec, SSH-2FA, Härtung über
den Helper. Repository hinzufügen, APT-Cache, Pins, Installation einer bestimmten
Version. Einzelne Images aktualisieren oder aufräumen. Grundsatz-Regeln, Firewall
per Gruppe, „Alle Server aktualisieren“. Mails, Benachrichtigungen, Alarme,
Backups und MCP (Seiten nur geladen, nicht bedient). Agent-Token erneuern.
RouterOS, DSM, Aktivierungs- und Passwort-Links. Service Worker bei geänderten
UI-Dateien.

## E2E-Übertragung (05.11.2026)

Die Playwright-Suite des alten Frontends (`frontend/e2e/`) läuft jetzt als
TypeScript unter `ui/e2e/` gegen das echte Binary (`--dev --demo`) mit der neuen
Oberfläche. Ausführen: `cd ui && npx vite build && touch dist/.gitkeep && npx
playwright test`. Beim Übertragen aufgefallen:

**Behoben (mit Test in der Suite):**
- Karte „Betriebsfähigkeit“ ließ `last_error_params` weg („ein anderer Prozess
  hält die Paketverwaltung fest“ statt „durch unattended-upgr (PID 1234)“) und
  verlinkte den Job auch ohne `jobs:read`.
- Das Jobende wurde nicht gemeldet, wenn der Job vor dem Abonnement fertig war
  (Demo, schnelle Befehle): kein „… abgeschlossen“-Toast. `watchJob` fragt den
  Stand jetzt einmal nach.
- Löschen einer Allowlist und einer CrowdSec-LAPI sowie „Testen“ eines
  Benachrichtigungskanals taten nichts: Die Zeile trägt die ID als Text, die
  Suche verglich sie mit der Zahl.
- Codeblöcke in der Doku (`<pre>`) waren nicht per Tastatur scrollbar
  (`tabindex="0"`).
- **Passwortprüfung:** `password.too_weak` trägt die Codes der verletzten
  Regeln als `params.problems`; die Fehlermeldung nennt sie ausgeschrieben.
  Beim Anlegen und Zurücksetzen eines Kontos zeigt das Feld Stärke und Gründe
  nach der Serverregel (Standard-Passwort, Benutzername …), „Stark“ gibt es
  für solche Passwörter nicht mehr.
- **Frühwarnung mit lokaler Kopie ohne Spiegel:** Die Seite sagt „noch nie
  gespiegelt“ statt „ausgeschaltet“ und „nicht nachgesehen“.
- **Toast gegen Speicherleiste:** Der Toaster hebt seinen Stapel über eine
  sichtbare Speicherleiste; der Umweg im E2E-Test entfällt.
- **Suche im Reiter Pakete** filtert beim Tippen.
- **Containerseite:** „+2 weitere“ klappt alle Server auf (Maus und Tastatur).
- **Rechte einschränken:** Die Bestätigung nennt wieder, was danach erlaubt
  und gesperrt ist, wogegen der Modus nicht schützt und dass es nicht per
  Klick rückgängig zu machen ist (BUG-030).
- **Betriebsfähigkeit:** Die Karte zeigt immer eine Zeile je Bereich;
  veralteter Paketstand, die Warnung einer Paketquelle und die Job-Ausgabe
  (mit `jobs:read`) stehen wieder in der Karte.
- **`jobs:read`:** Der Reiter „Jobs“ am Server und „Job ansehen“ in den
  Meldungen erscheinen nur mit dem Recht (auch die Sprünge zur Jobliste).
- **Hauptmenü:** Pos1 und Ende springen zum ersten und letzten Eintrag der
  Leiste (Logo, Kontomenü; im alten Frontend war „Doku“ der letzte).
- **Seitenwarnung** „Alarmregeln ohne Kanal benachrichtigen niemanden“ stand
  schon auf der Seite; Test ergänzt, Singular korrigiert.
- **Mausauswahl in `lcm-combobox`:** Die Liste blieb nach dem ersten Scrollen
  (Fokus unter der Falz) geschlossen und öffnete nie wieder; sie folgt jetzt
  dem Feld, weicht nach oben aus, und ein E2E-Test wählt mit der Maus.
- **Barrierefreiheit (axe, WCAG 2.1 AA):** Keine Ausnahmen mehr. Suchen-Knopf
  und Kontomenü nennen Kürzel und Initialen im Namen, Erklärspalten und
  oberste Karten beginnen mit Ebene 2, gefüllte Knöpfe nutzen `--accent-solid`
  (dunkel 4,9 : 1; `--accent` bleibt Text- und Linienfarbe).
- **Job-Art-Filter** am Verlauf: `harden-ssh`, `join`, `reconnect`,
  `acl-setup` und `perm-sync` fehlten in `jobTypes`; ein Test gleicht die
  Liste mit den Arten der Oberfläche ab.

**Nicht übertragen (mit Grund):**
- `i18n.spec.js` (2 Tests): `npm run check:locales` und die Unit-Tests prüfen
  Schlüssel, Platzhalter und fest eingetragenen Text vollständiger.
- Chart-Hover (Tooltip mit Prozentwert, Punkt unter der Maus): Die Funktion gibt
  es so nicht mehr (siehe oben).
- „Pakete-Tab zeigt CVE-Badges am Paket, Klick springt in den Reiter
  Sicherheit“: Das Paket trägt jetzt „Sicherheit“ als Art, einen Sprung gibt es
  nicht. Der Test prüft Art und Fundliste getrennt.
- Neue Version nach Update: Der Service Worker selbst lässt sich im Test nicht
  tauschen; der Test sendet die Meldung `new-version` über den BroadcastChannel,
  die er verschickt.

## Nachtrag 05.11.2026: Browser-Anmeldung in zwei Varianten

Entscheidung des Inhabers: Die Anmeldung im Browser ist einstellbar
(`browser_session_mode`, Standard „memory“), der Bearer-Login ist entfallen.

- **Erledigt:** `token` in der Login-Antwort, `authenticateBearer`, `credBearer`
  im Ereigniskanal, `IssueToken` und `ValidateToken` sind entfernt; `Login` im
  Service gibt es nicht mehr (`VerifyPassword` + `CompleteLogin`). Die Sitzung
  trägt ihren Transport (`cookie`/`header`), `Authenticate` prüft `X-API-Key`,
  `X-LCM-Session`, Cookie. Cookies heißen `__Host-lcm_session` und
  `__Host-lcm_csrf`, immer `Secure`.
- **Ereigniskanal mit Fahrkarte:** `POST /api/v1/events/ticket`; der Speicher der
  Fahrkarten (`services/tickets.go`) ist mit der Konsole gemeinsam. Der
  Zugriffslog schwärzt `ticket`. Die Konsole holte ihre Fahrkarte schon über
  die authentifizierte Route und funktioniert mit `X-LCM-Session` unverändert.
- **Bleibt:** Das JWT (`golang-jwt`) trägt weiter die 2FA-Challenge und damit
  `newSigningKey`. `access_token_ttl_minutes` heißt weiter so, ist aber die
  Leerlauf-Frist der Sitzung (Vorgabe, wenn die Einstellung „Sitzungsdauer“ 0
  ist); eine bestehende `config.json` muss weiter laden.
- **Offen:** Der Wechsel des Sitzungswegs beendet alle Sitzungen, auch die des
  Betreibers, ohne Vorwarnung außer dem Hinweis in der Karte. Der Seed-Text der
  Rolle „Manage LCM“ (`storage/seed_blocks.go`) spricht noch vom
  „JWT-Geheimnis“ in der `config.json`; das stimmt seit der Bindung des
  Schlüssels an den Prozessstart nicht mehr, ist aber Datenbestand und wurde
  hier nicht angefasst.
- **Eigener Passwortwechsel:** beendet weiter die eigene Sitzung (in beiden
  Wegen); die Oberfläche meldet danach ab und führt zur Anmeldung, es braucht
  kein neues Token für den Tab.
