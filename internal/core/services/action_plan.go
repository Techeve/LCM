package services

import (
	"fmt"
	"strings"

	"LCM/internal/core/domain"
	"LCM/internal/safego"
	"LCM/internal/storage/repositories"
)

// Pläne der Paket-Aktionen.
//
// Jede Aktion, die auf dem Server ein Skript ausführt, baut es in einem
// planX-Schritt. Lauf und Befehlsvorschau (action_preview.go) rufen denselben
// Schritt auf: Die Vorschau kann deshalb nichts anderes zeigen, als der Lauf
// tatsächlich absetzt. Alle Prüfungen der Aktion (Schutzlisten, Pins,
// Unterstützung der Paketverwaltung) sitzen im Plan und gelten damit für
// beide.

// packagePlan beschreibt, was eine Paket-Aktion auf dem Server ausführt.
type packagePlan struct {
	jobType string
	name    string // Anzeigename des Jobs (ohne " @ Server")
	script  string // unverpackt; der Lauf übergibt privRun(server, script)
}

// newPackagePlan baut das Skript für die erkannte Paketverwaltung des Servers.
// RouterOS hat keine (Linux-)Paketverwaltung - alle paketbasierten Aktionen
// (Updates, Tool-Installationen) sind dort nicht anwendbar. Zentraler
// Sperrpunkt, bevor ein sinnloser Job startet.
func (s *ServerService) newPackagePlan(server *domain.Server, jobType, name string, build func(mgr string) string) (packagePlan, error) {
	if err := ensureNotRouterOS(server); err != nil {
		return packagePlan{}, err
	}
	script := build(server.PackageManager)
	if jobType == domain.RuleTypeUpdate {
		script = aptUpgradeEnv(s.settings, s.servers, server) + script
	}
	return packagePlan{jobType: jobType, name: name, script: script}, nil
}

// runPlanned prüft den Zugriff, plant die Aktion und startet sie als Job.
func (s *ServerService) runPlanned(scope repositories.AccessScope, id uint, actor string, plan func(*domain.Server) (packagePlan, error)) (*domain.Job, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	p, err := plan(server)
	if err != nil {
		return nil, err
	}
	return s.startPlan(server, p, actor)
}

// startPlan legt den Job an (Concurrency-Lock pro Server) und führt den Plan
// asynchron aus.
func (s *ServerService) startPlan(server *domain.Server, plan packagePlan, actor string) (*domain.Job, error) {
	job, err := s.jobs.Start(&server.ID, nil, plan.jobType, plan.name+" @ "+server.Name, actor)
	if err != nil {
		return nil, err // u.a. ErrServerBusy → der Controller mappt auf 409
	}
	s.audit.Log(actor, "server.package-update", "server", server.ID, plan.name)
	safego.GoCleanup("job:package", jobPanicCleanup(s.jobs, job), func() {
		s.runPackageJob(job, server, plan.script, actor)
	})
	return job, nil
}

func (s *ServerService) planUpgradeAll(server *domain.Server) (packagePlan, error) {
	return s.newPackagePlan(server, domain.RuleTypeUpdate,
		"Alle Pakete aktualisieren", pkgUpgradeAllScript)
}

func (s *ServerService) planRefresh(server *domain.Server) (packagePlan, error) {
	return s.newPackagePlan(server, domain.RuleTypePackageScan,
		"Paketliste aktualisieren", pkgRefreshScript)
}

func (s *ServerService) planUpgradeSecurity(server *domain.Server) (packagePlan, error) {
	return s.newPackagePlan(server, domain.RuleTypeSecurity,
		"Security-Updates einspielen", pkgSecurityUpgradeScript)
}

// planAutoremove: Die gesetzten Paket-Pins werden dabei durchgesetzt. Bei apt
// und dnf schreibt der Lauf zuerst die Schutzdatei (die Paketverwaltung
// respektiert sie dann selbst), bei zypper und pacman - die keine solche Datei
// kennen - wird die Kandidatenliste um die geschützten Pakete gekürzt. Ohne das
// räumte der Lauf weiterhin ältere Kernel weg.
func (s *ServerService) planAutoremove(server *domain.Server) (packagePlan, error) {
	pins := s.effectivePins(server)
	build := func(mgr string) string {
		if len(pins) == 0 {
			return pkgAutoremoveScript(mgr)
		}
		// Erst schützen, dann aufräumen - in dieser Reihenfolge, sonst ist die
		// Schutzdatei beim Lauf noch nicht da.
		return pkgPinScript(mgr, pins) + "\n" + pkgAutoremoveScriptWithPins(mgr, pins)
	}
	return s.newPackagePlan(server, domain.RuleTypeAutoremove,
		"Nicht mehr benötigte Pakete entfernen", build)
}

// planRemove: Geschützte Systempakete werden abgelehnt, ebenso Pakete, auf
// denen ein Pin „nicht entfernen" liegt - sonst schützte er nur gegen den
// Aufräum-Lauf und der Klick daneben entfernte das Paket doch.
func (s *ServerService) planRemove(server *domain.Server, names []string) (packagePlan, error) {
	clean, err := parsePackageNames(strings.Join(names, " "))
	if err != nil {
		return packagePlan{}, err
	}
	for _, n := range clean {
		if isProtectedPackage(n) {
			return packagePlan{}, fmt.Errorf("%w: %s", ErrProtectedPackage, n)
		}
	}
	for _, pin := range s.effectivePins(server) {
		if !pin.NoRemove {
			continue
		}
		for _, n := range clean {
			if pin.Matches(n) {
				return packagePlan{}, fmt.Errorf("%w: %s (Pin: %s)", ErrPinnedPackage, n, pin.Name)
			}
		}
	}
	build := func(mgr string) string { return pkgRemovePackagesScript(mgr, clean) }
	return s.newPackagePlan(server, domain.RuleTypeAutoremove,
		"Pakete entfernen: "+strings.Join(clean, ", "), build)
}

// planUpdate: Ist version gesetzt, muss names genau ein Paket enthalten.
func (s *ServerService) planUpdate(server *domain.Server, names []string, version string) (packagePlan, error) {
	if version == "" {
		clean, err := parsePackageNames(strings.Join(names, " "))
		if err != nil {
			return packagePlan{}, err
		}
		build := func(mgr string) string { return pkgUpgradePackagesScript(mgr, clean) }
		return s.newPackagePlan(server, domain.RuleTypePackages,
			"Pakete aktualisieren: "+strings.Join(clean, ", "), build)
	}
	if len(names) != 1 {
		return packagePlan{}, ErrVersionOnePkg
	}
	if !rePackageName.MatchString(names[0]) {
		return packagePlan{}, ErrInvalidPackage
	}
	if !rePackageVersion.MatchString(version) {
		return packagePlan{}, ErrInvalidVersion
	}
	build := func(mgr string) string { return pkgInstallVersionScript(mgr, names[0], version) }
	return s.newPackagePlan(server, domain.RuleTypePackages,
		fmt.Sprintf("Paket %s → %s", names[0], version), build)
}

// requireSnapd weist Snap-Aktionen auf Servern ohne snapd ab, statt ein
// Kommando abzusetzen, das mit „command not found" endet.
func requireSnapd(server *domain.Server) error {
	if !server.HasSnap {
		return ErrNoSnapd
	}
	return nil
}

func (s *ServerService) planSnapRefreshAll(server *domain.Server) (packagePlan, error) {
	if err := requireSnapd(server); err != nil {
		return packagePlan{}, err
	}
	return s.newPackagePlan(server, domain.RuleTypeUpdate, "Alle Snaps aktualisieren",
		func(string) string { return snapRefreshAllScript() })
}

func (s *ServerService) planSnapRefresh(server *domain.Server, names []string) (packagePlan, error) {
	clean, err := parseSnapNames(strings.Join(names, " "))
	if err != nil {
		return packagePlan{}, err
	}
	if err := requireSnapd(server); err != nil {
		return packagePlan{}, err
	}
	return s.newPackagePlan(server, domain.RuleTypePackages,
		"Snap aktualisieren: "+strings.Join(clean, ", "),
		func(string) string { return snapRefreshScript(clean) })
}

func (s *ServerService) planSnapRemove(server *domain.Server, names []string) (packagePlan, error) {
	clean, err := parseSnapNames(strings.Join(names, " "))
	if err != nil {
		return packagePlan{}, err
	}
	for _, n := range clean {
		if isProtectedSnap(n) {
			return packagePlan{}, fmt.Errorf("%w: %s", ErrProtectedSnap, n)
		}
	}
	if err := requireSnapd(server); err != nil {
		return packagePlan{}, err
	}
	return s.newPackagePlan(server, domain.RuleTypePackages,
		"Snap entfernen: "+strings.Join(clean, ", "),
		func(string) string { return snapRemoveScript(clean) })
}

// ruleScript liefert das Skript einer paketbezogenen Regel samt der Umgebung
// des Voll-Updates - der Lauf der Regel (Executor) und die Vorschau lesen es
// hier. Der zweite Rückgabewert ist false für nicht-paketbezogene Typen.
func ruleScript(settings *repositories.SettingsRepository, servers *repositories.ServerRepository, server *domain.Server, rule *domain.Rule) (string, bool) {
	script, ok := scriptForRule(server.PackageManager, rule.Type, rule.Command)
	if ok && rule.Type == domain.RuleTypeUpdate {
		script = aptUpgradeEnv(settings, servers, server) + script
	}
	return script, ok
}
