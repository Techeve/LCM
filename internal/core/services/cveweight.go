package services

import (
	"sort"
	"strings"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// CVE-Gewichtung: Die rohe Trivy-Schwere wird für Ampel und Alarme in eine
// EFFEKTIVE Schwere übersetzt:
//
//   - Docker-CVEs zählen standardmäßig GAR NICHT - Container sind isoliert,
//     ihre Pakete von außen nicht direkt erreichbar, und für Image-Inhalte ist
//     der Image-Anbieter verantwortlich. Nur CVEs von Containern, die in den
//     Server-Einstellungen ausdrücklich als relevant markiert sind
//     (Server.CVERelevantContainers), fließen ein - dann mit voller Schwere.
//   - CVEs von Paketen auf der Hochgewichtungs-Liste (GlobalSettings bzw.
//     Standardliste) eine Stufe RAUF - exponierte Dienste wie Webserver.
//   - CVEs von Paketen, die laut Scan auf von außen erreichbaren Ports
//     lauschen (Server.ListeningPackages), ebenfalls eine Stufe RAUF
//     (nur OS-Quelle - im Container lauscht der docker-proxy, nicht das Paket).
//
// Die Roh-Schwere in der UI bleibt unverändert - gewichtet wird die
// Einordnung (Ampel, Alarme).

// severityLadder ordnet die verschiebbaren Schweregrade (unknown bleibt fix).
var severityLadder = []string{
	domain.SeverityLow, domain.SeverityMedium, domain.SeverityHigh, domain.SeverityCritical,
}

func ladderIndex(sev string) int {
	for i, s := range severityLadder {
		if s == sev {
			return i
		}
	}
	return -1
}

// matchesPackageList prüft einen Paketnamen gegen eine normalisierte Liste:
// exakter Treffer oder Listenname als Präfix mit "-" (postgresql → postgresql-14).
func matchesPackageList(pkg string, list []string) bool {
	p := strings.ToLower(strings.TrimSpace(pkg))
	for _, name := range list {
		if p == name || strings.HasPrefix(p, name+"-") {
			return true
		}
	}
	return false
}

// vulnCounts meldet, ob ein Fund überhaupt in die Bewertung einfließt:
// OS-Funde immer, Docker-Funde nur, wenn ihr Image zu einem als relevant
// markierten Container gehört.
func vulnCounts(f repositories.VulnFact, relevantRefs map[string]bool) bool {
	return f.Source != domain.VulnSourceDocker || relevantRefs[f.ImageRef]
}

// weightedSeverity liefert die effektive Schwere eines zählenden Funds.
func weightedSeverity(f repositories.VulnFact, weightList, listening []string) string {
	idx := ladderIndex(domain.NormalizeSeverity(f.Severity))
	if idx < 0 {
		return domain.SeverityUnknown
	}
	raised := matchesPackageList(f.PackageName, weightList) ||
		(f.Source != domain.VulnSourceDocker && matchesPackageList(f.PackageName, listening))
	if raised {
		idx++
	}
	if idx >= len(severityLadder) {
		idx = len(severityLadder) - 1
	}
	return severityLadder[idx]
}

// weightedVulnSummary zählt die zählenden Funde je EFFEKTIVER Schwere -
// ausschließlich BEHEBBARE (R2-056): Funde ohne verfügbaren Fix können
// weder Ampel noch Alarm auslösen, weil es für sie keine mögliche Handlung
// gibt. Ein voll gepflegtes Debian bliebe sonst dauerhaft rot und erzöge
// zum Wegsehen. Unbehebbares wird separat gezählt (unfixableCritHigh) und
// als Info ausgewiesen - verschwiegen wird nichts.
// relevantRefs sind die Image-Referenzen der als CVE-relevant markierten
// Container (leer/nil ⇒ alle Docker-Funde bleiben außen vor).
func weightedVulnSummary(facts []repositories.VulnFact, weightList, listening []string, relevantRefs map[string]bool) map[string]int {
	out := map[string]int{}
	for _, f := range facts {
		if !vulnCounts(f, relevantRefs) || !f.Fixable() {
			continue
		}
		out[weightedSeverity(f, weightList, listening)]++
	}
	return out
}

// raisedVulnPackages liefert die Pakete, deren zählende Funde durch die
// Gewichtung eine Stufe HÖHER eingeordnet werden als ihre Roh-Schwere -
// sortiert und ohne Dubletten.
//
// Warum das gebraucht wird: Ampel und Sicherheitsübersicht sprechen sonst
// verschiedene Sprachen. Die Ampel meldet „2 hohe Sicherheitslücken", die
// Liste zeigt dieselben Funde als „mittel" - denn dort steht bewusst die
// unveränderte Roh-Schwere. Wer daraufhin nach hohen Funden sucht, findet
// keine, hält die Bewertung für hängengeblieben und scannt endlos neu. Mit
// den Paketnamen ist der Zusammenhang in einem Satz erklärbar.
func raisedVulnPackages(facts []repositories.VulnFact, weightList, listening []string, relevantRefs map[string]bool) []string {
	seen := map[string]bool{}
	for _, f := range facts {
		if !vulnCounts(f, relevantRefs) || !f.Fixable() {
			continue
		}
		raw := ladderIndex(domain.NormalizeSeverity(f.Severity))
		if raw < 0 {
			continue // unbekannte Schwere wird nicht verschoben
		}
		if ladderIndex(weightedSeverity(f, weightList, listening)) > raw {
			seen[f.PackageName] = true
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// unfixableCritHigh zählt die zählenden kritischen/hohen Funde OHNE
// verfügbaren Fix (effektive Schwere) - für den Info-Hinweis im Status:
// bekannt und ernst, aber ohne mögliche Handlung.
func unfixableCritHigh(facts []repositories.VulnFact, weightList, listening []string, relevantRefs map[string]bool) int {
	n := 0
	for _, f := range facts {
		if !vulnCounts(f, relevantRefs) || f.Fixable() {
			continue
		}
		switch weightedSeverity(f, weightList, listening) {
		case domain.SeverityCritical, domain.SeverityHigh:
			n++
		}
	}
	return n
}

// countedVulns zählt alle in die Bewertung einfließenden BEHEBBAREN Funde
// (jede Schwere) - die Basis für das „Sehr gut"-Kriterium TotalVulns == 0.
// Auch hier bleibt Unbehebbares außen vor: „Sehr gut" heißt „nichts mehr zu
// tun", nicht „der Hersteller hat keine offenen Baustellen" (R2-056).
func countedVulns(facts []repositories.VulnFact, relevantRefs map[string]bool) int {
	n := 0
	for _, f := range facts {
		if vulnCounts(f, relevantRefs) && f.Fixable() {
			n++
		}
	}
	return n
}

// dockerRelevantRefs löst die als CVE-relevant markierten Container-Namen
// eines Servers (CVERelevantContainers) in die Image-Referenzen auf, unter
// denen der CVE-Scan seine Docker-Funde ablegt (Vulnerability.ImageRef):
// die Container-Image-Ref wie gestartet plus alle Inventar-Refs mit derselben
// Image-ID. nil bei leerer Auswahl - dann zählt kein Docker-Fund.
func dockerRelevantRefs(servers *repositories.ServerRepository, server *domain.Server) map[string]bool {
	// Der Server ist ausgenommen: kein Docker-Fund zählt. Das sticht bewusst
	// auch die Markierung einzelner Container UND die automatische Relevanz
	// erreichbarer Container weiter unten - wer die Funde eines Servers gar
	// nicht sehen will, meint alle. Die Verantwortung dafür liegt damit
	// sichtbar bei dem, der den Schalter umlegt.
	if server.DockerCVEsIgnored {
		return nil
	}
	nameSet := map[string]bool{}
	for _, n := range splitCSVList(server.CVERelevantContainers) {
		nameSet[n] = true
	}
	containers, err := servers.FindDockerContainers(server.ID)
	if err != nil {
		return nil
	}
	// Von außen erreichbare Container zählen AUTOMATISCH mit.
	//
	// LCM gewichtet CVEs von Paketen, die auf erreichbaren Ports lauschen,
	// bereits eine Stufe hoch (ListeningPackages) - für Container fehlte
	// genau diese Logik, obwohl die Frage dort schärfer ist: Docker-Funde
	// zählen sonst GAR NICHT, außer jemand hakt den Container von Hand an.
	// Ein Container, dessen Port an der Firewall vorbei aus dem Netz
	// erreichbar ist, ist der stärkste denkbare Kandidat dafür; er
	// stillschweigend unbewertet zu lassen, wäre dieselbe Art von
	// Schönfärberei wie ein Server ohne Paketbestand in Grün.
	for i := range containers {
		c := &containers[i]
		if strings.EqualFold(c.State, "running") && c.BypassesHostFirewall() {
			nameSet[strings.ToLower(strings.TrimSpace(c.Name))] = true
		}
	}
	if len(nameSet) == 0 {
		return nil
	}
	images, _ := servers.FindDockerImages(server.ID)
	byImageID := map[string][]string{}
	for i := range images {
		if images[i].ImageID != "" {
			byImageID[images[i].ImageID] = append(byImageID[images[i].ImageID], images[i].Ref())
		}
	}
	refs := map[string]bool{}
	for i := range containers {
		c := &containers[i]
		if !nameSet[strings.ToLower(strings.TrimSpace(c.Name))] {
			continue
		}
		if c.Image != "" {
			refs[c.Image] = true
		}
		for _, r := range byImageID[c.ImageID] {
			refs[r] = true
		}
	}
	return refs
}

// splitCSVList zerlegt eine kommagetrennte Liste (z. B. ListeningPackages)
// in normalisierte Namen.
func splitCSVList(raw string) []string {
	var list []string
	for _, part := range strings.Split(raw, ",") {
		if p := strings.ToLower(strings.TrimSpace(part)); p != "" {
			list = append(list, p)
		}
	}
	return list
}
