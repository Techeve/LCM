package trivy

import (
	"context"
	"os"
	"strings"
	"testing"

	"LCM/internal/core/domain"
)

// TestLiveSidecar prüft die komplette Kette gegen einen ECHTEN Trivy-Sidecar:
// LCM baut das SBOM, schickt es über HTTP, der Sidecar ruft das echte Trivy
// mit der echten Schwachstellen-Datenbank auf, und LCM wertet dieselbe
// JSON-Ausgabe aus wie beim lokalen Lauf.
//
// Die Einzeltests mit einem gefälschten Sidecar können genau das nicht
// zeigen: dass die Aufrufzeile stimmt, dass Trivy das SBOM annimmt und dass
// das PURL-Matching auch über diesen Weg greift. Ein Scan, der über eine
// Attrappe funktioniert und in echt still nichts findet, wäre die
// schlimmste Variante - eine leere Fundliste sieht aus wie Entwarnung.
//
// Manueller Verifikationslauf, wie TestLiveScan. Voraussetzung:
//
//	docker run -d --name trivyd -p 19330:9330 -e LCM_TRIVY_TOKEN=… lcm-trivyd
//	LCM_TRIVY_URL=http://127.0.0.1:19330 LCM_TRIVY_TOKEN=… go test ./... -run LiveSidecar
func TestLiveSidecar(t *testing.T) {
	url := os.Getenv("LCM_TRIVY_URL")
	if url == "" {
		t.Skip("LCM_TRIVY_URL nicht gesetzt - Sidecar-Integrationslauf übersprungen")
	}
	scanner := NewRemote(url, os.Getenv("LCM_TRIVY_TOKEN"))

	st := scanner.Info(context.Background())
	if st.Error != "" && st.UpdatedAt == nil {
		t.Fatalf("Sidecar meldet keinen brauchbaren Zustand: %s", st.Error)
	}
	if st.SandboxBackend != SidecarBackend {
		t.Errorf("die Abschottung muss als %q gemeldet werden, bekam %q", SidecarBackend, st.SandboxBackend)
	}
	t.Logf("Sidecar: Trivy %s, Datenbank vom %v", st.Version, st.UpdatedAt)

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
		t.Fatalf("Scan über den Sidecar fehlgeschlagen: %v", err)
	}
	if len(vulns) == 0 {
		t.Fatal("KEINE CVEs über den Sidecar gefunden - SBOM/PURL-Matching greift auf diesem Weg nicht")
	}
	sev := map[string]int{}
	sawSSL := false
	for _, v := range vulns {
		sev[strings.ToLower(v.Severity)]++
		if v.PackageName == "openssl" || v.PackageName == "libssl3" {
			sawSSL = true
		}
	}
	t.Logf("Sidecar lieferte %d Funde: %v", len(vulns), sev)
	if !sawSSL {
		t.Error("erwartete openssl/libssl-Funde")
	}
}
