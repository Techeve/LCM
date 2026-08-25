// Package repositories ist die Datenbank-Zugriffsschicht.
// Repositories kapseln GORM vollständig - Services kennen keine
// GORM-Details, sondern arbeiten nur mit Domain-Structs.
package repositories

import (
	"errors"

	"gorm.io/gorm"

	"LCM/internal/core/domain"
)

// ErrNotFound wird zurückgegeben, wenn eine Entität nicht existiert.
// Services/Controller prüfen dagegen mit errors.Is.
var ErrNotFound = errors.New("nicht gefunden")

func translate(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	return err
}

// UserRepository verwaltet User inkl. Rollen und Permissions.
type UserRepository struct {
	db *gorm.DB
}

func NewUserRepository(db *gorm.DB) *UserRepository {
	return &UserRepository{db: db}
}

// preload lädt Rollen samt Permissions - nötig für RBAC-Checks.
func (r *UserRepository) preload() *gorm.DB {
	return r.db.Preload("Roles.Permissions")
}

func (r *UserRepository) Create(user *domain.User) error {
	return r.db.Create(user).Error
}

func (r *UserRepository) Delete(id uint) error {
	// Alles, was am Konto hängt, stirbt mit ihm: API-Keys (auch widerrufene),
	// Rollen-Zuordnungen und offene Aktivierungslinks. Ohne das hielt jede
	// (auch längst widerrufene) Key-Zeile ihren Fremdschlüssel auf den
	// Benutzer und JEDES Löschen endete für immer mit einem nackten 500
	// (R2-052; Rollen-Zeilen waren R2-035). Ein Key ohne Konto hat keinen
	// Rechte-Kontext mehr; Erzeugung/Widerruf bleiben im Audit-Log.
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", id).Delete(&domain.APIKey{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", id).Delete(&domain.ActivationLink{}).Error; err != nil {
			return err
		}
		if err := tx.Exec("DELETE FROM user_roles WHERE user_id = ?", id).Error; err != nil {
			return err
		}
		res := tx.Delete(&domain.User{}, id)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	})
}

func (r *UserRepository) FindByID(id uint) (*domain.User, error) {
	var user domain.User
	if err := r.preload().First(&user, id).Error; err != nil {
		return nil, translate(err)
	}
	return &user, nil
}

func (r *UserRepository) FindByUsername(username string) (*domain.User, error) {
	var user domain.User
	// Username ist verschlüsselt (zufälliger Nonce) - Suche über den
	// deterministischen Blindindex.
	if err := r.preload().Where("username_bidx = ?", BlindIndex(username)).First(&user).Error; err != nil {
		return nil, translate(err)
	}
	return &user, nil
}

// FindByEmail sucht einen User über seine E-Mail-Adresse (case-insensitiv) -
// Grundlage des Self-Service-Passwort-Resets. Suche über den Blindindex
// (E-Mail ist verschlüsselt); BlindIndex normalisiert klein/getrimmt.
func (r *UserRepository) FindByEmail(email string) (*domain.User, error) {
	var user domain.User
	if err := r.preload().Where("email_bidx = ?", BlindIndex(email)).First(&user).Error; err != nil {
		return nil, translate(err)
	}
	return &user, nil
}

func (r *UserRepository) FindAll() ([]domain.User, error) {
	var users []domain.User
	if err := r.preload().Order("id").Find(&users).Error; err != nil {
		return nil, err
	}
	return users, nil
}

// SetRoles ersetzt die Rollenzuordnung eines Users vollständig.
func (r *UserRepository) SetRoles(user *domain.User, roles []domain.Role) error {
	return r.db.Model(user).Association("Roles").Replace(roles)
}

func (r *UserRepository) Count() (int64, error) {
	var n int64
	err := r.db.Model(&domain.User{}).Count(&n).Error
	return n, err
}

// UpdateFields aktualisiert einzelne Spalten eines Users (z.B. Passwort,
// 2FA-Status) ohne die Assoziationen anzufassen.
func (r *UserRepository) UpdateFields(id uint, fields map[string]any) error {
	return r.db.Model(&domain.User{}).Where("id = ?", id).Updates(fields).Error
}
