# Release A: Fehlgeschlagene Aktionen wirken auf den Serverstatus

Stand: 31.10.2026 · Status: abgestimmter Entwurf · Teil des Fahrplans
(`doc/plan-2026-10-31-fahrplan.md`)

## Ziel

Kann LCM auf einem Server nicht mehr handeln, etwa weil Updates an einer
dpkg-Sperre scheitern, muss das sofort und dauerhaft im Status stehen, nicht
nur im Protokoll. Scheitern Updates, ist der Server **rot**.

## Was heute dazu führt, dass es untergeht

- Der Status kennt nur „letzter beendeter Job fehlgeschlagen“ (gelb), und zwar
  den jüngsten Job **jeder** Art (`server_status.go:164,212`,
  `job_repository.go:138`). Der nächste erfolgreiche Health-Ping überschreibt
  ein fehlgeschlagenes Update nach wenigen Minuten.
- Abgebrochene Jobs (`aborted`) zählen nicht als Fehlschlag
  (`server_status.go:213`).
- „Paketliste aktualisieren“ meldet immer Erfolg, auch bei Fehlern von
  `apt update` (`server_refresh.go:159`).
- Fehler stehen nur als Text im Job-Output (`job_service.go:484`). Es gibt
  weder ein Fehlerfeld am Server noch Fehlerklassen.
- „Trivy nicht verfügbar“ und Fehler des flottenweiten CVE-Scans werden nicht
  gespeichert (`executor.go:1554,1590`).
- Es gibt keinen Alarm für fehlgeschlagene Aktionen.

## Lösung

### 1. Betriebsfähigkeit je Aktionsart

Neue Tabelle `ServerCapability`, ein Eintrag je Server und Kategorie:

| Kategorie | Umfasst | Bei Fehlschlag |
|---|---|---|
| `connection` | SSH-/Agent-Verbindung | rot (wie heute) |
| `packages` | Paketliste aktualisieren, Updates und Security-Updates einspielen, Einzelpaket-Aktionen | **rot** |
| `rules` | Zeitplan- und Grundsatz-Regeln, Skripte, Firewall, Benutzer-Sync, Rechte | gelb |
| `cve_scan` | CVE-Scan des Servers, Scanner nicht verfügbar | gelb |

Felder: `State` (ok/failed), `FailingSince`, `ConsecutiveFailures`,
`LastSuccessAt`, `LastErrorClass`, `LastErrorMessage`, `LastJobID`.

Regeln:
- Ein Fehlschlag setzt nur **seine** Kategorie auf `failed`.
- **Zurückgesetzt wird nur durch einen Erfolg derselben Kategorie.** Ein
  Health-Ping heilt kein fehlgeschlagenes Update.
- Ausnahme `lock_busy` (dpkg-Sperre durch einen anderen Prozess): beim ersten
  Fehlschlag gelb, ab dem zweiten Fehlschlag in Folge rot. Sperren durch
  unattended-upgrades sind oft nach Minuten vorbei.
- `aborted` durch den Watchdog zählt als Fehlschlag. Ein von Hand
  abgebrochener Job wird nur protokolliert.
- `blocked` (Server gerade belegt) zählt nicht.
- Die Kategorie wird am Jobende bestimmt, aus dem Jobtyp bzw. dem Regeltyp.

### 2. Fehlerklassen mit Handlungshinweis

Ein Klassifizierer (reine Funktion) liest Exit-Code und Output:

| Klasse | Erkennung (Beispiele) | Hinweis |
|---|---|---|
| `lock_busy` | Exit 75 (`aptlock.go:45`), „Could not get lock“, „dpkg frontend lock“ | sperrenden Prozess nennen (wird in `aptlock.go` schon ermittelt), z. B. unattended-upgrades |
| `dpkg_interrupted` | „dpkg was interrupted“ | Reparatur-Knopf „dpkg --configure -a ausführen“ |
| `broken_deps` | „unmet dependencies“, „held broken packages“ | betroffene Pakete nennen, Reparatur-Knopf „apt -f install ausführen“ |
| `disk_full` | „No space left on device“ | Datenträger nennen |
| `repo_error` | `Err:`/`E:` zu einer Quelle, 404, `NO_PUBKEY`, „Release file … expired / not valid yet“ | Quelle nennen; bei „not valid yet“ Hinweis auf die Serverzeit |
| `network` | „Temporary failure resolving“, Verbindungs-Timeouts | DNS, Proxy bzw. APT-Cache prüfen |
| `timeout` | Watchdog-Abbruch, `timeout`-Exit | letzter Output, Hinweis auf langsame Hardware |
| `unknown` | alles andere | Auszug aus dem Output |

Getestet mit echten Ausgaben von apt, dnf und zypper.

**Reparatur-Knöpfe:**
- Für `dpkg_interrupted` und `broken_deps` (bei dnf/zypper die
  entsprechenden Befehle).
- Nur mit `servers:write` für den Server, mit Bestätigung, als normaler Job mit
  Protokoll.
- Laufen über dieselbe apt-Sperrlogik wie Updates (`aptlock.go`).
- Nach Erfolg wird die fehlgeschlagene Aktion **nicht** automatisch
  wiederholt. Die Karte bietet „Erneut versuchen“ an, erst deren Erfolg setzt
  die Kategorie zurück.
- Für `lock_busy` gibt es keinen Knopf zum Beenden des sperrenden Prozesses,
  nur die Anzeige, welcher Prozess sperrt.

### 3. „Paketliste aktualisieren“ wertet Fehler aus

`runRefreshJob` prüft die Ausgabe von `apt update`:
- `E:` oder alle Quellen mit `Err:` → Fehlschlag `packages` (rot).
- Einzelne Quellen mit `Err:` → Erfolg, dazu gelber Hinweis „Paketquelle X
  fehlerhaft“ (`repo_error`).
- Reine Warnungen (`W:`) → Info.

### 4. Stillstand ohne Fehlermeldung

Finden Läufe gar nicht mehr statt (dauerhaft blockiert, Zeitplan aus, Job
hängt), ist LCM ebenfalls handlungsunfähig:
- Letzte **erfolgreiche** Paketaktualisierung älter als 2 × das Regelintervall,
  mindestens 48 h → gelb „Paketstand veraltet seit …“.
- Älter als 7 Tage → rot.
- Beide Fristen sind unter Einstellungen → Allgemein änderbar.

### 5. Ampel

`TrafficLightInput` bekommt die Kategorien statt `LastJobFailed`:
- `packages` failed → **rot** (bei `lock_busy` erst ab dem zweiten
  Fehlschlag in Folge), Insight z. B. „Updates schlagen seit 2 Tagen
  fehl (3×): dpkg-Sperre durch unattended-upgr (PID 1234)“.
- `rules` bzw. `cve_scan` failed → gelb, mit Regelname und Fehlerklasse.
- Stillstand nach Punkt 4.
- Statuscache wird wie heute beim Jobende verworfen (`server_status.go:91`).

### 6. Alarm `action_failed`

- Löst **beim Wechsel** ok → failed sofort aus, nicht erst im 30-Minuten-Lauf.
- Meldet die Rückkehr failed → ok als Entwarnung.
- Entdoppelung je Server und Kategorie. Erinnerung nach einstellbarer Frist,
  solange der Fehler besteht.
- Der bestehende Alarm `failed_updates` misst ausstehende Pakete. Er heißt in
  der Oberfläche künftig **„Ausstehende Updates“** (Typ-Schlüssel bleibt wegen
  bestehender Alarmregeln).

### 7. Oberfläche (im bestehenden Frontend)

- Ampel-Insights kommen aus dem Backend, dazu neue Texte in de/en.
- Serverdetail: Karte **„Betriebsfähigkeit“** mit den vier Kategorien: Zustand,
  seit wann, Anzahl Fehlschläge, letzter Fehler mit Hinweis, Link zum Job,
  Reparatur-Knopf (falls die Klasse einen hat) und „Erneut versuchen“.
- Dashboard: Filter „LCM handlungsunfähig“ (`packages` oder `connection`
  failed).
- Einstellungen → Alarme: neuer Typ, umbenannter Typ.

## Leistung

- Der Zustand wird **einmal am Jobende** geschrieben (ein Upsert), nicht bei
  jeder Statusabfrage berechnet.
- Die Statusberechnung lädt die Betriebsfähigkeit für alle Server einer Liste
  in **einer** Abfrage (Index auf `server_id`), keine Einzelabfragen je Server.
- Der Klassifizierer liest nur das Ende des Outputs (begrenzte Länge).
- Die Stillstandsprüfung läuft im bestehenden 30-Minuten-Lauf über einen
  Index auf `LastSuccessAt`.

## Sicherheit

- `LastErrorMessage` kann Pfade, Hostnamen oder Paketquellen enthalten. Sie
  wird gekürzt und **verschlüsselt** gespeichert wie der Job-Output und nur mit
  `servers:read` für den jeweiligen Server ausgeliefert (Mandantentrennung).
- Alarme enthalten Kategorie, Klasse und Hinweis, aber keinen rohen Output.
- Der sperrende Prozess wird nur mit Namen und PID gemeldet, ohne
  Kommandozeile.

## Tests

- Unit: Klassifizierer mit echten Ausgaben, Zustandswechsel je Kategorie,
  Ampelregeln (Health-Ping heilt kein Update, `aborted` zählt, `blocked` nicht).
- Testumgebung lcm-test:
  - dpkg-Sperre halten (`flock /var/lib/dpkg/lock-frontend sleep 900`),
    Update auslösen → rot und Alarm. Sperre lösen, erneut auslösen → grün und
    Entwarnung.
  - kaputte Paketquelle (falsche URL) → gelber Hinweis bzw. rot, wenn alle
    Quellen fehlschlagen.
  - unterbrochenes dpkg → `dpkg_interrupted`, Reparatur-Knopf, erneut
    versuchen → grün.
  - Sperre einmal → gelb, zweimal in Folge → rot.
