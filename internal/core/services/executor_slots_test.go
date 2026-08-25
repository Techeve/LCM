package services_test

import (
	"sync"
	"testing"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

// TestZeitplaeneStapelnSichNicht hält fest, was die prozessweite Schranke
// leistet: Fallen zwei Zeitpläne auf dieselbe Minute - beim mitgelieferten
// Stand tun das der Viertelstunden-Health-Check und der nächtliche
// System-Sync -, dann addiert sich ihre Last NICHT.
//
// Die Grenze je Regel-Lauf allein reicht dafür nicht: Sie gilt eben je Lauf.
// Auf dem LCM-Host von Techeve endete das jede Nacht damit, dass der Prozess
// unter Speicherdruck minutenlang stehen blieb und systemd ihn abräumte.
func TestZeitplaeneStapelnSichNicht(t *testing.T) {
	env := newTestEnv(t)

	// Genug Server, dass die Läufe sich nicht schon am Job-Lock je Server
	// gegenseitig ausbremsen: Ohne Schranke könnten hier drei Läufe à
	// ruleParallelism gleichzeitig ziehen, also mehr als GlobalServerSlots.
	for i := 0; i < 20; i++ {
		joinTestServer(t, env, "web"+string(rune('a'+i)))
	}
	// Jedes Kommando bekommt eine kurze Dauer - sonst laufen die Läufe so
	// schnell durch, dass sie sich nie überlappen und der Höchststand nichts
	// über die Schranke aussagt.
	env.Dialer.Delay = 5 * time.Millisecond
	env.Dialer.Reset()
	env.Dialer.PeakConns = 0

	rules := []*domain.Rule{
		findSystemHealthRule(t, env),
		findSystemRuleOfType(t, env, domain.RuleTypeSync),
		findSystemRuleOfType(t, env, domain.RuleTypePackageScan),
	}

	var wg sync.WaitGroup
	for _, rule := range rules {
		wg.Add(1)
		go func() {
			defer wg.Done()
			env.Executor.RunRule(rule, "scheduler")
		}()
	}
	wg.Wait()

	if peak := env.Dialer.Peak(); peak > services.GlobalServerSlots {
		t.Errorf("Höchststand %d gleichzeitige Verbindungen, erlaubt sind %d - "+
			"die Läufe mehrerer Zeitpläne haben sich addiert", peak, services.GlobalServerSlots)
	}
}

// findSystemRuleOfType liefert die mitgelieferte Grundsatz-Regel einer Art.
func findSystemRuleOfType(t *testing.T, env *testEnv, kind string) *domain.Rule {
	t.Helper()
	groups, err := env.Groups.List(repositories.ScopeAll())
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range groups {
		if !g.IsSystem {
			continue
		}
		rules, err := env.Groups.ListRules(repositories.ScopeAll(), g.ID)
		if err != nil {
			t.Fatal(err)
		}
		for i := range rules {
			if rules[i].Type == kind {
				return &rules[i]
			}
		}
	}
	t.Fatalf("keine mitgelieferte Regel der Art %q gefunden", kind)
	return nil
}
