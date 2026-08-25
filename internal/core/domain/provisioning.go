package domain

import "time"

// ActivationLink ist ein zeitlich begrenzter Einmal-Link, über den ein
// neu angelegter User seine Credentials (Passwort/SSH-Key) selbst setzt.
// Gespeichert wird nur der SHA-256-Hash des Tokens.
type ActivationLink struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `json:"created_at"`

	UserID     uint       `gorm:"not null;index" json:"user_id"`
	TokenHash  string     `gorm:"uniqueIndex;not null" json:"-"`
	ExpiresAt  time.Time  `gorm:"not null" json:"expires_at"`
	ConsumedAt *time.Time `json:"consumed_at"`
}

// Usable meldet, ob der Link noch einlösbar ist.
func (l *ActivationLink) Usable(now time.Time) bool {
	return l.ConsumedAt == nil && now.Before(l.ExpiresAt)
}
