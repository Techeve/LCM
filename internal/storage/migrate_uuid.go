package storage

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"LCM/internal/core/domain"
)

// migrateToUUID überführt die Protokoll- und Bestandstabellen von
// fortlaufenden Ganzzahl-IDs auf UUID-Primärschlüssel (eingeführt mit
// v0.2.0). Betroffen sind: jobs, audit_logs, packages, ssh_sessions,
// ssh_commands. Fremdschlüssel zwischen ihnen (ssh_sessions.job_id,
// ssh_commands.ssh_session_id) werden konsistent mitgezogen; kein
// Datensatz geht verloren.
//
// Warum als Vor-Migration (vor AutoMigrate)? SQLite kann den Typ einer
// PRIMARY-KEY-Spalte nicht per ALTER ändern, und GORMs AutoMigrate lässt
// eine bestehende INTEGER-id unangetastet. Die Tabellen müssen also
// vollständig neu aufgebaut werden - das erledigen wir hier, bevor
// AutoMigrate die Modelle abgleicht (danach sieht es das Zielschema und
// hat nichts mehr zu tun).
//
// Idempotent: Läuft nur, solange mindestens eine der Tabellen noch eine
// INTEGER-id besitzt. Auf einer frischen Datenbank (Tabellen existieren
// noch nicht) und nach erfolgter Migration ist es ein No-op.
//
// Ablauf in EINER Transaktion (atomar; defer_foreign_keys hält die
// Fremdschlüssel bis zum Commit konsistent):
//  1. Alt-Daten je Tabelle einlesen (Ganzzahl-IDs).
//  2. Für jobs und ssh_sessions eine Abbildung alte-id → UUID bilden
//     (die von Kindtabellen referenziert werden).
//  3. Kindtabellen zuerst, dann Elterntabellen verwerfen.
//  4. Tabellen im neuen Schema (TEXT-id) neu anlegen.
//  5. Zeilen mit neuen UUIDs und umgesetzten Fremdschlüsseln zurückschreiben.
//
// Für das Audit-Log wird die alte Ganzzahl-id als Seq übernommen, damit die
// manipulationssichere Hash-Kette ihre Reihenfolge behält (der Hash selbst
// enthält weder ID noch Seq und bleibt daher gültig).
func migrateToUUID(db *gorm.DB) error {
	if !needsUUIDMigration(db) {
		return nil
	}

	// --- 1. Alt-Daten einlesen (außerhalb der DDL, in Ruhe). ---
	var oldJobs []legacyJob
	var oldSessions []legacySession
	var oldCommands []legacyCommand
	var oldPackages []legacyPackage
	var oldAudit []legacyAudit
	if err := db.Table("jobs").Find(&oldJobs).Error; err != nil {
		return fmt.Errorf("uuid-migration: jobs lesen: %w", err)
	}
	if err := db.Table("ssh_sessions").Find(&oldSessions).Error; err != nil {
		return fmt.Errorf("uuid-migration: ssh_sessions lesen: %w", err)
	}
	if err := db.Table("ssh_commands").Find(&oldCommands).Error; err != nil {
		return fmt.Errorf("uuid-migration: ssh_commands lesen: %w", err)
	}
	if err := db.Table("packages").Find(&oldPackages).Error; err != nil {
		return fmt.Errorf("uuid-migration: packages lesen: %w", err)
	}
	if err := db.Table("audit_logs").Find(&oldAudit).Error; err != nil {
		return fmt.Errorf("uuid-migration: audit_logs lesen: %w", err)
	}

	// --- 2. Abbildung alte-id → UUID für referenzierte Tabellen. ---
	jobMap := make(map[int64]string, len(oldJobs))
	for _, j := range oldJobs {
		jobMap[j.ID] = uuid.NewString()
	}
	sessMap := make(map[int64]string, len(oldSessions))
	for _, s := range oldSessions {
		sessMap[s.ID] = uuid.NewString()
	}

	return db.Transaction(func(tx *gorm.DB) error {
		// Fremdschlüssel-Prüfung bis zum Commit aufschieben - der
		// Zwischenzustand (Tabellen im Umbau) verletzt sonst Constraints.
		if err := tx.Exec("PRAGMA defer_foreign_keys = ON").Error; err != nil {
			return err
		}

		// --- 3. Alte Tabellen verwerfen (Kind vor Eltern). ---
		for _, name := range []string{"ssh_commands", "ssh_sessions", "jobs", "packages", "audit_logs"} {
			if err := tx.Migrator().DropTable(name); err != nil {
				return fmt.Errorf("uuid-migration: %s verwerfen: %w", name, err)
			}
		}

		// --- 4. Neues Schema anlegen (Eltern vor Kind). ---
		if err := tx.Migrator().CreateTable(
			&domain.Job{}, &domain.SSHSession{}, &domain.SSHCommand{},
			&domain.Package{}, &domain.AuditLog{},
		); err != nil {
			return fmt.Errorf("uuid-migration: tabellen neu anlegen: %w", err)
		}

		// --- 5. Zeilen mit UUIDs und umgesetzten FKs zurückschreiben. ---
		jobs := make([]domain.Job, 0, len(oldJobs))
		for _, o := range oldJobs {
			jobs = append(jobs, domain.Job{
				ID: jobMap[o.ID], CreatedAt: o.CreatedAt, UpdatedAt: o.UpdatedAt,
				ServerID: o.ServerID, RuleID: o.RuleID, Type: o.Type, Name: o.Name,
				Status: o.Status, Output: o.Output, ExitCode: o.ExitCode,
				TriggeredBy: o.TriggeredBy, StartedAt: o.StartedAt, FinishedAt: o.FinishedAt,
			})
		}
		if err := createAll(tx, jobs); err != nil {
			return fmt.Errorf("uuid-migration: jobs schreiben: %w", err)
		}

		sessions := make([]domain.SSHSession, 0, len(oldSessions))
		for _, o := range oldSessions {
			sessions = append(sessions, domain.SSHSession{
				ID: sessMap[o.ID], CreatedAt: o.CreatedAt, ServerID: o.ServerID,
				JobID:   mapJobID(o.JobID, jobMap),
				Purpose: o.Purpose, Actor: o.Actor, Host: o.Host, User: o.User,
				OpenedAt: o.OpenedAt, ClosedAt: o.ClosedAt,
				CommandCount: o.CommandCount, HadError: o.HadError,
			})
		}
		if err := createAll(tx, sessions); err != nil {
			return fmt.Errorf("uuid-migration: ssh_sessions schreiben: %w", err)
		}

		commands := make([]domain.SSHCommand, 0, len(oldCommands))
		for _, o := range oldCommands {
			newSessID, ok := sessMap[o.SSHSessionID]
			if !ok {
				// verwaistes Kommando ohne Session - überspringen.
				continue
			}
			commands = append(commands, domain.SSHCommand{
				SSHSessionID: newSessID, Seq: o.Seq, StartedAt: o.StartedAt,
				DurationMs: o.DurationMs, Command: o.Command, Output: o.Output,
				ExitCode: o.ExitCode, Err: o.Err,
			})
		}
		if err := createAll(tx, commands); err != nil {
			return fmt.Errorf("uuid-migration: ssh_commands schreiben: %w", err)
		}

		packages := make([]domain.Package, 0, len(oldPackages))
		for _, o := range oldPackages {
			packages = append(packages, domain.Package{
				CreatedAt: o.CreatedAt, UpdatedAt: o.UpdatedAt, ServerRef: domain.ServerRef(o.ServerID),
				Name: o.Name, Version: o.Version, CandidateVersion: o.CandidateVersion,
				Security: o.Security,
			})
		}
		if err := createAll(tx, packages); err != nil {
			return fmt.Errorf("uuid-migration: packages schreiben: %w", err)
		}

		audit := make([]domain.AuditLog, 0, len(oldAudit))
		for _, o := range oldAudit {
			audit = append(audit, domain.AuditLog{
				// Seq = alte id bewahrt die Reihenfolge der Hash-Kette.
				Seq: uint(o.ID), CreatedAt: o.CreatedAt, Actor: o.Actor,
				Action: o.Action, Entity: o.Entity, EntityID: o.EntityID,
				Details: o.Details, PrevHash: o.PrevHash, Hash: o.Hash,
			})
		}
		if err := createAll(tx, audit); err != nil {
			return fmt.Errorf("uuid-migration: audit_logs schreiben: %w", err)
		}
		return nil
	})
}

// needsUUIDMigration meldet true, solange mindestens eine der betroffenen
// Tabellen noch eine INTEGER-id besitzt (Alt-Schema vor v0.2.0).
func needsUUIDMigration(db *gorm.DB) bool {
	for _, name := range []string{"jobs", "audit_logs", "packages", "ssh_sessions", "ssh_commands"} {
		if idColumnType(db, name) == "integer" {
			return true
		}
	}
	return false
}

// idColumnType liefert den deklarierten Typ der id-Spalte einer Tabelle
// in Kleinschreibung ("" wenn die Tabelle fehlt) - "integer" = Alt-Schema,
// "text" = bereits UUID.
func idColumnType(db *gorm.DB, table string) string {
	if !db.Migrator().HasTable(table) {
		return ""
	}
	var typ string
	db.Raw("SELECT type FROM pragma_table_info(?) WHERE name = 'id'", table).Scan(&typ)
	return strings.ToLower(typ)
}

// createAll schreibt Zeilen in Blöcken zurück (BeforeCreate vergibt die
// UUID, sofern ID leer ist). Leere Slices sind ein No-op.
func createAll[T any](tx *gorm.DB, rows []T) error {
	if len(rows) == 0 {
		return nil
	}
	return tx.CreateInBatches(rows, 200).Error
}

// mapJobID setzt eine (nullbare) alte Job-Referenz auf die neue UUID um.
func mapJobID(old *int64, jobMap map[int64]string) *string {
	if old == nil {
		return nil
	}
	newID, ok := jobMap[*old]
	if !ok {
		return nil
	}
	return &newID
}

// --- Alt-Schema-Strukturen (Ganzzahl-IDs) nur zum Einlesen. ---

type legacyJob struct {
	ID          int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
	ServerID    *uint
	RuleID      *uint
	Type        string
	Name        string
	Status      string
	Output      string
	ExitCode    *int
	TriggeredBy string
	StartedAt   *time.Time
	FinishedAt  *time.Time
}

func (legacyJob) TableName() string { return "jobs" }

type legacySession struct {
	ID           int64
	CreatedAt    time.Time
	ServerID     *uint
	JobID        *int64
	Purpose      string
	Actor        string
	Host         string
	User         string
	OpenedAt     time.Time
	ClosedAt     *time.Time
	CommandCount int
	HadError     bool
}

func (legacySession) TableName() string { return "ssh_sessions" }

type legacyCommand struct {
	ID           int64
	SSHSessionID int64
	Seq          int
	StartedAt    time.Time
	DurationMs   int64
	Command      string
	Output       string
	ExitCode     int
	Err          string
}

func (legacyCommand) TableName() string { return "ssh_commands" }

type legacyPackage struct {
	ID               int64
	CreatedAt        time.Time
	UpdatedAt        time.Time
	ServerID         uint
	Name             string
	Version          string
	CandidateVersion string
	Security         bool
}

func (legacyPackage) TableName() string { return "packages" }

type legacyAudit struct {
	ID        int64
	CreatedAt time.Time
	Actor     string
	Action    string
	Entity    string
	EntityID  uint
	Details   string
	PrevHash  string
	Hash      string
}

func (legacyAudit) TableName() string { return "audit_logs" }
