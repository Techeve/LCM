package services_test

import (
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/storage/repositories"
)

// precedenceSetup legt einen Server in zwei Gruppen mit verschiedenem Vorrang
// an, jede mit einem eigenen Zeitplan und einer geplanten Firewall-Regel.
func precedenceSetup(t *testing.T, env *testEnv) (basis, web *domain.Rule, basisSchedule uint) {
	t.Helper()
	id := joinTestServer(t, env, "web01")
	env.Dialer.Responses["then echo ufw"] = sshx.FakeResponse{Output: "ufw\n"}
	env.Dialer.Responses["ufw status verbose"] = sshx.FakeResponse{Output: "Status: active\n"}

	basis = scheduledFirewall(t, env, id, "Basis", 10, "80")
	web = scheduledFirewall(t, env, id, "Web", 100, "8080")
	return basis, web, *basis.ScheduleID
}

func scheduledFirewall(t *testing.T, env *testEnv, serverID uint, groupName string, priority int, ports string) *domain.Rule {
	t.Helper()
	scope := repositories.ScopeAll()
	group, err := env.Groups.Create(groupName, "", &priority, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Groups.AssignServer(scope, group.ID, serverID, "admin"); err != nil {
		t.Fatal(err)
	}
	schedule, err := env.Groups.DefineSchedule(scope, group.ID, groupName+" nachts", "0 3 * * *", 0, "admin")
	if err != nil {
		t.Fatal(err)
	}
	rule, err := env.Groups.DefineRule(scope, group.ID, groupName+"-Firewall", domain.RuleTypeFirewall, ports, &schedule.ID, false, "admin")
	if err != nil {
		t.Fatal(err)
	}
	return rule
}

// lastJobOf liefert den jüngsten Job einer Regel.
func lastJobOf(t *testing.T, env *testEnv, ruleID uint) domain.Job {
	t.Helper()
	var job domain.Job
	if err := env.DB().Where("rule_id = ?", ruleID).Order("rowid desc").First(&job).Error; err != nil {
		t.Fatal(err)
	}
	return job
}

// TestGeplanteFirewallDerSchwaecherenGruppeWirdUebersprungen: Zwei Gruppen
// planen Firewall-Regeln für denselben Server. Vorher lief jede zu ihrer
// Uhrzeit, und die zuletzt gelaufene bestimmte die offenen Ports. Jetzt führt
// die Gruppe mit dem stärkeren Vorrang; die andere wird im Job benannt
// übersprungen.
func TestGeplanteFirewallDerSchwaecherenGruppeWirdUebersprungen(t *testing.T) {
	env := newTestEnv(t)
	basis, web, _ := precedenceSetup(t, env)

	env.Dialer.Commands = nil
	env.Executor.RunRule(web, "admin")

	if all := strings.Join(env.Dialer.Commands, "\n"); strings.Contains(all, "port 8080") {
		t.Errorf("die Regel der schwächeren Gruppe darf nicht angewendet werden:\n%s", all)
	}
	job := lastJobOf(t, env, web.ID)
	if job.Status != domain.JobStatusSuccess || !strings.Contains(job.Output, `Gruppe "Basis"`) || !strings.Contains(job.Output, "Basis-Firewall") {
		t.Errorf("Job muss als übersprungen die führende Gruppe nennen: %s / %s", job.Status, job.Output)
	}

	env.Dialer.Commands = nil
	env.Executor.RunRule(basis, "admin")
	if all := strings.Join(env.Dialer.Commands, "\n"); !strings.Contains(all, "ufw allow proto tcp to any port 80") {
		t.Errorf("die Regel der führenden Gruppe muss laufen:\n%s", all)
	}
}

// TestAbgeschalteterZeitplanFuehrtNicht: Eine Regel an einem abgeschalteten
// Zeitplan läuft nie - sie darf die Regel einer anderen Gruppe nicht verdrängen.
func TestAbgeschalteterZeitplanFuehrtNicht(t *testing.T) {
	env := newTestEnv(t)
	_, web, basisSchedule := precedenceSetup(t, env)
	if err := env.Groups.SetScheduleEnabled(repositories.ScopeAll(), basisSchedule, false, "admin"); err != nil {
		t.Fatal(err)
	}

	env.Dialer.Commands = nil
	env.Executor.RunRule(web, "admin")

	if all := strings.Join(env.Dialer.Commands, "\n"); !strings.Contains(all, "ufw allow proto tcp to any port 8080") {
		t.Errorf("ohne laufende Basis-Regel muss die Web-Regel greifen:\n%s", all)
	}
}

// TestHealthPingStelltGrundsatzRegelDerSchwaecherenGruppeZurueck: Eine Grundsatz-
// Regel der schwächeren Gruppe setzte beim nächsten Health-Ping zurück, was die
// geplante Regel der stärkeren Gruppe nachts angewendet hatte. Der Health-
// Bericht nennt sie jetzt als zurückgestellt.
func TestHealthPingStelltGrundsatzRegelDerSchwaecherenGruppeZurueck(t *testing.T) {
	env := newTestEnv(t)
	_, web, _ := precedenceSetup(t, env)
	if _, err := env.Groups.DefineRule(repositories.ScopeAll(), web.GroupID, "Web-Grundsatz", domain.RuleTypeFirewall, "8443", nil, true, "admin"); err != nil {
		t.Fatal(err)
	}

	env.Dialer.Commands = nil
	env.Executor.RunRule(findSystemHealthRule(t, env), "admin")

	if all := strings.Join(env.Dialer.Commands, "\n"); strings.Contains(all, "port 8443") {
		t.Errorf("die Grundsatz-Regel der schwächeren Gruppe darf nicht durchgesetzt werden:\n%s", all)
	}
	var job domain.Job
	if err := env.DB().Where("type = ?", domain.RuleTypeHealth).Order("rowid desc").First(&job).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(job.Output, "[Web-Grundsatz] übersprungen") || !strings.Contains(job.Output, `Gruppe "Basis"`) {
		t.Errorf("Health-Bericht muss die zurückgestellte Regel und die führende Gruppe nennen:\n%s", job.Output)
	}
}
