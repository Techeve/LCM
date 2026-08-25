package repositories

import (
	"gorm.io/gorm"

	"LCM/internal/core/domain"
)

// AppRepository verwaltet den Anwendungskatalog und die Funde je Server.
type AppRepository struct {
	db *gorm.DB
}

func NewAppRepository(db *gorm.DB) *AppRepository {
	return &AppRepository{db: db}
}

// ---- Katalog --------------------------------------------------------------

func (r *AppRepository) FindAll() ([]domain.AppCatalogEntry, error) {
	var entries []domain.AppCatalogEntry
	// Mitgelieferte zuerst - sie sind der Bezugspunkt für eigene.
	err := r.db.Order("builtin DESC, name").Find(&entries).Error
	return entries, err
}

// FindEnabled liefert die Einträge, die beim Scan geprüft werden.
func (r *AppRepository) FindEnabled() ([]domain.AppCatalogEntry, error) {
	var entries []domain.AppCatalogEntry
	err := r.db.Where("enabled = ?", true).Order("slug").Find(&entries).Error
	return entries, err
}

func (r *AppRepository) FindByID(id uint) (*domain.AppCatalogEntry, error) {
	var entry domain.AppCatalogEntry
	if err := r.db.First(&entry, id).Error; err != nil {
		return nil, translate(err)
	}
	return &entry, nil
}

func (r *AppRepository) FindBySlug(slug string) (*domain.AppCatalogEntry, error) {
	var entry domain.AppCatalogEntry
	if err := r.db.Where("slug = ?", slug).First(&entry).Error; err != nil {
		return nil, translate(err)
	}
	return &entry, nil
}

func (r *AppRepository) Create(e *domain.AppCatalogEntry) error {
	return r.db.Create(e).Error
}

func (r *AppRepository) Update(e *domain.AppCatalogEntry) error {
	return r.db.Model(&domain.AppCatalogEntry{ID: e.ID}).Updates(map[string]any{
		"name": e.Name, "description": e.Description, "enabled": e.Enabled,
		"name_en": e.NameEN, "description_en": e.DescriptionEN,
		"markers": e.Markers, "version_command": e.VersionCommand,
		"version_pattern": e.VersionPattern, "compare": e.Compare,
		"latest_source": e.LatestSource, "latest_pattern": e.LatestPattern,
		"backup_action_id": e.BackupActionID, "update_action_id": e.UpdateActionID,
	}).Error
}

// UpdateLatest schreibt das Ergebnis des Abgleichs mit der Quelle fort.
func (r *AppRepository) UpdateLatest(id uint, fields map[string]any) error {
	return r.db.Model(&domain.AppCatalogEntry{ID: id}).Updates(fields).Error
}

// Delete entfernt einen Eintrag samt der Funde, die auf ihn zeigen - ein Fund
// ohne Steckbrief wäre eine Zeile, zu der niemand mehr etwas sagen kann.
func (r *AppRepository) Delete(id uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		entry := domain.AppCatalogEntry{}
		if err := tx.First(&entry, id).Error; err != nil {
			return translate(err)
		}
		if err := tx.Where("slug = ?", entry.Slug).Delete(&domain.DetectedApp{}).Error; err != nil {
			return err
		}
		return tx.Delete(&domain.AppCatalogEntry{}, id).Error
	})
}

// ---- Funde je Server ------------------------------------------------------

func (r *AppRepository) DetectedFor(serverID uint) ([]domain.DetectedApp, error) {
	var apps []domain.DetectedApp
	err := r.db.Where("server_id = ?", serverID).Order("name").Find(&apps).Error
	return apps, err
}

func (r *AppRepository) UnknownFor(serverID uint) ([]domain.UnknownApp, error) {
	var apps []domain.UnknownApp
	err := r.db.Where("server_id = ?", serverID).Order("unit").Find(&apps).Error
	return apps, err
}

// ReplaceDetected setzt den Fund-Bestand eines Servers neu. Wie überall beim
// Scan ein Soll-Zustand: Was nicht mehr gefunden wird, ist weg.
func (r *AppRepository) ReplaceDetected(serverID uint, apps []domain.DetectedApp) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("server_id = ?", serverID).Delete(&domain.DetectedApp{}).Error; err != nil {
			return err
		}
		if len(apps) == 0 {
			return nil
		}
		for i := range apps {
			apps[i].ID, apps[i].ServerID = 0, serverID
		}
		return tx.Create(&apps).Error
	})
}

func (r *AppRepository) ReplaceUnknown(serverID uint, apps []domain.UnknownApp) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("server_id = ?", serverID).Delete(&domain.UnknownApp{}).Error; err != nil {
			return err
		}
		if len(apps) == 0 {
			return nil
		}
		for i := range apps {
			apps[i].ID, apps[i].ServerID = 0, serverID
		}
		return tx.Create(&apps).Error
	})
}

// CountDetectedBySlug zählt, auf wie vielen Servern eine Anwendung gefunden
// wurde - der Verwendungsnachweis vor dem Löschen eines Eintrags.
func (r *AppRepository) CountDetectedBySlug(slug string) (int64, error) {
	var n int64
	err := r.db.Model(&domain.DetectedApp{}).Where("slug = ?", slug).Count(&n).Error
	return n, err
}
