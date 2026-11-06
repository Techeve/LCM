package services_test

import (
	"errors"
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/storage/repositories"
)

// assertActionMatchesPreview ist assertRunMatchesPreview für Aktionen, die
// synchron laufen oder einen Job starten: Jeder Schritt der Vorschau, der
// immer läuft, muss genau so an die SSH-Verbindung gegangen sein. Bedingte
// Schritte (when) prüft der Test nur, wenn er sie mit conditional ausdrücklich
// erwartet.
func assertActionMatchesPreview(t *testing.T, env *testEnv, id uint, action string, args services.PreviewArgs, conditional bool, run func() (*domain.Job, error)) *services.ActionPreview {
	t.Helper()
	server, err := env.Servers.Get(repositories.ScopeAll(), id)
	if err != nil {
		t.Fatal(err)
	}
	p := previewOf(t, env, id, action, args)
	want := p.ExpectedCommandsForTest(server)
	if len(want) == 0 {
		t.Fatalf("%s: Vorschau ohne Kommandos", action)
	}

	env.Dialer.Commands = nil
	job, err := run()
	if job != nil {
		waitForJob(t, env, job.ID)
	} else if err != nil && !conditional {
		t.Fatalf("%s: Lauf: %v", action, err)
	}

	for i, cmd := range want {
		if p.Commands[i].When != "" && !conditional {
			continue
		}
		if !dialed(env, cmd) {
			t.Errorf("%s: der Lauf hat dieses Kommando der Vorschau nicht abgesetzt (when=%q):\n%s\nabgesetzt:\n%s",
				action, p.Commands[i].When, cmd, strings.Join(env.Dialer.Commands, "\n---\n"))
		}
	}
	return p
}

func dialed(env *testEnv, cmd string) bool {
	for _, got := range env.Dialer.Commands {
		if got == cmd {
			return true
		}
	}
	return false
}

// syncRun macht aus einer synchronen Aktion die Form, die der Abgleich erwartet.
func syncRun(run func() (string, error)) func() (*domain.Job, error) {
	return func() (*domain.Job, error) {
		_, err := run()
		return nil, err
	}
}

func setServerFields(t *testing.T, env *testEnv, id uint, fields map[string]any) {
	t.Helper()
	if err := env.DB().Model(&domain.Server{}).Where("id = ?", id).Updates(fields).Error; err != nil {
		t.Fatal(err)
	}
}

// TestPreviewMatchesRunSSHAndFirewall: Firewall, SSH-Port, Root-Login und
// SSH-Härtung setzen genau die Befehle ab, die ihre Vorschau zeigt.
func TestPreviewMatchesRunSSHAndFirewall(t *testing.T) {
	scope := repositories.ScopeAll()
	rules := `[{"port":443,"proto":"tcp"}]`
	cases := []struct {
		name   string
		action string
		args   services.PreviewArgs
		setup  func(*testing.T, *testEnv, uint)
		run    func(*testEnv, uint) (*domain.Job, error)
	}{
		{"firewall anwenden", services.PreviewFirewallApply, services.PreviewArgs{FirewallRules: rules}, nil,
			func(e *testEnv, id uint) (*domain.Job, error) {
				return e.Servers.ConfigureFirewall(scope, id, true, rules, domain.FirewallSSHSources{}, "admin")
			}},
		{"firewall erneut anwenden", services.PreviewFirewallApply, services.PreviewArgs{},
			func(t *testing.T, e *testEnv, id uint) {
				setServerFields(t, e, id, map[string]any{"firewall_tool": "ufw", "firewall_active": true, "firewall_rules": rules})
			},
			func(e *testEnv, id uint) (*domain.Job, error) {
				return e.Servers.ConfigureFirewall(scope, id, true, rules, domain.FirewallSSHSources{}, "admin")
			}},
		{"firewall aus", services.PreviewFirewallDisable, services.PreviewArgs{}, nil,
			func(e *testEnv, id uint) (*domain.Job, error) {
				return e.Servers.ConfigureFirewall(scope, id, false, "", domain.FirewallSSHSources{}, "admin")
			}},
		{"ssh-port", services.PreviewSSHPort, services.PreviewArgs{Port: 2222},
			func(t *testing.T, e *testEnv, id uint) {
				setServerFields(t, e, id, map[string]any{"firewall_tool": "ufw", "firewall_active": true})
			},
			func(e *testEnv, id uint) (*domain.Job, error) {
				return syncRun(func() (string, error) { return e.Servers.ChangeSSHPort(scope, id, 2222, "admin") })()
			}},
		{"root sperren", services.PreviewRootLoginLock, services.PreviewArgs{}, nil,
			func(e *testEnv, id uint) (*domain.Job, error) {
				return syncRun(func() (string, error) { return e.Servers.SetSSHRootLogin(scope, id, true, "admin") })()
			}},
		{"root freigeben", services.PreviewRootLoginUnlock, services.PreviewArgs{}, nil,
			func(e *testEnv, id uint) (*domain.Job, error) {
				return syncRun(func() (string, error) { return e.Servers.SetSSHRootLogin(scope, id, false, "admin") })()
			}},
		{"härten", services.PreviewHardenSSH, services.PreviewArgs{},
			func(t *testing.T, e *testEnv, _ uint) {
				e.Dialer.Responses["sshd -T 2>&1"] = sshx.FakeResponse{Output: "passwordauthentication no\n"}
			},
			func(e *testEnv, id uint) (*domain.Job, error) {
				return syncRun(func() (string, error) { return e.Servers.HardenSSH(scope, id, "admin") })()
			}},
		{"härtung aufheben", services.PreviewUnhardenSSH, services.PreviewArgs{}, nil,
			func(e *testEnv, id uint) (*domain.Job, error) {
				return syncRun(func() (string, error) { return e.Servers.UnhardenSSH(scope, id, "admin") })()
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newTestEnv(t)
			id := joinTestServer(t, env, "web01")
			if tc.setup != nil {
				tc.setup(t, env, id)
			}
			assertActionMatchesPreview(t, env, id, tc.action, tc.args, false, func() (*domain.Job, error) { return tc.run(env, id) })
		})
	}
}

// TestPreviewConditionalStepsRun: Die bedingten Schritte der Vorschau sind
// die, die der Lauf im Bedarfsfall absetzt - Firewall-Installation, Rollback
// des Portwechsels und Stilllegen einer vorrangigen PermitRootLogin-Zeile.
func TestPreviewConditionalStepsRun(t *testing.T) {
	scope := repositories.ScopeAll()

	t.Run("firewall installieren", func(t *testing.T) {
		env := newTestEnv(t)
		id := joinTestServer(t, env, "web01")
		p := assertActionMatchesPreview(t, env, id, services.PreviewFirewallApply, services.PreviewArgs{FirewallRules: "[]"}, true,
			func() (*domain.Job, error) {
				return env.Servers.ConfigureFirewall(scope, id, true, "[]", domain.FirewallSSHSources{}, "admin")
			})
		if p.Commands[1].When != "if_needed" || !hasNote(p, "preview.firewall_install") {
			t.Errorf("Installation nicht als bedingter Schritt: %+v %+v", p.Commands[1], p.Notes)
		}
	})

	t.Run("portwechsel rollback", func(t *testing.T) {
		env := newTestEnv(t)
		id := joinTestServer(t, env, "web01")
		setServerFields(t, env, id, map[string]any{"firewall_tool": "ufw", "firewall_active": true})
		env.Dialer.FailKeyPort = 2222
		server, _ := env.Servers.Get(scope, id)
		p := previewOf(t, env, id, services.PreviewSSHPort, services.PreviewArgs{Port: 2222})
		want := p.ExpectedCommandsForTest(server)
		env.Dialer.Commands = nil
		if _, err := env.Servers.ChangeSSHPort(scope, id, 2222, "admin"); err == nil {
			t.Fatal("Portwechsel trotz unerreichbarem Port erfolgreich")
		}
		for i, c := range p.Commands {
			if c.When == "on_failure" && !dialed(env, want[i]) {
				t.Errorf("Rollback-Schritt nicht abgesetzt:\n%s", want[i])
			}
		}
	})

	t.Run("root-login stilllegen", func(t *testing.T) {
		env := newTestEnv(t)
		id := joinTestServer(t, env, "web01")
		env.Dialer.Responses["LCMEFFECTIVE"] = sshx.FakeResponse{Output: "LCMEFFECTIVE=yes\nLCMSOURCES\n/etc/ssh/sshd_config:3:PermitRootLogin yes\n"}
		assertActionMatchesPreview(t, env, id, services.PreviewRootLoginLock, services.PreviewArgs{}, true,
			syncRun(func() (string, error) { return env.Servers.SetSSHRootLogin(scope, id, true, "admin") }))
	})
}

// TestPreviewSSHAndFirewallRefusedLikeRun: Wo der Lauf abweist, weist die
// Vorschau mit demselben Fehler ab - RouterOS, DSM, Agent, Proxmox.
func TestPreviewSSHAndFirewallRefusedLikeRun(t *testing.T) {
	scope := repositories.ScopeAll()
	ssh := []string{services.PreviewSSHPort, services.PreviewRootLoginLock, services.PreviewRootLoginUnlock,
		services.PreviewHardenSSH, services.PreviewUnhardenSSH}
	cases := []struct {
		name    string
		fields  map[string]any
		actions []string
		want    error
	}{
		{"RouterOS", map[string]any{"os_id": domain.OSIDRouterOS}, append([]string{services.PreviewFirewallApply, services.PreviewAptCacheOff}, ssh...), services.ErrRouterOSUnsupported},
		{"DSM", map[string]any{"os_id": domain.OSIDSynologyDSM}, append([]string{services.PreviewFirewallDisable, services.PreviewAptCacheOff}, ssh...), services.ErrDSMUnsupported},
		{"Agent", map[string]any{"transport": domain.TransportAgent}, ssh, services.ErrAgentTransport},
		{"Proxmox", map[string]any{"proxmox_type": "pve"}, []string{services.PreviewFirewallApply, services.PreviewFirewallDisable}, services.ErrProxmoxRestricted},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newTestEnv(t)
			id := joinTestServer(t, env, "web01")
			setServerFields(t, env, id, tc.fields)
			for _, action := range tc.actions {
				_, err := env.Servers.PreviewAction(scope, id, action, services.PreviewArgs{Port: 2222})
				if !errors.Is(err, tc.want) {
					t.Errorf("%s: %v, erwartet %v", action, err, tc.want)
				}
			}
		})
	}
}

func TestPreviewSSHAndFirewallDetails(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	scope := repositories.ScopeAll()

	if _, err := env.Servers.PreviewAction(scope, id, services.PreviewSSHPort, services.PreviewArgs{Port: 70000}); err == nil {
		t.Error("ungültiger Port ohne Fehler")
	}
	same := previewOf(t, env, id, services.PreviewSSHPort, services.PreviewArgs{Port: 22})
	if len(same.Commands) != 0 || !hasNote(same, "preview.ssh_port_unchanged") {
		t.Errorf("gleicher Port: %+v", same)
	}
	if _, err := env.Servers.PreviewAction(scope, id, services.PreviewFirewallApply, services.PreviewArgs{FirewallRules: `[{"port":99999}]`}); !errors.Is(err, services.ErrInvalidFirewallRules) {
		t.Errorf("ungültige Regel: %v", err)
	}

	fw := previewOf(t, env, id, services.PreviewFirewallApply, services.PreviewArgs{FirewallRules: `[{"port":443,"proto":"tcp"}]`})
	if !hasNote(fw, "preview.firewall_default_deny") || !fw.Reversible {
		t.Errorf("Firewall-Hinweise: %+v", fw.Notes)
	}
	for _, n := range fw.Notes {
		if n.Code == "preview.firewall_default_deny" && n.Params["ports"] != "22,443" {
			t.Errorf("freie Ports: %v", n.Params["ports"])
		}
	}

	port := previewOf(t, env, id, services.PreviewSSHPort, services.PreviewArgs{Port: 2222})
	if !hasNote(port, "preview.ssh_port_reconnect") || hasNote(port, "preview.ssh_port_firewall") {
		t.Errorf("Portwechsel ohne Firewall: %+v", port.Notes)
	}
	if !strings.Contains(port.Commands[0].Run, "Port 22\nPort 2222") {
		t.Errorf("Übergang ohne beide Ports:\n%s", port.Commands[0].Run)
	}

	setServerFields(t, env, id, map[string]any{"restricted_sudo": true})
	harden := previewOf(t, env, id, services.PreviewHardenSSH, services.PreviewArgs{})
	if len(harden.Commands) != 1 || !strings.Contains(harden.Commands[0].Run, "lcm-helper ssh-harden 'on'") {
		t.Errorf("Härtung im eingeschränkten Modus: %+v", harden.Commands)
	}
}

// toolInstalled lässt die Zustandsabfrage beide Werkzeuge als installiert melden.
func toolInstalled(env *testEnv) {
	env.Dialer.Responses["command -v fail2ban-client"] = sshx.FakeResponse{Output: "yes\n"}
	env.Dialer.Responses["command -v cscli"] = sshx.FakeResponse{Output: "yes\n"}
}

// TestPreviewMatchesRunSecurityTools: Einrichtung, Verwaltung und SSH-2FA.
func TestPreviewMatchesRunSecurityTools(t *testing.T) {
	scope := repositories.ScopeAll()
	fail2ban := services.SecurityToolInput{Tool: "fail2ban", AllowlistIPs: []string{"198.51.100.7"}}
	crowdsec := services.SecurityToolInput{Tool: "crowdsec", Bouncer: true}
	manage := func(action string) services.SecurityToolManageInput {
		return services.SecurityToolManageInput{Tool: "fail2ban", Action: action, AllowlistIPs: []string{"198.51.100.7"}}
	}
	cases := []struct {
		name   string
		action string
		args   services.PreviewArgs
		run    func(*testEnv, uint) (*domain.Job, error)
	}{
		{"fail2ban", services.PreviewSecurityTool, services.PreviewArgs{SecurityTool: fail2ban},
			func(e *testEnv, id uint) (*domain.Job, error) {
				return e.Servers.ConfigureSecurityTool(scope, id, fail2ban, "admin")
			}},
		{"crowdsec", services.PreviewSecurityTool, services.PreviewArgs{SecurityTool: crowdsec},
			func(e *testEnv, id uint) (*domain.Job, error) {
				return e.Servers.ConfigureSecurityTool(scope, id, crowdsec, "admin")
			}},
		{"entfernen", services.PreviewSecurityManage, services.PreviewArgs{SecurityTool: services.SecurityToolInput{Tool: "fail2ban"}, ToolAction: "uninstall"},
			func(e *testEnv, id uint) (*domain.Job, error) {
				return e.Servers.ManageSecurityTool(scope, id, manage("uninstall"), "admin")
			}},
		{"stoppen", services.PreviewSecurityManage, services.PreviewArgs{SecurityTool: services.SecurityToolInput{Tool: "fail2ban"}, ToolAction: "stop"},
			func(e *testEnv, id uint) (*domain.Job, error) {
				return e.Servers.ManageSecurityTool(scope, id, manage("stop"), "admin")
			}},
		{"allowlist", services.PreviewSecurityManage, services.PreviewArgs{SecurityTool: fail2ban, ToolAction: "allowlist"},
			func(e *testEnv, id uint) (*domain.Job, error) {
				return e.Servers.ManageSecurityTool(scope, id, manage("allowlist"), "admin")
			}},
		{"2fa an", services.PreviewSSH2FAEnable, services.PreviewArgs{},
			func(e *testEnv, id uint) (*domain.Job, error) {
				return e.Servers.ConfigureSSH2FA(scope, id, true, "admin")
			}},
		{"2fa aus", services.PreviewSSH2FADisable, services.PreviewArgs{},
			func(e *testEnv, id uint) (*domain.Job, error) {
				return e.Servers.ConfigureSSH2FA(scope, id, false, "admin")
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newTestEnv(t)
			id := joinTestServer(t, env, "web01")
			setServerFields(t, env, id, map[string]any{"lcm_source_ip": "192.0.2.10"})
			toolInstalled(env)
			assertActionMatchesPreview(t, env, id, tc.action, tc.args, false, func() (*domain.Job, error) { return tc.run(env, id) })
		})
	}
}

// TestPreviewSecurityToolMasksLapiSecrets: Das Passwort der Remote-LAPI geht
// an den Server, steht aber nicht in der Vorschau.
func TestPreviewSecurityToolMasksLapiSecrets(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	env.Servers.WithCrowdSecConfig(func(uint) (services.CrowdSecConfig, error) {
		return services.CrowdSecConfig{LapiURL: "https://lapi.test", LapiLogin: "lcm", LapiPassword: "hunter2geheim"}, nil
	})
	in := services.SecurityToolInput{Tool: "crowdsec", LapiMode: "remote"}
	p := assertActionMatchesPreview(t, env, id, services.PreviewSecurityTool, services.PreviewArgs{SecurityTool: in}, false,
		func() (*domain.Job, error) {
			return env.Servers.ConfigureSecurityTool(repositories.ScopeAll(), id, in, "admin")
		})
	shown := ""
	for _, c := range p.Commands {
		shown += c.Run
	}
	if strings.Contains(shown, "aHVudGVyMmdlaGVpbQ") || strings.Contains(shown, "hunter2geheim") {
		t.Errorf("LAPI-Passwort in der Vorschau:\n%s", shown)
	}
	if !hasNote(p, "preview.secrets_masked") {
		t.Errorf("Hinweis preview.secrets_masked fehlt: %+v", p.Notes)
	}
}

// TestPreviewSecurityToolsRefusedLikeRun: dieselben Sperren wie im Lauf.
func TestPreviewSecurityToolsRefusedLikeRun(t *testing.T) {
	scope := repositories.ScopeAll()
	args := services.PreviewArgs{SecurityTool: services.SecurityToolInput{Tool: "fail2ban"}, ToolAction: "restart"}
	actions := []string{services.PreviewSecurityTool, services.PreviewSecurityManage, services.PreviewSSH2FAEnable, services.PreviewSSH2FADisable}
	cases := []struct {
		name   string
		fields map[string]any
		want   error
	}{
		{"RouterOS", map[string]any{"os_id": domain.OSIDRouterOS}, services.ErrRouterOSUnsupported},
		{"DSM", map[string]any{"os_id": domain.OSIDSynologyDSM}, services.ErrDSMUnsupported},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newTestEnv(t)
			id := joinTestServer(t, env, "web01")
			setServerFields(t, env, id, tc.fields)
			for _, action := range actions {
				if _, err := env.Servers.PreviewAction(scope, id, action, args); !errors.Is(err, tc.want) {
					t.Errorf("%s: %v, erwartet %v", action, err, tc.want)
				}
			}
		})
	}

	// Eingeschränkt: Einrichten und Verwalten der Sicherheits-Tools laufen über
	// den Helper; gesperrt bleibt nur die SSH-2FA (Eingriff in PAM und sshd).
	t.Run("eingeschränkt", func(t *testing.T) {
		env := newTestEnv(t)
		id := joinTestServer(t, env, "web01")
		setServerFields(t, env, id, map[string]any{"restricted_sudo": true})
		for _, action := range actions {
			_, err := env.Servers.PreviewAction(scope, id, action, args)
			ssh2fa := action == services.PreviewSSH2FAEnable || action == services.PreviewSSH2FADisable
			if ssh2fa && !errors.Is(err, services.ErrRestrictedSudo) {
				t.Errorf("%s: %v, erwartet %v", action, err, services.ErrRestrictedSudo)
			}
			if !ssh2fa && err != nil {
				t.Errorf("%s im eingeschränkten Modus: %v", action, err)
			}
		}
	})

	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	if _, err := env.Servers.PreviewAction(scope, id, services.PreviewSecurityTool, services.PreviewArgs{SecurityTool: services.SecurityToolInput{Tool: "snort"}}); !errors.Is(err, services.ErrUnknownSecurityTool) {
		t.Errorf("unbekanntes Werkzeug: %v", err)
	}
	if _, err := env.Servers.PreviewAction(scope, id, services.PreviewSecurityManage, services.PreviewArgs{SecurityTool: services.SecurityToolInput{Tool: "fail2ban"}, ToolAction: "unban", UnbanIP: "kein-ip"}); !errors.Is(err, services.ErrUnbanIPMissing) {
		t.Errorf("Entsperren ohne IP: %v", err)
	}
}

// TestPreviewMatchesRunReposAndAptCache: Paketquellen und APT-Cache.
func TestPreviewMatchesRunReposAndAptCache(t *testing.T) {
	scope := repositories.ScopeAll()
	const revert = "https://deb.debian.org/debian"
	cases := []struct {
		name   string
		action string
		args   services.PreviewArgs
		run    func(*testEnv, uint) (*domain.Job, error)
	}{
		{"https", services.PreviewReposSecure, services.PreviewArgs{},
			func(e *testEnv, id uint) (*domain.Job, error) {
				return syncRun(func() (string, error) { return e.Servers.SecureRepositories(scope, id, "admin") })()
			}},
		{"zurück auf http", services.PreviewReposRevert, services.PreviewArgs{URIs: []string{revert}},
			func(e *testEnv, id uint) (*domain.Job, error) {
				return syncRun(func() (string, error) {
					return e.Servers.RevertRepositoriesHTTPS(scope, id, []string{revert}, "admin")
				})()
			}},
		{"quelle hinzufügen", services.PreviewReposAdd, services.PreviewArgs{RepoKey: "docker"},
			func(e *testEnv, id uint) (*domain.Job, error) {
				return syncRun(func() (string, error) { return e.Servers.AddKnownRepository(scope, id, "docker", "admin") })()
			}},
		{"cache anbinden", services.PreviewAptCacheOn, services.PreviewArgs{},
			func(e *testEnv, id uint) (*domain.Job, error) {
				return syncRun(func() (string, error) { return e.Servers.ConfigureAptProxy(scope, id, true, 0, "admin") })()
			}},
		{"cache trennen", services.PreviewAptCacheOff, services.PreviewArgs{},
			func(e *testEnv, id uint) (*domain.Job, error) {
				return syncRun(func() (string, error) { return e.Servers.ConfigureAptProxy(scope, id, false, 0, "admin") })()
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newTestEnv(t)
			id := joinTestServer(t, env, "web01")
			setServerFields(t, env, id, map[string]any{"https_revert_urls": revert})
			addAptCache(t, env, "Standort A", "http://cache.local:3142")
			assertActionMatchesPreview(t, env, id, tc.action, tc.args, false, func() (*domain.Job, error) { return tc.run(env, id) })
		})
	}
}

func TestPreviewReposAndAptCacheRefused(t *testing.T) {
	scope := repositories.ScopeAll()
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	preview := func(action string, args services.PreviewArgs) error {
		_, err := env.Servers.PreviewAction(scope, id, action, args)
		return err
	}
	if err := preview(services.PreviewReposAdd, services.PreviewArgs{RepoKey: "gibtsnicht"}); !errors.Is(err, services.ErrUnknownRepo) {
		t.Errorf("unbekannte Quelle: %v", err)
	}
	if err := preview(services.PreviewReposRevert, services.PreviewArgs{}); !errors.Is(err, services.ErrNoRevertCandidates) {
		t.Errorf("ohne Kandidaten: %v", err)
	}
	if err := preview(services.PreviewAptCacheOn, services.PreviewArgs{}); !errors.Is(err, services.ErrNoAptCacheURL) {
		t.Errorf("ohne Cache: %v", err)
	}
	setPackageManager(t, env, id, "dnf")
	if err := preview(services.PreviewReposSecure, services.PreviewArgs{}); !errors.Is(err, services.ErrRepoPackageManagerMismatch) {
		t.Errorf("https auf dnf: %v", err)
	}
	setServerFields(t, env, id, map[string]any{"os_id": domain.OSIDRouterOS})
	for _, action := range []string{services.PreviewReposSecure, services.PreviewReposRevert} {
		if err := preview(action, services.PreviewArgs{}); !errors.Is(err, services.ErrRouterOSUnsupported) {
			t.Errorf("%s auf RouterOS: %v", action, err)
		}
	}
	if err := preview(services.PreviewReposAdd, services.PreviewArgs{RepoKey: "docker"}); !errors.Is(err, services.ErrRouterOSUnsupported) {
		t.Errorf("Quelle auf RouterOS: %v", err)
	}
}

// TestPreviewMatchesRunSettingsAndTools: DNS, Zeit, Pins, Werkzeuge und
// abgeschottete Verzeichnisse.
func TestPreviewMatchesRunSettingsAndTools(t *testing.T) {
	scope := repositories.ScopeAll()
	target := services.HardenTarget{Path: "/srv/data", Group: "www-data"}
	cases := []struct {
		name   string
		action string
		args   services.PreviewArgs
		run    func(*testEnv, uint) (*domain.Job, error)
	}{
		{"dns", services.PreviewDNS, services.PreviewArgs{Servers: []string{"1.1.1.1", "9.9.9.9"}},
			func(e *testEnv, id uint) (*domain.Job, error) {
				return syncRun(func() (string, error) {
					return e.Servers.ConfigureDNS(scope, id, []string{"1.1.1.1", "9.9.9.9"}, "admin")
				})()
			}},
		{"dns freigeben", services.PreviewDNS, services.PreviewArgs{},
			func(e *testEnv, id uint) (*domain.Job, error) {
				return syncRun(func() (string, error) { return e.Servers.ConfigureDNS(scope, id, nil, "admin") })()
			}},
		{"zeitzone", services.PreviewTimezone, services.PreviewArgs{Timezone: "Europe/Berlin"},
			func(e *testEnv, id uint) (*domain.Job, error) {
				return syncRun(func() (string, error) { return e.Servers.SetTimezone(scope, id, "Europe/Berlin", "admin") })()
			}},
		{"ntp", services.PreviewNTP, services.PreviewArgs{Servers: []string{"ptbtime1.ptb.de"}},
			func(e *testEnv, id uint) (*domain.Job, error) {
				return syncRun(func() (string, error) { return e.Servers.ConfigureNTP(scope, id, []string{"ptbtime1.ptb.de"}, "admin") })()
			}},
		{"pins", services.PreviewPinsApply, services.PreviewArgs{},
			func(e *testEnv, id uint) (*domain.Job, error) { return e.Servers.ApplyPackagePins(scope, id, "admin") }},
		{"deep-scan-tools", services.PreviewDeepScanTools, services.PreviewArgs{},
			func(e *testEnv, id uint) (*domain.Job, error) {
				return e.Servers.InstallDeepScanTools(scope, id, "admin")
			}},
		{"acl", services.PreviewACLInstall, services.PreviewArgs{},
			func(e *testEnv, id uint) (*domain.Job, error) { return e.Servers.InstallACLSupport(scope, id, "admin") }},
		{"abschotten", services.PreviewPathHarden, services.PreviewArgs{Paths: []services.HardenTarget{target}},
			func(e *testEnv, id uint) (*domain.Job, error) {
				_, err := e.Servers.HardenPathsBulk(scope, id, []services.HardenTarget{target}, "admin")
				return nil, err
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newTestEnv(t)
			env.Servers.WithPackagePins(repositories.NewPackagePinRepository(env.DB()))
			id := joinTestServer(t, env, "web01")
			assertActionMatchesPreview(t, env, id, tc.action, tc.args, false, func() (*domain.Job, error) { return tc.run(env, id) })
		})
	}
}

func TestPreviewMatchesRunRestorePath(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	scope := repositories.ScopeAll()
	env.Dialer.Responses["LCM-VORHER"] = sshx.FakeResponse{Output: "LCM-VORHER: 755 root\nLCM-NACHHER: 750 www-data\n"}
	hardened, err := env.Servers.HardenPath(scope, id, "/srv/data", "www-data", "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	p := assertActionMatchesPreview(t, env, id, services.PreviewPathRestore, services.PreviewArgs{PathID: hardened.ID}, false,
		func() (*domain.Job, error) { return nil, env.Servers.RestorePath(scope, id, hardened.ID, "admin") })
	if !hasNote(p, "preview.path_restore") {
		t.Errorf("Hinweis fehlt: %+v", p.Notes)
	}
}

// TestPreviewMatchesRunLcmHost: Einrichtungen auf dem LCM-Host.
func TestPreviewMatchesRunLcmHost(t *testing.T) {
	scope := repositories.ScopeAll()
	cases := []struct {
		action string
		args   services.PreviewArgs
		run    func(*testEnv, uint) (*domain.Job, error)
	}{
		{services.PreviewHostTrivy, services.PreviewArgs{}, func(e *testEnv, id uint) (*domain.Job, error) { return e.Servers.InstallTrivy(scope, id, "admin") }},
		{services.PreviewHostSandbox, services.PreviewArgs{}, func(e *testEnv, id uint) (*domain.Job, error) { return e.Servers.InstallSandbox(scope, id, "admin") }},
		{services.PreviewHostAptCacher, services.PreviewArgs{}, func(e *testEnv, id uint) (*domain.Job, error) { return e.Servers.InstallAptCacher(scope, id, "admin") }},
		{services.PreviewHostCrowdSec, services.PreviewArgs{Bouncer: true}, func(e *testEnv, id uint) (*domain.Job, error) {
			return e.Servers.InstallCrowdSecLapi(scope, id, services.LapiInstallInput{Bouncer: true}, "admin")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.action, func(t *testing.T) {
			env := newTestEnv(t)
			id := joinTestServer(t, env, "lcm")
			setServerFields(t, env, id, map[string]any{"host": "localhost", "package_manager": "apt"})
			assertActionMatchesPreview(t, env, id, tc.action, tc.args, false, func() (*domain.Job, error) { return tc.run(env, id) })
		})
	}

	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	if _, err := env.Servers.PreviewAction(scope, id, services.PreviewHostTrivy, services.PreviewArgs{}); !errors.Is(err, services.ErrNotLcmHost) {
		t.Errorf("fremder Server: %v", err)
	}
}

func TestPreviewSettingsRefused(t *testing.T) {
	scope := repositories.ScopeAll()
	env := newTestEnv(t)
	env.Servers.WithPackagePins(repositories.NewPackagePinRepository(env.DB()))
	id := joinTestServer(t, env, "web01")
	preview := func(action string, args services.PreviewArgs) error {
		_, err := env.Servers.PreviewAction(scope, id, action, args)
		return err
	}
	if err := preview(services.PreviewTimezone, services.PreviewArgs{Timezone: "Europe/Berlin; reboot"}); !errors.Is(err, services.ErrInvalidTimezone) {
		t.Errorf("ungültige Zeitzone: %v", err)
	}
	if err := preview(services.PreviewNTP, services.PreviewArgs{}); !errors.Is(err, services.ErrInvalidNTPServer) {
		t.Errorf("NTP ohne Server: %v", err)
	}
	if err := preview(services.PreviewDNS, services.PreviewArgs{Servers: []string{"kein-ip"}}); !errors.Is(err, services.ErrInvalidDNSServer) {
		t.Errorf("ungültiger DNS-Server: %v", err)
	}
	setServerFields(t, env, id, map[string]any{"virtualization": "lxc"})
	if err := preview(services.PreviewNTP, services.PreviewArgs{Servers: []string{"ptbtime1.ptb.de"}}); !errors.Is(err, services.ErrClockFromHost) {
		t.Errorf("NTP im Container: %v", err)
	}
	setServerFields(t, env, id, map[string]any{"restricted_sudo": true})
	if err := preview(services.PreviewPinsApply, services.PreviewArgs{}); err != nil {
		t.Errorf("Pins im eingeschränkten Modus gehen über den Helper: %v", err)
	}
	setServerFields(t, env, id, map[string]any{"os_id": domain.OSIDRouterOS})
	for _, action := range []string{services.PreviewDNS, services.PreviewTimezone, services.PreviewNTP, services.PreviewPinsApply,
		services.PreviewDeepScanTools, services.PreviewACLInstall} {
		if err := preview(action, services.PreviewArgs{Timezone: "UTC", Servers: []string{"1.1.1.1"}}); !errors.Is(err, services.ErrRouterOSUnsupported) {
			t.Errorf("%s auf RouterOS: %v", action, err)
		}
	}
	if err := preview(services.PreviewPathHarden, services.PreviewArgs{Paths: []services.HardenTarget{{Path: "/srv"}}}); !errors.Is(err, services.ErrRouterOSUnsupported) {
		t.Errorf("Abschotten auf RouterOS: %v", err)
	}
}

// TestPreviewMatchesRunDocker: Docker-Aktionen aus dem gescannten Inventar.
func TestPreviewMatchesRunDocker(t *testing.T) {
	scope := repositories.ScopeAll()
	cases := []struct {
		name   string
		action string
		args   services.PreviewArgs
		setup  func(*testing.T, *testEnv, uint)
		run    func(*testEnv, uint) (*domain.Job, error)
	}{
		{"compose", services.PreviewDockerCompose, services.PreviewArgs{Project: "webshop"}, nil,
			func(e *testEnv, id uint) (*domain.Job, error) {
				return e.Servers.UpdateComposeProject(scope, id, "webshop", "", "admin")
			}},
		{"pull", services.PreviewDockerPull, services.PreviewArgs{Image: "nginx:1.25"}, nil,
			func(e *testEnv, id uint) (*domain.Job, error) {
				return e.Servers.PullDockerImage(scope, id, "nginx:1.25", "admin")
			}},
		{"pull-all", services.PreviewDockerPullAll, services.PreviewArgs{}, nil,
			func(e *testEnv, id uint) (*domain.Job, error) {
				return e.Servers.PullAllDockerImages(scope, id, "admin")
			}},
		{"image löschen", services.PreviewDockerRemove, services.PreviewArgs{Image: "nginx:1.25"},
			func(t *testing.T, e *testEnv, id uint) {
				if err := e.DB().Model(&domain.DockerImage{}).Where("repository = ?", "nginx").Update("in_use", false).Error; err != nil {
					t.Fatal(err)
				}
			},
			func(e *testEnv, id uint) (*domain.Job, error) {
				return e.Servers.DeleteDockerImage(scope, id, "nginx:1.25", "admin")
			}},
		{"aufräumen", services.PreviewDockerPrune, services.PreviewArgs{}, nil,
			func(e *testEnv, id uint) (*domain.Job, error) { return e.Servers.PruneDockerImages(scope, id, "admin") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newTestEnv(t)
			id := joinDockerServer(t, env, "docker01")
			if tc.setup != nil {
				tc.setup(t, env, id)
			}
			assertActionMatchesPreview(t, env, id, tc.action, tc.args, false, func() (*domain.Job, error) { return tc.run(env, id) })
		})
	}
}

func TestPreviewDockerRefusedLikeRun(t *testing.T) {
	scope := repositories.ScopeAll()
	env := newTestEnv(t)
	id := joinDockerServer(t, env, "docker01")
	preview := func(action string, args services.PreviewArgs) error {
		_, err := env.Servers.PreviewAction(scope, id, action, args)
		return err
	}
	if err := preview(services.PreviewDockerPull, services.PreviewArgs{Image: "boesewicht/backdoor:1"}); !errors.Is(err, services.ErrDockerImageUnknown) {
		t.Errorf("fremdes Image: %v", err)
	}
	if err := preview(services.PreviewDockerPull, services.PreviewArgs{Image: "nginx:1.25'; reboot"}); !errors.Is(err, services.ErrInvalidDockerRef) {
		t.Errorf("ungültige Referenz: %v", err)
	}
	if err := preview(services.PreviewDockerRemove, services.PreviewArgs{Image: "nginx:1.25"}); !errors.Is(err, services.ErrDockerImageInUse) {
		t.Errorf("genutztes Image: %v", err)
	}
	if err := preview(services.PreviewDockerCompose, services.PreviewArgs{Project: "gibtsnicht"}); !errors.Is(err, services.ErrComposeProjectUnknown) {
		t.Errorf("fremdes Projekt: %v", err)
	}
	setServerFields(t, env, id, map[string]any{"docker_updates_disabled": true})
	if err := preview(services.PreviewDockerPullAll, services.PreviewArgs{}); !errors.Is(err, services.ErrDockerUpdatesDisabled) {
		t.Errorf("Updates gesperrt: %v", err)
	}
}

// TestPreviewMatchesRunServerUsers: Sperren, Freigeben und Entfernen eines
// gescannten Kontos.
func TestPreviewMatchesRunServerUsers(t *testing.T) {
	cases := []struct {
		action string
		run    func(*testEnv, *domain.Server) (string, error)
	}{
		{services.PreviewUserLock, func(e *testEnv, s *domain.Server) (string, error) {
			return e.Prov.SetServerUserDisabled(s, "alice", true, "admin")
		}},
		{services.PreviewUserUnlock, func(e *testEnv, s *domain.Server) (string, error) {
			return e.Prov.SetServerUserDisabled(s, "alice", false, "admin")
		}},
		{services.PreviewUserRemove, func(e *testEnv, s *domain.Server) (string, error) {
			return e.Prov.RemoveServerUser(s, "alice", "admin")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.action, func(t *testing.T) {
			env := newTestEnv(t)
			id := joinTestServer(t, env, "web01")
			server, _ := env.Servers.Get(repositories.ScopeAll(), id)
			assertActionMatchesPreview(t, env, id, tc.action, services.PreviewArgs{Username: "alice"}, false,
				syncRun(func() (string, error) { return tc.run(env, server) }))
		})
	}

	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	for _, name := range []string{"root", "bad name"} {
		if _, err := env.Servers.PreviewAction(repositories.ScopeAll(), id, services.PreviewUserRemove, services.PreviewArgs{Username: name}); err == nil {
			t.Errorf("%q ohne Fehler", name)
		}
	}
	setServerFields(t, env, id, map[string]any{"os_id": domain.OSIDRouterOS})
	if _, err := env.Servers.PreviewAction(repositories.ScopeAll(), id, services.PreviewUserLock, services.PreviewArgs{Username: "alice"}); !errors.Is(err, services.ErrRouterOSUnsupported) {
		t.Errorf("RouterOS: %v", err)
	}
}
