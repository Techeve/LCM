package migrations

import (
	"gorm.io/gorm"

	"LCM/internal/core/domain"
)

// v1.15.0 löst das Ja/Nein-Flag für den Enterprise-Paketkanal durch den
// Kanalnamen ab: seitdem gibt es drei Kanäle (community/beta/enterprise).
// AutoMigrate legt die neue Spalte an; hier wird der bisherige Stand
// übernommen - ein umgestellter Host war auf dem Enterprise-Kanal, alle
// anderen auf Community.
//
// Die alte Spalte bleibt liegen (SQLite kennt kein sauberes DROP COLUMN über
// gorm, und sie stört niemanden). Frische Installationen haben sie gar nicht
// erst - deshalb die Abfrage. Idempotent.
func init() {
	Register(Migration{
		Version: "1.15.0",
		Name:    "1.15.0-subscription-apt-channel",
		Run: func(tx *gorm.DB) error {
			if tx.Migrator().HasColumn(&domain.GlobalSettings{}, "subscription_apt_active") {
				if err := tx.Exec(
					`UPDATE global_settings SET subscription_apt_channel = ?`+
						` WHERE subscription_apt_active = 1`,
					domain.AptChannelEnterprise,
				).Error; err != nil {
					return err
				}
			}
			return tx.Exec(
				`UPDATE global_settings SET subscription_apt_channel = ?`+
					` WHERE subscription_apt_channel IS NULL OR subscription_apt_channel = ''`,
				domain.AptChannelCommunity,
			).Error
		},
	})
}
