# Changelog

## v2.0.0-beta.1 - 2026-11-05

### 💥 Breaking Changes

- **api**: Browser-Anmeldung per Kopfzeile oder Cookie, der Bearer-Login entfällt (a0f74c63)

### 🚀 Features

- **api**: Befehlsvorschau für DNS, Zeit, Pins, Werkzeuge, Verzeichnisse und LCM-Host (22d02c60)
- **api**: Befehlsvorschau für Docker-Aktionen und Server-Konten (726d9ba4)
- **api**: Befehlsvorschau für Firewall, SSH-Port, Root-Login und SSH-Härtung (86c549f4)
- **api**: Befehlsvorschau für Paketquellen und APT-Cache (ddecf2c2)
- **api**: Befehlsvorschau für Server-Aktionen (ddc29f9c)
- **api**: Befehlsvorschau für fail2ban, CrowdSec und SSH-2FA (6ad3f5fe)
- **api**: Eingabeprüfung für alle Felder: SQL-Suchmetrik, Zahlengrenzen, strenge IP-, Netz- und IPv6-Prüfung (7a4ca7b9)
- **api**: Fehlercodes statt deutscher Freitexte in allen API-Fehlern (bdc36107)
- **api**: Listen mit Filter, Sortierung und Blättern im Backend, ETag, Mandantentrennung (c281f38c)
- **api**: Vorschau nennt die Wirkung von Root-Login und Pins (f875d644)
- **api**: is_lcm_host und Scanner-Stand für die LCM-Host-Karte, Demo-LCM-Host heißt lcm-host (3e486e08)
- **auth**: Cookie-Sitzung mit CSRF-Schutz und Sprache am Konto (5443c04e)
- **dns**: bis zu vier Nameserver je Server (41c18a7f)
- **events**: Ereigniskanal per WebSocket und Dashboard-Aufruf (b327a011)
- **mail**: Verschlüsselung als Auswahl (TLS, STARTTLS, Keine) und Absender mit Anzeigenamen (e549ebbf)
- **platform**: Ubuntu 26.04 LTS im Lebenszyklus (097827ac)
- **ui**: Anmeldung im Browser im Arbeitsspeicher oder per Cookie, Karte und einmalige Frage (4a79a0bb)
- **ui**: Apps im Serverdetail sichern und aktualisieren (e2ca1478)
- **ui**: Assistent „Server hinzufügen“ für SSH, Agent, RouterOS und DSM, dazu Neu verbinden (ab543fe2)
- **ui**: Befehlsvorschau aus dem Backend für Konfigurations-Aktionen (40935525)
- **ui**: Befehlsvorschau für SSH-Härtung, 2FA-Entfernen und Deep-Scan-Werkzeuge; APT-Proxy weist API-Geräte ab (7a9ab2a4)
- **ui**: Container-, Benutzer-, Deep-Scan- und Speicheraktionen im Serverdetail (2e2654cd)
- **ui**: Dashboard und Jobs-Seite (91cac1b0)
- **ui**: Dateien vorkomprimiert ausliefern, Grenzwert für das Start-JavaScript in der CI (dd884c68)
- **ui**: Doku mit Seitenliste, Abschnittsankern und sicherer Darstellung (908dc3b6)
- **ui**: Eingabefelder mit Zeichengrenze und Nur-Ziffern-Modus (497fd908)
- **ui**: Eingabeprüfung im Server-Assistenten, bei Sicherheitswerkzeugen und Pins, Namenskonflikt am Feld, Tests (a2cfd7dc)
- **ui**: Eingabeprüfung in Gruppen, Linux-Benutzern, Konto und Anmeldung (749f324b)
- **ui**: Eingabeprüfung in allen Einstellungsbereichen, API-Schlüssel mit Höchstlaufzeit (260f68cd)
- **ui**: Einstellungen Benachrichtigungen, Alarme und Ereignisse (ee6e17b4)
- **ui**: Einstellungen CrowdSec und Allowlists (4daca7d3)
- **ui**: Einstellungen Repositories, APT-Cache und Apps (d7f17ee8)
- **ui**: Einstellungen System mit Allgemein, Abo, Backups, Zeit und DNS (0981def3)
- **ui**: Einstellungen Zeitpläne und Eigene Aktionen (650d21a8)
- **ui**: Einstellungen Zugang mit Benutzern, API-Schlüsseln, MCP und Sicherheit (30052081)
- **ui**: Fehler im offenen Fenster bleiben im Fenster (ebefdeab)
- **ui**: Feldbreiten in Konto, Linux-Benutzern, Gruppen und Assistent; Feinschliff (a2133fc8)
- **ui**: Feldbreiten nach Inhalt und feste Inhaltsspalte für Einstellungen (247009f5)
- **ui**: Fenstergröße ml und dezentes Info-i für Erklärungen (a18144dd)
- **ui**: Firewall-Editor mit TCP/UDP-Zeile, strenger Quellenprüfung und Feldfehlern (adb59893)
- **ui**: Gerüst für das neue Frontend und API-Fehler mit Code (1b00e0f9)
- **ui**: Gestaltung nach ZPO-Linie, Serverdetail in der bisherigen Gliederung (7bc58f6e)
- **ui**: Grundbausteine für Router, i18n, API, Ereigniskanal und Service Worker (575bbeb1)
- **ui**: Gruppenseite mit Liste, Detail, Zeitplänen und Regeln (759c1bcb)
- **ui**: Karte Betriebsfähigkeit zeigt je Bereich eine Zeile, Paketstand, Quellenwarnung und Job-Ausgabe (b8a52b0a)
- **ui**: Komponenten und Katalog mit drei Designrichtungen (b7d54e15)
- **ui**: Konto, Anmeldung und Assistenten mit lcm-notice und Speicherleiste (4d29e68d)
- **ui**: Kontoseite meldet Passwort und 2FA in der Befehlspalette an (fcba1078)
- **ui**: Linux-Benutzer dichter, Tabellen mit Seiten, Fenster zweispaltig (50e77a4c)
- **ui**: Linux-Benutzer, Profile und Regelbausteine (Zwischenstand) (31bde9e6)
- **ui**: Mein Konto mit Profil, Passwort, 2FA und Darstellung; Pflichtschritt führt zum Ziel zurück (1df23555)
- **ui**: Menüpunkt heißt Subscription statt Abo (f7883476)
- **ui**: Paket-Pins, Versionswahl, Paketquellen, unbekannte Apps und alle Servereinstellungen im Serverdetail (cef1d014)
- **ui**: Popover, Dialoge, Schubladen und Meldungen blenden kurz ein und aus (e5b718cb)
- **ui**: Reiter Jobs und Repositories im Serverdetail (a66bc4b8)
- **ui**: Reiter Pakete mit Serveraktionen, Vorschau und Bestätigung (21ae1ce1)
- **ui**: Reiter Snaps, Container, Apps, Benutzer, Sicherheit, Deep-Scan, Speicher und SSH-Sitzungen (79442635)
- **ui**: Rückfrage, bevor ungespeicherte Änderungen beim Seitenwechsel verloren gehen (c6359401)
- **ui**: Seiten, Server, Gruppen und Reiter blenden beim Wechsel kurz ein (b0577cf6)
- **ui**: Selbst-Update mit Hinweisbalken, Info-Fenster mit Version und Impressum, Seitenkürzel und Pfeiltasten (4f01150d)
- **ui**: Servereinstellungen schmaler, mit Hinweisen und Eingabeprüfung (8e433c16)
- **ui**: Serverliste mit Befunden, Kennzeichen und Proxmox, Jobfilter Ausgelöst von, Regeln je Zeitplan, Apps-Schalter (a359acbe)
- **ui**: Serverseite mit Liste, Detailkopf, Aktionsleiste und Übersicht (9a8d4278)
- **ui**: Serverübersicht mit Hardware/Netz und System, LCM-Host-Karte und Gruppenzuordnung (e93ccccc)
- **ui**: Shell mit Hauptmenü, Befehlspalette, Anmeldung und Einstellungen (f40c91d4)
- **ui**: Sicherheit des Servers – Firewall-Editor, Sicherheits-Tools, SSH-2FA, Verzeichnisrechte und CVE-Herkunft (1a5916f9)
- **ui**: Sicherheit und Container als neue Seiten (53b98abf)
- **ui**: Terminal und Servereinstellungen im Serverdetail (220a6eaa)
- **ui**: Texte für Linux-Benutzer, Profile und Regelbausteine (155c8945)
- **ui**: Toast am Jobende mit Link, Dauer zählt mit, Update-Stand unsicher statt „Alles aktuell“ bei handlungsunfähigen Servern, Kernzahl im Singular (4bc8ab71)
- **ui**: Toaster, Speicherleiste und Systembänder nach Ansatz B (23aa504e)
- **ui**: Verschlüsselung als Auswahl mit Warnung bei Klartext, Absender mit Anzeigenamen, Doku (e1c69a85)
- **ui**: Zustände am Server als Band im Kopf (8cb58935)
- **ui**: eine Designrichtung – hell wie „Ruhe“, dunkel wie „Leitstand“ (02f9af95)
- **ui**: gemeinsame Eingabeprüfung mit Grenzen wie im Backend (ce5c5a6d)
- **ui**: gemeinsame Prüfhelfer, Zeichengrenzen in Passwort-, Cron- und Suchfeldern (330d9c73)
- **ui**: lcm-notice für feste Meldungen, Fehler über dem Fensterfuß (53cd807b)
- **ui**: neue Einträge in Live-Listen blenden ein, aktive Serverzeile wieder markiert (7be03d87)
- **ui**: schmale Serverliste mit Ampel und OS-Logo ohne Überschrift, Logo vor dem Namen (915fa924)
- **ui**: öffentliche Aktivierung für LCM-Konto und Linux-Benutzer, Passwort zurücksetzen (bb53188f)
- **ui**: „+2 weitere“ auf der Containerseite klappt alle Server auf (0f556601)
- **update**: Hinweise zur neuen Version als Fenster nach dem Update (4bf693f6)
- Binary liefert die neue Oberfläche unter strenger CSP aus (acdcdaa8)

### 🐛 Bugfixes

- **anmeldung**: Prüfung nachgezogen, API-Referenz ohne Bearer-Login, Hinweis zur Sitzungsdauer korrigiert (8f9a2330)
- **api**: Jobverlauf filtert nach allen Arten, die die Oberfläche anbietet (SSH härten, Onboarding, ACL, Rechte-Abgleich) (9c3cd9b5)
- **api**: Längen-, Zeichen- und Bereichsprüfung für Einstellungen, Bausteine, Alarmregeln, Anmeldung und E-Mail-Kanal (d1328390)
- **api**: Zugriffsprotokoll zeigt den Status von API-Fehlern richtig an (34934ab6)
- **api**: doppelte Namen von Gruppen, Eigenen Aktionen und Kanälen als 409 statt 500, Auffangnetz für Eindeutigkeitsfehler (c0b0838f)
- **firewall**: Bemerkung, Quellen und Regelzahl begrenzen statt still zu kürzen (74d3ab41)
- **helper**: Portwechsel im eingeschränkten Modus behält im Übergang Port 22 (bd93b623)
- **i18n**: Übersetzungen für auth.csrf_invalid und auth.language_invalid (c7dc520f)
- **lcm-host**: Trivy-Einrichtung lässt sich wiederholen (d8944bbb)
- **pakete**: Paketnamen behalten ihre Schreibweise, Trivy-Datenbank gleich nach der Einrichtung laden (137576d4)
- **profiles**: Regelbausteine gehen beim Speichern eines Profils nicht mehr verloren (d62a4eb7)
- **server**: Update-Prüfung meldet Fehler, Ziel-Bereinigung auch eingeschränkt, EPEL für fail2ban, Adressen ohne Container-Netze (80850db7)
- **settings**: ungültiger CVE-Zeitplan ist ein Eingabefehler, kein 500 (dd3106cb)
- **sicherheit**: fail2ban-Einrichtung meldet keinen Erfolg, wenn das Paket fehlt (2ad25168)
- **ssh**: Rechte einschränken auch auf sudo-rs (Ubuntu 26.04), Rücknahme räumt die Temp-Datei weg (3b87060d)
- **ssh**: Root-Login und Portwechsel reißen socket-aktivierten sshd nicht mehr ab (195428a6)
- **ui**: Barrierefreiheit ohne Ausnahmen: Name enthält sichtbaren Text, Überschriften ohne Sprung, Kontrast im dunklen Thema (b3d18fb6)
- **ui**: Dashboard verlinkt Jobs nur mit jobs:read (4a86554d)
- **ui**: Doku leitet beim Wechsel ins Hauptmenü nicht mehr zurück (b6a5de2a)
- **ui**: Eingeklappte Erklärspalte steht sofort eingeklappt da (8af24478)
- **ui**: Erklärspalten unter der Reiterleiste, Kopf und Reiter springen nicht mehr (e4501c58)
- **ui**: Erklärung im Aktionen-Menü ohne style-Attribut, das die CSP verwirft (34f97085)
- **ui**: Escape in der Auswahlliste schließt nicht mehr das ganze Fenster (7c20baf3)
- **ui**: Frühwarnung mit lokaler Kopie ohne Spiegellauf sagt „noch nie gespiegelt“ statt „ausgeschaltet“ (d92200c7)
- **ui**: Gruppenliste kompakt mit Ampel, Doppelpunkt in der Erklärspalte (2c648c8c)
- **ui**: HTTPS-Umstellung nur auf APT-Servern, Erklärung am Einrichten-Knopf der Sicherheits-Tools (95219e65)
- **ui**: Hinweise über offenen Fenstern, gesperrter Hauptknopf erkennbar, Fenster ohne Fokusrahmen beim Öffnen (208265a4)
- **ui**: Jobende wird auch gemeldet, wenn der Job vor dem Abonnement fertig war (2f2f2603)
- **ui**: Karte Betriebsfähigkeit nennt Prozess und PID der Sperre und verlinkt den Job nur mit jobs:read (f0867c38)
- **ui**: Karte Betriebsfähigkeit ohne Trennlinien zwischen den Zeilen (2365465c)
- **ui**: Liste der Auswahl mit Tippfilter bleibt beim Scrollen offen und folgt dem Feld (af4a476e)
- **ui**: Löschen von Allowlists und CrowdSec-LAPIs und Testversand an Kanäle fanden ihren Eintrag nicht (Zahl gegen Text verglichen) (2de554f7)
- **ui**: Meldungs-Demo hebt bei zwei Ereignissen die richtige Regel hervor (1b30af4b)
- **ui**: Passwortprüfung nennt den Grund und die Stärkeanzeige folgt der Regel des Servers (edf0abdb)
- **ui**: Pos1 und Ende springen im Hauptmenü zum ersten und letzten Eintrag (f1e54a2d)
- **ui**: Regeln je Zeitplan mit Blase, Apps-Schalter in der Namensspalte, Pfeiltasten auch in eingebetteten Shadow DOMs (a4bdc112)
- **ui**: Reiter Jobs und „Job ansehen“ in Meldungen nur mit jobs:read (7ecbffd5)
- **ui**: Server und Art in der Jobtabelle anzeigen (9595183e)
- **ui**: Serverkopf und Aktionen nach Rückmeldung (2b9c857d)
- **ui**: Serverliste und Pakete-Reiter ändern die Adresse außerhalb des Renderns (7340666b)
- **ui**: Suche im Reiter Pakete filtert schon beim Tippen (76691ec6)
- **ui**: Suchfelder in Serverliste, Paketen und Gruppenzuordnung über die volle Breite; doppelte Übersetzungen nach Zusammenführung entfernt (e972df7f)
- **ui**: Tabellen ohne leere Spaltenköpfe, im Spaltenwähler stand bisher ein leerer Eintrag (af44592c)
- **ui**: Tabellenzellen auf Sicherheit und Container zeigen Links und gedämpften Text richtig (ea2e5bc2)
- **ui**: Tabellenzellen der Betriebsreiter schmaler und lesbar; Parität ergänzt (a424687b)
- **ui**: Tabellenzellen mit Luft über und unter zweizeiligem Inhalt (df83d2e7)
- **ui**: Terminal nur, wo es eine Konsole gibt, Scan-Fehler sichtbar, Kernel-Aufräumen nur auf apt, schmale Serverliste (83d63b2b)
- **ui**: Terminal unter strenger CSP mit Schrift, Farben und Cursor, sauberes Wiederverbinden (0441b49f)
- **ui**: Toast weicht der Speicherleiste aus und verdeckt den Speichern-Knopf nicht mehr (2f2851d8)
- **ui**: Updates-Spalte zeigt auch 0 als graue Marke (c545011a)
- **ui**: Warnung zu Alarmregeln ohne Kanal im Singular und mit Test auf der Seite (649e3a5a)
- **ui**: Zahlenspalten, Familienname und Abstände auf der Linux-Benutzer-Seite (c3d16c1c)
- **ui**: gespeicherte Befunde bleiben sichtbar, wenn der Scanner fehlt (b0e2b5d3)
- **ui**: lange CVE- und Meldungstitel kürzen statt in die Nachbarspalte zu laufen (cb75e80b)
- **ui**: tolerierter Ausfall in der Serverliste auch im Dunkeln erkennbar (c6ebe69e)
- **ui**: „Rechte einschränken“ sagt wieder, was danach gilt, wogegen es nicht schützt und dass es nicht per Klick rückgängig geht (9349642b)
- Befunde aus der Abschlussprüfung (d2e345b6)

### ♻️ Refactoring

- **ui**: CSS aufgeräumt, Dialoge und Hilfsklassen an einer Stelle (bb2a19d5)
- **ui**: Hinweisblöcke der übrigen Seiten auf lcm-notice (b9279b83)
- **ui**: Rückfrage beim Seitenwechsel läuft über das Register unsaved, nur bei Benutzeraktionen (6a4cfcdb)
- **ui**: Seitenschubladen durch mittige Fenster ersetzt (f29750f6)
- **ui**: lange Funktionen geteilt, Tabelle typisiert, Reste entfernt (86d4919f)
- **ui**: ungenutzte Stile der Einstellungsbausteine entfernt (4e8f9ce4)

### 🔧 Sonstiges

- **ci**: CI, Docker und Doku auf die neue Oberfläche in ui/ umstellen (214e525a)
- **commits**: von Git erzeugten Merge-Betreff zulassen (301f7685)
- **deps**: update dependency svelte to v5.57.3 (db84ef21)
- **e2e**: Benutzer, Speicher, Apps, Jobs und Einstellungen am Server (3a8bd41a)
- **e2e**: Container, Doku, Jobs, Einstellungen (Allgemein, Zeit, Abo) und Rahmen (68b236b6)
- **e2e**: Einstellungen: Paketquellen, Caches, Allowlists, Aktionen, Anwendungen, Schlüssel (1ce8fb31)
- **e2e**: Linux-Benutzer, Einstellungen-Benutzer und Servergruppen (f3747d4f)
- **e2e**: Pakete, Pins, Kernel, Sicherheit, Firewall und Bestätigungen am Server (767bb21a)
- **e2e**: Playwright-Suite für die neue Oberfläche: Grundlage, Anmeldung, Dashboard, Serverdetail, Sicherheit, Alarme (e68d1b9a)
- **e2e**: Regelbausteine und Berechtigungsprofile (323955ae)
- **e2e**: Tastatur, Barrierefreiheit, Rückfrage bei ungespeicherten Änderungen, CSP und Feldfehler der API (fdf9d9f6)
- **e2e**: Wartebedingungen in Gruppen- und Paletten-Test, damit sie unter Last nicht wackeln (6297637f)
- **intern**: Abschnitt E2E-Übertragung: Befunde, offene Punkte und gestrichene Tests (a21076ea)
- **intern**: Commit-Verweise auf den bereinigten Stand (8b54f425)
- **intern**: E2E-Übertragung: erledigte Lücken aus „nicht entschieden“ nach „Behoben“ (36814aa4)
- **intern**: Ergebnis des Funktionstests auf lcm-test festhalten (ee4cbe80)
- **intern**: Nachträge zum Funktionstest und Entscheidungen festhalten (055044e5)
- **intern**: Plan, Fahrplan und Backend-Probleme auf die zwei Anmeldewege gebracht (b6e447a9)
- **plan**: Designentscheidung für Release B festgehalten (6f5da6a3)
- **plan**: Fahrplan mit Releases A, B, C (b381faf3)
- **ui**: Doku-Test rechnet mit der Weiterleitung auf die erste Seite (3d5e4477)
- **ui**: E2E-Suite auf der Cookie-Variante, neue Spec für beide Wege der Anmeldung (f95c96f9)
- **ui**: Katalog und Konzept auf die umgesetzten Meldungen (776fbfce)
- **ui**: Konzept für einheitliche Meldungen mit drei Ansätzen im Katalog (09822752)
- **ui**: Pins, Versionen, Paketquellen und Servereinstellungen; Parität nachgetragen (dd83f16c)
- **ui**: Regeln für Seiten, Dev-Ports per Umgebung einstellbar (1ba5f56f)
- **ui**: Reihenfolge der Karten unter Allgemein nachgezogen (ce01c971)
- **ui**: altes Svelte-Frontend entfernen (bded09bb)
- **ui**: leere Übersetzungsblöcke nach Zusammenführung entfernt (dfee1ca1)
- **ui**: speichere() wartet auf die letzte Meldung, wenn zwei Meldungen zugleich stehen (8adc686d)
- Altlasten aus dem Neubau entfernt (ac660e4b)
- Backend-Probleme aus Dashboard und Jobs (a3163649)
- Backend-Probleme aus Ereigniskanal und Befehlsvorschau (1f9fdbef)
- Backend-Probleme aus den Einstellungen System und Zugang (eef34401)
- Backend-Probleme aus der Gruppen-Seite (7aa71d3e)
- Backend-Probleme aus der Serverseite (1b3aaed9)
- Backend-Probleme und Korrekturen aus Linux-Benutzer und Protokoll (63111482)
- Browser-Anmeldung in zwei Varianten, REST-API nur noch mit X-API-Key (49ee3839)
- Eingabeprüfung und Audit-Befunde, Firewall-Fenster größer, Altformat der Portliste begrenzt (17fee130)
- Handbuch auf die neue Oberfläche gebracht (f5ae10d7)
- Härtungsvorschläge ohne Verbindung und Demo-Jobs als offene Backend-Punkte (50c42451)
- Merge branch 'feat/b-account' into feat/frontend-neubau (05653bde)
- Parität der Server-Sicherheit nachgetragen; Abstände und Feldbreiten der Sicherheitskarten (01641976)
- Parität des Serverdetails und Backend-Korrekturen für den LCM-Host (a3c279b9)
- Paritätsabgleich der übrigen Seiten mit dem alten Frontend (cb1bffd2)
- Screenshots der neuen Oberfläche (7365798d)
- Status je Lücke im Paritätsabgleich der übrigen Seiten (75f499c9)
- englische API-Referenz wird wirklich veröffentlicht (79d86486)
- gofmt (9e5ccbda)

## v1.49.1-beta.1 - 2026-11-01

### 🐛 Bugfixes

- **ci**: Merge von community nach enterprise rollt das Paket aus, ohne Tag (42e317b3)

## v1.49.0-beta.1 - 2026-11-01

### 🚀 Features

- **status**: fehlgeschlagene Aktionen wirken auf den Serverstatus (c5b92409)

### 🐛 Bugfixes

- **status**: Regel-Fehler heilt nur der Health-Ping derselben Quelle (654b9a04)
- **status**: zypper wie apt auswerten, Netzwerk-Hinweis ohne APT-Bezug (5e0a89a9)

## v1.48.1 - 2026-10-31

### 🐛 Bugfixes

- **deps**: update go-abhängigkeiten (minor/patch) (68a27ff7)

### 🔧 Sonstiges

- **deps**: update aquasec/trivy docker tag to v0.76.1 (d34ffb8e)
- **deps**: update dependency @axe-core/playwright to v4.14.0 (cc6436de)
- **deps**: update dependency vite to v8.3.5 (91e83701)
- **deps**: update module golang.org/x/net to v0.62.0 [security] (4bb0da29)

## v1.48.0 - 2026-10-31

### 🚀 Features

- **apt**: Kernel-Updates mitnehmen, Zurückgehaltenes benennen und nicht abwerten (4e356391)
- **apt-cache**: mehrere APT-Caches, Auswahl in Regel und Aktion, Funktionstest (4db5b56e)
- **crowdsec**: mehrere zentrale LAPIs mit Bezeichnung (adac88a5)
- **gruppen**: Regeln je Auslöser als Karten, umsortierbar; Server und Verwaltungs-User einklappbar (d680d8bd)
- **paket**: apt purge entfernt alles, was LCM angelegt hat (bcaa6c68)
- **regeln**: Ausführungsreihenfolge je Auslöser festlegbar (5df6fc87)
- **regeln**: DNS-Test-Regel mit eigenen Domains (1184db83)
- **regeln**: Rechte-Soll und ACL einrichten auch per Zeitplan (23e6edbc)
- **schedules**: Zeitfenster statt fester Startzeit (51a4f78d)
- **storage**: Protokolle komprimieren, Routine-Ausgaben kurz aufbewahren, Datenbank verdichten (c0bfb472)
- Festplatten-Diagramm, Linux-Benutzer-Anleitung, Docker-Regeln wie Watchtower (8af16890)

### 🐛 Bugfixes

- **abgleich**: gesetztes Server-Passwort bleibt bei Synchronisation erhalten (69f378d5)
- **apt**: Sperren abwarten, verwaiste Updates beenden, Ausgabe live streamen (3dd2d309)
- **cli**: credentials init nimmt die Paketpfade, nicht das Binary-Verzeichnis (548a80a8)
- **crypto**: Rohschlüssel mit Leerzeichen am Rand nicht mehr verwerfen (08e66c9c)
- **deps**: source-map-js auf 1.2.2 gegen GHSA-68fv-2mgg-jv7q (07c8adb9)
- **docker**: Scan für Docker 26 und 29, Container-Regel wählt veraltete Container (c00d47d5)
- **gruppen**: Regel-Dialog für Firewall-Regeln breit genug (19d726fc)
- **konsole**: Sitzung schließt nach exit in der Shell (83e46c85)
- **konsole**: Sitzungen enden wirklich, blockieren LCM nicht und sind sichtbar (a3e989cd)
- **mail**: Klartext-Anmeldung ohne STARTTLS, STARTTLS-Haken wirkt (7c1df3d0)
- **ssh**: Härtungs-Status beim Aktualisieren effektiv messen (78b07974)
- **ui**: Web-Konsole wächst nicht mehr über den Bildschirm hinaus (fe596785)

### ⚡ Performance

- **dashboard**: Ampeln gesammelt laden, zwischenspeichern und den letzten Job per Index finden (e47f24f5)

### 🔧 Sonstiges

- **deps**: Go 1.27.2 und golang.org/x/net v0.60.0 gegen HTTP/2-Lücken (1c51d19d)
- **deps**: Go- und npm-Abhängigkeiten aktualisieren (5cd7f080)
- **deps**: update aquasec/trivy docker tag to v0.75.0 (d016a742)
- **deps**: update dependency @playwright/test to v1.64.1 (3926e7df)
- **e2e**: LAPI-Aufräumen wartet, bis jede Zeile gelöscht ist (7b179fb8)
- **e2e**: Rechte-Soll ist auch am Zeitplan wählbar; Script-Prüfung greift wieder (cd615b68)
- **e2e**: ⓘ-Popover nur öffnen, wenn es zu ist (b52dce3d)
- **executor**: Begründung der globalen Lauf-Schranke berichtigt (dd4b272a)
- **openspec**: OpenSpec für spec-getriebene Entwicklung einführen (d703663a)
- **plan**: Arbeitsliste der Iteration 2026-10-17 (969ebef1)
- **plan**: Iteration 2026-10-17 abgeschlossen (d7f9a707)
- **plan**: Nachtrag 2026-10-17 umgesetzt (a3781a17)
- **plan**: Stand der Iteration festgehalten (8d962dfb)
- **renovate**: Sicherheitsupdates nach grüner Pipeline automatisch mergen (b44611ed)
- **renovate**: täglich, mit Go-Toolchain, Node und CI-Images (512e3a15)
- Go-Cache vom Runner statt über MinIO (2424b569)
- LCM läuft auf Runner-Paar A (73e63b10)

## v1.42.3 - 2026-09-23

### 🚀 Features

- **backup**: Reste abgebrochener Sicherungsläufe selbst wegräumen (7b86dbd6)
- **security**: Empfehlungen der Sicherheitsanalyse umgesetzt - Helper statt NOPASSWD:ALL, Rechte-Wächter, Eingabegrenzen (2859da5f)
- **security**: Schlüssel aus dem Datenverzeichnis holen - Credentials, kein jwt_secret, Empfänger-Backups (29cdf536)
- **ui**: Barrierefreiheit und Tastaturbedienung - Kontraste, Screenreader, Tastenkürzel mit Hilfe (89e1fe4e)
- **ui**: Warnung in der Browser-Konsole gegen Self-XSS (4ce6fca0)

### 🐛 Bugfixes

- **onboarding**: fehlendes sudo selbst nachinstallieren statt die Aufnahme abzubrechen (b34e78e7)
- **packaging**: Upgrade hebt die Einschränkung des LCM-Hosts nicht mehr auf (73d625ff)
- **security**: Sicherheitsanalyse Runde 1 - Sichtbereich, Proxy-Vertrauen, Re-Auth, Einmal-TOTP (abb21d57)
- **ui**: Tastenkürzel gelten nur in der angemeldeten Oberfläche (9e8ee0f9)
- **ui**: abgelaufene Sitzung nicht erst zum Server schicken (fba23c86)

### 🔧 Sonstiges

- **release**: Versionsableitung an die eigene Linie binden (33d7d381)
- versehentlich eingechecktes Binary "release" entfernen (7053cc37)

## v1.38.0 - 2026-09-08

### 🚀 Features

- **security**: Post-Quanten-Zustand der SSH-Verbindung erfassen und zeigen (8e5365af)

### 🔧 Sonstiges

- **deps**: Go 1.27.1, sqlite 1.58.0, landlock 0.10.0 (a048859f)

## v1.37.0 - 2026-09-07

### 🚀 Features

- **docs**: App-Seite "Was LCM auf dem Server ändert" samt Rückbau-Anleitung (2cf2e176)

### 🐛 Bugfixes

- **appdocs**: Auszeichnungen über den Zeilenumbruch hinweg erkennen (4856863f)

### 🔧 Sonstiges

- **release**: Anleitung im Release-Skript auf den tatsächlichen Weg bringen (3a161a9a)

## v1.36.0 - 2026-09-07

### 🚀 Features

- **console**: Konsole als Schaltfläche oben, je Server abschaltbar (0c7da281)
- **lcm**: Speicher-Überwachung, Hardware-Erkennung, Job-Warteschlange, Web-Terminal, Verschlüsselungs-Fix (d0c7db30)
- **servers**: Server umbenennen und den Hostname erfassen (b86f08f2)

### 🐛 Bugfixes

- **deps**: x/crypto auf v0.56.0 - zwei DoS-Lücken in ssh behoben (f965a9ef)
- **errors**: Substantive in den Fehlermeldungen großschreiben (19ad31b5)
- **testing**: MD-Reparatur wartet auf den Zielzustand, nicht auf ein Resync-Ende (3216ba27)

### 🔧 Sonstiges

- **storage**: Gegenprobe zur Speicher-Diagnose - behobene Defekte müssen verschwinden (1b188dd5)
- Go- und Node-Image auf feste Tags (8d8493c0)
- Kampagnen-Parameter an Links auf eigene Seiten (17e8875b)
- Prüfjobs nicht doppelt auf den Release-Branches laufen lassen (c252d69b)
- enterprise wieder mit vollem Testlauf (fdefb7d4)

## v1.34.2 - 2026-08-29

### 🐛 Bugfixes

- **ci**: pipeline-riegel kennt den neuen internen projektpfad (009b7892)

### ♻️ Refactoring

- **repo**: oeffentlicher spiegel heisst jetzt techeve/lcm (41d69c28)

### 🔧 Sonstiges

- **logo**: neutrale wortmarke als fallback fuer gitlab und docker hub (7349007f)

## v1.34.1 - 2026-08-29

### 🔧 Sonstiges

- **readme**: live-demo im kopfbereich (en/de) (f445dcfd)

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
