package services_test

import (
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/storage/repositories"
)

// TestDNSTestAction prüft die synchrone DNS-Test-Aktion end-to-end über den
// FakeDialer: die kanonische getent-Ausgabe wird zum dreistufigen Status geparst
// und samt aktivem Resolver am Server gespeichert.
func TestDNSTestAction(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")

	// Der DNS-Test führt ein Schleifen-Skript aus; der FakeDialer liefert für den
	// getent-Aufruf die simulierte Server-Ausgabe (eine Domain scheitert).
	env.Dialer.Responses["getent hosts"] = sshx.FakeResponse{
		Output: "OK github.com\nOK cloudflare.com\nFAIL deb.debian.org\n",
	}
	// dnsCurrentScript liefert (nach seiner Shell-Aufbereitung, die der FakeDialer
	// nicht ausführt) die aktiven Resolver leerzeichengetrennt.
	env.Dialer.Responses["resolvectl dns"] = sshx.FakeResponse{Output: "1.1.1.1 8.8.8.8\n"}

	res, err := env.Servers.DNSTest(repositories.ScopeAll(), id, "admin")
	if err != nil {
		t.Fatalf("DNSTest fehlgeschlagen: %v", err)
	}
	if res.Status != domain.DNSTestPartial {
		t.Fatalf("Status = %q, erwartet %q", res.Status, domain.DNSTestPartial)
	}
	if res.Current != "1.1.1.1, 8.8.8.8" {
		t.Fatalf("aktive Resolver falsch geparst: %q", res.Current)
	}

	// Das Ergebnis muss am Server persistiert sein.
	srv, err := env.Servers.Get(repositories.ScopeAll(), id)
	if err != nil {
		t.Fatal(err)
	}
	if srv.DNSTestStatus != domain.DNSTestPartial {
		t.Fatalf("dns_test_status am Server = %q", srv.DNSTestStatus)
	}
	if srv.DNSTestAt == nil {
		t.Fatal("dns_test_at wurde nicht gesetzt")
	}
	if srv.DNSCurrent != "1.1.1.1, 8.8.8.8" {
		t.Fatalf("dns_current am Server = %q", srv.DNSCurrent)
	}
}

// TestConfigureDNSRejectsInvalid stellt sicher, dass ungültige Eingaben vor jedem
// SSH-Zugriff abgelehnt werden.
func TestConfigureDNSRejectsInvalid(t *testing.T) {
	env := newTestEnv(t)
	id := joinTestServer(t, env, "web01")

	if _, err := env.Servers.ConfigureDNS(repositories.ScopeAll(), id, []string{"kein-ip"}, "admin"); err == nil {
		t.Fatal("ungültige IP hätte abgelehnt werden müssen")
	}
	if _, err := env.Servers.ConfigureDNS(repositories.ScopeAll(), id, []string{"1.1.1.1", "8.8.8.8", "9.9.9.9", "8.8.4.4"}, "admin"); err == nil {
		t.Fatal("zu viele DNS-Server hätten abgelehnt werden müssen")
	}
}
