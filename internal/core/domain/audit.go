package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"time"

	"gorm.io/gorm"
)

// AuditLog ist ein manipulationssicherer Protokoll-Eintrag.
//
// Tamper-Proofing (Hash-Chain): Jeder Eintrag enthält einen Hash über
// seine eigenen Daten UND den Hash des vorherigen Eintrags - analog zu
// einer Blockchain. Löscht oder ändert jemand nachträglich einen Eintrag
// direkt in der Datenbank, bricht die Kette; die Applikation erkennt das
// beim Start (VerifyChain) und warnt vor der Manipulation.
type AuditLog struct {
	// ID ist eine UUID (seit v0.2.0). Da UUIDs nicht sortierbar sind, hält
	// Seq die lückenlose Einfügereihenfolge - sie ist der Anker der
	// Hash-Kette (VerifyChain/Append sortieren danach). Der Hash selbst
	// (ComputeHash) enthält weder ID noch Seq; die Kette bleibt beim
	// Umstieg auf UUIDs also unverändert gültig.
	ID        string    `gorm:"type:text;primarykey" json:"id"`
	Seq       uint      `gorm:"uniqueIndex;not null" json:"seq"`
	CreatedAt time.Time `json:"created_at"`

	Actor    string `gorm:"not null" json:"actor"`  // Username oder "system"
	Action   string `gorm:"not null" json:"action"` // z.B. "server.join"
	Entity   string `json:"entity"`                 // z.B. "server"
	EntityID uint   `json:"entity_id"`
	Details  string `json:"details"`

	PrevHash string `gorm:"not null" json:"prev_hash"`
	Hash     string `gorm:"not null" json:"hash"`
}

// BeforeCreate vergibt eine UUID, falls noch keine gesetzt ist. Seq wird
// vom Repository (Append) unter Lock gesetzt und hier nicht angefasst.
func (a *AuditLog) BeforeCreate(*gorm.DB) error {
	if a.ID == "" {
		a.ID = newUUID()
	}
	return nil
}

// ComputeHash berechnet den Ketten-Hash dieses Eintrags aus dessen
// Feldern und dem Hash des Vorgängers. Muss VOR dem Speichern gesetzt
// werden (PrevHash zuerst befüllen).
func (a *AuditLog) ComputeHash() string {
	h := sha256.New()
	h.Write([]byte(a.PrevHash))
	h.Write([]byte{0})
	h.Write([]byte(a.CreatedAt.UTC().Format(time.RFC3339Nano)))
	h.Write([]byte{0})
	h.Write([]byte(a.Actor))
	h.Write([]byte{0})
	h.Write([]byte(a.Action))
	h.Write([]byte{0})
	h.Write([]byte(a.Entity))
	h.Write([]byte{0})
	h.Write([]byte(strconv.FormatUint(uint64(a.EntityID), 10)))
	h.Write([]byte{0})
	h.Write([]byte(a.Details))
	return hex.EncodeToString(h.Sum(nil))
}

// AuditChainStart ist der PrevHash des allerersten Eintrags (Genesis).
const AuditChainStart = "genesis"
