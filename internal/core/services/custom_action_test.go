package services_test

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

func TestCustomActionCRUDAndValidation(t *testing.T) {
	env := newTestEnv(t)

	// Name Pflicht.
	if _, err := env.CustomActions.Create("", "", "echo hi", "admin"); !errors.Is(err, services.ErrCustomActionNameRequired) {
		t.Errorf("leerer name nicht abgelehnt: %v", err)
	}
	// Mindestens ein Kommando.
	if _, err := env.CustomActions.Create("Leer", "", "\n  \n# nur kommentar\n", "admin"); !errors.Is(err, services.ErrCustomActionEmpty) {
		t.Errorf("kommandolose aktion nicht abgelehnt: %v", err)
	}
	// Gültig anlegen.
	act, err := env.CustomActions.Create("Neustart-Check", "prüft dienste", "systemctl is-active nginx\nuptime", "admin")
	if err != nil {
		t.Fatalf("gültige aktion abgelehnt: %v", err)
	}
	if act.ID == 0 {
		t.Fatal("aktion nicht angelegt")
	}

	// Liste + Update.
	list, _ := env.CustomActions.List()
	if len(list) != 1 {
		t.Fatalf("erwartet 1 aktion, bekam %d", len(list))
	}
	if _, err := env.CustomActions.Update(act.ID, "Neustart-Check", "neu", "uptime", "admin"); err != nil {
		t.Fatalf("update: %v", err)
	}
}

// TestCustomActionDeleteBlockedWhileUsed prüft, dass eine von einer Rule
// referenzierte Custom-Aktion nicht gelöscht werden kann.
func TestCustomActionDeleteBlockedWhileUsed(t *testing.T) {
	env := newTestEnv(t)
	act, err := env.CustomActions.Create("Aufräumen", "", "rm -f /tmp/x\ntrue", "admin")
	if err != nil {
		t.Fatal(err)
	}
	group, _ := env.Groups.Create("Ops", "", nil, "admin")
	sched, _ := env.Groups.DefineSchedule(repositories.ScopeAll(), group.ID, "Nacht", "0 3 * * *", "admin")
	// Custom-Rule verweist per Command auf die Action-ID.
	if _, err := env.Groups.DefineRule(repositories.ScopeAll(), group.ID, "cleanup", domain.RuleTypeCustom,
		strconv.FormatUint(uint64(act.ID), 10), &sched.ID, false, "admin"); err != nil {
		t.Fatalf("custom-rule definieren: %v", err)
	}

	// Löschen ist blockiert, solange die Rule existiert.
	if err := env.CustomActions.Delete(act.ID, "admin"); !errors.Is(err, services.ErrCustomActionInUse) {
		t.Errorf("löschen einer benutzten aktion sollte blockiert sein: %v", err)
	}

	// Nicht existierende Action-ID in einer Custom-Rule wird abgelehnt.
	if _, err := env.Groups.DefineRule(repositories.ScopeAll(), group.ID, "bad", domain.RuleTypeCustom,
		"99999", &sched.ID, false, "admin"); !errors.Is(err, services.ErrCustomActionNotSelected) {
		t.Errorf("unbekannte custom-aktion nicht abgelehnt: %v", err)
	}
}

// TestCustomRuleRunsCommandsSequentially prüft die Ausführung: alle
// Kommandos der Aktion laufen in Reihenfolge auf dem Server.
func TestCustomRuleRunsCommandsSequentially(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	act, err := env.CustomActions.Create("Wartung", "", "echo eins\necho zwei\necho drei", "admin")
	if err != nil {
		t.Fatal(err)
	}
	group, _ := env.Groups.Create("Ops", "", nil, "admin")
	if err := env.Groups.AssignServer(repositories.ScopeAll(), group.ID, id, "admin"); err != nil {
		t.Fatal(err)
	}
	sched, _ := env.Groups.DefineSchedule(repositories.ScopeAll(), group.ID, "Nacht", "0 3 * * *", "admin")
	rule, err := env.Groups.DefineRule(repositories.ScopeAll(), group.ID, "wartung", domain.RuleTypeCustom,
		strconv.FormatUint(uint64(act.ID), 10), &sched.ID, false, "admin")
	if err != nil {
		t.Fatal(err)
	}

	env.Dialer.Commands = nil
	env.Executor.RunRule(rule, "admin")

	all := strings.Join(env.Dialer.Commands, "\n")
	for _, want := range []string{"echo eins", "echo zwei", "echo drei"} {
		if !strings.Contains(all, want) {
			t.Errorf("kommando %q nicht ausgeführt:\n%s", want, all)
		}
	}
	// Die Kommandos laufen als root (sudo), damit z.B. apt/systemctl geht.
	if !strings.Contains(all, "sudo sh -c") {
		t.Errorf("custom-kommandos sollten als root (sudo) laufen:\n%s", all)
	}
}
