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
	sort.SliceStable(ordered, func(i, j int) bool {
		pi, pj := enforceGroupPriority(&ordered[i]), enforceGroupPriority(&ordered[j])
		if pi != pj {
			return pi < pj
		}
		if ordered[i].GroupID != ordered[j].GroupID {
			return ordered[i].GroupID < ordered[j].GroupID
		}
		return ordered[i].ID < ordered[j].ID
	})

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
		rules, err := s.groups.FindEnforceRulesForServer(servers[i].ID)
		if err != nil {
			slog.Warn("enforce overlap check skipped", "server", servers[i].Name, "error", err)
			continue
		}
		_, superseded := resolveEnforceRules(rules)
		for _, sup := range superseded {
			if !sup.Tie {
				continue
			}
			slog.Warn("grundsatz-regeln gleichen typs treffen ohne unterscheidbaren vorrang zusammen",
				"server", servers[i].Name, "typ", sup.Rule.Type,
				"wirksam", enforceGroupLabel(&sup.Winner)+"/"+sup.Winner.Name,
				"zurueckgestellt", enforceGroupLabel(&sup.Rule)+"/"+sup.Rule.Name,
				"vorrang", enforceGroupPriority(&sup.Rule),
				"hinweis", "Vorrang der Gruppen ausdrücklich setzen (kleinere Zahl gewinnt)")
		}
	}
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

// enforceOnlyRuleType meldet Typen, die AUSSCHLIESSLICH als Grundsatz-Regel
// taugen. Sie beschreiben einen Zustand, der gelten soll - nicht eine Aktion
// zu einer Uhrzeit. An einem Zeitplan hätten sie keinen Ausführungspfad und
// meldeten nur „kein Kommando für rule-typ".
func enforceOnlyRuleType(ruleType string) bool {
	switch ruleType {
	case domain.RuleTypeACLSetup, domain.RuleTypePermSync:
		return true
	}
	return false
}
