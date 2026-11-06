package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const notesChangelog = `# Changelog

## v2.0.1 - 2026-11-08

### 🐛 Bugfixes

- **api**: Port wird geprüft (aaaa1111)

## v2.0.0-beta.2 - 2026-11-06

### 🐛 Bugfixes

- **ui**: fenster schließt wieder (bbbb2222)

### ♻️ Refactoring

- **ui**: Bausteine zusammengelegt (cccc3333)

### 🔧 Sonstiges

- **e2e**: Suite läuft in WebKit (dddd4444)

## v2.0.0-beta.1 - 2026-11-05

### 💥 Breaking Changes

- **api**: Bearer-Login entfällt (eeee5555)

### 🚀 Features

- Neue Oberfläche (ffff6666)
`

// TestReleaseNotesScript: Der handgeschriebene Text steht über allen Versionen
// seiner Reihe, der Auszug nennt nur, was Anwender betrifft.
func TestReleaseNotesScript(t *testing.T) {
	dir := t.TempDir()
	changelog := filepath.Join(dir, "CHANGELOG.md")
	info := filepath.Join(dir, "release-info")
	target := filepath.Join(dir, "notes")
	if err := os.WriteFile(changelog, []byte(notesChangelog), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(info, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(info, "2.0.0.md"), []byte("Handtext.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("sh", "../release-notes.sh", changelog, target, info).CombinedOutput(); err != nil {
		t.Fatalf("release-notes.sh: %v\n%s", err, out)
	}

	for version, want := range map[string]string{
		"2.0.1":        "\n### Behoben\n\n- Port wird geprüft\n",
		"2.0.0-beta.2": "Handtext.\n\n### Behoben\n\n- Fenster schließt wieder\n",
		"2.0.0-beta.1": "Handtext.\n\n### Das musst du wissen\n\n- Bearer-Login entfällt\n\n### Neu\n\n- Neue Oberfläche\n",
	} {
		got, err := os.ReadFile(filepath.Join(target, version+".md"))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Errorf("%s:\n%q\nerwartet:\n%q", version, got, want)
		}
	}
}
