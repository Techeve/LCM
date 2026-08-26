package main

import (
	"regexp"
	"sort"
	"strings"
)

// Beim Community-Release sollen die Vorabversionen im Finale aufgehen: Steht
// community auf 1.30.6 und beta hat 1.30.7-beta.1 und 1.30.8-beta.1
// durchlaufen, trägt der Abschnitt zu v1.30.8 alle deren Einträge - statt
// leer zu sein ("0 Commits seit dem letzten Tag"), während der Inhalt in
// Beta-Abschnitten darüber verstreut steht.
//
// Zusammengelegt werden genau die FÜHRENDEN Vorabversions-Abschnitte des
// Changelogs - bis zum ersten finalen Abschnitt. Auf dem Release-Zug sind das
// exakt die Betas seit dem letzten Finale; ein Wartungszweig (enterprise), auf
// dem oben ein finaler Abschnitt steht, bleibt dadurch unberührt.

// versionSection ist ein geparster "## vX.Y.Z"-Abschnitt: die Rubriken
// ("### 🚀 Features" …) mit ihren Einträgen als Rohzeilen.
type versionSection struct {
	entriesByCategory map[string][]string
	categoryOrder     []string
}

var sectionHeader = regexp.MustCompile(`^## v(\S+) - `)

// splitLeadingPrereleases trennt die führenden Vorabversions-Abschnitte vom
// Rest des Changelogs (ohne Kopfzeile "# Changelog"). Der Rest beginnt mit dem
// ersten finalen Abschnitt - oder ist leer, wenn es keinen gibt.
func splitLeadingPrereleases(changelogBody string) (leading []versionSection, rest string) {
	lines := strings.Split(changelogBody, "\n")
	i := 0
	for i < len(lines) {
		// Bis zum nächsten Abschnittskopf vorspulen (Leerzeilen zwischen
		// Abschnitten überspringen).
		if strings.TrimSpace(lines[i]) == "" {
			i++
			continue
		}
		m := sectionHeader.FindStringSubmatch(lines[i])
		if m == nil || !strings.Contains(m[1], "-") {
			break // finaler Abschnitt oder etwas anderes: hier beginnt der Rest
		}
		end := i + 1
		for end < len(lines) && !strings.HasPrefix(lines[end], "## ") {
			end++
		}
		leading = append(leading, parseSection(lines[i+1:end]))
		i = end
	}
	return leading, strings.TrimLeft(strings.Join(lines[i:], "\n"), "\n")
}

// parseSection liest die Rubriken eines Abschnitts. Zeilen außerhalb von
// "- "-Einträgen (Leerzeilen) fallen weg.
func parseSection(lines []string) versionSection {
	s := versionSection{entriesByCategory: map[string][]string{}}
	category := ""
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "### "):
			category = strings.TrimPrefix(line, "### ")
			if _, seen := s.entriesByCategory[category]; !seen {
				s.categoryOrder = append(s.categoryOrder, category)
				s.entriesByCategory[category] = nil
			}
		case strings.HasPrefix(line, "- ") && category != "":
			s.entriesByCategory[category] = append(s.entriesByCategory[category], line)
		}
	}
	return s
}

// mergeSections legt Abschnitte zu einem zusammen: Rubriken in der
// kanonischen Reihenfolge des Werkzeugs, Einträge je Rubrik sortiert und
// ohne exakte Doppel. Unbekannte Rubriken (von Hand nachgetragen) hängen
// hinten an, damit nichts verloren geht.
func mergeSections(version, date string, parts []versionSection) string {
	merged := versionSection{entriesByCategory: map[string][]string{}}
	canonical := make([]string, 0, len(sections))
	for _, s := range sections {
		canonical = append(canonical, s.Title)
	}
	for _, p := range parts {
		for _, cat := range p.categoryOrder {
			if _, seen := merged.entriesByCategory[cat]; !seen {
				merged.categoryOrder = append(merged.categoryOrder, cat)
			}
			merged.entriesByCategory[cat] = append(merged.entriesByCategory[cat], p.entriesByCategory[cat]...)
		}
	}
	order := make([]string, 0, len(merged.categoryOrder))
	for _, cat := range canonical {
		if _, ok := merged.entriesByCategory[cat]; ok {
			order = append(order, cat)
		}
	}
	for _, cat := range merged.categoryOrder {
		known := false
		for _, c := range canonical {
			if c == cat {
				known = true
				break
			}
		}
		if !known {
			order = append(order, cat)
		}
	}

	var b strings.Builder
	b.WriteString("## v" + version + " - " + date + "\n")
	for _, cat := range order {
		entries := dedupSorted(merged.entriesByCategory[cat])
		if len(entries) == 0 {
			continue
		}
		b.WriteString("\n### " + cat + "\n\n")
		for _, e := range entries {
			b.WriteString(e + "\n")
		}
	}
	return b.String()
}

func dedupSorted(entries []string) []string {
	sort.Strings(entries)
	out := entries[:0]
	for i, e := range entries {
		if i == 0 || e != entries[i-1] {
			out = append(out, e)
		}
	}
	return out
}

// ConsolidateFinal baut den Abschnitt eines FINALEN Releases: der frisch
// erzeugte Abschnitt (Commits seit dem letzten Tag) plus alle führenden
// Vorabversions-Abschnitte des bestehenden Changelogs. Zurück kommen der
// zusammengelegte Abschnitt und der Rest des Changelogs ohne die
// aufgegangenen Abschnitte.
func ConsolidateFinal(freshSnippet, version, date, changelogBody string) (snippet, rest string) {
	leading, rest := splitLeadingPrereleases(changelogBody)
	if len(leading) == 0 {
		return freshSnippet, rest
	}
	parts := []versionSection{parseSection(strings.Split(freshSnippet, "\n"))}
	parts = append(parts, leading...)
	return mergeSections(version, date, parts), rest
}
