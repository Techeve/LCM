package domain

import (
	"time"

	"gorm.io/gorm"
)

// SnapPackage ist ein per snap (Snap Store) installiertes Paket - die
// zweite Paketverwaltung neben apt/dpkg, verbreitet vor allem auf Ubuntu.
// Anders als bei apt gibt es keine sources.list: Ein Snap „trackt" einen
// Kanal (Channel/Tracking, z.B. "latest/stable") aus dem Snap Store; dieser
// Kanal ist das Snap-Äquivalent zur Paketquelle.
type SnapPackage struct {
	ID        string    `gorm:"type:text;primarykey" json:"id"` // UUID
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	ServerID uint   `gorm:"not null;index:idx_snap_server_name,unique" json:"server_id"`
	Name     string `gorm:"not null;index:idx_snap_server_name,unique" json:"name"`
	Version  string `gorm:"not null" json:"version"`
	Revision string `json:"revision"` // Snap-Revision (z.B. "1974")
	Channel  string `json:"channel"`  // Tracking/Kanal, z.B. "latest/stable"
	// Publisher ist der Herausgeber im Store (z.B. "canonical").
	Publisher string `json:"publisher"`
	// CandidateVersion: verfügbares Update aus `snap refresh --list`
	// ("" = aktuell).
	CandidateVersion string `json:"candidate_version"`
}

// BeforeCreate vergibt eine UUID, falls noch keine gesetzt ist.
func (s *SnapPackage) BeforeCreate(*gorm.DB) error {
	if s.ID == "" {
		s.ID = newUUID()
	}
	return nil
}

// Outdated meldet, ob für das Snap ein Update verfügbar ist.
func (s *SnapPackage) Outdated() bool {
	return s.CandidateVersion != "" && s.CandidateVersion != s.Version
}
