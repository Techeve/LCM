//go:build acme_integration

package acmex

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-acme/lego/v5/challenge/dns01"
	"github.com/go-acme/lego/v5/challenge/resolver"
)

// DNS-01 gegen Pebble mit dem echten Anbieter rfc2136: Ein Test-DNS auf
// Loopback nimmt die Updates mit TSIG an und beantwortet die Fragen von lego.
// Pebble und Umgebung wie in pebble_integration_test.go (PEBBLE_VA_ALWAYS_VALID=1
// - Pebble wertet die Prüfung selbst als bestanden; geprüft wird hier, dass
// LCM den Eintrag setzt, lego ihn beim Resolver sieht, die CA das Zertifikat
// für den Platzhalter ausstellt und der Eintrag danach wieder weg ist).
//
//	LCM_PEBBLE_CA=$PEBBLE_DIR/test/certs/pebble.minica.pem \
//	  go test -tags acme_integration -run TestPebbleDNS01 -count=1 ./internal/infrastructure/acmex/
func TestPebbleDNS01(t *testing.T) {
	caFile := os.Getenv("LCM_PEBBLE_CA")
	if caFile == "" {
		t.Skip("LCM_PEBBLE_CA nicht gesetzt (Pfad zu pebble.minica.pem)")
	}
	t.Setenv("LEGO_CA_CERTIFICATES", caFile)
	directory := os.Getenv("LCM_PEBBLE_DIRECTORY")
	if directory == "" {
		directory = "https://localhost:14000/dir"
	}

	fake := startFakeDNS(t, "example.com")
	provider, err := NewDNSProvider("rfc2136", fake.rfc2136Values(), DNSEnv{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	// Wie DNS01, nur ohne die Prüfung bei den autoritativen Servern: Der
	// Test-DNS lauscht nicht auf Port 53.
	solver := func(m *resolver.SolverManager) error {
		useCheckNameservers([]string{fake.addr})
		return m.SetDNS01Provider(provider, dns01.DisableAuthoritativeNssPropagationRequirement())
	}

	m := NewManager(t.TempDir())
	cfg := Config{
		DirectoryURL:  directory,
		Email:         "admin@lcm.example",
		Names:         []string{"*.lcm.example.com"},
		TermsAccepted: true,
		Solver:        solver,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	out, err := m.Reconcile(ctx, cfg)
	if err != nil || Failures(out) != nil || out[0].Action != ActionIssued {
		t.Fatalf("Ausgang: %+v, %v, %v", out, err, Failures(out))
	}
	certs, err := m.Certificates()
	if err != nil || len(certs) != 1 || len(certs[0].Names) != 1 || certs[0].Names[0] != "*.lcm.example.com" ||
		!strings.Contains(certs[0].Issuer, "Pebble") {
		t.Fatalf("Zertifikate: %+v, %v", certs, err)
	}
	if updates, unsigned := fake.stats(); updates < 2 || unsigned != 0 {
		t.Errorf("%d Updates, davon %d ohne gültige Signatur", updates, unsigned)
	}
	if records := fake.records(); len(records) != 0 {
		t.Errorf("nach dem Lauf bleibt im DNS: %v", records)
	}
}
