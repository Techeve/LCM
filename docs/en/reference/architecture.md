---
sidebar:
  order: 20
title: Architecture
description: The Go backend's layered model, directory structure, startup flow, versioning, build, and how to operate LCM.
---

LCM compiles a Go backend (Fiber v3) and a Svelte 5 frontend into **a single binary**. The frontend is built by Vite into `frontend/dist` and embedded via `go:embed` (`frontend/embed.go`).

## The backend's layered model

Every request passes through the layers in a fixed order:

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

**Rules:**

- Controllers contain no business logic and no DB access.
- Services never import `fiber` - they are testable without a web server.
- Repositories are the only place with GORM code. Errors are normalized to `repositories.ErrNotFound`.
- Domain structs (`internal/core/domain`) are allowed everywhere, but have no dependencies of their own.

## Building a new feature - step by step

Example: a "Notes" feature. Always work from the inside out:

**1. Domain model** - `internal/core/domain/note.go`

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

**2. Register the migration** - add it to `Migrate()` in `internal/storage/database.go`:

```go
return db.AutoMigrate(
    // ... bestehende Entitäten ...
    &domain.Note{},
)
```

**3. Repository** - `internal/storage/repositories/note_repository.go` (template: `alert_repository.go`). GORM calls only, no logic.

**4. Service** - `internal/core/services/note_service.go`. Validation and rules go here; export your own errors as `var ErrXyz = errors.New(...)`.

**5. Controller** - `internal/api/controllers/note_controller.go` (template: `custom_action_controller.go`). Translate errors into status codes with `mapServiceError`.

**6. Route + permission** - define the permission code in `domain/rbac.go`, assign it to a role in the seeding (`storage/seed.go`), and register the route in `internal/api/router/router.go`:

```go
notes := api.Group("/notes")
notes.Get("/", middlewares.RequireAuth(), noteCtrl.List)
notes.Post("/", middlewares.RequirePermission(domain.PermNotesWrite), noteCtrl.Create)
```

:::caution[Important (Fiber v3)]
Handlers run in the order given - middlewares come **before** the controller handler, otherwise the route is unprotected. The regression test `TestRBACMiddlewareRunsBeforeHandler` guards against this.
:::

**7. Frontend API class + page** - see [Frontend & API](/en/reference/api/).

:::note[Special case: logic without a database]
For pure logic processes (calculations, version info, external API calls) the repository layer is dropped - the chain is just **Controller → Service**. Reference example: `SystemService` (`internal/core/services/system_service.go`) and `SystemController` with the route `GET /api/v1/system/info`. All other rules (service knows no HTTP, controller has no logic) apply unchanged.
:::

**8. Tests** - service test in `internal/core/services/` against in-memory SQLite (template: `services_test.go`), route test in `internal/api/router/router_test.go`.

## Directory structure

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

## Configuration & startup flow

On startup the binary looks for a `config.json` **next to the executable** (overridable with `-config <path>`). If it is missing, it is created with secure random values - including a cryptographically strong JWT secret. Details: [Security model](/en/reference/security-model/).

Then: open SQLite (WAL mode), GORM auto-migration, the **update process** (compare version.json with the binary version, run update migrations if needed - see [Database & Migrations](/en/reference/database/)), and idempotent seeding (roles, permissions, the `system` and `admin` users; in demo mode additionally example servers with packages, storage history, and CVE findings). The generated admin password appears **once** in the console.

An installation therefore consists of four files in the same directory: the binary, `config.json`, `version.json`, and `app.db` - all three companion files are created automatically on first start.

## Network listeners

LCM binds up to **three** separate listeners - deliberately on their own ports, so attack surface and access paths can be cleanly separated:

- **Web UI + REST API** (`host`/`port`, default `127.0.0.1:9310`): the Fiber router from `internal/api/router` with the embedded frontend. HTTPS with a self-signed certificate; `--dev` allows HTTP.
- **Agent listener** (`agent_host`/`agent_port`, default `0.0.0.0:9320`): a **separate** Fiber gateway (`router.NewAgentGateway`) carrying only the `/mqtt` endpoint for LCM Remote (`internal/remote`, embedded MQTT broker + AgentHub). It runs alongside the UI listener and uses the same TLS certificate; `agent_port: 0` disables it.
- **MCP listener** (`internal/mcp`, off by default, `127.0.0.1:9330`): a lean HTTP listener, toggleable at runtime via the settings, carrying only the `/mcp` JSON-RPC endpoint. Authentication via a bearer API key with MCP scope; only the read-only whitelist DTO is served.

Details on ports and environment variables: [Installation](/en/getting-started/installation/); on the features [LCM Remote](/en/guides/remote/) and [MCP interface](/en/guides/mcp/).

## Versioning & build number

- **Semantic version:** the `VERSION` file in the project root (e.g. `1.2.0`) - maintained by hand.
- **Build number:** the `.buildnumber` file - increments automatically on every build target (Makefile target `bump-build`, runs exactly once per `make` invocation, even for `build-all`).
- Both values are injected into the `internal/version` package via `-ldflags -X`, together with the build timestamp.

Queryable via: `./lcm -version` (CLI), `GET /api/v1/system/info` (API), the web app footer, and `make version`.

**Update detection:** every installation keeps a `version.json` next to the database (created automatically on first start). On every start it is compared against the binary version - a difference means "the binary was updated" and triggers the version-bound update migrations. Details and instructions: [Database & Migrations](/en/reference/database/).

## Build & cross-compiling

`make build` runs the pipeline: `npm audit` → `vite build` → `govulncheck` → `go build`. If a security check fails, the build aborts.

Because the SQLite driver is CGO-free, cross-compiling works without toolchains - including ARM:

```sh
make build-linux         # bin/lcm-linux-amd64
make build-linux-arm64   # bin/lcm-linux-arm64   (z.B. Raspberry Pi, ARM-Server)
make build-windows       # bin/lcm-windows-amd64.exe
make build-macos         # bin/lcm-darwin-arm64  (Apple Silicon)
make build-macos-intel   # bin/lcm-darwin-amd64
make build-all           # alle Plattformen
```

## Operating with Docker

As an alternative to a system service there is a hardened Alpine container image (non-root, read-only; the pre-built binary is copied in) plus a Compose example with a `./data` volume for config.json/app.db/version.json - complete guide: [Docker](/en/guides/docker/). Short version: `make docker-build && docker compose up -d`.

## Operating as a system service

The binary is self-initializing (config + DB are created on first start) - ideal for services.

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

**Windows:** `sc.exe create LCM binPath= "C:\lcm\lcm.exe" start= auto` (or more conveniently with [NSSM](https://nssm.cc)).

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

The binary handles SIGINT/SIGTERM with a graceful shutdown.

## Crash resilience & self-healing

LCM manages other servers and parses their output. A single server answering
unexpectedly must never take down the management of all the others. Protection
is therefore layered - from "catch the error" to "restart in a controlled way
if in doubt".

### Layer 1 - catching panics

In Go an unhandled panic **always terminates the entire process**, even from an
arbitrary background goroutine. All execution paths are secured:

| Path | Protection |
|---|---|
| HTTP (UI/REST, agent gateway, MCP) | Fiber's `recover` middleware, first in each chain |
| Background goroutines (job runners, workers, listeners) | [`internal/safego`](https://gitlab.techeve.de/techeve/lcm/-/tree/community/internal/safego) - `safego.Go` / `safego.GoCleanup` |
| Scheduled runs (cron) | `cron.Recover` chain in the scheduler |
| MQTT hooks + the agents' WebSocket connection | `safego.Recover` per hook |

The MQTT path needs its own protection because it runs **outside** any HTTP
middleware: the connection is handed to a separate goroutine on WebSocket
upgrade, and neither the broker library nor the underlying HTTP server recovers
anything there. The authentication hook is also reachable by **unauthenticated**
packets. All hooks therefore work *fail-closed*: a caught panic counts as a
rejection, never as approval.

Job runners add a second aspect: a job holds a **lock on its server**. If the
runner aborts without finishing the job, that server would be blocked for all
further actions. `safego.GoCleanup` therefore closes the job as failed - the
lock is released and the error is visible in the job history.

### Layer 2 - health checks

`GET /api/v1/health` checks what the service actually needs: a **database
ping**. Without a database LCM cannot do anything useful, yet it would still
accept HTTP - so a plain "ok" answer would be worthless.

- **not signed in:** only `{"status":"ok"}` or HTTP 503 with
  `{"status":"unhealthy"}`
- **signed in:** plus diagnostics (`fail_streak`, `panics_total`,
  `panics_recent`, `watchdog_active`, `uptime_seconds`)

Error texts stay reserved for signed-in users - they may contain paths or
database internals.

### Layer 3 - self-restart

A crashed process is restarted by systemd anyway. The harder cases are those
where the process **keeps running but no longer works** - exactly what the
self-monitoring covers:

| Situation | Reaction |
|---|---|
| Database persistently unreachable (5 consecutive checks) | controlled restart |
| Accumulating caught panics (10 within 15 minutes) | controlled restart |
| Process fully hung/blocked | systemd watchdog steps in (heartbeat stops) |
| Brief hiccup (e.g. SQLite file locked during a backup) | **no** restart - the counter resets on success |

The watchdog is deliberately wired so the heartbeat (`WATCHDOG=1`) is sent
**only while the database is reachable**. If it stops, systemd terminates the
service after `WatchdogSec` and restarts it. This additionally covers the case
where the monitoring goroutine itself blocks.

When LCM restarts itself it exits with **code 70**; a staged backup restore uses
**42**. Both are unambiguous in the journal.

:::note[Without a service manager]
When LCM runs in the foreground (development, manual start), it stays down after
a self-restart - nobody brings it back. This is intentional and clearly logged;
a process continuing with corrupted state would be the worse option. Under
systemd and in Docker (`restart: unless-stopped`) the service restarts
automatically.
:::

### Operational hardening (systemd)

The shipped unit sets:

- `Type=notify` - the service only counts as started once it is truly ready
- `Restart=always`, `RestartSec=5`
- `WatchdogSec=90` - heartbeat supervision
- `StartLimitIntervalSec=300` / `StartLimitBurst=5` - crash-loop brake: if the
  service fails to come up after 5 attempts within 5 minutes it stays stopped
  instead of straining the machine. Visible via `systemctl status lcm`.

A clean shutdown emits the log line `=== LCM-Dienst wird beendet ===`. **If it
is missing before a start marker, it was a crash** - the quickest way to tell a
restart from a crash in the journal.
