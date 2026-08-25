package services

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Die Skripte, die die vorrangige PermitRootLogin-Zeile stilllegen und wieder
// freigeben, werden hier wirklich ausgeführt - gegen eine echte Datei, mit
// sshd/systemctl als Attrappen im PATH. Ein reiner Textvergleich würde nicht
// zeigen, ob der sed-Ausdruck trifft, was er treffen soll (und nur das).

// rootLoginSandbox ist ein Mini-/etc/ssh mit Attrappen.
type rootLoginSandbox struct {
	dir string
	cfg string
	bin string
}

// newRootLoginSandbox legt eine sshd_config an, wie sie auf einem
// Cloud-Image liegt: die Direktive steht VOR dem Include der Drop-ins, und
// daneben gibt es eine bereits auskommentierte Zeile.
func newRootLoginSandbox(t *testing.T, sshdOK bool) *rootLoginSandbox {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "sshd_config")
	body := "# Beispielkonfiguration\nPort 22\nPermitRootLogin yes\n" +
		"#PermitRootLogin prohibit-password\n" +
		"   PermitRootLogin without-password\n" +
		"Include /etc/ssh/sshd_config.d/*.conf\n"
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	rc := "0"
	if !sshdOK {
		rc = "1"
	}
	// sshd -t entscheidet über Übernahme oder Rollback; systemctl/service
	// schlucken den Reload.
	for name, script := range map[string]string{
		"sshd":       "#!/bin/sh\nexit " + rc + "\n",
		"systemctl":  "#!/bin/sh\nexit 0\n",
		"service":    "#!/bin/sh\nexit 0\n",
		"rc-service": "#!/bin/sh\nexit 0\n",
	} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return &rootLoginSandbox{dir: dir, cfg: cfg, bin: bin}
}

func (s *rootLoginSandbox) run(t *testing.T, script string) (string, error) {
	t.Helper()
	cmd := exec.Command("sh", "-c", script)
	cmd.Env = append(os.Environ(), "PATH="+s.bin+":"+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (s *rootLoginSandbox) read(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(s.cfg)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestNeutralizeRootLoginLegtNurDieAktivenZeilenStill: Die aktiven
// PermitRootLogin-Zeilen werden markiert, die bereits auskommentierte bleibt
// unangetastet - sonst käme sie beim Zurücknehmen fälschlich als aktive
// Zeile wieder hoch.
func TestNeutralizeRootLoginLegtNurDieAktivenZeilenStill(t *testing.T) {
	sb := newRootLoginSandbox(t, true)

	out, err := sb.run(t, neutralizeRootLoginScriptIn(sb.cfg))
	if err != nil {
		t.Fatalf("stilllegen fehlgeschlagen: %v\n%s", err, out)
	}
	got := sb.read(t)

	if strings.Contains(got, "\nPermitRootLogin yes") {
		t.Errorf("die vorrangige zeile ist noch aktiv:\n%s", got)
	}
	if !strings.Contains(got, rootLoginMarker+"PermitRootLogin yes") {
		t.Errorf("die zeile wurde nicht markiert stillgelegt:\n%s", got)
	}
	// Eingerückte Zeile ebenfalls - sshd wertet sie genauso aus.
	if !strings.Contains(got, rootLoginMarker+"   PermitRootLogin without-password") {
		t.Errorf("die eingerückte zeile wurde übersehen:\n%s", got)
	}
	// Die schon auskommentierte Zeile darf NICHT doppelt markiert werden.
	if strings.Contains(got, rootLoginMarker+"#PermitRootLogin") {
		t.Errorf("eine bereits auskommentierte zeile wurde angefasst:\n%s", got)
	}
	// Alles außerhalb der Direktive bleibt, wie es war.
	if !strings.Contains(got, "Port 22") || !strings.Contains(got, "Include /etc/ssh/sshd_config.d/*.conf") {
		t.Errorf("fremde zeilen wurden verändert:\n%s", got)
	}
	if _, err := os.Stat(sb.cfg + ".lcmbak"); err == nil {
		t.Error("die sicherung sollte nach erfolg aufgeräumt sein")
	}
}

// TestRootLoginStilllegenUndZuruecknehmen: Der Rückweg stellt den
// Ausgangszustand ZEICHENGENAU wieder her. Das ist die Zusage, mit der LCM
// überhaupt an eine fremde Datei gehen darf.
func TestRootLoginStilllegenUndZuruecknehmen(t *testing.T) {
	sb := newRootLoginSandbox(t, true)
	before := sb.read(t)

	if out, err := sb.run(t, neutralizeRootLoginScriptIn(sb.cfg)); err != nil {
		t.Fatalf("stilllegen: %v\n%s", err, out)
	}
	if out, err := sb.run(t, restoreRootLoginScriptIn(sb.cfg)); err != nil {
		t.Fatalf("zurücknehmen: %v\n%s", err, out)
	}

	if after := sb.read(t); after != before {
		t.Errorf("die datei kam nicht im ursprungszustand zurück:\n--- vorher ---\n%s\n--- nachher ---\n%s", before, after)
	}
}

// TestRestoreRootLoginOhneMarkierungAendertNichts: Wo LCM nichts stillgelegt
// hat, wird auch nichts angefasst - die Freigabe darf keine fremden
// Kommentarzeilen aktivieren.
func TestRestoreRootLoginOhneMarkierungAendertNichts(t *testing.T) {
	sb := newRootLoginSandbox(t, true)
	before := sb.read(t)

	if out, err := sb.run(t, restoreRootLoginScriptIn(sb.cfg)); err != nil {
		t.Fatalf("zurücknehmen ohne markierung sollte folgenlos sein: %v\n%s", err, out)
	}
	if after := sb.read(t); after != before {
		t.Errorf("die datei wurde ohne markierung verändert:\n%s", after)
	}
}

// TestNeutralizeRootLoginRolltZurueckWennSshdAblehnt: Lehnt sshd die
// geänderte Datei ab, bleibt der Ausgangszustand bestehen und das Skript
// scheitert. Eine kaputte sshd-Konfiguration darf nie zurückbleiben.
func TestNeutralizeRootLoginRolltZurueckWennSshdAblehnt(t *testing.T) {
	sb := newRootLoginSandbox(t, false)
	before := sb.read(t)

	out, err := sb.run(t, neutralizeRootLoginScriptIn(sb.cfg))
	if err == nil {
		t.Fatalf("bei abgelehnter konfiguration muss das skript scheitern:\n%s", out)
	}
	if !strings.Contains(out, "zurückgerollt") {
		t.Errorf("der rollback wird nicht gemeldet:\n%s", out)
	}
	if after := sb.read(t); after != before {
		t.Errorf("die datei blieb verändert zurück:\n%s", after)
	}
	if _, err := os.Stat(sb.cfg + ".lcmbak"); err == nil {
		t.Error("die sicherung sollte nach dem rollback wieder die aktive datei sein")
	}
}
