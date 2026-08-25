package domain

import (
	"time"

	"gorm.io/gorm"
)

// Job-Status. "blocked" dokumentiert die Concurrency Control: Läuft auf
// einem Server bereits ein Job, werden parallel getriggerte Jobs für
// diesen Server abgewiesen, um Systemkollisionen zu verhindern.
const (
	JobStatusPending = "pending"
	JobStatusRunning = "running"
	JobStatusSuccess = "success"
	JobStatusFailed  = "failed"
	JobStatusBlocked = "blocked"
	// JobStatusAborted: manuell oder vom Watchdog (Zeitüberschreitung)
	// beendet - KEIN echter Fehlschlag. Vorher trug ein Abbruch denselben
	// „failed"-Status wie ein fehlgeschlagenes Kommando, sodass sich die
	// drei Sachverhalte in der Historie nicht trennen ließen (R2-068).
	JobStatusAborted = "aborted"
)

// Job protokolliert JEDE Ausführung - Cronjob, Rule, manuelle Aktion -
// permanent in der Datenbank (Protokollierungspflicht). Der exakte
// Konsolen-Output (Stdout/Stderr) der SSH-Ausführung wird in Output
// gespeichert und ist im Web-Interface zur Fehleranalyse abrufbar.
//
// Log Retention: Jobs werden nach konfigurierbarer Frist (Default 90
// Tage) automatisch vom Cleanup-Schedule gelöscht.
type Job struct {
	// ID ist eine UUID (seit v0.2.0). Anders als eine fortlaufende Zahl
	// verrät sie weder die Anzahl der Jobs noch erlaubt sie das Erraten
	// benachbarter IDs - deshalb chronologisch nach CreatedAt sortiert.
	ID        string    `gorm:"type:text;primarykey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// ServerID ist leer bei serverlosen System-Jobs (z.B. Backup).
	ServerID *uint   `gorm:"index" json:"server_id"`
	Server   *Server `json:"server,omitempty"`
	// RuleID verknüpft Rule-Ausführungen; leer bei manuellen Aktionen.
	RuleID *uint `gorm:"index" json:"rule_id"`

	Type        string `gorm:"not null" json:"type"` // RuleType* oder "join", "harden-ssh", ...
	Name        string `gorm:"not null" json:"name"`
	Status      string `gorm:"not null;index" json:"status"`
	Output      string `gorm:"serializer:aesgcm" json:"-"` // Stdout/Stderr, AES-GCM at rest - über eigenen Endpunkt abrufbar
	ExitCode    *int   `json:"exit_code"`
	TriggeredBy string `json:"triggered_by"` // Username oder "scheduler"

	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
}

// BeforeCreate vergibt eine UUID, falls noch keine gesetzt ist.
func (j *Job) BeforeCreate(*gorm.DB) error {
	if j.ID == "" {
		j.ID = newUUID()
	}
	return nil
}
