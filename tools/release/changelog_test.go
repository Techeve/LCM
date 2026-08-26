package main

import (
	"strings"
	"testing"
)

const betaChangelogBody = `## v1.30.8-beta.1 - 2026-08-25

### 🚀 Features

- **docker**: Releases zusätzlich nach Docker Hub (aaaa1111)

### 🐛 Bugfixes

- **ci**: Gegenprobe des Enterprise-Deploys kann grün werden (bbbb2222)

## v1.30.7-beta.1 - 2026-08-24

### 🐛 Bugfixes

- **release**: develop leitet keine finale Version mehr ab (cccc3333)

## v1.30.6 - 2026-08-20

### 🚀 Features

- **profiles**: Berechtigungsprofile kopieren (dddd4444)
`

// TestBetasGehenImFinaleAuf ist der Kern der Zusammenlegung: community steht
// auf 1.30.6, beta hat 1.30.7-beta.1 und 1.30.8-beta.1 durchlaufen - der
// Abschnitt zu v1.30.8 trägt ALLE deren Einträge, die Beta-Abschnitte
// verschwinden, und v1.30.6 bleibt unangetastet stehen.
func TestBetasGehenImFinaleAuf(t *testing.T) {
	fresh := "## v1.30.8 - 2026-08-26\n" // keine neuen Commits seit der letzten Beta
	snippet, rest := ConsolidateFinal(fresh, "1.30.8", "2026-08-26", betaChangelogBody)

	for _, want := range []string{
		"## v1.30.8 - 2026-08-26",
		"- **docker**: Releases zusätzlich nach Docker Hub (aaaa1111)",
		"- **ci**: Gegenprobe des Enterprise-Deploys kann grün werden (bbbb2222)",
		"- **release**: develop leitet keine finale Version mehr ab (cccc3333)",
	} {
		if !strings.Contains(snippet, want) {
			t.Errorf("dem Finale fehlt %q:\n%s", want, snippet)
		}
	}
	if strings.Contains(snippet, "beta.1") {
		t.Errorf("das Finale nennt noch Beta-Versionen:\n%s", snippet)
	}
	if !strings.HasPrefix(rest, "## v1.30.6 - 2026-08-20") {
		t.Errorf("der Rest beginnt nicht mit dem letzten Finale:\n%.120s", rest)
	}
	if strings.Contains(rest, "beta") {
		t.Errorf("die aufgegangenen Beta-Abschnitte stehen noch im Rest:\n%s", rest)
	}
	if !strings.Contains(rest, "dddd4444") {
		t.Error("der bestehende v1.30.6-Abschnitt wurde beschädigt")
	}
}

// TestNeueCommitsUndBetasLandenInDenselbenRubriken: Commits seit der letzten
// Beta und die Beta-Einträge sortieren sich in gemeinsame Rubriken - nicht in
// doppelte Überschriften.
func TestNeueCommitsUndBetasLandenInDenselbenRubriken(t *testing.T) {
	fresh := "## v1.30.8 - 2026-08-26\n\n### 🐛 Bugfixes\n\n- **api**: frischer Fix nach der letzten Beta (eeee5555)\n"
	snippet, _ := ConsolidateFinal(fresh, "1.30.8", "2026-08-26", betaChangelogBody)

	if got := strings.Count(snippet, "### 🐛 Bugfixes"); got != 1 {
		t.Errorf("Rubrik Bugfixes kommt %d-mal vor, erwartet 1:\n%s", got, snippet)
	}
	for _, want := range []string{"eeee5555", "bbbb2222", "cccc3333"} {
		if !strings.Contains(snippet, want) {
			t.Errorf("Eintrag %s fehlt", want)
		}
	}
	// Rubriken-Reihenfolge bleibt kanonisch: Features vor Bugfixes.
	if strings.Index(snippet, "### 🚀 Features") > strings.Index(snippet, "### 🐛 Bugfixes") {
		t.Errorf("Rubriken nicht in kanonischer Reihenfolge:\n%s", snippet)
	}
}

// TestOhneFuehrendeBetasBleibtAllesBeimAlten: Steht oben schon ein finaler
// Abschnitt (Wartungszweig, Hotfix), wird nichts eingesammelt.
func TestOhneFuehrendeBetasBleibtAllesBeimAlten(t *testing.T) {
	body := "## v1.30.6 - 2026-08-20\n\n### 🚀 Features\n\n- **profiles**: Kopieren (dddd4444)\n"
	fresh := "## v1.30.7 - 2026-08-26\n\n### 🐛 Bugfixes\n\n- **x**: Hotfix (ffff6666)\n"
	snippet, rest := ConsolidateFinal(fresh, "1.30.7", "2026-08-26", body)
	if snippet != fresh {
		t.Errorf("ohne führende Betas darf sich der Abschnitt nicht ändern:\n%s", snippet)
	}
	if rest != body {
		t.Errorf("ohne führende Betas darf sich der Rest nicht ändern:\n%s", rest)
	}
}
