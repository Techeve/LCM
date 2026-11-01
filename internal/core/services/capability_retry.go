package services

import (
	"errors"
	"slices"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// Aktionen zur Betriebsfähigkeit: Paket-Reparatur und „Erneut versuchen".

var (
	// ErrUnknownRepairAction: Die Reparatur-Aktion gibt es nicht.
	ErrUnknownRepairAction = errors.New("unbekannte reparatur-aktion (erlaubt: dpkg_configure, fix_deps)")
	// ErrRepairUnsupported: Die Aktion gibt es, aber nicht für die
	// Paketverwaltung dieses Servers.
	ErrRepairUnsupported = errors.New("diese reparatur wird für die paketverwaltung dieses servers nicht unterstützt")
	// ErrUnknownCapability: Die Kategorie gibt es nicht.
	ErrUnknownCapability = errors.New("unbekannte kategorie der betriebsfähigkeit")
	// ErrCapabilityNotFailed: Es gibt nichts zu wiederholen.
	ErrCapabilityNotFailed = errors.New("diese kategorie ist nicht fehlgeschlagen - es gibt nichts erneut zu versuchen")
	// ErrNotRetryable: Die fehlgeschlagene Aktion lässt sich nicht erneut
	// auslösen (etwa ein Skript, dessen Regel gelöscht wurde, oder eine
	// Einzelpaket-Aktion, deren Paketliste nicht festgehalten wird).
	ErrNotRetryable = errors.New("die fehlgeschlagene aktion lässt sich nicht erneut auslösen")
	// ErrRetryNotWired: Der Executor ist nicht verdrahtet (schlanke Tests).
	ErrRetryNotWired = errors.New("erneutes ausführen ist in dieser instanz nicht verfügbar")
)

// CapabilityRetryRunner startet die Einzelläufe, die „Erneut versuchen" braucht.
// Der Executor erfüllt ihn; als Schnittstelle, weil er den ServerService nicht
// kennen soll und umgekehrt (dasselbe Muster wie dockerCheckTrigger).
type CapabilityRetryRunner interface {
	RunRuleOnServer(rule *domain.Rule, server *domain.Server, actor string) (*domain.Job, error)
	CheckConnection(server *domain.Server, actor string) (*domain.Job, error)
	StartCVEScanServer(server *domain.Server, actor string) (*domain.Job, error)
}

// WithRetryRunner verdrahtet die Einzelläufe für CapabilityActions und
// RetryCapability. Ohne sie ist nichts wiederholbar.
func (s *ServerService) WithRetryRunner(r CapabilityRetryRunner) *ServerService {
	s.retry = r
	return s
}

// --- Reparatur -------------------------------------------------------------------

// RepairPackages repariert die Paketverwaltung: angebrochene dpkg-Installationen
// abschließen oder kaputte Abhängigkeiten nachziehen. Läuft als normaler
// Paket-Job (Sperre, Protokoll, Audit, Neu-Erfassung danach).
//
// Die fehlgeschlagene Aktion wird danach NICHT wiederholt, und die Reparatur
// heilt keine Kategorie: Erst „Erneut versuchen" beweist, dass sie wieder läuft.
func (s *ServerService) RepairPackages(scope repositories.AccessScope, id uint, action, actor string) (*domain.Job, error) {
	jobType, name, ok := repairJob(action)
	if !ok {
		return nil, ErrUnknownRepairAction
	}
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	if err := ensureNotRouterOS(server); err != nil {
		return nil, err
	}
	script := pkgRepairScript(server.PackageManager, action)
	if script == "" {
		return nil, ErrRepairUnsupported
	}
	return s.startPackageJob(scope, id, jobType, name, func(string) string { return script }, actor)
}

// repairJob liefert Job-Typ und Anzeigename einer Reparatur-Aktion.
func repairJob(action string) (jobType, name string, ok bool) {
	switch action {
	case RepairDpkgConfigure:
		return JobTypeRepairDpkg, "Paketverwaltung reparieren (dpkg --configure -a)", true
	case RepairFixDeps:
		return JobTypeRepairDeps, "Paketverwaltung reparieren (Abhängigkeiten)", true
	}
	return "", "", false
}

// repairActionFor sagt, welche Reparatur zum Fehler einer Kategorie passt. ""
// heißt: keine - weil die Klasse keine hat oder die Paketverwaltung des Servers
// den Befehl nicht kennt.
func repairActionFor(mgr string, c *domain.ServerCapability) string {
	if c.Category != domain.CapabilityPackages || c.State != domain.CapabilityFailed {
		return ""
	}
	action := ""
	switch c.LastErrorClass {
	case domain.ErrClassDpkgInterrupted:
		action = RepairDpkgConfigure
	case domain.ErrClassBrokenDeps:
		action = RepairFixDeps
	}
	if pkgRepairScript(mgr, action) == "" {
		return ""
	}
	return action
}

// --- Erneut versuchen ------------------------------------------------------------

// CapabilityActions sind die Aktionen, die der Server zu einer Kategorie anbietet.
type CapabilityActions struct {
	Repair    string // RepairDpkgConfigure, RepairFixDeps oder ""
	Retryable bool
}

// CapabilityActions ermittelt die Aktionen zu einer Kategorie des Servers.
func (s *ServerService) CapabilityActions(server *domain.Server, c *domain.ServerCapability) CapabilityActions {
	// Nur fehlgeschlagene Kategorien bieten etwas an - für die übrigen wäre
	// der Job-Zugriff in retryPlanFor umsonst.
	if c.State != domain.CapabilityFailed {
		return CapabilityActions{}
	}
	_, err := s.retryPlanFor(c)
	return CapabilityActions{
		Repair:    repairActionFor(server.PackageManager, c),
		Retryable: err == nil,
	}
}

// retryPlan beschreibt, was „Erneut versuchen" auslöst.
type retryPlan struct {
	rule    *domain.Rule // die Regel, die den Fehlschlag verursachte - nur für diesen Server
	jobType string       // sonst: Typ der Ad-hoc-Aktion
}

// retryPlanFor bestimmt, wie die fehlgeschlagene Aktion der Kategorie erneut
// auszulösen ist.
//
// Die Verbindung wird immer geprüft, nie die letzte Aktion wiederholt: Ein
// Update, das an der Verbindung scheiterte, ist der Anlass, nicht die Aufgabe.
func (s *ServerService) retryPlanFor(c *domain.ServerCapability) (retryPlan, error) {
	if s.retry == nil {
		return retryPlan{}, ErrRetryNotWired
	}
	if c.Category == domain.CapabilityConnection {
		return retryPlan{jobType: domain.RuleTypeHealth}, nil
	}
	if rule := s.ruleOfFailedJob(c); rule != nil {
		return retryPlan{rule: rule}, nil
	}
	switch c.LastJobType {
	case domain.RuleTypeUpdate, domain.RuleTypeSecurity, domain.RuleTypePackageScan,
		domain.RuleTypeAutoremove, domain.RuleTypeCVEScan, domain.RuleTypeHealth:
		return retryPlan{jobType: c.LastJobType}, nil
	}
	return retryPlan{}, ErrNotRetryable
}

// ruleOfFailedJob lädt die Regel, aus der der fehlgeschlagene Job stammt. nil:
// Er kam nicht aus einer Regel, ist nicht mehr da oder die Regel wurde gelöscht.
func (s *ServerService) ruleOfFailedJob(c *domain.ServerCapability) *domain.Rule {
	if c.LastJobID == "" || s.groups == nil {
		return nil
	}
	job, err := s.jobs.Status(c.LastJobID)
	// Der Job muss zum Server der Zeile gehören: Die Regel läuft mit dessen
	// Rechten auf genau diesem Server.
	if err != nil || job.RuleID == nil || job.ServerID == nil || *job.ServerID != c.ServerID {
		return nil
	}
	rule, err := s.groups.FindRule(repositories.ScopeAll(), *job.RuleID)
	if err != nil {
		return nil
	}
	return rule
}

// RetryCapability löst die fehlgeschlagene Aktion einer Kategorie erneut aus -
// dieselbe Regel nur für diesen Server oder die entsprechende Ad-hoc-Aktion.
// Erst ihr Erfolg setzt die Kategorie zurück.
func (s *ServerService) RetryCapability(scope repositories.AccessScope, id uint, category, actor string) (*domain.Job, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(domain.CapabilityCategories, category) {
		return nil, ErrUnknownCapability
	}
	c, err := s.failedCapability(id, category)
	if err != nil {
		return nil, err
	}
	plan, err := s.retryPlanFor(c)
	if err != nil {
		return nil, err
	}
	job, err := s.startRetry(scope, server, plan, actor)
	if err != nil {
		return nil, err
	}
	s.audit.Log(actor, "server.capability-retry", "server", id, category)
	return job, nil
}

// failedCapability lädt die Zeile der Kategorie und verlangt, dass sie fehlgeschlagen ist.
func (s *ServerService) failedCapability(serverID uint, category string) (*domain.ServerCapability, error) {
	if s.capabilities == nil {
		return nil, ErrCapabilityNotFailed
	}
	c, err := s.capabilities.Find(serverID, category)
	if err != nil {
		return nil, err
	}
	if c == nil || c.State != domain.CapabilityFailed {
		return nil, ErrCapabilityNotFailed
	}
	return c, nil
}

// startRetry führt den Plan aus. Ist der Server belegt, kommt ErrServerBusy.
func (s *ServerService) startRetry(scope repositories.AccessScope, server *domain.Server, plan retryPlan, actor string) (*domain.Job, error) {
	if plan.rule != nil {
		return s.retry.RunRuleOnServer(plan.rule, server, actor)
	}
	switch plan.jobType {
	case domain.RuleTypeUpdate:
		return s.UpgradeAllPackages(scope, server.ID, actor)
	case domain.RuleTypeSecurity:
		return s.UpgradeSecurityPackages(scope, server.ID, actor)
	case domain.RuleTypePackageScan:
		return s.RefreshPackages(scope, server.ID, actor)
	case domain.RuleTypeAutoremove:
		return s.AutoremovePackages(scope, server.ID, actor)
	case domain.RuleTypeCVEScan:
		return s.retry.StartCVEScanServer(server, actor)
	case domain.RuleTypeHealth:
		return s.retry.CheckConnection(server, actor)
	}
	return nil, ErrNotRetryable
}
