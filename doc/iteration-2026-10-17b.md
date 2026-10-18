# Iteration 2026-10-17 (Nachtrag) – Arbeitsliste

Drei Punkte aus dem Betrieb. Jeder Punkt: Befund (mit Fundstellen), Lösung,
Prüfung. Commits je Punkt lokal, Push nach develop; MR erst auf Ansage.

## 1. Festplatten-Diagramm: Maus und Grafik passen nicht (Fehler)

**Befund**
- `frontend/src/pages/ServerDetail.svelte` `chartHover`: rechnet die Mausposition
  über die volle Box des SVG (`getBoundingClientRect`) in viewBox-Koordinaten um.
- Das SVG hat `max-height: 300px` und `viewBox` mit fester Proportion. Ist es
  breiter als die Grafik, sitzt die Zeichnung zentriert mit Rand (Standard
  `preserveAspectRatio`). Die Mausberechnung ignoriert den Rand, daher der Versatz
  rechts.

**Lösung**
- Diagramm füllt die volle Breite (`w-100 d-block`, ohne `max-height`).
- Mausposition über die Bildschirm-Matrix des SVG (`getScreenCTM().inverse()`)
  umrechnen; damit stimmt sie unabhängig von Rand und Skalierung.

**Prüfung**: Build; im Browser die Maus an den rechten und linken Rand fahren
und Punkt/Tooltip gegen den Datenpunkt prüfen.

## 2. Anleitung: Passwort, E-Mail, voller Name für Linux-Benutzer (Feature)

**Befund**
- Die In-App-Doku liegt unter `internal/appdocs/pages/<sprache>/`; die Seite
  „Serveränderungen" erklärt Änderungen am Server, aber nicht die Einrichtung
  eines Benutzers.
- Passwort: Der Mitarbeiter setzt es über den Aktivierungslink
  (`/linux-aktivierung`, `ConsumeActivation`). Ohne Passwort bleibt das Konto
  gesperrt (`usermod -p '*'`), SSH-Key-Login geht, Anmeldung per PAM (Proxmox-
  Web-UI, Konsole) nicht.
- Voller Name: `LinuxUser.FullName` → `chfn -f` auf dem Server.
- E-Mail: `LinuxUser.Email` dient nur dem Aktivierungslink. Sie wird nicht auf
  den Server geschrieben.
- Der Reiter „Benutzer" der Server-Seite hat noch keinen Verweis auf eine
  Anleitung.

**Lösung**
- Neue Seite `80-linux-benutzer.md` (de + en): Passwort festlegen (Aktivierungs-
  link, Mail oder manuell), voller Name, Shell, SSH-Key, E-Mail (ehrlich: nur für
  den Link; auf dem Server selbst setzen, falls nötig), Sperren/Entfernen.
- Schaltfläche „Anleitung" im Reiter Benutzer, verlinkt auf `#/doku/linux-benutzer`.

**Prüfung**: `appdocs`-Tests (Sprachen gleich viele Seiten, Links gültig);
Link im Browser öffnen.

## 3. Docker: überholte Images unsichtbar, Aufräumen unvollständig (Fehler)

**Befund**
- Nach `docker pull nginx:latest` mit neuem Digest verliert das alte Image seinen
  Tag und wird „überholt" (Repo `<none>`). Ein laufender Container hält es, bis er
  neu erstellt ist.
- `internal/core/services/docker_scan.go` `parseDockerImages` verwirft Zeilen mit
  `<none>`-Repo. Die überholten Images erscheinen daher nirgends.
- Die Regel `docker-prune` (`docker image prune -af`) würde sie nach dem Ende der
  Nutzung entfernen, aber niemand sieht, dass sie da liegen.
- Die Aktionen „Compose-Update" und „Image ziehen" (`docker.go`-Aktionen) und die
  Regel `docker-update-unused` räumen nach dem Neustart nichts auf.

**Lösung**
- Scan erfasst überholte Images zusätzlich (`docker images -f dangling=true`,
  Herkunft über `RepoDigests`). Sie werden als eigene Zeilen gespeichert
  (`Superseded = true`, Tag leer).
- Eindeutigkeit: Der Index `idx_dockeri_server_ref` (Server, Repo, Tag) wird um
  die Image-ID erweitert. Migration per `DROP INDEX IF EXISTS` vor AutoMigrate,
  Muster wie in `database.go`.
- Anzeige: je Repository die Zahl überholter Fassungen („nginx:latest · 2 ältere
  Fassungen"), dazu die Zeilen der überholten Images. Verwendete markieren wir
  wie bisher (`InUse`).
- Aufräumen: `docker image prune -af` bleibt die Regel; nach jeder neuen Regel
  (Punkt 3b) wird zusätzlich `docker image prune -f` (nur untagged) ausgeführt.

**Prüfung**: Tests für Parser (überholte Zeile → Superseded, Herkunft aus
RepoDigest), Migration, Zählung je Repository.

## 3b. Docker-Regeln wie Watchtower (Feature)

**Befund**
- Vorhanden: `docker-update-unused` (nur ungenutzte Images ziehen),
  `docker-prune`, die Aktionen „Compose-Update" und „Image ziehen" (nur am
  Server, nicht als Regel). Ein Container wird nicht neu erstellt; das übernimmt
  heute der Betreiber.

**Lösung**
- `docker-compose-update` (Regel): je Compose-Projekt mit verfügbarem Update
  `docker compose pull && up -d` am Projektort, danach `docker image prune -f`.
  Command = optional ein Projektname, leer = alle betroffenen Projekte.
- `docker-container-update` (Regel): je laufendem Standalone-Container mit
  verfügbarem Update (Tag vorhanden): Image ziehen, Container mit gleicher
  Konfiguration neu erstellen, altes Container-Image entfernen. Die Konfiguration
  kommt aus `docker inspect` (Config + HostConfig + Netzwerke) und geht über die
  Engine-API (`POST /containers/create` per curl auf dem Socket). Damit werden alle
  Einstellungen übernommen, nicht nur eine Auswahl von Flags.
  Sicherheit: Der alte Container wird nur umbenannt (`NAME.lcm-old`). Scheitert
  die Neuerstellung oder der Start, wird er zurückbenannt und wieder gestartet.
  Erst danach wird der alte Container entfernt.
  Command = optional Containername, leer = alle.
- Beide Regeln respektieren `DockerUpdatesDisabled`. Auf eingeschränkten Servern
  (`RestrictedSudo`) nicht erlaubt, da sie Docker-Schreibrechte brauchen.
- Typen registrieren: `validRuleType`, Job-Typen (`job_controller.go`), Gruppen-UI
  (Typ, Beschreibung, Command-Feld), Doku `docs/guides/docker.md` (de + en).

**Prüfung**: Unit-Tests für die Konfigurations-Umwandlung (Ports, Mounts, Env,
Netzwerk, Restart, Rechte), Rollback-Skript, Validierung; Gruppen-E2E.
Live-Test: wo ein Docker-Host verfügbar ist (lcm-demo oder Testumgebung).

## Reihenfolge der Umsetzung

1. Festplatten-Diagramm (klein, sofort sichtbar)
2. Anleitung Linux-Benutzer + Verweis
3. Überholte Docker-Images (Scan, Index, Anzeige, Zählung)
4. Docker-Regeln (Compose-Update, Container-Update mit Rollback)
5. Dokumentation Docker, Tests, Prüfung

## Protokoll

| Punkt | Stand | Commit |
|---|---|---|
| 1 Festplatten-Diagramm | umgesetzt, geprüft | 8af1689 |
| 2 Anleitung Linux-Benutzer | umgesetzt, geprüft | 8af1689 |
| 3 Überholte Images | umgesetzt, geprüft | 8af1689 |
| 3b Docker-Regeln | umgesetzt, geprüft | 8af1689 |

Alle Go-Tests, Frontend-Build, npm audit, govulncheck und E2E (165/165) grün.
