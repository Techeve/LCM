# Parität übrige Seiten (alt → neu)

Stand 04.11.2026, Branch `feat/frontend-neubau`. Nacharbeit auf `feat/b-shell2`: alle 22 Lücken geschlossen, Stand je Punkt unter **Status** (G3 ohne Tastenabzeichen an Knöpfen, Begründung dort). Verglichen: `frontend/src/**` (Svelte) gegen `ui/src/**` (Lit). Serverdetail und Beitritts-Assistent sind nicht Teil dieses Vergleichs.
Methode: Ich bin jede alte Seite und jede Komponente, die sie einbindet, Abschnitt für Abschnitt durchgegangen. Je Funktion habe ich den Backend-Aufruf im Neubau gesucht (auch zusammengesetzte URLs wie `${ADVISORIES_URL}/poll`) und die Felder bzw. Spalten im neuen Code nachgeprüft. Zeilenangaben beziehen sich auf den alten Code.

## Shell und globale Funktionen (`App.svelte`, `Navbar.svelte`, `lib/hotkeys.svelte.js` → `ui/src/shell/**`)

- **G1 Update-Hinweis und Selbst-Update fehlen ganz.** Alt: `App.svelte:219-330` (Logik) und `:372-450` (Balken). Der Balken meldete „Version X verfügbar (installiert Y)“, verlinkte das Release und war je Version wegklickbar. Er bot „Jetzt installieren“ an (`POST /system/self-update`) und zeigte den Fortschritt: Warten auf laufende Jobs, Sicherung läuft, Installation läuft, Fehler, Name der Sicherungsdatei. Wenn das Selbst-Update nicht möglich war, stand der Grund dabei. Neu: Kein Code ruft `/system/update-info` oder `/system/self-update` auf. Vorhanden sind nur die Fehlertexte `selfupdate.*` in `locales/en.json:1913`. Das Backend bietet die Endpunkte weiter an (`ops_controller.go:616-652`). Folge: LCM lässt sich nicht mehr aus der Oberfläche aktualisieren, und niemand erfährt von einer neuen Version.
  - **Status:** erledigt. Balken unter dem Hauptmenü (`shell/update-banner.ts`): neue Version mit Link, je Version wegklickbar, „Jetzt installieren“ mit Bestätigung (Sicherung vorher, Ausfall), Fortschritt Warten auf Jobs / Sicherung / Installation samt Sicherungsdatei, Fehler und Grund, warum es nicht geht. Fortschritt über das Ereignis `system.build` (Thema `system`), kein Abfragen im Takt.
- **G2 Fußzeile mit Info-Fenster fehlt.**
  - **G2a Version:** Version, Build, Commit, Plattform und das Abzeichen „Dirty-Build“ (`App.svelte:470-495`). Neu wird die Version nirgends angezeigt. `GET /system/info` liest nur `api/provisioning.ts:159`, und zwar nur für `agent_port`.
    - **Status:** erledigt. Info-Fenster „Über LCM“ (`shell/about-dialog.ts`, im Kontomenü mit Version als Hinweis): Version, Build, Commit mit Abzeichen „Dirty-Build“, Plattform, Build-Datum.
  - **G2b Impressum:** Betreiber, Inhaber, Anschrift, Mail und Web, dazu das Logo (`App.svelte:146-156`, `:497-571`). Es war auch auf der Anmeldeseite und ohne Anmeldung sichtbar. Neu: Die Angaben fehlen ganz, eine Suche nach „Talweg“ oder „info@techeve“ in `ui/` findet nichts.
    - **Status:** erledigt. Impressum mit Logo im Info-Fenster; ohne Anmeldung über das Info-Symbol neben Sprache und Theme (Anmeldung, Aktivierung, Doku). Ohne Anmeldung nur Version und Build, wie das Backend es liefert.
  - **G2c Updates von Hand prüfen:** Knopf „Jetzt prüfen“ (`POST /system/update-check`, `App.svelte:238-262`) mit Anzeige von neuester Version, Kanal und Prüffehler. Neu: kein Aufruf. Die Fundstelle `update-check` in `schedule-model.ts` gehört zu einem Systemzeitplan, nicht zu diesem Knopf.
    - **Status:** erledigt. „Jetzt prüfen“ im Info-Fenster (`settings:manage`) mit neuester Version, Kanal, Prüfzeit und Prüffehler; das Ergebnis erscheint danach im Balken, auch „aktuell“.
  - **G2d Kürzel-Hilfe:** Link „Tastenkürzel ?“ in der Fußzeile (`:488-496`). Neu im Kontomenü. Kein echter Verlust.
    - **Status:** unverändert im Kontomenü und in der Hilfe; kein Verlust.
- **G3 Seitenkürzel mit einer Taste fehlen.** Alt: `data-hotkey` mit `n` = Neu, `/` = Suchfeld, `u` = Sammel-Update (`hotkeys.svelte.js:83-96`, `:249`). Verwendet in `Dashboard.svelte:161,206`, `Jobs.svelte:199`, `Security.svelte:230` und neun Einstellungs- bzw. Benutzerseiten. Dazu kamen Tastenabzeichen an den Knöpfen und ein Schalter dafür in der Hilfe (`HotkeyHelp.svelte:110-112`). Neu: nur `g`+Buchstabe, `?`, Strg/Cmd+K und Strg/Cmd+S (`lib/hotkeys.ts`). Die Seitenbefehle liegen jetzt in der Befehlspalette (`palette.register`). Strg+S funktioniert auf den Formularseiten (`lcm-save`).
  - **Status:** erledigt bis auf Abzeichen an Knöpfen. Seitenbefehle der Palette tragen `key`: `n` (Server hinzufügen auf Dashboard und Serverliste, Neu-Befehle der Einstellungs-, Gruppen- und Linux-Benutzer-Seiten), `u` (Sammel-Update), `/` setzt den Fokus in das Suchfeld der Seite. Die Taste steht in der Palette rechts und in der Hilfe unter „Auf dieser Seite“. Tastenabzeichen an den Knöpfen samt Schalter entfallen: Sie bräuchten Änderungen an `lcm-button` und widersprechen der ruhigen Gestaltung; Palette und Hilfe zeigen die Tasten.
- **G4 Pfeiltasten wirken nur noch eingeschränkt.** Alt: Pfeiltasten in Menüleisten und von Zelle zu Zelle in Tabellen (`hotkeys.svelte.js:102-150`). Neu: nur Zeile hoch/runter in `lcm-data-table.ts:477` und links/rechts in Reitern (`lcm-tabs.ts:96`).
  - **Status:** erledigt (`lib/focus-nav.ts`). Pfeil links/rechts wandert in Menüleisten (Hauptmenü) und in Tabellenzeilen von der Zeile zu deren Bedienelementen und zurück, auch durch Shadow DOMs. Reiter, Menüs, Listen und Auswahlfelder behalten ihre eigene Bedienung. Reine Textzellen bekommen keinen Fokus, das wäre nur Lärm für Screenreader.
- **G5 Name im Kontomenü fehlt.** Alt stand dort „Angemeldet als <Name>“ (`Navbar.svelte:201`). Neu zeigt das Konto-Symbol nur die Initialen (`nav-model.ts initialsOf`).
  - **Status:** erledigt. Kontomenü oben „Angemeldet als <Name>“ (führt zu Mein Konto), das Konto-Symbol heißt für Screenreader „Konto von <Name>“.
- **G6 Self-XSS-Warnung in der Browser-Konsole fehlt** (`main.js:9-11`, `lib/consoleWarning.js`). Neu gibt es keine. Das Risiko ist kleiner, weil die Sitzung nicht im localStorage liegt: Standard ist der Arbeitsspeicher des Tabs (`lib/api.ts`, Modulvariable), alternativ ein httpOnly-Cookie.
  - **Status:** erledigt (`lib/console-warning.ts`, im Produktionsbau beim Start, in der Sprache der Oberfläche).
- **G7 Neu laden nach einem Update funktioniert anders.** Alt: Abfrage von `/system/info` alle 60 s und Banner „wird neu geladen“ (`App.svelte:179-215`, `:359-370`). Neu: Der Service Worker meldet die neue Version (`sw.ts`, `lib/version.ts`). Ein Hinweis erscheint nur, wenn noch Ungespeichertes offen ist. Gleichwertig, nur ohne Ansage im Normalfall.
  - **Status:** erledigt. Vor dem Neuladen steht drei Sekunden „LCM wurde aktualisiert. Die Seite lädt gleich neu.“ mit „Jetzt neu laden“, auch auf der Anmeldeseite (`lib/version.ts`). In der Arbeitsspeicher-Variante meldet dieses Neuladen ab; wer angemeldet war, meldet sich neu an und kommt per `next` zur selben Adresse zurück.
- Toasts: vollständig (`lcm-toaster.ts`, Fehler bleiben stehen).
- Eine Serverliste in der Navbar gab es alt nicht. Neu sucht die Befehlspalette auch nach Servern (ein Zugewinn).

## Dashboard (`Dashboard.svelte` → `pages/dashboard/**`)

Das alte Dashboard war die Serverliste. Seine Lücken stehen unter „Serverliste“. Das neue Dashboard (Kacheln, Handlungsbedarf, Jobs, anstehende Läufe, Alarme) geht darüber hinaus.
- **D1 Knopf „Server hinzufügen“ fehlt auf dem Dashboard** (`Dashboard.svelte:160-162`). Neu gibt es ihn nur auf `/servers` (`servers-page.ts` Kopf) und im Leerzustand der Liste.
  - **Status:** erledigt. Knopf im Kopf des Dashboards, dazu Taste `n`.

## Serverliste (`ui/src/pages/servers/servers-list.ts`, verglichen mit dem alten Dashboard)

Alle Felder unten lädt `normaliseServer` bereits (`api/servers.ts`). Es fehlt nur die Anzeige.
- **S1 Kennzeichen „Konsole offen“ mit Namen** (`Dashboard.svelte:109,267-270`, `GET /servers/consoles`). Neu nur im Detail (`server-detail.ts:231,367`). In der Liste sieht man nicht, auf welchem Server gerade jemand eine Konsole offen hat.
  - **Status:** erledigt. Kleines Konsolen-Kennzeichen hinter dem Namen, wer sie offen hat steht in der Blase (`GET /servers/consoles`, neu geladen mit dem Thema `servers`).
- **S2 Kennzeichen „Wartung“** (`:290-293`). Neu nur im Detail (`server-detail.ts:342`). Die Wartung erklärt, warum ein Server nicht antwortet.
  - **Status:** erledigt. Wartungs-Kennzeichen hinter dem Namen, vor „offline“, Erklärung in der Blase.
- **S3 Kennzeichen „Offline“ und gedämpfte Zeile** (`:142-150`, `:264-266`, `:294-299`). Offline hieß: erst ab zwei Fehlversuchen in Folge, mit Tooltip zur Anzahl. Ein tolerierter Ausfall wurde ausgegraut. Neu: In der Liste fehlen beide Angaben, `reachable` und `failed_checks` werden nicht angezeigt.
  - **Status:** erledigt. Offline-Kennzeichen ab zwei Fehlversuchen mit Anzahl und letztem Fehler in der Blase; tolerierter Ausfall dämpft den Namen.
- **S4 Port und Agent-Kennzeichen in der Host-Spalte** (`:273-277`). Alt stand dort `host:ssh_port`, bei Agent-Servern das Abzeichen „Agent“. Neu nur `host` (`servers-list.ts:189`). Den Port und die Verbindungsart sieht man nicht mehr.
  - **Status:** erledigt. Host-Spalte `host:port` (IPv6 in Klammern), Agent-Server mit Kennzeichen und Verbindungsstand in der Blase.
- **S5 Befunde an der Ampel** (`StatusBadge.svelte`, `Dashboard.svelte:286`). Ein Klick auf das ⓘ zeigte alle Gründe für Gelb oder Rot. Neu: `lcm-status` ohne Befunde (`servers-list.ts:197-201`), obwohl `insights` mitgeladen wird. Das Dashboard nennt nur einen Befund je roten oder gelben Server.
  - **Status:** erledigt. Alle Befunde in der Blase an der Ampel, auch in der schmalen Liste neben dem Detail.
- **S6 Proxmox-Produkt und Gerätelogo** (`:16-26`, `:281-282`, `OsLogo.svelte:46-51`). Alt: „Proxmox VE 8.2“ samt Proxmox-Logo statt „Debian 12“. Synology und MikroTik wurden am Modell erkannt. Neu nur `os_name os_version` und das Logo zu `os_id` (`servers-list.ts:205-208`, `os-logos.ts` ohne Proxmox).
  - **Status:** erledigt (`pages/servers/server-os.ts`). „Proxmox VE 8.2“ mit Proxmox-Logo, MikroTik-Logo für RouterOS, Synology für DSM, Raspberry Pi am Gerätemodell.
- **S7 Kernel-Filter vergleicht jetzt Teilstrings.** Alt Präfix je Versionsteil: „6“ traf 6.x, aber nicht 16.x oder 7.1.6 (`:38-51`). Neu Teilstring im Backend (`internal/core/services/server_list.go:191`): „6“ trifft auch 16.x und 5.10.0-26.
  - **Status:** erledigt im Backend: `kernelMatches` in `services/server_list.go` vergleicht Stelle für Stelle („6“ trifft 6.x, nicht 16.x oder 5.10.0-26; eine letzte Stelle mit Text wie „6.1.0-13“ als Textpräfix).
- **S8 OS- und Host-Filter.** Den OS-Filter gab es als Auswahl aus den vorkommenden Werten (`:54-56`, `:213-218`), jetzt ist er Freitext. Den eigenen Host-Filter (`:209-210`) ersetzt die gemeinsame Suche über Name und Host. Inhaltlich abgedeckt.
  - **Status:** erledigt. OS-Filter ist eine Auswahl aus den vorkommenden Systemen (`os_names` neu in `GET /servers`); der Host-Filter bleibt in der gemeinsamen Suche.
- **S9 Trefferzahl.** Die Zahl der gefilterten Server stand immer da (`:249`). Neu nur ab mehr als 100 Treffern, im Blätterbereich (`servers-list.ts:254`).
  - **Status:** erledigt. „12 Server“ steht immer unter der Liste, ab mehr als 100 der Bereich mit Blättern.
- Sammelaktionen: Die alte Liste hatte keine, es fehlt also nichts. Neu dazugekommen: Gruppenfilter, Sortierung, Spaltenwahl, Spalte „Updates“, Status „Handlungsbedarf“.

## Jobs (`Jobs.svelte` → `pages/jobs/**`)

- **J1 Filter „Ausgelöst von“ fehlt** (`Jobs.svelte:19-20`, `:58`, `:201-207`). Das Backend wertet `?triggered_by=` weiter aus (`job_controller.go:49,70`, gleicher Handler für `/jobs` und `/jobs/history`). `api/jobs.ts:146-152` liest die Optionen sogar ein. Aber `jobs-filters.ts` bietet kein Feld dafür, und `toJobsQuery` (`jobs-state.ts`) schickt den Parameter nicht. Die Spalte ist vorhanden, gefiltert werden kann nicht.
  - **Status:** erledigt. Auswahl „Ausgelöst von“ aus `/jobs/filter-options`, in der Adresse als `by`, an die API als `triggered_by`.
- Sonst vollständig: Status, Art, Suche, Zeitraum, Health-Schalter, Abbrechen, Ausgabe mit Exit-Code, SSH-Sitzungen und Live-Aktualisierung.

## Gruppen (`Groups.svelte` → `pages/groups/**`)

vollständig. Geprüft: Priorität, ACL-Abdeckung, Manager, alle 20 Regeltypen, die vier Durchsetzungstypen, Firewall-Editor mit Allowlists, APT-Cache-Auswahl, Docker-Umfang, DNS-Domains, Streuung, Reihenfolge, System-Gruppen und -Zeitpläne sowie Ausführen, An/Aus, Bearbeiten und Löschen.

## Sicherheit (`Security.svelte`, `AdvisoryList.svelte`, `CacheStats.svelte` → `pages/security/**`)

vollständig. Abgedeckt sind: CVE-Liste mit Quelle, Image und Link, Zusammenfassung, Sammel-Update mit Fortschritt und letztem Fehler, Scanner-Stand mit Sandbox und DB-Aktualisierung, Hinweise mit Abfrage (`/advisories/poll`), Quittieren, Aktualisieren, Version einfrieren, Malware- und Exploited-Kennzeichen, lokaler Kopie und „Abfrage aus“ sowie die Cache-Statistik. Einzige Ausnahme ist das Kürzel `u`, siehe G3.

## Container (`Docker.svelte` → `pages/containers/**`, nur die Liste)

vollständig: Images mit Status, CVEs, Verwendung und Servern, Instanzen mit „nur laufende“, Compose-Projekte und die Zählabzeichen im Kopf.

## Linux-Benutzer (`LinuxUsers.svelte`, `linuxusers/*` → `pages/linux-users/**`)

vollständig. Das alte sudo-Häkchen beim Anlegen ersetzt das Standardprofil, das im Backend ohnehin Vorrang hat. Weiter abgedeckt: Schlüssel hinzufügen und erzeugen samt Download und Anleitung, Aktivierungslink und -mail, Gruppenzuordnung mit Profil je Gruppe, ACL-Warnung, Entfernen von allen Servern mit Bericht, Löschen. Bei Profilen und Bausteinen: Kopieren, Verwendung, Vorschau, Suche, eingebaute Einträge.

## Mein Konto (`Account.svelte` → `pages/account/**`)

vollständig (Profil mit Passwortabfrage bei geänderter Mail, Passwort mit 2FA-Code, 2FA einrichten und abschalten). Neu dazu: Einstellungen.

## Doku (`Docs.svelte` → `pages/docs/**`)

vollständig (Seitenliste, Sprache, öffentlich, „Anmelden“-Knopf, neu mit Inhaltsverzeichnis).

## Öffentliche Seiten (`Login`, `Activate`, `LinuxActivate`, `NotFound` → `pages/login-page.ts`, `pages/public/**`, `pages/not-found-page.ts`)

vollständig (2FA-Schritt, Passwort vergessen, Erststart-Hinweis, Zugang aktivieren und Passwort zurücksetzen, Linux-Aktivierung mit Passwort sowie Schlüssel einfügen oder erzeugen samt Download und Anleitung, 404). Die alten Adressen leiten weiter (`routes.ts`).

## Einstellungen (`settings/*.svelte` → `pages/settings/**`)

Alle 19 Abschnitte existieren mit denselben Rechten wie im alten `settingsNavItems.js`. `MovedToLinuxUsers` wird zu einer Weiterleitung in `routes.ts`, `SettingsHome` zur Weiterleitung auf den ersten Abschnitt.
- Allgemein: vollständig. Die Felder für Pflicht-2FA und Sitzungsdauer stehen jetzt unter Sicherheit.
- Abo: vollständig (aktivieren, prüfen, entfernen, APT-Kanal, alle Statusfelder).
- Backups: vollständig (Zeitplan, Empfängerschlüssel, sofort sichern, Download, Löschen, Wiederherstellen auch per Upload, automatischer Neustart).
- Zeit: vollständig.
- DNS: vollständig.
- Benutzer: vollständig (anlegen, bearbeiten, Rollen, Passwort, Einladung mit Mail, sperren, löschen).
- API-Schlüssel: vollständig. Das Präfix steht jetzt in der Namensspalte, neu kommen Ablauf und Besitzer dazu.
- MCP: vollständig. Dass nur lokale Adressen erlaubt sind, prüft das Backend schon heute (`settings_service.go:77`).
- Sicherheit: vollständig (CVE-Scan mit Cron, Gewichtung, Phased Updates, Hinweise, lokale Kopie, Spiegeln, Cache-TTL).
- Repositories: vollständig.
- APT-Cache: vollständig (Caches, Status, Statistik, Neustart, Dauer-Cache, Proxy trennen, Überwachungsregel).
- **Apps: kleine Lücke.**
  - **E2 Schnellschalter fehlt.** Aktivieren und Deaktivieren gingen direkt in der Liste (`Apps.svelte:98-102`, `:163-165`). Neu nur im Bearbeiten-Dialog (`apps.ts:255-261`).
    - **Status:** erledigt. Kleiner Schalter vorn in der Zeile (eine eigene Spalte passt neben der Erklärspalte nicht); Abschalten sofort, Einschalten mit Bestätigung, weil dann Kommandos als root laufen.
- CrowdSec: vollständig (LAPIs, Prüfung, Console-Schlüssel, angebundene Server mit Modus und Dienst, Überwachungsregel).
- Allowlists: vollständig.
- **Zeitpläne: eine Lücke.**
  - **E1 Spalte „Regeln“ fehlt** (`Schedules.svelte:55`, `:73`). Sie listete die Regeltypen je Zeitplan. Neu nur als Anzahl im Bestätigungsdialog (`automation/schedule-model.ts:79`). In der Übersicht sieht man nicht mehr, was ein Lauf tut. Die Spalte „Herkunft“ steht jetzt als Unterzeile beim Namen.
    - **Status:** erledigt. Spalte „Regeln“ mit der Zahl der aktiven Regeln, jede Regel mit Typ in der Blase; System-Zeitpläne als „Systemaufgabe“. Rhythmus und nächster Lauf stehen dafür in einer Spalte, der Cron-Ausdruck im Titel.
- Eigene Aktionen: vollständig.
- Benachrichtigungen: vollständig (vier Kanaltypen, Test).
- Alarme: vollständig (alle 13 Typen, Jetzt auswerten, Verlauf mit Zustellfehler, Hinweis auf fehlenden Kanal).
- Ereignisse: vollständig (Stufe, Suche, Live-Stream, befristetes Debug).

---

## Priorisierte Liste

Alle 22 Punkte sind erledigt (Stand je Punkt unter **Status** oben); bei G3 entfallen nur die Tastenabzeichen an Knöpfen.

### (1) Funktionen, die man gar nicht mehr erreicht (4)
1. **G1** LCM selbst aktualisieren und der Hinweis „neue Version verfügbar“, `GET/POST /system/self-update`, `GET /system/update-info` (`App.svelte:219-330`, `:372-450`).
2. **G2c** Updates von Hand prüfen, `POST /system/update-check` (`App.svelte:238-262`, `:557-568`).
3. **G2b** Impressum und Info-Fenster, auch ohne Anmeldung (`App.svelte:146-156`, `:497-571`).
4. **J1** Jobs nach „Ausgelöst von“ filtern (`Jobs.svelte:201-207`). Backend und Optionsabruf sind schon da, im Neubau fehlen nur das Feld und der Query-Parameter.

### (2) Fehlende Informationen (8)
1. **G2a** LCM-Version, Build, Commit, Plattform und Dirty-Build-Abzeichen (`App.svelte:470-487`).
2. **S5** Befunde an der Ampel in der Serverliste (`StatusBadge.svelte`, `Dashboard.svelte:286`).
3. **S3** Offline-Kennzeichen (ab zwei Fehlversuchen) und gedämpfte Zeile bei toleriertem Ausfall (`Dashboard.svelte:142-150`, `:264`, `:294-299`).
4. **S2** Wartungskennzeichen in der Liste (`Dashboard.svelte:290-293`).
5. **S1** „Konsole offen“ mit Namen in der Liste (`Dashboard.svelte:267-270`).
6. **S4** SSH-Port und Agent-Kennzeichen in der Host-Spalte (`Dashboard.svelte:273-277`).
7. **S6** Proxmox-Produkt und Version sowie Proxmox-Logo in der Liste (`Dashboard.svelte:16-26`, `:281-282`).
8. **E1** Regeltypen je Zeitplan in der Übersicht „Zeitpläne“ (`Schedules.svelte:55`, `:73`).

### (3) Kleinere Abweichungen (10)
1. **G3** Ein-Tasten-Kürzel (`n`, `/`, `u`), Tastenabzeichen und deren Schalter. Ersatz ist die Befehlspalette.
2. **G4** Pfeiltasten in Menüleisten und von Zelle zu Zelle in Tabellen.
3. **G5** „Angemeldet als <Name>“ im Kontomenü.
4. **G6** Self-XSS-Warnung in der Konsole.
5. **G7** Ansage „wird neu geladen“ nach einem Update (Mechanismus gleichwertig).
6. **D1** „Server hinzufügen“ auf dem Dashboard (gibt es auf `/servers`).
7. **S7** Kernel-Filter: Teilstring statt Präfix je Versionsteil (Backend `server_list.go:191`).
8. **S8** OS-Filter als Freitext statt Auswahl, eigener Host-Filter in die Suche aufgegangen.
9. **S9** Trefferzahl der Serverliste nur bei mehr als 100 Treffern.
10. **E2** Apps in der Liste direkt an- und abschalten.
