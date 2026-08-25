package storage

import (
	"path/filepath"
	"testing"

	"gorm.io/gorm"

	"LCM/internal/storage/migrations"
	"LCM/internal/version"
)

func newMigrationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	return db
}

// setBinaryVersion simuliert eine per ldflags injizierte Binary-Version.
func setBinaryVersion(t *testing.T, v string) {
	t.Helper()
	old := version.Version
	version.Version = v
	t.Cleanup(func() { version.Version = old })
}

// addTestMigration registriert temporär eine Migration in der Registry.
func addTestMigration(t *testing.T, m migrations.Migration) *bool {
	t.Helper()
	ran := false
	orig := m.Run
	m.Run = func(tx *gorm.DB) error {
		ran = true
		if orig != nil {
			return orig(tx)
		}
		return nil
	}
	migrations.Registry = append(migrations.Registry, m)
	t.Cleanup(func() { migrations.Registry = migrations.Registry[:len(migrations.Registry)-1] })
	return &ran
}

func countApplied(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var n int64
	db.Model(&appliedMigration{}).Count(&n)
	return n
}

// TestFreshInstallCreatesVersionFile: Erststart (keine DB, keine
// Versionsdatei) legt version.json an und überspringt alle Migrationen
// (Baseline) - die frische DB ist bereits auf aktuellem Stand.
func TestFreshInstallCreatesVersionFile(t *testing.T) {
	setBinaryVersion(t, "1.0.0")
	db := newMigrationTestDB(t)
	vfile := filepath.Join(t.TempDir(), version.FileName)
	ran := addTestMigration(t, migrations.Migration{Version: "1.0.0", Name: "test_fresh"})

	if _, err := RunUpdateMigrations(db, UpdateOptions{VersionFilePath: vfile, FreshInstall: true}); err != nil {
		t.Fatal(err)
	}

	if *ran {
		t.Error("bei Erstinstallation darf keine Migration laufen (Baseline)")
	}
	installed, err := version.ReadInstalled(vfile)
	if err != nil || installed == nil {
		t.Fatalf("versionsdatei nicht angelegt: %v", err)
	}
	if installed.Version != "1.0.0" {
		t.Errorf("versionsdatei enthält %q statt 1.0.0", installed.Version)
	}
	// Baseline umfasst alle Migrationen bis zur Binary-Version (1.0.0) -
	// zukünftige (z.B. das 1.1.0-Skript) bleiben offen fürs Update.
	expected := 0
	for _, m := range migrations.Registry {
		if version.Compare(m.Version, "1.0.0") <= 0 {
			expected++
		}
	}
	if got := countApplied(t, db); int(got) != expected {
		t.Errorf("baseline unvollständig: %d von %d", got, expected)
	}
}

// TestUpdateRunsMigrationsInRange: Update 1.0.0 -> 1.2.0 führt die
// Migrationen für 1.1.0 und 1.2.0 aus, in SemVer-Reihenfolge - aber
// keine, die für eine spätere Version (1.3.0) deklariert ist.
func TestUpdateRunsMigrationsInRange(t *testing.T) {
	setBinaryVersion(t, "1.0.0")
	db := newMigrationTestDB(t)
	vfile := filepath.Join(t.TempDir(), version.FileName)

	// Installation auf Stand 1.0.0 bringen.
	if _, err := RunUpdateMigrations(db, UpdateOptions{VersionFilePath: vfile, FreshInstall: true}); err != nil {
		t.Fatal(err)
	}

	// "Update einspielen": Binary ist jetzt 1.2.0 und bringt drei
	// Migrationen mit (absichtlich unsortiert registriert).
	var order []string
	track := func(name string) func(tx *gorm.DB) error {
		return func(tx *gorm.DB) error {
			order = append(order, name)
			return nil
		}
	}
	addTestMigration(t, migrations.Migration{Version: "1.2.0", Name: "m_120", Run: track("m_120")})
	addTestMigration(t, migrations.Migration{Version: "1.1.0", Name: "m_110", Run: track("m_110")})
	futureRan := addTestMigration(t, migrations.Migration{Version: "1.3.0", Name: "m_130"})
	setBinaryVersion(t, "1.2.0")

	if _, err := RunUpdateMigrations(db, UpdateOptions{VersionFilePath: vfile}); err != nil {
		t.Fatal(err)
	}

	if len(order) != 2 || order[0] != "m_110" || order[1] != "m_120" {
		t.Errorf("falsche Ausführung/Reihenfolge: %v (erwartet [m_110 m_120])", order)
	}
	if *futureRan {
		t.Error("migration für zukünftige Version 1.3.0 darf nicht laufen")
	}

	// Versionsdatei wurde fortgeschrieben.
	installed, _ := version.ReadInstalled(vfile)
	if installed.Version != "1.2.0" {
		t.Errorf("versionsdatei nicht aktualisiert: %q", installed.Version)
	}

	// Zweiter Start derselben Version: nichts läuft doppelt.
	order = nil
	if _, err := RunUpdateMigrations(db, UpdateOptions{VersionFilePath: vfile}); err != nil {
		t.Fatal(err)
	}
	if len(order) != 0 {
		t.Errorf("migrationen doppelt gelaufen: %v", order)
	}
}

// TestLegacyInstallWithoutVersionFile: DB existiert, aber keine
// version.json (Stand vor Einführung der Datei) => wird als Update
// behandelt, ausstehende Migrationen laufen, Datei wird angelegt.
func TestLegacyInstallWithoutVersionFile(t *testing.T) {
	setBinaryVersion(t, "1.0.0")
	db := newMigrationTestDB(t)
	vfile := filepath.Join(t.TempDir(), version.FileName)
	ran := addTestMigration(t, migrations.Migration{Version: "1.0.0", Name: "test_legacy"})

	// FreshInstall = false: die DB war schon da.
	if _, err := RunUpdateMigrations(db, UpdateOptions{VersionFilePath: vfile, FreshInstall: false}); err != nil {
		t.Fatal(err)
	}

	if !*ran {
		t.Error("ausstehende Migration muss bei Legacy-Installation laufen")
	}
	if installed, _ := version.ReadInstalled(vfile); installed == nil {
		t.Error("versionsdatei wurde nicht angelegt")
	}
}

// TestDevBuildRunsAllPending: Dev-Builds ("0.0.0-dev") haben keine
// Versions-Obergrenze - unprotokollierte Migrationen laufen immer nach.
func TestDevBuildRunsAllPending(t *testing.T) {
	setBinaryVersion(t, "0.0.0-dev")
	db := newMigrationTestDB(t)
	vfile := filepath.Join(t.TempDir(), version.FileName)
	ran := addTestMigration(t, migrations.Migration{Version: "9.9.9", Name: "m_future_dev"})

	if _, err := RunUpdateMigrations(db, UpdateOptions{VersionFilePath: vfile, FreshInstall: false}); err != nil {
		t.Fatal(err)
	}
	if !*ran {
		t.Error("dev-build sollte alle ausstehenden Migrationen ausführen")
	}
}

// TestKnownRepoTecheveMigration: die 1.3.0-Migration ergänzt den TechEve-
// Katalogeintrag und läuft idempotent (kein Duplikat bei erneutem Lauf).
func TestKnownRepoTecheveMigration(t *testing.T) {
	db := newMigrationTestDB(t)

	var mig *migrations.Migration
	for i := range migrations.Registry {
		if migrations.Registry[i].Name == "1.3.0-known-repo-techeve" {
			mig = &migrations.Registry[i]
			break
		}
	}
	if mig == nil {
		t.Fatal("Migration 1.3.0-known-repo-techeve nicht registriert")
	}

	countTecheve := func() int64 {
		var n int64
		db.Table("known_repos").Where(`"key" = ?`, "techeve").Count(&n)
		return n
	}
	if countTecheve() != 0 {
		t.Fatalf("frische DB sollte keinen techeve-Eintrag haben")
	}
	if err := mig.Run(db); err != nil {
		t.Fatalf("Migration fehlgeschlagen: %v", err)
	}
	if countTecheve() != 1 {
		t.Fatalf("techeve-Eintrag fehlt nach Migration (Anzahl %d)", countTecheve())
	}
	// Zweiter Lauf darf kein Duplikat erzeugen.
	if err := mig.Run(db); err != nil {
		t.Fatalf("zweiter Lauf fehlgeschlagen: %v", err)
	}
	if countTecheve() != 1 {
		t.Errorf("Migration nicht idempotent - %d Einträge", countTecheve())
	}

	var line string
	db.Table("known_repos").Where(`"key" = ?`, "techeve").Select("line").Scan(&line)
	if line != "deb [signed-by=/etc/apt/keyrings/techeve.asc] https://repo.techeve.de stable main" {
		t.Errorf("unerwartete Repo-Zeile: %q", line)
	}
}

func TestSemverCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.0", "1.0.1", -1},
		{"1.2.0", "1.10.0", -1}, // numerisch, nicht lexikografisch
		{"2.0.0", "1.9.9", 1},
		{"1.0.0-rc1", "1.0.0", -1}, // Prerelease < Release
		{"v1.2.3", "1.2.3", 0},     // v-Präfix toleriert
	}
	for _, c := range cases {
		if got := version.Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q, %q) = %d, erwartet %d", c.a, c.b, got, c.want)
		}
	}
	if version.IsRelease("0.0.0-dev") {
		t.Error("0.0.0-dev darf keine Release-Version sein")
	}
	if !version.IsRelease("1.0.0") {
		t.Error("1.0.0 sollte eine Release-Version sein")
	}
}
