package main

import "strings"

// Renovate liefert Commits wie "fix(deps): update module github.com/x/y to
// v1.2.3". Als Liste englischer Betreffe sind sie für Anwender wertlos, und
// ein Wochen-Release trägt davon Dutzende. Der Changelog fasst deshalb alle
// Abhängigkeits-Commits einer Version zu je einer Zeile zusammen.
//
//	fix(deps)    geht ins Produkt (Go-Module, Toolchain, ausgelieferte
//	             Oberfläche und Images) -> "Abhängigkeiten aktualisiert" unter
//	             Bugfixes, löst ein Patch-Release aus
//	chore(deps)  Entwicklungs- und CI-Werkzeuge -> eine Zeile unter Sonstiges,
//	             löst nichts aus
const (
	depsScope = "deps"

	depsLine         = "Abhängigkeiten aktualisiert"
	depsSecurityLine = "Abhängigkeiten mit Sicherheitskorrekturen aktualisiert"
	devToolsLine     = "Entwicklungswerkzeuge aktualisiert"

	// securityMarker hängt Renovate an den Betreff von Sicherheitsupdates
	// (commitMessageSuffix der vulnerabilityAlerts in renovate.json).
	securityMarker = "[security]"
)

// IsSecurityCommit sagt, ob der Betreff das Sicherheitsmerkmal trägt.
// Groß-/Kleinschreibung zählt nicht: Renovates eigene Vorgabe ist "[SECURITY]".
func IsSecurityCommit(c Commit) bool {
	return strings.Contains(strings.ToLower(c.Subject), securityMarker)
}

// renovateVerbs sind die Anfänge der Betreffe, die Renovate erzeugt. Ein von
// Hand geschriebener fix(deps)-Commit mit Erklärung ("gorilla/websocket auf den
// Stand mit sicherem Maskenschlüssel heben") bleibt dagegen als eigene Zeile
// stehen: Er sagt Anwendern etwas, das die Sammelzeile verschluckte.
var renovateVerbs = []string{"update ", "pin ", "replace ", "migrate ", "lock file maintenance"}

func isDepsCommit(c Commit) bool {
	if c.Scope != depsScope || c.Breaking || (c.Type != "fix" && c.Type != "chore") {
		return false
	}
	for _, verb := range renovateVerbs {
		if strings.HasPrefix(c.Subject, verb) {
			return true
		}
	}
	return false
}

// CollapseDeps ersetzt alle Renovate-Commits für Abhängigkeiten durch höchstens
// je eine Sammelzeile. Die Sammelzeilen tragen keinen Hash: Sie stehen für viele Commits.
func CollapseDeps(commits []Commit) []Commit {
	var out []Commit
	var product, tools, productSecurity bool
	for _, c := range commits {
		if !isDepsCommit(c) {
			out = append(out, c)
			continue
		}
		if c.Type == "fix" {
			product = true
			productSecurity = productSecurity || IsSecurityCommit(c)
		} else {
			tools = true
		}
	}
	if product {
		line := depsLine
		if productSecurity {
			line = depsSecurityLine
		}
		out = append(out, Commit{Type: "fix", Subject: line})
	}
	if tools {
		out = append(out, Commit{Type: "chore", Subject: devToolsLine})
	}
	return out
}

// dropPlainDepsLine entfernt die einfache Abhängigkeitszeile, wenn dieselbe
// Rubrik schon die mit Sicherheitskorrekturen trägt (zusammengelegte
// Abschnitte aus mehreren Versionen).
func dropPlainDepsLine(entries []string) []string {
	plain, security := "- "+depsLine, "- "+depsSecurityLine
	hasSecurity := false
	for _, e := range entries {
		hasSecurity = hasSecurity || e == security
	}
	if !hasSecurity {
		return entries
	}
	out := entries[:0]
	for _, e := range entries {
		if e != plain {
			out = append(out, e)
		}
	}
	return out
}
