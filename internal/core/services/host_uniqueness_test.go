package services_test

import (
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/storage"
	"LCM/internal/storage/repositories"
)

// TestHostExistsPortScoped: Eindeutig ist das Paar (Host, SSH-Port). Dieselbe
// IP mit unterschiedlichem Port (NAT/Port-Forwarding) ist KEIN Duplikat.
func TestHostExistsPortScoped(t *testing.T) {
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.Migrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repositories.NewServerRepository(db)
	s := &domain.Server{Name: "a", Host: "127.0.0.1", SSHPort: 2221, PrivateKeyEnc: "x", HostKeyFingerprint: "fp"}
	if err := repo.Create(s); err != nil {
		t.Fatal(err)
	}

	// Gleiche IP + gleicher Port → Duplikat.
	if taken, _ := repo.HostExists("127.0.0.1", 2221, 0); !taken {
		t.Error("gleiche IP+Port muss als Duplikat gelten")
	}
	// Gleiche IP, ANDERER Port → kein Duplikat (Port-Forward).
	if taken, _ := repo.HostExists("127.0.0.1", 2222, 0); taken {
		t.Error("gleiche IP mit anderem Port darf KEIN Duplikat sein")
	}
	// Port 0 wird als 22 gelesen - hier kein Treffer (bestehender Port 2221).
	if taken, _ := repo.HostExists("127.0.0.1", 0, 0); taken {
		t.Error("port 0 (=22) trifft nicht den 2221-Eintrag")
	}
	// excludeID schließt den eigenen Datensatz aus (Bearbeiten).
	if taken, _ := repo.HostExists("127.0.0.1", 2221, s.ID); taken {
		t.Error("excludeID muss den eigenen Server ausschließen")
	}
}
