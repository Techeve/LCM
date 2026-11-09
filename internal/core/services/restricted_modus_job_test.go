package services_test

import (
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/storage/repositories"
)

// TestRestrictedAktionenLaufenUeberDenHelper: Sicherheits-Tools, Paket-Pins und
// Firewall sind im eingeschränkten Modus nicht mehr gesperrt. Die Vorschau zeigt
// dieselben Kommandos wie der Lauf, und im Lauf steht der Helper-Aufruf.
func TestRestrictedAktionenLaufenUeberDenHelper(t *testing.T) {
	scope := repositories.ScopeAll()
	fail2ban := services.SecurityToolInput{Tool: "fail2ban", AllowlistIPs: []string{"198.51.100.7"}}
	crowdsec := services.SecurityToolInput{Tool: "crowdsec", Bouncer: true}
	manage := func(tool, action string) services.SecurityToolManageInput {
		return services.SecurityToolManageInput{Tool: tool, Action: action, UnbanIP: "203.0.113.7", AllowlistIPs: []string{"198.51.100.7"}}
	}
	cases := []struct {
		name   string
		action string
		args   services.PreviewArgs
		helper string
		run    func(*testEnv, uint) (*domain.Job, error)
	}{
		{"fail2ban einrichten", services.PreviewSecurityTool, services.PreviewArgs{SecurityTool: fail2ban}, "lcm-helper fail2ban-config",
			func(e *testEnv, id uint) (*domain.Job, error) {
				return e.Servers.ConfigureSecurityTool(scope, id, fail2ban, "admin")
			}},
		{"crowdsec einrichten", services.PreviewSecurityTool, services.PreviewArgs{SecurityTool: crowdsec}, "lcm-helper crowdsec-configure",
			func(e *testEnv, id uint) (*domain.Job, error) {
				return e.Servers.ConfigureSecurityTool(scope, id, crowdsec, "admin")
			}},
		{"fail2ban stoppen", services.PreviewSecurityManage, services.PreviewArgs{SecurityTool: services.SecurityToolInput{Tool: "fail2ban"}, ToolAction: "stop"}, "lcm-helper security-service 'fail2ban' 'stop'",
			func(e *testEnv, id uint) (*domain.Job, error) {
				return e.Servers.ManageSecurityTool(scope, id, manage("fail2ban", "stop"), "admin")
			}},
		{"crowdsec entsperren", services.PreviewSecurityManage, services.PreviewArgs{SecurityTool: services.SecurityToolInput{Tool: "crowdsec"}, ToolAction: "unban", UnbanIP: "203.0.113.7"}, "lcm-helper security-unban 'crowdsec'",
			func(e *testEnv, id uint) (*domain.Job, error) {
				return e.Servers.ManageSecurityTool(scope, id, manage("crowdsec", "unban"), "admin")
			}},
		{"fail2ban entfernen", services.PreviewSecurityManage, services.PreviewArgs{SecurityTool: services.SecurityToolInput{Tool: "fail2ban"}, ToolAction: "uninstall"}, "lcm-helper security-purge 'fail2ban'",
			func(e *testEnv, id uint) (*domain.Job, error) {
				return e.Servers.ManageSecurityTool(scope, id, manage("fail2ban", "uninstall"), "admin")
			}},
		{"fail2ban Allowlist", services.PreviewSecurityManage, services.PreviewArgs{SecurityTool: fail2ban, ToolAction: "allowlist"}, "lcm-helper fail2ban-config",
			func(e *testEnv, id uint) (*domain.Job, error) {
				return e.Servers.ManageSecurityTool(scope, id, manage("fail2ban", "allowlist"), "admin")
			}},
		{"Pins", services.PreviewPinsApply, services.PreviewArgs{}, "lcm-helper package-pins 'apt' 'linux-image-*'",
			func(e *testEnv, id uint) (*domain.Job, error) { return e.Servers.ApplyPackagePins(scope, id, "admin") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newTestEnv(t)
			env.Servers.WithPackagePins(repositories.NewPackagePinRepository(env.DB()))
			id := joinTestServer(t, env, "web01")
			if err := env.DB().Create(&domain.PackagePin{Name: "linux-image-*", NoRemove: true}).Error; err != nil {
				t.Fatal(err)
			}
			setServerFields(t, env, id, map[string]any{"restricted_sudo": true, "lcm_source_ip": "192.0.2.10"})
			toolInstalled(env)
			assertActionMatchesPreview(t, env, id, tc.action, tc.args, false, func() (*domain.Job, error) { return tc.run(env, id) })
			// Die Kommandos stehen in sh -c '…' mit '\'' für jedes einfache Anführungszeichen.
			if all := strings.ReplaceAll(strings.Join(env.Dialer.Commands, "\n"), `'\''`, "'"); !strings.Contains(all, tc.helper) {
				t.Errorf("%q fehlt in den ausgeführten Kommandos:\n%s", tc.helper, all)
			}
		})
	}
}

// TestRestrictedFirewalldUeberHelper: firewalld im eingeschränkten Modus.
func TestRestrictedFirewalldUeberHelper(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "rocky01")
	setServerFields(t, env, id, map[string]any{"os_id": "rocky", "package_manager": "dnf", "firewall_tool": "", "restricted_sudo": true})
	env.Dialer.Responses["then echo ufw"] = sshx.FakeResponse{Output: "firewalld\n"}
	env.Dialer.Responses["firewall-status"] = sshx.FakeResponse{Output: "running\n"}
	env.Dialer.Commands = nil

	job, err := env.Servers.ConfigureFirewall(repositories.ScopeAll(), id, true, "80", domain.FirewallSSHSources{}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if done := waitForJob(t, env, job.ID); done.Status != domain.JobStatusSuccess {
		t.Fatalf("job fehlgeschlagen: %s", done.Output)
	}
	all := strings.ReplaceAll(strings.Join(env.Dialer.Commands, "\n"), `'\''`, "'")
	if !strings.Contains(all, "lcm-helper firewall-apply") || !strings.Contains(all, "'--add-port=80/tcp'") {
		t.Errorf("firewalld läuft nicht über den Helper:\n%s", all)
	}
	if strings.Contains(all, "firewall-cmd --permanent") {
		t.Errorf("firewall-cmd läuft am Helper vorbei:\n%s", all)
	}
}

// TestRestrictedAptCacheRegelUeberHelper: Die Gruppen-Regel „APT-Cache anbinden"
// lief im eingeschränkten Modus als Skript, das /etc/apt als normaler Benutzer
// beschreiben wollte. Jetzt geht sie über den Helper, wie die Server-Aktion.
func TestRestrictedAptCacheRegelUeberHelper(t *testing.T) {
	env := newTestEnv(t)
	serverID, groupID := aptProxyTestGroup(t, env)
	setServerFields(t, env, serverID, map[string]any{"restricted_sudo": true})
	sched, err := env.Groups.DefineSchedule(repositories.ScopeAll(), groupID, "nächtlich", "0 3 * * *", 0, "admin")
	if err != nil {
		t.Fatal(err)
	}
	rule, err := env.Groups.DefineRule(repositories.ScopeAll(), groupID, "apt-cache", domain.RuleTypeAptProxy, "", &sched.ID, false, "admin")
	if err != nil {
		t.Fatal(err)
	}
	env.Dialer.Commands = nil
	env.Executor.RunRule(rule, "admin")

	all := strings.ReplaceAll(strings.Join(env.Dialer.Commands, "\n"), `'\''`, "'")
	if !strings.Contains(all, "lcm-helper apt-proxy 'http://cache.local:3142'") {
		t.Errorf("die Regel ruft den Helper nicht auf:\n%s", all)
	}
	if strings.Contains(all, "02lcm-apt-cache") {
		t.Errorf("die Regel schreibt das Drop-in selbst:\n%s", all)
	}
}
