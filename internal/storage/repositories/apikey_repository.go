package repositories

import (
	"time"

	"gorm.io/gorm"

	"LCM/internal/core/domain"
)

// APIKeyRepository verwaltet API-Keys für Service-Kommunikation.
type APIKeyRepository struct {
	db *gorm.DB
}

func NewAPIKeyRepository(db *gorm.DB) *APIKeyRepository {
	return &APIKeyRepository{db: db}
}

func (r *APIKeyRepository) Create(key *domain.APIKey) error {
	return r.db.Create(key).Error
}

// FindActiveByHash liefert einen gültigen (nicht widerrufenen, nicht
// abgelaufenen) Key anhand seines Hashes, inkl. User mit Rollen/Permissions.
func (r *APIKeyRepository) FindActiveByHash(hash string) (*domain.APIKey, error) {
	var key domain.APIKey
	err := r.db.Preload("User.Roles.Permissions").
		Where("key_hash = ? AND revoked = ?", hash, false).
		First(&key).Error
	if err != nil {
		return nil, translate(err)
	}
	if key.ExpiresAt != nil && key.ExpiresAt.Before(time.Now()) {
		return nil, ErrNotFound
	}
	return &key, nil
}

func (r *APIKeyRepository) FindAll() ([]domain.APIKey, error) {
	var keys []domain.APIKey
	if err := r.db.Order("id").Find(&keys).Error; err != nil {
		return nil, err
	}
	return keys, nil
}

// FindByID liefert einen Key unabhängig von seinem Widerrufs-Status.
func (r *APIKeyRepository) FindByID(id uint) (*domain.APIKey, error) {
	var key domain.APIKey
	if err := r.db.First(&key, id).Error; err != nil {
		return nil, translate(err)
	}
	return &key, nil
}

// Delete entfernt einen Key endgültig aus der Datenbank - Aufräum-Schritt
// NACH dem Widerruf (R2-053), nie der erste.
func (r *APIKeyRepository) Delete(id uint) error {
	res := r.db.Delete(&domain.APIKey{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *APIKeyRepository) TouchLastUsed(id uint) error {
	now := time.Now()
	return r.db.Model(&domain.APIKey{}).Where("id = ?", id).Update("last_used_at", &now).Error
}

func (r *APIKeyRepository) Revoke(id uint) error {
	res := r.db.Model(&domain.APIKey{}).Where("id = ?", id).Update("revoked", true)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
