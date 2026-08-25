package services_test

import (
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/storage/repositories"
)

// TestRefreshHardwareReadsFactsWithoutUpgrade: „Hardware aktualisieren" liest
// die Fakten neu ein und führt KEIN Upgrade/Install aus.
func TestRefreshHardwareReadsFactsWithoutUpgrade(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")

	// Scan liefert nun andere Hardware-Werte (Server aufgerüstet).
	env.Dialer.Responses["nproc"] = sshx.FakeResponse{Output: "8\n"}
	env.Dialer.Responses["/^Mem:/"] = sshx.FakeResponse{Output: "16000 4000\n"}
	env.Dialer.Commands = nil

	if _, err := env.Servers.RefreshHardware(repositories.ScopeAll(), id, "admin"); err != nil {
		t.Fatalf("refresh-hardware: %v", err)
	}
	done := waitServerJob(t, env, id, services.JobTypeHardwareRefresh)
	if done.Status != domain.JobStatusSuccess {
		t.Fatalf("job nicht erfolgreich: %+v", done)
	}
	srv, _ := env.Servers.Get(repositories.ScopeAll(), id)
	if srv.CPUCores != 8 || srv.MemTotalMB != 16000 {
		t.Errorf("hardware nicht aktualisiert: %d kerne, %d MB", srv.CPUCores, srv.MemTotalMB)
	}
	all := strings.Join(env.Dialer.Commands, "\n")
	if strings.Contains(all, "upgrade") || strings.Contains(all, "install") {
		t.Errorf("refresh darf nichts installieren:\n%s", all)
	}
}

// TestRefreshAllReadsFirewallAndSSHStatus: „Alles aktualisieren" liest u.a.
// den Firewall- und SSH-Härtungs-Status live vom Server.
func TestRefreshAllReadsFirewallAndSSHStatus(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")

	// Server meldet aktive Firewall (Port 80 zusätzlich) und LCM-Härtung.
	env.Dialer.Responses["then echo ufw"] = sshx.FakeResponse{Output: "ufw\n"}
	env.Dialer.Responses["ufw status verbose"] = sshx.FakeResponse{
		Output: "Status: active\n\nTo                         Action      From\n--                         ------      ----\n22/tcp                     ALLOW       Anywhere\n80/tcp                     ALLOW       Anywhere\n",
	}
	env.Dialer.Responses["60-lcm-hardening.conf"] = sshx.FakeResponse{Output: "yes\n"}
	env.Dialer.Commands = nil

	if _, err := env.Servers.RefreshAll(repositories.ScopeAll(), id, "admin"); err != nil {
		t.Fatalf("refresh-all: %v", err)
	}
	done := waitServerJob(t, env, id, services.JobTypeFullRefresh)
	if done.Status != domain.JobStatusSuccess {
		t.Fatalf("job nicht erfolgreich: %+v", done)
	}
	srv, _ := env.Servers.Get(repositories.ScopeAll(), id)
	if !srv.FirewallActive {
		t.Errorf("firewall-status nicht live erkannt: %+v", srv)
	}
	if srv.FirewallAllowedPorts != "80" {
		t.Errorf("freigegebene ports (ohne ssh) falsch: %q", srv.FirewallAllowedPorts)
	}
	if !srv.SSHHardened {
		t.Errorf("ssh-härtung nicht live erkannt: %+v", srv)
	}
}

// TestPortlisteVerschwindetMitDerFirewall: die Portliste beschreibt, was
// GERADE freigegeben ist. Nach `apt-get purge ufw` erkennt LCM auf Debian das
// vorinstallierte nftables - der ufw-Zweig, der die Liste sonst neu aufbaut,
// laeuft dann nicht mehr, und die alten ufw-Ports blieben ohne diese Regel
// fuer immer stehen. Live gefunden: firewall_active war korrekt false, aber
// die Anzeige nannte weiterhin Port 8080 als freigegeben (R2-013).
func TestPortlisteVerschwindetMitDerFirewall(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")

	// Erst ufw aktiv mit Port 80 - der Ausgangszustand.
	env.Dialer.Responses["then echo ufw"] = sshx.FakeResponse{Output: "ufw\n"}
	env.Dialer.Responses["ufw status verbose"] = sshx.FakeResponse{
		Output: "Status: active\n\n22/tcp                     ALLOW       Anywhere\n80/tcp                     ALLOW       Anywhere\n",
	}
	if _, err := env.Servers.RefreshAll(repositories.ScopeAll(), id, "admin"); err != nil {
		t.Fatalf("refresh-all: %v", err)
	}
	waitServerJob(t, env, id, services.JobTypeFullRefresh)
	if srv, _ := env.Servers.Get(repositories.ScopeAll(), id); srv.FirewallAllowedPorts != "80" {
		t.Fatalf("Ausgangszustand falsch: %q", srv.FirewallAllowedPorts)
	}

	// Jetzt ist ufw weg und nftables wird erkannt - ohne aktive Tabelle.
	env.Dialer.Responses["then echo ufw"] = sshx.FakeResponse{Output: "nftables\n"}
	// Das echte Kommando endet wegen `|| echo` immer mit 0 - nur die Ausgabe
	// sagt, dass keine LCM-Tabelle existiert.
	env.Dialer.Responses["nft list table inet lcm"] = sshx.FakeResponse{Output: "LCM: keine lcm-tabelle\n"}
	if _, err := env.Servers.RefreshAll(repositories.ScopeAll(), id, "admin"); err != nil {
		t.Fatalf("zweiter refresh-all: %v", err)
	}
	waitServerJob(t, env, id, services.JobTypeFullRefresh)

	srv, _ := env.Servers.Get(repositories.ScopeAll(), id)
	if srv.FirewallActive {
		t.Error("firewall_active muss ohne aktive Firewall false sein")
	}
	if srv.FirewallAllowedPorts != "" {
		t.Errorf("die Portliste zeigt weiterhin den alten Stand: %q", srv.FirewallAllowedPorts)
	}
}

// TestRefreshHardwareIncludesDNS: der Hardware-Scan liest die aktive
// DNS-Konfiguration aus und führt den Auflösungstest der Test-Domains
// automatisch mit aus - nicht nur die manuelle Aktion "DNS-Test".
func TestRefreshHardwareIncludesDNS(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")

	env.Dialer.Responses["getent hosts"] = sshx.FakeResponse{Output: "OK github.com\nOK deb.debian.org\n"}
	env.Dialer.Responses["resolvectl dns"] = sshx.FakeResponse{Output: "9.9.9.9\n"}
	env.Dialer.Commands = nil

	if _, err := env.Servers.RefreshHardware(repositories.ScopeAll(), id, "admin"); err != nil {
		t.Fatalf("refresh-hardware: %v", err)
	}
	done := waitServerJob(t, env, id, services.JobTypeHardwareRefresh)
	if done.Status != domain.JobStatusSuccess {
		t.Fatalf("job nicht erfolgreich: %+v", done)
	}
	srv, _ := env.Servers.Get(repositories.ScopeAll(), id)
	if srv.DNSTestStatus != domain.DNSTestFull {
		t.Errorf("dns_test_status = %q, erwartet %q", srv.DNSTestStatus, domain.DNSTestFull)
	}
	if srv.DNSCurrent != "9.9.9.9" {
		t.Errorf("dns_current = %q, erwartet 9.9.9.9", srv.DNSCurrent)
	}
	if srv.DNSTestAt == nil {
		t.Error("dns_test_at wurde nicht gesetzt")
	}
}
