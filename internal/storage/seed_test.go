package storage

import (
	"testing"

	"LCM/internal/config"
	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// TestSeedResolvesAllRolePermissions ist der Regressionstest gegen den Bug,
// bei dem eine Rolle eine Permission zugewiesen bekam, die nicht im Seed
// registriert war (=> Permission-ID 0 => Foreign-Key-Verletzung). Der Seed
// muss vollständig durchlaufen und jede Rollen-Permission muss existieren.
func TestSeedResolvesAllRolePermissions(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err := Seed(db, &config.Config{AdminInitialPassword: "x"}); err != nil {
		t.Fatalf("seed fehlgeschlagen: %v", err)
	}

	roleRepo := repositories.NewRoleRepository(db)

	// Manager muss exakt die ManagerPermissions besitzen - alle aufgelöst.
	manager, err := roleRepo.FindByName(domain.RoleManager)
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, p := range manager.Permissions {
		if p.ID == 0 {
			t.Errorf("manager hat eine nicht aufgelöste permission (id 0): %q", p.Code)
		}
		have[p.Code] = true
	}
	for _, code := range domain.ManagerPermissions() {
		if !have[code] {
			t.Errorf("manager fehlt permission %q", code)
		}
	}

	// Mandantentrennung: der Linux-Benutzer-KATALOG ist global und daher
	// admin-only - der Manager darf ihn NICHT verwalten (sonst könnte er
	// fremden Accounts SSH-Keys unterschieben). Regressionsschutz für die
	// bewusste Rechte-Beschränkung.
	for _, code := range []string{domain.PermLinuxUsersRead, domain.PermLinuxUsersWrite} {
		if have[code] {
			t.Errorf("manager hat unerwartet die katalog-weite permission %q (Mandanten-Leck)", code)
		}
	}

	// Es dürfen keine role_permissions mit permission_id 0 existieren.
	var zero int64
	db.Table("role_permissions").Where("permission_id = 0").Count(&zero)
	if zero != 0 {
		t.Errorf("%d role_permissions mit permission_id 0 (nicht aufgelöst)", zero)
	}
}

// TestSeedUpgradesExistingRoles ist der Regressionstest gegen den Bug, bei
// dem Bestandsinstallationen neue Permission-Codes nie bekamen: Das Seeding
// lief nur bei leerer User-Tabelle, also fehlte z.B. linuxusers:read der
// Admin-Rolle - und die UI blendete den Menüpunkt "Linux-Benutzer" aus.
// Seed() muss die Rollen-Permission-Sets bei JEDEM Start nachziehen.
func TestSeedUpgradesExistingRoles(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err := Seed(db, &config.Config{AdminInitialPassword: "x"}); err != nil {
		t.Fatalf("erstes seed: %v", err)
	}

	// Alte Installation simulieren: der Admin-Rolle eine "neue" Permission
	// wegnehmen (so als wäre sie mit einem älteren Stand geseedet worden).
	roleRepo := repositories.NewRoleRepository(db)
	admin, err := roleRepo.FindByName(domain.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(
		"DELETE FROM role_permissions WHERE role_id = ? AND permission_id IN (SELECT id FROM permissions WHERE code IN (?, ?))",
		admin.ID, domain.PermLinuxUsersRead, domain.PermLinuxUsersWrite,
	).Error; err != nil {
		t.Fatal(err)
	}

	// Zweiter Start (User-Tabelle ist NICHT leer) muss die Rechte nachziehen.
	if err := Seed(db, &config.Config{}); err != nil {
		t.Fatalf("zweites seed: %v", err)
	}
	admin, err = roleRepo.FindByName(domain.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, p := range admin.Permissions {
		have[p.Code] = true
	}
	for _, code := range []string{domain.PermLinuxUsersRead, domain.PermLinuxUsersWrite} {
		if !have[code] {
			t.Errorf("admin fehlt nach upgrade-seed die permission %q", code)
		}
	}

	// Und es darf kein zweiter Admin-User entstanden sein (idempotent).
	var admins int64
	db.Table("users").Where("username = ?", domain.AdminUsername).Count(&admins)
	if admins != 1 {
		t.Errorf("erwartet genau 1 admin-user, gefunden: %d", admins)
	}
}

// TestSeedAddsPackageScanRuleToExistingSystemGroup prüft den Upgrade-Pfad:
// eine bestehende System-Gruppe ohne Paketlisten-Scan-Rule bekommt sie beim
// nächsten Start nachgezogen - genau einmal (idempotent).
func TestSeedAddsPackageScanRuleToExistingSystemGroup(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err := Seed(db, &config.Config{AdminInitialPassword: "x"}); err != nil {
		t.Fatalf("erstes seed: %v", err)
	}

	// Alte Installation simulieren: die package-scan-Rule wieder entfernen.
	if err := db.Where("type = ?", domain.RuleTypePackageScan).
		Delete(&domain.Rule{}).Error; err != nil {
		t.Fatal(err)
	}
	var before int64
	db.Model(&domain.Rule{}).Where("type = ?", domain.RuleTypePackageScan).Count(&before)
	if before != 0 {
		t.Fatalf("setup: package-scan-rule sollte entfernt sein, sind %d", before)
	}

	// Zweiter Start zieht die Rule nach - im täglichen Sync-Schedule.
	if err := Seed(db, &config.Config{}); err != nil {
		t.Fatalf("zweites seed: %v", err)
	}
	var scan domain.Rule
	if err := db.Where("type = ?", domain.RuleTypePackageScan).First(&scan).Error; err != nil {
		t.Fatalf("package-scan-rule wurde nicht nachgezogen: %v", err)
	}
	if !scan.IsSystem || scan.ScheduleID == nil {
		t.Errorf("package-scan-rule nicht korrekt: %+v", scan)
	}
	// Sie hängt am selben Schedule wie die Sync-Rule (täglicher Sync).
	var sync domain.Rule
	db.Where("type = ?", domain.RuleTypeSync).First(&sync)
	if sync.ScheduleID == nil || *scan.ScheduleID != *sync.ScheduleID {
		t.Errorf("package-scan hängt nicht am sync-schedule (scan=%v sync=%v)", scan.ScheduleID, sync.ScheduleID)
	}

	// Dritter Start darf keine zweite Rule anlegen (idempotent).
	if err := Seed(db, &config.Config{}); err != nil {
		t.Fatal(err)
	}
	var after int64
	db.Model(&domain.Rule{}).Where("type = ?", domain.RuleTypePackageScan).Count(&after)
	if after != 1 {
		t.Errorf("erwartet genau 1 package-scan-rule, gefunden: %d", after)
	}
}

// TestSeedAddsDockerCheckRuleToExistingSystemGroup prüft den Upgrade-Pfad
// für v0.10.0: Der zentrale Docker-Check ist kein eigener Settings-Cron
// mehr, sondern eine Rule des täglichen System-Sync - Bestandsdatenbanken
// bekommen sie beim nächsten Start nachgezogen (idempotent).
func TestSeedAddsDockerCheckRuleToExistingSystemGroup(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err := Seed(db, &config.Config{AdminInitialPassword: "x"}); err != nil {
		t.Fatalf("erstes seed: %v", err)
	}

	// Bestandsinstallation simulieren: die docker-check-Rule entfernen.
	if err := db.Where("type = ?", domain.RuleTypeDockerCheck).
		Delete(&domain.Rule{}).Error; err != nil {
		t.Fatal(err)
	}

	// Zweiter Start zieht die Rule in den Sync-Schedule nach.
	if err := Seed(db, &config.Config{}); err != nil {
		t.Fatalf("zweites seed: %v", err)
	}
	var check domain.Rule
	if err := db.Where("type = ?", domain.RuleTypeDockerCheck).First(&check).Error; err != nil {
		t.Fatalf("docker-check-rule wurde nicht nachgezogen: %v", err)
	}
	var sync domain.Rule
	db.Where("type = ?", domain.RuleTypeSync).First(&sync)
	if !check.IsSystem || check.ScheduleID == nil || sync.ScheduleID == nil || *check.ScheduleID != *sync.ScheduleID {
		t.Errorf("docker-check hängt nicht am sync-schedule: %+v", check)
	}

	// Dritter Start bleibt idempotent.
	if err := Seed(db, &config.Config{}); err != nil {
		t.Fatal(err)
	}
	var n int64
	db.Model(&domain.Rule{}).Where("type = ?", domain.RuleTypeDockerCheck).Count(&n)
	if n != 1 {
		t.Errorf("erwartet genau 1 docker-check-rule, gefunden: %d", n)
	}
}

// TestSeedKnownReposRespectsUserChanges: Der Katalog bekannter Paketquellen
// wird nur auf einer leeren Tabelle befüllt - Nutzer-Änderungen (Edit,
// Löschung) überleben weitere Seed-Läufe (= Neustarts).
func TestSeedKnownReposRespectsUserChanges(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{AdminInitialPassword: "x"}
	if err := Seed(db, cfg); err != nil {
		t.Fatalf("seed fehlgeschlagen: %v", err)
	}

	var n int64
	db.Model(&domain.KnownRepo{}).Count(&n)
	if want := int64(len(domain.DefaultKnownRepos())); n != want {
		t.Fatalf("erwartet %d katalog-einträge, gefunden %d", want, n)
	}

	// Nutzer bearbeitet einen Eintrag und löscht einen anderen.
	if err := db.Model(&domain.KnownRepo{}).Where("key = ?", "docker").
		Update("name", "Docker (angepasst)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("key = ?", "grafana").Delete(&domain.KnownRepo{}).Error; err != nil {
		t.Fatal(err)
	}

	// Zweiter Seed-Lauf (Neustart) - nichts wird zurückgesetzt oder ergänzt.
	if err := Seed(db, cfg); err != nil {
		t.Fatalf("zweiter seed fehlgeschlagen: %v", err)
	}
	var docker domain.KnownRepo
	if err := db.Where("key = ?", "docker").First(&docker).Error; err != nil {
		t.Fatal(err)
	}
	if docker.Name != "Docker (angepasst)" {
		t.Errorf("nutzer-änderung überschrieben: %q", docker.Name)
	}
	db.Model(&domain.KnownRepo{}).Where("key = ?", "grafana").Count(&n)
	if n != 0 {
		t.Error("gelöschter katalog-eintrag wurde wieder angelegt")
	}
}
