package services

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// runWrapped führt cmd im Ausgabe-Wrapper aus und liefert jede Zeile samt
// dem Zeitpunkt, zu dem sie ankam.
func runWrapped(t *testing.T, cmd string) (lines []string, arrived []time.Duration, exit int) {
	t.Helper()
	sh := exec.Command("sh", "-c", detachBackgroundFDs(cmd))
	out, err := sh.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := sh.Start(); err != nil {
		t.Fatal(err)
	}
	scan := bufio.NewScanner(out)
	for scan.Scan() {
		lines = append(lines, scan.Text())
		arrived = append(arrived, time.Since(start))
	}
	_ = sh.Wait()
	return lines, arrived, sh.ProcessState.ExitCode()
}

// TestDetachStreamsOutput: Die Ausgabe kommt an, während das Kommando noch
// läuft - sonst hält der Job-Watchdog jeden längeren Lauf für hängend.
func TestDetachStreamsOutput(t *testing.T) {
	lines, arrived, exit := runWrapped(t, "echo eins; sleep 3; echo zwei; exit 7")
	if strings.Join(lines, ",") != "eins,zwei" {
		t.Fatalf("ausgabe %q, erwartet eins,zwei", lines)
	}
	if arrived[0] > 2*time.Second {
		t.Errorf("erste zeile kam erst nach %v - nicht gestreamt", arrived[0])
	}
	if exit != 7 {
		t.Errorf("exit-code %d, erwartet 7", exit)
	}
}

// TestDetachIgnoresBackgroundChildren: Ein Hintergrundprozess, der die
// Ausgabe erbt, hält den Lauf nicht auf (der ursprüngliche Grund für den
// Datei-Umweg).
func TestDetachIgnoresBackgroundChildren(t *testing.T) {
	start := time.Now()
	lines, _, exit := runWrapped(t, "(sleep 30; echo spaet) & echo fertig")
	if time.Since(start) > 10*time.Second {
		t.Fatalf("lauf wartete auf den hintergrundprozess (%v)", time.Since(start))
	}
	if strings.Join(lines, ",") != "fertig" || exit != 0 {
		t.Fatalf("ausgabe %q, exit %d", lines, exit)
	}
}

// TestDetachKeepsShortCommandsFast: Ein Scan besteht aus Dutzenden kurzer
// Kommandos - das Weiterreichen darf sie nicht spürbar bremsen.
func TestDetachKeepsShortCommandsFast(t *testing.T) {
	start := time.Now()
	lines, _, _ := runWrapped(t, "printf 'a\\nb\\n'")
	if took := time.Since(start); took > 500*time.Millisecond {
		t.Errorf("kurzes kommando brauchte %v", took)
	}
	if strings.Join(lines, ",") != "a,b" {
		t.Errorf("ausgabe %q", lines)
	}
}

// withTempDirs setzt die Orte der Ausgabedatei für die Dauer eines Tests.
func withTempDirs(t *testing.T, dirs ...string) {
	t.Helper()
	old := wrapperTempDirs
	wrapperTempDirs = dirs
	t.Cleanup(func() { wrapperTempDirs = old })
}

// unwritableDirs liefert Orte, an denen mktemp scheitert: einen fehlenden
// und - außer für root, den Rechte nicht aufhalten - einen schreibgeschützten.
func unwritableDirs(t *testing.T) []string {
	t.Helper()
	dirs := []string{filepath.Join(t.TempDir(), "fehlt")}
	if os.Geteuid() != 0 {
		ro := filepath.Join(t.TempDir(), "nurlesen")
		if err := os.Mkdir(ro, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(ro, 0o700) })
		dirs = append(dirs, ro)
	}
	return dirs
}

// TestDetachFallsBackToNextTempDir: Ist /tmp voll oder gesperrt, weicht der
// Wrapper auf den nächsten Ort aus und räumt die Datei dort wieder ab.
func TestDetachFallsBackToNextTempDir(t *testing.T) {
	spare := t.TempDir()
	withTempDirs(t, append(unwritableDirs(t), spare)...)

	lines, _, exit := runWrapped(t, `echo "lf=$__lf"; exit 5`)
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "lf="+spare+"/lcm.") {
		t.Fatalf("ausgabe %q, erwartet eine datei unter %s", lines, spare)
	}
	if exit != 5 {
		t.Errorf("exit-code %d, erwartet 5", exit)
	}
	if left, _ := os.ReadDir(spare); len(left) != 0 {
		t.Errorf("ausgabedatei liegt noch da: %v", left)
	}
}

// TestDetachRunsDirectlyWithoutTempDir: Ist kein Ort beschreibbar, läuft das
// Kommando direkt - früher leitete die Subshell nach "" um, und LCM kam auf
// einem vollen Server an kein einziges Kommando mehr heran.
func TestDetachRunsDirectlyWithoutTempDir(t *testing.T) {
	withTempDirs(t, unwritableDirs(t)...)

	lines, _, exit := runWrapped(t, `echo "lf=$__lf"; echo fehler >&2; exit 4`)
	if strings.Join(lines, ",") != "lf=,fehler" {
		t.Fatalf("ausgabe %q, erwartet lf=,fehler", lines)
	}
	if exit != 4 {
		t.Errorf("exit-code %d, erwartet 4", exit)
	}
}
