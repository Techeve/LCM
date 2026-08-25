package domain

import (
	"time"

	"gorm.io/gorm"
)

// Package ist ein auf einem Server installiertes (Debian-)Paket.
// Der Bestand wird beim Join und danach regelmäßig vom System-Sync
// aktualisiert; CandidateVersion != "" markiert ein verfügbares Update.
type Package struct {
	ID        string    `gorm:"type:text;primarykey" json:"id"` // UUID (seit v0.2.0)
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// ServerRef ersetzt die Klartext-server_id durch das deterministische
	// Server-Token (HMAC, siehe ServerRef). Das Unique-Paar (server_ref, name)
	// bleibt bestehen - Paket je Server bleibt eindeutig.
	ServerRef string `gorm:"not null;index:idx_pkg_server_name,unique" json:"-"`
	Name      string `gorm:"not null;index:idx_pkg_server_name,unique" json:"name"`
	Version   string `gorm:"not null" json:"version"`
	// CandidateVersion: verfügbare neuere Version ("" = aktuell).
	CandidateVersion string `json:"candidate_version"`
	// Security markiert Updates aus Security-Quellen (z.B. debian-security).
	Security bool `gorm:"default:false" json:"security"`
}

// BeforeCreate vergibt eine UUID, falls noch keine gesetzt ist.
func (p *Package) BeforeCreate(*gorm.DB) error {
	if p.ID == "" {
		p.ID = newUUID()
	}
	return nil
}

// Outdated meldet, ob für das Paket ein Update verfügbar ist.
func (p *Package) Outdated() bool {
	return p.CandidateVersion != "" && p.CandidateVersion != p.Version
}

// AptRepository ist eine auf dem Server hinterlegte Paketquelle.
// Die Sicherheitsprüfung bewertet die Quelle (https, signiert).
type AptRepository struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	ServerID uint   `gorm:"not null;index" json:"server_id"`
	Line     string `gorm:"not null" json:"line"` // z.B. "deb https://deb.debian.org/debian bookworm main"
	// Insecure: Quelle über unverschlüsseltes http (Manipulationsrisiko).
	Insecure bool `gorm:"default:false" json:"insecure"`
}
