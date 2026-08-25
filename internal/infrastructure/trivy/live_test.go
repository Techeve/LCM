package trivy

import (
	"context"
	"os"
	"testing"

	"LCM/internal/core/domain"
)

// TestLiveScan prüft die komplette SBOM→Trivy-Kette gegen das echte Trivy-
// Binary: Es baut ein SBOM für ein bewusst veraltetes Ubuntu-Paket und
// erwartet, dass Trivy dafür bekannte CVEs meldet. Der Test wird
// übersprungen, wenn Trivy nicht installiert ist (CI/Standard-Dev-Host) -
// er ist ein manueller Verifikationslauf.
//
// Cache-Verzeichnis über TRIVY_CACHE_DIR steuerbar (Vuln-DB-Download beim
// ersten Lauf, danach gecacht).
func TestLiveScan(t *testing.T) {
	cacheDir := os.Getenv("TRIVY_CACHE_DIR")
	scanner := New("trivy", cacheDir)
	if !scanner.Available() {
		t.Skip("trivy nicht installiert - Integrationslauf übersprungen")
	}

	vulns, err := scanner.Scan(context.Background(), Target{
		OSID: "ubuntu", OSVersionID: "22.04", PackageManager: "apt",
		Packages: []domain.Package{
			// Basis-Release-Versionen von Ubuntu 22.04 - dafür gibt es
			// zahlreiche bekannte, längst behobene CVEs.
			{Name: "openssl", Version: "3.0.2-0ubuntu1"},
			{Name: "libssl3", Version: "3.0.2-0ubuntu1"},
			{Name: "curl", Version: "7.81.0-1"},
			{Name: "bash", Version: "5.1-6ubuntu1"},
		},
	})
	if err != nil {
		t.Fatalf("Scan fehlgeschlagen: %v", err)
	}
	if len(vulns) == 0 {
		t.Fatal("KEINE CVEs gefunden - SBOM/PURL-Matching greift nicht (distro-Qualifier prüfen)")
	}
	// Mindestens ein openssl/libssl-Fund erwartet.
	sawSSL := false
	sev := map[string]int{}
	for _, v := range vulns {
		sev[v.Severity]++
		if v.PackageName == "openssl" || v.PackageName == "libssl3" {
			sawSSL = true
		}
	}
	t.Logf("Trivy lieferte %d Funde: %v", len(vulns), sev)
	if !sawSSL {
		t.Errorf("erwartete openssl/libssl-Funde, bekam nur: %+v", vulns[:min(3, len(vulns))])
	}
}
