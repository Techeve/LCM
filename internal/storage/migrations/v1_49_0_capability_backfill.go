package migrations

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"LCM/internal/core/domain"
)

// v1.49.0 führt die Betriebsfähigkeit der Server ein (server_capabilities).
// Die Tabelle legt AutoMigrate an, sie ist aber leer: Ohne Zeile wüsste die
// Stillstandsprüfung nicht, wann ein Server zuletzt erfolgreich aktualisiert
// wurde, und jeder bestehende Server stünde unmittelbar nach dem Update als
// „Paketstand veraltet" da.
//
// Deshalb bekommt jeder Server, der schon einen erfolgreichen Paket-Job hatte,
// eine Zeile `packages` im Zustand ok, mit dem Ende dieses Jobs als letztem
// Erfolg. Server ohne solchen Job bleiben ohne Zeile und zählen damit ab ihrer
// Anlage als veraltet (packagesStaleness) - das ist gewollt: Läufe, die nie
// stattfinden, machen LCM handlungsunfähig, und genau das soll die
// Stillstandsprüfung zeigen. Idempotent: Eine vorhandene Zeile bleibt
// unangetastet.
func init() {
	Register(Migration{
		Version: "1.49.0",
		Name:    "1.49.0-capability-packages-backfill",
		Run:     backfillPackagesCapability,
	})
}

func backfillPackagesCapability(tx *gorm.DB) error {
	var serverIDs []uint
	if err := tx.Model(&domain.Server{}).Pluck("id", &serverIDs).Error; err != nil {
		return err
	}
	jobTypes := packageJobTypes()
	for _, id := range serverIDs {
		var last domain.Job
		// Nur die Spalten, die gebraucht werden: Der Output ist verschlüsselt
		// und groß, hier ohne Nutzen.
		res := tx.Select("id", "finished_at").
			Where("server_id = ? AND status = ? AND type IN ? AND finished_at IS NOT NULL",
				id, domain.JobStatusSuccess, jobTypes).
			Order("finished_at DESC").Limit(1).Find(&last)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			continue
		}
		row := domain.ServerCapability{
			ServerID: id, Category: domain.CapabilityPackages,
			State: domain.CapabilityOK, LastSuccessAt: last.FinishedAt,
		}
		// DoNothing: Eine bereits geführte Zeile ist genauer als der Rückblick.
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
			return err
		}
	}
	return nil
}

// packageJobTypes sind die Job-Typen der Kategorie packages.
func packageJobTypes() []string {
	return []string{
		domain.RuleTypeUpdate, domain.RuleTypePackages, domain.RuleTypeSecurity,
		domain.RuleTypePackageScan, domain.RuleTypeAutoremove,
	}
}
