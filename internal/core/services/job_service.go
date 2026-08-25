package services

import (
	"errors"
	"io"
	"log/slog"
	"sync"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// ErrServerBusy: Auf dem Server läuft bereits ein Job - parallel
// getriggerte Jobs werden blockiert, um Systemkollisionen zu verhindern
// (Concurrency Control laut Spezifikation).
var ErrServerBusy = errors.New("auf diesem server läuft bereits ein job - ausführung blockiert")

// ErrJobNotRunning: Abbruch angefordert, aber der Job läuft (nicht mehr).
var ErrJobNotRunning = errors.New("dieser job läuft nicht (mehr)")

// activeJob ist das Laufzeit-Handle eines laufenden Jobs: die zugehörigen
// SSH-Verbindungen (zum Zwangs-Schließen beim Abbruch) und das
// Finalisierungs-Flag, das das Rennen zwischen Abort und Complete entscheidet.
type activeJob struct {
	closers   []io.Closer
	finalized bool // true: Abort hat den Job bereits abgeschlossen
}

// JobService verwaltet den Lebenszyklus aller Jobs und erzwingt den
// Concurrency-Lock pro Server.
type JobService struct {
	jobs *repositories.JobRepository
	// audit protokolliert Abbrüche (manuell + Watchdog) im revisionssicheren
	// Log. Vorher fehlte der Watchdog-Abbruch dort ganz und der manuelle
	// verwies mit entity_id=0 auf keinen Job (R2-067). Optional (nil = kein
	// Audit; Tests).
	audit *AuditService

	// mu serialisiert die Prüfung "läuft schon ein Job?" + Job-Anlage,
	// damit zwei gleichzeitige Trigger nicht beide durchkommen. Schützt
	// zugleich die active-Registry.
	mu sync.Mutex
	// active hält die Laufzeit-Handles aller in DIESEM Prozess laufenden
	// Jobs (jobID → Handle) - Grundlage für Abbruch und Watchdog.
	active map[string]*activeJob
}

// WithAudit verdrahtet das Audit-Log für Job-Abbrüche (R2-067).
func (s *JobService) WithAudit(audit *AuditService) *JobService {
	s.audit = audit
	return s
}

func NewJobService(jobs *repositories.JobRepository) *JobService {
	return &JobService{jobs: jobs, active: map[string]*activeJob{}}
}

// Start legt einen Job im Status running an. Läuft auf dem Ziel-Server
// bereits ein Job, wird stattdessen ein blocked-Eintrag protokolliert
// und ErrServerBusy geliefert. serverID nil = serverloser System-Job
// (z.B. Backup), der nicht gegen Server-Locks läuft.
func (s *JobService) Start(serverID, ruleID *uint, jobType, name, triggeredBy string) (*domain.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	job := &domain.Job{
		ServerID:    serverID,
		RuleID:      ruleID,
		Type:        jobType,
		Name:        name,
		TriggeredBy: triggeredBy,
		Status:      domain.JobStatusRunning,
		StartedAt:   &now,
	}

	if serverID != nil {
		running, err := s.jobs.HasRunningForServer(*serverID)
		if err != nil {
			return nil, err
		}
		if running {
			job.Status = domain.JobStatusBlocked
			job.FinishedAt = &now
			job.Output = "Blockiert: auf dem Server lief zum Trigger-Zeitpunkt bereits ein Job."
			_ = s.jobs.Create(job)
			return job, ErrServerBusy
		}
	}
	if err := s.jobs.Create(job); err != nil {
		return nil, err
	}
	// Laufzeit-Handle registrieren - darüber können Abort und Watchdog den
	// Job später abbrechen und seine Verbindungen schließen.
	s.active[job.ID] = &activeJob{}
	return job, nil
}

// AttachCloser hängt eine offene Verbindung an einen laufenden Job - beim
// Abbruch (manuell oder Watchdog) wird sie zwangsweise geschlossen, wodurch
// ein hängendes Remote-Kommando sofort mit Fehler zurückkehrt. Wurde der Job
// bereits abgebrochen, wird die Verbindung direkt geschlossen (Abbruch war
// schneller als der Verbindungsaufbau).
func (s *JobService) AttachCloser(jobID string, closer io.Closer) {
	if closer == nil {
		return
	}
	s.mu.Lock()
	entry, ok := s.active[jobID]
	if ok && !entry.finalized {
		entry.closers = append(entry.closers, closer)
		s.mu.Unlock()
		return
	}
	finalized := ok && entry.finalized
	s.mu.Unlock()
	if finalized {
		_ = closer.Close()
	}
}

// Complete schließt einen Job ab: Output (exakter Konsolen-Output),
// Exit-Code und Erfolg/Fehlschlag werden permanent protokolliert.
// Wurde der Job zwischenzeitlich abgebrochen (Abort/Watchdog), ist die
// DB-Zeile bereits finalisiert - dann wird sie nicht mehr überschrieben.
func (s *JobService) Complete(job *domain.Job, output string, exitCode *int, runErr error) {
	s.mu.Lock()
	entry := s.active[job.ID]
	delete(s.active, job.ID)
	aborted := entry != nil && entry.finalized
	s.mu.Unlock()
	if aborted {
		return
	}

	now := time.Now()
	job.FinishedAt = &now
	// Secrets aus dem Job-Output entfernen, bevor er (at-rest unverschlüsselt)
	// in jobs.output gespeichert wird - dieselbe Redaction wie im SSH-Protokoll.
	// Ein Kommando einer Custom-Aktion/Regel könnte sonst ein Passwort ausgeben.
	output = redactSecrets(output)
	job.Output = output
	job.ExitCode = exitCode
	if runErr != nil {
		job.Status = domain.JobStatusFailed
		// Das strukturierte exit_code-Feld muss den Fehlschlag widerspiegeln:
		// NIE null (836 von 849 fehlgeschlagenen Jobs im Langzeittest) und
		// NIE 0 (dort las ein Aufrufer, der auf exit_code==0 prüft, einen
		// Erfolg). Ein echter, von Null verschiedener Code (z.B. 3 aus einer
		// Custom Action) bleibt erhalten; fehlt er oder ist er 0, tritt ein
		// Sentinel an seine Stelle (R2-065).
		if exitCode == nil || *exitCode == 0 {
			code := 1
			job.ExitCode = &code
		}
		if output != "" {
			job.Output = output + "\n"
		}
		job.Output += "FEHLER: " + runErr.Error()
	} else {
		job.Status = domain.JobStatusSuccess
	}
	_ = s.jobs.Update(job)
}

// HistoryFiltered liefert die gefilterte, seitenweise Job-Historie samt
// Gesamtanzahl.
func (s *JobService) HistoryFiltered(scope repositories.AccessScope, f repositories.JobFilter) ([]domain.Job, int64, error) {
	return s.jobs.HistoryFiltered(scope, f)
}

// FilterOptions liefert distinkte Typen und Auslöser für die Filter-Dropdowns.
func (s *JobService) FilterOptions(scope repositories.AccessScope, serverID uint) (types, triggeredBy []string, err error) {
	return s.jobs.FilterOptions(scope, serverID)
}

// ConsoleOutput liefert den exakten Stdout/Stderr-Output eines Jobs
// zur Fehleranalyse im Web-Interface.
func (s *JobService) ConsoleOutput(scope repositories.AccessScope, id string) (*domain.Job, string, error) {
	job, err := s.jobs.FindByID(scope, id)
	if err != nil {
		return nil, "", err
	}
	return job, job.Output, nil
}

// CleanupOlderThan setzt die Log-Retention um (Default 90 Tage).
func (s *JobService) CleanupOlderThan(days int) (int64, error) {
	if days <= 0 {
		return 0, nil
	}
	cutoff := time.Now().AddDate(0, 0, -days)
	return s.jobs.DeleteOlderThan(cutoff)
}

// RunningForServer liefert den aktuell laufenden Job eines Servers (nil,
// wenn keiner läuft) - für die Laufender-Job-Anzeige im Server-Detail.
func (s *JobService) RunningForServer(serverID uint) (*domain.Job, error) {
	return s.jobs.RunningForServer(serverID)
}

// Status liefert den aktuellen Zustand eines Jobs (systemintern, ohne
// Scope-Prüfung) - z.B. um im Bulk-Update auf den Abschluss zu warten.
func (s *JobService) Status(id string) (*domain.Job, error) {
	return s.jobs.FindByID(repositories.ScopeAll(), id)
}

// Abort bricht einen laufenden Job manuell ab (Aufhebung der Server-Sperre):
// die angehängten Verbindungen werden geschlossen und der Job als failed
// protokolliert. Die Sichtbarkeits-Prüfung läuft über den Scope des Aufrufers.
func (s *JobService) Abort(scope repositories.AccessScope, id, actor string) (*domain.Job, error) {
	job, err := s.jobs.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	if job.Status != domain.JobStatusRunning {
		return nil, ErrJobNotRunning
	}
	return s.abort(job, actor, "Manuell abgebrochen von "+actor+" - die Server-Sperre wurde aufgehoben.")
}

// abort finalisiert einen laufenden Job als failed und schließt seine
// Verbindungen. Das finalized-Flag sorgt dafür, dass ein späteres Complete
// der (ggf. noch laufenden) Goroutine die Zeile nicht mehr überschreibt.
func (s *JobService) abort(job *domain.Job, actor, reason string) (*domain.Job, error) {
	s.mu.Lock()
	entry := s.active[job.ID]
	var closers []io.Closer
	if entry != nil {
		if entry.finalized {
			s.mu.Unlock()
			return nil, ErrJobNotRunning
		}
		entry.finalized = true
		closers = entry.closers
		entry.closers = nil
	}
	s.mu.Unlock()

	if entry == nil {
		// Kein Laufzeit-Handle (mehr): Der Job könnte soeben regulär beendet
		// worden sein - Status neu laden, damit kein Erfolg überschrieben wird.
		fresh, err := s.jobs.FindByID(repositories.AccessScope{All: true}, job.ID)
		if err != nil {
			return nil, err
		}
		if fresh.Status != domain.JobStatusRunning {
			return nil, ErrJobNotRunning
		}
		job = fresh
	}

	// Verbindungen zwangsweise schließen: hängende Remote-Kommandos kehren
	// damit sofort mit Transportfehler zurück, die Goroutine endet sauber.
	for _, c := range closers {
		_ = c.Close()
	}

	now := time.Now()
	job.FinishedAt = &now
	job.Status = domain.JobStatusAborted
	if job.Output != "" {
		job.Output += "\n"
	}
	job.Output += "ABGEBROCHEN: " + reason
	if err := s.jobs.Update(job); err != nil {
		return nil, err
	}
	if s.audit != nil {
		// Job-UUID + Name im details, da entity_id (INTEGER) die UUID nicht
		// fassen kann (R2-067). Watchdog-Abbruch trägt den Actor
		// "job-watchdog", damit er im Protokoll vom manuellen Abbruch
		// unterscheidbar ist.
		s.audit.Log(actor, "job.abort", "job", 0,
			"job "+job.ID+" ("+job.Name+"): "+reason)
	}
	return job, nil
}

// SelfUpdateRecovery beschreibt den einen Job-Abbruch, der keiner ist: Der
// Lauf hat LCM selbst aktualisiert, der Dienst wurde dabei neu gestartet und
// hat seine eigene Verbindung verloren. Match erkennt die betroffenen Jobs,
// Note begründet den Abschluss im Protokoll.
type SelfUpdateRecovery struct {
	Match func(*domain.Job) bool
	Note  string
}

// FailInterruptedOnStartup räumt verwaiste Jobs nach einem Dienst-Neustart
// auf: alle noch als laufend/wartend markierten Einträge werden abgeschlossen.
// Ohne diesen Schritt hielte ein beim Neustart unterbrochener Job die
// Server-Sperre für immer (Jobs laufen nur im Prozess - nach einem Neustart
// KANN keiner mehr legitim laufen). VOR dem Scheduler-Start aufrufen.
//
// Regelfall ist „failed": der Job hat sein Ziel nachweislich nicht erreicht.
// self (optional, nil = kein Versionswechsel bei diesem Start) hebt den
// Sonderfall heraus, in dem genau das Gegenteil gilt - das Update, das den
// Dienst mitgenommen hat, ist durchgelaufen, sonst liefe jetzt nicht die neue
// Version. Solche Jobs als Fehler zu melden, wäre schlicht falsch.
func (s *JobService) FailInterruptedOnStartup(self *SelfUpdateRecovery) {
	jobs, err := s.jobs.FindUnfinished()
	if err != nil {
		slog.Error("orphaned jobs could not be determined", "error", err)
		return
	}
	var failed, selfUpdate int
	for i := range jobs {
		job := &jobs[i]
		now := time.Now()
		job.FinishedAt = &now
		if job.Output != "" {
			job.Output += "\n"
		}
		if self != nil && self.Match != nil && self.Match(job) {
			job.Status = domain.JobStatusSuccess
			job.ExitCode = new(int) // 0 - der Lauf hat sein Ziel erreicht
			job.Output += self.Note
			selfUpdate++
		} else {
			job.Status = domain.JobStatusFailed
			job.Output += "ABGEBROCHEN: Vom Dienst-Neustart unterbrochen - die Server-Sperre wurde aufgehoben."
			failed++
		}
		if err := s.jobs.Update(job); err != nil {
			slog.Error("orphaned job could not be completed", "job", job.ID, "error", err)
		}
	}
	if failed > 0 {
		slog.Warn("orphaned jobs marked as failed after restart", "count", failed)
	}
	if selfUpdate > 0 {
		slog.Info("orphaned jobs completed as self-update after restart", "count", selfUpdate)
	}
}

// watchdogInterval ist der Prüfabstand des Job-Watchdogs.
const watchdogInterval = time.Minute

// RunWatchdog prüft periodisch auf Jobs, die die konfigurierte Maximaldauer
// überschritten haben, und bricht sie ab (Sperre wird frei). maxRuntime wird
// pro Tick gelesen (0 = Watchdog aus). Läuft als Goroutine über die gesamte
// Prozess-Lebensdauer.
func (s *JobService) RunWatchdog(maxRuntime func() time.Duration) {
	ticker := time.NewTicker(watchdogInterval)
	defer ticker.Stop()
	for range ticker.C {
		limit := maxRuntime()
		if limit <= 0 {
			continue
		}
		running, err := s.jobs.FindRunning()
		if err != nil {
			slog.Error("job watchdog: running jobs not determinable", "error", err)
			continue
		}
		cutoff := time.Now().Add(-limit)
		for i := range running {
			job := &running[i]
			// Laufzeit-Vergleich in Go (robust gegen Zeitzonen-Offsets in der DB).
			if job.StartedAt == nil || !job.StartedAt.Before(cutoff) {
				continue
			}
			if _, err := s.abort(job, "job-watchdog", "Zeitüberschreitung (Watchdog): Job lief länger als die Maximaldauer von "+
				limit.String()+" - die Server-Sperre wurde aufgehoben."); err != nil {
				if !errors.Is(err, ErrJobNotRunning) {
					slog.Error("job watchdog: abort failed", "job", job.ID, "error", err)
				}
				continue
			}
			slog.Warn("job watchdog: job aborted after timeout",
				"job", job.ID, "name", job.Name, "max_duration", limit.String())
		}
	}
}
