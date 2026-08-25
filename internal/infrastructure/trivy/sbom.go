package trivy

import (
	"encoding/json"
	"strings"
)

// CycloneDX-SBOM-Erzeugung. Trivy scannt Dritt-SBOMs im CycloneDX-Format:
// Wir bauen eine minimale, aber vollständige Struktur - eine
// operating-system-Komponente (Distribution) und je Paket eine Komponente mit
// package URL (PURL) inkl. distro-Qualifier. Der Abhängigkeitsgraph verknüpft
// die Pakete mit dem Betriebssystem, damit Trivy sie als OS-Pakete der
// jeweiligen Distribution erkennt und die passende Advisory-Datenbank wählt.

// cdxBOM ist die serialisierte CycloneDX-1.5-Struktur.
type cdxBOM struct {
	BOMFormat    string          `json:"bomFormat"`
	SpecVersion  string          `json:"specVersion"`
	Version      int             `json:"version"`
	Metadata     cdxMetadata     `json:"metadata"`
	Components   []cdxComponent  `json:"components"`
	Dependencies []cdxDependency `json:"dependencies"`
}

type cdxMetadata struct {
	Component cdxComponent `json:"component"`
}

type cdxComponent struct {
	BOMRef  string `json:"bom-ref"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
	PURL    string `json:"purl,omitempty"`
}

type cdxDependency struct {
	Ref       string   `json:"ref"`
	DependsOn []string `json:"dependsOn,omitempty"`
}

// BuildSBOM erzeugt ein CycloneDX-JSON-SBOM aus dem Ziel.
func BuildSBOM(target Target) ([]byte, error) {
	const rootRef, osRef = "lcm-root", "lcm-os"

	components := []cdxComponent{{
		BOMRef:  osRef,
		Type:    "operating-system",
		Name:    osFamily(target.OSID),
		Version: osVersion(target.OSID, target.OSVersionID),
	}}

	pkgRefs := make([]string, 0, len(target.Packages))
	seen := map[string]bool{}
	for _, p := range target.Packages {
		if p.Name == "" {
			continue
		}
		purl := PurlFor(target, p.Name, p.Version)
		if seen[purl] {
			continue
		}
		seen[purl] = true
		components = append(components, cdxComponent{
			BOMRef:  purl,
			Type:    "library",
			Name:    p.Name,
			Version: p.Version,
			PURL:    purl,
		})
		pkgRefs = append(pkgRefs, purl)
	}

	bom := cdxBOM{
		BOMFormat:   "CycloneDX",
		SpecVersion: "1.5",
		Version:     1,
		Metadata: cdxMetadata{Component: cdxComponent{
			BOMRef: rootRef, Type: "application", Name: "lcm-managed-server",
		}},
		Components: components,
		Dependencies: []cdxDependency{
			{Ref: rootRef, DependsOn: []string{osRef}},
			{Ref: osRef, DependsOn: pkgRefs},
		},
	}
	return json.MarshalIndent(bom, "", "  ")
}

// PurlFor baut die package URL eines Pakets. deb- und rpm-Typ bestimmt die
// Paketverwaltung; der distro-Qualifier nennt Trivy die Distribution.
// Exportiert, weil die Frühwarnung (Etappe B) dieselben purls fuer ihre
// Online-Abfragen braucht - eine Quelle fuer SBOM, Scan-Cache und Poller.
func PurlFor(target Target, name, version string) string {
	typ := "deb"
	if pkgIsRPM(target.PackageManager) {
		typ = "rpm"
	}
	namespace := purlNamespace(target.OSID)
	distro := namespace + "-" + osVersion(target.OSID, target.OSVersionID)
	purl := "pkg:" + typ + "/" + namespace + "/" + name
	if version != "" {
		purl += "@" + version
	}
	return purl + "?distro=" + distro
}

// pkgIsRPM meldet, ob die Paketverwaltung rpm-basiert ist (dnf/yum/zypper).
func pkgIsRPM(mgr string) bool {
	switch mgr {
	case "dnf", "yum", "zypper":
		return true
	default:
		return false
	}
}

// osFamily bildet die os-release-ID auf die von Trivy erwartete OS-Klasse ab
// (best effort - bei der Live-Verifikation nachschärfbar).
func osFamily(osID string) string {
	switch strings.ToLower(osID) {
	case "ubuntu":
		return "ubuntu"
	case "debian":
		return "debian"
	case "rhel", "redhat":
		return "redhat"
	case "centos":
		return "centos"
	case "rocky":
		return "rocky"
	case "almalinux", "alma":
		return "alma"
	case "fedora":
		return "fedora"
	case "ol", "oracle":
		return "oracle"
	case "amzn", "amazon":
		return "amazon"
	case "opensuse-leap", "opensuse":
		return "opensuse.leap"
	case "sles", "suse":
		return "suse linux enterprise server"
	default:
		return strings.ToLower(osID)
	}
}

// purlNamespace ist der kurze Distributions-Token im PURL/distro-Qualifier.
func purlNamespace(osID string) string {
	switch strings.ToLower(osID) {
	case "opensuse-leap", "opensuse":
		return "opensuse.leap"
	case "sles", "suse":
		return "sles"
	case "rhel", "redhat":
		return "rhel"
	default:
		return strings.ToLower(osID)
	}
}

// osVersion normalisiert die VERSION_ID für Trivy: RPM-Familien und Debian
// werden auf die Major-Version gekürzt (Trivy adressiert Advisories je
// Major-Release), Ubuntu/SUSE behalten "major.minor".
func osVersion(osID, versionID string) string {
	versionID = strings.TrimSpace(versionID)
	if versionID == "" {
		return ""
	}
	switch strings.ToLower(osID) {
	case "ubuntu", "opensuse-leap", "opensuse", "sles", "suse":
		return versionID // "22.04", "15.5"
	default:
		// debian, rhel-familie, rocky, alma, fedora, oracle, amazon → Major.
		if i := strings.IndexByte(versionID, '.'); i >= 0 {
			return versionID[:i]
		}
		return versionID
	}
}
