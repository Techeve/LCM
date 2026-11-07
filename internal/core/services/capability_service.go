package services

import (
	"log/slog"
	"strings"
	"sync"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// Betriebsfähigkeit der Server (Release A).
//
// Der Dienst übersetzt das Ende jedes Jobs in den Zustand je (Server,
// Kategorie): Ein Fehlschlag setzt NUR die Kategorie des Jobs auf failed, und
// nur ein Erfolg, der sie heilt (ServerCapability.Heals), setzt sie zurück. Ein
// Health-Ping überschreibt damit kein fehlgeschlagenes Update mehr.

const (
	// connectionHealInterval: Ein Erfolg schreibt die Verbindungs-Zeile nur
	// dann neu, wenn sich etwas ändert oder der letzte Erfolg so alt ist.
	// Health-Pings laufen alle paar Minuten auf jedem Server - ein Schreibzugriff
	// je Ping wäre bei großer Flotte reine Last für einen unveränderten Wert.
	connectionHealInterval = 10 * time.Minute

	// errorLinePrefix leitet die Fehlerzeile ein, die JobService.Complete an
	// den Output eines fehlgeschlagenen Jobs hängt. connectionErrorPrefix
	// beginnt der Text, wenn der Lauf an der Verbindung scheiterte: Alle
	// Ausführungswege schließen dann mit fmt.Errorf("verbindung: %w", err) ab.
	// ("verbindung mit altem key: …" o. Ä. zählt nicht.)
	errorLinePrefix       = "FEHLER: "
	connectionErrorPrefix = "verbindung: "

	// warningAptNotice: apt meldet W:-Zeilen, aber keine kaputte Quelle.
	warningAptNotice = "apt_warning"
)

// CapabilityService führt die Betriebsfähigkeit der Server nach.
type CapabilityService struct {
	repo *repositories.CapabilityRepository

	// mu serialisiert Lesen-Ändern-Schreiben. Jobs EINES Servers laufen
	// nacheinander, der flottenweite CVE-Scan schreibt aber für viele Server
	// zugleich, und zwei Jobs desselben Servers können gleichzeitig enden.
	mu sync.Mutex

	// onTransition erfährt, wenn eine Kategorie alarmwürdig wird oder eine
	// alarmwürdige geheilt ist. Optional; wird außerhalb von mu gerufen.
	onTransition func(serverID uint, c domain.ServerCapability, recovered bool)

	// nothingPending prüft, ob auf dem Server noch Updates offen sind.
	// Optional - ohne sie heilt nur ein erfolgreiches Update.
	nothingPending func(serverID uint) (bool, error)
}

func NewCapabilityService(repo *repositories.CapabilityRepository) *CapabilityService {
	return &CapabilityService{repo: repo}
}

// WithNothingPending verdrahtet die Prüfung auf offene Updates.
func (s *CapabilityService) WithNothingPending(fn func(serverID uint) (bool, error)) *CapabilityService {
	s.nothingPending = fn
	return s
}

// OnTransition meldet künftig jeden Wechsel in oder aus dem Alarmzustand an fn.
// recovered=true: Die Kategorie war alarmwürdig und ist jetzt in Ordnung.
func (s *CapabilityService) OnTransition(fn func(serverID uint, c domain.ServerCapability, recovered bool)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onTransition = fn
}

// transitionHook liest den Abonnenten unter der Sperre: Der Watchdog läuft
// schon, wenn main die Alarme verdrahtet, und kann dabei ein Jobende melden.
func (s *CapabilityService) transitionHook() func(serverID uint, c domain.ServerCapability, recovered bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.onTransition
}

// RecordJob wertet das Ende eines Jobs aus (Abonnent von JobService.OnOutcome).
func (s *CapabilityService) RecordJob(o JobOutcome) {
	job := o.Job
	if job == nil || job.ServerID == nil || o.Skipped {
		return
	}
	switch {
	case job.Status == domain.JobStatusSuccess:
		s.recordSuccess(*job.ServerID, job)
	case job.Status == domain.JobStatusFailed, job.Status == domain.JobStatusAborted && o.ByWatchdog:
		s.recordFailure(*job.ServerID, o)
	}
	// Alles andere (manueller Abbruch, blockiert, wartend) sagt nichts über
	// den Server.
}

// RecordCVEScan hält das Ergebnis des flottenweiten CVE-Scans fest. Dieser
// Lauf ist ein serverloser System-Job und erreicht RecordJob nicht. err nil =
// Erfolg.
func (s *CapabilityService) RecordCVEScan(serverID uint, err error) {
	now := time.Now()
	if err == nil {
		s.update(serverID, domain.CapabilityCVEScan, func(c *domain.ServerCapability) bool {
			c.LastSuccessAt = &now
			if c.State == domain.CapabilityFailed && c.Heals(&domain.Job{Type: domain.RuleTypeCVEScan}) {
				resetToOK(c)
			}
			return true
		})
		return
	}
	f := domain.ClassifyFailure(domain.FailureInput{Output: err.Error()})
	s.update(serverID, domain.CapabilityCVEScan, func(c *domain.ServerCapability) bool {
		applyFailure(c, f, &domain.Job{Type: domain.RuleTypeCVEScan, Name: "CVE-Scan"}, now)
		return true
	})
}

// --- Fehlschlag --------------------------------------------------------------------

func (s *CapabilityService) recordFailure(serverID uint, o JobOutcome) {
	category, f, ok := failureOf(o)
	if !ok {
		return
	}
	now := time.Now()
	s.update(serverID, category, func(c *domain.ServerCapability) bool {
		applyFailure(c, f, o.Job, now)
		return true
	})
}

// failureOf bestimmt Kategorie und Klasse eines Fehlschlags. ok=false: Er
// betrifft keine Kategorie.
func failureOf(o JobOutcome) (category string, f domain.Failure, ok bool) {
	job := o.Job

	// Konnte LCM den Server nicht erreichen, ist die Verbindung betroffen -
	// gleich, welche Aktion es versuchte. Das Update-Paket ist dann nicht
	// kaputt, es kam nur nicht an.
	if line, found := connectionFailure(job.Output); found {
		f = domain.Failure{Class: domain.ErrClassUnknown, Message: line}
		if domain.ClassifyFailure(domain.FailureInput{Output: line}).Class == domain.ErrClassNetwork {
			f.Class = domain.ErrClassNetwork
		}
		return domain.CapabilityConnection, f, true
	}

	category = domain.CapabilityCategoryFor(job.Type)
	// Ein Health-Ping, der den Server erreicht hat und trotzdem scheitert,
	// scheitert an den mitlaufenden Grundsatz-Regeln - die Verbindung steht.
	// Beendet ihn der Watchdog, hing er dagegen: Das betrifft die Verbindung,
	// und die heilt der nächste Erfolg, ohne dass dieselbe Regel laufen muss.
	if category == domain.CapabilityConnection && !o.ByWatchdog {
		category = domain.CapabilityRules
	}
	if category == "" {
		return "", f, false
	}
	// Ein nicht erreichbarer Server wird beim CVE-Scan bewusst übersprungen
	// (ErrCVEScanUnreachable) - auch im Einzelscan kein Fehlschlag des Scans.
	if job.Type == domain.RuleTypeCVEScan && strings.Contains(job.Output, ErrCVEScanUnreachable.Error()) {
		return "", f, false
	}
	in := domain.FailureInput{
		ExitCode: job.ExitCode, Output: job.Output, ByWatchdog: o.ByWatchdog,
		PackageJob: category == domain.CapabilityPackages,
	}
	return category, domain.ClassifyFailure(in), true
}

// connectionFailure liefert die Fehlerzeile (ohne "FEHLER: "), wenn der Job an
// der Verbindung scheiterte. Complete hängt die Fehlerzeile zuletzt an - die
// letzte "FEHLER: "-Zeile ist daher die des Jobs, nicht eine, die ein Skript
// zufällig ausgegeben hat.
func connectionFailure(output string) (string, bool) {
	i := strings.LastIndex(output, "\n"+errorLinePrefix)
	switch {
	case i >= 0:
		i++ // den Zeilenumbruch überspringen
	case strings.HasPrefix(output, errorLinePrefix):
		i = 0
	default:
		return "", false
	}
	line, _, _ := strings.Cut(output[i+len(errorLinePrefix):], "\n")
	return line, strings.HasPrefix(line, connectionErrorPrefix)
}

func applyFailure(c *domain.ServerCapability, f domain.Failure, job *domain.Job, now time.Time) {
	if c.State == domain.CapabilityFailed {
		c.ConsecutiveFailures++
	} else {
		c.State = domain.CapabilityFailed
		c.FailingSince = &now
		c.ConsecutiveFailures = 1
	}
	c.LastErrorClass = f.Class
	c.LastErrorMessage = f.Message
	c.LastErrorParams = f.Params
	c.LastJobID, c.LastJobType, c.LastJobName, c.LastRuleID = job.ID, job.Type, job.Name, job.RuleID
}

// --- Erfolg ------------------------------------------------------------------------

func (s *CapabilityService) recordSuccess(serverID uint, job *domain.Job) {
	// Wer einen Job ausführen konnte, erreicht den Server.
	s.healConnection(serverID)
	s.healWhenNothingPending(serverID, job)

	category := domain.CapabilityCategoryFor(job.Type)
	if category == domain.CapabilityConnection {
		s.healEnforcement(serverID, job)
		return
	}
	if category == "" {
		return
	}
	now := time.Now()
	s.update(serverID, category, func(c *domain.ServerCapability) bool {
		// Der Stillstandsprüfung zählt der letzte ERFOLGREICHE Lauf - auch
		// dann, wenn dieser Erfolg die Kategorie nicht heilt.
		c.LastSuccessAt = &now
		if c.State == domain.CapabilityFailed && c.Heals(job) {
			resetToOK(c)
		}
		if job.Type == domain.RuleTypePackageScan {
			setPackageWarning(c, job.Output)
		}
		return true
	})
}

// healWhenNothingPending: Ein gescheitertes Update heilt sonst nur ein
// erfolgreiches. Findet ein Scan nichts mehr zu aktualisieren, käme dieses nie,
// und die Meldung bliebe stehen, obwohl nichts mehr zu tun ist. Fällt eine
// Paketquelle aus, kann der Scan offene Updates übersehen, dann bleibt sie.
func (s *CapabilityService) healWhenNothingPending(serverID uint, job *domain.Job) {
	if s.nothingPending == nil || (job.Type != domain.RuleTypePackageScan && job.Type != JobTypeFullRefresh) {
		return
	}
	if domain.ClassifyRefresh(job.Output).PartiallyFailed() {
		return
	}
	if done, err := s.nothingPending(serverID); err != nil || !done {
		return
	}
	s.update(serverID, domain.CapabilityPackages, func(c *domain.ServerCapability) bool {
		if c.State != domain.CapabilityFailed || !c.InstallFailed() {
			return false
		}
		resetToOK(c)
		return true
	})
}

// healConnection setzt die Verbindung nach einem Erfolg zurück - und
// schreibt bei unverändertem Zustand nur selten (connectionHealInterval).
func (s *CapabilityService) healConnection(serverID uint) {
	now := time.Now()
	s.update(serverID, domain.CapabilityConnection, func(c *domain.ServerCapability) bool {
		unchanged := c.State == domain.CapabilityOK && c.LastSuccessAt != nil &&
			now.Sub(*c.LastSuccessAt) < connectionHealInterval
		if unchanged {
			return false
		}
		c.LastSuccessAt = &now
		resetToOK(c)
		return true
	})
}

// healEnforcement: Ist der Regel-Abgleich eines früheren Health-Pings
// gescheitert (failureOf ordnet ihn rules zu), heilt ihn nur ein Health-Ping
// derselben Quelle, dessen Abgleich wieder durchläuft (HealsRules). Andere
// Regel-Fehler bleiben stehen - auch der Erfolg eines anderen Health-Pings.
func (s *CapabilityService) healEnforcement(serverID uint, job *domain.Job) {
	s.update(serverID, domain.CapabilityRules, func(c *domain.ServerCapability) bool {
		if c.State != domain.CapabilityFailed || !c.HealsRules(job) {
			return false
		}
		resetToOK(c)
		return true
	})
}

// resetToOK setzt eine Kategorie zurück. LastSuccessAt und die Warnung bleiben:
// Sie gehören zum Erfolg, nicht zum Fehler.
func resetToOK(c *domain.ServerCapability) {
	c.State = domain.CapabilityOK
	c.FailingSince = nil
	c.ConsecutiveFailures = 0
	c.LastErrorClass = ""
	c.LastErrorMessage = ""
	c.LastErrorParams = nil
}

// setPackageWarning hält den Hinweis eines ERFOLGREICHEN Paketlisten-Laufs
// fest: eine kaputte Quelle unter vielen (gelb, apt und zypper) oder bloße
// apt-Warnungen. Den Paketmanager erkennt ClassifyRefresh an der Ausgabe.
func setPackageWarning(c *domain.ServerCapability, output string) {
	r := domain.ClassifyRefresh(output)
	switch {
	case r.PartiallyFailed():
		c.WarningClass = domain.ErrClassRepoError
		c.WarningMessage = strings.Join(r.FailedSources, ", ")
	case r.Warnings:
		c.WarningClass = warningAptNotice
		c.WarningMessage = firstAptWarning(output)
	default:
		c.WarningClass, c.WarningMessage = "", ""
	}
}

// firstAptWarning gibt die erste W:-Zeile zurück. Die Länge kürzt
// ServerCapability.BeforeSave.
func firstAptWarning(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if line = strings.TrimSpace(line); strings.HasPrefix(line, "W: ") {
			return line
		}
	}
	return ""
}

// --- Speichern und Wechsel ---------------------------------------------------------

// update lädt die Zeile (oder beginnt eine neue, in Ordnung), wendet mutate an
// und speichert, wenn mutate true liefert. Wechselt die Zeile dabei in oder
// aus dem Alarmzustand, erfährt es OnTransition - nach dem Freigeben der
// Sperre, damit ein langsamer Abonnent (Mail) die übrigen Jobs nicht aufhält.
func (s *CapabilityService) update(serverID uint, category string, mutate func(c *domain.ServerCapability) bool) {
	c, wasAlertable, changed := s.persist(serverID, category, mutate)
	notify := s.transitionHook()
	if !changed || notify == nil {
		return
	}
	switch isAlertable := alertable(c); {
	case isAlertable && !wasAlertable:
		notify(serverID, *c, false)
	case wasAlertable && !isAlertable:
		notify(serverID, *c, true)
	}
}

func (s *CapabilityService) persist(serverID uint, category string, mutate func(c *domain.ServerCapability) bool) (c *domain.ServerCapability, wasAlertable, changed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.repo.Find(serverID, category)
	if err != nil {
		slog.Error("capability not loadable", "server", serverID, "category", category, "error", err)
		return nil, false, false
	}
	if c == nil {
		c = &domain.ServerCapability{ServerID: serverID, Category: category, State: domain.CapabilityOK}
	}
	wasAlertable = alertable(c)
	if !mutate(c) {
		return c, wasAlertable, false
	}
	if err := s.repo.Save(c); err != nil {
		slog.Error("capability not saveable", "server", serverID, "category", category, "error", err)
		return c, wasAlertable, false
	}
	return c, wasAlertable, true
}

// alertable: Ab wann ein Zustand einen Alarm wert ist. Eine belegte Sperre
// zählt erst ab dem zweiten Fehlschlag in Folge (Effective liefert bis dahin
// nur gelb) - Sperren durch unattended-upgrades sind oft nach Minuten vorbei.
//
// Fehlt der CVE-Scanner, ist das ein Problem des LCM-Hosts, nicht des Servers:
// Der Zustand bleibt je Server festgehalten (gelb in der Ampel), einen Alarm
// je Server löst er nicht aus - das meldet der cve_db_stale-Selbstalarm einmal.
func alertable(c *domain.ServerCapability) bool {
	if c.LastErrorClass == domain.ErrClassScannerUnavailable {
		return false
	}
	switch c.Effective() {
	case domain.CapabilityCritical:
		return true
	case domain.CapabilityWarning:
		return c.LastErrorClass != domain.ErrClassLockBusy
	}
	return false
}
