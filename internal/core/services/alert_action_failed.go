package services

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/safego"
	"LCM/internal/storage/repositories"
)

// Alarm action_failed: LCM kann auf einem Server nicht mehr handeln.
//
// Zwei Wege führen zum Ereignis:
//   - Sofort beim Wechsel (OnCapabilityTransition): ok → fehlgeschlagen löst
//     aus, ohne die halbstündige Auswertung abzuwarten, und die Rückkehr wird
//     als Entwarnung gemeldet.
//   - Erinnerung und Stillstand im 30-Minuten-Lauf (evaluateActionFailed):
//     besteht der Fehler weiter, kommt nach der Frist der Regel (Cooldown) eine
//     Erinnerung; bleiben die Paketläufe lautlos aus, meldet es der Stillstand.
//
// Entprellt wird je Server UND Kategorie über den Ereignis-Code
// „action_failed:<kategorie>": Scheitern Updates und Regeln zugleich, sind das
// zwei Meldungen, und die Erinnerung der einen sperrt die andere nicht.

const (
	actionFailedCodePrefix = "action_failed:"
	// actionFailedStaleCode meldet den Stillstand der Paketläufe (keine
	// Kategorie der Betriebsfähigkeit, sondern eine abgeleitete Aussage).
	actionFailedStaleCode = actionFailedCodePrefix + "packages_stale"
	// recoveredCodeSuffix trennt die Entwarnung vom Fehler-Code: Sonst sperrte
	// sie als jüngstes Ereignis die Erinnerung, falls die Kategorie kurz darauf
	// wieder scheitert.
	recoveredCodeSuffix = ":recovered"
)

func actionFailedCode(category string) string { return actionFailedCodePrefix + category }

// WithCapabilities verdrahtet den Speicher der Betriebsfähigkeit (optional) -
// Grundlage von Erinnerung und Stillstand des action_failed-Alarms.
func (s *AlertService) WithCapabilities(repo *repositories.CapabilityRepository) *AlertService {
	s.capabilities = repo
	return s
}

// WithSettings verdrahtet die globalen Einstellungen (optional) - daraus kommt
// die Frist, ab der ein Paketstand als Stillstand gilt.
func (s *AlertService) WithSettings(get func() (*domain.GlobalSettings, error)) *AlertService {
	s.settings = get
	return s
}

// staleLimits liest die Fristen des Stillstands; bei Fehler gelten die Vorgaben
// (ein nicht lesbarer Einstellungssatz darf den Alarm nicht abschalten).
func (s *AlertService) staleLimits() staleLimits {
	gs := &domain.GlobalSettings{}
	if s.settings != nil {
		loaded, err := s.settings()
		if err != nil {
			slog.Warn("action_failed: settings not loadable, using defaults", "error", err)
		} else {
			gs = loaded
		}
	}
	return staleLimitsOf(gs)
}

// --- Sofort beim Wechsel ----------------------------------------------------

// OnCapabilityTransition ist der Abonnent von CapabilityService.OnTransition:
// eine Kategorie ist alarmwürdig geworden (recovered=false) oder wieder in
// Ordnung (recovered=true).
//
// Die Arbeit läuft nicht im Aufrufer: Der Aufruf kommt aus dem Ende eines
// Jobs, und das darf nie auf Datenbank oder Versand warten. Sie läuft aber
// der Reihe nach (transitionQueue): Eine Goroutine je Wechsel ließe bei
// schnellem Fehlschlag und Heilung die Entwarnung vor dem Alarm ankommen.
func (s *AlertService) OnCapabilityTransition(serverID uint, c domain.ServerCapability, recovered bool) {
	s.notifyWG.Add(1)
	s.transitions.enqueue(func() {
		defer s.notifyWG.Done()
		s.alertTransition(serverID, c, recovered)
	})
}

// transitionQueue arbeitet Aufgaben in der Reihenfolge des Eintreffens ab, auf
// höchstens einer Goroutine, die nur läuft, solange etwas ansteht. Unbegrenzt
// statt Kanal mit fester Größe: Ein voller Puffer ließe entweder das Jobende
// warten oder einen Wechsel verlieren.
type transitionQueue struct {
	mu      sync.Mutex
	pending []func()
	running bool
}

func (q *transitionQueue) enqueue(fn func()) {
	q.mu.Lock()
	q.pending = append(q.pending, fn)
	start := !q.running
	q.running = true
	q.mu.Unlock()
	if start {
		safego.Go("alert:capability-transitions", q.drain)
	}
}

func (q *transitionQueue) drain() {
	for {
		fn := q.next()
		if fn == nil {
			return
		}
		q.run(fn)
	}
}

// next liefert die älteste Aufgabe; nil und running=false, wenn keine ansteht.
func (q *transitionQueue) next() func() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.pending) == 0 {
		q.running = false
		return nil
	}
	fn := q.pending[0]
	q.pending[0] = nil
	q.pending = q.pending[1:]
	return fn
}

// run schützt die Schleife: Ein Panic in einem Wechsel darf die folgenden
// nicht liegen lassen.
func (q *transitionQueue) run(fn func()) {
	defer safego.Recover("alert:capability-transition", nil)
	fn()
}

func (s *AlertService) alertTransition(serverID uint, c domain.ServerCapability, recovered bool) {
	rules, err := s.rules.FindEnabledByType(domain.AlertTypeActionFailed)
	if err != nil {
		slog.Warn("action_failed: rules not loadable", "error", err)
		return
	}
	if len(rules) == 0 {
		return
	}
	// Den Server einmal laden und die Gruppenzugehörigkeit daran prüfen, statt
	// je Regel alle Mitglieder aller Gruppen zu holen.
	server, err := s.servers.FindByID(repositories.ScopeAll(), serverID)
	if err != nil {
		slog.Warn("action_failed: server not loadable", "server", serverID, "error", err)
		return
	}
	// Demo-Server haben erfundene Daten, ein Server in Wartung ist absichtlich
	// aus - beides wäre ein Fehlalarm (wie in Evaluate).
	if server.IsDemo || server.InMaintenance() {
		return
	}
	f := transitionFinding(server, c, recovered, s.now())
	for i := range rules {
		if ruleTargets(&rules[i], server) {
			// Ohne Sperrfrist: Der Wechsel selbst ist die Nachricht. Die
			// Sperrfrist gilt erst für die Erinnerungen.
			s.recordAndNotify(&rules[i], server, f)
		}
	}
}

// ruleTargets meldet, ob die Regel für den Server gilt: ohne Gruppen für alle,
// sonst bei Mitgliedschaft in mindestens einer gewählten Gruppe. server muss
// seine Gruppen geladen haben.
func ruleTargets(rule *domain.AlertRule, server *domain.Server) bool {
	if len(rule.Groups) == 0 {
		return true
	}
	for i := range rule.Groups {
		for j := range server.Groups {
			if rule.Groups[i].ID == server.Groups[j].ID {
				return true
			}
		}
	}
	return false
}

func transitionFinding(server *domain.Server, c domain.ServerCapability, recovered bool, now time.Time) finding {
	if recovered {
		return recoveredFinding(server, c)
	}
	return failedFinding(server, c, now)
}

// --- Erinnerung und Stillstand im 30-Minuten-Lauf ---------------------------

// evaluateActionFailed wertet eine action_failed-Regel für ihre Zielserver aus
// und liefert die Zahl geprüfter Server. Eigener Weg statt evaluateServer, weil
// ein Server hier mehrere Befunde haben kann (je Kategorie einen), und weil die
// Betriebsfähigkeit aller Server mit EINER Abfrage geladen wird.
func (s *AlertService) evaluateActionFailed(rule *domain.AlertRule, servers []domain.Server) (int, tally) {
	if s.capabilities == nil {
		return 0, tally{}
	}
	targets := make([]domain.Server, 0, len(servers))
	ids := make([]uint, 0, len(servers))
	for _, server := range servers {
		if !server.IsDemo {
			targets = append(targets, server)
			ids = append(ids, server.ID)
		}
	}
	caps, err := s.capabilities.FindForServers(ids)
	if err != nil {
		slog.Warn("action_failed: capabilities not loadable", "rule", rule.Name, "error", err)
		return 0, tally{}
	}
	limits, now := s.staleLimits(), s.now()
	var out tally
	for i := range targets {
		server := &targets[i]
		for _, f := range actionFailedFindings(server, caps[server.ID], limits, now) {
			out.add(s.fire(rule, server, f))
		}
	}
	return len(targets), out
}

// actionFailedFindings liefert die Befunde eines Servers: je alarmwürdiger
// Kategorie einen, dazu den Stillstand der Paketläufe.
func actionFailedFindings(server *domain.Server, rows []domain.ServerCapability, limits staleLimits, now time.Time) []finding {
	var out []finding
	for i := range rows {
		if alertable(&rows[i]) {
			out = append(out, failedFinding(server, rows[i], now))
		}
	}
	// Scheitern die Paketläufe ohnehin gemeldet, wäre „Stillstand" dieselbe
	// Ursache ein zweites Mal. Der Stillstand-Befund gehört dem stillen Fall:
	// Läufe finden gar nicht mehr statt, und keine Kategorie steht auf failed.
	if pkg := capabilityOf(rows, domain.CapabilityPackages); pkg != nil && alertable(pkg) {
		return out
	}
	if f, ok := staleFinding(server, rows, limits, now); ok {
		out = append(out, f)
	}
	return out
}

// staleFinding meldet den Stillstand nur auf der roten Stufe. Die gelbe
// („Paketstand veraltet" ab 48 h) bleibt ein reiner Hinweis in der Ampel: Sie
// tritt schon bei einem verpassten Wochenende auf, und ein Alarm, der
// regelmäßig grundlos kommt, macht die echten unglaubwürdig. Die Einstufung
// selbst kommt aus packagesStaleness, damit Ampel und Alarm nie auseinanderlaufen.
func staleFinding(server *domain.Server, rows []domain.ServerCapability, limits staleLimits, now time.Time) (finding, bool) {
	level, since := packagesStaleness(server, rows, limits, now)
	if level != domain.PackagesStaleCritical {
		return finding{}, false
	}
	return finding{
		fired:       true,
		code:        actionFailedStaleCode,
		description: fmt.Sprintf("%s (%s)", domain.PackagesStaleAlertText(since, now), server.Name),
		details: strings.Join([]string{
			"Kategorie: packages (Stillstand)",
			"Letzter erfolgreicher Paketlauf: " + since.Format("2006-01-02 15:04"),
			"Es gab keinen Fehlschlag, der das erklärt: Zeitplan, Sperre durch einen anderen Job oder ein hängender Lauf prüfen.",
		}, "\n"),
	}, true
}

// --- Inhalt der Ereignisse ---------------------------------------------------

// failedFinding und recoveredFinding bauen Beschreibung und Details aus
// Kategorie, Fehlerklasse, Hinweis, Zähler und Beginn - NIE aus dem Rohtext des
// Werkzeugs (ServerCapability.LastErrorMessage): Alarme laufen über Mail und
// Chat-Kanäle, die Ausgabe kann Pfade, Hostnamen und Paketquellen enthalten.
func failedFinding(server *domain.Server, c domain.ServerCapability, now time.Time) finding {
	lines := []string{"Kategorie: " + c.Category}
	if c.LastErrorClass != "" {
		lines = append(lines, "Fehlerklasse: "+c.LastErrorClass)
	}
	if c.ConsecutiveFailures > 0 {
		lines = append(lines, fmt.Sprintf("Fehlschläge in Folge: %d", c.ConsecutiveFailures))
	}
	if c.FailingSince != nil {
		lines = append(lines, "Seit: "+c.FailingSince.Format("2006-01-02 15:04"))
	}
	if c.LastJobName != "" {
		lines = append(lines, "Job: "+c.LastJobName)
	}
	lines = append(lines, "Die Ausgabe des Jobs steht in LCM am Server unter Jobs.")
	return finding{
		fired:       true,
		code:        actionFailedCode(c.Category),
		description: fmt.Sprintf("%s (%s)", domain.ActionFailedAlertText(c, now), server.Name),
		details:     strings.Join(lines, "\n"),
	}
}

func recoveredFinding(server *domain.Server, c domain.ServerCapability) finding {
	return finding{
		fired:       true,
		code:        actionFailedCode(c.Category) + recoveredCodeSuffix,
		severity:    domain.AlertSeverityInfo,
		description: fmt.Sprintf("Entwarnung: %s (%s)", domain.ActionRecoveredText(c.Category), server.Name),
		details:     "Kategorie: " + c.Category + "\nDer Fehlerzustand ist behoben.",
	}
}
