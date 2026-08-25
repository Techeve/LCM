package services

import (
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// TestUnbehebbareCVEsFaerbenNicht (R2-056): Das Befund-Szenario aus dem
// Langzeittest - ein voll gepflegtes Debian mit 23 kritischen/hohen
// OS-CVEs, für die es KEINEN Fix gibt. Solche Funde dürfen weder Ampel
// noch Alarm auslösen (keine mögliche Handlung), müssen aber als Info
// sichtbar bleiben.
func TestUnbehebbareCVEsFaerbenNicht(t *testing.T) {
	// 23 unbehebbare crit/high (Debian-Basispakete) + 1 behebbare medium.
	var facts []repositories.VulnFact
	for i := 0; i < 12; i++ {
		facts = append(facts, repositories.VulnFact{Severity: domain.SeverityCritical, Source: domain.VulnSourceOS, PackageName: "perl"})
	}
	for i := 0; i < 11; i++ {
		facts = append(facts, repositories.VulnFact{Severity: domain.SeverityHigh, Source: domain.VulnSourceOS, PackageName: "libxml2"})
	}
	facts = append(facts, repositories.VulnFact{Severity: domain.SeverityMedium, Source: domain.VulnSourceOS, PackageName: "curl", FixedVersion: "8.9.1"})

	weighted := weightedVulnSummary(facts, nil, nil, nil)
	if weighted[domain.SeverityCritical] != 0 || weighted[domain.SeverityHigh] != 0 {
		t.Fatalf("unbehebbare Funde dürfen nicht in die Eskalations-Zählung: %v", weighted)
	}
	if weighted[domain.SeverityMedium] != 1 {
		t.Errorf("die behebbare medium-CVE muss zählen: %v", weighted)
	}
	if n := unfixableCritHigh(facts, nil, nil, nil); n != 23 {
		t.Errorf("unbehebbare crit/high: erwartet 23, bekam %d", n)
	}
	// „Sehr gut"-Basis: nur die eine behebbare zählt dagegen.
	if n := countedVulns(facts, nil); n != 1 {
		t.Errorf("countedVulns: erwartet 1 (nur behebbare), bekam %d", n)
	}

	// Ampel: erreichbar, gepflegt, gehärtet, Firewall an → GRÜN, mit dem
	// Unbehebbar-Hinweis als Info. Vorher war dieses System dauerhaft rot.
	srv := &domain.Server{Reachable: true, SSHHardened: true, FirewallActive: true}
	status, insights := srv.TrafficLight(domain.TrafficLightInput{
		CriticalVulns: weighted[domain.SeverityCritical], HighVulns: weighted[domain.SeverityHigh],
		UnfixableVulns: 23, TotalVulns: countedVulns(facts, nil),
	})
	if status == domain.ServerStatusRed || status == domain.ServerStatusYellow {
		t.Fatalf("gepflegtes System mit nur unbehebbaren CVEs darf nicht %q sein (Insights: %+v)", status, insights)
	}
	found := false
	for _, in := range insights {
		if in.Severity == "info" && strings.Contains(in.Message, "ohne verfügbaren Fix") {
			found = true
		}
	}
	if !found {
		t.Errorf("der Unbehebbar-Hinweis fehlt in den Insights: %+v", insights)
	}

	// Gegenprobe: EIN behebbarer kritischer Fund muss weiterhin rot machen.
	fixable := []repositories.VulnFact{{Severity: domain.SeverityCritical, Source: domain.VulnSourceOS, PackageName: "openssl", FixedVersion: "3.0.15"}}
	w2 := weightedVulnSummary(fixable, nil, nil, nil)
	status2, _ := srv.TrafficLight(domain.TrafficLightInput{CriticalVulns: w2[domain.SeverityCritical]})
	if status2 != domain.ServerStatusRed {
		t.Errorf("behebbare kritische CVE muss rot bleiben, bekam %q", status2)
	}

	// Hochgewichtung wirkt auch auf Unbehebbares (high → critical zählt in
	// den Unbehebbar-Hinweis, nicht in die Eskalation).
	listedHigh := []repositories.VulnFact{{Severity: domain.SeverityMedium, Source: domain.VulnSourceOS, PackageName: "nginx"}}
	if n := unfixableCritHigh(listedHigh, []string{"nginx"}, nil, nil); n != 1 {
		t.Errorf("hochgewichtete unbehebbare medium→high sollte im Hinweis zählen, bekam %d", n)
	}
}
