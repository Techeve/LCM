package storage

import (
	"gorm.io/gorm"

	"LCM/internal/core/domain"
)

// migrateAlertRuleGroups überführt die einzelne Gruppenzuordnung einer
// Alarm-Regel (Spalte group_id) in die Mehrfachauswahl (Tabelle
// alert_rule_groups). Dieselbe Schwelle gilt in der Regel für mehrere
// Infrastruktur-Gruppen; vorher war dafür je Gruppe eine eigene, ansonsten
// identische Regel nötig.
//
// Idempotent: Die Alt-Spalte wird nach der Übernahme geleert, und frische
// Datenbanken haben sie gar nicht mehr.
func migrateAlertRuleGroups(db *gorm.DB) error {
	if !db.Migrator().HasColumn(&domain.AlertRule{}, "group_id") {
		return nil
	}
	type legacy struct {
		ID      uint
		GroupID uint
	}
	var rows []legacy
	if err := db.Raw(`SELECT id, group_id FROM alert_rules WHERE group_id IS NOT NULL AND group_id != 0`).
		Scan(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		if err := db.Exec(`INSERT OR IGNORE INTO alert_rule_groups (alert_rule_id, server_group_id) VALUES (?, ?)`,
			row.ID, row.GroupID).Error; err != nil {
			return err
		}
		if err := db.Exec(`UPDATE alert_rules SET group_id = NULL WHERE id = ?`, row.ID).Error; err != nil {
			return err
		}
	}
	return nil
}
