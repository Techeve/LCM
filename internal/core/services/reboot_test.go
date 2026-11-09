package services_test

import (
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

// TestRebootAdHocSuccess: die Ein-Klick-Aktion schickt das detached
// Neustart-Kommando und schließt den Job erst ab, wenn der Server wieder
// antwortet - samt Vermerk der Rückkehr im Protokoll.
func TestRebootAdHocSuccess(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	repo := repositories.NewServerRepository(env.DB())

	env.Dialer.Commands = nil
	job, err := env.Servers.Reboot(repositories.ScopeAll(), id, "admin")
	if err != nil {
		t.Fatalf("reboot fehlgeschlagen: %v", err)
	}
	if job.Type != domain.RuleTypeReboot {
		t.Errorf("erwartet job-typ %q, bekam %q", domain.RuleTypeReboot, job.Type)
	}
	done := waitServerJob(t, env, id, domain.RuleTypeReboot)
	if done.Status != domain.JobStatusSuccess {
		t.Fatalf("job nicht erfolgreich: %+v", done)
	}

	// Das Neustart-Kommando ist vom SSH-Kanal abgekoppelt (nohup + Hintergrund).
	all := strings.Join(env.Dialer.Commands, "\n")
	if !strings.Contains(all, "nohup") || !strings.Contains(all, "reboot") {
		t.Errorf("neustart-kommando fehlt/unvollständig:\n%s", all)
	}

	// Der Job endet erst, wenn der Server wieder antwortet - hier tut das der
	// Fake-Dialer sofort. Entsprechend gilt der Server danach wieder als
	// erreichbar und der Neustart-Hinweis ist abgeräumt. (Früher endete der
	// Job direkt nach dem Absetzen des Kommandos und ließ den Server als
	// „nicht erreichbar" stehen, bis der nächste Health-Check lief.)
	if !strings.Contains(done.Output, "wieder erreichbar") {
		t.Errorf("die rückkehr fehlt im job-protokoll:\n%s", done.Output)
	}
	srv, err := repo.FindByID(repositories.ScopeAll(), id)
	if err != nil {
		t.Fatal(err)
	}
	if !srv.Reachable {
		t.Error("nach der bestätigten rückkehr muss der server wieder als erreichbar gelten")
	}
	if srv.LastError != "" {
		t.Errorf("der neustart-hinweis sollte nach der rückkehr weg sein: %q", srv.LastError)
	}
}

// TestRebootRestrictedUsesHelper: Im eingeschränkten Modus steht `reboot` nicht
// auf der Whitelist, der Neustart läuft über den LCM-Helper.
func TestRebootRestrictedUsesHelper(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	env.DB().Model(&domain.Server{}).Where("id = ?", id).Update("restricted_sudo", true)

	env.Dialer.Commands = nil
	if _, err := env.Servers.Reboot(repositories.ScopeAll(), id, "admin"); err != nil {
		t.Fatalf("reboot fehlgeschlagen: %v", err)
	}
	if done := waitServerJob(t, env, id, domain.RuleTypeReboot); done.Status != domain.JobStatusSuccess {
		t.Fatalf("job nicht erfolgreich: %+v", done)
	}
	all := strings.Join(env.Dialer.Commands, "\n")
	if !strings.Contains(all, "lcm-helper reboot") || strings.Contains(all, "nohup") {
		t.Errorf("neustart läuft nicht über den helper:\n%s", all)
	}
}

// TestRebootDemoServerSimulated: Demo-Server werden nie per SSH kontaktiert.
func TestRebootDemoServerSimulated(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	env.DB().Model(&domain.Server{}).Where("id = ?", id).Update("is_demo", true)

	env.Dialer.Commands = nil
	if _, err := env.Servers.Reboot(repositories.ScopeAll(), id, "admin"); err != nil {
		t.Fatalf("demo-neustart sollte nicht fehlschlagen: %v", err)
	}
	if len(env.Dialer.Commands) != 0 {
		t.Errorf("demo-server sollte nie per ssh kontaktiert werden, kommandos: %v", env.Dialer.Commands)
	}
}

// TestRebootScheduledRule: die geplante Gruppen-Regel führt denselben
// detached Neustart aus wie die Ein-Klick-Aktion.
func TestRebootScheduledRule(t *testing.T) {
	env := newTestEnv(t)
	serverID := joinTestServer(t, env, "web01")
	group, err := env.Groups.Create("Web", "", nil, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Groups.AssignServer(repositories.ScopeAll(), group.ID, serverID, "admin"); err != nil {
		t.Fatal(err)
	}
	sched, err := env.Groups.DefineSchedule(repositories.ScopeAll(), group.ID, "wartungsfenster", "0 3 * * 0", 0, "admin")
	if err != nil {
		t.Fatal(err)
	}
	rule, err := env.Groups.DefineRule(repositories.ScopeAll(), group.ID, "neustart", domain.RuleTypeReboot, "", &sched.ID, false, "admin")
	if err != nil {
		t.Fatalf("reboot-rule definieren: %v", err)
	}

	env.Dialer.Commands = nil
	env.Executor.RunRule(rule, "admin")

	all := strings.Join(env.Dialer.Commands, "\n")
	if !strings.Contains(all, "nohup") || !strings.Contains(all, "reboot") {
		t.Errorf("neustart-kommando fehlt/unvollständig:\n%s", all)
	}
	// Auch die geplante Regel wartet die Rückkehr ab - ein Neustart aus dem
	// Wartungsfenster, aus dem der Server nicht zurückkommt, ist derselbe
	// Vorfall wie bei der Ein-Klick-Aktion. Der Fake-Dialer antwortet sofort
	// wieder, der Server gilt danach also als erreichbar.
	done := waitServerJob(t, env, serverID, domain.RuleTypeReboot)
	if !strings.Contains(done.Output, "wieder erreichbar") {
		t.Errorf("die rückkehr fehlt im job-protokoll:\n%s", done.Output)
	}
	repo := repositories.NewServerRepository(env.DB())
	srv, _ := repo.FindByID(repositories.ScopeAll(), serverID)
	if !srv.Reachable {
		t.Error("nach der bestätigten rückkehr muss der server wieder als erreichbar gelten")
	}
}

// TestRebootRuleRunsOnRestrictedServer: Eine geplante Neustart-Regel läuft auch
// auf einem eingeschränkten Server, über den LCM-Helper.
func TestRebootRuleRunsOnRestrictedServer(t *testing.T) {
	env := newTestEnv(t)
	serverID := joinTestServer(t, env, "web01")
	env.DB().Model(&domain.Server{}).Where("id = ?", serverID).Update("restricted_sudo", true)
	group, err := env.Groups.Create("Web", "", nil, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Groups.AssignServer(repositories.ScopeAll(), group.ID, serverID, "admin"); err != nil {
		t.Fatal(err)
	}
	sched, err := env.Groups.DefineSchedule(repositories.ScopeAll(), group.ID, "wartungsfenster", "0 3 * * 0", 0, "admin")
	if err != nil {
		t.Fatal(err)
	}
	rule, err := env.Groups.DefineRule(repositories.ScopeAll(), group.ID, "neustart", domain.RuleTypeReboot, "", &sched.ID, false, "admin")
	if err != nil {
		t.Fatalf("reboot-rule definieren: %v", err)
	}

	env.Dialer.Commands = nil
	env.Executor.RunRule(rule, "admin")

	if done := waitServerJob(t, env, serverID, domain.RuleTypeReboot); done.Status != domain.JobStatusSuccess {
		t.Fatalf("job nicht erfolgreich: %+v", done)
	}
	if all := strings.Join(env.Dialer.Commands, "\n"); !strings.Contains(all, "lcm-helper reboot") {
		t.Errorf("neustart läuft nicht über den helper:\n%s", all)
	}
}

// TestRebootRefusedOnAPIDevices: RouterOS und DSM haben keine POSIX-Shell, ein
// Shell-Neustart darf dort nie abgesetzt werden.
func TestRebootRefusedOnAPIDevices(t *testing.T) {
	for osID, want := range map[string]error{
		domain.OSIDRouterOS:    services.ErrRouterOSUnsupported,
		domain.OSIDSynologyDSM: services.ErrDSMUnsupported,
	} {
		env := newTestEnv(t)
		id := joinTestServer(t, env, "gerät")
		// os_id liegt verschlüsselt: über das Modell schreiben.
		repo := repositories.NewServerRepository(env.DB())
		srv, _ := repo.FindByIDUnscoped(id)
		srv.OSID = osID
		if err := repo.Update(srv); err != nil {
			t.Fatal(err)
		}
		env.Dialer.Commands = nil
		if _, err := env.Servers.Reboot(repositories.ScopeAll(), id, "admin"); err != want {
			t.Errorf("%s: erwartet %v, bekam %v", osID, want, err)
		}
		if len(env.Dialer.Commands) != 0 {
			t.Errorf("%s: es wurden Kommandos abgesetzt: %v", osID, env.Dialer.Commands)
		}
	}
}
