package migrations

import (
	"time"

	"gorm.io/gorm"
)

// v1.3.0 nimmt das TechEve-Paketrepository (u. a. LCM) in den Katalog bekannter
// Paketquellen auf. DefaultKnownRepos() deckt nur FRISCHE Installationen ab -
// ein bereits befüllter Katalog wird beim Seeden nicht mehr angefasst. Diese
// Migration ergänzt den Eintrag daher in bestehenden Installationen, sofern er
// (per Schlüssel) noch nicht vorhanden ist. Idempotent.
func init() {
	Register(Migration{
		Version: "1.3.0",
		Name:    "1.3.0-known-repo-techeve",
		Run: func(tx *gorm.DB) error {
			var n int64
			if err := tx.Table("known_repos").Where(`"key" = ?`, "techeve").Count(&n).Error; err != nil {
				return err
			}
			if n > 0 {
				return nil // bereits vorhanden (oder vom Betreiber angelegt)
			}
			now := time.Now()
			return tx.Exec(
				`INSERT INTO known_repos (created_at, updated_at, "key", name, description, key_url, line)`+
					` VALUES (?, ?, ?, ?, ?, ?, ?)`,
				now, now, "techeve", "TechEve",
				"TechEve-Paketrepository (u. a. LCM) - https://repo.techeve.de",
				"https://repo.techeve.de/repo-key.gpg",
				"deb [signed-by=/etc/apt/keyrings/techeve.asc] https://repo.techeve.de stable main",
			).Error
		},
	})
}
