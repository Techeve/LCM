package repositories

import (
	"gorm.io/gorm"

	"LCM/internal/core/domain"
)

// PendingUserSyncRepository verwaltet den Rückstand offener Benutzer-Abgleiche
// (siehe domain.PendingUserSync).
type PendingUserSyncRepository struct {
	db *gorm.DB
}

func NewPendingUserSyncRepository(db *gorm.DB) *PendingUserSyncRepository {
	return &PendingUserSyncRepository{db: db}
}

// Queue legt einen Eintrag an - oder lässt ihn, wenn derselbe Auftrag für
// denselben Server schon offen ist. Ein zweites „alle Konten verteilen" bringt
// nichts Neues, und zweimal dasselbe Konto zu entfernen erst recht nicht.
func (r *PendingUserSyncRepository) Queue(serverID uint, username string) error {
	q := r.db.Model(&domain.PendingUserSync{}).Where("server_id = ?", serverID)
	if username == "" {
		q = q.Where("username_bidx = ''")
	} else {
		q = q.Where("username_bidx = ?", domain.UserBlindIndex(username))
	}
	var count int64
	if err := q.Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	return r.db.Create(&domain.PendingUserSync{ServerID: serverID, Username: username}).Error
}

// ForServer liefert den Rückstand eines Servers (älteste zuerst).
func (r *PendingUserSyncRepository) ForServer(serverID uint) ([]domain.PendingUserSync, error) {
	var out []domain.PendingUserSync
	err := r.db.Where("server_id = ?", serverID).Order("id").Find(&out).Error
	return out, err
}

// CountByServer liefert die Zahl offener Einträge je Server (für die Anzeige).
func (r *PendingUserSyncRepository) CountByServer(serverID uint) (int64, error) {
	var n int64
	err := r.db.Model(&domain.PendingUserSync{}).Where("server_id = ?", serverID).Count(&n).Error
	return n, err
}

// ServerIDs liefert alle Server mit offenen Einträgen.
func (r *PendingUserSyncRepository) ServerIDs() ([]uint, error) {
	var ids []uint
	err := r.db.Model(&domain.PendingUserSync{}).Distinct().Pluck("server_id", &ids).Error
	return ids, err
}

func (r *PendingUserSyncRepository) Delete(id uint) error {
	return r.db.Delete(&domain.PendingUserSync{}, id).Error
}

// DeleteForServer räumt den Rückstand eines Servers ab - etwa wenn der Server
// selbst gelöscht wird.
func (r *PendingUserSyncRepository) DeleteForServer(serverID uint) error {
	return r.db.Where("server_id = ?", serverID).Delete(&domain.PendingUserSync{}).Error
}

// MarkFailed hält den Fehlversuch am Eintrag fest, statt ihn stumm zu wiederholen.
func (r *PendingUserSyncRepository) MarkFailed(entry *domain.PendingUserSync, cause string) error {
	return r.db.Model(&domain.PendingUserSync{}).Where("id = ?", entry.ID).
		Updates(map[string]any{"attempts": entry.Attempts + 1, "last_error": cause}).Error
}
