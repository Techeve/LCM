package migrations

import "gorm.io/gorm"

// v2.2.2 stellt den Katalogeintrag des Techeve-Paketrepositorys auf die Suite
// „community" um: der Community-Kanal heißt auf dem Server nicht mehr „stable".
// „stable" bleibt vorerst mit gleichem Inhalt erreichbar, wird aber
// irgendwann abgeschaltet.
//
// Angefasst wird nur der unveränderte Auslieferungstext der Zeile - hat der
// Betreiber sie selbst angepasst, bleibt seine Fassung unberührt. Idempotent.
func init() {
	Register(Migration{
		Version: "2.2.2",
		Name:    "2.2.2-known-repo-techeve-community",
		Run: func(tx *gorm.DB) error {
			return tx.Exec(
				`UPDATE known_repos SET line = ?`+
					` WHERE "key" = 'techeve' AND line = ?`,
				"deb [signed-by=/etc/apt/keyrings/techeve.asc] https://repo.techeve.de community main",
				"deb [signed-by=/etc/apt/keyrings/techeve.asc] https://repo.techeve.de stable main",
			).Error
		},
	})
}
