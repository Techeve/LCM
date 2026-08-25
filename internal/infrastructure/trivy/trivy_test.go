package trivy

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"LCM/internal/core/domain"
)

func TestBuildSBOMDeb(t *testing.T) {
	data, err := BuildSBOM(Target{
		OSID: "ubuntu", OSVersionID: "22.04", PackageManager: "apt",
		Packages: []domain.Package{
			{Name: "openssl", Version: "3.0.2-0ubuntu1.15"},
			{Name: "bash", Version: "5.1-6ubuntu1"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var bom cdxBOM
	if err := json.Unmarshal(data, &bom); err != nil {
		t.Fatalf("SBOM ist kein gültiges JSON: %v", err)
	}
	if bom.BOMFormat != "CycloneDX" || bom.SpecVersion != "1.5" {
		t.Errorf("falscher SBOM-Header: %+v", bom)
	}
	// Erste Komponente ist das Betriebssystem.
	if bom.Components[0].Type != "operating-system" || bom.Components[0].Name != "ubuntu" {
		t.Errorf("OS-Komponente falsch: %+v", bom.Components[0])
	}
	// Ein deb-PURL mit distro-Qualifier ist vorhanden.
	wantPURL := "pkg:deb/ubuntu/openssl@3.0.2-0ubuntu1.15?distro=ubuntu-22.04"
	found := false
	for _, c := range bom.Components {
		if c.PURL == wantPURL {
			found = true
		}
	}
	if !found {
		t.Errorf("erwarteter PURL %q nicht gefunden in %s", wantPURL, data)
	}
	// Der Abhängigkeitsgraph verknüpft OS → Pakete (2 Pakete).
	var osDep *cdxDependency
	for i := range bom.Dependencies {
		if bom.Dependencies[i].Ref == "lcm-os" {
			osDep = &bom.Dependencies[i]
		}
	}
	if osDep == nil || len(osDep.DependsOn) != 2 {
		t.Errorf("OS-Abhängigkeiten falsch: %+v", osDep)
	}
}

func TestPurlRPMAndVersionNormalization(t *testing.T) {
	// RHEL-Familie: rpm-Typ, Major-Version im distro-Qualifier.
	got := PurlFor(Target{OSID: "rocky", OSVersionID: "9.3", PackageManager: "dnf"}, "openssl", "3.0.7-24.el9")
	if !strings.HasPrefix(got, "pkg:rpm/rocky/openssl@") {
		t.Errorf("rpm-PURL erwartet, bekam %q", got)
	}
	if !strings.HasSuffix(got, "?distro=rocky-9") {
		t.Errorf("distro-Qualifier sollte Major-Version nutzen (rocky-9), bekam %q", got)
	}
	// Ubuntu behält major.minor.
	if v := osVersion("ubuntu", "22.04"); v != "22.04" {
		t.Errorf("ubuntu-Version sollte 22.04 bleiben, bekam %q", v)
	}
	// Debian → Major.
	if v := osVersion("debian", "12"); v != "12" {
		t.Errorf("debian 12 → 12, bekam %q", v)
	}
}

func TestParseReport(t *testing.T) {
	out := `{
	  "Results": [
	    {
	      "Vulnerabilities": [
	        {"VulnerabilityID":"CVE-2023-0286","PkgName":"openssl","InstalledVersion":"3.0.2","FixedVersion":"3.0.3","Severity":"CRITICAL","Title":"type confusion","PrimaryURL":"https://x"},
	        {"VulnerabilityID":"CVE-2023-1111","PkgName":"bash","InstalledVersion":"5.1","Severity":"low"},
	        {"VulnerabilityID":"","PkgName":"leer","Severity":"HIGH"}
	      ]
	    }
	  ]
	}`
	vulns, err := parseReport([]byte(out), "apt")
	if err != nil {
		t.Fatal(err)
	}
	// Der leere Eintrag (ohne ID) wird verworfen.
	if len(vulns) != 2 {
		t.Fatalf("erwartet 2 Funde, bekam %d", len(vulns))
	}
	if vulns[0].Severity != domain.SeverityCritical {
		t.Errorf("Severity sollte normalisiert 'critical' sein, war %q", vulns[0].Severity)
	}
	if vulns[1].Severity != domain.SeverityLow {
		t.Errorf("zweite Severity 'low', war %q", vulns[1].Severity)
	}
	if vulns[0].PkgManager != "apt" {
		t.Errorf("PkgManager sollte durchgereicht werden, war %q", vulns[0].PkgManager)
	}
}

func TestAvailableMissingBinary(t *testing.T) {
	if New("definitiv-nicht-vorhandenes-binary-xyz", "").Available() {
		t.Error("nicht vorhandenes Binary darf nicht als verfügbar gelten")
	}
}

// TestScanErrorUebersetztBekannteMuster: der Ampel-Text muss dem Betreiber
// sagen, was los ist - ein roher Trivy-Stacktrace tut das nicht (R2-005).
func TestScanErrorUebersetztBekannteMuster(t *testing.T) {
	raw := `2026-08-06T04:00:00Z	FATAL	Fatal error	run error: sbom scan error: scan error: scan failed: scan failed: failed to detect vulnerabilities: unable to scan OS packages: failed vulnerability detection of OS packages: failed detection: redhat vulnerability detection error: failed to get Red Hat advisories: unable to find CPE indices. See https://github.com/aquasecurity/trivy-db/issues/435 for details`
	// Der Fehler traegt den stderr-Text - beim lokalen Lauf wie beim
	// Sidecar. Genau darauf greift die Uebersetzung zu.
	err := scanError(fmt.Errorf("%w: %s", errExit1{}, raw))
	msg := err.Error()
	for _, want := range []string{"Red-Hat-Advisories", "trivy-db#435", "CentOS Stream", "kein Fehler an Server oder LCM"} {
		if !strings.Contains(msg, want) {
			t.Errorf("Meldung enthält %q nicht: %s", want, msg)
		}
	}
	if strings.Contains(msg, "FATAL") || strings.Contains(msg, "sbom scan error") {
		t.Errorf("roher Stacktrace darf im bekannten Fall nicht durchschlagen: %s", msg)
	}

	// Unbekannte Fehler behalten den vollen stderr - er ist die einzige Spur.
	err = scanError(fmt.Errorf("%w: FATAL something completely new", errExit1{}))
	if !strings.Contains(err.Error(), "something completely new") {
		t.Errorf("unbekannter Fehler verlor den stderr: %s", err)
	}
}

// errExit1 ist ein minimaler Fehler-Stub für scanError-Tests.
type errExit1 struct{}

func (errExit1) Error() string { return "exit status 1" }

// TestScanLaedtNichtsNach: Trivy hält seine Datenbank nur 24 Stunden für
// gültig und will danach mitten im Scan nachladen. In der Sandbox gibt es
// dafür kein Netz - der Scan endete deshalb jeden Tag zwischen Ablauf der
// Datenbank und dem nächsten Datenbank-Lauf mit einem Fatal-Fehler statt mit
// einer Bewertung (in der Testumgebung beobachtet). Der Scan muss deshalb
// ausdrücklich ohne Nachladen laufen.
func TestScanLaedtNichtsNach(t *testing.T) {
	line := strings.Join(sbomScanArgs("/var/lib/lcm/trivy", "/tmp/sbom.json"), " ")
	if !strings.Contains(line, "--skip-db-update") {
		t.Fatalf("Scan ohne --skip-db-update: %s", line)
	}
	if !strings.HasSuffix(line, "/tmp/sbom.json") {
		t.Errorf("SBOM steht nicht am Ende der Aufrufzeile: %s", line)
	}
	if !strings.Contains(line, "--cache-dir /var/lib/lcm/trivy") {
		t.Errorf("Cache-Verzeichnis fehlt: %s", line)
	}
}
