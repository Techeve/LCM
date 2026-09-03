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

// ErrJobSuperseded: Der Zeitplan-Lauf kam nicht zum Zug und wird es auch
// nicht mehr - entweder stand derselbe Lauf für denselben Server schon in der
// Warteschlange, oder er hat länger gewartet, als sein eigener Takt lang ist.
// Beides ist KEIN Fehler, sondern die richtige Entscheidung: Der nächste
// Durchgang ist aktueller als er.
var ErrJobSuperseded = errors.New("zeitplan-lauf übersprungen - der nächste durchgang ist aktueller")

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

	// queues hält je Server die wartenden Zeitplan-Läufe (siehe StartOrQueue),
	// stärkster Vorrang zuerst. Sie stehen unter demselben Mutex wie active:
	// „läuft schon einer?" und „wer ist als Nächstes dran?" sind dieselbe
	// Frage und dürfen nicht auseinanderlaufen.
	queues map[uint][]*queuedJob

	// activity hält je überwachtem Job den Zeitpunkt seines letzten
	// Lebenszeichens. Bewusst mit EIGENEM Mutex: Der Eintrag wird bei jedem
	// Ausgabe-Block der Gegenseite fortgeschrieben, und diese Schreibvorgänge
	// dürfen nicht hinter mu warten, das Start() über zwei Datenbankzugriffe
	// hinweg hält - sonst stockte der Ausgabestrom aller laufenden Jobs,
	// sobald irgendwo ein neuer startet.
	//
	// Wer hier KEINEN Eintrag hat, wird nicht überwacht: reine System-Jobs
	// (Backup, CVE-Scan) lassen kein Kommando auf einem fremden System
	// laufen und können dort folglich auch nicht hängen.
	activityMu sync.Mutex
	activity   map[string]time.Time
}

// WithAudit verdrahtet das Audit-Log für Job-Abbrüche (R2-067).
func (s *JobService) WithAudit(audit *AuditService) *JobService {
	s.audit = audit
	return s
}

func NewJobService(jobs *repositories.JobRepository) *JobService {
	return &JobService{
		jobs:     jobs,
		active:   map[string]*activeJob{},
		activity: map[string]time.Time{},
		queues:   map[uint][]*queuedJob{},
	}
}

// createLocked legt die Job-Zeile an. Erwartet den gehaltenen Mutex - die
// Prüfung „läuft schon einer?" und das Anlegen müssen ein Schritt sein.
// running-Jobs bekommen ihren Startzeitpunkt, wartende nicht: Bei ihnen ist
// CreatedAt die Einreihung und StartedAt bleibt leer, bis sie wirklich laufen.
func (s *JobService) createLocked(serverID, ruleID *uint, jobType, name, triggeredBy, status string) (*domain.Job, error) {
	job := &domain.Job{
		ServerID:    serverID,
		RuleID:      ruleID,
		Type:        jobType,
		Name:        name,
		TriggeredBy: triggeredBy,
		Status:      status,
	}
	if status == domain.JobStatusRunning {
		now := time.Now()
		job.StartedAt = &now
	}
	if err := s.jobs.Create(job); err != nil {
		return nil, err
	}
	return job, nil
}

// Start legt einen Job im Status running an. Läuft auf dem Ziel-Server
// bereits ein Job, wird stattdessen ein blocked-Eintrag protokolliert
// und ErrServerBusy geliefert. serverID nil = serverloser System-Job
// (z.B. Backup), der nicht gegen Server-Locks läuft.
//
// Für Zeitplan-Läufe gibt es StartOrQueue: Die dürfen warten, statt verworfen
// zu werden. Start bleibt der Weg für unmittelbare Aktionen, hinter denen
// jemand steht und auf eine Antwort wartet.
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

// --- Warteschlange je Server ------------------------------------------------

// queuedJob ist ein wartender Zeitplan-Lauf.
type queuedJob struct {
	job      *domain.Job
	ruleID   *uint
	priority int       // Gruppen-Vorrang, kleiner = stärker
	since    time.Time // Auslösezeitpunkt - entscheidet bei gleichem Vorrang
	// ready meldet dem Wartenden, dass sein Job jetzt läuft (nil) oder warum
	// nicht. Gepuffert, damit die Weitergabe nie am Wartenden hängenbleibt.
	ready chan error
}

// QueuedStart beschreibt einen Zeitplan-Lauf, der warten darf.
type QueuedStart struct {
	ServerID    uint
	RuleID      *uint
	Type        string
	Name        string
	TriggeredBy string
	// Priority ist der Vorrang der Gruppe, aus der die Regel stammt
	// (domain.ServerGroup.Priority; kleiner = stärker, System-Gruppe am
	// schwächsten).
	Priority int
	// MaxWait ist der Takt des eigenen Zeitplans. Wer länger wartet, wird vom
	// nächsten Durchgang überholt und deshalb verworfen. 0 = gar nicht warten.
	MaxWait time.Duration
}

// StartOrQueue startet einen Zeitplan-Lauf - oder reiht ihn ein und wartet,
// bis der Server frei ist.
//
// Das ist der Unterschied zu Start: Eine unmittelbare Aktion bekommt sofort
// ErrServerBusy, weil jemand davorsteht und auf eine Antwort wartet. Ein
// Zeitplan-Lauf hat es nicht eilig - er hat einen Takt. Ihn wegzuwerfen, nur
// weil zufällig gerade ein anderer Lauf aktiv war, hieß bisher: Der
// nächtliche System-Sync fiel für die Server aus, auf denen um vier Uhr noch
// der Health-Check lief. Jetzt reiht er sich ein.
//
// Der Aufruf BLOCKIERT, bis der Job läuft oder feststeht, dass er nicht mehr
// läuft. Der Aufrufer darf dabei keine Ausführungs-Slots halten - sonst
// blockierte ein Wartender die Arbeit an ganz anderen Servern.
func (s *JobService) StartOrQueue(in QueuedStart) (*domain.Job, error) {
	job, wait, err := s.enqueue(in)
	if err != nil || wait == nil {
		return job, err // sofort gestartet oder abgelehnt
	}

	timer := time.NewTimer(in.MaxWait)
	defer timer.Stop()
	select {
	case err := <-wait.ready:
		return wait.job, err
	case <-timer.C:
		// Zu lange gewartet - aber vielleicht ist er in genau diesem Moment
		// gestartet worden. Wer das Rennen verliert, bekommt seinen Job.
		if s.dropQueued(in.ServerID, wait, "Übersprungen: hat "+in.MaxWait.String()+
			" auf einen freien Server gewartet - der nächste Durchgang dieses Zeitplans ist aktueller.") {
			return nil, ErrJobSuperseded
		}
		return wait.job, <-wait.ready
	}
}

// enqueue legt den Job an: entweder sofort laufend, oder wartend in der
// Warteschlange des Servers. wait == nil heißt „läuft bereits".
func (s *JobService) enqueue(in QueuedStart) (*domain.Job, *queuedJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	running, err := s.jobs.HasRunningForServer(in.ServerID)
	if err != nil {
		return nil, nil, err
	}
	// Freie Bahn heißt: kein laufender Job UND niemand, der schon länger
	// wartet. Ohne die zweite Bedingung überholte ein frisch ausgelöster Lauf
	// die gesamte Warteschlange.
	if !running && len(s.queues[in.ServerID]) == 0 {
		job, err := s.createLocked(&in.ServerID, in.RuleID, in.Type, in.Name, in.TriggeredBy, domain.JobStatusRunning)
		if err != nil {
			return nil, nil, err
		}
		s.active[job.ID] = &activeJob{}
		return job, nil, nil
	}

	// Derselbe Lauf steht schon an: Ein zweiter brächte nichts. Nach einem
	// einstündigen Update stünden sonst vier Health-Checks in der Schlange,
	// die alle dasselbe prüfen.
	if in.RuleID != nil {
		for _, q := range s.queues[in.ServerID] {
			if q.ruleID != nil && *q.ruleID == *in.RuleID {
				return nil, nil, ErrJobSuperseded
			}
		}
	}
	if in.MaxWait <= 0 {
		return nil, nil, ErrServerBusy
	}

	job, err := s.createLocked(&in.ServerID, in.RuleID, in.Type, in.Name, in.TriggeredBy, domain.JobStatusPending)
	if err != nil {
		return nil, nil, err
	}
	q := &queuedJob{
		job: job, ruleID: in.RuleID, priority: in.Priority,
		since: time.Now(), ready: make(chan error, 1),
	}
	s.queues[in.ServerID] = insertByPriority(s.queues[in.ServerID], q)
	slog.Debug("job queued", "job", job.ID, "name", in.Name,
		"priority", in.Priority, "position", positionOf(s.queues[in.ServerID], q),
		"queue_length", len(s.queues[in.ServerID]), "max_wait", in.MaxWait.String())
	return job, q, nil
}

// positionOf liefert den Platz eines Wartenden in der Schlange (1-basiert) -
// für das Debug-Protokoll, damit die Reihenfolge nachvollziehbar ist.
func positionOf(queue []*queuedJob, q *queuedJob) int {
	for i, other := range queue {
		if other == q {
			return i + 1
		}
	}
	return 0
}

// insertByPriority fügt einen Wartenden an seiner Stelle ein: stärkster
// Vorrang zuerst, bei gleichem Vorrang der zuerst Ausgelöste.
//
// Eingefügt statt hinterher sortiert, damit die Reihenfolge zu jedem Zeitpunkt
// gilt - auch wenn zwischen zwei Einreihungen ein Job weitergegeben wird.
func insertByPriority(queue []*queuedJob, q *queuedJob) []*queuedJob {
	at := len(queue)
	for i, other := range queue {
		if q.priority < other.priority ||
			(q.priority == other.priority && q.since.Before(other.since)) {
			at = i
			break
		}
	}
	queue = append(queue, nil)
	copy(queue[at+1:], queue[at:])
	queue[at] = q
	return queue
}

// dropQueued nimmt einen Wartenden aus der Schlange und schließt seinen Job
// mit Begründung ab. false heißt: Er stand nicht mehr drin - er wurde soeben
// gestartet, und der Aufrufer hat das Rennen verloren.
func (s *JobService) dropQueued(serverID uint, q *queuedJob, reason string) bool {
	s.mu.Lock()
	queue := s.queues[serverID]
	at := -1
	for i, other := range queue {
		if other == q {
			at = i
			break
		}
	}
	if at < 0 {
		s.mu.Unlock()
		return false
	}
	s.queues[serverID] = append(queue[:at], queue[at+1:]...)
	s.mu.Unlock()

	now := time.Now()
	q.job.Status = domain.JobStatusBlocked
	q.job.FinishedAt = &now
	q.job.Output = reason
	if err := s.jobs.Update(q.job); err != nil {
		slog.Error("queued job could not be closed", "job", q.job.ID, "error", err)
	}
	return true
}

// startNext gibt den Server an den nächsten Wartenden weiter. Aufzurufen,
// sobald ein Job endet - von dort aus fließt die Schlange.
func (s *JobService) startNext(serverID uint) {
	s.mu.Lock()
	queue := s.queues[serverID]
	if len(queue) == 0 {
		s.mu.Unlock()
		return
	}
	// Auch hier gegen die Datenbank prüfen: Zwischen dem Ende des einen und
	// der Weitergabe kann eine unmittelbare Aktion dazwischengegangen sein.
	// Sie darf vor, aber nicht neben dem Wartenden laufen.
	running, err := s.jobs.HasRunningForServer(serverID)
	if err != nil || running {
		s.mu.Unlock()
		return
	}
	next := queue[0]
	s.queues[serverID] = queue[1:]
	now := time.Now()
	next.job.Status = domain.JobStatusRunning
	next.job.StartedAt = &now
	s.active[next.job.ID] = &activeJob{}
	s.mu.Unlock()

	err = s.jobs.Update(next.job)
	if err != nil {
		s.mu.Lock()
		delete(s.active, next.job.ID)
		s.mu.Unlock()
		slog.Error("queued job could not be started", "job", next.job.ID, "error", err)
	} else {
		slog.Info("queued job started", "job", next.job.ID, "name", next.job.Name,
			"waited", time.Since(next.since).Round(time.Second).String())
	}
	next.ready <- err
}

// QueuedForServer liefert die wartenden Jobs eines Servers in der Reihenfolge,
// in der sie an die Reihe kommen - für die Anzeige „was steht an".
func (s *JobService) QueuedForServer(serverID uint) []domain.Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Job, 0, len(s.queues[serverID]))
	for _, q := range s.queues[serverID] {
		out = append(out, *q.job)
	}
	return out
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
		// Ab der ersten Verbindung ist der Job überwacht: Von jetzt an muss
		// er Lebenszeichen liefern, sonst greift der Watchdog.
		s.startWatching(jobID)
		return
	}
	finalized := ok && entry.finalized
	s.mu.Unlock()
	if finalized {
		_ = closer.Close()
	}
}

// startWatching nimmt einen Job in die Überwachung auf.
func (s *JobService) startWatching(jobID string) {
	s.activityMu.Lock()
	s.activity[jobID] = time.Now()
	s.activityMu.Unlock()
}

// stopWatching nimmt einen abgeschlossenen Job aus der Überwachung.
func (s *JobService) stopWatching(jobID string) {
	s.activityMu.Lock()
	delete(s.activity, jobID)
	s.activityMu.Unlock()
}

// MarkActivity hält fest, dass der Job gerade arbeitet: Ausgabe der
// Gegenseite, Beginn oder Ende eines Kommandos, ein Takt einer Warteschleife.
// Der Watchdog bricht nur ab, was sich über die erlaubte Stille hinaus gar
// nicht mehr meldet - eine feste Maximaldauer gibt es nicht, weil sie einen
// langsamen Rechner und einen hängenden Prozess nicht unterscheiden kann.
//
// Nicht überwachte und bereits abgeschlossene Jobs sind ein No-Op: Der Eintrag
// entsteht mit der ersten Verbindung und verschwindet mit dem Abschluss.
func (s *JobService) MarkActivity(jobID string) {
	s.activityMu.Lock()
	if _, ok := s.activity[jobID]; ok {
		s.activity[jobID] = time.Now()
	}
	s.activityMu.Unlock()
}

// lastActivity liefert das letzte Lebenszeichen eines Jobs. ok=false, wenn er
// nicht überwacht wird - dann gibt es nichts abzubrechen.
func (s *JobService) lastActivity(jobID string) (time.Time, bool) {
	s.activityMu.Lock()
	defer s.activityMu.Unlock()
	at, ok := s.activity[jobID]
	return at, ok
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
	s.stopWatching(job.ID)
	// Der Server ist frei - der nächste Wartende darf. Auch im
	// Abbruch-Fall: Dort hat abort() die Zeile schon geschlossen, die
	// Warteschlange muss trotzdem weiterfließen.
	defer s.releaseServer(job)
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
	switch job.Status {
	case domain.JobStatusRunning:
		return s.abort(job, actor, "Manuell abgebrochen von "+actor+" - die Server-Sperre wurde aufgehoben.")
	case domain.JobStatusPending:
		// Ein Wartender lässt sich ebenso abbrechen wie ein laufender: Er
		// steht sichtbar in der Liste, also muss man ihn dort auch wieder
		// herausnehmen können. Eine Sperre gibt er nicht frei - er hatte nie
		// eine.
		return s.abort(job, actor, "Manuell aus der Warteschlange genommen von "+actor+".")
	default:
		return nil, ErrJobNotRunning
	}
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
	s.stopWatching(job.ID)

	if entry == nil {
		// Kein Laufzeit-Handle (mehr): Der Job könnte soeben regulär beendet
		// worden sein - Status neu laden, damit kein Erfolg überschrieben wird.
		fresh, err := s.jobs.FindByID(repositories.AccessScope{All: true}, job.ID)
		if err != nil {
			return nil, err
		}
		switch fresh.Status {
		case domain.JobStatusRunning:
		case domain.JobStatusPending:
			// Ein Wartender lässt sich ebenfalls abbrechen - er steht
			// sichtbar in der Liste, also muss man ihn auch wieder
			// herausnehmen können. Er hält keine Verbindung; frei wird nur
			// sein Platz in der Schlange.
			s.removeFromQueue(fresh)
		default:
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
	s.releaseServer(job)
	return job, nil
}

// removeFromQueue nimmt einen wartenden Job aus der Schlange seines Servers -
// ohne seine Zeile anzufassen; die schließt der Aufrufer.
func (s *JobService) removeFromQueue(job *domain.Job) {
	if job.ServerID == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	queue := s.queues[*job.ServerID]
	for i, q := range queue {
		if q.job.ID != job.ID {
			continue
		}
		s.queues[*job.ServerID] = append(queue[:i], queue[i+1:]...)
		// Den Wartenden aufwecken, sonst hinge seine Goroutine bis zum
		// Ablauf seiner Frist.
		q.ready <- ErrJobSuperseded
		return
	}
}

// releaseServer gibt den Server eines beendeten Jobs an den nächsten
// Wartenden weiter. Serverlose System-Jobs (Backup, Cleanup) haben keine
// Warteschlange.
func (s *JobService) releaseServer(job *domain.Job) {
	if job != nil && job.ServerID != nil {
		s.startNext(*job.ServerID)
	}
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
	var failed, selfUpdate, queued int
	for i := range jobs {
		job := &jobs[i]
		now := time.Now()
		job.FinishedAt = &now
		if job.Output != "" {
			job.Output += "\n"
		}
		switch {
		case job.Status == domain.JobStatusPending:
			// Er stand nur in der Warteschlange und hat nie gelaufen - ihn als
			// Fehlschlag zu melden, wäre schlicht falsch. Sein Zeitplan holt
			// ihn beim nächsten Durchgang ohnehin nach.
			job.Status = domain.JobStatusBlocked
			job.Output += "Nicht ausgeführt: stand beim Dienst-Neustart noch in der Warteschlange."
			queued++
		case self != nil && self.Match != nil && self.Match(job):
			job.Status = domain.JobStatusSuccess
			job.ExitCode = new(int) // 0 - der Lauf hat sein Ziel erreicht
			job.Output += self.Note
			selfUpdate++
		default:
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
	if queued > 0 {
		slog.Info("queued jobs discarded after restart", "count", queued)
	}
}

// DefaultJobIdleTimeout ist die erlaubte Stille, solange die Einstellungen
// nicht lesbar sind - großzügig gewählt: Im Zweifel ist ein Lauf, der noch
// arbeiten könnte, weniger schädlich als ein grundlos abgeschnittener.
const DefaultJobIdleTimeout = 30 * time.Minute

// JobIdleLimit baut die Nachschlagefunktion für RunWatchdog: Sie liefert die
// erlaubte Stille eines Jobs aus den globalen Einstellungen - für Jobs auf
// schwacher Hardware den größeren Wert, weil dort lange stille Phasen zum
// Normalbetrieb gehören (siehe domain.Server.IsSlowHardware).
func JobIdleLimit(settings *repositories.SettingsRepository, servers *repositories.ServerRepository) func(*domain.Job) time.Duration {
	return func(job *domain.Job) time.Duration {
		st, err := settings.Get()
		if err != nil {
			return DefaultJobIdleTimeout
		}
		if st.JobIdleTimeoutMinutes <= 0 {
			return 0 // Watchdog aus
		}
		minutes := st.JobIdleTimeoutMinutes
		if job.ServerID != nil {
			if server, err := servers.FindByIDUnscoped(*job.ServerID); err == nil && server.IsSlowHardware() {
				minutes = st.JobIdleTimeoutSlowMinutes
			}
		}
		return time.Duration(domain.ClampJobIdleTimeout(minutes)) * time.Minute
	}
}

// watchdogInterval ist der Prüfabstand des Job-Watchdogs.
const watchdogInterval = time.Minute

// RunWatchdog bricht Jobs ab, die keine Lebenszeichen mehr geben, und gibt
// damit die Server-Sperre wieder frei. Läuft als Goroutine über die gesamte
// Prozess-Lebensdauer.
//
// Maßgeblich ist die STILLE, nicht die Gesamtdauer: Solange auf der Gegenseite
// Ausgabe entsteht, arbeitet der Lauf - ob er dafür zehn Minuten oder sechs
// Stunden braucht, ist die Sache des Rechners, nicht die des Watchdogs. Ein
// Upgrade über 200 Pakete auf einem alten Raspberry Pi ist kein Fehlerfall;
// eine feste Maximaldauer hätte genau diesen Lauf mitten im dpkg-Durchgang
// abgeschnitten. Umgekehrt ist ein apt, das seit einer halben Stunde auf einen
// dpkg-Lock wartet, auch dann tot, wenn die Frist noch lange nicht um wäre.
//
// idleLimit liefert die erlaubte Stille für einen Job (0 = nicht überwachen)
// - sie hängt am Ziel-Server, weil schwache Hardware längere stille Phasen
// hat (siehe domain.Server.IsSlowHardware).
func (s *JobService) RunWatchdog(idleLimit func(job *domain.Job) time.Duration) {
	ticker := time.NewTicker(watchdogInterval)
	defer ticker.Stop()
	for range ticker.C {
		running, err := s.jobs.FindRunning()
		if err != nil {
			slog.Error("job watchdog: running jobs not determinable", "error", err)
			continue
		}
		for i := range running {
			s.abortIfStalled(&running[i], idleLimit)
		}
	}
}

// abortIfStalled bricht einen Job ab, wenn seit dem letzten Lebenszeichen
// mehr Zeit vergangen ist, als für ihn erlaubt ist.
func (s *JobService) abortIfStalled(job *domain.Job, idleLimit func(job *domain.Job) time.Duration) {
	limit := idleLimit(job)
	if limit <= 0 {
		return
	}
	last, ok := s.lastActivity(job.ID)
	if !ok {
		return // kein Laufzeit-Handle oder keine Quelle für Lebenszeichen
	}
	idle := time.Since(last)
	if idle < limit {
		return
	}
	reason := "Ohne Lebenszeichen: seit " + idle.Round(time.Second).String() +
		" kam keine Ausgabe mehr (erlaubt sind " + limit.String() +
		") - der Lauf gilt als hängend, die Server-Sperre wurde aufgehoben."
	if _, err := s.abort(job, "job-watchdog", reason); err != nil {
		if !errors.Is(err, ErrJobNotRunning) {
			slog.Error("job watchdog: abort failed", "job", job.ID, "error", err)
		}
		return
	}
	slog.Warn("job watchdog: job aborted after silence",
		"job", job.ID, "name", job.Name, "idle", idle.String(), "idle_limit", limit.String())
}
