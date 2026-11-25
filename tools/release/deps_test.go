package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func parsed(subjects ...string) []Commit {
	var out []Commit
	for i, s := range subjects {
		out = append(out, ParseCommit("abcdef12345"+string(rune('a'+i)), s, ""))
	}
	return out
}

// Abhängigkeits-Updates der Oberfläche und Werkzeuge entscheiden, ob ein
// Wochen-Release entsteht: fix(deps) ja, chore(deps) nein.
func TestDecideBumpAbhaengigkeiten(t *testing.T) {
	if got := DecideBump(parsed("fix(deps): update module github.com/x/y to v1.2.3")); got != BumpPatch {
		t.Errorf("fix(deps): %s, erwartet patch", got)
	}
	if got := DecideBump(parsed("chore(deps): update dependency vitest to v5.0.6", "chore(deps): update node.js")); got != BumpNone {
		t.Errorf("chore(deps): %s, erwartet none", got)
	}
}

func TestIsSecurityCommit(t *testing.T) {
	for subject, want := range map[string]bool{
		"fix(deps): update module x to v1.0.1 [security]": true,
		"fix(deps): update module x to v1.0.1 [SECURITY]": true,
		"fix(deps): update module x to v1.0.1":            false,
		"fix(core): Sicherheitslücke geschlossen":         false,
	} {
		if got := IsSecurityCommit(ParseCommit("h", subject, "")); got != want {
			t.Errorf("%q: %v, erwartet %v", subject, got, want)
		}
	}
}

func TestChangelogFasstAbhaengigkeitenZusammen(t *testing.T) {
	commits := parsed(
		"fix(deps): update module github.com/a/b to v1.2.3",
		"fix(deps): update dependency lit to v3.3.4",
		"chore(deps): update dependency vite to v8.3.8",
		"chore(deps): update dependency vitest to v5.0.6",
		"fix(ui): Fenster schließt wieder",
	)
	got := RenderChangelog("2.3.3", time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC), commits)
	want := "## v2.3.3 - 2026-11-30\n" +
		"\n### 🐛 Bugfixes\n\n- **ui**: Fenster schließt wieder (abcdef12)\n- Abhängigkeiten aktualisiert\n" +
		"\n### 🔧 Sonstiges\n\n- Entwicklungswerkzeuge aktualisiert\n"
	if got != want {
		t.Errorf("Changelog:\n%s\nerwartet:\n%s", got, want)
	}
	if strings.Contains(got, "github.com") || strings.Contains(got, "vitest") {
		t.Errorf("englische Renovate-Betreffe im Changelog:\n%s", got)
	}
}

func TestChangelogNenntSicherheitskorrekturen(t *testing.T) {
	commits := parsed(
		"fix(deps): update module github.com/a/b to v1.2.3",
		"fix(deps): update module golang.org/x/net to v0.99.0 [security]",
	)
	got := RenderChangelog("2.3.3", time.Now(), commits)
	if !strings.Contains(got, "- "+depsSecurityLine+"\n") || strings.Contains(got, "- "+depsLine+"\n") {
		t.Errorf("erwartet nur die Zeile mit Sicherheitskorrekturen:\n%s", got)
	}
}

// Ein Sicherheitsmerkmal an einem Entwicklungswerkzeug ändert nichts am Produkt
// und darf die Zeile für Anwender nicht färben.
func TestChangelogSicherheitAmWerkzeugFaerbtNicht(t *testing.T) {
	commits := parsed(
		"fix(deps): update module github.com/a/b to v1.2.3",
		"chore(deps): update dependency vite to v8.3.8 [security]",
	)
	got := RenderChangelog("2.3.3", time.Now(), commits)
	if strings.Contains(got, "Sicherheitskorrekturen") {
		t.Errorf("Werkzeug-Update als Sicherheitskorrektur ausgewiesen:\n%s", got)
	}
}

func TestNurWerkzeugeKeineBugfixRubrik(t *testing.T) {
	got := RenderChangelog("2.3.3", time.Now(), parsed("chore(deps): update dependency vite to v8.3.8"))
	if strings.Contains(got, "Bugfixes") {
		t.Errorf("Bugfix-Rubrik ohne Produktänderung:\n%s", got)
	}
}

func TestZusammengelegteAbschnitteBehaltenNurDieSicherheitszeile(t *testing.T) {
	body := "## v2.4.0-beta.2 - 2026-11-30\n\n### 🐛 Bugfixes\n\n- " + depsSecurityLine + "\n\n" +
		"## v2.4.0-beta.1 - 2026-11-23\n\n### 🐛 Bugfixes\n\n- " + depsLine + "\n"
	snippet, _ := ConsolidateFinal("## v2.4.0 - 2026-12-01\n", "2.4.0", "2026-12-01", body)
	if strings.Count(snippet, "Abhängigkeiten") != 1 || !strings.Contains(snippet, depsSecurityLine) {
		t.Errorf("erwartet genau die Sicherheitszeile:\n%s", snippet)
	}
}

// Die Sammelzeile erscheint im Fenster nach dem Update unter "Behoben".
func TestReleaseNotesAbhaengigkeitszeile(t *testing.T) {
	dir := t.TempDir()
	changelog := filepath.Join(dir, "CHANGELOG.md")
	text := "# Changelog\n\n## v2.3.3 - 2026-11-30\n\n### 🐛 Bugfixes\n\n- " + depsSecurityLine + "\n\n### 🔧 Sonstiges\n\n- " + devToolsLine + "\n"
	if err := os.WriteFile(changelog, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "notes")
	if out, err := exec.Command("sh", "../release-notes.sh", changelog, target, filepath.Join(dir, "leer")).CombinedOutput(); err != nil {
		t.Fatalf("release-notes.sh: %v\n%s", err, out)
	}
	got, err := os.ReadFile(filepath.Join(target, "2.3.3.md"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "\n### Behoben\n\n- " + depsSecurityLine + "\n"; string(got) != want {
		t.Errorf("Hinweise:\n%q\nerwartet:\n%q", got, want)
	}
}

// Von Hand geschriebene fix(deps)-Commits behalten ihre Zeile: Sie erklären
// etwas, das die Sammelzeile nicht sagt. Sie lösen trotzdem ein Release aus.
func TestHandgeschriebeneAbhaengigkeitsCommitsBleiben(t *testing.T) {
	commits := parsed(
		"fix(deps): gorilla/websocket auf den Stand mit sicherem Maskenschlüssel heben",
		"fix(deps): update module github.com/a/b to v1.2.3",
	)
	got := RenderChangelog("2.3.3", time.Now(), commits)
	for _, want := range []string{"- **deps**: gorilla/websocket auf den Stand mit sicherem Maskenschlüssel heben (abcdef12)", "- " + depsLine} {
		if !strings.Contains(got, want) {
			t.Errorf("fehlt %q:\n%s", want, got)
		}
	}
}
