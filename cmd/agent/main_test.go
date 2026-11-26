package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"LCM/internal/agent"
)

func writeStatus(t *testing.T, updated time.Time) string {
	t.Helper()
	dir := t.TempDir()
	s := agent.Status{
		Version: "2.4.0", Started: updated.Add(-time.Hour), Updated: updated,
		Connection: agent.ConnConnected, Since: updated.Add(-time.Minute), Jobs: []agent.StatusJob{},
	}
	if err := agent.WriteStatus(dir, s); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestStatusFrischLesbar(t *testing.T) {
	t.Setenv("LC_ALL", "de_DE.UTF-8")
	now := time.Now()
	var out bytes.Buffer
	if err := printStatus(writeStatus(t, now), false, now, &out); err != nil {
		t.Fatalf("frischer Stand: %v", err)
	}
	if !strings.Contains(out.String(), "verbunden seit 1 min") || !strings.Contains(out.String(), "2.4.0") {
		t.Fatalf("Ausgabe:\n%s", out.String())
	}
}

func TestStatusJSONRoh(t *testing.T) {
	now := time.Now()
	dir := writeStatus(t, now)
	var out bytes.Buffer
	if err := printStatus(dir, true, now, &out); err != nil {
		t.Fatal(err)
	}
	disk, err := os.ReadFile(filepath.Join(dir, agent.StatusFile))
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != string(disk) {
		t.Fatalf("--json weicht von der Datei ab:\n%s", out.String())
	}
	var s agent.Status
	if err := json.Unmarshal(out.Bytes(), &s); err != nil || s.Connection != agent.ConnConnected {
		t.Fatalf("kein gültiges JSON: %v", err)
	}
}

func TestStatusVeraltetSagtDassDerAgentNichtLaeuft(t *testing.T) {
	t.Setenv("LC_ALL", "de_DE.UTF-8")
	now := time.Now()
	dir := writeStatus(t, now.Add(-time.Minute))
	var out bytes.Buffer
	err := printStatus(dir, false, now, &out)
	if err == nil || !strings.Contains(err.Error(), "scheint nicht zu laufen") {
		t.Fatalf("Fehler = %v", err)
	}
	if !strings.Contains(out.String(), "Der Agent scheint nicht zu laufen - der Stand ist 1 min alt") {
		t.Fatalf("Ausgabe:\n%s", out.String())
	}
	// Auch roh bleibt der Stand lesbar; nur der Endcode meldet es.
	out.Reset()
	if err := printStatus(dir, true, now, &out); err == nil || !strings.HasPrefix(out.String(), "{") {
		t.Fatalf("--json bei veraltetem Stand: %v, %q", err, out.String())
	}
}

func TestStatusOhneDatei(t *testing.T) {
	t.Setenv("LC_ALL", "de_DE.UTF-8")
	var out bytes.Buffer
	err := printStatus(t.TempDir(), false, time.Now(), &out)
	if err == nil || !strings.Contains(err.Error(), "scheint nicht zu laufen") || out.Len() != 0 {
		t.Fatalf("Fehler = %v, Ausgabe %q", err, out.String())
	}
}

func TestStatusKommandoPruefAufrufe(t *testing.T) {
	if err := statusCommand([]string{"--xml"}, &bytes.Buffer{}); err == nil {
		t.Fatal("unbekannter Schalter muss scheitern")
	}
	if err := statusCommand([]string{"--json", "x"}, &bytes.Buffer{}); err == nil {
		t.Fatal("zu viele Angaben müssen scheitern")
	}
}

func TestRunCommandVerwirftUnbekannteSchalter(t *testing.T) {
	level := new(slog.LevelVar)
	if err := runCommand([]string{"--nope"}, slog.Default(), level); err == nil {
		t.Fatal("unbekannter Schalter muss scheitern")
	}
}
