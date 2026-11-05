# Release C: Zentrale Update-Erkennung, Kerndienste und CVE-gesteuertes Patchen

Stand: 31.10.2026 · Status: abgestimmter Entwurf · Teil des Fahrplans
(`doc/plan-2026-10-31-fahrplan.md`)

## Ziel

**Gefährliche Sicherheitslücken schneller schließen.** Dafür:
1. LCM erkennt neue Paketversionen **selbst** direkt bei den Paketquellen,
   in Minuten statt beim nächsten nächtlichen Scan.
2. LCM weiß, welche Pakete auf einem Server wirklich zählen: **Kerndienste**,
   **basis-kritische Pakete** und **Pakete mit offenen Ports**.
3. Fixes für hochkritische CVEs an diesen Paketen werden **unverzüglich**
   eingespielt, nach Richtlinie, mit Zeitfenstern, Freigaben, Health-Checks
   und gestaffelt im Verbund.
4. Der Serverstatus zeigt **rot nur noch für relevante Pakete**.

Alle Teile gehören in **ein** Release. Die Oberfläche entsteht im neuen
Frontend aus Release B. Release A liefert die Betriebsfähigkeit je
Aktionsart, auf der die Patch-Zustände aufbauen.

## Leistungsziele

Leistung ist der Kern dieses Releases: Jede Minute zwischen Fix und Einspielung
ist eine Minute offene Lücke.

| Strecke | Ziel |
|---|---|
| Fix erscheint in einer Security-Quelle → LCM kennt ihn | ≤ 10 min |
| Fix bekannt → betroffene Server bestimmt (1.000 Server) | < 5 s |
| neue CVE-Daten → Bewertung der betroffenen Pakete | < 30 s |
| Stufe „Sofort“: Fix bekannt → Patch startet | < 1 min |
| Paketänderung auf dem Server → LCM weiß davon | sofort mit Agent, ≤ 5 min per SSH-Leichtcheck |
| Patch auf 50 unabhängigen Servern | parallel, begrenzt nur durch Parallelitätsgrenze und APT-Cache |

Die Kennzahl **„Zeit vom Erscheinen des Fixes bis zur Einspielung“** wird je
Server, Gruppe und Flotte gemessen und angezeigt.

---

## 1. Zentraler Repository-Abgleich

### 1.1 Prinzip

- Der Server meldet: installierte Pakete, Paketquellen, Pinning, gehaltene
  Pakete, Architektur(en), `os-release`.
- LCM fragt die Paketquellen selbst ab und berechnet je Server die
  Kandidatenversionen.
- Die Prüfung auf dem Server (`apt list --upgradable` usw.) läuft weiter, aber
  nur noch täglich, als **Kontrolle**. Abweichungen zwischen zentraler Sicht und
  Server-Sicht werden je Quelle gezählt. Dauerhafte Abweichung erzeugt eine
  interne Warnung (Fehler in der Berechnung).

### 1.2 Quellen-Register nach Bedarf

- Aus den Quellen aller registrierten Server entsteht ein Register eindeutiger
  **Index-Quellen** `(Paketverwaltung, URL, Suite, Komponente, Architektur)`.
  Sind nur Ubuntu-24.04-Server registriert, werden nur `noble`,
  `noble-updates` und `noble-security` (amd64) abgefragt.
- Referenzzählung: Fällt der letzte Server einer Quelle weg, endet die Abfrage,
  die Daten werden nach einer Frist gelöscht.
- Woher die Quellen kommen:
  1. Distribution (aus `os-release` und den Quelldateien des Servers, `.list`
     und deb822-`.sources`),
  2. Katalog `KnownRepo` (Docker, PGDG, Grafana, HashiCorp …),
  3. beim Scan gefundene Sonderquellen (PPAs, Hersteller-Repos). Sie werden
     automatisch überwacht, wenn sie öffentlich und signiert erreichbar sind.
- Quellen, die LCM nicht selbst abfragen kann (mit Zugangsdaten wie Ubuntu
  Pro/ESM, nicht erreichbar, unsigniert), bekommen den Status **„nur
  Server-Prüfung“**. Für sie gilt weiter das Ergebnis vom Server.
- Häufig gefundene Sonderquellen erscheinen als Vorschlag für den Katalog.

### 1.3 Abruf (apt)

- `InRelease` per bedingtem GET (`If-Modified-Since`/ETag). Nur bei Änderung
  wird `Packages.xz` über `by-hash` geholt.
- **Signatur von `InRelease` wird immer geprüft** (Abschnitt 8).
- Intervalle, einstellbar: Security-Suites 5 min, Updates-Suites 30 min,
  Drittquellen 60 min. Backoff bei Fehlern, verteilte Startzeiten, eigener
  User-Agent.
- Security-Suites direkt beim Ursprung (`security.debian.org`,
  `security.ubuntu.com`), nicht über nachlaufende Mirrors.
- Der vollständige Index liegt komprimiert im Dateicache. In die Datenbank
  kommen nur Einträge für Paketnamen, die auf mindestens einem Server mit dieser
  Quelle installiert sind: Version, Quellpaket (`Source:`), Architektur,
  `Phased-Update-Percentage`, **erstmals gesehen am**.

### 1.4 Delta-Verarbeitung

- Bei einem neuen Index werden nur die **geänderten Einträge** gegenüber dem
  vorherigen Stand ermittelt (Version neu oder anders).
- Nur diese Änderungen lösen die Kandidatenberechnung aus, und zwar nur für die
  Server, die das Paket aus dieser Quelle installiert haben.
- Dafür hält LCM einen **umgekehrten Index im Speicher**:
  `(Index-Quelle, Paketname) → Server`. Er wird bei Änderungen des
  Paketbestands eines Servers inkrementell gepflegt, nicht neu aufgebaut.

### 1.5 Kandidatenberechnung

- Versionsvergleich mit erprobter Bibliothek (Debian: Epoch, `~`; RPM:
  rpmvercmp). Version beim Einbau aus der Registry prüfen.
- Berücksichtigt werden: Pin-Prioritäten (`preferences.d`), `apt-mark hold`,
  `NotAutomatic`/`ButAutomaticUpgrades` (Backports), Multiarch, Komponenten.
  Ubuntu-Phasing wird als „gestaffelt freigegeben“ gekennzeichnet, nicht als
  überfällig.
- Neue Felder am `Package`: `CandidateSource` (`central`/`server`),
  `CandidateSeenAt` (Grundlage der Zeit-bis-zum-Patch-Kennzahl).
- Vor jeder Ausführung auf dem Server: `apt-get update`, dann Simulation
  (`apt-get -s`, gibt es schon in `services/aptheld.go`). Die Simulation liefert
  das genaue Ergebnis, auch neue Abhängigkeiten wie ein neues Kernel-ABI-Paket.

### 1.6 Änderungserkennung auf dem Server

- **SSH-Leichtcheck alle 5 min:** Änderungszeit und Hash von
  `/var/lib/dpkg/status` bzw. der rpm-Datenbank. Nur bei Änderung wird der
  Paketbestand neu eingelesen. Damit fallen auch manuelle Updates sofort auf.
- **Agent:** `DPkg::Post-Invoke`-Hook bzw. dnf-Plugin meldet Änderungen sofort.
  `wire.Inventory` wird dafür um Paketänderungen erweitert.
- Die Port-Erkennung (2.1) läuft bei jedem Neueinlesen mit.
- Täglicher Vollscan bleibt als Abgleich.

### 1.7 Weitere Paketverwaltungen

| Familie | Distributionen | Index | Besonderheit |
|---|---|---|---|
| apt | Debian, Ubuntu, Proxmox | `InRelease` → `Packages` | zuerst |
| dnf/yum | RHEL, Rocky, Alma, Fedora | `repomd.xml` → `primary.xml` | `updateinfo.xml` liefert Security-Advisories mit CVE und Schwere; Mirrorlist/Metalink; AppStream-Module |
| zypper | openSUSE, SLES | `repomd.xml` + `updateinfo` | SLES braucht Zugangsdaten → „nur Server-Prüfung“ |
| pacman, apk | Arch, Alpine | – | weiter Prüfung auf dem Server |

Die Abrufschicht ist je Familie austauschbar gebaut.

---

## 2. Relevanz: Kerndienste, Basis, offene Ports

### 2.1 Begriffe

- **Kerndienst:** benannte Funktion eines Servers (z. B. „Webserver“,
  „PostgreSQL“), bestehend aus Paketen und ggf. erkannten Anwendungen
  (`DetectedApp`, nicht über die Paketverwaltung installiert). Ist eine Instanz
  eines Dienstprofils (2.4).
- **Basis-kritische Pakete:** gelten auf jedem Server. Startliste, pflegbar:
  `openssh-server`, `openssl`/`libssl*`, `libc6`/`glibc`, `sudo`, `systemd`,
  `polkit`, Kernel (`linux-image-*`), `apt`/`dpkg`/`rpm`, `ca-certificates`,
  `xz-utils`/`liblzma*`.
- **Port-Pakete:** alle Pakete, deren Prozesse auf einem nicht-lokalen Port
  lauschen. Die Erkennung gibt es schon (`ListeningPackages`,
  `server_scan.go:351`: `ss` → PID → Binary → `dpkg -S`/`rpm -qf`). Port-Pakete
  zählen **automatisch, ohne Bestätigung**. Das erfasst auch Hintergrunddienste,
  die eine Anwendung als Abhängigkeit mitbringt und die man nicht auf dem Schirm
  hat. Lauschende Prozesse ohne Paket (`UnknownApp`) werden ebenfalls als
  relevant angezeigt, mit Versionsprüfung über die Anwendungserkennung.
- **Relevante Pakete** = Kerndienste ∪ Basis ∪ Port-Pakete, jeweils mit ihren
  zugehörigen Paketen (2.2).
  - Nur relevante Pakete können die rote Warnstufe auslösen (4.3).
  - Automatisch nach Richtlinie gepatcht werden **bestätigte Kerndienste** und
    **Basis-Pakete**. Port-Pakete werden als Kerndienst vorgeschlagen.
- Die Hochgewichtungs-Liste und die Stufen-Anhebung in `cveweight.go` werden
  durch dieses Modell ersetzt.

### 2.2 Zugehörige Pakete

In absteigender Genauigkeit:
1. **gleiches Quellpaket** (`nginx` → `nginx-common`, `nginx-core`), aus dem
   Index,
2. **tatsächlich geladene Bibliotheken:** `/proc/<pid>/maps` der
   Dienstprozesse, per `dpkg -S`/`rpm -qf` den Paketen zugeordnet (so zählt
   `libssl3` zum Webserver),
3. Rückfall: direkte `Depends`, eine Ebene, nur Bibliotheken.

Zugehörige Pakete erben die Einstufung ihres Kerndiensts.

### 2.3 Erkennung und Vorschläge

Signale, nach Stärke gewichtet. Jedes wird als Begründung angezeigt:
1. **Port-Paket** ohne Kerndienst (stärkstes Signal), z. B. „redis-server
   lauscht auf 0.0.0.0:6379, installiert als Abhängigkeit von …“,
2. laufende systemd-Units, die einem Paket gehören,
3. Treffer der Anwendungserkennung,
4. Hostname-Muster als Verstärker: `web|www|nginx|apache|proxy|lb` →
   Webserver/Proxy, `db|sql|pg|maria|mongo` → Datenbank, `mail|mx|smtp` →
   Mail, `dns|ns\d` → DNS, `git|ci|runner`, `vpn` …,
5. Regeln aus Gruppe bzw. global (2.5).

Vorschläge werden bestätigt oder verworfen. Verworfene werden gemerkt. Je
Gruppe einstellbar: „Vorschläge automatisch übernehmen“.

### 2.4 Dienstprofile

- **Dienstprofil** = wiederverwendbare Definition eines Kerndiensts:
  Paketmuster (`nginx`, `nginx-*`), optional App-Slug, Erkennungsmerkmale (Port,
  Unit, Hostname-Muster), Health-Check-Vorlagen (3.4), Richtlinien-Vorgabe.
- Mitgeliefert und pflegbar: nginx, Apache, HAProxy, PostgreSQL,
  MariaDB/MySQL, Redis, Postfix, Dovecot, Bind, PowerDNS, Docker-Host.
- Eingebautes Profil **„Basis“** mit den basis-kritischen Paketen, hängt an
  jedem Server.
- **Gruppen verweisen auf Profile, nicht auf Pakete.**
- Am **Server** ist ein Kerndienst eine Profil-Instanz: von der Gruppe
  geerbt, erkannt oder manuell. Abweichungen (Port, URL, Stufe) werden am
  Server überschrieben.

### 2.5 Manuelle Pflege und Regeln

- Kerndienste je Server anlegen und ändern.
- Regeln global oder je Gruppe, z. B. „Hostname passt auf `^web` → Profil
  Webserver“, „Paket `postgresql-*` installiert → Profil PostgreSQL“.

### 2.6 Sonderbehandlung

- **openssh-server:** Nach dem Update baut LCM sofort eine neue Verbindung auf.
  Schlägt das fehl: dringender Alarm, die offene Sitzung bleibt bestehen,
  Rückweg auf die vorherige Version. Nie zusammen mit einem Neustart in einem
  Schritt. Wo der Agent läuft, ist er der zweite Kanal.
- **Bibliotheken (openssl, glibc):** Ein Fix wirkt erst nach Neustart der
  Prozesse, die die alte Bibliothek geladen haben. Nach dem Update werden sie
  ermittelt (`needrestart -b` bzw. gelöschte Bibliotheken in `/proc/*/maps`) und
  die Dienste nach Richtlinie neu gestartet.
- **Kernel:** Kernel-Pakete sind basis-kritisch und werden **immer**
  mitbewertet.
  - Bewertet wird gegen den **laufenden** Kernel (`uname -r`), nicht nur gegen
    das installierte Paket. Ein installierter, aber noch nicht gebooteter Fix
    schließt die Lücke nicht.
  - Das Kernel-Paket wird nach der Stufe der Richtlinie installiert (bei
    „Sofort“ sofort).
  - Der **Neustart** erfolgt nur im Neustart-Zeitfenster der Richtlinie oder
    auf Anforderung in der Oberfläche, auch bei Stufe „Sofort“. Ein ungeplanter
    Reboot trifft alle Dienste des Servers.
  - Gestartet wird über den koordinierten Neustart (3.5), damit abhängige
    Server nicht gleichzeitig neu starten.
  - Bis zum Neustart bleibt der CVE rot, mit Info „Kernel-Fix installiert,
    Neustart geplant um …“ bzw. „Neustart ausstehend“.

---

## 3. Gruppen: Patch-Richtlinie, Verbundgruppen, Health-Checks

### 3.1 Reiter „Dienste & Patchen“

Die Gruppenseite bekommt neben **„Regeln“** (Zeitpläne und Grundsatz-Regeln,
unverändert) einen eigenen Reiter **„Dienste & Patchen“**. Regeln haben eine
Reihenfolge und laufen nach Uhrzeit; die Patch-Richtlinie reagiert auf
Ereignisse und besteht aus Einzelwerten. Darum getrennt.

Aufbau:
1. **Zugewiesene Profile**, dazu „Alle Kerndienste“ als Gruppen-Standard.
2. Je Eintrag: Stufe, Zeitfenster, Höchstwartezeit, Neustart-Zeitfenster,
   Health-Checks. Jedes Feld darf leer sein und heißt dann „nicht festgelegt“.
3. Bei Verbundgruppen zusätzlich die Verbund-Einstellungen (3.3).

Bedienung:
- Leer, bis man ihn braucht, mit Vorschlag („3 Kerndienste erkannt,
  übernehmen?“).
- **Vorlagen** für die ganze Richtlinie: „Sicherheit zuerst“ (Sofort), „Im
  Wartungsfenster“, „Nur mit Bestätigung“.
- **Gruppenansicht je Profil zusammengefasst**, z. B. „nginx · 12 Server ·
  10 aktuell · 2 mit kritischem CVE, Fix um 14:00“. Aufklappen zeigt die
  Server, Pakete erst am Server.
- **Drift-Hinweis**, wenn ein zugewiesenes Profil auf einem Mitglied fehlt
  oder dort zusätzlich ein erkannter Dienst läuft.

### 3.2 Auflösung je Feld

Für Server S, Profil P und Feld F:
1. Server-Überschreibung, falls gesetzt,
2. sonst alle Gruppen von S, die F für P oder für „Alle Kerndienste“ festlegen.
   Es gewinnt der stärkste Vorrang (kleinste Zahl, wie bei den Regeln).
   Innerhalb derselben Gruppe schlägt der Profileintrag den Gruppen-Standard,
3. sonst der globale Standard.

Aufgelöst wird **je Feld, nicht je Block**. Legt „Kunde X“ nur das Zeitfenster
fest und „Web“ nur die Stufe, gelten beide. Ein Konflikt entsteht nur, wenn zwei
Gruppen dasselbe Feld verschieden festlegen. Ausnahme: Verbund-Budgets gelten
alle gleichzeitig (3.3).

**Ein** Resolver liefert je (Server, Profil, Feld) den Wert, die Herkunft und
die überstimmten Werte. Die Ansicht „Wirksame Einstellungen“, die Prüfung und
die Ausführung nutzen denselben Resolver.

### 3.3 Verbundgruppen

Ein Verbund ist eine **als Verbund gekennzeichnete Gruppe**. Gruppen, die keine
Verbünde sind, bleiben normale Gruppen.

- **Kennzeichnung:** Gruppenart „Verbund“. Abzeichen in der Gruppenliste und an
  jedem Mitglied („Verbund web (5)“).
- **Bedeutung:** Die Mitglieder sind austauschbare Knoten. Der Verbund schützt
  den **Server**: Jede störende Aktion des Patch-Wegs (Patch mit
  Dienst-Neustart, Kernel-Neustart) nimmt den Knoten aus dem Dienst und wird
  gestaffelt. Für die regulären Zeitplan-Regeln gilt die Staffelung nicht (4.5).
- **Verbund-Einstellungen:**
  - höchstens N Server (oder %) gleichzeitig außer Dienst, Standard 1,
  - Mindestanzahl gesunder Server vor Beginn (fehlt schon einer, wird
    gemeldet statt gepatcht),
  - Reihenfolge: erster Server fest oder automatisch, Wartezeit zwischen den
    Servern,
  - Health-Check **nach** dem Update rot: melden und Durchlauf stoppen
    (Standard) / zusätzlich auf die vorherige Version zurück,
  - Health-Check schon **vor** dem Update rot: trotzdem patchen (Standard) /
    anhalten und melden,
  - Load Balancer: ZPO-Anbindung, Zuordnung der Mitglieder zu Adresse/Port
    (Vorschlag aus Server-IPs und Profil-Ports). Siehe
    `ZPO/doc/plan-lcm-wartungsdrain.md`.
- **Rolle und Reihenfolge je Mitglied:** zwei Spalten an der Mitgliedschaft
  (primär/sekundär, Position). Sekundäre zuerst, primär zuletzt.
- **Server in mehreren Verbünden:** Ein Knoten wird nur herausgenommen, wenn
  **jeder** seiner Verbünde das zulässt.
- **Vorschläge:** Verbundgruppe anlegen (gleiches Profil, gleicher Port,
  Namensreihe `web01…web05`); bestehende gleichförmige Gruppe als Verbund
  kennzeichnen; neuen passenden Server aufnehmen.

### 3.4 Health-Checks

- **Typen:** TCP-Port, HTTP(S) (URL, erwarteter Status, optional Text,
  Zertifikat gültig), systemd-Unit aktiv, eigener Befehl (Exit 0).
- **Ort:** von LCM aus oder auf dem Server per SSH gegen localhost.
- **Vorlagen im Profil** mit Platzhaltern (`{host}`, `{port}`), Überschreibung
  am Server. Ohne eigenen Check gilt: Unit aktiv und bekannte Ports lauschen
  wieder.
- **Ablauf:** vor dem Update (Grundzustand) und danach mit Wiederholungen
  (z. B. 6 × 10 s). Verhalten bei Rot nach den Verbund-Einstellungen, für Server
  ohne Verbund nach Gruppe bzw. Server.

### 3.5 Koordinierter Neustart einer Gruppe

Für jede Gruppe gibt es die Aktion **„Gruppe nacheinander neu starten“**.
LCM koordiniert den Durchlauf selbst, damit voneinander abhängige Dienste nicht
gleichzeitig ausfallen.

- **Auslöser:** von Hand in der Oberfläche (Gruppe oder Auswahl in der
  Serverliste) oder automatisch im Neustart-Zeitfenster für alle Mitglieder mit
  ausstehendem Neustart (z. B. nach einem Kernel-Fix).
- **Verbundgruppe:** Es gelten die Verbund-Einstellungen (3.3): Budget,
  Mindestanzahl gesund, Rollen und Reihenfolge, Health-Checks, ZPO-Drain.
- **Normale Gruppe:** Reihenfolge und Anzahl gleichzeitig werden im Dialog
  gewählt (Standard: einer nach dem anderen, Reihenfolge per Ziehen oder nach
  Name).
- **Je Server:** Neustart, warten bis SSH bzw. Agent wieder antworten, laufender
  Kernel prüfen, Health-Checks. Erst dann folgt der nächste.
- **Bei Fehler:** Durchlauf stoppen, Alarm, restliche Server bleiben
  unangetastet.
- Fortschritt live über den Ereigniskanal. Jeder Durchlauf als `PatchRun` mit
  Schritten im Protokoll.

### 3.6 Konfigurationsprüfung

Läuft bei jedem Speichern (Gruppe, Mitgliedschaft, Server-Überschreibung,
Zeitfenster) und zeigt das Ergebnis mit den betroffenen Servern. Dazu eine
Gesamtliste unter Einstellungen und ein Hinweis am Server.

| Stufe | Fall |
|---|---|
| Fehler | gleicher Vorrang, gleiches Feld, verschiedene Werte |
| Fehler | „Sofort im Zeitfenster“ ohne Fenster, oder das Fenster öffnet nie |
| Warnung | „Nur mit Bestätigung“ wird von „Sofort“ einer stärkeren Gruppe überstimmt (hervorgehoben) |
| Warnung | Einstellung wirkt auf keinem Mitglied, weil sie überall überstimmt wird |
| Warnung | Verbundgruppe mit weniger als 2 Mitgliedern, oder „höchstens gleichzeitig“ ≥ Mitgliederzahl |
| Warnung | Server in mehreren Verbünden, deren Budgets zusammen keinen Knoten freigeben |
| Warnung | Verbundmitglied ohne das Profil der übrigen (Drift) |
| Hinweis | Profil zugewiesen, aber auf keinem Mitglied erkannt |
| Hinweis | Health-Check auf einen Port, der nicht lauscht |

Fehler aus der Eingabe selbst blockieren das Speichern. Entstehen sie später
(z. B. durch eine neue Mitgliedschaft), gewinnt bei gleichem Vorrang die ältere
Gruppe (wie in `services/enforce_priority.go`), und die Meldung bleibt rot, bis
sie behoben ist.

### 3.7 Datenmodell

- `ServerGroup` + `IsVerbund`; Verbund-Einstellungen als eigene Tabelle (1:1).
- Mitgliedschaft `server_group_servers` + `Role`, `Position`.
- `ServiceProfile`, `ServerService` (Kerndienst-Instanz, Status
  vorgeschlagen/bestätigt/verworfen, Begründung).
- `GroupServicePolicy (group_id, profile_id | null = alle, Felder nullable)`.
- `ServerServicePolicy (server_id, profile_id, Felder nullable)`.
- `MaintenanceWindow` (global, wiederverwendbar).
- `RepoSource` (Index-Quelle), `RepoPackage` (nur installierte Namen),
  `PatchRun` (Durchlauf mit Schritten je Server).

---

## 4. CVE-Bewertung und Patch-Richtlinie

### 4.1 Datenquellen

- Trivy und OSV/EUVD (bestehend).
- Debian Security Tracker (JSON: Dringlichkeit, behobene Version je Release),
  Ubuntu-Prioritäten (OSV/USN), RHEL/SUSE `updateinfo`, **CISA KEV** (aktiv
  ausgenutzt), **EPSS** (Ausnutzungswahrscheinlichkeit).
- Zuordnung CVE → Quellpaket → Binärpakete → Server über das Quellpaket aus dem
  Index.
- **Schnellweg für Debian/Ubuntu:** Die Tracker-Daten nennen je Quellpaket und
  Release die behobene Version. LCM vergleicht direkt mit dem Paketbestand und
  weiß sofort, ob ein Fix verfügbar ist, ohne einen Trivy-Lauf abzuwarten.
  Trivy läuft weiter für die vollständige Bewertung und die übrigen
  Distributionen, aber ereignisgesteuert nur für betroffene Server (5.2).

### 4.2 Hochkritisch

Standard, global und je Gruppe einstellbar:
- CVSS ≥ 9.0, **oder**
- in KEV gelistet, **oder**
- CVSS ≥ 7.0 **und** relevantes Paket.

Hochkritisch + relevantes Paket + Fix verfügbar → die Patch-Richtlinie greift
(nur für bestätigte Kerndienste und Basis-Pakete, 2.1).

### 4.3 Warnstufe

Die Relevanz entscheidet über die Farbe:

| Paket | Fund | Warnstufe |
|---|---|---|
| relevant | Fix verfügbar, CVSS ≥ 7.0 oder KEV | **rot** |
| relevant | Fix verfügbar, CVSS 4.0–6.9 | Info |
| relevant | **kein Fix**, CVSS ≥ 9.0 | **gelb** + Alarm `cve_unfixed_core` |
| nicht relevant | Fix verfügbar, CVSS ≥ 9.0 oder KEV | **gelb** |
| nicht relevant | sonst | Info (in der Sicherheitsübersicht gezählt) |

Zustände des Patch-Wegs:

| Zustand | Warnstufe |
|---|---|
| automatischer Patch fehlgeschlagen | rot (Kategorie `packages` aus Release A) |
| Health-Check nach dem Patch rot | rot |
| SSH nach openssh-Update nicht erreichbar | rot (Kategorie `connection`) |
| Fix wartet auf Freigabe | gelb |
| Fix vorgemerkt fürs Zeitfenster | rot über den CVE, dazu Info „Einspielung geplant um …“ |
| Kernel-Fix installiert, laufender Kernel noch verwundbar | rot über den CVE, dazu Info „Neustart geplant um …“ bzw. „Neustart ausstehend“ |
| Verbund unter Mindestanzahl gesunder Server | gelb an Mitgliedern und Gruppe |
| Paketquelle nicht abrufbar oder Signatur ungültig über die Frist | gelb |
| Quelle „nur Server-Prüfung“, Abweichung zentral ↔ Server | Info |

Insights nennen Paket und Grund der Relevanz („nginx, Kerndienst Webserver“,
„redis-server, lauscht auf :6379“). Der Alarm `security_cve` löst nur noch bei
Rot aus. Gelb wird gesammelt bzw. im täglichen Bericht gemeldet (einstellbar).

### 4.4 Richtlinien-Stufen

| Stufe | Hochkritischer CVE mit Fix |
|---|---|
| **Sofort** | sofort einspielen, ohne Rückfrage |
| **Sofort im Zeitfenster** | im nächsten erlaubten Fenster; optional Höchstwartezeit, nach der trotzdem eingespielt wird |
| **Nur mit Bestätigung** | muss ausdrücklich gesetzt werden; Freigabe in der Oberfläche |
| **Normal** | im nächsten Lauf der Richtlinie |

**Zeitfenster** sind eigene, wiederverwendbare Objekte (z. B. Mo–Fr 06–22 Uhr),
auswählbar in Gruppen und an Servern.

**Freigabe** nur in der Oberfläche nach Anmeldung, mit Berechtigung für den
Server. Die Benachrichtigung enthält einen Link dorthin.

### 4.5 Reguläres Patchen bleibt getrennt

- Alle übrigen Updates laufen über die bestehenden **Zeitplan-Regeln**, in der
  dort eingestellten Häufigkeit (täglich, zweimal pro Woche, wöchentlich …),
  ohne Verbund-Staffelung.
- Die Update-Regel bekommt die Option **„Kerndienst-Pakete auslassen“**,
  **Standard an** auf allen Servern mit bestätigten Kerndiensten. Der Zeitplan
  fasst dann gezielt nur die übrigen Pakete an (`--only-upgrade <Liste>`).
- Kerndienst- und Basis-Pakete samt zugehörigen Paketen laufen ausschließlich
  über die Patch-Richtlinie: hochkritische CVEs nach Stufe, alle anderen
  Updates dieser Pakete im Zeitfenster der Richtlinie, im Verbund gestaffelt.

---

## 5. Ausführung

### 5.1 Ablauf eines Patches

1. Auslöser: neue Kandidatenversion (1.4) oder neue CVE-Daten machen eine
   vorhandene Version zum Fix.
2. Resolver: Stufe, Zeitfenster, Freigabe, Verbund-Budget.
3. **Vorladen:** Die Fix-Pakete werden einmal in LCMs APT-Cache
   (apt-cacher-ng) geholt, bevor die Server starten. Alle Server laden danach aus
   dem lokalen Cache.
4. Auf dem Server: `apt-get update`, Simulation **nur** für die betroffenen
   Pakete, dann Einspielen mit **fester Version** (`paket=version`), genau der
   bewerteten.
5. ZPO-Drain (falls Verbund mit Load Balancer).
6. Paketbestand neu lesen, Dienste nach 2.6 neu starten.
7. Health-Checks, SSH-Test.
8. Ggf. zurück in den Load Balancer, Ergebnis protokollieren, Betriebsfähigkeit
   (Release A) setzen, benachrichtigen. Bei Fehler: Alarm, keine
   Wiederholungsschleife.

### 5.2 Geschwindigkeit in der Ausführung

- **Sicherheits-Patches überholen** in der Job-Warteschlange je Server alle
  normalen Jobs (Priorität in `JobService`).
- Unabhängige Server werden **parallel** gepatcht, mit einstellbarer
  Parallelitätsgrenze, die den APT-Cache nicht überlastet.
- Nach einem Patch wird der CVE-Status sofort aus dem Schnellweg (4.1)
  aktualisiert. Trivy folgt im Hintergrund nur für diesen Server.

### 5.3 Verbund-Durchlauf

- Nie mehr Server gleichzeitig außer Dienst als das Budget aller betroffenen
  Verbünde erlaubt.
- Der nächste Server folgt erst nach bestandenem Health-Check des vorigen.
- Fortschritt live über den Ereigniskanal aus Release B.

### 5.4 Einführung auf bestehenden Installationen

Nach dem Update auf Release C laufen die Richtlinien zunächst im
**Probelauf**: LCM meldet, was es patchen würde, und spielt nichts ein. Der
Probelauf wird je Gruppe oder global beendet.

### 5.5 Benachrichtigungen

| Typ | Wann |
|---|---|
| `cve_fix_available` | hochkritischer CVE mit Fix an Kerndienst/Basis: wird sofort/im Fenster um … eingespielt bzw. wartet auf Freigabe |
| `cve_patch_applied` / `cve_patch_failed` | nach der Ausführung, mit Dauer, Neustarts, Prüfergebnis |
| `cve_unfixed_core` | hochkritischer CVE an Kerndienst **ohne** Fix, mit Hinweis, den Dienst ggf. vom Netz zu nehmen; KEV/EPSS hervorgehoben; erneute Meldung, sobald ein Fix erscheint |
| `cve_approval_pending` | Erinnerung bei offener Freigabe |

Entdoppelung je CVE und Server.

### 5.6 Protokoll

Jede automatische Aktion ins Audit-Log: auslösender CVE, Stufe und Herkunft
(global/Gruppe/Server), Zeitfenster, Freigabe (wer, wann), Ergebnis, Dauer.

---

## 6. Oberfläche (neues Frontend)

- **Server → Reiter „Dienste“:** Kerndienste mit Status, Vorschläge mit
  Begründung (bestätigen/verwerfen), Port-Pakete, **Wirksame Einstellungen** je
  Kerndienst mit Herkunft und überstimmten Werten.
- **Gruppe → Reiter „Dienste & Patchen“** (3.1), bei Verbundgruppen mit
  Verbund-Einstellungen und Live-Durchlauf. Aktion „Gruppe nacheinander neu
  starten“ (3.5).
- **Dashboard:** geplante Patches, wartende Freigaben, laufende
  Verbund-Durchläufe, Zeit-bis-zum-Patch.
- **Sicherheit:** CVEs nach Relevanz gruppiert, Fix-Status, KEV/EPSS.
- **Einstellungen → Abschnitt „Dienste & Patchen“:** Dienstprofile,
  Basis-Pakete, Zeitfenster, globale Patch-Standards und Schwellen,
  Paketquellen (Register mit Abrufstatus und Abweichungen),
  ZPO-Anbindung, Schalter **„Automatisches Patchen pausieren“**.

---

## 7. Leistung im Detail

- **Bedingte Abrufe:** Unveränderte Quellen kosten ein `304`.
- **Streaming:** Indizes werden beim Entpacken zeilenweise verarbeitet, nie
  komplett im Speicher. Speicher je Abruf begrenzt.
- **Delta statt Vollberechnung** (1.4), **umgekehrter Index im Speicher**,
  Versionsvergleiche zwischengespeichert.
- **Datenbank:** nur installierte Paketnamen je Quelle, Sammel-Schreibvorgänge,
  Indizes auf `(server, name)` und `(source, name)`, keine Einzelabfragen je
  Server in Listen und Status.
- **CVE-Daten:** Feeds mit ETag bedingt abgerufen (Tracker 15 min, KEV
  stündlich, EPSS täglich). Ausgewertet werden nur geänderte Einträge.
- **Ereignisgesteuert statt Takt:** Neue Versionen, neue CVE-Daten und
  Paketänderungen lösen Bewertung und Patch aus, nicht ein fester Zeitplan.
- **Parallelität** beim Abruf je Host begrenzt (höflich zu Mirrors), beim
  Patchen über die Parallelitätsgrenze.
- **Vorladen in den APT-Cache** (5.1).
- **Messung:** Laufzeit jedes Abrufs, jeder Bewertung und jeder Strecke aus den
  Leistungszielen wird gespeichert und unter Einstellungen → Paketquellen bzw.
  auf dem Dashboard angezeigt, mit interner Warnung bei Überschreitung.

## 8. Sicherheit

- **Signaturen:** `InRelease` bzw. `repomd.xml` werden gegen die Schlüssel
  geprüft: Distributions-Keyrings, `KnownRepo.KeyURL`, für Sonderquellen der
  Schlüssel, dem der Server vertraut (`signed-by`, Fingerprint gespeichert und
  festgehalten). Ohne gültige Signatur: keine Aussage, gelbe Warnung.
- **Schutz vor veralteten oder zurückgespielten Indizes:** `Valid-Until` wird
  geprüft, und `Date` darf nie älter sein als der zuletzt angenommene Stand der
  Quelle. Ein Mirror kann so keine alten Daten unterschieben und damit Fixes
  verstecken.
- **Abrufe absichern (SSRF):** Die URLs stammen aus den Quelldateien der Server,
  und ein kompromittierter Server könnte sie manipulieren. Deshalb:
  - nur `http`/`https`,
  - keine Ziele in privaten, Loopback- oder Link-Local-Netzen, außer
    ausdrücklich erlaubten (eigene Mirrors, APT-Cache),
  - Weiterleitungen nur unter denselben Regeln,
  - Größen- und Zeitgrenzen, Schutz vor Entpack-Bomben.
- **Server-Meldungen sind nicht vertrauenswürdig:** Paketbestand und Quellen
  eines Servers wirken nur auf diesen Server. Eine Index-Quelle ist über URL
  **und** Schlüssel-Fingerprint bestimmt, eine manipulierte Quelle betrifft keine
  anderen Server.
- **Eingespielt wird genau die bewertete Version** (`paket=version`) aus einer
  signierten Quelle.
- **Freigaben** nur in der Oberfläche, mit Berechtigung für den Server, mit
  Audit-Eintrag.
- **Not-Aus:** „Automatisches Patchen pausieren“ global und je Gruppe.
- **ZPO-API-Schlüssel** mit eigenem, schmalen Scope, gespeichert im
  verschlüsselten Zugangsspeicher von LCM.
- **Feeds** nur per HTTPS, Prüfsummen, wo angeboten.

---

## 9. Umsetzungsreihenfolge innerhalb von Release C

1. Quellen-Register, apt-Abruf mit Signatur- und Aktualitätsprüfung,
   SSRF-Schutz, Delta, Kandidatenberechnung im **Schattenbetrieb** neben dem
   bestehenden Scan, Abweichungsmessung.
2. Änderungserkennung (SSH-Leichtcheck, Agent-Hook), Umschalten auf die zentrale
   Sicht mit Rückfall „nur Server-Prüfung“.
3. Relevanz: Basis, Port-Pakete, Dienstprofile, Kerndienste, zugehörige
   Pakete, Vorschläge, Server-Reiter „Dienste“.
4. CVE: Feeds, Schnellweg, Hochkritisch, neue Warnstufe.
5. Gruppen-Reiter, Resolver, Wirksame Einstellungen, Konfigurationsprüfung,
   Zeitfenster, Stufen mit Vorlagen, Freigabe, Alarme, Probelauf.
6. Ausführung: Vorladen, Patch mit fester Version, Dienst-Neustarts,
   SSH-Sicherung, Health-Checks, Verbundgruppen mit Durchlauf, koordinierter
   Neustart mit Bewertung des laufenden Kernels, ZPO-Drain,
   Option „Kerndienst-Pakete auslassen“, Priorität und Parallelität.
7. dnf- und zypper-Abruf mit `updateinfo`.

Nach Abstimmung als openspec-Changes anlegen.

## Entscheidungen (31.10.2026)

- **Neue Server:** Stufe „Sofort“ für Kerndienste und Basis ab dem ersten Tag.
  Bestehende Installationen starten nach dem Update im Probelauf (5.4).
- **Rot-Schwelle** für relevante Pakete: ab CVSS 7.0 (4.2, 4.3).
- **Kernel:** immer mitbewertet, gegen den laufenden Kernel. Neustart nur im
  Neustart-Zeitfenster oder auf Anforderung, koordiniert über die Gruppe
  (2.6, 3.5).
