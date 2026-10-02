package services_test

import (
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// TestHeldBackUpdatesDoNotCountAsOverdue: Zurückgehaltene Updates werten den
// Server nicht ab - eine CVE darauf bleibt dagegen sichtbar und sorgt dafür,
// dass ein gestaffeltes Update vorgezogen wird.
func TestHeldBackUpdatesDoNotCountAsOverdue(t *testing.T) {
	env := newTestEnv(t)
	servers := repositories.NewServerRepository(env.DB())
	s := &domain.Server{Name: "noble", Host: "10.1.1.1", Reachable: true}
	if err := servers.Create(s); err != nil {
		t.Fatal(err)
	}
	if err := servers.ReplacePackages(s.ID, []domain.Package{
		{Name: "bash", Version: "5.2", CandidateVersion: "5.3"},
		{Name: "sosreport", Version: "4.10", CandidateVersion: "4.11", HeldReason: domain.HeldReasonPhased},
		{Name: "libxpm4", Version: "1", CandidateVersion: "2", HeldReason: domain.HeldReasonPhased},
		{Name: "openssl", Version: "3.0.15", CandidateVersion: "3.0.16", HeldReason: domain.HeldReasonHold},
	}); err != nil {
		t.Fatal(err)
	}
	if n, _ := servers.CountOutdatedPackages(s.ID); n != 1 {
		t.Errorf("überfällige updates %d, erwartet 1 (nur bash)", n)
	}

	for _, pkg := range []string{"libxpm4", "openssl"} {
		v := &domain.Vulnerability{ServerRef: repositories.ServerRef(s.ID), CVEID: "CVE-2026-0001",
			PackageName: pkg, Severity: domain.SeverityHigh, Source: domain.VulnSourceOS}
		if err := env.DB().Create(v).Error; err != nil {
			t.Fatal(err)
		}
	}
	names, err := servers.PhasedPackagesWithCVE(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Gesperrte Pakete bleiben gesperrt - auch mit CVE, das hat der
	// Betreiber so entschieden.
	if strings.Join(names, ",") != "libxpm4" {
		t.Fatalf("vorzuziehen %v, erwartet nur libxpm4", names)
	}
}
