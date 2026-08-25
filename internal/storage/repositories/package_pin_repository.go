package repositories

import (
	"strings"

	"gorm.io/gorm"

	"LCM/internal/core/domain"
)

// PackagePinRepository verwaltet die Paket-Pins (Schutz vor Autoremove und
// optionale Versions-Fixierung). Pins mit ServerID == 0 sind global und gelten
// fuer alle Server.
type PackagePinRepository struct {
	db *gorm.DB
}

func NewPackagePinRepository(db *gorm.DB) *PackagePinRepository {
	return &PackagePinRepository{db: db}
}

// ListGlobal liefert nur die globalen Pins.
func (r *PackagePinRepository) ListGlobal() ([]domain.PackagePin, error) {
	var pins []domain.PackagePin
	if err := r.db.Where("server_id = 0").Order("name").Find(&pins).Error; err != nil {
		return nil, err
	}
	return pins, nil
}

// ListForServer liefert die Pins, die fuer DIESEN Server gelten: die globalen
// und die serverspezifischen zusammen. Genau diese Vereinigung wird auf dem
// Ziel angewendet - der globale Kernel-Schutz laesst sich damit einmal setzen
// und je Server um Sonderfaelle ergaenzen.
func (r *PackagePinRepository) ListForServer(serverID uint) ([]domain.PackagePin, error) {
	var pins []domain.PackagePin
	if err := r.db.Where("server_id = 0 OR server_id = ?", serverID).
		Order("server_id, name").Find(&pins).Error; err != nil {
		return nil, err
	}
	return pins, nil
}

// FindByID laedt einen einzelnen Pin.
func (r *PackagePinRepository) FindByID(id uint) (*domain.PackagePin, error) {
	var pin domain.PackagePin
	if err := r.db.First(&pin, id).Error; err != nil {
		return nil, translate(err)
	}
	return &pin, nil
}

// Create legt einen Pin an. Existiert derselbe Name im selben Geltungsbereich
// bereits, werden nur die Wirkungen aktualisiert - ein zweiter Klick auf
// „schuetzen" soll keinen Doppeleintrag erzeugen.
func (r *PackagePinRepository) Create(pin *domain.PackagePin) error {
	pin.Name = strings.TrimSpace(pin.Name)
	var existing domain.PackagePin
	err := r.db.Where("server_id = ? AND name = ?", pin.ServerID, pin.Name).First(&existing).Error
	if err == nil {
		existing.NoRemove = pin.NoRemove
		existing.Hold = pin.Hold
		existing.Note = pin.Note
		if err := r.db.Save(&existing).Error; err != nil {
			return err
		}
		*pin = existing
		return nil
	}
	if err != gorm.ErrRecordNotFound {
		return err
	}
	return r.db.Create(pin).Error
}

func (r *PackagePinRepository) Delete(id uint) error {
	res := r.db.Delete(&domain.PackagePin{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteForServer raeumt die Pins eines Servers weg (beim Entfernen des
// Servers) - globale Pins bleiben unberuehrt.
func (r *PackagePinRepository) DeleteForServer(serverID uint) error {
	return r.db.Where("server_id = ?", serverID).Delete(&domain.PackagePin{}).Error
}
