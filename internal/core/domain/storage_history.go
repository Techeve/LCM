package domain

import (
	"math"
	"time"

	"gorm.io/gorm"
)

// StorageHistory ist der historische Verlauf der Festplattenbelegung eines
// Servers - genau EINE Zeile pro Server und Tag (Tagesdurchschnitt). Der
// Health-Check misst stündlich (gedrosselt) Kapazität und Nutzung; die
// Messungen eines Tages werden fortlaufend zu einem Tagesdurchschnitt
// gemittelt und langfristig aufbewahrt. So lässt sich über die Zeit sehen,
// wie sich der belegte Speicher entwickelt.
type StorageHistory struct {
	ID        string    `gorm:"type:text;primarykey" json:"id"` // UUID
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// ServerID + Day bilden zusammen einen eindeutigen Schlüssel: pro Tag
	// gibt es genau eine (fortgeschriebene) Durchschnitts-Zeile je Server.
	ServerID uint   `gorm:"not null;index:idx_storage_server_day,unique" json:"server_id"`
	Day      string `gorm:"not null;index:idx_storage_server_day,unique" json:"day"` // "2006-01-02"

	// Tagesdurchschnitt (gerundet) aus allen Messungen des Tages.
	DiskTotalMB int64 `json:"disk_total_mb"`
	DiskUsedMB  int64 `json:"disk_used_mb"`

	// Samples zählt die eingeflossenen Messungen (für die laufende Mittelung).
	Samples int `json:"samples"`
	// LastSampleAt hält den Zeitpunkt der letzten Messung - steuert die
	// stündliche Drosselung im Health-Check.
	LastSampleAt time.Time `json:"last_sample_at"`
}

// BeforeCreate vergibt eine UUID, falls noch keine gesetzt ist.
func (h *StorageHistory) BeforeCreate(*gorm.DB) error {
	if h.ID == "" {
		h.ID = newUUID()
	}
	return nil
}

// UsagePercent liefert die Belegung dieses Tages-Snapshots in Prozent.
func (h *StorageHistory) UsagePercent() int {
	if h.DiskTotalMB <= 0 {
		return 0
	}
	return int(h.DiskUsedMB * 100 / h.DiskTotalMB)
}

// minForecastSamples ist die Mindestanzahl an Tages-Datenpunkten, die für
// eine aussagekräftige Prognose benötigt werden.
const minForecastSamples = 7

// StorageForecast ist die lineare Extrapolation auf Basis des historischen
// Speicher-Verlaufs: geschätzte Restlaufzeit bis zur vollen Festplatte.
type StorageForecast struct {
	// DaysRemaining: geschätzte Tage bis zur Erschöpfung (0 = bereits voll).
	DaysRemaining int `json:"days_remaining"`
	// Unlimited: kein wesentlicher Zuwachs erkennbar oder Horizont > 365 Tage.
	Unlimited bool `json:"unlimited"`
	// InsufficientData: weniger als minForecastSamples Tagesdurchschnitte vorhanden.
	InsufficientData bool `json:"insufficient_data"`
}

// ComputeForecast berechnet per linearer Regression über den gesamten Verlauf
// die geschätzte Restlaufzeit. Die Regression minimiert den quadratischen
// Fehler über alle Tages-Snapshots (y = DiskUsedMB).
// Rückgabe:
//   - InsufficientData: < minForecastSamples Datenpunkte
//   - Unlimited: Zuwachs ≤ 0 oder Restlaufzeit > 365 Tage
//   - DaysRemaining: konkrete Schätzung (abgerundet auf ganze Tage)
func ComputeForecast(history []StorageHistory, currentTotalMB int64) StorageForecast {
	if len(history) < minForecastSamples {
		return StorageForecast{InsufficientData: true}
	}

	n := float64(len(history))
	var sumX, sumY, sumXY, sumX2 float64
	for i, h := range history {
		x := float64(i)
		y := float64(h.DiskUsedMB)
		sumX += x
		sumY += y
		sumXY += x * y
		sumX2 += x * x
	}

	denom := n*sumX2 - sumX*sumX
	if denom == 0 {
		return StorageForecast{Unlimited: true}
	}
	slope := (n*sumXY - sumX*sumY) / denom // MB/Tag

	if slope <= 0 {
		return StorageForecast{Unlimited: true}
	}

	lastUsed := float64(history[len(history)-1].DiskUsedMB)
	remaining := float64(currentTotalMB) - lastUsed
	if remaining <= 0 {
		return StorageForecast{DaysRemaining: 0}
	}

	days := int(math.Round(remaining / slope))
	if days > 365 {
		return StorageForecast{Unlimited: true}
	}
	return StorageForecast{DaysRemaining: days}
}
