package services

import (
	"LCM/internal/core/domain"
	"LCM/internal/safego"
)

// Einzelläufe für „Erneut versuchen" (Betriebsfähigkeit). Anders als RunRule,
// das eine Regel auf alle Server der Gruppe loslässt und erst nach dem letzten
// zurückkehrt, legen diese Wege den Job sofort an und geben ihn zurück - die
// Oberfläche verfolgt ihn - und führen den Rest im Hintergrund aus.

// connectionCheckName benennt den Verbindungs-Check, den „Erneut versuchen" der
// Kategorie connection auslöst.
const connectionCheckName = "Verbindung prüfen"

// ruleIDOf liefert die Regel-ID für den Job. Eine nicht gespeicherte Regel
// (ID 0, etwa der Verbindungs-Check) verknüpft den Job mit keiner Regel.
func ruleIDOf(rule *domain.Rule) *uint {
	if rule.ID == 0 {
		return nil
	}
	id := rule.ID
	return &id
}

// RunRuleOnServer führt die Regel für genau diesen Server aus - sofort, ohne
// Warteschlange: Wer „Erneut versuchen" drückt, will eine Antwort. Ist der
// Server belegt, kommt ErrServerBusy zurück.
func (e *Executor) RunRuleOnServer(rule *domain.Rule, server *domain.Server, actor string) (*domain.Job, error) {
	job, err := e.startOnServer(server, rule, actor, runPlan{})
	if err != nil {
		return nil, err
	}
	safego.GoCleanup("job:retry", jobPanicCleanup(e.jobs, job), func() {
		e.runStarted(job, server, rule, actor, nil)
	})
	return job, nil
}

// CheckConnection prüft die Verbindung zum Server wie der Health-Ping, auch
// mit dem Abgleich der Grundsatz-Regeln - ohne dass dafür eine Health-Regel
// existieren muss.
func (e *Executor) CheckConnection(server *domain.Server, actor string) (*domain.Job, error) {
	rule := &domain.Rule{Name: connectionCheckName, Type: domain.RuleTypeHealth}
	return e.RunRuleOnServer(rule, server, actor)
}

// StartCVEScanServer startet den CVE-Scan eines einzelnen Servers im
// Hintergrund und gibt den Job zurück.
func (e *Executor) StartCVEScanServer(server *domain.Server, actor string) (*domain.Job, error) {
	job, err := e.jobs.Start(&server.ID, nil, domain.RuleTypeCVEScan, "CVE-Scan @ "+server.Name, actor)
	if err != nil {
		return nil, err
	}
	safego.GoCleanup("job:cve-scan", jobPanicCleanup(e.jobs, job), func() {
		e.runCVEScanJob(job, server)
	})
	return job, nil
}
