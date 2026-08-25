package repositories

import (
	"gorm.io/gorm"

	"LCM/internal/core/domain"
)

// PrivilegeProfileRepository verwaltet die Berechtigungsprofile samt ihrer
// Regeln.
type PrivilegeProfileRepository struct {
	db *gorm.DB
}

func NewPrivilegeProfileRepository(db *gorm.DB) *PrivilegeProfileRepository {
	return &PrivilegeProfileRepository{db: db}
}

// withRules lädt ein Profil immer mit seinen drei Regelarten - ohne sie ist
// es nur eine Überschrift.
func (r *PrivilegeProfileRepository) withRules() *gorm.DB {
	return r.db.Preload("SudoRules", orderByID).
		Preload("EditRules", orderByID).
		Preload("PathRules", orderByID).
		// Die Bausteine samt Varianten: Ohne sie stünde beim Anwenden nur die
		// Referenz da, und die daraus entstehenden Regeln fehlten.
		Preload("BlockUses", orderByID).
		Preload("BlockUses.Block.Variants", orderByID)
}

// orderByID hält die Regeln in einer stabilen Reihenfolge.
func orderByID(db *gorm.DB) *gorm.DB { return db.Order("id") }

func (r *PrivilegeProfileRepository) FindAll() ([]domain.PrivilegeProfile, error) {
	var profiles []domain.PrivilegeProfile
	// Eingebaute zuerst: Sie bilden den Grundzustand ab und sind der
	// Bezugspunkt für alles, was jemand selbst anlegt.
	if err := r.withRules().Order("builtin DESC, name").Find(&profiles).Error; err != nil {
		return nil, err
	}
	return profiles, nil
}

func (r *PrivilegeProfileRepository) FindByID(id uint) (*domain.PrivilegeProfile, error) {
	var profile domain.PrivilegeProfile
	if err := r.withRules().First(&profile, id).Error; err != nil {
		return nil, translate(err)
	}
	return &profile, nil
}

func (r *PrivilegeProfileRepository) FindBySlug(slug string) (*domain.PrivilegeProfile, error) {
	var profile domain.PrivilegeProfile
	if err := r.withRules().Where("slug = ?", slug).First(&profile).Error; err != nil {
		return nil, translate(err)
	}
	return &profile, nil
}

func (r *PrivilegeProfileRepository) Create(p *domain.PrivilegeProfile) error {
	return r.db.Create(p).Error
}

// Update speichert Kopfdaten und Regeln in einem Zug: Die Regeln werden
// ersetzt, nicht zusammengeführt. Ein Profil beschreibt einen Soll-Zustand -
// eine entfernte Regel muss verschwinden und darf nicht als Rest
// zurückbleiben.
func (r *PrivilegeProfileRepository) Update(p *domain.PrivilegeProfile) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&domain.PrivilegeProfile{ID: p.ID}).
			Updates(map[string]any{
				"name": p.Name, "description": p.Description, "account_type": p.AccountType,
			}).Error; err != nil {
			return err
		}
		if err := deleteRulesOf(tx, p.ID); err != nil {
			return err
		}
		return createRulesOf(tx, p)
	})
}

// Delete entfernt ein Profil samt seiner Regeln.
func (r *PrivilegeProfileRepository) Delete(id uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := deleteRulesOf(tx, id); err != nil {
			return err
		}
		res := tx.Delete(&domain.PrivilegeProfile{}, id)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// deleteRulesOf räumt die Regeln eines Profils ab. SQLite setzt
// Fremdschlüssel-Kaskaden nicht in jedem Fall durch - deshalb ausdrücklich.
func deleteRulesOf(tx *gorm.DB, profileID uint) error {
	for _, model := range []any{
		&domain.ProfileSudoRule{}, &domain.ProfileEditRule{}, &domain.ProfilePathRule{},
		&domain.ProfileBlockUse{},
	} {
		if err := tx.Where("profile_id = ?", profileID).Delete(model).Error; err != nil {
			return err
		}
	}
	return nil
}

// createRulesOf legt die Regeln eines Profils an (IDs werden neu vergeben).
func createRulesOf(tx *gorm.DB, p *domain.PrivilegeProfile) error {
	for i := range p.SudoRules {
		p.SudoRules[i].ID, p.SudoRules[i].ProfileID = 0, p.ID
	}
	for i := range p.EditRules {
		p.EditRules[i].ID, p.EditRules[i].ProfileID = 0, p.ID
	}
	for i := range p.PathRules {
		p.PathRules[i].ID, p.PathRules[i].ProfileID = 0, p.ID
	}
	for i := range p.BlockUses {
		p.BlockUses[i].ID, p.BlockUses[i].ProfileID = 0, p.ID
		p.BlockUses[i].Block = nil // Referenz, nicht mitschreiben
	}
	if len(p.SudoRules) > 0 {
		if err := tx.Create(&p.SudoRules).Error; err != nil {
			return err
		}
	}
	if len(p.EditRules) > 0 {
		if err := tx.Create(&p.EditRules).Error; err != nil {
			return err
		}
	}
	if len(p.PathRules) > 0 {
		if err := tx.Create(&p.PathRules).Error; err != nil {
			return err
		}
	}
	if len(p.BlockUses) > 0 {
		if err := tx.Create(&p.BlockUses).Error; err != nil {
			return err
		}
	}
	return nil
}

// ---- Gesetzte Verzeichnisrechte ---------------------------------------------

// AppliedPathsForServer liefert die Pfade, auf denen LCM auf diesem Server
// ACLs gesetzt hat.
func (r *PrivilegeProfileRepository) AppliedPathsForServer(serverID uint) ([]domain.AppliedProfilePath, error) {
	var rows []domain.AppliedProfilePath
	err := r.db.Where("server_id = ?", serverID).Order("id").Find(&rows).Error
	return rows, err
}

// ReplaceAppliedPaths schreibt den neuen Stand für einen Server fort.
func (r *PrivilegeProfileRepository) ReplaceAppliedPaths(serverID uint, rows []domain.AppliedProfilePath) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("server_id = ?", serverID).Delete(&domain.AppliedProfilePath{}).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		return tx.Create(&rows).Error
	})
}
