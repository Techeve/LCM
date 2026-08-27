package services

import (
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// demoSimDB baut eine minimale In-Memory-Datenbank nur mit den Tabellen, die
// die Demo-Simulation anfasst (kein storage.Migrate - das ergäbe einen
// Importzyklus storage→services).
func demoSimDB(t *testing.T) (*gorm.DB, *repositories.ServerRepository) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("db öffnen: %v", err)
	}
	if err := db.AutoMigrate(&domain.Server{}, &domain.Package{}, &domain.Vulnerability{},
		&domain.DockerImage{}, &domain.DockerContainer{}); err != nil {
		t.Fatalf("migrieren: %v", err)
	}
	return db, repositories.NewServerRepository(db)
}

// TestDemoSimulatePackageUpdate: das simulierte Update hebt die Paketstände,
// entfernt dadurch geschlossene CVEs und liefert Paketmanager-Output.
func TestDemoSimulatePackageUpdate(t *testing.T) {
	db, repo := demoSimDB(t)
	server := &domain.Server{Name: "web01", Host: "10.0.0.1", PackageManager: "apt", IsDemo: true}
	if err := db.Create(server).Error; err != nil {
		t.Fatal(err)
	}
	ref := repositories.ServerRef(server.ID)
	db.Create(&[]domain.Package{
		{ServerRef: ref, Name: "openssl", Version: "3.0.11-1", CandidateVersion: "3.0.14-1", Security: true},
		{ServerRef: ref, Name: "curl", Version: "7.88.1-10"},
	})
	db.Create(&[]domain.Vulnerability{
		{ServerRef: ref, CVEID: "CVE-2023-0286", PackageName: "openssl", FixedVersion: "3.0.14-1",
			Severity: domain.SeverityCritical, Source: "os"},
		{ServerRef: ref, CVEID: "CVE-2023-38545", PackageName: "curl", FixedVersion: "",
			Severity: domain.SeverityMedium, Source: "os"},
	})

	output := demoSimulatePackageUpdate(repo, server)
	if !strings.Contains(output, "Setting up openssl (3.0.14-1)") {
		t.Errorf("apt-Output erwartet, bekam: %q", output)
	}

	pkgs, err := repo.FindPackages(server.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pkgs {
		if p.Name == "openssl" && (p.Version != "3.0.14-1" || p.CandidateVersion != "" || p.Security) {
			t.Errorf("openssl nicht aktualisiert: %+v", p)
		}
	}
	vulns, err := repo.FindVulnerabilities(server.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range vulns {
		if v.CVEID == "CVE-2023-0286" {
			t.Error("geschlossene CVE ist noch da")
		}
	}
	if len(vulns) != 1 {
		t.Errorf("CVE ohne Fix muss bleiben, bekam %d Funde", len(vulns))
	}

	// Zweiter Lauf: nichts mehr zu tun.
	if output := demoSimulatePackageUpdate(repo, server); !strings.Contains(output, "0 upgraded") {
		t.Errorf("Leerlauf-Output erwartet, bekam: %q", output)
	}
}

// TestDemoSimulateReboot: reboot_required wird gelöscht und der Server bootet
// in den neuesten installierten Kernel.
func TestDemoSimulateReboot(t *testing.T) {
	db, repo := demoSimDB(t)
	server := &domain.Server{
		Name: "pve01", Host: "10.0.0.2", RebootRequired: true, KernelVersion: "6.8.12-1-pve",
		InstalledKernels: `[{"name":"proxmox-kernel-6.8.12-4-pve","release":"6.8.12-4-pve","version":"6.8.12-4"},` +
			`{"name":"proxmox-kernel-6.8.12-1-pve","release":"6.8.12-1-pve","version":"6.8.12-1"}]`,
		IsDemo: true,
	}
	if err := db.Create(server).Error; err != nil {
		t.Fatal(err)
	}

	output := demoSimulateReboot(repo, server)
	if !strings.Contains(output, "6.8.12-4-pve") {
		t.Errorf("neuer Kernel fehlt im Output: %q", output)
	}
	var after domain.Server
	db.First(&after, server.ID)
	if after.RebootRequired {
		t.Error("reboot_required wurde nicht gelöscht")
	}
	if after.KernelVersion != "6.8.12-4-pve" {
		t.Errorf("kernel_version = %q, erwartet 6.8.12-4-pve", after.KernelVersion)
	}
}

// TestDemoRuleOutput deckt die Zweige ohne Datenbank-Effekte ab.
func TestDemoRuleOutput(t *testing.T) {
	_, repo := demoSimDB(t)
	server := &domain.Server{IsDemo: true}
	tests := []struct {
		ruleType, command, want string
	}{
		{domain.RuleTypeHealth, "", "lcm-health-ok"},
		{domain.RuleTypeScript, "uptime", "$ uptime"},
		{domain.RuleTypeRebootIfNeeded, "", "Kein Neustart erforderlich"},
		{domain.RuleTypeSync, "", "simuliert"},
	}
	for _, tt := range tests {
		rule := &domain.Rule{Type: tt.ruleType, Command: tt.command}
		if got := demoRuleOutput(repo, server, rule); !strings.Contains(got, tt.want) {
			t.Errorf("demoRuleOutput(%s) = %q, erwartet Teilstring %q", tt.ruleType, got, tt.want)
		}
	}
}
