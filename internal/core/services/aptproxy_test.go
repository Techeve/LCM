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

func TestConfigureAptProxyRequiresCacheURL(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")

	// Ohne konfigurierte Cache-URL wird die Anbindung klar abgelehnt.
	if _, err := env.Servers.ConfigureAptProxy(repositories.ScopeAll(), id, true, "admin"); !errors.Is(err, services.ErrNoAptCacheURL) {
		t.Fatalf("erwartet ErrNoAptCacheURL, bekommen %v", err)
	}
}

func TestConfigureAptProxyEnableDisable(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	if _, err := env.Settings.UpdateGlobal(services.GlobalSettingsInput{AptCacheURL: sp("http://cache.local:3142")}, "admin"); err != nil {
		t.Fatal(err)
	}

	out, err := env.Servers.ConfigureAptProxy(repositories.ScopeAll(), id, true, "admin")
	if err != nil {
		t.Fatalf("anbinden: %v (output: %q)", err, out)
	}
	// Das Skript schreibt das Drop-in mit http- UND https-Proxy und testet
	// die Anbindung sofort per apt-Update.
	var script string
	for _, cmd := range env.Dialer.Commands {
		if strings.Contains(cmd, "02lcm-apt-cache") && strings.Contains(cmd, "printf") {
			script = cmd
		}
	}
	if script == "" {
		t.Fatalf("kein apt-proxy-skript ausgeführt: %v", env.Dialer.Commands)
	}
	for _, want := range []string{
		`Acquire::http::Proxy "http://cache.local:3142";`,
		`Acquire::https::Proxy "http://cache.local:3142";`,
		"apt-get update",
		"rm -f /etc/apt/apt.conf.d/02lcm-apt-cache", // Rollback-Zweig
	} {
		if !strings.Contains(script, want) {
			t.Errorf("skript ohne %q:\n%s", want, script)
		}
	}
	server, _ := env.Servers.Get(repositories.ScopeAll(), id)
	if !server.AptProxyActive {
		t.Error("apt_proxy_active nach anbindung nicht gesetzt")
	}

	// Trennen entfernt das Drop-in und setzt das Feld zurück.
	if _, err := env.Servers.ConfigureAptProxy(repositories.ScopeAll(), id, false, "admin"); err != nil {
		t.Fatalf("trennen: %v", err)
	}
	server, _ = env.Servers.Get(repositories.ScopeAll(), id)
	if server.AptProxyActive {
		t.Error("apt_proxy_active nach trennung noch gesetzt")
	}
}

func TestConfigureAptProxyRollbackOnFailure(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")
	if _, err := env.Settings.UpdateGlobal(services.GlobalSettingsInput{AptCacheURL: sp("http://cache.local:3142")}, "admin"); err != nil {
		t.Fatal(err)
	}

	// apt-Update durch den Proxy schlägt fehl → Skript meldet exit 1
	// (und hat das Drop-in serverseitig wieder entfernt).
	env.Dialer.Responses["02lcm-apt-cache"] = sshx.FakeResponse{
		Output:   "LCM: apt-update ueber den cache fehlgeschlagen - drop-in wieder entfernt\n",
		ExitCode: 1,
	}
	if _, err := env.Servers.ConfigureAptProxy(repositories.ScopeAll(), id, true, "admin"); err == nil {
		t.Fatal("fehlgeschlagene anbindung muss als fehler gemeldet werden")
	}
	server, _ := env.Servers.Get(repositories.ScopeAll(), id)
	if server.AptProxyActive {
		t.Error("apt_proxy_active trotz fehlschlag gesetzt")
	}
}

// Der Refresh-Live-Status erkennt die Anbindung am Drop-in.
func TestReadServerLiveStatusDetectsAptProxy(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")

	env.Dialer.Responses["test -f /etc/apt/apt.conf.d/02lcm-apt-cache"] = sshx.FakeResponse{Output: "yes\n"}
	if _, err := env.Servers.RefreshAll(repositories.ScopeAll(), id, "admin"); err != nil {
		t.Fatal(err)
	}
	done := waitServerJob(t, env, id, services.JobTypeFullRefresh)
	if done.Status != domain.JobStatusSuccess {
		t.Fatalf("job nicht erfolgreich: %+v", done)
	}
	server, _ := env.Servers.Get(repositories.ScopeAll(), id)
	if !server.AptProxyActive {
		t.Error("refresh-all hat die apt-cache-anbindung nicht erkannt")
	}
}

// TestConfigureAptProxyDisconnectWarnsAboutEnforceRule: beim Trennen weist
// der Output darauf hin, wenn eine aktive apt-proxy-Grundsatz-Regel den
// Server beim nächsten Health-Check wieder anbinden würde - sonst wirkte die
// Trennung scheinbar grundlos nur wenige Minuten.
func TestConfigureAptProxyDisconnectWarnsAboutEnforceRule(t *testing.T) {
	env := newTestEnv(t)
	serverID, groupID := aptProxyTestGroup(t, env)
	if _, err := env.Groups.DefineRule(repositories.ScopeAll(), groupID, "apt-cache-pflicht", domain.RuleTypeAptProxy, "", nil, true, "admin"); err != nil {
		t.Fatalf("enforce-rule definieren: %v", err)
	}

	out, err := env.Servers.ConfigureAptProxy(repositories.ScopeAll(), serverID, false, "admin")
	if err != nil {
		t.Fatalf("trennen: %v", err)
	}
	if !strings.Contains(out, "apt-cache-pflicht") || !strings.Contains(out, "Grundsatz-Regel") {
		t.Errorf("output ohne enforce-hinweis:\n%s", out)
	}

	// Ohne Grundsatz-Regel: kein Hinweis.
	env2 := newTestEnv(t)
	id2 := joinTestServer(t, env2, "web02")
	if _, err := env2.Settings.UpdateGlobal(services.GlobalSettingsInput{AptCacheURL: sp("http://cache.local:3142")}, "admin"); err != nil {
		t.Fatal(err)
	}
	out2, err := env2.Servers.ConfigureAptProxy(repositories.ScopeAll(), id2, false, "admin")
	if err != nil {
		t.Fatalf("trennen ohne regel: %v", err)
	}
	if strings.Contains(out2, "Grundsatz-Regel") {
		t.Errorf("unerwarteter enforce-hinweis ohne regel:\n%s", out2)
	}
}
