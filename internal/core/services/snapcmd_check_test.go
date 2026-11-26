package services

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeSnap ist ein `snap`, das sich wie snapd mit laufender Anwendung
// verhält: `snap refresh` meldet „All snaps up to date." und endet mit 0,
// obwohl yq weiter ein Update hat. foo ist zurückgehalten (--hold).
const fakeSnap = `#!/bin/sh
case "$*" in
"refresh --list") printf '%s' "$SNAP_PENDING" ;;
refresh*) echo "All snaps up to date." ;;
list) printf 'Name  Version  Rev   Tracking       Publisher  Notes\nyq    v4.49.2  2759  latest/stable  mikefarah  -\nfoo   1.0      12    latest/stable  someone    held\nbar   2.0      7     latest/stable  someone    -\n' ;;
esac
`

const pendingYqFoo = "Name  Version  Rev   Size    Publisher  Notes\n" +
	"yq    v4.54.3  3003  21.6MB  mikefarah  -\n" +
	"foo   1.1      13    1MB     someone    -\n"

// runWithFakeSnap führt script mit fakeSnap im PATH aus.
func runWithFakeSnap(t *testing.T, script, pending string) (string, int) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "snap"), []byte(fakeSnap), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", "-c", script)
	cmd.Env = []string{"PATH=" + dir + ":/usr/bin:/bin", "SNAP_PENDING=" + pending}
	out, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return string(out), exitErr.ExitCode()
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(out), 0
}

// TestSnapRefreshMeldetVerschobenesUpdate: snapd verschiebt das Update eines
// Snaps, dessen Anwendung läuft, und meldet trotzdem Erfolg (nachgestellt auf
// Ubuntu 24.04, snapd 2.78). Der Lauf darf dann nicht grün enden.
func TestSnapRefreshMeldetVerschobenesUpdate(t *testing.T) {
	out, code := runWithFakeSnap(t, snapRefreshAllScript(), pendingYqFoo)
	if code == 0 {
		t.Fatalf("Lauf endete mit 0, obwohl yq nicht aktualisiert wurde:\n%s", out)
	}
	if !strings.Contains(out, "yq") {
		t.Errorf("Meldung nennt yq nicht:\n%s", out)
	}
	if strings.Contains(out, "foo") {
		t.Errorf("das zurückgehaltene foo darf nicht als Fehler zählen:\n%s", out)
	}
}

// TestSnapRefreshEinzelnPrueftNurDieGenannten: Beim Aktualisieren einzelner
// Snaps zählen nur diese - ein offenes Update eines anderen Snaps nicht.
func TestSnapRefreshEinzelnPrueftNurDieGenannten(t *testing.T) {
	if out, code := runWithFakeSnap(t, snapRefreshScript([]string{"bar"}), pendingYqFoo); code != 0 {
		t.Errorf("bar ist aktuell, der Lauf endete trotzdem mit %d:\n%s", code, out)
	}
	if _, code := runWithFakeSnap(t, snapRefreshScript([]string{"yq"}), pendingYqFoo); code == 0 {
		t.Error("yq hat weiter ein Update, der Lauf endete trotzdem mit 0")
	}
}

// TestSnapRefreshOhneOffeneUpdates: Ist alles aktuell, bleibt der Lauf grün.
func TestSnapRefreshOhneOffeneUpdates(t *testing.T) {
	if out, code := runWithFakeSnap(t, snapRefreshAllScript(), "All snaps up to date.\n"); code != 0 {
		t.Errorf("Lauf endete mit %d:\n%s", code, out)
	}
}
