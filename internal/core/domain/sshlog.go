package domain

import (
	"time"

	"gorm.io/gorm"
)

// SSHSession protokolliert EINE authentifizierte SSH-Verbindung zu einem
// Server: wer sie zu welchem Zweck geöffnet hat, wann sie auf/zu ging und
// welche Kommandos darüber liefen (als SSHCommand-Kinder). Jede Session ist
// mit dem Server verknüpft (ServerID) und - sofern die Verbindung zu einer
// Job-Ausführung gehört - mit dem Job (JobID). So lässt sich jedes Protokoll
// sowohl unter dem Server als auch beim jeweiligen Job wiederfinden.
//
// Sicherheits-/Compliance-Hinweis: Kommandozeilen und Ausgaben werden vor
// dem Speichern durch die Secret-Redaction geschleust (Passwörter, Tokens,
// URL-Basic-Auth werden unkenntlich gemacht).
//
// Log Retention: Sessions werden - wie Jobs - nach konfigurierbarer Frist
// vom Cleanup-Schedule gelöscht (Commands per Cascade).
type SSHSession struct {
	ID        string    `gorm:"type:text;primarykey" json:"id"` // UUID (seit v0.2.0)
	CreatedAt time.Time `json:"created_at"`

	// ServerID ist die verwaltete Maschine. Nullbar nur für den kurzen
	// Moment während des Onboardings, bevor der Server persistiert ist -
	// danach wird die Session dem Server (und Onboarding-Job) zugeordnet.
	ServerID *uint   `gorm:"index" json:"server_id"`
	Server   *Server `json:"server,omitempty"`
	// JobID verknüpft die Verbindung mit einer Job-Ausführung (leer bei
	// Ad-hoc-Aktionen wie SSH-Härtung, die keinen eigenen Job erzeugen).
	JobID *string `gorm:"type:text;index" json:"job_id"`

	Purpose string `gorm:"not null" json:"purpose"` // z.B. "rule:Update", "sync", "harden-ssh", "join"
	Actor   string `json:"actor"`                   // auslösender Benutzer oder "scheduler"
	Host    string `json:"host"`
	User    string `json:"user"` // SSH-Login-User (i.d.R. der Service-User)

	OpenedAt     time.Time  `json:"opened_at"`
	ClosedAt     *time.Time `json:"closed_at"`
	CommandCount int        `json:"command_count"`
	HadError     bool       `json:"had_error"`

	Commands []SSHCommand `gorm:"constraint:OnDelete:CASCADE" json:"commands,omitempty"`
}

// BeforeCreate vergibt eine UUID, falls noch keine gesetzt ist.
func (s *SSHSession) BeforeCreate(*gorm.DB) error {
	if s.ID == "" {
		s.ID = newUUID()
	}
	return nil
}

// SSHCommand ist ein einzelnes über eine Session ausgeführtes Kommando -
// der Baustein der lückenlosen Protokollierung. Command und Output sind
// bereits redigiert (Secrets entfernt); Output ist auf eine sinnvolle Größe
// begrenzt (sehr lange Ausgaben werden in der Mitte gekürzt).
type SSHCommand struct {
	ID           string    `gorm:"type:text;primarykey" json:"id"`                 // UUID (seit v0.2.0)
	SSHSessionID string    `gorm:"type:text;not null;index" json:"ssh_session_id"` // FK → SSHSession (UUID)
	Seq          int       `json:"seq"`                                            // laufende Nummer innerhalb der Session
	StartedAt    time.Time `json:"started_at"`
	DurationMs   int64     `json:"duration_ms"`
	Command      string    `gorm:"serializer:aesgcm" json:"command"` // AES-GCM at rest
	Output       string    `gorm:"serializer:aesgcm" json:"output"`  // AES-GCM at rest
	ExitCode     int       `json:"exit_code"`
	Err          string    `json:"err,omitempty"` // Transportfehler (kein Non-Zero-Exit)
}

// BeforeCreate vergibt eine UUID, falls noch keine gesetzt ist.
func (c *SSHCommand) BeforeCreate(*gorm.DB) error {
	if c.ID == "" {
		c.ID = newUUID()
	}
	return nil
}
