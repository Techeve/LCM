package services

import (
	"strings"
	"testing"

	"LCM/internal/core/domain"
)

// ruleIn baut eine Grundsatz-Regel in einer Gruppe mit gegebenem Vorrang.
func ruleIn(ruleID, groupID uint, groupName string, priority int, ruleType, ruleName string) domain.Rule {
	return domain.Rule{
		ID: ruleID, GroupID: groupID, Type: ruleType, Name: ruleName, Enforce: true,
		Group: &domain.ServerGroup{ID: groupID, Name: groupName, Priority: priority},
	}
}

// TestVorrangEntscheidetBeiGleichemTyp: Zwei Firewall-Grundsatz-Regeln aus
// zwei Gruppen - durchgesetzt wird nur die der stärkeren (kleineren) Zahl.
// Vorher liefen beide nacheinander und setzten sich bei jedem Health-Ping
// gegenseitig zurück.
func TestVorrangEntscheidetBeiGleichemTyp(t *testing.T) {
	rules := []domain.Rule{
		ruleIn(1, 7, "Web-Prod", 100, domain.RuleTypeFirewall, "Web-Firewall"),
		ruleIn(2, 3, "Basis-Härtung", 10, domain.RuleTypeFirewall, "Basis-Firewall"),
	}

	active, superseded := resolveEnforceRules(rules)

	if len(active) != 1 || active[0].Name != "Basis-Firewall" {
		t.Fatalf("erwartet nur die Regel der stärkeren Gruppe, bekam %+v", active)
	}
	if len(superseded) != 1 || superseded[0].Rule.Name != "Web-Firewall" {
		t.Fatalf("erwartet genau eine zurückgestellte Regel, bekam %+v", superseded)
	}
	if superseded[0].Tie {
		t.Error("unterschiedliche Vorränge dürfen nicht als Gleichstand gelten")
	}
	note := supersededNote(superseded[0])
	for _, want := range []string{"Web-Firewall", "Basis-Härtung", "10"} {
		if !strings.Contains(note, want) {
			t.Errorf("Meldung nennt %q nicht: %s", want, note)
		}
	}
}

// TestVorrangLaesstVerschiedeneTypenNebeneinander: Firewall und apt-Cache
// widersprechen einander nicht - beide müssen laufen.
func TestVorrangLaesstVerschiedeneTypenNebeneinander(t *testing.T) {
	rules := []domain.Rule{
		ruleIn(1, 7, "Web-Prod", 100, domain.RuleTypeFirewall, "Firewall"),
		ruleIn(2, 3, "Basis", 10, domain.RuleTypeAptProxy, "APT-Cache"),
	}

	active, superseded := resolveEnforceRules(rules)

	if len(active) != 2 {
		t.Fatalf("beide Typen müssen durchgesetzt werden, bekam %+v", active)
	}
	if len(superseded) != 0 {
		t.Fatalf("kein Konflikt zwischen verschiedenen Typen erwartet, bekam %+v", superseded)
	}
}

// TestVorrangGleichstandIstDeterministischUndSichtbar: Bei gleichem Vorrang
// gewinnt die ältere Gruppe (niedrigere ID) - das ist der Zustand im
// Altbestand, in dem alle Gruppen denselben Standardwert tragen. Er muss
// deterministisch sein UND als Gleichstand erkennbar bleiben, damit der
// Startup-Report ihn meldet.
func TestVorrangGleichstandIstDeterministischUndSichtbar(t *testing.T) {
	rules := []domain.Rule{
		ruleIn(9, 12, "Später angelegt", domain.DefaultGroupPriority, domain.RuleTypeFirewall, "Neu"),
		ruleIn(4, 5, "Zuerst angelegt", domain.DefaultGroupPriority, domain.RuleTypeFirewall, "Alt"),
	}

	active, superseded := resolveEnforceRules(rules)

	if len(active) != 1 || active[0].Name != "Alt" {
		t.Fatalf("bei Gleichstand muss die ältere Gruppe gewinnen, bekam %+v", active)
	}
	if len(superseded) != 1 || !superseded[0].Tie {
		t.Fatalf("Gleichstand muss als solcher markiert sein, bekam %+v", superseded)
	}
	if note := supersededNote(superseded[0]); !strings.Contains(note, "Gleichstand") {
		t.Errorf("Meldung benennt den Gleichstand nicht: %s", note)
	}

	// Umgekehrte Eingabereihenfolge, gleiches Ergebnis.
	activeRev, _ := resolveEnforceRules([]domain.Rule{rules[1], rules[0]})
	if len(activeRev) != 1 || activeRev[0].Name != "Alt" {
		t.Fatalf("Auswahl hängt an der Eingabereihenfolge: %+v", activeRev)
	}
}

// TestVorrangLaesstEingabeUnveraendert: resolveEnforceRules sortiert eine
// Kopie - der Aufrufer bekommt seine Liste unverändert zurück.
func TestVorrangLaesstEingabeUnveraendert(t *testing.T) {
	rules := []domain.Rule{
		ruleIn(1, 7, "Schwach", 100, domain.RuleTypeFirewall, "Erste"),
		ruleIn(2, 3, "Stark", 10, domain.RuleTypeFirewall, "Zweite"),
	}

	resolveEnforceRules(rules)

	if rules[0].Name != "Erste" || rules[1].Name != "Zweite" {
		t.Errorf("Eingabe wurde umsortiert: %s, %s", rules[0].Name, rules[1].Name)
	}
}

// TestVorrangOhneGeladeneGruppe: Ist die Gruppe nicht mitgeladen, gilt der
// Standardwert - die Auswahl darf nicht von einem zufälligen Nullwert abhängen.
func TestVorrangOhneGeladeneGruppe(t *testing.T) {
	ohneGruppe := domain.Rule{ID: 1, GroupID: 9, Type: domain.RuleTypeFirewall, Name: "Ohne Gruppe", Enforce: true}
	stark := ruleIn(2, 3, "Stark", 10, domain.RuleTypeFirewall, "Mit Gruppe")

	active, superseded := resolveEnforceRules([]domain.Rule{ohneGruppe, stark})

	if len(active) != 1 || active[0].Name != "Mit Gruppe" {
		t.Fatalf("die Regel mit ausdrücklichem Vorrang muss gewinnen, bekam %+v", active)
	}
	if len(superseded) != 1 {
		t.Fatalf("erwartet eine zurückgestellte Regel, bekam %+v", superseded)
	}
	// Der Gruppenname fehlt - die Meldung muss trotzdem etwas Nennbares zeigen.
	if note := supersededNote(superseded[0]); !strings.Contains(note, "Ohne Gruppe") {
		t.Errorf("Meldung nennt die zurückgestellte Regel nicht: %s", note)
	}
}

// TestNichtWidersprechendeTypenBleibenAlle: Typen ohne widersprüchlichen
// Soll-Zustand werden nicht über den Vorrang entschieden. Sonst würde beim
// späteren Hinzukommen solcher Typen (acl-setup, perm-sync) die zweite Gruppe
// stillschweigend übergangen, obwohl beide dasselbe fordern.
func TestNichtWidersprechendeTypenBleibenAlle(t *testing.T) {
	if conflictingEnforceType(domain.RuleTypeScript) {
		t.Error("script trägt keinen Soll-Zustand und darf nicht über den Vorrang entschieden werden")
	}
	rules := []domain.Rule{
		ruleIn(1, 7, "A", 100, domain.RuleTypeScript, "Skript A"),
		ruleIn(2, 3, "B", 10, domain.RuleTypeScript, "Skript B"),
	}
	active, superseded := resolveEnforceRules(rules)
	if len(active) != 2 || len(superseded) != 0 {
		t.Fatalf("nicht widersprechende Typen dürfen nicht zurückgestellt werden: %+v / %+v", active, superseded)
	}
}
