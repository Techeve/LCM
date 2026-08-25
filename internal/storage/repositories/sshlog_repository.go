package repositories

import (
	"time"

	"gorm.io/gorm"

	"LCM/internal/core/domain"
)

// SSHLogRepository persistiert die SSH-Protokolle (Sessions + Kommandos).
// Die Scope-Prüfung erfolgt in der Service-Schicht über den bereits
// autorisierten Server/Job - die Queries hier arbeiten mit bereits
// freigegebenen IDs.
type SSHLogRepository struct {
	db *gorm.DB
}

func NewSSHLogRepository(db *gorm.DB) *SSHLogRepository {
	return &SSHLogRepository{db: db}
}

func (r *SSHLogRepository) CreateSession(s *domain.SSHSession) error {
	return r.db.Create(s).Error
}

func (r *SSHLogRepository) AddCommand(c *domain.SSHCommand) error {
	return r.db.Create(c).Error
}

// FinishSession aktualisiert Abschlusszeit, Kommandozahl und Fehlerflag.
func (r *SSHLogRepository) FinishSession(id string, closedAt time.Time, commandCount int, hadError bool) error {
	return r.db.Model(&domain.SSHSession{}).Where("id = ?", id).Updates(map[string]any{
		"closed_at":     closedAt,
		"command_count": commandCount,
		"had_error":     hadError,
	}).Error
}

// LinkSession ordnet eine (während des Onboardings zunächst serverlose)
// Session nachträglich Server und Job zu.
func (r *SSHLogRepository) LinkSession(id string, serverID uint, jobID *string) error {
	return r.db.Model(&domain.SSHSession{}).Where("id = ?", id).Updates(map[string]any{
		"server_id": serverID,
		"job_id":    jobID,
	}).Error
}

// SessionsForServer liefert die Sessions eines Servers (neueste zuerst),
// ohne die (u.U. großen) Kommando-Ausgaben.
func (r *SSHLogRepository) SessionsForServer(serverID uint, limit int) ([]domain.SSHSession, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var sessions []domain.SSHSession
	// UUID-PKs sind nicht chronologisch - nach opened_at sortieren, rowid
	// (Einfügereihenfolge) als eindeutiger Tiebreaker.
	err := r.db.Where("server_id = ?", serverID).
		Order("opened_at DESC, rowid DESC").Limit(limit).Find(&sessions).Error
	return sessions, err
}

// SessionsForUser liefert die Sitzungen EINES Login-Benutzers auf einem
// Server. Für den LCM-Zugangsbenutzer ist das die einzige Quelle: LCM meldet
// sich ohne Terminal an (Kommando-Sitzung), und dafür schreibt sshd weder
// wtmp noch lastlog - die Anmeldungen tauchen in der Benutzer-Übersicht sonst
// überhaupt nicht auf.
func (r *SSHLogRepository) SessionsForUser(serverID uint, user string, limit int) ([]domain.SSHSession, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var sessions []domain.SSHSession
	err := r.db.Where("server_id = ? AND user = ?", serverID, user).
		Order("opened_at DESC, rowid DESC").Limit(limit).Find(&sessions).Error
	return sessions, err
}

// SessionStatsForUser liefert Anzahl und jüngsten Zeitpunkt der Sitzungen
// eines Login-Benutzers (Grundlage für „zuletzt angemeldet" und die Zählung
// in der Benutzer-Übersicht).
func (r *SSHLogRepository) SessionStatsForUser(serverID uint, user string) (int, *time.Time, error) {
	var count int64
	if err := r.db.Model(&domain.SSHSession{}).
		Where("server_id = ? AND user = ?", serverID, user).Count(&count).Error; err != nil {
		return 0, nil, err
	}
	if count == 0 {
		return 0, nil, nil
	}
	// Den jüngsten Zeitpunkt über die Zeile holen statt per MAX(): Der
	// SQLite-Treiber liefert einen berechneten Zeitstempel als Text zurück,
	// der sich nicht in time.Time einlesen lässt.
	var newest domain.SSHSession
	if err := r.db.Where("server_id = ? AND user = ?", serverID, user).
		Order("opened_at DESC, rowid DESC").First(&newest).Error; err != nil {
		return int(count), nil, err
	}
	last := newest.OpenedAt
	return int(count), &last, nil
}

// SessionsForJob liefert alle Sessions einer Job-Ausführung inklusive der
// Kommandos (chronologisch) - das ist die Detailansicht "was hat dieser
// Job genau ausgeführt".
func (r *SSHLogRepository) SessionsForJob(jobID string) ([]domain.SSHSession, error) {
	var sessions []domain.SSHSession
	err := r.db.Preload("Commands", func(db *gorm.DB) *gorm.DB {
		return db.Order("ssh_commands.seq ASC")
	}).Where("job_id = ?", jobID).Order("opened_at ASC, rowid ASC").Find(&sessions).Error
	return sessions, err
}

// SessionWithCommands lädt eine einzelne Session samt Kommandos.
func (r *SSHLogRepository) SessionWithCommands(id string) (*domain.SSHSession, error) {
	var s domain.SSHSession
	err := r.db.Preload("Commands", func(db *gorm.DB) *gorm.DB {
		return db.Order("ssh_commands.seq ASC")
	}).First(&s, "id = ?", id).Error
	if err != nil {
		return nil, translate(err)
	}
	return &s, nil
}

// DeleteOlderThan entfernt Sessions (und per Cascade deren Kommandos), die
// vor dem Stichtag geöffnet wurden - Teil der Log-Retention.
func (r *SSHLogRepository) DeleteOlderThan(cutoff time.Time) (int64, error) {
	// Kommandos zuerst löschen (SQLite-Cascade greift nur bei aktivierten
	// Foreign Keys; explizit ist robuster).
	r.db.Where("ssh_session_id IN (SELECT id FROM ssh_sessions WHERE created_at < ?)", cutoff).
		Delete(&domain.SSHCommand{})
	res := r.db.Where("created_at < ?", cutoff).Delete(&domain.SSHSession{})
	return res.RowsAffected, res.Error
}
