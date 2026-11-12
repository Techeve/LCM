package services

import (
	"fmt"
	"log/slog"
	"sort"

	"LCM/internal/core/domain"
)

// Vorrang-Auflösung der Grundsatz-Regeln.
//
// Ein Server darf in beliebig vielen Servergruppen sein. Tragen zwei davon
// eine Grundsatz-Regel desselben Typs, beschreiben sie denselben Soll-Zustand
// unterschiedlich - und vorher wurden beide nacheinander durchgesetzt: bei
// jedem Health-Ping, mit je einem Eingriffs-Eintrag, die zuletzt ausgeführte
// gewann. Die Ports pendelten, und im Protokoll stand zweimal „neu
// angewendet", ohne dass der Konflikt erkennbar war.
//
// Deshalb gilt jetzt: Pro Typ mit widersprüchlichem Soll-Zustand greift NUR
// die Regel der Gruppe mit dem stärksten Vorrang. Die übrigen werden nicht
// ausgeführt, aber ausdrücklich im Job-Bericht benannt - aufgelöst, nicht
// stillschweigend verschluckt.
//
// Dasselbe gilt über geplante Regeln hinweg. Eine Firewall-Regel am Zeitplan
// von Gruppe A und eine von Gruppe B setzten sonst nachts nacheinander ihre
// Ports, und eine Grundsatz-Regel der einen Gruppe drehte die geplante der
// anderen beim nächsten Health-Ping zurück. Darum FÜHRT je Server und Typ
// genau eine Gruppe: die mit dem stärksten Vorrang unter allen Gruppen, die
// den Typ dort mit einer laufenden Regel regeln, gleich ob am Zeitplan oder
// als Grundsatz. Nur ihre Regeln greifen; die der anderen Gruppen werden
// benannt übersprungen. Das ist dieselbe Leserichtung wie die Auflösung der
// Patch-Richtlinie je Feld in Release C: Vorrang der Gruppe, kleinste Zahl
// gewinnt, bei Gleichstand die ältere Gruppe - und der Gleichstand wird
// gemeldet, statt still entschieden.

// conflictingEnforceType meldet, ob zwei Regeln dieses Typs einander
// widersprechen KÖNNEN und daher über den Vorrang entschieden werden müssen.
//
// firewall und apt-proxy tragen einen ausdrücklichen Soll-Zustand (Portliste
// bzw. Cache-URL), von dem es nur einen geben kann. Typen, deren Soll-Zustand
// ein bloßes „vorhanden/eingerichtet" ist, gehören NICHT hierher - mehrere
// Gruppen, die dasselbe fordern, sind dort kein Konflikt, sondern eine
// Dopplung, und die zweite Ausführung wäre schlicht wirkungslos.
func conflictingEnforceType(ruleType string) bool {
	switch ruleType {
	case domain.RuleTypeFirewall, domain.RuleTypeAptProxy:
		return true
	}
	return false
}

// supersededEnforce ist eine Grundsatz-Regel, die wegen des Vorrangs einer
// anderen Gruppe nicht durchgesetzt wird.
type supersededEnforce struct {
	Rule   domain.Rule // die zurückgestellte Regel
	Winner domain.Rule // die Regel, die stattdessen greift
	// Tie: Beide Gruppen haben denselben Vorrang; entschieden hat die
	// niedrigere Gruppen-ID. Deterministisch, aber ungewollt - der Betreiber
	// soll das sehen und den Vorrang ausdrücklich setzen.
	Tie bool
}

// enforceGroupPriority liefert den Vorrang der Gruppe einer Regel. Ist die
// Gruppe nicht mitgeladen, gilt der Standardwert - eine fehlende Zuordnung
// darf die Reihenfolge nicht zufällig machen.
func enforceGroupPriority(rule *domain.Rule) int {
	if rule.Group != nil && rule.Group.Priority > 0 {
		return rule.Group.Priority
	}
	return domain.DefaultGroupPriority
}

// enforceGroupLabel liefert den Gruppennamen für Meldungen.
func enforceGroupLabel(rule *domain.Rule) string {
	if rule.Group != nil && rule.Group.Name != "" {
		return rule.Group.Name
	}
	return fmt.Sprintf("#%d", rule.GroupID)
}

// resolveEnforceRules teilt die Grundsatz-Regeln eines Servers in die
// tatsächlich durchzusetzenden und die vom Vorrang zurückgestellten.
//
// Sortiert wird nach Vorrang, dann Gruppen-ID, dann Regel-ID - die Auswahl
// ist damit auch bei Gleichstand deterministisch und über Läufe hinweg
// stabil. Die Eingabe wird nicht verändert.
func resolveEnforceRules(rules []domain.Rule) ([]domain.Rule, []supersededEnforce) {
	ordered := make([]domain.Rule, len(rules))
	copy(ordered, rules)
	sort.SliceStable(ordered, func(i, j int) bool { return ruleBefore(&ordered[i], &ordered[j]) })

	active := make([]domain.Rule, 0, len(ordered))
	var superseded []supersededEnforce
	winners := make(map[string]domain.Rule, len(ordered))
	seenDeduped := make(map[string]bool, len(ordered))
	for _, rule := range ordered {
		if dedupedEnforceType(rule.Type) {
			if !seenDeduped[rule.Type] {
				seenDeduped[rule.Type] = true
				active = append(active, rule)
			}
			continue
		}
		if !conflictingEnforceType(rule.Type) {
			active = append(active, rule)
			continue
		}
		winner, taken := winners[rule.Type]
		if !taken {
			winners[rule.Type] = rule
			active = append(active, rule)
			continue
		}
		superseded = append(superseded, supersededEnforce{
			Rule:   rule,
			Winner: winner,
			Tie:    enforceGroupPriority(&rule) == enforceGroupPriority(&winner),
		})
	}
	return active, superseded
}

// ruleBefore ordnet zwei Regeln nach Vorrang: Gruppen-Vorrang, dann
// Gruppen-ID, dann Regel-ID. Deterministisch und über Läufe hinweg stabil.
func ruleBefore(a, b *domain.Rule) bool {
	pa, pb := enforceGroupPriority(a), enforceGroupPriority(b)
	if pa != pb {
		return pa < pb
	}
	if a.GroupID != b.GroupID {
		return a.GroupID < b.GroupID
	}
	return a.ID < b.ID
}

// conflictingRuleTypes sind die Typen, über die der Vorrang entscheidet
// (siehe conflictingEnforceType) - als Liste für die Datenbankabfrage.
var conflictingRuleTypes = []string{domain.RuleTypeFirewall, domain.RuleTypeAptProxy}

// ruleOwners bestimmt je Typ mit Soll-Zustand die Regel, deren Gruppe den Typ
// auf einem Server führt. rules sind alle dort laufenden Regeln, geplante wie
// Grundsatz-Regeln; andere Typen werden übergangen.
func ruleOwners(rules []domain.Rule) map[string]domain.Rule {
	owners := make(map[string]domain.Rule)
	for i := range rules {
		rule := &rules[i]
		if !conflictingEnforceType(rule.Type) {
			continue
		}
		if owner, taken := owners[rule.Type]; !taken || ruleBefore(rule, &owner) {
			owners[rule.Type] = *rule
		}
	}
	return owners
}

// yieldsTo meldet, ob eine Regel auf dem Server der führenden Gruppe ihres
// Typs weicht. Regeln derselben Gruppe weichen einander nicht: Was eine Gruppe
// am Zeitplan und als Grundsatz regelt, ist ihre eigene Entscheidung.
func yieldsTo(rule *domain.Rule, owners map[string]domain.Rule) (supersededEnforce, bool) {
	owner, ok := owners[rule.Type]
	if !ok || owner.GroupID == rule.GroupID {
		return supersededEnforce{}, false
	}
	return supersededEnforce{
		Rule:   *rule,
		Winner: owner,
		Tie:    enforceGroupPriority(rule) == enforceGroupPriority(&owner),
	}, true
}

// yieldEnforceToOwners nimmt die Grundsatz-Regeln heraus, deren Typ eine
// stärkere Gruppe mit einer geplanten Regel führt, und richtet die
// Begründung der schon zurückgestellten auf die führende Gruppe aus.
func yieldEnforceToOwners(active []domain.Rule, superseded []supersededEnforce, owners map[string]domain.Rule) ([]domain.Rule, []supersededEnforce) {
	kept := make([]domain.Rule, 0, len(active))
	out := make([]supersededEnforce, 0, len(superseded))
	for i := range active {
		if s, yields := yieldsTo(&active[i], owners); yields {
			out = append(out, s)
			continue
		}
		kept = append(kept, active[i])
	}
	for _, s := range superseded {
		if moved, yields := yieldsTo(&s.Rule, owners); yields {
			s = moved
		}
		out = append(out, s)
	}
	return kept, out
}

// ReportEnforceOverlaps meldet beim Start die Server, auf denen mehrere
// Grundsatz-Regeln desselben Typs zusammentreffen, OHNE dass der Vorrang die
// Entscheidung trägt - beide Gruppen haben denselben Wert, entschieden hat
// dann die Gruppen-ID.
//
// Genau das ist die Lage im Altbestand: Vor der Einführung des Vorrangs liefen
// beide Regeln nacheinander (die zuletzt ausgeführte gewann), nach dem Update
// gewinnt eine - das kann auf einem laufenden System Ports öffnen oder
// schließen. Der Betreiber soll das sehen, bevor er es merkt.
//
// Bewusst NUR bei Gleichstand: Sobald jemand die Vorränge auseinanderzieht,
// ist die Auswahl eine getroffene Entscheidung und keine Meldung mehr wert.
// Der Hinweis verstummt also von selbst, sobald er befolgt wurde. Welche Regel
// zurückgestellt wurde, steht ohnehin in jedem Health-Check-Bericht.
//
// Best effort: Fehler werden protokolliert, der Start hängt nicht daran.
func (s *GroupService) ReportEnforceOverlaps() {
	servers, err := s.servers.FindAllUnscoped()
	if err != nil {
		slog.Warn("enforce overlap check skipped", "error", err)
		return
	}
	for i := range servers {
		rules, err := s.groups.FindActiveRulesOfTypesForServer(servers[i].ID, conflictingRuleTypes)
		if err != nil {
			slog.Warn("enforce overlap check skipped", "server", servers[i].Name, "error", err)
			continue
		}
		for _, sup := range yieldedRules(rules) {
			if !sup.Tie {
				continue
			}
			slog.Warn("regeln gleichen typs aus mehreren gruppen treffen ohne unterscheidbaren vorrang zusammen",
				"server", servers[i].Name, "typ", sup.Rule.Type,
				"wirksam", enforceGroupLabel(&sup.Winner)+"/"+sup.Winner.Name,
				"zurueckgestellt", enforceGroupLabel(&sup.Rule)+"/"+sup.Rule.Name,
				"vorrang", enforceGroupPriority(&sup.Rule),
				"hinweis", "Vorrang der Gruppen ausdrücklich setzen (kleinere Zahl gewinnt)")
		}
	}
}

// yieldedRules liefert die Regeln, die auf einem Server einer anderen Gruppe
// weichen - je Gruppe und Typ eine Meldung, damit der Startbericht nicht für
// jede Regel derselben Gruppe dieselbe Zeile schreibt.
func yieldedRules(rules []domain.Rule) []supersededEnforce {
	owners := ruleOwners(rules)
	seen := make(map[string]bool)
	var out []supersededEnforce
	for i := range rules {
		s, yields := yieldsTo(&rules[i], owners)
		key := fmt.Sprintf("%s/%d", rules[i].Type, rules[i].GroupID)
		if !yields || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, s)
	}
	return out
}

// supersededNote formuliert die Zeile für den Job-Bericht.
func supersededNote(s supersededEnforce) string {
	winner := &s.Winner
	if s.Tie {
		return fmt.Sprintf("  [%s] übersprungen - Gleichstand beim Vorrang (%d) mit Gruppe %q; "+
			"entschieden hat die ältere Gruppe. Vorrang ausdrücklich setzen, um das festzulegen.",
			s.Rule.Name, enforceGroupPriority(winner), enforceGroupLabel(winner))
	}
	return fmt.Sprintf("  [%s] übersprungen - Vorrang liegt bei Gruppe %q (%d).",
		s.Rule.Name, enforceGroupLabel(winner), enforceGroupPriority(winner))
}

// enforceableRuleType meldet, ob ein Typ als Grundsatz-Regel taugt: Er muss
// einen Soll-Zustand tragen, gegen den sich prüfen lässt. Ein Shell-Kommando
// hat keinen - es liefe bedingungslos bei jedem Health-Check.
func enforceableRuleType(ruleType string) bool {
	switch ruleType {
	case domain.RuleTypeFirewall, domain.RuleTypeAptProxy,
		domain.RuleTypeACLSetup, domain.RuleTypePermSync:
		return true
	}
	return false
}

// dedupedEnforceType meldet Typen, deren Soll-Zustand ein bloßes
// „vorhanden/abgeglichen" ist. Fordern mehrere Gruppen dasselbe, ist das kein
// Konflikt, sondern eine Dopplung - die zweite Ausführung wäre wirkungslos.
// Sie werden deshalb ENTDOPPELT statt nach Vorrang aufgelöst; eine
// „übersprungen"-Meldung wäre hier Lärm, weil niemandem etwas entgeht.
func dedupedEnforceType(ruleType string) bool {
	switch ruleType {
	case domain.RuleTypeACLSetup, domain.RuleTypePermSync:
		return true
	}
	return false
}

// yieldNote formuliert den Bericht eines Laufs, der übersprungen wird, weil
// eine andere Gruppe den Typ auf diesem Server führt.
func yieldNote(s supersededEnforce) string {
	winner := &s.Winner
	note := fmt.Sprintf("übersprungen - auf diesem Server führt Gruppe %q (Vorrang %d) den Typ %q mit der Regel %q; "+
		"Regeln anderer Gruppen dieses Typs greifen hier nicht.",
		enforceGroupLabel(winner), enforceGroupPriority(winner), s.Rule.Type, winner.Name)
	if s.Tie {
		note += " Gleichstand beim Vorrang - entschieden hat die ältere Gruppe. Vorrang ausdrücklich setzen, um das festzulegen."
	}
	return note
}

// yieldToStrongerGroup prüft vor einem Lauf, ob die Regel auf diesem Server
// einer stärkeren Gruppe weicht, und liefert dann den Bericht für den Job.
// Die Regel selbst zählt mit, auch wenn sie gerade von Hand an einem
// abgeschalteten Zeitplan läuft - sonst wiche die führende Gruppe sich selbst.
//
// Ist der Bestand nicht lesbar, läuft die Regel wie bisher: Ein Lesefehler
// soll keine Firewall stilllegen.
func (e *Executor) yieldToStrongerGroup(server *domain.Server, rule *domain.Rule) (string, bool) {
	if !conflictingEnforceType(rule.Type) || rule.GroupID == 0 {
		return "", false
	}
	rules, err := e.groups.FindActiveRulesOfTypesForServer(server.ID, conflictingRuleTypes)
	if err != nil {
		slog.Warn("rule precedence not checked", "server", server.Name, "rule", rule.Name, "error", err)
		return "", false
	}
	self := *rule
	if self.Group == nil {
		self.Group, _ = e.groups.GroupByID(rule.GroupID)
	}
	s, yields := yieldsTo(&self, ruleOwners(append(rules, self)))
	if !yields {
		return "", false
	}
	return yieldNote(s), true
}

// yieldEnforceToScheduled lässt die Grundsatz-Regeln weichen, deren Typ eine
// stärkere Gruppe mit einer geplanten Regel führt. Ohne Regel mit Soll-Zustand
// spart sie sich die Abfrage.
func (e *Executor) yieldEnforceToScheduled(server *domain.Server, active []domain.Rule, superseded []supersededEnforce) ([]domain.Rule, []supersededEnforce) {
	if !anyConflictingType(active) {
		return active, superseded
	}
	rules, err := e.groups.FindActiveRulesOfTypesForServer(server.ID, conflictingRuleTypes)
	if err != nil {
		slog.Warn("rule precedence not checked", "server", server.Name, "error", err)
		return active, superseded
	}
	return yieldEnforceToOwners(active, superseded, ruleOwners(rules))
}

func anyConflictingType(rules []domain.Rule) bool {
	for i := range rules {
		if conflictingEnforceType(rules[i].Type) {
			return true
		}
	}
	return false
}
