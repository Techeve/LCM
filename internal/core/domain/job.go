package domain

import (
	"time"

	"gorm.io/gorm"
)

// Job-Status.
//
// Auf einem Server läuft immer höchstens EIN Job - zwei gleichzeitige Zugriffe
// auf dieselbe Paketverwaltung oder Firewall wären ein Systemkonflikt. Was
// dagegen läuft, hängt davon ab, wer den Job ausgelöst hat:
//
//   - "pending": Ein Zeitplan-Lauf, der warten darf. Er steht in der
//     Warteschlange des Servers und startet, sobald der laufende Job fertig
//     ist - der Reihe nach, stärkster Gruppen-Vorrang zuerst. Ein feuernder
//     Zeitplan verliert seine Arbeit damit nicht mehr, nur weil zufällig
//     gerade ein anderer Lauf auf demselben Server aktiv war.
//   - "blocked": Der Job kam nicht zum Zug und wird es auch nicht mehr. Das
//     trifft eine unmittelbare Aktion (jemand hat auf einen Knopf gedrückt,
//     während etwas lief - dann ist eine sofortige Absage ehrlicher als eine
//     stille Verzögerung) und einen Wartenden, der zu lange gewartet hat: Ist
//     der nächste Durchgang seines Zeitplans ohnehin näher als sein eigener
//     Start, wäre er beim Laufen schon überholt.
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

// RoutineJobTypes sind die Job-Typen, deren Ausgabe nur kurz aufbewahrt wird
// (GlobalSettings.RoutineLogRetentionDays).
var RoutineJobTypes = []string{RuleTypeHealth, RuleTypeAlertCheck}

// HealthCheckPurpose ist der Zweck der SSH-Sitzungen des Health-Pings - stabil,
// unabhängig vom frei umbenennbaren Regelnamen.
const HealthCheckPurpose = "health-check"

// RoutineOutputRemoved ersetzt die Ausgabe eines Routine-Jobs nach Ablauf
// seiner Frist.
const RoutineOutputRemoved = "(Ausgabe entfernt - Routine-Protokolle werden nur kurz aufbewahrt, siehe Einstellungen → Allgemein)"

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
	ID string `gorm:"type:text;primarykey" json:"id"`
	// idx_jobs_server_created trägt die Frage „letzter Job dieses Servers"
	// der Ampel. Ohne ihn sortierte jede Status-Abfrage sämtliche Jobs des
	// Servers - bei Health-Check im Viertelstundentakt über 90 Tage rund
	// 8600 Zeilen, und das für jeden Server des Dashboards.
	CreatedAt time.Time `gorm:"index:idx_jobs_server_created,priority:2" json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// ServerID ist leer bei serverlosen System-Jobs (z.B. Backup).
	ServerID *uint   `gorm:"index;index:idx_jobs_server_created,priority:1" json:"server_id"`
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
