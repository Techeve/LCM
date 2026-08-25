package migrations

import "gorm.io/gorm"

// v1.8.0 macht den Katalog bekannter Paketquellen mehrsprachig: die neue Spalte
// package_manager legt fest, FÜR WELCHE Paketverwaltung ein Eintrag gilt
// (apt/dnf/zypper/pacman/apk). AutoMigrate legt die Spalte an; hier werden
// Altbestände auf "apt" gesetzt (bis dahin war der Katalog rein apt-basiert).
//
// Zusätzlich wird die Schreibweise des mitgelieferten Techeve-Eintrags
// korrigiert: „TechEve" → „Techeve" (Name und Beschreibung). Nur der
// unveränderte Auslieferungstext wird angefasst - hat der Betreiber Name oder
// Beschreibung selbst angepasst, bleibt seine Fassung unberührt. Idempotent.
func init() {
	Register(Migration{
		Version: "1.8.0",
		Name:    "1.8.0-known-repo-package-manager",
		Run: func(tx *gorm.DB) error {
			// Altbestand ohne gesetzte Paketverwaltung → apt.
			if err := tx.Exec(
				`UPDATE known_repos SET package_manager = 'apt' WHERE package_manager IS NULL OR package_manager = ''`,
			).Error; err != nil {
				return err
			}
			// Techeve-Schreibweise korrigieren, aber nur den Auslieferungstext.
			if err := tx.Exec(
				`UPDATE known_repos SET name = 'Techeve' WHERE "key" = 'techeve' AND name = 'TechEve'`,
			).Error; err != nil {
				return err
			}
			return tx.Exec(
				`UPDATE known_repos SET description = 'Techeve-Paketrepository (u. a. LCM) - https://repo.techeve.de'` +
					` WHERE "key" = 'techeve' AND description = 'TechEve-Paketrepository (u. a. LCM) - https://repo.techeve.de'`,
			).Error
		},
	})
}
