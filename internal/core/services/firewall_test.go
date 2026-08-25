package services_test

import (
	"regexp"
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/storage/repositories"
)

// waitForJob pollt einen asynchronen Job bis zum Endzustand und liefert ihn.
func waitForJob(t *testing.T, env *testEnv, jobID string) *domain.Job {
	t.Helper()
	var job domain.Job
	waitFor(t, func() bool {
		if err := env.DB().Where("id = ?", jobID).First(&job).Error; err != nil {
			return false
		}
		return job.Status == domain.JobStatusSuccess || job.Status == domain.JobStatusFailed
	})
	return &job
}

// TestConfigureFirewallEnableWithPorts prüft den asynchronen Firewall-Job:
// SSH plus die konfigurierten Ports werden freigegeben (Legacy-CSV-Eingabe)
// und die Konfiguration (JSON + CSV + Werkzeug) wird am Server persistiert.
func TestConfigureFirewallEnableWithPorts(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	// Erkennung meldet ufw, der Status eine aktive Firewall.
	env.Dialer.Responses["then echo ufw"] = sshx.FakeResponse{Output: "ufw\n"}
	env.Dialer.Responses["ufw status verbose"] = sshx.FakeResponse{Output: "Status: active\n"}

	env.Dialer.Commands = nil
	job, err := env.Servers.ConfigureFirewall(repositories.ScopeAll(), id, true, "80, 443, 443, abc, 70000", domain.FirewallSSHSources{}, "admin")
	if err != nil {
		t.Fatalf("firewall aktivieren: %v", err)
	}
	done := waitForJob(t, env, job.ID)
	if done.Status != domain.JobStatusSuccess {
		t.Fatalf("job fehlgeschlagen: %s", done.Output)
	}
	all := strings.Join(env.Dialer.Commands, "\n")
	for _, want := range []string{
		"ufw allow proto tcp to any port 22",
		"ufw allow proto tcp to any port 80",
		"ufw allow proto tcp to any port 443",
		"ufw --force enable", "ufw default deny incoming",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("firewall-skript enthielt %q nicht:\n%s", want, all)
		}
	}
	// Ungültige/doppelte Ports sind verworfen (nachsichtige Legacy-CSV).
	if strings.Contains(all, "port 70000") || strings.Contains(all, "abc") {
		t.Errorf("ungültige ports nicht gefiltert:\n%s", all)
	}

	server, _ := env.Servers.Get(repositories.ScopeAll(), id)
	if !server.FirewallActive {
		t.Error("firewall_active nicht gesetzt")
	}
	if server.FirewallAllowedPorts != "80,443" {
		t.Errorf("persistierte ports falsch: %q", server.FirewallAllowedPorts)
	}
	if !strings.Contains(server.FirewallRules, `"port":80`) || !strings.Contains(server.FirewallRules, `"port":443`) {
		t.Errorf("persistierte regeln falsch: %q", server.FirewallRules)
	}
	if server.FirewallTool != domain.FirewallToolUfw {
		t.Errorf("firewall_tool falsch: %q", server.FirewallTool)
	}
}

// TestConfigureFirewallRichRules: JSON-Regeln mit Protokoll, Bind und
// IP-Version landen im Skript und in der persistierten Konfiguration.
func TestConfigureFirewallRichRules(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	env.Dialer.Responses["then echo ufw"] = sshx.FakeResponse{Output: "ufw\n"}
	env.Dialer.Responses["ufw status verbose"] = sshx.FakeResponse{Output: "Status: active\n"}

	env.Dialer.Commands = nil
	spec := `[{"port":53,"proto":"udp","ip_version":"v4"},{"port":5432,"proto":"tcp","source_ips":["10.0.0.5"],"comment":"Datenbank"}]`
	job, err := env.Servers.ConfigureFirewall(repositories.ScopeAll(), id, true, spec, domain.FirewallSSHSources{}, "admin")
	if err != nil {
		t.Fatalf("rich rules: %v", err)
	}
	if done := waitForJob(t, env, job.ID); done.Status != domain.JobStatusSuccess {
		t.Fatalf("job fehlgeschlagen: %s", done.Output)
	}
	all := strings.Join(env.Dialer.Commands, "\n")
	for _, want := range []string{
		"ufw allow proto udp to 0.0.0.0/0 port 53",
		// Die Bemerkung landet im Kommentar der ufw-Regel (die Quotes sind hier
		// durch die sudo-Verpackung escaped, deshalb nur der Kern).
		"ufw allow proto tcp from 10.0.0.5 to any port 5432 comment ",
		"lcm: Datenbank",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("skript enthielt %q nicht:\n%s", want, all)
		}
	}
	server, _ := env.Servers.Get(repositories.ScopeAll(), id)
	if !strings.Contains(server.FirewallRules, `"source_ips":["10.0.0.5"]`) || !strings.Contains(server.FirewallRules, `"proto":"udp"`) {
		t.Errorf("regeln nicht persistiert: %q", server.FirewallRules)
	}
	// Die Bemerkung gehört zur Regel und muss den Weg durch Job und Speicher
	// überstehen - sie ging in der Oberfläche schon einmal verloren.
	if !strings.Contains(server.FirewallRules, `"comment":"Datenbank"`) {
		t.Errorf("bemerkung nicht persistiert: %q", server.FirewallRules)
	}
	// Ungültige Regeln werden VOR dem Job abgelehnt (400er-Pfad).
	if _, err := env.Servers.ConfigureFirewall(repositories.ScopeAll(), id, true, `[{"port":80,"proto":"icmp"}]`, domain.FirewallSSHSources{}, "admin"); err == nil {
		t.Error("ungültiges protokoll muss abgelehnt werden")
	}
}

// TestConfigureFirewallSSHSources: die SSH-Freigabe lässt sich über die
// erlaubten Quellen einschränken; der SSH-Port bleibt offen (nicht löschbar),
// nur die zugelassenen Absender werden eingeengt. Die Quellen werden am
// Server persistiert.
func TestConfigureFirewallSSHSources(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	env.Dialer.Responses["then echo ufw"] = sshx.FakeResponse{Output: "ufw\n"}
	env.Dialer.Responses["ufw status verbose"] = sshx.FakeResponse{Output: "Status: active\n"}

	env.Dialer.Commands = nil
	src := domain.FirewallSSHSources{SourceIPs: []string{"10.0.0.5"}}
	job, err := env.Servers.ConfigureFirewall(repositories.ScopeAll(), id, true, "80", src, "admin")
	if err != nil {
		t.Fatalf("firewall mit ssh-quellen: %v", err)
	}
	if done := waitForJob(t, env, job.ID); done.Status != domain.JobStatusSuccess {
		t.Fatalf("job fehlgeschlagen: %s", done.Output)
	}
	all := strings.Join(env.Dialer.Commands, "\n")
	// SSH (Port 22) nur noch von dieser Quelle, aber weiterhin offen.
	if !strings.Contains(all, "ufw allow proto tcp from 10.0.0.5 to any port 22") {
		t.Errorf("SSH-freigabe nicht auf die quelle eingeschränkt:\n%s", all)
	}
	server, _ := env.Servers.Get(repositories.ScopeAll(), id)
	if !strings.Contains(server.FirewallSSHSources, "10.0.0.5") {
		t.Errorf("ssh-quellen nicht persistiert: %q", server.FirewallSSHSources)
	}

	// Ungültige Quelle wird VOR dem Job abgelehnt (400er-Pfad).
	bad := domain.FirewallSSHSources{SourceIPs: []string{"kein-ip"}}
	if _, err := env.Servers.ConfigureFirewall(repositories.ScopeAll(), id, true, "80", bad, "admin"); err == nil {
		t.Error("ungültige ssh-quelle muss abgelehnt werden")
	}

	// Ohne Quellen ist SSH wieder für alle offen.
	env.Dialer.Commands = nil
	job, err = env.Servers.ConfigureFirewall(repositories.ScopeAll(), id, true, "80", domain.FirewallSSHSources{}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if done := waitForJob(t, env, job.ID); done.Status != domain.JobStatusSuccess {
		t.Fatalf("job fehlgeschlagen: %s", done.Output)
	}
	if !strings.Contains(strings.Join(env.Dialer.Commands, "\n"), "ufw allow proto tcp to any port 22") {
		t.Errorf("SSH ohne quellen nicht wieder für alle geöffnet:\n%s", strings.Join(env.Dialer.Commands, "\n"))
	}
}

// TestConfigureFirewallDisable deaktiviert die Firewall; die Regel-
// Konfiguration bleibt erhalten.
func TestConfigureFirewallDisable(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	env.Dialer.Responses["then echo ufw"] = sshx.FakeResponse{Output: "ufw\n"}
	env.Dialer.Responses["ufw status verbose"] = sshx.FakeResponse{Output: "Status: active\n"}
	job, err := env.Servers.ConfigureFirewall(repositories.ScopeAll(), id, true, "8080", domain.FirewallSSHSources{}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if done := waitForJob(t, env, job.ID); done.Status != domain.JobStatusSuccess {
		t.Fatalf("aktivieren fehlgeschlagen: %s", done.Output)
	}

	env.Dialer.Commands = nil
	job, err = env.Servers.ConfigureFirewall(repositories.ScopeAll(), id, false, "", domain.FirewallSSHSources{}, "admin")
	if err != nil {
		t.Fatalf("firewall deaktivieren: %v", err)
	}
	if done := waitForJob(t, env, job.ID); done.Status != domain.JobStatusSuccess {
		t.Fatalf("deaktivieren fehlgeschlagen: %s", done.Output)
	}
	if !strings.Contains(strings.Join(env.Dialer.Commands, "\n"), "ufw --force disable") {
		t.Errorf("kein disable-kommando: %v", env.Dialer.Commands)
	}
	server, _ := env.Servers.Get(repositories.ScopeAll(), id)
	if server.FirewallActive {
		t.Error("firewall_active nach disable noch true")
	}
	// Die Port-Konfiguration bleibt beim Deaktivieren erhalten.
	if server.FirewallAllowedPorts != "8080" {
		t.Errorf("port-konfiguration beim disable verloren: %q", server.FirewallAllowedPorts)
	}
}

// TestConfigureFirewallInstallsMissingTool: Rocky ohne Firewall-Werkzeug →
// firewalld wird installiert und verwendet (vorgesehenes Backend der Distro).
func TestConfigureFirewallInstallsMissingTool(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "rocky01")
	if err := env.DB().Model(&domain.Server{}).Where("id = ?", id).
		Updates(map[string]any{"os_id": "rocky", "package_manager": "dnf", "firewall_tool": ""}).Error; err != nil {
		t.Fatal(err)
	}
	// Erkennung: kein Werkzeug vorhanden; firewalld meldet nach der
	// Installation "running".
	env.Dialer.Responses["then echo ufw"] = sshx.FakeResponse{Output: "none\n"}
	env.Dialer.Responses["firewall-cmd --state"] = sshx.FakeResponse{Output: "running\n"}

	env.Dialer.Commands = nil
	job, err := env.Servers.ConfigureFirewall(repositories.ScopeAll(), id, true, "80", domain.FirewallSSHSources{}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	done := waitForJob(t, env, job.ID)
	if done.Status != domain.JobStatusSuccess {
		t.Fatalf("job fehlgeschlagen: %s", done.Output)
	}
	all := strings.Join(env.Dialer.Commands, "\n")
	if !strings.Contains(all, "dnf install -y") || !strings.Contains(all, "firewalld") {
		t.Errorf("firewalld-installation fehlt:\n%s", all)
	}
	if !strings.Contains(all, `--add-port=22/tcp`) || !strings.Contains(all, `--add-port=80/tcp`) {
		t.Errorf("firewalld-freigaben fehlen:\n%s", all)
	}
	server, _ := env.Servers.Get(repositories.ScopeAll(), id)
	if server.FirewallTool != domain.FirewallToolFirewalld || !server.FirewallActive {
		t.Errorf("firewalld nicht persistiert: tool=%q active=%v", server.FirewallTool, server.FirewallActive)
	}
}

// TestConfigureFirewallUsesInstalledTool: auf Ubuntu ist bereits firewalld
// installiert → LCM verwendet es und installiert NICHT ufw daneben.
func TestConfigureFirewallUsesInstalledTool(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "ubuntu01")
	if err := env.DB().Model(&domain.Server{}).Where("id = ?", id).
		Updates(map[string]any{"os_id": "ubuntu"}).Error; err != nil {
		t.Fatal(err)
	}
	env.Dialer.Responses["then echo ufw"] = sshx.FakeResponse{Output: "firewalld\n"}
	env.Dialer.Responses["firewall-cmd --state"] = sshx.FakeResponse{Output: "running\n"}

	env.Dialer.Commands = nil
	job, err := env.Servers.ConfigureFirewall(repositories.ScopeAll(), id, true, "443", domain.FirewallSSHSources{}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	done := waitForJob(t, env, job.ID)
	if done.Status != domain.JobStatusSuccess {
		t.Fatalf("job fehlgeschlagen: %s", done.Output)
	}
	all := strings.Join(env.Dialer.Commands, "\n")
	if strings.Contains(all, "apt-get install") {
		t.Errorf("es darf keine zweite firewall installiert werden:\n%s", all)
	}
	if !strings.Contains(all, `--add-port=443/tcp`) {
		t.Errorf("firewalld nicht verwendet:\n%s", all)
	}
	if !strings.Contains(done.Output, "bereits installiert") {
		t.Errorf("konflikt-hinweis fehlt im job-output:\n%s", done.Output)
	}
	server, _ := env.Servers.Get(repositories.ScopeAll(), id)
	if server.FirewallTool != domain.FirewallToolFirewalld {
		t.Errorf("verwendetes werkzeug nicht persistiert: %q", server.FirewallTool)
	}
}

// TestConfigureFirewallRestrictedNonUfw: im eingeschränkten Modus ist nur
// ufw verwaltbar - nftables/firewalld werden ehrlich abgelehnt.
func TestConfigureFirewallRestrictedNonUfw(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "deb01")
	if err := env.DB().Model(&domain.Server{}).Where("id = ?", id).
		Updates(map[string]any{"os_id": "debian", "restricted_sudo": true, "firewall_tool": ""}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := env.Servers.ConfigureFirewall(repositories.ScopeAll(), id, true, "80", domain.FirewallSSHSources{}, "admin"); err == nil {
		t.Error("nftables im eingeschränkten modus muss abgelehnt werden")
	}
}

// TestFirewallGroupRuleAppliesToAllServers prüft, dass eine Firewall-
// Grundsatz-Regel (Enforce) beim direkten Auslösen auf alle Server der
// Gruppe angewendet wird - Legacy-CSV-Command inklusive.
func TestFirewallGroupRuleAppliesToAllServers(t *testing.T) {
	env := newTestEnv(t)
	id1 := joinTestServer(t, env, "web01")
	id2 := joinTestServer(t, env, "web02")
	env.Dialer.Responses["then echo ufw"] = sshx.FakeResponse{Output: "ufw\n"}
	env.Dialer.Responses["ufw status verbose"] = sshx.FakeResponse{Output: "Status: active\n"}

	// Gruppe "Webserver" mit beiden Servern.
	group, err := env.Groups.Create("Webserver", "Web-Frontends", nil, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Groups.AssignServer(repositories.ScopeAll(), group.ID, id1, "admin"); err != nil {
		t.Fatal(err)
	}
	if err := env.Groups.AssignServer(repositories.ScopeAll(), group.ID, id2, "admin"); err != nil {
		t.Fatal(err)
	}

	// Firewall-Grundsatz-Regel: gibt zusätzlich 80 und 443 frei (Legacy-CSV).
	rule, err := env.Groups.DefineRule(repositories.ScopeAll(), group.ID, "web-fw", domain.RuleTypeFirewall, "80,443", nil, true, "admin")
	if err != nil {
		t.Fatalf("firewall-rule definieren: %v", err)
	}

	env.Dialer.Commands = nil
	env.Executor.RunRule(rule, "admin")

	all := strings.Join(env.Dialer.Commands, "\n")
	if !strings.Contains(all, "ufw allow proto tcp to any port 80") || !strings.Contains(all, "ufw allow proto tcp to any port 443") {
		t.Errorf("firewall-regel nicht angewendet:\n%s", all)
	}
	// Beide Server müssen als firewall-aktiv markiert und konfiguriert sein.
	for _, id := range []uint{id1, id2} {
		srv, _ := env.Servers.Get(repositories.ScopeAll(), id)
		if !srv.FirewallActive || srv.FirewallAllowedPorts != "80,443" {
			t.Errorf("server %d nicht korrekt konfiguriert: active=%v ports=%q", id, srv.FirewallActive, srv.FirewallAllowedPorts)
		}
		if srv.FirewallTool != domain.FirewallToolUfw {
			t.Errorf("server %d: werkzeug nicht persistiert: %q", id, srv.FirewallTool)
		}
	}
}

// TestFirewallGroupRuleJSONCommand: eine Gruppen-Regel mit Rich-Rule-JSON
// (Protokoll/Bind/IP-Version) wird angewendet.
func TestFirewallGroupRuleJSONCommand(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	env.Dialer.Responses["then echo ufw"] = sshx.FakeResponse{Output: "ufw\n"}
	env.Dialer.Responses["ufw status verbose"] = sshx.FakeResponse{Output: "Status: active\n"}

	group, err := env.Groups.Create("Datenbanken", "", nil, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Groups.AssignServer(repositories.ScopeAll(), group.ID, id, "admin"); err != nil {
		t.Fatal(err)
	}
	rule, err := env.Groups.DefineRule(repositories.ScopeAll(), group.ID, "db-fw", domain.RuleTypeFirewall,
		`[{"port":5432,"proto":"tcp","source_ips":["10.0.0.5"]},{"port":53,"proto":"udp"}]`, nil, true, "admin")
	if err != nil {
		t.Fatal(err)
	}

	env.Dialer.Commands = nil
	env.Executor.RunRule(rule, "admin")

	all := strings.Join(env.Dialer.Commands, "\n")
	if !strings.Contains(all, "ufw allow proto tcp from 10.0.0.5 to any port 5432") || !strings.Contains(all, "ufw allow proto udp to any port 53") {
		t.Errorf("json-regel nicht angewendet:\n%s", all)
	}
	srv, _ := env.Servers.Get(repositories.ScopeAll(), id)
	if !strings.Contains(srv.FirewallRules, `"source_ips":["10.0.0.5"]`) || srv.FirewallAllowedPorts != "53,5432" {
		t.Errorf("json-regel nicht persistiert: rules=%q ports=%q", srv.FirewallRules, srv.FirewallAllowedPorts)
	}
}

// findSystemHealthRule liefert die geseedete Health-Check-Rule der
// System-Gruppe (der regelmäßige Ping).
func findSystemHealthRule(t *testing.T, env *testEnv) *domain.Rule {
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
			if rules[i].Type == domain.RuleTypeHealth {
				return &rules[i]
			}
		}
	}
	t.Fatal("system-health-rule nicht gefunden")
	return nil
}

// reLcmHash extrahiert den Regelsatz-Hash aus einem aufgezeichneten
// Enable-Kommando (comment 'lcm:<hash>').
var reLcmHash = regexp.MustCompile(`lcm:([0-9a-f]{12})`)

// TestEnforceRuleAppliedOnHealthPing ist der Kerntest der Grundsatz-Regeln:
// beim Health-Ping wird der Ist-Zustand der Firewall geprüft; bei
// Abweichung wird sie durchgesetzt, bei Übereinstimmung passiert NICHTS.
func TestEnforceRuleAppliedOnHealthPing(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")

	group, err := env.Groups.Create("Webserver", "Web-Frontends", nil, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Groups.AssignServer(repositories.ScopeAll(), group.ID, id, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.Groups.DefineRule(repositories.ScopeAll(), group.ID, "web-fw", domain.RuleTypeFirewall, "80", nil, true, "admin"); err != nil {
		t.Fatal(err)
	}
	healthRule := findSystemHealthRule(t, env)

	// 1. Abweichung: der Status ist zwar aktiv, trägt aber keinen LCM-Hash
	// (z.B. von Hand konfiguriert) → durchsetzen.
	env.Dialer.Responses["then echo ufw"] = sshx.FakeResponse{Output: "ufw\n"}
	env.Dialer.Responses["ufw status verbose"] = sshx.FakeResponse{Output: "Status: active\n"}
	env.Dialer.Commands = nil
	env.Executor.RunRule(healthRule, "scheduler")

	all := strings.Join(env.Dialer.Commands, "\n")
	if !strings.Contains(all, "ufw --force reset") || !strings.Contains(all, "ufw allow proto tcp to any port 80") {
		t.Errorf("abweichung sollte die firewall durchsetzen:\n%s", all)
	}
	srv, _ := env.Servers.Get(repositories.ScopeAll(), id)
	if !srv.FirewallActive || srv.FirewallAllowedPorts != "80" {
		t.Errorf("server nach durchsetzung falsch: active=%v ports=%q", srv.FirewallActive, srv.FirewallAllowedPorts)
	}
	var job domain.Job
	if err := env.DB().Where("type = ?", domain.RuleTypeHealth).Order("rowid desc").First(&job).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(job.Output, "Grundsatz-Regeln") || !strings.Contains(job.Output, "abweichung erkannt") {
		t.Errorf("health-job sollte die durchsetzung protokollieren:\n%s", job.Output)
	}

	// Hash des angewendeten Regelsatzes aus dem Enable-Kommando extrahieren -
	// er gehört in den konformen Status (Drift-Erkennung inkl. Bind/Version).
	m := reLcmHash.FindStringSubmatch(all)
	if m == nil {
		t.Fatalf("kein lcm-hash im enable-skript:\n%s", all)
	}

	// 2. Kein Drift: der Status entspricht dem Soll (22 + 80 offen, aktiv,
	// LCM-Hash vorhanden) → es darf KEIN Reset/Enable laufen.
	env.Dialer.Responses["ufw status verbose"] = sshx.FakeResponse{
		Output: "Status: active\n" +
			"22/tcp                     ALLOW IN    Anywhere                   # lcm:" + m[1] + "\n" +
			"80/tcp                     ALLOW IN    Anywhere\n" +
			"22/tcp (v6)                ALLOW IN    Anywhere (v6)\n" +
			"80/tcp (v6)                ALLOW IN    Anywhere (v6)\n",
	}
	env.Dialer.Commands = nil
	env.Executor.RunRule(healthRule, "scheduler")

	all = strings.Join(env.Dialer.Commands, "\n")
	if strings.Contains(all, "ufw --force reset") {
		t.Errorf("ohne abweichung darf die firewall nicht neu angewendet werden:\n%s", all)
	}
	var job2 domain.Job
	if err := env.DB().Where("type = ?", domain.RuleTypeHealth).Order("rowid desc").First(&job2).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(job2.Output, "firewall ok") {
		t.Errorf("health-job sollte 'firewall ok' melden:\n%s", job2.Output)
	}
}
