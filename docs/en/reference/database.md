---
sidebar:
  order: 23
title: Database & Migrations
description: SQLite setup, GORM auto-migration, the version-based update migration system, entities, the repository pattern, and tests.
---

## Setup

The application uses **SQLite** via the CGO-free driver `modernc.org/sqlite` (GORM binding: `github.com/glebarez/sqlite`). No CGO means: cross-compiling for Linux/Windows/macOS with a simple `GOOS=... go build`.

The connection (`internal/storage/database.go`) enables production-relevant pragmas:

- `journal_mode(WAL)` - concurrent readers do not block writers
- `busy_timeout(5000)` - waits instead of an immediate `database is locked`
- `foreign_keys(1)` - referential integrity

The DB path comes from `config.json` (`database_path`, default `app.db` next to the binary). Tests use `:memory:`.

## Migrations

The template uses **GORM auto-migration**: on startup `storage.Migrate()` reconciles the schema with the registered structs (new tables/columns/indexes are created; columns are never dropped).

Register a new entity:

```go
// internal/storage/database.go
func Migrate(db *gorm.DB) error {
    return db.AutoMigrate(
        &domain.User{},
        // ...
        &domain.MeineEntitaet{},   // <- hier ergänzen
    )
}
```

For everything AutoMigrate cannot do - rewriting data, backfills, destructive rebuilds - there is the built-in update migration system (next section).

### Structural pre-migrations (before AutoMigrate)

Some rebuilds cannot be done by SQLite via `ALTER` and cannot be detected by AutoMigrate - above all the **type change of a primary key**. Such steps run as an idempotent function **before** `autoMigrate()` in `storage.Migrate()`; they detect the old schema themselves and are a no-op on fresh or already-migrated databases. Model: `migrateRulesToSchedules` (checking a schema column) and `migrateToUUID`.

**UUID migration (v0.2.0, `internal/storage/migrate_uuid.go`):** The log and inventory tables (`jobs`, `audit_logs`, `packages`, `ssh_sessions`, `ssh_commands`; since v0.10.0 following the same pattern also `alert_events`, see `internal/storage/migrate_alert_events.go`) carry **UUID primary keys** instead of sequential numbers - not guessable, and without revealing volume. The migration reads the old rows, drops the tables (child before parent), recreates them in the new schema, and writes the rows back with new UUIDs; the foreign keys between them (`ssh_sessions.job_id`, `ssh_commands.ssh_session_id`) are remapped consistently - lossless, all in **one** transaction (`defer_foreign_keys`). Because UUIDs are not sortable, the repositories sort chronologically by `created_at`/`opened_at` with `rowid` as a unique tiebreaker; the audit log gets a monotonic `seq` column (carried over from the old id) that preserves the order of the hash chain.

## Update migration system (version-based)

Every installation keeps a **version file** `version.json` next to the database. It records which version of the application last ran there:

```json
{
  "version": "1.0.0",
  "build": "2",
  "updated_at": "2026-07-03T20:35:05Z"
}
```

On startup (after `Migrate()`, before `Seed()`) `storage.RunUpdateMigrations()` (`internal/storage/update.go`; the individual steps live under `internal/storage/migrations/`) runs and compares the file with the binary version:

| Situation | Behavior |
|---|---|
| **First start** (no DB, no version file) | `version.json` is created with the binary version. Migrations do not run - the fresh DB is already current (the registry is logged as a *baseline*). |
| **Same version** | Nothing to do. |
| **Newer binary version** (update applied) | The update is logged (`update erkannt - von 1.0.0 auf 1.2.0`). All not-yet-run migrations up to and including the new version are executed in SemVer order. Afterwards `version.json` is advanced. |
| **DB present, but no version file** | An update from a state before the file was introduced - all pending migrations run, and the file is created. |

**Skipped versions are not a problem:** jumping from 1.0.0 straight to 1.3.0 runs the migrations for 1.1.0, 1.2.0, and 1.3.0 one after another. Each migration runs in its own transaction together with its log entry in the `update_migrations` table - if it fails, the start aborts and nothing is half-applied. The DB log is the second safeguard alongside the version file: even if `version.json` is deleted, no migration runs twice.

### The migrations folder

The migration scripts live in `internal/storage/migrations/` - **one file per version** that needs data adjustments:

```
internal/storage/migrations/
├── migrations.go                  Rahmen: Migration-Typ, Registry, Register()
├── v0_3_0_package_manager.go      v0.3.0: Paketmanager-Feld backfillen
├── v0_4_0_cve.go                  v0.4.0: CVE-Scan aktivieren (Settings-Defaults)
└── v0_5_0_storage.go              v0.5.0: Speicher-Verlauf-Retention (90-365 Tage)
```

Over time more and more scripts are added; the SemVer sorting guarantees the correct order: an update from 1.0.0 to 1.3.0 first runs the patches for 1.1.0, then 1.2.0, then 1.3.0. An installation already on 1.3.0 skips 1.1.0/1.2.0 (noted in the log) and on the next update runs only the newer scripts. On a major-version jump, all scripts from the last installed version onward run accordingly.

### Writing an update migration

Typical flow when releasing a new version that needs data adjustments - as a concrete example the real v0.5.0 case (storage history: existing installations get the retention default):

1. Determine the target version: `make next-version` shows which version the release pipeline will compute from the Conventional Commits (the `VERSION` file itself is maintained automatically by the release bot, see [CI & Release](/en/reference/ci-release/)). A migration belongs to a `feat:` or `feat!:` commit - the target version is therefore the next minor or major version.
2. Make the schema change in the domain struct (the field `StorageHistoryRetentionDays` in `domain.Settings`) - AutoMigrate creates the new columns automatically on startup.
3. Create a new file in the migrations folder, `v0_5_0_storage.go`, that registers itself via `init()`:

```go
package migrations

func init() {
    Register(Migration{
        Version: "0.5.0",                          // Version, die diese Änderung einführt
        Name:    "0.5.0-storage-history-retention", // eindeutig & stabil (Dedup-Schlüssel)
        Run: func(tx *gorm.DB) error {
            // Bestehende Settings-Zeile auf den Mindestwert heben -
            // idempotent durch die präzise WHERE-Bedingung.
            return tx.Table("settings").
                Where("storage_history_retention_days IS NULL OR storage_history_retention_days < ?", 90).
                Update("storage_history_retention_days", 90).Error
        },
    })
}
```

4. `make build` - done. Every installation that starts the new binary detects the update from its `version.json`, runs the script exactly once, and advances the file. In the log:

```
INFO update erkannt - prüfe migrationen von=0.4.0 auf=0.5.0
INFO führe update-migration aus version=0.5.0 name=0.5.0-storage-history-retention
INFO update-migrationen abgeschlossen anzahl=1
```

**Rules:**

- `Version` is the version **the change is shipped with** - a migration declared for 1.2.0 does not run as long as the binary is 1.1.x (dev builds without a release version have no upper bound).
- `Name` is the dedup key: choose it uniquely and **never** change or reuse it.
- Never edit already-shipped migrations afterwards - instead add a new migration (a higher version) that corrects it.
- Write `Run` idempotently (precise `WHERE` conditions, check for column existence), even though the log prevents double execution.
- Access **removed legacy columns** (like `display_name`) via `tx.Table("users")`, not via the domain struct - it no longer knows about them.
- Data operations only - additive schema changes are done automatically by AutoMigrate beforehand.

Tested in `internal/storage/migrations_test.go`: fresh install, update across multiple versions (including order and the future lock), legacy install without a version file, dev builds, and SemVer comparison.

## Defining entities

Template conventions (see `internal/core/domain/`):

```go
type Note struct {
    ID        uint      `gorm:"primarykey" json:"id"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
    UserID    uint      `gorm:"index;not null" json:"user_id"`
    Title     string    `gorm:"not null" json:"title"`
    Secret    string    `json:"-"` // niemals an den Client serialisieren
}
```

- Explicit fields instead of `gorm.Model` (full control over JSON tags).
- `json:"-"` for anything that must never end up in an API response (hashes, secrets).
- n:m relations via `gorm:"many2many:join_table"` (example: `User.Roles`).
- Unique indexes over multiple columns: `gorm:"index:idx_name,unique"` on all participating fields (example: `StorageHistory` - unique per `server_id`+`day`).

## Encrypted columns (at-rest)

Secrets are never stored in plaintext in the DB. Two patterns:

- **Field-level AES-256-GCM** - the encrypted columns are registered centrally in `internal/storage/rotate.go` (`encryptedColumns`). These include, among others, `servers.private_key_enc`, the RouterOS login password `servers.login_password_enc`, `users.totp_secret_enc`, `linux_users.password_enc`, the system-mailer/CrowdSec fields in `global_settings` (`mail_password_enc`, `crowd_sec_lapi_password_enc`, `crowd_sec_console_key_enc`, `onboarding_key_enc`, `tls_key_pem_enc`, `default_ssh_password_enc`) and `notification_channels.secret_enc`. Such fields carry `json:"-"` in the domain struct, so they never leave the API.
- **GORM serializer `aesgcm`** - for large output (`jobs.output`, `ssh_commands.command`/`output`) and the server host/name. The server name additionally has a `name_bidx` column (blind index derived from the master key) for searching without plaintext.

The key for this is the **master key** (`lcm.key`, outside the DB). New encrypted columns **must** be registered in `rotate.go`, otherwise the rotation command `lcm rotate-db-key` will not cover them. Background and full list: [Security model](/en/reference/security-model/).

## Repository pattern

All GORM calls live in `internal/storage/repositories/`. One repository per aggregate:

```go
type NoteRepository struct{ db *gorm.DB }

func NewNoteRepository(db *gorm.DB) *NoteRepository { return &NoteRepository{db: db} }

func (r *NoteRepository) FindByID(id uint) (*domain.Note, error) {
    var note domain.Note
    if err := r.db.First(&note, id).Error; err != nil {
        return nil, translate(err) // gorm.ErrRecordNotFound -> ErrNotFound
    }
    return &note, nil
}
```

**Rules:**

- Never surface `gorm.ErrRecordNotFound` - always map it to `repositories.ErrNotFound` with `translate()`. Services/controllers check with `errors.Is`.
- Explicitly `Preload` the relations callers need (RBAC needs `Preload("Roles.Permissions")`, for example).
- Upserts as read-modify-write in the repository when a lock guarantees serialization (example: `ServerRepository.RecordStorageSample` - the per-server job lock prevents concurrent writers).
- No GORM type (`*gorm.DB`, clauses, …) ever leaves the repository layer.

## Seeding

`storage.Seed()` (in `seed.go`) runs on every start but only does something on the very first start (guard: `users.Count() > 0`). It creates permissions, roles, and the `system`/`admin` users; with the CLI flag `--demo` (deliberately not a config.json field) it additionally creates the demo data (`seed_demo.go`): example servers with packages, job histories, storage history, and CVE findings. Your own seed data belongs here, likewise idempotent.

## Testing with in-memory SQLite

The standard setup for integration tests (see `services_test.go`):

```go
db, _ := storage.Open(":memory:")
storage.Migrate(db)
storage.Seed(db, &config.Config{AdminInitialPassword: "test-admin-passwort", DemoMode: true})
repo := repositories.NewNoteRepository(db)
```

Each test thus gets a fresh, complete database in milliseconds - no fixtures, no cleanup, no test interference.
