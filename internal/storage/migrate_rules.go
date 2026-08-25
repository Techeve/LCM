package storage

import (
	"gorm.io/gorm"

	"LCM/internal/core/domain"
)

// migrateRulesToSchedules überführt Alt-Datenbestände in das Schedule-Modell:
// früher trug jede Rule ihren eigenen Cron-Ausdruck, heute hängen Rules an
// einem Schedule (mehrere Rules pro Zeitplan) oder sind Grundsatz-Regeln
// (Enforce), die bei jeder Verbindung geprüft und durchgesetzt werden.
//
//   - Alte Firewall-Rules werden zu Grundsatz-Regeln (genau dafür sind sie
//     gedacht: Zustand durchsetzen statt zeitgesteuert laufen).
//   - Alle anderen Rules bekommen einen eigenen Schedule mit ihrem
//     bisherigen Cron-Ausdruck und Aktiv-Status.
//
// Idempotent: bereits migrierte Rules (schedule_id gesetzt, enforce oder
// cron_expr geleert) werden übersprungen; frische Datenbanken haben die
// Alt-Spalte cron_expr gar nicht mehr.
func migrateRulesToSchedules(db *gorm.DB) error {
	if !db.Migrator().HasColumn(&domain.Rule{}, "cron_expr") {
		return nil
	}
	type legacyRule struct {
		ID       uint
		GroupID  uint
		Name     string
		Type     string
		CronExpr string
		Enabled  bool
		IsSystem bool
	}
	var legacy []legacyRule
	if err := db.Raw(`SELECT id, group_id, name, type, cron_expr, enabled, is_system FROM rules
		WHERE cron_expr IS NOT NULL AND cron_expr != '' AND schedule_id IS NULL AND enforce = 0`).
		Scan(&legacy).Error; err != nil {
		return err
	}
	for _, r := range legacy {
		if r.Type == domain.RuleTypeFirewall {
			if err := db.Exec(`UPDATE rules SET enforce = 1, cron_expr = '' WHERE id = ?`, r.ID).Error; err != nil {
				return err
			}
			continue
		}
		sched := domain.Schedule{
			GroupID: r.GroupID, Name: r.Name, CronExpr: r.CronExpr,
			Enabled: r.Enabled, IsSystem: r.IsSystem,
		}
		if err := db.Create(&sched).Error; err != nil {
			return err
		}
		if err := db.Exec(`UPDATE rules SET schedule_id = ?, cron_expr = '' WHERE id = ?`, sched.ID, r.ID).Error; err != nil {
			return err
		}
	}
	return nil
}
