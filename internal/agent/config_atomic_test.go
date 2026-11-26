package agent

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func atomicTestConfig(key string) *Config {
	return &Config{URL: "https://lcm.example:8443", AgentID: "a", Secret: "s", KeyPin: key}
}

// TestSaveBehaeltAlteDateiBeiAbbruch: Bricht das Schreiben mitten in der
// Zwischendatei ab, steht in agent.json weiter der alte Inhalt, und es bleibt
// keine Zwischendatei liegen.
func TestSaveBehaeltAlteDateiBeiAbbruch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.json")
	if err := atomicTestConfig("alt").Save(path); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)

	failure := errors.New("platte voll")
	writeTemp = func(f *os.File, data []byte) error {
		_, _ = f.Write(data[:len(data)/2])
		return failure
	}
	t.Cleanup(func() {
		writeTemp = func(f *os.File, data []byte) error { _, err := f.Write(data); return err }
	})

	if err := atomicTestConfig("neu").Save(path); !errors.Is(err, failure) {
		t.Fatalf("Fehler %v, erwartet %v", err, failure)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Errorf("Datei verändert:\n%s", after)
	}
	if _, err := LoadConfig(path); err != nil {
		t.Errorf("alte Datei nicht mehr lesbar: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("Zwischendatei liegt noch im Verzeichnis: %v", entries)
	}
}

// TestSaveRechte: Eine neue Datei bekommt 0600, eine bestehende behält ihre
// Rechte auch nach dem Ersetzen.
func TestSaveRechte(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.json")
	if err := atomicTestConfig("eins").Save(path); err != nil {
		t.Fatal(err)
	}
	assertMode(t, path, 0o600)

	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := atomicTestConfig("zwei").Save(path); err != nil {
		t.Fatal(err)
	}
	assertMode(t, path, 0o640)
	saved, err := LoadConfig(path)
	if err != nil || saved.KeyPin != "zwei" {
		t.Errorf("gespeichert %+v, Fehler %v", saved, err)
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("Rechte %04o, erwartet %04o", got, want)
	}
}

// TestWarnLooseMode: Bei 0644 steht eine Warnung im Journal und die Rechte
// bleiben, wie sie sind; bei 0600 und ohne Datei bleibt es still.
func TestWarnLooseMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.json")
	if err := atomicTestConfig("k").Save(path); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))

	WarnLooseMode(path, log)
	WarnLooseMode(filepath.Join(t.TempDir(), "fehlt.json"), log)
	if buf.Len() != 0 {
		t.Fatalf("Warnung bei 0600: %s", buf.String())
	}

	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	WarnLooseMode(path, log)
	if out := buf.String(); !strings.Contains(out, "level=WARN") || !strings.Contains(out, "0644") {
		t.Errorf("Warnung fehlt: %q", out)
	}
	assertMode(t, path, 0o644)
}
