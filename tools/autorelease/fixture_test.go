package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func testGit() gitRunner {
	return gitRunner{name: "Test", email: "test@example.com"}
}

// mustGit führt git aus und bricht den Test bei einem Fehler ab.
func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := testGit().run(dir, args...)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// commitFiles schreibt Dateien und committet sie.
func commitFiles(t *testing.T, dir, message string, files map[string]string) {
	t.Helper()
	writeFiles(t, dir, files)
	mustGit(t, dir, "add", "-A")
	mustGit(t, dir, "commit", "--quiet", "-m", message)
}

func requireTools(t *testing.T) {
	t.Helper()
	// Hermetisch: weder Hooks noch Signierzwang aus der Git-Konfiguration des Rechners.
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, tool := range []string{"git", "go"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s nicht verfügbar", tool)
		}
	}
}

// mergeFixture legt ein Repository an, in dem "ours" und "theirs" von einem
// gemeinsamen Stand abzweigen, und startet den Merge von theirs in ours.
// Zurück kommt das Arbeitsverzeichnis mit laufendem Merge.
func mergeFixture(t *testing.T, base, ours, theirs map[string]string) string {
	t.Helper()
	requireTools(t)
	dir := t.TempDir()
	mustGit(t, dir, "init", "--quiet", "-b", "ours")
	commitFiles(t, dir, "base", base)
	mustGit(t, dir, "switch", "--quiet", "-c", "theirs")
	commitFiles(t, dir, "theirs", theirs)
	mustGit(t, dir, "switch", "--quiet", "ours")
	commitFiles(t, dir, "ours", ours)
	if _, err := testGit().exec(dir, "merge", "--no-ff", "--no-commit", "theirs"); err == nil {
		t.Fatal("der Merge hätte Konflikte haben müssen")
	}
	return dir
}

func readFile(t *testing.T, dir, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
