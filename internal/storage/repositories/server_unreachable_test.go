package repositories_test

import (
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// TestMarkUnreachableZaehltInDerDatenbank: Jeder Aufruf zählt einen
// Fehlschlag mehr. Der Zähler wird per SQL-Ausdruck erhöht - ein Tippfehler
// darin täte stillschweigend gar nichts, deshalb geht der Test über die
// Datenbank.
func TestMarkUnreachableZaehltInDerDatenbank(t *testing.T) {
	repo := repositories.NewServerRepository(migrierteDB(t))
	srv := &domain.Server{
		Name: "test-01", Host: "10.0.0.1", SSHPort: 22, ServiceUser: domain.DefaultServiceUser,
		HostKeyFingerprint: "SHA256:TEST", PrivateKeyEnc: "x", Reachable: true,
	}
	if err := repo.Create(srv); err != nil {
		t.Fatal(err)
	}

	for _, cause := range []string{"timeout", "connection refused"} {
		if err := repo.MarkUnreachable(srv.ID, cause); err != nil {
			t.Fatal(err)
		}
	}

	got, err := repo.FindByID(repositories.ScopeAll(), srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Reachable || got.FailedChecks != 2 || got.LastError != "connection refused" {
		t.Errorf("reachable=%v failed=%d last_error=%q, erwartet false/2/\"connection refused\"",
			got.Reachable, got.FailedChecks, got.LastError)
	}
}
