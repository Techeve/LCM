---
sidebar:
  order: 20
title: Architektur
description: Schichtenmodell des Go-Backends, Verzeichnisstruktur, Startablauf, Versionierung, Build und Betrieb von LCM.
---

LCM kompiliert ein Go-Backend (Fiber v3) und ein Svelte-5-Frontend in **ein einziges Binary**. Das Frontend wird von Vite nach `frontend/dist` gebaut und per `go:embed` eingebettet (`frontend/embed.go`).

## Schichtenmodell des Backends

Jeder Request durchläuft die Schichten in fester Reihenfolge:

```
HTTP-Request
   │
   ▼
Router + Middlewares   internal/api/router, internal/api/middlewares
   │                   (JWT/API-Key-Auth, RBAC, Logging, Recover)
   ▼
Controller             internal/api/controllers
   │                   (JSON parsen, Input validieren, Statuscodes)
   ▼
Service                internal/core/services
   │                   (Business-Logik - kennt kein HTTP)
   ▼
Repository             internal/storage/repositories
   │                   (GORM-Operationen - kennt keine Business-Logik)
   ▼
SQLite                 CGO-frei via modernc.org/sqlite (glebarez/sqlite-Treiber)
```

**Regeln:**

- Controller enthalten keine Business-Logik und keine DB-Zugriffe.
- Services importieren niemals `fiber` - sie sind ohne Webserver testbar.
- Repositories sind der einzige Ort mit GORM-Code. Fehler werden auf `repositories.ErrNotFound` normalisiert.
- Domain-Structs (`internal/core/domain`) sind überall erlaubt, haben aber selbst keine Abhängigkeiten.

## Ein neues Feature bauen - Schritt für Schritt

Beispiel: eine „Notes"-Funktion. Immer von innen nach außen:

**1. Domain-Model** - `internal/core/domain/note.go`

```go
type Note struct {
    ID        uint      `gorm:"primarykey" json:"id"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
    UserID    uint      `gorm:"index;not null" json:"user_id"`
    Title     string    `gorm:"not null" json:"title"`
    Body      string    `json:"body"`
}
```

**2. Migration registrieren** - in `internal/storage/database.go` zu `Migrate()` hinzufügen:

```go
return db.AutoMigrate(
    // ... bestehende Entitäten ...
    &domain.Note{},
)
```

**3. Repository** - `internal/storage/repositories/note_repository.go` (Vorlage: `alert_repository.go`). Nur GORM-Aufrufe, keine Logik.

**4. Service** - `internal/core/services/note_service.go`. Validierung und Regeln hier; eigene Fehler als `var ErrXyz = errors.New(...)` exportieren.

**5. Controller** - `internal/api/controllers/note_controller.go` (Vorlage: `custom_action_controller.go`). Fehler mit `mapServiceError` in Statuscodes übersetzen.

**6. Route + Permission** - Permission-Code in `domain/rbac.go` definieren, im Seeding (`storage/seed.go`) einer Rolle zuweisen, Route in `internal/api/router/router.go` registrieren:

```go
notes := api.Group("/notes")
notes.Get("/", middlewares.RequireAuth(), noteCtrl.List)
notes.Post("/", middlewares.RequirePermission(domain.PermNotesWrite), noteCtrl.Create)
```

:::caution[Wichtig (Fiber v3)]
Handler laufen in Angabe-Reihenfolge - Middlewares stehen **vor** dem Controller-Handler, sonst ist die Route ungeschützt. Der Regressionstest `TestRBACMiddlewareRunsBeforeHandler` überwacht das.
:::

**7. Frontend-API-Klasse + Page** - siehe [Frontend & API](/reference/api/).

:::note[Sonderfall: Logik ohne Datenbank]
Für reine Logik-Prozesse (Berechnungen, Versionsinfo, externe API-Aufrufe) entfällt die Repository-Schicht - die Kette ist nur **Controller → Service**. Referenzbeispiel: `SystemService` (`internal/core/services/system_service.go`) und `SystemController` mit der Route `GET /api/v1/system/info`. Alle anderen Regeln (Service kennt kein HTTP, Controller keine Logik) gelten unverändert.
:::

**8. Tests** - Service-Test in `internal/core/services/` gegen In-Memory-SQLite (Vorlage: `services_test.go`), Routen-Test in `internal/api/router/router_test.go`.

## Verzeichnisstruktur

```
cmd/app/main.go            Einstiegspunkt: Config → DB → Seeding → Server
docs/                      Diese Dokumentation
internal/api/              HTTP-Transport (Controller, Middlewares, Router)
internal/core/domain/      Entitäten (GORM-Structs) + Permission-Codes
internal/core/services/    Business-Logik
internal/infrastructure/   Technik-Anbindungen: sshx (SSH), trivy (CVE-Scan),
                           notify (E-Mail), crypto, totp, tlsx
internal/remote/           LCM Remote: eingebetteter MQTT-Broker + AgentHub
                           (dedizierter Agent-Listener für lcm-agent)
internal/mcp/              MCP-Schnittstelle: eigener Listener + read-only
                           Whitelist-DTO (ServerView) für KI-Agenten
internal/netfilter/        IP-Allowlist-Matcher (config.json allowed_ips)
internal/storage/          DB-Verbindung, Migration, Seeding, Repositories
internal/config/           config.json-Management
frontend/src/api/          API-Service-Klassen (fetch-Abstraktion)
frontend/src/components/   Wiederverwendbare UI-Elemente
frontend/src/pages/        Eine Svelte-Datei pro Route
frontend/src/stores/       Globaler State (Svelte 5 Runes)
frontend/embed.go          go:embed des dist-Ordners
```

## Konfiguration & Startablauf

Beim Start sucht das Binary eine `config.json` **neben der ausführbaren Datei** (überschreibbar mit `-config <pfad>`). Fehlt sie, wird sie mit sicheren Zufallswerten erzeugt - inklusive kryptografisch starkem JWT-Secret. Details: [Sicherheitsmodell](/reference/security-model/).

Danach: SQLite öffnen (WAL-Modus), GORM-AutoMigration, **Update-Prozess** (version.json mit Binary-Version abgleichen, ggf. Update-Migrationen ausführen - siehe [Datenbank & Migrationen](/reference/database/)), idempotentes Seeding (Rollen, Permissions, `system`- und `admin`-User; im Demo-Modus zusätzlich Beispiel-Server samt Paketen, Speicher-Verlauf und CVE-Funden). Das generierte Admin-Passwort erscheint **einmalig** in der Konsole.

Eine Installation besteht damit aus vier Dateien im selben Verzeichnis: Binary, `config.json`, `version.json` und `app.db` - alle drei Begleitdateien entstehen beim ersten Start von selbst.

## Netzwerk-Listener

LCM bindet bis zu **drei** voneinander getrennte Listener - bewusst auf eigenen Ports, damit sich Angriffsfläche und Zugriffswege sauber trennen lassen:

- **Weboberfläche + REST-API** (`host`/`port`, Default `127.0.0.1:9310`): der Fiber-Router aus `internal/api/router` mit eingebettetem Frontend. HTTPS mit self-signed Zertifikat; `--dev` erlaubt HTTP.
- **Agent-Listener** (`agent_host`/`agent_port`, Default `0.0.0.0:9320`): ein **eigener** Fiber-Gateway (`router.NewAgentGateway`) mit ausschließlich dem `/mqtt`-Endpunkt für LCM Remote (`internal/remote`, eingebetteter MQTT-Broker + AgentHub). Läuft nebenläufig zum UI-Listener und nutzt dasselbe TLS-Zertifikat; `agent_port: 0` schaltet ihn ab.
- **MCP-Listener** (`internal/mcp`, Default aus, `127.0.0.1:9330`): ein zur Laufzeit über die Einstellungen an-/abschaltbarer, schlanker HTTP-Listener mit nur dem `/mcp`-JSON-RPC-Endpunkt. Authentifizierung per Bearer-API-Key mit MCP-Scope; ausgeliefert wird ausschließlich das read-only Whitelist-DTO.

Details zu Ports und Umgebungsvariablen: [Installation](/getting-started/installation/); zu den Features [LCM Remote](/guides/remote/) und [MCP-Schnittstelle](/guides/mcp/).

## Versionierung & Build-Nummer

- **Semantic Version:** Datei `VERSION` im Projekt-Root (z.B. `1.2.0`) - von Hand pflegen.
- **Build-Nummer:** Datei `.buildnumber` - zählt bei jedem Build-Target automatisch hoch (Makefile-Target `bump-build`, läuft pro `make`-Aufruf genau einmal, auch bei `build-all`).
- Beide Werte werden per `-ldflags -X` in das Package `internal/version` injiziert, zusammen mit dem Build-Zeitpunkt.

Abfragbar über: `./lcm -version` (CLI), `GET /api/v1/system/info` (API), Footer der Web-App und `make version`.

**Update-Erkennung:** Jede Installation führt eine `version.json` neben der Datenbank (wird beim Erststart automatisch angelegt). Bei jedem Start wird sie mit der Binary-Version verglichen - ein Unterschied bedeutet „Binary wurde aktualisiert" und triggert die versionsgebundenen Update-Migrationen. Details und Anleitung: [Datenbank & Migrationen](/reference/database/).

## Build & Cross-Compiling

`make build` läuft die Pipeline: `npm audit` → `vite build` → `govulncheck` → `go build`. Schlägt ein Sicherheits-Check fehl, bricht der Build ab.

Da der SQLite-Treiber CGO-frei ist, funktioniert Cross-Compiling ohne Toolchains - inklusive ARM:

```sh
make build-linux         # bin/lcm-linux-amd64
make build-linux-arm64   # bin/lcm-linux-arm64   (z.B. Raspberry Pi, ARM-Server)
make build-windows       # bin/lcm-windows-amd64.exe
make build-macos         # bin/lcm-darwin-arm64  (Apple Silicon)
make build-macos-intel   # bin/lcm-darwin-amd64
make build-all           # alle Plattformen
```

## Betrieb mit Docker

Alternativ zum Systemdienst gibt es ein gehärtetes Alpine-Container-Image (non-root, read-only; das fertig gebaute Binary wird hineinkopiert) samt Compose-Beispiel mit `./data`-Volume für config.json/app.db/version.json - komplette Anleitung: [Docker](/guides/docker/). Kurzform: `make docker-build && docker compose up -d`.

## Betrieb als Systemdienst

Das Binary ist selbst-initialisierend (Config + DB entstehen beim ersten Start) - ideal für Dienste.

**systemd** (`/etc/systemd/system/lcm.service`):

```ini
[Unit]
Description=LCM
After=network.target

[Service]
User=lcm
WorkingDirectory=/opt/lcm
ExecStart=/opt/lcm/lcm
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

**Windows:** `sc.exe create LCM binPath= "C:\lcm\lcm.exe" start= auto` (oder komfortabler mit [NSSM](https://nssm.cc)).

**macOS launchd** (`~/Library/LaunchAgents/de.lcm.kit.plist`):

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>de.lcm.kit</string>
  <key>ProgramArguments</key>
  <array><string>/usr/local/opt/lcm/lcm</string></array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
</dict>
</plist>
```

Das Binary behandelt SIGINT/SIGTERM mit Graceful Shutdown.

## Absturzsicherheit & Selbstheilung

LCM verwaltet fremde Server und wertet dabei deren Ausgaben aus. Ein einzelner
Server, der unerwartet antwortet, darf niemals die Verwaltung aller anderen
lahmlegen. Der Schutz ist deshalb mehrstufig aufgebaut - von „Fehler abfangen"
bis „im Zweifel kontrolliert neu starten".

### Stufe 1 - Panics abfangen

In Go beendet ein unbehandelter Panic **immer den gesamten Prozess**, auch aus
einer beliebigen Hintergrund-Goroutine heraus. Alle Ausführungspfade sind daher
abgesichert:

| Pfad | Schutz |
|---|---|
| HTTP (UI/REST, Agent-Gateway, MCP) | Fibers `recover`-Middleware, jeweils als erste Middleware |
| Hintergrund-Goroutinen (Job-Runner, Worker, Listener) | [`internal/safego`](https://gitlab.techeve.de/techeve/lcm/-/tree/community/internal/safego) - `safego.Go` / `safego.GoCleanup` |
| Geplante Läufe (Cron) | `cron.Recover`-Kette im Scheduler |
| MQTT-Hooks + WebSocket-Verbindung der Agents | `safego.Recover` je Hook |

Der MQTT-Pfad braucht die eigene Absicherung, weil er **außerhalb** jeder
HTTP-Middleware läuft: Die Verbindung wird beim WebSocket-Upgrade an eine
eigene Goroutine übergeben, und weder die Broker-Bibliothek noch der
darunterliegende HTTP-Server fangen dort etwas ab. Der Authentifizierungs-Hook
ist zudem durch **unauthentifizierte** Pakete erreichbar. Alle Hooks arbeiten
darum *fail-closed*: Ein abgefangener Panic gilt als Ablehnung, nie als
Zustimmung.

Bei Job-Runnern kommt ein zweiter Aspekt hinzu: Ein Job hält eine **Sperre auf
seinen Server**. Bricht der Runner ab, ohne den Job abzuschließen, wäre der
Server für alle weiteren Aktionen blockiert. `safego.GoCleanup` schließt den Job
deshalb als fehlgeschlagen ab - die Sperre fällt, der Fehler ist in der
Job-Historie sichtbar.

### Stufe 2 - Gesundheit prüfen

`GET /api/v1/health` prüft, was der Dienst zum Arbeiten braucht: einen
**Datenbank-Ping**. Ohne Datenbank kann LCM nichts Sinnvolles tun, würde aber
weiterhin HTTP annehmen - eine reine „ok"-Antwort wäre also wertlos.

- **nicht angemeldet:** nur `{"status":"ok"}` bzw. HTTP 503 mit
  `{"status":"unhealthy"}`
- **angemeldet:** zusätzlich Diagnosewerte (`fail_streak`, `panics_total`,
  `panics_recent`, `watchdog_active`, `uptime_seconds`)

Fehlertexte bleiben angemeldeten Nutzern vorbehalten - sie können Pfade oder
Datenbank-Interna enthalten.

### Stufe 3 - Selbstneustart

Ein abgestürzter Prozess wird von systemd ohnehin neu gestartet. Die
schwierigeren Fälle sind die, in denen der Prozess **weiterläuft, aber nicht
mehr arbeitet** - genau die deckt die Selbstüberwachung ab:

| Situation | Reaktion |
|---|---|
| Datenbank dauerhaft unerreichbar (5 Prüfungen in Folge) | kontrollierter Neustart |
| Gehäufte abgefangene Panics (10 in 15 Minuten) | kontrollierter Neustart |
| Prozess hängt/blockiert vollständig | systemd-Watchdog greift (Lebenszeichen bleibt aus) |
| Kurzer Aussetzer (z.B. gesperrte SQLite-Datei während eines Backups) | **kein** Neustart - die Zählung wird bei Erfolg zurückgesetzt |

Der Watchdog ist bewusst so verdrahtet, dass das Lebenszeichen (`WATCHDOG=1`)
**nur bei erreichbarer Datenbank** gesendet wird. Bleibt es aus, beendet systemd
den Dienst nach `WatchdogSec` und startet ihn neu. Das deckt zusätzlich den Fall
ab, dass die Überwachungs-Goroutine selbst blockiert.

Beendet sich LCM selbst, geschieht das mit **Exit-Code 70**; ein vorbereitetes
Backup-Restore nutzt **42**. Beide sind im Journal eindeutig zuzuordnen.

:::note[Ohne Dienstverwaltung]
Läuft LCM im Vordergrund (Entwicklung, manueller Start), bleibt der Dienst nach
einem Selbstneustart beendet - es gibt niemanden, der ihn wieder hochfährt. Das
ist beabsichtigt und wird deutlich protokolliert; ein weiterlaufender Prozess
mit beschädigtem Zustand wäre die schlechtere Alternative. Unter systemd und in
Docker (`restart: unless-stopped`) startet der Dienst automatisch neu.
:::

### Betriebliche Absicherung (systemd)

Die mitgelieferte Unit setzt:

- `Type=notify` - der Dienst gilt erst als gestartet, wenn er wirklich
  betriebsbereit ist
- `Restart=always`, `RestartSec=5`
- `WatchdogSec=90` - Lebenszeichen-Überwachung
- `StartLimitIntervalSec=300` / `StartLimitBurst=5` - Crash-Loop-Bremse: Kommt
  der Dienst nach 5 Versuchen in 5 Minuten nicht hoch, bleibt er gestoppt,
  statt die Maschine zu belasten. Sichtbar über `systemctl status lcm`.

Ein sauberes Beenden erzeugt die Logzeile `=== LCM-Dienst wird beendet ===`.
**Fehlt sie vor einem Start-Marker, war es ein Absturz** - der schnellste Weg,
im Journal zwischen Neustart und Absturz zu unterscheiden.
