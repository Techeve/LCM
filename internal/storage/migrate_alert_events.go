package storage

import (
	"fmt"
	"time"

	"gorm.io/gorm"

	"LCM/internal/core/domain"
)

// migrateAlertEventsToUUID überführt die Alarm-Historie (alert_events) von
// der fortlaufenden Ganzzahl-ID auf einen UUID-Primärschlüssel (v0.10.0) -
// damit haben ALLE unbegrenzt wachsenden Tabellen (Jobs, Audit, SSH-Logs,
// Pakete, CVEs, Speicher-Verlauf, Alarm-Events) UUIDs als PK.
//
// Gleiche Mechanik wie migrateToUUID (v0.2.0): SQLite kann den Typ einer
// PRIMARY-KEY-Spalte nicht ändern, also wird die Tabelle vor AutoMigrate
// neu aufgebaut. Nichts referenziert alert_events per Fremdschlüssel, die
// Zeilen werden 1:1 mit frischen UUIDs übernommen. Idempotent: läuft nur,
// solange die id-Spalte noch INTEGER ist.
func migrateAlertEventsToUUID(db *gorm.DB) error {
	if idColumnType(db, "alert_events") != "integer" {
		return nil
	}

	var old []legacyAlertEvent
	if err := db.Table("alert_events").Find(&old).Error; err != nil {
		return fmt.Errorf("alert-uuid-migration: events lesen: %w", err)
	}

	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Migrator().DropTable("alert_events"); err != nil {
			return fmt.Errorf("alert-uuid-migration: tabelle verwerfen: %w", err)
		}
		if err := tx.Migrator().CreateTable(&domain.AlertEvent{}); err != nil {
			return fmt.Errorf("alert-uuid-migration: tabelle neu anlegen: %w", err)
		}
		events := make([]domain.AlertEvent, 0, len(old))
		for _, o := range old {
			events = append(events, domain.AlertEvent{
				CreatedAt: o.CreatedAt, RuleID: o.RuleID, ServerID: o.ServerID,
				RuleName: o.RuleName, ServerName: o.ServerName, GroupName: o.GroupName,
				Type: o.Type, Severity: o.Severity, Code: o.Code, Description: o.Description,
				Notified: o.Notified, NotifyError: o.NotifyError,
			})
		}
		if err := createAll(tx, events); err != nil {
			return fmt.Errorf("alert-uuid-migration: events schreiben: %w", err)
		}
		return nil
	})
}

// legacyAlertEvent ist das Alt-Schema (Ganzzahl-ID) nur zum Einlesen.
type legacyAlertEvent struct {
	ID          int64
	CreatedAt   time.Time
	RuleID      uint
	ServerID    *uint
	RuleName    string
	ServerName  string
	GroupName   string
	Type        string
	Severity    string
	Code        string
	Description string
	Notified    bool
	NotifyError string
}

func (legacyAlertEvent) TableName() string { return "alert_events" }
