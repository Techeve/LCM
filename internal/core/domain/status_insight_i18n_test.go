package domain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Die Ampel-Befunde standen als fertige deutsche Sätze im Code und erschienen
// damit auch in der englischen Oberfläche. Seitdem trägt jeder Befund einen
// Übersetzungsschlüssel. Diese beiden Tests halten das: Ein neuer Befund ohne
// Schlüssel - oder mit einem Schlüssel, der in einem der Sprachkataloge
// fehlt - fällt hier auf und nicht erst dem Anwender.

// insightScenarios deckt jeden Befundzweig von TrafficLight mindestens einmal ab.
func insightScenarios() []struct {
	name   string
	server Server
	in     TrafficLightInput
} {
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	base := TrafficLightInput{Now: now}
	stale := CVEDBStatus{Available: true, AgeHours: 240, Freshness: CVEDBStale}

	return []struct {
		name   string
		server Server
		in     TrafficLightInput
	}{
		{"nicht erreichbar", Server{Reachable: false}, base},
		{"nicht erreichbar mit Ursache", Server{Reachable: false, LastError: "timeout"}, base},
		{"keine Bestandsaufnahme", Server{Reachable: true}, TrafficLightInput{Now: now, InventoryMissing: true}},
		{"alte CVE-Datenbank", Server{Reachable: true}, TrafficLightInput{Now: now, CVEDB: stale}},
		{"CVE-Scan fehlgeschlagen", Server{Reachable: true}, TrafficLightInput{Now: now, CVEScanError: "trivy fehlt"}},
		{"kritische und hohe CVEs", Server{Reachable: true}, TrafficLightInput{
			Now: now, CriticalVulns: 1, HighVulns: 3, RaisedVulnPackages: []string{"nginx"},
			UnfixableVulns: 2, TotalVulns: 4,
		}},
		{"überfällige Updates", Server{Reachable: true}, TrafficLightInput{Now: now, OutdatedPackages: 1, OutdatedContainerImages: 5}},
		{"Festplatte knapp", Server{Reachable: true, DiskTotalMB: 100, DiskUsedMB: 99}, base},
		{"Uhr vor", Server{Reachable: true, ClockOffsetSeconds: 9999}, base},
		{"Uhr nach", Server{Reachable: true, ClockOffsetSeconds: -9999}, base},
		{"Uhr vor im Container", Server{Reachable: true, ClockOffsetSeconds: 9999, Virtualization: "lxc"}, base},
		{"Uhr nach im Container", Server{Reachable: true, ClockOffsetSeconds: -9999, Virtualization: "lxc"}, base},
		{"kein Zeitdienst", Server{Reachable: true, TimeCheckedAt: &now}, base},
		{"Zeitdienst unsynchron", Server{Reachable: true, TimeCheckedAt: &now, NTPService: "chrony"}, base},
		{"Neustart nötig", Server{Reachable: true, RebootRequired: true}, base},
		{"Deep-Scan-Warnungen", Server{Reachable: true}, TrafficLightInput{Now: now, DeepScanWarnings: 2}},
		{"letzter Job fehlgeschlagen", Server{Reachable: true}, TrafficLightInput{Now: now, LastJobFailed: true}},
		{"letzter Job benannt", Server{Reachable: true}, TrafficLightInput{Now: now, LastJobFailed: true, LastJobName: "Update"}},
		{"Betriebssystem EOL", Server{Reachable: true, OSID: "ubuntu", OSVersionID: "18.04"}, base},
		{"RouterOS-Update", Server{
			Reachable: true, OSID: OSIDRouterOS, RouterOSUpdateAvailable: true,
		}, base},
		{"RouterOS-Update mit Version", Server{
			Reachable: true, OSID: OSIDRouterOS, RouterOSUpdateAvailable: true, RouterOSLatestVersion: "7.15",
		}, base},
		{"DSM-Update und Advisor", Server{
			Reachable: true, OSID: OSIDSynologyDSM, DSMUpdateAvailable: true,
			DSMLatestVersion: "7.2", DSMSecurityRisks: 3,
		}, base},
		{"Red Hat nicht registriert", Server{Reachable: true, RHSMStatus: RHSMUnregistered}, base},
		{"Red Hat ohne Berechtigung", Server{Reachable: true, RHSMStatus: RHSMInvalid}, base},
		{"OK, aber nicht Sehr gut", Server{Reachable: true, FirewallTool: "ufw"}, TrafficLightInput{Now: now, TotalVulns: 2}},
		{"OK ohne bekanntes Firewall-Werkzeug", Server{Reachable: true}, base},
	}
}

func TestJederStatusBefundHatEinenUebersetzungsschluessel(t *testing.T) {
	for _, sc := range insightScenarios() {
		server := sc.server
		_, insights := server.TrafficLight(sc.in)
		if len(insights) == 0 {
			t.Errorf("%s: keine Befunde - das Szenario prüft nichts", sc.name)
		}
		for _, in := range insights {
			if in.Key == "" {
				t.Errorf("%s: Befund ohne Schlüssel: %q", sc.name, in.Message)
			}
			if in.Message == "" {
				t.Errorf("%s: Befund %q ohne deutschen Klartext", sc.name, in.Key)
			}
		}
	}
}

func TestJederStatusBefundStehtInBeidenSprachkatalogen(t *testing.T) {
	catalogs := map[string]string{}
	for _, lang := range []string{"de", "en"} {
		path := filepath.Join("..", "..", "..", "frontend", "src", "locales", lang+".js")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("Sprachkatalog %s nicht lesbar: %v", lang, err)
		}
		// Nur der insights-Abschnitt zählt - ein gleichnamiger Schlüssel in
		// einem anderen Abschnitt wäre sonst ein falscher Freispruch.
		text := string(data)
		start := strings.Index(text, "\n  insights: {")
		if start < 0 {
			t.Fatalf("Sprachkatalog %s hat keinen insights-Abschnitt", lang)
		}
		end := strings.Index(text[start+1:], "\n  },")
		if end < 0 {
			t.Fatalf("insights-Abschnitt in %s nicht abgeschlossen", lang)
		}
		catalogs[lang] = text[start : start+1+end]
	}
	seen := map[string]bool{}
	for _, sc := range insightScenarios() {
		server := sc.server
		_, insights := server.TrafficLight(sc.in)
		for _, in := range insights {
			if in.Key == "" || seen[in.Key] {
				continue
			}
			seen[in.Key] = true
			for lang, text := range catalogs {
				if !strings.Contains(text, "\n    "+in.Key+":") {
					t.Errorf("Befund %q fehlt im Sprachkatalog %s (Szenario %s)", in.Key, lang, sc.name)
				}
			}
		}
	}
}
