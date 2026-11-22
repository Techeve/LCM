//go:build acme_integration

package acmehttp

import (
	"context"
	"os"
	"testing"
	"time"

	"LCM/internal/infrastructure/acmex"
)

// Prüfung HTTP-01 über den echten Listener: Pebble fragt den Listener von LCM
// nach dem Token und stellt dann das Zertifikat aus. Die Prüfung ist hier
// ECHT - Pebble läuft ohne PEBBLE_VA_ALWAYS_VALID.
//
// Pebble fragt Port 5002 (httpPort der Pebble-Config) auf der Adresse, zu der
// der Name aufgelöst wird. Läuft Pebble in Docker, ist host.docker.internal
// ein Name, der auf den Rechner des Tests zeigt:
//
//	docker run -d --name lcm-pebble -p 14000:14000 -p 15000:15000 \
//	  -e PEBBLE_VA_NOSLEEP=1 -v "$PEBBLE_DIR/test:/test:ro" \
//	  ghcr.io/letsencrypt/pebble:latest -config /test/config/pebble-config.json
//
//	LCM_PEBBLE_CA=$PEBBLE_DIR/test/certs/pebble.minica.pem \
//	LCM_PEBBLE_HTTP01_NAME=host.docker.internal \
//	  go test -tags acme_integration -run TestPebbleHTTP01 -count=1 ./internal/acmehttp/
func TestPebbleHTTP01(t *testing.T) {
	caFile, name := os.Getenv("LCM_PEBBLE_CA"), os.Getenv("LCM_PEBBLE_HTTP01_NAME")
	if caFile == "" || name == "" {
		t.Skip("LCM_PEBBLE_CA und LCM_PEBBLE_HTTP01_NAME nicht gesetzt")
	}
	t.Setenv("LEGO_CA_CERTIFICATES", caFile)
	directory := os.Getenv("LCM_PEBBLE_DIRECTORY")
	if directory == "" {
		directory = "https://localhost:14000/dir"
	}

	tokens := acmex.NewChallengeStore()
	server := New(tokens)
	if err := server.Start(":5002"); err != nil {
		t.Fatal(err)
	}
	defer server.Stop()

	m := acmex.NewManager(t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	out, err := m.Reconcile(ctx, acmex.Config{
		DirectoryURL:  directory,
		Names:         []string{name},
		TermsAccepted: true,
		Solver:        acmex.HTTP01(tokens),
	})
	if err != nil || acmex.Failures(out) != nil || out[0].Action != acmex.ActionIssued {
		t.Fatalf("Ausgang: %+v, %v, %v", out, err, acmex.Failures(out))
	}
	if certs, _ := m.Certificates(); len(certs) != 1 || certs[0].Names[0] != name {
		t.Errorf("Zertifikate: %+v", certs)
	}
}
