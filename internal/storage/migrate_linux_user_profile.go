package storage

import (
	"gorm.io/gorm"

	"LCM/internal/core/domain"
)

// migrateLinuxUserProfiles ordnet bestehende Linux-Benutzer den mitgelieferten
// Berechtigungsprofilen zu: „sudo" wird zum Voll-Administrator, alles andere
// zum Standardbenutzer.
//
// Damit ändert sich auf keinem Server ein Recht - die beiden Profile bilden
// genau die zwei Zustände ab, die es vorher gab. Ohne diesen Schritt stünden
// alle Bestandsbenutzer ohne Profil da, und der erste Sync nach dem Update
// nähme den sudo-Berechtigten ihre Rechte.
//
// Läuft NACH dem Seed der eingebauten Profile (Seed legt sie an) und ist
// idempotent: Nur Benutzer ohne Profil werden angefasst.
func migrateLinuxUserProfiles(db *gorm.DB) error {
	profileIDs := map[string]uint{}
	for _, slug := range []string{domain.ProfileSlugFullAdmin, domain.ProfileSlugStandard} {
		var profile domain.PrivilegeProfile
		if err := db.Where("slug = ?", slug).First(&profile).Error; err != nil {
			// Vor dem ersten Seed gibt es die Profile noch nicht - dann ist
			// auch nichts zuzuordnen.
			return nil
		}
		profileIDs[slug] = profile.ID
	}
	if err := db.Model(&domain.LinuxUser{}).
		Where("default_profile_id IS NULL AND sudo = ?", true).
		Update("default_profile_id", profileIDs[domain.ProfileSlugFullAdmin]).Error; err != nil {
		return err
	}
	return db.Model(&domain.LinuxUser{}).
		Where("default_profile_id IS NULL AND sudo = ?", false).
		Update("default_profile_id", profileIDs[domain.ProfileSlugStandard]).Error
}
