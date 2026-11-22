//go:build acme_integration

package acmex

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"LCM/internal/infrastructure/tlsx"
)

// Integrationstest gegen Pebble, die Test-CA von Let's Encrypt. Die normale CI
// braucht ihn nicht, deshalb der Build-Tag.
//
// Pebble starten (aus dem Pebble-Repository kommen test/config/pebble-config.json
// und die Zertifikate unter test/certs, hier in $PEBBLE_DIR/test). In der Config
// muss das Profil "shortlived" fehlen: Pebble wählt sonst zufällig zwischen
// 90 Tagen und 6 Tagen Laufzeit, und die Erneuerung ist nicht mehr prüfbar.
//
//	docker run -d --name lcm-pebble -p 14000:14000 -p 15000:15000 \
//	  -e PEBBLE_VA_ALWAYS_VALID=1 -e PEBBLE_VA_NOSLEEP=1 \
//	  -v "$PEBBLE_DIR/test:/test:ro" ghcr.io/letsencrypt/pebble:latest \
//	  -config /test/config/pebble-config.json
//
// PEBBLE_VA_ALWAYS_VALID=1 lässt Pebble jede Prüfung als bestanden werten: Der
// Test braucht weder Port 80 noch einen Namen im DNS. Ausführen:
//
//	LCM_PEBBLE_CA=$PEBBLE_DIR/test/certs/pebble.minica.pem \
//	  go test -tags acme_integration -run TestPebble -count=1 ./internal/infrastructure/acmex/
//
// LCM_PEBBLE_DIRECTORY ändert die Adresse (Standard https://localhost:14000/dir).
// Die CA des Verzeichnisses kennt lego über LEGO_CA_CERTIFICATES - derselbe Weg
// für eine eigene CA wie step-ca im Betrieb.
func TestPebble(t *testing.T) {
	caFile := os.Getenv("LCM_PEBBLE_CA")
	if caFile == "" {
		t.Skip("LCM_PEBBLE_CA nicht gesetzt (Pfad zu pebble.minica.pem)")
	}
	t.Setenv("LEGO_CA_CERTIFICATES", caFile)
	directory := os.Getenv("LCM_PEBBLE_DIRECTORY")
	if directory == "" {
		directory = "https://localhost:14000/dir"
	}

	m := NewManager(t.TempDir())
	cfg := Config{
		DirectoryURL:  directory,
		Email:         "admin@lcm.example",
		Names:         []string{"lcm.example.com", "agent.example.com"},
		TermsAccepted: true,
		Solver:        HTTP01(NewChallengeStore()),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	t.Run("Konto ohne angenommene Bedingungen", func(t *testing.T) {
		refused := NewManager(t.TempDir())
		noTerms := cfg
		noTerms.TermsAccepted = false
		out, err := refused.Reconcile(ctx, noTerms)
		if err != nil || len(out) != 1 || !strings.Contains(out[0].Err.Error(), ErrTermsNotAccepted.Error()) {
			t.Fatalf("Ausgang: %+v, %v", out, err)
		}
	})

	var first CertInfo
	t.Run("ein Zertifikat mit zwei Namen", func(t *testing.T) {
		out, err := m.Reconcile(ctx, cfg)
		if err != nil || Failures(out) != nil || out[0].Action != ActionIssued {
			t.Fatalf("Ausgang: %+v, %v, %v", out, err, Failures(out))
		}
		certs, err := m.Certificates()
		if err != nil || len(certs) != 1 {
			t.Fatalf("Zertifikate: %+v, %v", certs, err)
		}
		first = certs[0]
		if lifetime := first.NotAfter.Sub(first.NotBefore); lifetime < 30*24*time.Hour {
			t.Skipf("Pebble stellt Kurzzeit-Zertifikate aus (%v) - Config ohne das Profil shortlived nutzen", lifetime)
		}
		if len(first.Names) != 2 || !strings.Contains(first.Issuer, "Pebble") {
			t.Errorf("Zertifikat: %+v", first)
		}
		specs, err := Specs(m.dataDir)
		if err != nil || len(specs) != 1 || specs[0].Source != tlsx.SourceACME {
			t.Errorf("Pool-Eintrag: %+v, %v", specs, err)
		}
		if reg, err := loadRegistration(m.dataDir, directory); err != nil || reg == nil || reg.Email != cfg.Email {
			t.Errorf("Registrierung: %+v, %v", reg, err)
		}
	})

	t.Run("noch nicht fällig bleibt es", func(t *testing.T) {
		out, err := m.Reconcile(ctx, cfg)
		if err != nil || out[0].Err != nil || out[0].Action != ActionKept {
			t.Fatalf("Ausgang: %+v, %v", out, err)
		}
	})

	t.Run("fällig wird es erneuert, mit neuem Schlüssel", func(t *testing.T) {
		m.now = func() time.Time { return time.Now().Add(80 * 24 * time.Hour) }
		out, err := m.Reconcile(ctx, cfg)
		if err != nil || out[0].Err != nil || out[0].Action != ActionRenewed {
			t.Fatalf("Ausgang: %+v, %v", out, err)
		}
		certs, _ := m.Certificates()
		if len(certs) != 1 || certs[0].Fingerprint == first.Fingerprint {
			t.Errorf("nach der Erneuerung: %+v, vorher %s", certs, first.Fingerprint)
		}
	})

	t.Run("je Name ein Zertifikat, das alte fällt weg", func(t *testing.T) {
		m.now = time.Now
		perName := cfg
		perName.PerName = true
		out, err := m.Reconcile(ctx, perName)
		if err != nil || Failures(out) != nil || len(out) != 2 {
			t.Fatalf("Ausgang: %+v, %v, %v", out, err, Failures(out))
		}
		if certs, _ := m.Certificates(); len(certs) != 2 {
			t.Errorf("Zertifikate: %+v", certs)
		}
	})

	t.Run("ohne Namen bleibt nichts liegen", func(t *testing.T) {
		if err := m.RemoveAll(); err != nil {
			t.Fatal(err)
		}
		if certs, _ := m.Certificates(); len(certs) != 0 {
			t.Errorf("übrig: %+v", certs)
		}
	})
}
