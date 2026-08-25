package services_test

import (
	"errors"
	"strconv"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

func TestSystemGroupIsProtected(t *testing.T) {
	env := newTestEnv(t)

	// Die beim Seeding angelegte System-Gruppe finden.
	groups, err := env.Groups.List(repositories.ScopeAll())
	if err != nil {
		t.Fatal(err)
	}
	var sysID uint
	for _, g := range groups {
		if g.IsSystem {
			sysID = g.ID
		}
	}
	if sysID == 0 {
		t.Fatal("system-gruppe wurde nicht geseedet")
	}

	if err := env.Groups.Disband(repositories.ScopeAll(), sysID, "admin"); !errors.Is(err, services.ErrProtectedGroup) {
		t.Errorf("system-gruppe darf nicht auflösbar sein: %v", err)
	}

	// Die System-Rules der Gruppe (Health-Check, Sync, Paketlisten-Scan,
	// Docker-Check, Anwendungs-Check) sind geschützt. Backup und Cleanup sind
	// KEINE Gruppen-Rules mehr (system-globale Schedules aus den
	// Einstellungen).
	rules, err := env.Groups.ListRules(repositories.ScopeAll(), sysID)
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{
		domain.RuleTypeHealth, domain.RuleTypeSync,
		domain.RuleTypePackageScan, domain.RuleTypeDockerCheck, domain.RuleTypeAppCheck,
	}
	if len(rules) != len(expected) {
		t.Fatalf("erwartet %d system-rules %v, bekam %d", len(expected), expected, len(rules))
	}
	existing := map[string]bool{}
	for _, r := range rules {
		existing[r.Type] = true
	}
	for _, typ := range expected {
		if !existing[typ] {
			t.Errorf("system-rule %s fehlt", typ)
		}
	}
	if err := env.Groups.RemoveRule(repositories.ScopeAll(), rules[0].ID, "admin"); !errors.Is(err, services.ErrProtectedRule) {
		t.Errorf("system-rule darf nicht löschbar sein: %v", err)
	}

	// Auch die System-Schedules (Health-Check + Sync) sind geschützt.
	schedules, err := env.Groups.ListSchedules(repositories.ScopeAll(), sysID)
	if err != nil {
		t.Fatal(err)
	}
	if len(schedules) != 2 {
		t.Fatalf("erwartet genau 2 system-schedules, bekam %d", len(schedules))
	}
	if err := env.Groups.RemoveSchedule(repositories.ScopeAll(), schedules[0].ID, "admin"); !errors.Is(err, services.ErrProtectedSchedule) {
		t.Errorf("system-schedule darf nicht löschbar sein: %v", err)
	}
}

func TestDefineScheduleAndRuleValidation(t *testing.T) {
	env := newTestEnv(t)
	group, err := env.Groups.Create("webserver", "Alle Webserver", nil, "admin")
	if err != nil {
		t.Fatal(err)
	}

	// Ungültiger Cron-Ausdruck am Schedule.
	_, err = env.Groups.DefineSchedule(repositories.ScopeAll(), group.ID, "Bad", "kein-cron", "admin")
	if !errors.Is(err, services.ErrInvalidCron) {
		t.Errorf("erwartet ErrInvalidCron, bekam %v", err)
	}
	sched, err := env.Groups.DefineSchedule(repositories.ScopeAll(), group.ID, "Nachtlauf", "0 3 * * *", "admin")
	if err != nil {
		t.Fatalf("gültiger schedule abgelehnt: %v", err)
	}

	// Ungültiger Rule-Typ.
	_, err = env.Groups.DefineRule(repositories.ScopeAll(), group.ID, "Bad", "unsinn", "", &sched.ID, false, "admin")
	if !errors.Is(err, services.ErrInvalidRuleType) {
		t.Errorf("erwartet ErrInvalidRuleType, bekam %v", err)
	}
	// Weder Schedule noch Enforce.
	_, err = env.Groups.DefineRule(repositories.ScopeAll(), group.ID, "Bad", domain.RuleTypeUpdate, "", nil, false, "admin")
	if !errors.Is(err, services.ErrRuleNeedsTarget) {
		t.Errorf("erwartet ErrRuleNeedsTarget, bekam %v", err)
	}
	// Schedule UND Enforce gleichzeitig.
	_, err = env.Groups.DefineRule(repositories.ScopeAll(), group.ID, "Bad", domain.RuleTypeScript, "true", &sched.ID, true, "admin")
	if !errors.Is(err, services.ErrRuleNeedsTarget) {
		t.Errorf("erwartet ErrRuleNeedsTarget, bekam %v", err)
	}
	// Enforce ist nur für zustandserzwingende Typen erlaubt.
	_, err = env.Groups.DefineRule(repositories.ScopeAll(), group.ID, "Bad", domain.RuleTypeUpdate, "", nil, true, "admin")
	if !errors.Is(err, services.ErrEnforceRuleType) {
		t.Errorf("erwartet ErrEnforceRuleType, bekam %v", err)
	}

	// Gültige Rule am Schedule.
	rule, err := env.Groups.DefineRule(repositories.ScopeAll(), group.ID, "Daily Update", domain.RuleTypeUpdate, "", &sched.ID, false, "admin")
	if err != nil {
		t.Fatalf("gültige rule abgelehnt: %v", err)
	}
	if rule.ID == 0 || !rule.Enabled || rule.ScheduleID == nil || *rule.ScheduleID != sched.ID {
		t.Error("rule nicht korrekt am schedule angelegt")
	}
	// Gültige Grundsatz-Regel (Enforce).
	enf, err := env.Groups.DefineRule(repositories.ScopeAll(), group.ID, "Firewall", domain.RuleTypeFirewall, "80,443", nil, true, "admin")
	if err != nil {
		t.Fatalf("gültige grundsatz-regel abgelehnt: %v", err)
	}
	if !enf.Enforce || enf.ScheduleID != nil {
		t.Error("grundsatz-regel nicht korrekt angelegt")
	}

	// Der Schedule liefert seine Rules mit; Löschen entfernt Rules mit.
	full, err := env.Groups.FindSchedule(repositories.ScopeAll(), sched.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(full.Rules) != 1 || full.Rules[0].ID != rule.ID {
		t.Fatalf("schedule sollte genau die eine rule enthalten, hat %d", len(full.Rules))
	}
	if err := env.Groups.RemoveSchedule(repositories.ScopeAll(), sched.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.Groups.FindRule(repositories.ScopeAll(), rule.ID); !errors.Is(err, repositories.ErrNotFound) {
		t.Errorf("rule des gelöschten schedules sollte mit gelöscht sein: %v", err)
	}
	// Die Grundsatz-Regel bleibt (hängt an keinem Schedule).
	if _, err := env.Groups.FindRule(repositories.ScopeAll(), enf.ID); err != nil {
		t.Errorf("grundsatz-regel darf nicht mit gelöscht werden: %v", err)
	}
}

func TestServerOnlyOncePerGroup(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	group, err := env.Groups.Create("prod", "Produktion", nil, "admin")
	if err != nil {
		t.Fatal(err)
	}

	// Zweimal denselben Server zuordnen - darf nur einmal Mitglied sein.
	if err := env.Groups.AssignServer(repositories.ScopeAll(), group.ID, id, "admin"); err != nil {
		t.Fatal(err)
	}
	if err := env.Groups.AssignServer(repositories.ScopeAll(), group.ID, id, "admin"); err != nil {
		t.Fatalf("zweite zuordnung sollte idempotent sein: %v", err)
	}

	full, _ := env.Groups.Get(repositories.ScopeAll(), group.ID)
	count := 0
	for _, s := range full.Servers {
		if s.ID == id {
			count++
		}
	}
	if count != 1 {
		t.Errorf("server ist %d-mal in der gruppe (erwartet genau 1)", count)
	}
}

// TestGruppenVorrang: Standardwert beim Anlegen, geprüfte Eingabe und die
// System-Gruppe als schwächste Stufe. Der Vorrang entscheidet, welche
// Grundsatz-Regel sich auf einem Server durchsetzt, den mehrere Gruppen
// bespielen - ein stiller Nullwert wäre hier ein Rechtefehler, kein Schönheits-
// fehler.
func TestGruppenVorrang(t *testing.T) {
	env := newTestEnv(t)

	// Ohne Angabe gilt der Standardwert.
	group, err := env.Groups.Create("web", "", nil, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if group.Priority != domain.DefaultGroupPriority {
		t.Errorf("standard-vorrang %d erwartet, bekam %d", domain.DefaultGroupPriority, group.Priority)
	}

	// Die System-Gruppe ist bewusst die schwächste: Ihre Regeln gelten für
	// alle Server und dürfen von einer spezifischeren Gruppe überstimmt werden.
	groups, err := env.Groups.List(repositories.ScopeAll())
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range groups {
		if g.IsSystem && g.Priority != domain.SystemGroupPriority {
			t.Errorf("system-gruppe: vorrang %d erwartet, bekam %d", domain.SystemGroupPriority, g.Priority)
		}
	}

	// Ausdrücklich gesetzter Vorrang wird übernommen.
	stark := 10
	updated, err := env.Groups.UpdateSettings(repositories.ScopeAll(), group.ID, "web", "", &stark, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Priority != 10 {
		t.Errorf("vorrang 10 erwartet, bekam %d", updated.Priority)
	}

	// Ohne Angabe bleibt er beim Bearbeiten unverändert.
	unchanged, err := env.Groups.UpdateSettings(repositories.ScopeAll(), group.ID, "web", "neue beschreibung", nil, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Priority != 10 {
		t.Errorf("vorrang darf sich ohne angabe nicht ändern, bekam %d", unchanged.Priority)
	}

	// Unzulässige Werte werden abgewiesen - nicht still auf 0 gesetzt.
	for _, bad := range []int{0, -1, domain.MaxGroupPriority + 1} {
		if _, err := env.Groups.Create("bad-"+strconv.Itoa(bad), "", &bad, "admin"); !errors.Is(err, services.ErrInvalidGroupPriority) {
			t.Errorf("vorrang %d muss abgewiesen werden, bekam %v", bad, err)
		}
	}
}
