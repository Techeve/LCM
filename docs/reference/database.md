---
sidebar:
  order: 23
title: Datenbank & Migrationen
description: SQLite-Setup, GORM-AutoMigration, das versionsbasierte Update-Migrationssystem, Entitäten, Repository-Pattern und Tests.
---

## Setup

Die Anwendung nutzt **SQLite** über den CGO-freien Treiber `modernc.org/sqlite` (GORM-Anbindung: `github.com/glebarez/sqlite`). Kein CGO bedeutet: Cross-Compiling für Linux/Windows/macOS mit einem simplen `GOOS=... go build`.

Die Verbindung (`internal/storage/database.go`) aktiviert produktionsrelevante Pragmas:

- `journal_mode(WAL)` - parallele Leser blockieren Schreiber nicht
- `busy_timeout(5000)` - wartet statt sofortigem `database is locked`
- `foreign_keys(1)` - referentielle Integrität

Der DB-Pfad kommt aus der `config.json` (`database_path`, Default `app.db` neben dem Binary). Tests nutzen `:memory:`.

## Migrationen

Das Template verwendet **GORM-AutoMigration**: Beim Start gleicht `storage.Migrate()` das Schema mit den registrierten Structs ab (neue Tabellen/Spalten/Indizes werden angelegt; Spalten werden nie gelöscht).

Neue Entität registrieren:

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

Für alles, was AutoMigrate nicht kann - Daten umschreiben, Backfills, destruktive Umbauten - gibt es das eingebaute Update-Migrationssystem (nächster Abschnitt).

### Strukturelle Vor-Migrationen (vor AutoMigrate)

Manche Umbauten kann SQLite nicht per `ALTER` und AutoMigrate nicht erkennen - allen voran der **Typwechsel eines Primärschlüssels**. Solche Schritte laufen als idempotente Funktion **vor** `autoMigrate()` in `storage.Migrate()`; sie erkennen das Alt-Schema selbst und sind auf frischen bzw. bereits migrierten Datenbanken ein No-op. Vorbild: `migrateRulesToSchedules` (Schema-Spalte prüfen) und `migrateToUUID`.

**UUID-Migration (v0.2.0, `internal/storage/migrate_uuid.go`):** Die Protokoll- und Bestandstabellen (`jobs`, `audit_logs`, `packages`, `ssh_sessions`, `ssh_commands`; seit v0.10.0 nach demselben Muster auch `alert_events`, siehe `internal/storage/migrate_alert_events.go`) tragen **UUID-Primärschlüssel** statt fortlaufender Zahlen - nicht ratbar, ohne Mengengerüst. Die Migration liest die Alt-Zeilen, verwirft die Tabellen (Kind vor Eltern), legt sie im neuen Schema an und schreibt die Zeilen mit neuen UUIDs zurück; die Fremdschlüssel zwischen ihnen (`ssh_sessions.job_id`, `ssh_commands.ssh_session_id`) werden konsistent umgesetzt - verlustfrei, alles in **einer** Transaktion (`defer_foreign_keys`). Weil UUIDs nicht sortierbar sind, sortieren die Repositories chronologisch nach `created_at`/`opened_at` mit `rowid` als eindeutigem Tiebreaker; das Audit-Log erhält eine monotone `seq`-Spalte (aus der alten id übernommen), die die Reihenfolge der Hash-Kette bewahrt.

## Update-Migrationssystem (versionsbasiert)

Jede Installation führt eine **Versionsdatei** `version.json` neben der Datenbank. Sie hält fest, welche Version der Anwendung dort zuletzt lief:

```json
{
  "version": "1.0.0",
  "build": "2",
  "updated_at": "2026-07-03T20:35:05Z"
}
```

Beim Start (nach `Migrate()`, vor `Seed()`) läuft `storage.RunUpdateMigrations()` (`internal/storage/update.go`; die einzelnen Schritte liegen unter `internal/storage/migrations/`) und vergleicht die Datei mit der Version des Binaries:

| Situation | Verhalten |
|---|---|
| **Erststart** (keine DB, keine Versionsdatei) | `version.json` wird mit der Binary-Version angelegt. Migrationen laufen nicht - die frische DB ist bereits aktuell (Registry wird als *Baseline* protokolliert). |
| **Gleiche Version** | Nichts zu tun. |
| **Neuere Binary-Version** (Update eingespielt) | Update wird geloggt (`update erkannt - von 1.0.0 auf 1.2.0`). Alle noch nicht gelaufenen Migrationen bis einschließlich der neuen Version werden in SemVer-Reihenfolge ausgeführt. Danach wird `version.json` fortgeschrieben. |
| **DB vorhanden, aber keine Versionsdatei** | Update von einem Stand vor Einführung der Datei - alle ausstehenden Migrationen laufen, die Datei wird angelegt. |

**Übersprungene Versionen sind kein Problem:** Beim Sprung von 1.0.0 direkt auf 1.3.0 laufen die Migrationen für 1.1.0, 1.2.0 und 1.3.0 nacheinander. Jede Migration läuft in einer eigenen Transaktion zusammen mit ihrem Protokoll-Eintrag in der Tabelle `update_migrations` - schlägt sie fehl, bricht der Start ab und nichts ist halb angewendet. Das DB-Protokoll ist die zweite Sicherung neben der Versionsdatei: Selbst wenn `version.json` gelöscht wird, läuft keine Migration doppelt.

### Der Migrationsordner

Die Migrationsskripte liegen in `internal/storage/migrations/` - **eine Datei pro Version**, die Datenanpassungen braucht:

```
internal/storage/migrations/
├── migrations.go                  Rahmen: Migration-Typ, Registry, Register()
├── v0_3_0_package_manager.go      v0.3.0: Paketmanager-Feld backfillen
├── v0_4_0_cve.go                  v0.4.0: CVE-Scan aktivieren (Settings-Defaults)
└── v0_5_0_storage.go              v0.5.0: Speicher-Verlauf-Retention (90-365 Tage)
```

Mit der Zeit kommen immer mehr Skripte dazu; die SemVer-Sortierung garantiert die korrekte Reihenfolge: Ein Update von 1.0.0 auf 1.3.0 führt erst die Patches für 1.1.0 aus, dann 1.2.0, dann 1.3.0. Eine Installation, die bereits auf 1.3.0 steht, überspringt 1.1.0/1.2.0 (im Protokoll vermerkt) und führt beim nächsten Update nur noch die neueren Skripte aus. Bei einem Major-Version-Sprung laufen entsprechend alle Skripte ab der zuletzt installierten Version.

### Eine Update-Migration schreiben

Typischer Ablauf beim Release einer neuen Version, die Datenanpassungen braucht - als konkretes Beispiel der echte v0.5.0-Fall (Speicher-Verlauf: bestehende Installationen bekommen den Retention-Default):

1. Ziel-Version ermitteln: `make next-version` zeigt, welche Version die Release-Pipeline aus den Conventional Commits berechnen wird (die `VERSION`-Datei selbst pflegt der Release-Bot automatisch, siehe [CI & Release](/reference/ci-release/)). Eine Migration gehört zu einem `feat:`- oder `feat!:`-Commit - die Ziel-Version ist also die nächste Minor- bzw. Major-Version.
2. Schema-Änderung im Domain-Struct machen (Feld `StorageHistoryRetentionDays` in `domain.Settings`) - die neuen Spalten legt AutoMigrate beim Start automatisch an.
3. Neue Datei im Migrationsordner anlegen, `v0_5_0_storage.go`, die sich per `init()` registriert:

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

4. `make build` - fertig. Jede Installation, die das neue Binary startet, erkennt das Update anhand ihrer `version.json`, führt das Skript genau einmal aus und schreibt die Datei fort. Im Log:

```
INFO update erkannt - prüfe migrationen von=0.4.0 auf=0.5.0
INFO führe update-migration aus version=0.5.0 name=0.5.0-storage-history-retention
INFO update-migrationen abgeschlossen anzahl=1
```

**Regeln:**

- `Version` ist die Version, **mit der die Änderung ausgeliefert wird** - eine für 1.2.0 deklarierte Migration läuft nicht, solange das Binary 1.1.x ist (Dev-Builds ohne Release-Version haben keine Obergrenze).
- `Name` ist der Dedup-Schlüssel: eindeutig wählen und **niemals** ändern oder wiederverwenden.
- Bereits ausgelieferte Migrationen nie nachträglich editieren - stattdessen eine neue Migration (höhere Version) anlegen, die korrigiert.
- `Run` idempotent formulieren (präzise `WHERE`-Bedingungen, Spalten-Existenz prüfen), auch wenn das Protokoll Doppelausführung verhindert.
- Auf **entfernte Alt-Spalten** (wie `display_name`) über `tx.Table("users")` zugreifen, nicht über das Domain-Struct - das kennt sie nicht mehr.
- Nur Datenoperationen - additive Schema-Änderungen macht AutoMigrate vorher automatisch.

Getestet in `internal/storage/migrations_test.go`: Erstinstallation, Update über mehrere Versionen (inkl. Reihenfolge und Zukunfts-Sperre), Legacy-Installation ohne Versionsdatei, Dev-Builds und SemVer-Vergleich.

## Entitäten definieren

Konventionen des Templates (siehe `internal/core/domain/`):

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

- Explizite Felder statt `gorm.Model` (volle Kontrolle über JSON-Tags).
- `json:"-"` für alles, was nie in einer API-Response landen darf (Hashes, Secrets).
- n:m-Beziehungen via `gorm:"many2many:join_table"` (Beispiel: `User.Roles`).
- Unique-Indizes über mehrere Spalten: `gorm:"index:idx_name,unique"` auf allen beteiligten Feldern (Beispiel: `StorageHistory` - eindeutig je `server_id`+`day`).

## Verschlüsselte Spalten (At-Rest)

Geheimnisse liegen nie im Klartext in der DB. Zwei Muster:

- **Feldweise AES-256-GCM** - die verschlüsselten Spalten sind zentral in `internal/storage/rotate.go` registriert (`encryptedColumns`). Dazu zählen u.&nbsp;a. `servers.private_key_enc`, das RouterOS-Login-Passwort `servers.login_password_enc`, `users.totp_secret_enc`, `linux_users.password_enc`, die System-Mailer-/CrowdSec-Felder in `global_settings` (`mail_password_enc`, `crowd_sec_lapi_password_enc`, `crowd_sec_console_key_enc`, `onboarding_key_enc`, `tls_key_pem_enc`, `default_ssh_password_enc`) und `notification_channels.secret_enc`. Solche Felder tragen im Domain-Struct `json:"-"`, verlassen die API also nie.
- **GORM-Serializer `aesgcm`** - für großvolumige Ausgaben (`jobs.output`, `ssh_commands.command`/`output`) und Server-Host/-Name. Der Servername hat zusätzlich eine `name_bidx`-Spalte (Blindindex aus dem Master-Key) für die Suche ohne Klartext.

Der Schlüssel dafür ist der **Master-Key** (`lcm.key`, außerhalb der DB). Neue verschlüsselte Spalten **müssen** in `rotate.go` registriert werden, sonst erfasst sie das Rotations-Kommando `lcm rotate-db-key` nicht. Hintergrund und vollständige Liste: [Sicherheitsmodell](/reference/security-model/).

## Repository-Pattern

Alle GORM-Aufrufe leben in `internal/storage/repositories/`. Ein Repository pro Aggregat:

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

**Regeln:**

- `gorm.ErrRecordNotFound` niemals nach außen geben - immer mit `translate()` auf `repositories.ErrNotFound` mappen. Services/Controller prüfen mit `errors.Is`.
- Beziehungen, die die Aufrufer brauchen, explizit `Preload`-en (RBAC braucht z.B. `Preload("Roles.Permissions")`).
- Upserts als Read-Modify-Write im Repository, wenn ein Lock die Serialisierung garantiert (Beispiel: `ServerRepository.RecordStorageSample` - der Job-Lock pro Server verhindert parallele Schreiber).
- Kein GORM-Typ (`*gorm.DB`, Clauses, …) verlässt jemals die Repository-Schicht.

## Seeding

`storage.Seed()` (in `seed.go`) läuft bei jedem Start, tut aber nur beim allerersten Start etwas (Guard: `users.Count() > 0`). Es legt Permissions, Rollen und die User `system`/`admin` an; mit dem CLI-Flag `--demo` (bewusst kein config.json-Feld) zusätzlich die Demo-Daten (`seed_demo.go`): Beispiel-Server samt Paketen, Job-Historien, Speicher-Verlauf und CVE-Funden. Eigene Seed-Daten gehören hier hinein, ebenfalls idempotent.

## Testen mit In-Memory-SQLite

Das Standard-Setup für Integrationstests (siehe `services_test.go`):

```go
db, _ := storage.Open(":memory:")
storage.Migrate(db)
storage.Seed(db, &config.Config{AdminInitialPassword: "test-admin-passwort", DemoMode: true})
repo := repositories.NewNoteRepository(db)
```

Jeder Test bekommt so eine frische, vollständige Datenbank in Millisekunden - keine Fixtures, kein Aufräumen, keine Test-Interferenz.
