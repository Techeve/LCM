package services

import (
	"errors"
	"sync"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/safego"
	"LCM/internal/storage/repositories"
)

// ErrBulkUpdateRunning meldet, dass bereits ein „Alle VMs aktualisieren"-Lauf
// aktiv ist - es läuft immer höchstens einer gleichzeitig.
var ErrBulkUpdateRunning = errors.New("es läuft bereits ein Sammel-Update")

// BulkUpdateStatus ist der (JSON-serialisierbare) Fortschritt eines
// Sammel-Updates aller Server - für die Anzeige auf der Security-Seite.
type BulkUpdateStatus struct {
	Running    bool       `json:"running"`
	Total      int        `json:"total"`     // Anzahl einbezogener Server
	Completed  int        `json:"completed"` // erfolgreich aktualisiert
	Failed     int        `json:"failed"`    // fehlgeschlagen/übersprungen
	Current    string     `json:"current"`   // gerade laufender Server (Name)
	Actor      string     `json:"actor"`     // wer den Lauf gestartet hat
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	LastError  string     `json:"last_error"` // letzter Fehler (leer = ok)
}

// bulkUpdateRunner kapselt den Fortschritt hinter einem Mutex.
type bulkUpdateRunner struct {
	mu    sync.Mutex
	state BulkUpdateStatus
}

func (r *bulkUpdateRunner) snapshot() BulkUpdateStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state
}

func (r *bulkUpdateRunner) setCurrent(name string) {
	r.mu.Lock()
	r.state.Current = name
	r.mu.Unlock()
}

func (r *bulkUpdateRunner) markDone(ok bool, errMsg string) {
	r.mu.Lock()
	if ok {
		r.state.Completed++
	} else {
		r.state.Failed++
		if errMsg != "" {
			r.state.LastError = errMsg
		}
	}
	r.mu.Unlock()
}

func (r *bulkUpdateRunner) finish() {
	r.mu.Lock()
	now := time.Now()
	r.state.Running = false
	r.state.Current = ""
	r.state.FinishedAt = &now
	r.mu.Unlock()
}

// BulkUpdateStatus liefert den aktuellen Fortschritt des Sammel-Updates.
func (s *ServerService) BulkUpdateStatus() BulkUpdateStatus {
	return s.bulk.snapshot()
}

// StartBulkUpdate stößt Security-Updates auf allen erreichbaren Servern im
// Scope des Aufrufers an - nacheinander, damit der Fortschritt (x/N) verfolgbar
// bleibt und die Server-Sperren eingehalten werden. Läuft bereits eines,
// liefert die Methode ErrBulkUpdateRunning samt aktuellem Stand.
func (s *ServerService) StartBulkUpdate(scope repositories.AccessScope, actor string) (BulkUpdateStatus, error) {
	s.bulk.mu.Lock()
	if s.bulk.state.Running {
		st := s.bulk.state
		s.bulk.mu.Unlock()
		return st, ErrBulkUpdateRunning
	}

	all, err := s.servers.FindAll(scope)
	if err != nil {
		s.bulk.mu.Unlock()
		return BulkUpdateStatus{}, err
	}
	// Nur erreichbare Server einbeziehen - unerreichbare würden nur scheitern
	// und den Zähler verfälschen. Demo-Server sind erreichbar und werden vom
	// Paket-Job simuliert.
	var targets []domain.Server
	for i := range all {
		if all[i].Reachable {
			targets = append(targets, all[i])
		}
	}

	now := time.Now()
	s.bulk.state = BulkUpdateStatus{
		Running: true, Total: len(targets), Actor: actor, StartedAt: &now,
	}
	st := s.bulk.state
	s.bulk.mu.Unlock()

	s.audit.Log(actor, "security.bulk-update", "system", 0, "Security-Updates auf allen Servern gestartet")
	safego.Go("bulk-update", func() { s.runBulkUpdate(targets, actor) })
	return st, nil
}

// runBulkUpdate arbeitet die Server der Reihe nach ab: pro Server ein
// Security-Upgrade als Job, dann warten bis der Job terminal ist.
func (s *ServerService) runBulkUpdate(targets []domain.Server, actor string) {
	defer s.bulk.finish()
	for i := range targets {
		srv := &targets[i]
		s.bulk.setCurrent(srv.Name)
		job, err := s.UpgradeSecurityPackages(repositories.ScopeAll(), srv.ID, actor)
		if err != nil {
			// z.B. ErrServerBusy (anderer Job läuft) - als fehlgeschlagen zählen.
			s.bulk.markDone(false, srv.Name+": "+err.Error())
			continue
		}
		ok := s.waitJobTerminal(job.ID)
		s.bulk.markDone(ok, "")
	}
}

// waitJobTerminal wartet, bis ein Job einen Endzustand erreicht, und meldet,
// ob er erfolgreich war.
//
// Bewusst ohne eigene Frist: Ein Update auf schwacher Hardware darf Stunden
// dauern, und den Unterschied zwischen „langsam" und „hängt" trifft allein
// der Job-Watchdog - er beendet jeden Job, der keine Lebenszeichen mehr gibt,
// und löst damit auch dieses Warten auf. Eine zweite Uhr daneben würde nur
// den langsamen Fall abschneiden, den der Watchdog gerade durchlässt.
func (s *ServerService) waitJobTerminal(id string) bool {
	// Ist die Job-Zeile nicht mehr lesbar (gelöscht, DB-Fehler), gibt es
	// nichts mehr, worauf zu warten wäre - nach ein paar Fehlversuchen zählt
	// der Job als gescheitert, statt den Sammellauf festzuhalten.
	const (
		poll         = 300 * time.Millisecond
		maxReadFails = 10
	)
	readFails := 0
	for {
		job, err := s.jobs.Status(id)
		switch {
		case err != nil || job == nil:
			if readFails++; readFails >= maxReadFails {
				return false
			}
		case job.Status == domain.JobStatusSuccess:
			return true
		case job.Status == domain.JobStatusFailed,
			job.Status == domain.JobStatusBlocked,
			job.Status == domain.JobStatusAborted:
			return false
		default:
			readFails = 0
		}
		time.Sleep(poll)
	}
}
