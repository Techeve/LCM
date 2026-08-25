package repositories

import (
	"gorm.io/gorm"

	"LCM/internal/core/domain"
)

// SettingsRepository verwaltet die globale Einstellungs-Zeile (ID 1)
// und die Backup-Metadaten.
type SettingsRepository struct {
	db *gorm.DB
}

func NewSettingsRepository(db *gorm.DB) *SettingsRepository {
	return &SettingsRepository{db: db}
}

func (r *SettingsRepository) Get() (*domain.GlobalSettings, error) {
	var settings domain.GlobalSettings
	if err := r.db.First(&settings, 1).Error; err != nil {
		return nil, translate(err)
	}
	return &settings, nil
}

func (r *SettingsRepository) Save(settings *domain.GlobalSettings) error {
	settings.ID = 1
	return r.db.Save(settings).Error
}

// UpdateFields aktualisiert gezielt einzelne Spalten der Einstellungs-Zeile
// (id=1), ohne das ganze Struct zu überschreiben - für nebenläufige Teil-
// Updates wie das zwischengespeicherte Ergebnis der Update-Prüfung.
func (r *SettingsRepository) UpdateFields(fields map[string]any) error {
	return r.db.Model(&domain.GlobalSettings{}).Where("id = ?", 1).Updates(fields).Error
}

// ---- Backups -----------------------------------------------------------------

func (r *SettingsRepository) CreateBackup(b *domain.Backup) error {
	return r.db.Create(b).Error
}

func (r *SettingsRepository) FindBackups() ([]domain.Backup, error) {
	var backups []domain.Backup
	if err := r.db.Order("id DESC").Find(&backups).Error; err != nil {
		return nil, err
	}
	return backups, nil
}

// DeleteBackupsBeyond löscht die ältesten Backup-Einträge über der
// Aufbewahrungsgrenze und liefert sie zurück (zum Löschen der Dateien).
func (r *SettingsRepository) DeleteBackupsBeyond(keep int) ([]domain.Backup, error) {
	if keep <= 0 {
		return nil, nil
	}
	var stale []domain.Backup
	if err := r.db.Order("id DESC").Offset(keep).Find(&stale).Error; err != nil {
		return nil, err
	}
	if len(stale) == 0 {
		return nil, nil
	}
	ids := make([]uint, len(stale))
	for i, b := range stale {
		ids[i] = b.ID
	}
	if err := r.db.Delete(&domain.Backup{}, ids).Error; err != nil {
		return nil, err
	}
	return stale, nil
}

// DeleteBackupByName entfernt den Metadaten-Eintrag eines Backups anhand des
// Dateinamens. Liefert die Anzahl gelöschter Zeilen (0 = kein Treffer).
func (r *SettingsRepository) DeleteBackupByName(name string) (int64, error) {
	res := r.db.Where("file_name = ?", name).Delete(&domain.Backup{})
	return res.RowsAffected, res.Error
}
