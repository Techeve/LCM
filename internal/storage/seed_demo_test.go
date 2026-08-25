package storage

import (
	"testing"

	"LCM/internal/config"
	"LCM/internal/core/domain"
)

// TestDemoSeedCreatesTestData prüft, dass der Demo-Modus die
// UI-Testdaten anlegt (Server, Pakete, Gruppe, Jobs, Fake-User).
func TestDemoSeedCreatesTestData(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err := Seed(db, &config.Config{AdminInitialPassword: "x", DemoMode: true}); err != nil {
		t.Fatalf("demo-seed: %v", err)
	}

	var servers, packages, jobs, groups int64
	db.Model(&domain.Server{}).Count(&servers)
	db.Model(&domain.Package{}).Count(&packages)
	db.Model(&domain.Job{}).Count(&jobs)
	db.Model(&domain.ServerGroup{}).Count(&groups)

	if servers < 3 {
		t.Errorf("erwartet >=3 demo-server, bekam %d", servers)
	}
	if packages < 3 {
		t.Errorf("erwartet demo-pakete, bekam %d", packages)
	}
	if jobs < 3 {
		t.Errorf("erwartet demo-job-historie, bekam %d", jobs)
	}
	// System-Gruppe + Produktion-Gruppe.
	if groups < 2 {
		t.Errorf("erwartet >=2 gruppen, bekam %d", groups)
	}

	// Alle Demo-Server sind als IsDemo markiert (kein SSH-Kontakt).
	var nonDemo int64
	db.Model(&domain.Server{}).Where("is_demo = ?", false).Count(&nonDemo)
	if nonDemo != 0 {
		t.Errorf("demo-server müssen is_demo=true haben, %d ohne", nonDemo)
	}

	// Idempotenz: zweiter Seed-Aufruf legt nichts doppelt an.
	if err := Seed(db, &config.Config{DemoMode: true}); err != nil {
		t.Fatal(err)
	}
	var serversAfter int64
	db.Model(&domain.Server{}).Count(&serversAfter)
	if serversAfter != servers {
		t.Errorf("seed nicht idempotent: %d -> %d server", servers, serversAfter)
	}
}
