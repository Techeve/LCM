package storage

import (
	"testing"

	"LCM/internal/core/domain"
)

// TestEnableCVEScanMigration: die v0.4.0-Migration aktiviert den CVE-Scan bei
// der bestehenden Settings-Zeile und setzt einen Standard-Zeitplan, ohne einen
// bereits gesetzten Cron zu überschreiben.
func TestEnableCVEScanMigration(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}

	// Bestehende Settings-Zeile mit leerem Cron (Simulation eines Alt-Bestands).
	// Der leere Wert muss NACH dem Anlegen gesetzt werden: Die Spalte hat einen
	// DEFAULT, den GORM beim Einfügen einsetzt - sonst käme hier gar kein
	// Alt-Bestand zustande, sondern der heutige Standard.
	if err := db.Create(&domain.GlobalSettings{ID: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`UPDATE global_settings SET cve_scan_cron = '' WHERE id = 1`).Error; err != nil {
		t.Fatal(err)
	}

	runMigrationByName(t, db, "0.4.0-enable-cve-scan")

	var s domain.GlobalSettings
	if err := db.First(&s, 1).Error; err != nil {
		t.Fatal(err)
	}
	if s.CVEScanCron != "0 4 * * *" {
		t.Errorf("Cron sollte auf Standard gesetzt sein, war %q", s.CVEScanCron)
	}
	if !s.CVEScanEnabled {
		t.Error("CVE-Scan sollte aktiviert sein")
	}

	// Idempotent + kein Überschreiben eines gesetzten Crons.
	db.Model(&domain.GlobalSettings{}).Where("id = 1").Update("cve_scan_cron", "0 6 * * *")
	runMigrationByName(t, db, "0.4.0-enable-cve-scan")
	db.First(&s, 1)
	if s.CVEScanCron != "0 6 * * *" {
		t.Errorf("gesetzter Cron wurde überschrieben: %q", s.CVEScanCron)
	}
}
