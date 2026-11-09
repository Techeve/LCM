package services

import (
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/core/events"
)

// Ereignisse des Job-Lebenszyklus. Sie gehen auf den Bus (siehe
// internal/core/events), damit die Oberfläche nicht im Takt nachfragen muss,
// ob ein Job läuft, wartet oder fertig ist.
//
// Gemeldet wird immer NACH dem Freigeben von s.mu: Die Meldung fragt die
// Warteschlange ab (queuedCount) und würde sich sonst selbst aussperren.

// progressGap ist der Mindestabstand zwischen zwei job.progress-Meldungen
// eines Jobs. Die Ausgabe kommt in kleinen Blöcken; der Browser braucht nicht
// jeden einzelnen, nur „da ist etwas Neues".
const progressGap = 2 * time.Second

// progressMark merkt sich je überwachtem Job, wohin er gehört und wann er
// zuletzt gemeldet hat.
type progressMark struct {
	serverID *uint
	last     time.Time
}

// WithEvents verdrahtet den Ereignisbus (optional).
func (s *JobService) WithEvents(bus *events.Bus) *JobService {
	s.bus = bus
	return s
}

// queuedCount liefert die Zahl der Wartenden auf einem Server.
func (s *JobService) queuedCount(serverID uint) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.queues[serverID])
}

// publishBusy meldet, dass der Server wegen dieses Jobs belegt ist: Er läuft
// oder steht in der Schlange. Serverlose System-Jobs sperren keinen Server.
func (s *JobService) publishBusy(job *domain.Job, busy bool) {
	if s.bus == nil || job.ServerID == nil {
		return
	}
	s.bus.Publish(events.ServerBusy(events.ServerBusyData{
		ServerID: *job.ServerID, Busy: busy, JobID: job.ID, Kind: job.Type,
		Queued: s.queuedCount(*job.ServerID),
	}))
}

// publishFinished meldet das Ende eines Jobs. wasRunning unterscheidet den
// Job, der den Server freigibt, von einem Wartenden, der nur die Schlange
// verlässt - dann ändert sich nur der Zähler, der Server bleibt belegt.
func (s *JobService) publishFinished(job *domain.Job, wasRunning bool) {
	if s.bus == nil {
		return
	}
	s.bus.Publish(events.JobFinished(job.ID, job.ServerID, job.Status))
	s.publishBusy(job, !wasRunning)
}

// watchProgress nimmt einen überwachten Job in die Fortschrittsmeldung auf.
func (s *JobService) watchProgress(jobID string, serverID *uint) {
	if s.bus == nil {
		return
	}
	s.activityMu.Lock()
	s.progress[jobID] = &progressMark{serverID: serverID}
	s.activityMu.Unlock()
}

// reportProgress meldet „neue Ausgabe", höchstens alle progressGap.
func (s *JobService) reportProgress(jobID string) {
	if s.bus == nil {
		return
	}
	s.activityMu.Lock()
	mark := s.progress[jobID]
	due := mark != nil && time.Since(mark.last) >= progressGap
	if due {
		mark.last = time.Now()
	}
	s.activityMu.Unlock()
	if due {
		s.bus.Publish(events.JobProgress(jobID, mark.serverID))
	}
}
