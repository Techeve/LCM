package services_test

import (
	"errors"
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

// TestAssignManagerMakesServersVisible deckt BUG-018 ab: Das Datenmodell kannte
// ServerGroup.Managers und die Mandantentrennung filterte danach - aber es gab
// keinen einzigen Endpunkt, der die Beziehung SCHREIBT. Sie wurde nur gelesen.
// Jeder Manager sah deshalb dauerhaft eine leere Server-Liste, egal was ein
// Administrator ihm zuweisen wollte; das im README als Kernfunktion beworbene
// RBAC-Feature war damit funktionslos.
func TestAssignManagerMakesServersVisible(t *testing.T) {
	env := newTestEnv(t)
	serverID := joinTestServer(t, env, "web01")

	group, err := env.Groups.Create("Produktion", "", nil, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Groups.AssignServer(repositories.ScopeAll(), group.ID, serverID, "admin"); err != nil {
		t.Fatal(err)
	}

	manager, err := env.Users.CreateUser("manager1", "manager1@example.com",
		"Anker5-Leuchtturm!Wind", "Mana", "Ger", []string{domain.RoleManager}, "test-admin")
	if err != nil {
		t.Fatalf("manager anlegen: %v", err)
	}
	scope := repositories.ScopeManager(manager.ID)

	// Vor der Zuweisung: der Manager sieht nichts - das war der Dauerzustand.
	if before, err := env.Servers.List(scope); err != nil || len(before) != 0 {
		t.Fatalf("vor der Zuweisung erwartet 0 Server, bekam %d (err %v)", len(before), err)
	}

	if err := env.Groups.AssignManager(repositories.ScopeAll(), group.ID, manager.ID, "admin"); err != nil {
		t.Fatalf("AssignManager: %v", err)
	}

	after, err := env.Servers.List(scope)
	if err != nil || len(after) != 1 {
		t.Fatalf("nach der Zuweisung erwartet 1 Server, bekam %d (err %v)", len(after), err)
	}
	if after[0].ID != serverID {
		t.Errorf("falscher Server sichtbar: %+v", after[0])
	}

	// Entziehen wirkt ebenfalls.
	if err := env.Groups.RemoveManager(repositories.ScopeAll(), group.ID, manager.ID, "admin"); err != nil {
		t.Fatalf("RemoveManager: %v", err)
	}
	if again, _ := env.Servers.List(scope); len(again) != 0 {
		t.Errorf("nach dem Entziehen erwartet 0 Server, bekam %d", len(again))
	}
}

// TestAssignManagerRejectsUnknownUser: eine unbekannte Benutzer-ID muss zu
// einem sauberen Nicht-gefunden-Fehler führen, nicht zu einem durchschlagenden
// Datenbankfehler.
func TestAssignManagerRejectsUnknownUser(t *testing.T) {
	env := newTestEnv(t)
	group, err := env.Groups.Create("Produktion", "", nil, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Groups.AssignManager(repositories.ScopeAll(), group.ID, 9999, "admin"); err == nil {
		t.Fatal("erwartete einen Fehler für eine unbekannte Benutzer-ID")
	}
}

// TestRegelKommandoNurWoVerwendet (R2-088): Ein Kommando für einen Regeltyp,
// der keines auswertet (z. B. apt-proxy), wurde gespeichert und beim Lauf
// still ignoriert - eine Falle. Jetzt: Ablehnung mit Klartext.
func TestRegelKommandoNurWoVerwendet(t *testing.T) {
	env := newTestEnv(t)
	group, err := env.Groups.Create("cmdgrp", "", nil, "admin")
	if err != nil {
		t.Fatal(err)
	}

	// apt-proxy mit Kommando: abgelehnt (früher 201 + totes Feld).
	_, err = env.Groups.DefineRule(repositories.ScopeAll(), group.ID,
		"proxy", domain.RuleTypeAptProxy, "http://example.invalid:3142", nil, true, "admin")
	if !errors.Is(err, services.ErrRuleCommandUnused) {
		t.Fatalf("apt-proxy mit Kommando muss abgelehnt werden, bekam: %v", err)
	}
	// Ohne Kommando bleibt der Typ nutzbar.
	if _, err := env.Groups.DefineRule(repositories.ScopeAll(), group.ID,
		"proxy", domain.RuleTypeAptProxy, "", nil, true, "admin"); err != nil {
		t.Fatalf("apt-proxy ohne Kommando: %v", err)
	}
	// Typen MIT Kommando bleiben unberührt (script braucht seins) - seit
	// R2-087 allerdings nur noch an einem Zeitplan, nicht als Grundsatz-Regel.
	sched, err := env.Groups.DefineSchedule(repositories.ScopeAll(), group.ID, "nachts", "0 3 * * *", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.Groups.DefineRule(repositories.ScopeAll(), group.ID,
		"skript", domain.RuleTypeScript, "uptime", &sched.ID, false, "admin"); err != nil {
		t.Fatalf("script mit Kommando: %v", err)
	}
}

// TestGlobaleRegelSichtRespektiertDenScope (R2-085): Regeln waren nur je
// Gruppe abrufbar - die globale Sicht muss alle sichtbaren Gruppen umfassen,
// für Manager aber NUR die eigenen (Mandantentrennung).
func TestGlobaleRegelSichtRespektiertDenScope(t *testing.T) {
	env := newTestEnv(t)

	a, err := env.Groups.Create("Mandant A", "", nil, "admin")
	if err != nil {
		t.Fatal(err)
	}
	b, err := env.Groups.Create("Mandant B", "", nil, "admin")
	if err != nil {
		t.Fatal(err)
	}
	schedA, err := env.Groups.DefineSchedule(repositories.ScopeAll(), a.ID, "nachts", "0 3 * * *", "admin")
	if err != nil {
		t.Fatal(err)
	}
	schedB, err := env.Groups.DefineSchedule(repositories.ScopeAll(), b.ID, "nachts", "0 4 * * *", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.Groups.DefineRule(repositories.ScopeAll(), a.ID,
		"A-Update", domain.RuleTypeUpdate, "", &schedA.ID, false, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.Groups.DefineRule(repositories.ScopeAll(), b.ID,
		"B-Health", domain.RuleTypeHealth, "", &schedB.ID, false, "admin"); err != nil {
		t.Fatal(err)
	}

	// Admin sieht beide (plus evtl. System-Gruppen-Regeln), inkl. Gruppe.
	all, err := env.Groups.ListAllRules(repositories.ScopeAll())
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, r := range all {
		names[r.Name] = true
		if r.Name == "A-Update" && (r.Group == nil || r.Group.Name != "Mandant A") {
			t.Errorf("Regel ohne Gruppen-Kontext: %+v", r)
		}
	}
	if !names["A-Update"] || !names["B-Health"] {
		t.Fatalf("globale Sicht unvollständig: %v", names)
	}

	// Manager von A sieht NUR die A-Regel.
	manager, err := env.Users.CreateUser("manager2", "m2@example.com",
		"Anker5-Leuchtturm!Wind", "M", "Zwei", []string{domain.RoleManager}, "test-admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Groups.AssignManager(repositories.ScopeAll(), a.ID, manager.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	mine, err := env.Groups.ListAllRules(repositories.ScopeManager(manager.ID))
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range mine {
		if r.Name == "B-Health" {
			t.Error("Manager von A darf die Regel von Mandant B nicht sehen")
		}
	}
	found := false
	for _, r := range mine {
		if r.Name == "A-Update" {
			found = true
		}
	}
	if !found {
		t.Error("Manager von A muss die eigene Regel sehen")
	}
}

// TestFirewallGrundsatzregelBrauchtSollZustand (R2-082): Eine
// Firewall-Grundsatz-Regel ohne erkennbaren Soll-Zustand setzte still „alles
// außer SSH schließen" durch - bestandsweit, alle 15 Minuten, ohne
// Rückmeldung. Beim Anlegen UND beim Ändern wird das jetzt abgewiesen; die
// leere Liste bleibt als ausdrückliche Ansage erlaubt.
func TestFirewallGrundsatzregelBrauchtSollZustand(t *testing.T) {
	env := newTestEnv(t)
	group, err := env.Groups.Create("Produktion", "", nil, "admin")
	if err != nil {
		t.Fatal(err)
	}
	scope := repositories.ScopeAll()

	// Ohne Soll-Zustand: abgelehnt.
	if _, err := env.Groups.DefineRule(scope, group.ID, "fw-leer",
		domain.RuleTypeFirewall, "", nil, true, "admin"); !errors.Is(err, services.ErrEnforceFirewallSpec) {
		t.Errorf("leerer Soll-Zustand muss abgelehnt werden, bekam: %v", err)
	}
	// Unverstandener Text (TS-18-08: "echo x" wurde als „keine Ports" gelesen).
	if _, err := env.Groups.DefineRule(scope, group.ID, "fw-quatsch",
		domain.RuleTypeFirewall, "echo x", nil, true, "admin"); !errors.Is(err, services.ErrEnforceFirewallSpec) {
		t.Errorf("unverstandener Soll-Zustand muss abgelehnt werden, bekam: %v", err)
	}
	// Ausdrücklich „nur SSH": erlaubt.
	nurSSH, err := env.Groups.DefineRule(scope, group.ID, "fw-nur-ssh",
		domain.RuleTypeFirewall, "[]", nil, true, "admin")
	if err != nil {
		t.Fatalf("ausdrückliches [] muss erlaubt sein: %v", err)
	}
	// Gültige Portliste: erlaubt.
	rule, err := env.Groups.DefineRule(scope, group.ID, "fw-web",
		domain.RuleTypeFirewall, `[{"port":443,"proto":"tcp"}]`, nil, true, "admin")
	if err != nil {
		t.Fatalf("gültiger Soll-Zustand abgelehnt: %v", err)
	}

	// Auch der Änderungspfad (PATCH) prüft - sonst ließe sich die Regel
	// nachträglich entwerten.
	if _, err := env.Groups.UpdateRule(scope, rule.ID, "", "echo x", "admin"); !errors.Is(err, services.ErrEnforceFirewallSpec) {
		t.Errorf("UpdateRule muss unverstandenen Soll-Zustand ablehnen, bekam: %v", err)
	}
	if _, err := env.Groups.UpdateRule(scope, nurSSH.ID, "", "[]", "admin"); err != nil {
		t.Errorf("UpdateRule auf [] muss erlaubt bleiben: %v", err)
	}
}

// TestScriptIstKeineGrundsatzregel (R2-087): Ein Shell-Kommando hat keinen
// Soll-Zustand - als Grundsatz-Regel lief es bedingungslos bei jedem
// Health-Check (auf der System-Gruppe 1344-mal am Tag, ohne Job und ohne
// Audit). Als Zeitplan-Regel bleibt der Typ selbstverständlich nutzbar.
func TestScriptIstKeineGrundsatzregel(t *testing.T) {
	env := newTestEnv(t)
	group, err := env.Groups.Create("Produktion", "", nil, "admin")
	if err != nil {
		t.Fatal(err)
	}
	scope := repositories.ScopeAll()

	_, err = env.Groups.DefineRule(scope, group.ID, "cron-ersatz",
		domain.RuleTypeScript, "touch /tmp/marker", nil, true, "admin")
	if !errors.Is(err, services.ErrEnforceRuleType) {
		t.Fatalf("script als Grundsatz-Regel muss abgelehnt werden, bekam: %v", err)
	}
	if !strings.Contains(err.Error(), "Zeitplan") {
		t.Errorf("die Meldung soll den Weg über einen Zeitplan nennen: %v", err)
	}

	// Am Zeitplan bleibt script erlaubt.
	sched, err := env.Groups.DefineSchedule(scope, group.ID, "nachts", "0 3 * * *", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.Groups.DefineRule(scope, group.ID, "nachtskript",
		domain.RuleTypeScript, "touch /tmp/marker", &sched.ID, false, "admin"); err != nil {
		t.Errorf("script an einem Zeitplan muss weiter erlaubt sein: %v", err)
	}
}
