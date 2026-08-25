package services

import (
	"errors"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// ErrLogForbidden signalisiert, dass die Session zu keinem für den Benutzer
// sichtbaren Server gehört (Tenant-Isolation).
var ErrLogForbidden = errors.New("kein zugriff auf dieses protokoll")

// SSHLogService liefert die SSH-Protokolle scope-gefiltert aus. Die
// Sichtbarkeit wird über den zugehörigen Server (bzw. Job) erzwungen -
// ein Manager sieht nur Protokolle seiner eigenen Servergruppen.
type SSHLogService struct {
	logs    *repositories.SSHLogRepository
	servers *repositories.ServerRepository
	jobs    *repositories.JobRepository
}

func NewSSHLogService(logs *repositories.SSHLogRepository, servers *repositories.ServerRepository, jobs *repositories.JobRepository) *SSHLogService {
	return &SSHLogService{logs: logs, servers: servers, jobs: jobs}
}

// ServerSessions liefert die Protokoll-Sessions eines Servers (ohne
// Kommando-Ausgaben). Autorisierung: der Server muss im Scope sichtbar sein.
func (s *SSHLogService) ServerSessions(scope repositories.AccessScope, serverID uint, limit int) ([]domain.SSHSession, error) {
	if _, err := s.servers.FindByID(scope, serverID); err != nil {
		return nil, err
	}
	return s.logs.SessionsForServer(serverID, limit)
}

// JobSessions liefert die Sessions (inkl. Kommandos) einer Job-Ausführung -
// "was hat dieser Job genau ausgeführt". Autorisierung über den Job-Scope.
func (s *SSHLogService) JobSessions(scope repositories.AccessScope, jobID string) ([]domain.SSHSession, error) {
	if _, err := s.jobs.FindByID(scope, jobID); err != nil {
		return nil, err
	}
	return s.logs.SessionsForJob(jobID)
}

// Session liefert eine einzelne Session samt Kommandos. Autorisierung über
// den Server der Session (serverlose Alt-Sessions nur für Admins).
func (s *SSHLogService) Session(scope repositories.AccessScope, id string) (*domain.SSHSession, error) {
	sess, err := s.logs.SessionWithCommands(id)
	if err != nil {
		return nil, err
	}
	if sess.ServerID == nil {
		if !scope.All {
			return nil, ErrLogForbidden
		}
		return sess, nil
	}
	if _, err := s.servers.FindByID(scope, *sess.ServerID); err != nil {
		if errors.Is(err, repositories.ErrNotFound) {
			return nil, ErrLogForbidden
		}
		return nil, err
	}
	return sess, nil
}
