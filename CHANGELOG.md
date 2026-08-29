# Changelog

## v1.34.0 - 2026-08-29

### 🚀 Features

- **groups**: verwaltungs-user in der oberflaeche zuweisen (8e3f02c7)

### 🐛 Bugfixes

- **demo**: demo-manager der gruppe produktion zuweisen (7eac41ef)

### 🔧 Sonstiges

- **readme**: logo im kopfbereich, social-preview fuers repo (fc66f638)

## v1.33.0 - 2026-08-27

### 🚀 Features

- **demo**: oeffentlicher demo-modus --demo-public mit echter simulation (2b684222)

### ♻️ Refactoring

- **demo**: login-integration der demo-zugaenge wieder entfernt (add4417f)

### 🔧 Sonstiges

- **readme**: englisches README als Standard, deutsche Fassung daneben (d422fc6b)

## v1.32.0 - 2026-08-26

### 🚀 Features

- **release**: Betas gehen im Community-Finale auf (3a686e2f)
- **release**: Release-Pakete auch am Spiegel und auf GitHub (a6c81b0f)

### 🐛 Bugfixes

- **ci**: Mirror-Anstoß in release:public ist verzeihend (28d3e4ba)
- **ci**: release:public läuft in busybox-sh, nicht bash (87c0d2ca)
- **release**: Release-Vorbereitungs-Commits sind keine Changelog-Einträge (37a44c32)

### 🔧 Sonstiges

- **docker**: READMEs für die Docker-Hub-Repos, HTTPS-Korrektur (d9b32f6b)
- Repo-Links auf den öffentlichen Spiegel, GitHub ergänzt, SSH-2FA dokumentiert (6491262b)
- v1.30.8 - Version & Changelog vorbereitet (0120aa3f)

## v1.30.8 - 2026-08-26

### 🔧 Sonstiges

- **images**: Releases zusätzlich nach Docker Hub veröffentlichen (bea08b2d)
- **install**: Docker Hub als empfohlener Container-Weg (DE und EN) (f3e4ed3e)
- **mirror**: Pipelines nur im privaten Repo, GitHub-Issues in den Briefkasten (6263f024)
- v1.30.8-beta.1 - Version & Changelog vorbereitet (e2e54143)

## v1.30.8-beta.1 - 2026-08-26

### 🔧 Sonstiges

- **mirror**: Pipelines nur im privaten Repo, GitHub-Issues in den Briefkasten (6263f024)

## v1.30.7-beta.1 - 2026-08-25

### 🐛 Bugfixes

- **ci**: Gegenprobe des Enterprise-Deploys kann grün werden (ae878744)
- **release**: develop leitet keine finale Version mehr ab (21768050)

### 🔧 Sonstiges

- **mirror**: öffentlicher Spiegel als Schnappschuss statt Commit-Graph (a6a07dc2)

## v1.30.6 - 2026-08-25

### 🔧 Sonstiges

- v1.12.1 — Community-Stand in den Enterprise-Kanal übernehmen (cc5c2a58)
- v1.12.3 — Community-Stand in den Enterprise-Kanal übernehmen (544fdd70)
- v1.12.5 — Community-Stand in den Enterprise-Kanal übernehmen (4eb37ee4)
- v1.16.1 — Version & Changelog vorbereitet (f8e9e2f9)
- v1.30.5 - Version & Changelog vorbereitet (d177b50d)

## v1.30.5 - 2026-08-25

### 🔧 Sonstiges

- **repo**: Docker unter docker/, Changelog gebündelt, Artefakte raus (008e2207)
- **repo**: Gedanken- und Halbgeviertstriche durch normale Bindestriche (12ac6e11)
- v1.30.4 — Version & Changelog vorbereitet (b9af6e63)
- v1.30.5-beta.1 - Version & Changelog vorbereitet (2e4e91b1)

## v1.30.5-beta.1 - 2026-08-25

### 🔧 Sonstiges

- **repo**: Docker unter docker/, Changelog gebündelt, Artefakte raus (008e2207)
- **repo**: Gedanken- und Halbgeviertstriche durch normale Bindestriche (12ac6e11)

## v1.30.4 - 2026-08-25

### 🚀 Features

- **catalog**: Regelbausteine und Anwendungen zweisprachig (2b91c521)
- **rhel**: Red Hat als Client vollwertig unterstützen (445ecf32)
- **apps**: Reiter „Anwendungen" für Software neben der Paketverwaltung (3a8fc853)
- **ui**: Tabellen blättern, Pakete sortieren, lange Karten zuklappen (964e7389)
- **agent**: lcm-agent auch als RPM, APK und Arch-Paket (d571efa7)
- **kernel**: alte Kernel samt Begleitpaketen entfernen (55ba3264)
- **packages**: Snaps aktualisieren und entfernen (9b704f6d)
- **profiles**: Berechtigungsprofile lassen sich kopieren (68c18d4f)
- **profiles**: Berechtigungsprofile und Regelbausteine als Reiter der Linux-Benutzer-Seite (f09d77b8)
- **profiles**: Katalog von 67 mitgelieferten Regelbausteinen (554d31b2)
- **profiles**: Regelbausteine tragen Verzeichnisrechte und einen Zielbenutzer (cd8a0b46)
- **repos**: https-Umstellung der Paketquellen rückgängig machen (7295f679)
- **rules**: Regeltyp „Neustart bei Bedarf" (c911cd51)
- **system**: LCM spielt sein eigenes Paket auf Knopfdruck ein (c2d89701)
- **docker**: Trivy als eigener Container, damit der CVE-Scan auch dort laeuft (3fee3be3)
- **docker**: Runtime-Image auf scratch, und der Container weiss, wo er ist (d5bee43e)
- **docker**: Betriebsart erkennen und sich selbst pruefen koennen (45d756fc)
- **docker**: Server im Klartext und drei Sichten statt einer Tabelle (59641af7)
- **groups**: Vorrang entscheidet bei konkurrierenden Grundsatz-Regeln (e85a4d37)
- **profiles**: Berechtigungsprofile fuer Linux-Benutzer (0b1c99e8)
- **profiles**: Kontotyp „nur Dateizugriff" und Wirkung im eingeschraenkten Modus (5359bf0e)
- **profiles**: Profile zuweisen und auf den Servern anwenden (d457ca89)
- **profiles**: Regelbausteine als Vorlagen fuer Berechtigungsprofile (3120cde4)
- **profiles**: Standardverzeichnisse gesammelt abschotten (c8800105)
- **profiles**: Verzeichnisrechte ueber POSIX-ACLs (18fd24fa)
- **profiles**: Verzeichnisse abschotten und Rechte-Soll gegen Drift (acaf7e00)
- **security**: CVE-Quelle je Server sichtbar, filterbar, ungenutzte Images ausgenommen (521c038b)
- **security**: Frühwarnung auf Knopfdruck, mit Zustand und Filter (88b1254c)
- **security**: Trefferquote beider Zwischenspeicher sichtbar (7cbb3583)
- **alerts**: Alarm auf neue Frühwarn-Befunde (527433e5)
- **security**: Ausnutzungs-Signal der EUVD (Etappe C) (42914f87)
- **security**: Frühwarnung fragt alle 15 Minuten ab (opt-in) (5232d6d0)
- **security**: Frühwarnung - Datenmodell und OSV-Anbindung (f4102efa)
- **security**: Trivy-Datenbank aktualisiert sich selbst alle 6 Stunden (22cf2054)
- **security**: lokale OSV-Kopie - nichts verlaesst das Haus (Etappe C) (1285716d)
- **ui**: Frühwarn-Ansicht mit Update- und Einfrier-Aktion (84b5d8fe)
- **alerts**: Gruppen-Auswahl mit Suche und Pillen (1138db8e)
- **security**: Sandbox des CVE-Scanners nachrüsten (80940279)
- **alerts**: Alarmregeln gelten für mehrere Servergruppen (3543855b)
- **groups**: Zeitplan-Baukasten und Regeltyp „Paketliste aktualisieren" (15eb6d77)
- **users**: Linux-Benutzer automatisch abgleichen, mit Rückstand (f5cd4e6b)
- **docs**: mitgelieferte Anwender-Doku unter /doku (77d14662)
- **server**: Benutzer-Tab um Sync, serverweite Sperre und Login-Historie (76fd7966)
- **security**: CVE-Scanner läuft in einer Sandbox (6c8daa74)
- **onboarding**: aufklappbare Anleitung „Root-SSH öffnen" im Join-Wizard (2281210c)
- **server**: Benutzer-Übersicht je Server und SSH-2FA-Option (3d34bfa9)
- **repo-server**: lcm-channel-verify - prüfen, ob ein Kanal wirklich ausliefert (865da35c)
- **docker**: Docker-Updates sperren und Container-CVEs pro Server ausblenden (b05a838a)
- **subscription**: Beta-Paketkanal direkt in der Oberfläche wählbar (c8d26470)
- **subscription,repo**: Kanäle kennzeichnen und LCM per Vorrang-Regel binden (e849e9c0)
- **backup**: schwache Backup-Passphrasen abweisen (5aae08d9)
- **privacy**: Server-Zuordnung und Benutzerfelder at rest tokenisieren (4bc9f5b7)
- **restricted**: veralteten LCM-Helper erkennen und ausweisen (df95ece9)
- **subscription**: Server-Kontingent des Vertrags anzeigen - Hinweis statt Sperre (3b8710e5)

### 🐛 Bugfixes

- **health**: Lebenszeichen hängt nicht mehr am Datenbank-Ping (af0a8f13)
- **profiles**: Klonen nimmt dem Original nicht mehr die Regeln (edda94b3)
- **update**: Beta-Updates werden wieder erkannt, und vorher wird gesichert (4afe9e30)
- **i18n**: fehlenden Schlüssel ergänzen und beide Kataloge absichern (64f6ba11)
- **agent**: OpenRC startet den Agent nach einem Absturz wieder (fa2ddd6a)
- **agent**: Paket auf Arch installierbar, Alpine bekommt einen Dienst (8d3733c7)
- **kernel**: Kernel-Inventar nach jeder Paket-Aktion nachziehen (524c133a)
- **e2e**: Docker-Schalter-Test stellt seine Ausgangslage her, statt sie vorauszusetzen (64c0edab)
- **e2e**: Einstieg meldet sich an, wenn die vorbereitete Sitzung fehlt (baca82f9)
- **profiles**: die drei Befunde aus dem Langzeittest (be49c0e2)
- **ci**: Image-Push scheiterte an den Attestationen von buildx (f9586b4c)
- **frontend**: Pfad-Deep-Links beim App-Start in Hash-Routen uebersetzen (6c34ab43)
- **build**: Go-Untergrenze gehoert ins Repository, nicht ins CI-Image (31b212d8)
- **profiles**: Profilwechsel loeste die alte Gruppe auf BusyBox nicht (08b6f74a)
- **storage**: Update einer Bestandsdatenbank scheiterte am Tabellen-Neuaufbau (9626a7ab)
- **alerts**: Selbstbeobachtung haengt nicht mehr an einem Servereintrag (8c11b346)
- **demo**: Demo-Benutzer standen ohne Berechtigungsprofil da (806bfcaf)
- **packages**: Update-Pfade frischten die Paketliste unterschiedlich auf (bcb188cd)
- **profiles**: Distributions-Durchsicht - Drift rechnete dezimal, Dienstprobe brauchte systemd (5d8c29b6)
- **profiles**: die neuen Funktionen waren in der Oberflaeche nicht erreichbar (488f1960)
- **security**: Abschottung des Scanners war ohne Servereintrag unsichtbar (f8be6a3c)
- **security**: Spiegellauf und Durchgang melden ihr Ergebnis (fdab9a50)
- **settings**: Fehleingaben ergeben 400 mit Grund statt „interner Serverfehler" (29e28421)
- **settings**: eigene Seite „Sicherheit" - der Hinweis lief ins Leere (95e9db8f)
- **ui**: Frühwarn-Tabelle - Aktionen nebeneinander, Seitennavigation (b912e3b8)
- **ui**: Sicherheits-Karten einklappbar, SSH-2FA erst nach der Einrichtung (bf845760)
- **ui**: nach einem Neubau blieb die alte Oberflaeche stehen (5a2f72a1)
- **security**: CVE-Scan lädt die Datenbank nicht mehr mitten im Scan nach (75bb030b)
- **security**: Frühwarnung fand online ueberhaupt nichts (769096cb)
- **security**: Schwere der Befunde wird tatsaechlich ermittelt (8eae2a1a)
- **settings**: Seite „Allgemein" laesst sich wieder speichern (a78eabc5)
- **server**: Anmeldungen des LCM-Zugangsbenutzers sichtbar machen (597b6261)
- **ui**: deaktivierte Konten treten im Dark Mode zurück (76bf4b5c)
- **server**: „zuletzt angemeldet" auch ohne lastlog (ab95e7c0)
- **ui**: Statusbefunde und Sitzungsmeldung auch auf Englisch (18f8345a)
- **security**: SSH-2FA-Aussperr-Probe über den Lese-Slot (5d8dd3fb)
- **server**: sshd-Konfiguration greift auch bei socket-aktiviertem Dauer-Daemon (9734794a)
- **packaging**: Gegenprobe scheitert nicht mehr am pipefail-EPIPE (9fe3befa)
- **release**: Auslieferung nachweisen und den Agent aus dem Enterprise-Kanal halten (923df743)
- **ci**: Release nicht mehr an SIGPIPE bei der Tag-Suche scheitern lassen (08e618d6)
- **reboot,ui**: Rückkehr nach Neustart überwachen, Oberfläche nach Update wirklich neu laden (be51aabd)
- **ssh**: falsches Passwort nicht mehr als gehärteten Server melden (095974e6)
- **ssh**: vorrangige PermitRootLogin-Zeile selbst stilllegen und beim Freigeben zurücknehmen (0c1d737f)
- **update**: Selbst-Update sauber abschließen und die Oberfläche nachziehen (38f7f656)
- **update,status,ssh**: Kanal der Update-Prüfung, gewichtete CVEs erklären, Root-Sperre nachweisen (302f1ee3)
- **firewall**: Bemerkung speichern, Bind-/Zieladresse entfernen (7a6489d2)
- **scheduler**: Update-Prüfung lässt sich wieder auslösen (b000c919)
- **subscription,repos**: Community-Quelle beim Enterprise-Wechsel wirklich stilllegen (3a8757ee)
- **ssh,firewall**: Portwechsel am Generator ausrichten, Bemerkung auch in nftables (3dbef8d8)
- **ssh,firewall**: Portwechsel auf socket-aktiviertem sshd, Quellen für SSH, Bemerkungen, Port-Abfrage (a994594d)
- **backup**: Backup und Restore streamen statt die Datenbank in den RAM zu legen (71c28b4b)
- **packaging**: config.json für den Dienst beschreibbar machen (861aa609)
- **restore**: Entpack-Grenze für hochgeladene Archive wieder ziehen (89506298)
- **restore**: schreibgeschützte config.json legt LCM nicht mehr lahm (78e7e7ff)
- **settings**: Zeit & NTP speichert wieder (8faf7537)
- **ci**: E2E-Läufe serialisieren und in der CI wiederholen (6281ad3c)
- **ci**: finale Versionen erst auf community releasen (7b755569)
- **deep-scan**: im eingeschränkten Modus wirklich prüfen statt "sauber" zu melden (68989de5)
- **deps**: nanoid auf 3.3.17 heben (npm-audit-Gate) (642b657e)
- **e2e**: Assertion-Timeouts an langsamen CI-Runner anpassen (31259bcd)
- **ssh**: Root-Login-Sperre nicht mehr durch ein fehlendes Feld aufheben (52bf52a2)
- **ssh**: socket-aktivierten sshd nicht mehr zerlegen (Debian 13) (676cfdd8)

### ⚡ Performance

- **ci**: Cross-Build bekommt einen eigenen Cache-Schluessel (e582e67f)
- **ci**: e2e nutzt das Playwright-Image statt Browser nachzuladen (ead558aa)
- **ci**: nur noch cache-fuellende Jobs schreiben den Cache zurueck (d794a00f)
- **e2e**: Sitzung einmal je Worker, eine Instanz je Worker (da1567b1)
- **security**: CVE-Scan wiederholt identische Paketbestaende nicht mehr (364b080d)

### ♻️ Refactoring

- **blocks**: Parameternamen der Regelbausteine auf Englisch (ff9a801f)
- Variablennamen durchgehend auf Englisch (60ed350b)
- **trivy**: Naht zwischen „was Trivy tut" und „wie es ausgefuehrt wird" (2219aecf)
- **deps**: QR-Code und Log-Rotation selbst statt als Fremdpaket (aab12753)

### 🔧 Sonstiges

- **api**: die 52 fehlenden Endpunkte nachtragen und den Stand absichern (beabdbc8)
- Planungs- und Assistenten-Dateien aus dem Repo nehmen (2ca91f4d)
- englische Doku auf den Stand der deutschen bringen (1069f774)
- veraltete Aussagen im README richtigstellen (4dcba037)
- **remote**: Agent hinter einem Reverse-Proxy einrichten (a1099064)
- **ci**: Sidecar traegt die Trivy-Version statt der von LCM (357f5b33)
- **ci-release**: Branch-Modell auf beta/community/enterprise nachziehen (1d375d9e)
- **deps**: sqlite ist durch upgradecheck eine direkte Abhaengigkeit (51b76283)
- **upgrade**: Upgrade-Test von 1.11.0 auf den Pruefstand (1e79335c)
- v1.26.0-beta.1 - Version & Changelog vorbereitet (fa74881d)
- **images**: Container-Images fuer amd64 und arm64 bauen und veroeffentlichen (ae6fb5d3)
- **e2e**: Port umstellbar, damit zwei Arbeitsbaeume parallel testen koennen (e7e61cde)
- **router**: Zeitlimit der Testanfragen haelt Lastspitzen aus (cf9e72d5)
- Release-Zug ohne Versions-Bump scheitert jetzt im Merge Request (0026e687)
- v1.23.0-beta.2 - Version & Changelog vorbereitet (6237ac9e)
- **sshx**: Fake-Dialer haelt paralleler Ausfuehrung stand (5ca7cf5c)
- **app**: Anleitungen für Agent, RouterOS und Synology DSM (c3735f71)
- **app**: Anleitung „Zwei-Faktor-Anmeldung für SSH einrichten" (0d9280db)
- **ci**: npm-Pakete ohne postinstall-Skripte installieren (c4fff4e9)
- **deps**: Abhängigkeiten aktualisieren (c0a723bf)
- **alerts**: Webhook-Ziel im eigenen Netz braucht eine vertraute CA (67b67eba)
- MRs aus community nach enterprise zulassen (df8010b3)
- **release**: Doku- und Wartungs-Commits lösen kein Release aus (b1c9bc6f)
- **release**: festhalten, dass die Vorabversion die Zielnummer trägt (71ba9212)
- **release**: Schlusshinweis auf beta nach Versionsform unterscheiden (1cadfa46)
- Verhaltensänderungen der letzten Fixes nachziehen (e051874c)
- **ui**: Seitennavigation unter allen Tabellen vereinheitlichen (52adf918)
- Open Source unter AGPL-3.0 - Lizenz, CLA und Beitrag-Workflow (8eadb3b2)
- Pipelines auf das Branch-/Kanal-Modell umstellen (486fed0c)
- interne Entwicklungs-Artefakte (dev-docs) aus dem Repo auslagern (5b426477)
- interne Entwicklungs-Artefakte (dev-docs) aus dem Repo auslagern (728dbc17)

## v1.11.0 - 2026-08-07

### 🚀 Features

- **backup**: feste Backup-Uhrzeit und Alarm bei ueberfaelligem Backup (R2-034, R2-028) (a991255b)
- **deep-scan**: jeden Lauf als datierten Bericht ablegen und den Fortschritt ausweisen (0e381777)
- **dsm**: Synology DSM als API-Geraetetyp ueberwachen (e92bc845)
- **kernel**: laufenden Kernel und installierte Kernel-Fassungen ausweisen (1352b0d8)
- **security**: Stand der CVE-Datenbank sichtbar machen und nachladbar (998b37c7)
- **security-tools**: installierte Werkzeuge verwalten (Backend + API) (23eac57c)
- **security-tools,packages,ui**: Werkzeuge bedienen, Paket-Pins, ehrliches Feedback (d220bef5)
- **self-register**: den LCM-Host automatisch als Server aufnehmen (2e5947f4)
- **subscription**: Enterprise-Paketkanal in LCM verwalten (fb9db3db)
- **time**: Zeitzone, Zeitabgleich und NTP verwalten (debe228f)
- **packaging**: Community- und Enterprise-Paketkanal fuer das apt-Repository (3190d139)
- **robustness**: Panic-Schutz, Selbstueberwachung und kontrollierter Selbstneustart (706c26aa)

### 🐛 Bugfixes

- **alerts,mail**: Versand entkoppeln und SMTP-Standardkopfzeilen setzen (R2-033, R2-031) (a3a3d881)
- **api**: Schreibendpunkte lehnen ab, statt Eingaben still zu verwerfen (086ce942)
- **api**: praezise Meldungen, zweistufige Key-Loeschung, globale Regel-Sicht (R2-032/053/074/085) (4eee4b87)
- **cve**: Trivy-Fehler verstaendlich melden statt Stacktrace in der Ampel (R2-005) (d602dc8e)
- **cve**: unerreichbare Server nicht mit frischer Scan-Zeit bewerten (R2-017) (cfffafb3)
- **firewall**: Allowlist-Menue in den Top-Layer heben (42dbe33c)
- **firewall**: Portliste verschwindet mit der Firewall, nicht erst mit dem Werkzeug (7f63eb48)
- **log**: abgewiesene Anmeldungen im Access-Log unterscheidbar machen (R2-049) (8f55fe57)
- **monitoring**: Offline-Kennzeichen fuer jeden ausgefallenen Server (31e3f1f9)
- **monitoring**: Unerreichbarkeit loggen, listening_packages einheitlich (R2-018, R2-084) (c9d468c2)
- **ops**: Betrieb ehrlich melden - Backup, Jobs, Alarme, Audit-Trail (b8c7b02d)
- **packages**: Paketquellen, Sicherheits-Tools und Befunde ehrlich ausweisen (ac3f09ef)
- **privileges**: eingeschraenkten Modus auf allen Distributionen tragfaehig machen (e6267950)
- **remote**: Zeit-Aktionen fuer Agent-Server freigeben, RestrictSudo sperren (bb615c4a)
- **rules**: Grundsatz-Regeln brauchen einen Soll-Zustand und melden ihre Eingriffe (R2-082, R2-087) (5af25a74)
- **scan**: DNS-Befund und Kernel-Bestand in JEDEM Scan-Weg erheben (66b83184)
- **security**: Ampel auf Behebbares stuetzen, Firewall-Freigaben nie fail-open (c6799d97)
- **ssh**: Haertung nur melden, wenn sie nachweislich wirkt (b88c1e7e)
- **users**: Alpine-Kontoanlage auf gesperrtes Passwort und 700er-Home normalisieren (R2-046) (45e499f8)
- **users**: Benutzer sperren, Zugaenge wirklich entziehen, Rechtevergabe protokollieren (bf181904)
- **users**: Reconnect-Zweck klarstellen, Token-Hygiene, Doku ans Verhalten (R2-010, R2-047, R2-045) (711a438a)

### ♻️ Refactoring

- **i18n**: Installer-, Dienst- und Journal-Ausgaben auf Englisch mit Deutsch bei deutschem System (1cbc5b33)

### 🔧 Sonstiges

- **install**: curl- und gnupg-Voraussetzung fuer Minimal-Images nennen (R2-001, R2-002) (eff8de36)
- **openapi**: fehlende Endpunkte nachtragen - vollstaendige Routen-Abdeckung (7c76e58f)

## v1.9.1 - 2026-07-27

### 🚀 Features

- **agent**: lcm-agent Binary - enroll, run, uninstall (b53028f7)
- **allowlists,crowdsec,firewall**: Zentrale IP-Allowlists + CrowdSec-LAPI auf dem LCM-Host (6ad60d49)
- **firewall,security,dns,deep-scan,apt-cache**: Multi-Backend-Firewall, Sicherheit-Tools, DNS, Deep Scan, APT-Cache-Seite, Kernel-Filter (6ca42f38)
- **frontend**: RouterOS/MCP-Oberflächen, Passwort-Stärkeanzeige, APT-Cache-Übersicht, Sicherheits-UI (b9975179)
- **remote**: /mqtt-WebSocket-Route im HTTPS-Listener + IP-Allowlist-Ausnahme (6033af33)
- **remote**: Agent-Enrollment - Token-Erzeugung und HTTP-Endpunkte (c7cc3e69)
- **remote**: Agent-Transport im Verbindungs-Dispatch + SSH-Feature-Gating (a767a7f9)
- **remote**: Transport- und Agent-Felder am Server-Modell + MQTT-Abhängigkeiten (6433560d)
- **remote**: eingebetteter MQTT-Broker mit Agent-Auth, ACL und RPC-Hub (ec3bacc3)
- **routeros,mcp,packages,backups,security**: RouterOS-Geräte, MCP-Schnittstelle, Autoremove, Backup-/APT-Cache-Härtung, Sicherheits-Audit (a98e149c)
- **ui**: Agent-Modus im Join-Wizard, Badges, Gating + Tests (5cf63327)
- **ui**: Agent-Onboarding als 3-Schritt-Anleitung (Repo → Installation → Dienst) (05c6310a)

### 🐛 Bugfixes

- **dns,ports,crowdsec**: DNS-Check im Hardware-Scan, LCM-Portumzug (9310/9320/9330), CrowdSec-LAPI-Monitoring (53e259d2)
- **frontend**: postcss auf 8.5.23 anheben (GHSA-r28c-9q8g-f849) (611db53f)
- **remote**: Agent-Kommandos robuster - Erst-Scan-Race und Disconnect-Haenger (e00966ff)

### 🔧 Sonstiges

- **agent**: lcm-agent-Paket, CI-Jobs und Binary-Download vom Server (e91f2187)
- RouterOS/MCP/Remote-Guides ergänzen, Sicherheit/Backups/APT-Cache/OpenAPI aktualisieren (83f5242b)

## v1.8.0 - 2026-07-23

### 🚀 Features

- **packages,repos**: Voll-Support pacman/apk + Paketquellen-Katalog je Paketverwaltung (a86b75f7)
- **alerts,docs**: Hinweis auf stumme Alarmregeln, Doku-Fußnote zu Kompression (df4a52b0)
- **security**: Docker-Ports an der Firewall benennen und für die CVE-Bewertung werten (a6db313b)
- **alerts**: Alarmtyp "APT-Cache nicht erreichbar" (989f0c2e)
- **build**: Commit-Kennung im Binary - welcher Code läuft, ist jetzt belegbar (94d17cb3)
- **i18n**: Texte für Login-Eskalation, APT-Cache, Alarmtyp und Dev-Build-Marker (7f94672c)
- **rbac**: Manager-Zuweisung ergänzen, API-Fehlermeldungen schärfen (e61eacac)
- **server**: APT-Cache-Seite mit Transfer-Statistiken und erweiterten Einstellungen (ce92eaea)
- **server**: passwortloses "login root" als Eskalationsweg beim Onboarding (72511ee9)
- **system**: Update-Prüfung über das eigene apt-Repository statt GitLab (aa8db62f)

### 🐛 Bugfixes

- **ci**: gofmt-Prüfung auf eigene Quellen begrenzen, x/text-Lücke schließen (15696ffb)
- **demo**: Paketbestand für alle Demo-Server hinterlegen (38c85bdc)
- **diag**: Debug-Logging der SSH-Strecke, Ursachen in Fehlermeldungen, CVE-Meldung mit Bezug (39a27662)
- **provisioning,docs**: provisionierte Konten nutzbar machen, Doku an die Wirklichkeit angleichen (d58a4392)
- **security**: SSH-Härtung, Firewall und Proxmox-Erkennung melden nur noch belegte Ergebnisse (dfde8c02)
- **server**: Onboarding scheitert sichtbar, statt unverwaltbare Server als gesund aufzunehmen (87eff384)
- **status**: Server ohne Bestandsaufnahme und ohne CVE-Bewertung nie mehr als gesund führen (7ec0c105)

### 🔧 Sonstiges

- **security**: Reichweite des eingeschränkten Modus ehrlich benennen (6e4676d4)
- OpenAPI-Spezifikation auf den aktuellen Routenstand bringen (88fce5f4)
- REST-API-Referenz + OpenAPI-Spezifikation aus dem Quellcode (1939e0af)
- Screenshots und Beispiele in Guides ergänzen (DE+EN) (c26839bd)
- Sidebar chronologisch sortieren statt alphabetisch (6fd2f37f)
- gofmt-Formatierung in drei Dateien nachziehen (f01e88b3)

## v1.5.0 - 2026-07-19

### 🚀 Features

- **server**: Eingeschränkter Modus deckt alle Kernfunktionen ab - validierender lcm-helper (94c7836c)
- **status**: OK-Gründe, Docker-CVE-Relevanz je Container und Alle-Images-Pull (1f676e32)
- **ui**: Status-Popover bei OK, CVE-Relevanz-Toggle, Pull-All, Restricted-UI und Offline-Pille (5c7ea147)
- **server**: LCM-Host richtet Trivy/apt-cacher-ng nach Installation selbst ein (8afa4389)
- **repos**: TechEve-Paketrepository in den Katalog aufnehmen (91bd3a8e)
- **server**: LCM-Host-Einrichtung, Duplikat-Host-Erkennung und LCM-Logo (be73f257)
- **packages**: Paketnamen-Suche im Pakete-Tab (c4e7a9c3)
- **Festplatten**: Erkennung aller eingehängten Volumes (durchgereichte Dateisysteme) mit Belegungsbalken und Verlaufs-Tooltip; Speicherangaben in binären Einheiten (MiB/GiB/TiB); das Root-Volume bleibt maßgeblich für Ampel und Prognose
- **Sicherheit**: "Alle VMs aktualisieren" auf der Security-Seite (Sammel-Lauf mit Fortschrittsanzeige) sowie ein Filter zum Ausblenden von Docker-CVEs
- **Verfügbarkeit**: pro Server "Nichterreichbarkeit unkritisch" mit Kulanzfrist (Standard 28 Tage) - offline gegangene Server behalten ihren Status und werden im Dashboard nur ausgegraut
- **Server-Aktionen**: Neustart und nachträgliches Einschränken der LCM-Rechte (jeweils mit Bestätigung); Aktionen-Dropdown auf der Detailseite
- **UI**: Emoji durchgängig durch theme-adaptive SVG-Icons ersetzt
- **docker**: ungenutzte Images aktualisieren + CVE-Rescan nach Updates (5acc4025)
- **monitoring**: Status "Sehr gut", gewichtete CVEs und Neustart-Erkennung (16a1812a)
- **onboarding**: SSH-Key-Anleitung und automatische su-Root-Erkennung (7fd4e535)
- **server**: per-VM SSH-Schutz und Einstellungs-Modal (97a9e068)
- **ui**: LCM-Logo - Marke, Favicon und Wortmarke in der Navbar (f9978a58)

### 🐛 Bugfixes

- **ui**: LCM-Host-Logo ohne weißen Kreis und etwas größer darstellen (254911d5)
- **web**: index.html mit no-cache ausliefern (Updates kamen nicht an) (19a633ac)
- **monitoring**: Betriebssystem am oder weniger als einen Monat vor End-of-Life setzt den Server auf rot/kritisch
- **update-check**: LCM-Selbst-Update-Prüfung fest im Kern verankert (alle 3 Stunden, keine Einstellungsoption mehr)
- **onboarding**: SSH-Schlüssel unter Windows als Standardschlüssel einrichtbar (Verbindung ohne -i)

### 📖 Dokumentation

- Doku auf reines Markdown umgestellt (Astro-Gerüst aus dem Repo entfernt, CI-Build/-Deploy über das zentrale Builder-Image) und um die neuen Funktionen ergänzt (DE + EN)

### 🔧 Sonstiges

- Seite „Status-Berechnung" (DE+EN) + CVE-Gewichtung in den Guides aktualisiert (2fbdb93e)
- apt-Repo-Installation, Hero-Logo, Changelog-Seite und Repository-Link (a617cc90)
- überflüssige benutzerdefinierte 404-Seiten entfernen (81eda32a)
- Header-Logo und benutzerdefinierte 404-Seite ergänzen (de3bd592)

## v1.0.2-beta.1 - 2026-07-16

### 🚀 Features

- **i18n**: Dashboard, Sicherheit, Docker, 404, StatusBadge & Formathelfer zweisprachig (83f9fa60)
- **i18n**: Jobs, SSH-Protokolle, Reconnect-Wizard, Passwort-/Modal-Komponenten (373e0a68)
- **i18n**: Mein Konto, Server-Onboarding, Aktivierungsseiten zweisprachig (75230c95)
- **i18n**: Server-Detailansicht vollständig zweisprachig + locale-abhängiges Zahlenformat (5f54e486)
- **i18n**: Servergruppen & Linux-Benutzer zweisprachig (inkl. Modals & Meldungen) (070831dd)
- **i18n**: Settings Allgemein zweisprachig - i18n-Abdeckung der UI abgeschlossen (2dd430db)
- **i18n**: Settings Backups, Benachrichtigungen & Alarme zweisprachig (a3410dbb)
- **i18n**: Settings Benutzer & Repositories zweisprachig (a8db3d93)
- **i18n**: Settings-Navigation, Schedules, API-Keys, Custom-Aktionen zweisprachig (5b7a67b2)
- **i18n**: Sprachumschalter (DE/EN) mit Flaggen + i18n-Fundament (8de93b48)
- **logging**: persistente, rotierende Logdatei + Dienst-Lifecycle-Marker (0c1a9590)
- **onboarding**: eingeschränkter Service-User (sudo-Whitelist statt Voll-Root) (f4bca427)
- **ui**: Impressum-Daten eingetragen + Popover als kleines Fenster (48a4d3c4)
- **ui**: Info-Fenster zentriert als Modal (Backdrop, größer, 'Info'-Titel) (0594768e)
- **ui**: Log-Retention 7 Tage; Farbmodus & Sprache als je eine durchschaltende Icon-Schaltfläche (1d6359ee)
- **ui**: Navbar-Farbmodus als Icons neben Konto-Pille, Emojis raus, Impressum-Feinschliff (116918f6)
- **ui**: Update-Prüfung mit Banner, Footer-Copyright & Impressum-Popover (bd922702)
- **docker**: Button 'Ungenutzte Images aufräumen' (prune) je Server (82dcfeba)

### 🐛 Bugfixes

- **jobs**: Server-Sperre robust - Startup-Recovery, Watchdog, manueller Abbruch, Verbindungs-Limit, parallele Gruppen-Läufe (063807f6)
- **cve**: CVE-Bewertung nach Paket-Updates automatisch neu erstellen (b5c36795)
- **onboarding**: Server mit sudo-Benutzer (ohne Root-SSH) anbinden (0be986f7)
- **security**: CVE-Tabellen paginieren (50/Seite, Gesamtzahl, Navigation) (5a4ae38a)
- Backups löschbar, Session-Dauer einstellbar, deb822-Repos erkannt (7b55ca64)

### 🔧 Sonstiges

- Build-Nummer auf 96 erhöhen (43936335)
- Changelog für v1.0.0-beta.1 nachtragen (5028e272)
- Changelog vor dem Merge auf develop vorbereiten statt danach zurückschreiben (568fbf48)

## v1.0.0-beta.1 - 2026-07-12

### 💥 Breaking Changes

- **db**: UUID-Primärschlüssel für Protokoll-/Bestandstabellen (v0.2.0) (180345e3)
- **demo**: demo-modus mit testdaten, tutorial-rückbau, e2e für LCM-ui (ff6ecec2)

### 🚀 Features

- **alerts**: add alert rules, history, and evaluation engine (bad9cbc3)
- **apt-cache**: apt-cacher-ng-anbindung in den einstellungen mit erreichbarkeits-check (a58f1ce4)
- **apt-cache**: gruppen-regel apt-proxy (geplant + grundsatz), anleitung, v0.9.0 (54e379d0)
- **apt-cache**: server-aktion "APT-Cache verwenden" mit funktionstest und rollback (1a8d58ed)
- **audit**: lückenlose ssh-protokollierung, verknüpft mit server & job (494bed9a)
- **backup**: Frontend - Historie mit Download/Rollback + Upload-Restore (1d78bb87)
- **backup**: Restore via Staging + Anwendung beim Start (6aa1de45)
- **backup**: Restore-API (Download, Rollback, Upload) + Neustart-Schalter (96851115)
- **backup**: verschlüsseltes Voll-Backup als ein portables .lcmbak-Archiv (6724d8c4)
- **core**: fundament - AES-GCM-verschlüsselung, LCM-domain-modelle, rollen (31c06995)
- **docker**: aktionen - compose-update, image-pull, inventar-refresh + api (37a5bfcc)
- **docker**: demo-daten, e2e-tests und doku - v0.7.0 (b3b70559)
- **docker**: frontend - docker-tab, globale übersicht, settings (c9baa8c5)
- **docker**: image-cve-scan mit trivy, quellengetrennte funde, ampel (8fc0e3b9)
- **docker**: inventar von containern und images per ssh-scan (60077a11)
- **docker**: ungenutzte images loeschen + prune-gruppenregel (v0.8.0) (475222cb)
- **docker**: zentraler registry-update-check der image-tags (d72fb0cd)
- **firewall**: aktivieren/deaktivieren + konfigurierbare ports, pro server und als gruppenregel (87f21f05)
- **frontend**: responsive navbar, einstellungs-sektion, linux-benutzer-ui (37af4156)
- **frontend**: svelte-5-ui für alle LCM-bereiche (02a0e155)
- **jobs**: health-checks in der job-historie ausblendbar (14a81f8c)
- **jobs**: inline-output, pagination und filter für die job-historie (0ef97ed4)
- **linux-users**: ssh-schlüsselpaar serverseitig generieren mit einmal-download (12e6878e)
- **linux-users**: tabellen-ansicht, popups, deprovisionieren vor löschen (11399c2c)
- **onboarding**: system-ssh-key + key-login fuer join/reconnect gehaerteter server (807ddd2c)
- **packages**: aktion + tägliche rule zum aktualisieren der paketliste (b65feaf8)
- **packages**: gezieltes paket-updaten pro server + neue paket-rule-typen (97b2094a)
- **provisioning**: user-zertifikat-verteilung, service↔user-zuordnung, aktivierungslinks (a9292eb3)
- **release**: Prerelease-Versionen unterstützen (Suffix aus VERSION) (524e582f)
- **repos**: http-quellen per klick auf https umstellen, katalog bekannter repositories (355708eb)
- **repos**: katalog bekannter paketquellen unter einstellungen pflegbar (537f3d43)
- **rules**: schedules bündeln mehrere rules; grundsatz-regeln werden bei jeder verbindung durchgesetzt (b509734a)
- **scheduler**: cron-scheduler, rule-ausführung, gruppen und monitoring (0942b334)
- **security**: 2FA/TOTP, ssh-hardening, firewall, HTTPS by default (c7bcb489)
- **security**: CVE-scan des paketbestands mit trivy (v0.4.0) (59cf6df9)
- **security**: IP-Allowlist für den Zugriff auf Weboberfläche und API (a6fd296e)
- **security**: Login-Policy serverseitig erzwingen (Token, 2FA, Passwortwechsel) (247c574f)
- **security**: Re-Authentifizierung beim eigenen Passwortwechsel (737ed8ba)
- **security**: SSH-/Job-Konsolen-Output at-rest verschlüsseln (AES-GCM) (b7b8560f)
- **security**: Server-Host und IP-Adressen at-rest verschlüsseln (AES-GCM) (427ebeea)
- **security**: Server-Name at-rest verschlüsseln (AES-GCM + HMAC-Blindindex) (b776a12e)
- **server**: apt und snap als getrennte paketverwaltungen ausweisen (e00dd11d)
- **server**: entfernen mit ziel-bereinigung vs. einfachem löschen (3616efcb)
- **server**: paketverwaltung für RHEL-familie (dnf/yum) und SUSE (zypper) (870235f8)
- **server**: reconnect-prozess - credentials neu setzen bei geänderten/ausgetauschten servern (b47103c2)
- **server**: snap-pakete als zweite paketverwaltung anzeigen (472ab81e)
- **server**: virtualisierung, OS-Support (LTS/EOL) und Status-Gründe (44cabcf2)
- **servers**: SSH-infrastruktur, zero-trust-onboarding und monitoring (b4518b97)
- **servers**: buttons 'Hardware aktualisieren' und 'Alles aktualisieren' (b74ca025)
- **storage**: Speicherprognose via linearer Regression (af14bce8)
- **storage**: festplatten-verlauf pro server (health-check misst stündlich) (c75816a7)
- **ui**: Dark Mode, Seiten-Übergänge und Design-Feinschliff (d5b7bdeb)
- **ui**: Konto-Pille mit Dropdown in der Navbar (spart Platz) (1154422f)
- **ui**: Konto-Seite über Navbar, Einstellungen sortiert, Schedules-Label (4b9c8d06)
- **ui**: cve-badges direkt an paket- und container-zeilen (bbc88f75)
- **ui**: echte Betriebssystem-Logos als PNG-Assets statt Inline-SVG (6ce2757a)
- **ui**: filter und pagination für die server-tabelle im dashboard (2db32b26)
- **ui**: firewall-popup schlägt docker-veröffentlichte ports vor (1f27cc79)
- **ui**: servergruppe nachträglich bearbeitbar (name + beschreibung) (edf8f5c7)
- **users**: linux-benutzer als eigenes modell, LCM-user-bearbeitung, schedule-umbau, 2FA-QR (32f6d225)
- E-Mail-/Webhook-Kanäle, Proxmox-Erkennung, Dark-Mode-Feinschliff und Session-Härtung (c1f37340)
- linux-user-aktivierungslinks, user-modals, gruppen-server-zuordnung, server-optik (ae1641cb)
- neue server automatisch in system-gruppe; reservierte usernamen sperren (057be0be)
- projektbasis aus gosvelte-kit-template - LCM-rebranding, spezifikation und umsetzungsplan (9ea0bcbe)
- ssh-härtung aufheben (toggle) + custom-aktionen als rule-typ (a52c7df5)

### 🐛 Bugfixes

- **alerts**: alert_events auf UUID-Primärschlüssel (7795a481)
- **alerts,backup**: alarme-seite laedt beim mount, restore meldet ab, backup_dir aus config (b5223be7)
- **apt-cache**: drop-in mit echten anführungszeichen schreiben (dash-printf) (95a7a64e)
- **ci**: ungültiges YAML im packages:deb-Script (Doppelpunkt im echo) (37dbc47e)
- **config**: Demo-Modus nur noch über das CLI-Flag --demo (eab8b711)
- **custom-actions**: kommandos als root (sudo) ausführen (18009662)
- **docker**: live-test-befunde - <no value>-labels und scan per digest (143f9d03)
- **groups**: rule an schedule speicherbar (schedule_id korrekt geparst) (64f3c647)
- **logs**: health-check-sessions wieder ausblendbar (40ddd926)
- **rules**: paket-rule ohne gültige paketliste liefert 422 statt 500 (d4c0ff26)
- **security**: Brute-Force-Schutz für Login und 2FA-Verify (efd62456)
- **security**: Go-Toolchain auf 1.26.5 (crypto/tls-CVE GO-2026-5856) (f34d959f)
- **security**: Mandantentrennungs-Lücken in drei Nebenpfaden schließen (87b313f6)
- **security**: Medium-Härtung (API-Keys, Injection, Datei-Rechte, Redaction) (0c8af45d)
- **security**: Transport-Härtung (CSP, HSTS, Timeouts, .gitignore, argon2) (25591a54)
- **seed**: Demodaten nur noch opt-in, Demo ohne Wartungs-Schedule (aa472842)
- **seed**: permissions und rollen bei jedem start abgleichen (28294e54)
- **server**: snap-paketverwaltung auch ohne installierte snaps anzeigen (279013d7)
- **ssh**: data race im output-capturing behebt sporadisch leere job-outputs (1817966b)
- **storage**: decommission mit job-historie + in-memory-DB-konsistenz (244dc3e7)
- **ui**: OS-Icons einheitlich groß und sichtbar; Proxmox-Mark vollständig (3026925a)
- **ui**: Username in der Navbar als erkennbare Konto-Schaltfläche (79c96f1c)
- **ui**: korrektes zweifarbiges Proxmox-Logo + weißer Kreis für Dark Mode (5641cae6)

### ♻️ Refactoring

- **docker**: Docker-Check als Rule des System-Sync-Schedules (af83bcaf)
- **ui**: servergruppen-seite auf tabellen + popups umstellen (e3a319b4)
- toten Code entfernen, Duplikate zusammenführen, Alert-Retention verdrahten (637b0433)

### 🔧 Sonstiges

- **backup**: portables verschlüsseltes Archiv + Restore/Download/Auto-Restart (71f854f8)
- **ci**: RENOVATE_TOKEN als geschützte CI-Variable (develop ist protected) (2ad79ccc)
- **e2e**: specs an neue frontend-struktur anpassen (1222ff19)
- **security**: live-integrationstest der SBOM→Trivy-CVE-Kette (edf91b06)
- .deb-Pakete beim Release auf den apt-Repository-Server ausrollen (d23da82a)
- Add new file plan (e97fea7c)
- Dependency-Bot (Renovate) für Go- und npm-Abhängigkeiten (32eb3759)
- Dokumentation auf den Stand v0.6.0 bringen (c0db24e2)
- Initial commit (e2bdbaf9)
- Release & apt-Deploy an ALLE Tests binden (inkl. e2e) (46f766ea)
- Roadmap auf Kern-Features eingedampft; Version 1.0.0-beta.1 (ecaef14b)
- buildnummer 60 (lokaler build) (5acae90f)
- check:commits toleriert Release-Commits (e9e1e470)
- debian-pakete (.deb) für amd64/arm64 mit systemd-dienst (13791557)
- e2e-webserver-timeout erhöhen und go-cache ergänzen (baac8055)
- readme-hinweis auf docker-image-aufräumen (118b7007)
- test (a00f127b)
- v0.10.0 - Doku zu Demo-Modus, Docker-Check-Verlagerung und UUID-PKs (2c44c307)
- v0.3.0 - multi-distro-paketverwaltung + snap, mit datenmigration (fedede09)
- zweisprachige Astro-Starlight-Doku-Website (DE/EN) in docs/ (d51f4b91)
