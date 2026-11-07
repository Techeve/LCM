package services

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Die Skripte der https-Umstellung laufen hier wirklich: in einem nachgebauten
// /etc/apt, mit einer apt-get-Attrappe im PATH. Der Fall, um den es geht, ist
// der vom 07.11.2026: Ein Spiegel spricht kein HTTPS, `apt-get update` endet
// trotzdem mit 0.

const (
	aptOutputOK = "Hit:1 https://security.ubuntu.com/ubuntu noble-security InRelease\n" +
		"Reading package lists..."
	aptOutputSourceFailed = "Ign:2 https://de.archive.ubuntu.com/ubuntu noble InRelease\n" +
		"Err:2 https://de.archive.ubuntu.com/ubuntu noble InRelease\n" +
		"  Could not connect to de.archive.ubuntu.com:443 - connect (111: Connection refused)\n" +
		"W: Failed to fetch https://de.archive.ubuntu.com/ubuntu/dists/noble/InRelease\n" +
		"W: Some index files failed to download."
	aptOutputLocked = "E: Could not get lock /var/lib/apt/lists/lock"
)

// repoScriptRun ist ein Lauf eines Umstell-Skripts in der Sandbox.
type repoScriptRun struct {
	sources  string // Inhalt der Quellen-Datei nach dem Lauf
	output   string
	exitCode int
	updates  int // Aufrufe von `apt-get update`
	backup   bool
	leftover bool // *.lcm-bak ist liegen geblieben
}

// runRepoScript führt script gegen eine Quellen-Datei mit dem Inhalt sources
// aus. Die Attrappe liefert beim ersten `apt-get update` updateOutput mit
// Exit-Code updateRC, danach Erfolg.
func runRepoScript(t *testing.T, script, sources, updateOutput string, updateRC int) repoScriptRun {
	t.Helper()
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("kein sh")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	list := filepath.Join(dir, "etc/apt/sources.list.d/ubuntu.sources")
	for _, d := range []string{bin, filepath.Dir(list)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(list, []byte(sources), 0o644); err != nil {
		t.Fatal(err)
	}
	stub := `#!/bin/sh
[ "$1" = update ] || exit 0
echo x >> "$STUB_DIR/updates"
[ "$(wc -l < "$STUB_DIR/updates")" -eq 1 ] || exit 0
printf '%s\n' "$STUB_OUTPUT"
exit "$STUB_RC"
`
	if err := os.WriteFile(filepath.Join(bin, "apt-get"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}

	// `sed -i.x` versteht auch das sed von macOS; die Sandbox ersetzt die Pfade.
	script = strings.ReplaceAll(script, "sed -i ", "sed -i.sedbak ")
	script = strings.ReplaceAll(script, "/etc/apt", dir+"/etc/apt")
	script = strings.ReplaceAll(script, "/var/backups", dir+"/var/backups")
	cmd := exec.Command(sh, "-c", script)
	cmd.Env = []string{
		"PATH=" + bin + ":/usr/bin:/bin",
		"STUB_DIR=" + dir,
		"STUB_OUTPUT=" + updateOutput,
		"STUB_RC=" + itoa(updateRC),
	}
	out, runErr := cmd.CombinedOutput()
	run := repoScriptRun{output: string(out)}
	if exit, ok := runErr.(*exec.ExitError); ok {
		run.exitCode = exit.ExitCode()
	} else if runErr != nil {
		t.Fatal(runErr)
	}
	got, err := os.ReadFile(list)
	if err != nil {
		t.Fatal(err)
	}
	run.sources = string(got)
	if counted, err := os.ReadFile(filepath.Join(dir, "updates")); err == nil {
		run.updates = strings.Count(string(counted), "x")
	}
	// Das Protokoll hängt den vollen Original-Pfad an, hier also samt Sandbox.
	_, statErr := os.Stat(dir + httpsBackupDir + list)
	run.backup = statErr == nil
	run.leftover = len(findLcmBak(t, dir)) > 0
	return run
}

func findLcmBak(t *testing.T, dir string) []string {
	t.Helper()
	found, err := filepath.Glob(filepath.Join(dir, "etc/apt/sources.list.d/*.lcm-bak"))
	if err != nil {
		t.Fatal(err)
	}
	return found
}

// helperRepoScript baut aus dem Helper ein Skript, das nur das genannte
// Unterkommando ausführt.
func helperRepoScript(t *testing.T, call string) string {
	t.Helper()
	return strings.Join([]string{
		"set -u",
		helperFunktion(t, "apt_update_ok"),
		helperFunktion(t, "restore_sources"),
		helperFunktion(t, "repos_https"),
		helperFunktion(t, "repos_http"),
		"valid_repo_url() { return 0; }",
		"die() { exit 2; }",
		call,
	}, "\n")
}

func TestHTTPSUmstellungPrueftDieAusgabeVonApt(t *testing.T) {
	const before = "URIs: http://de.archive.ubuntu.com/ubuntu/\n"
	const after = "URIs: https://de.archive.ubuntu.com/ubuntu/\n"
	scripts := map[string]string{
		"skript": secureHTTPSScript(),
		"helper": helperRepoScript(t, "repos_https"),
	}
	cases := []struct {
		name     string
		output   string
		rc       int
		rollback bool
	}{
		{"alles erreichbar", aptOutputOK, 0, false},
		{"Quelle scheitert bei Exit 0", aptOutputSourceFailed, 0, true},
		{"apt bricht ab", aptOutputLocked, 100, true},
	}
	for label, script := range scripts {
		for _, tc := range cases {
			t.Run(label+"/"+tc.name, func(t *testing.T) {
				run := runRepoScript(t, script, before, tc.output, tc.rc)
				if run.leftover {
					t.Errorf("eine .lcm-bak-Datei ist liegen geblieben")
				}
				if !tc.rollback {
					if run.exitCode != 0 || run.sources != after || !run.backup {
						t.Errorf("exit %d, sicherung %v, quellen %q\n%s", run.exitCode, run.backup, run.sources, run.output)
					}
					return
				}
				if run.exitCode != 1 || run.sources != before {
					t.Errorf("kein rollback: exit %d, quellen %q\n%s", run.exitCode, run.sources, run.output)
				}
				if run.backup {
					t.Error("eine zurückgerollte Umstellung darf nicht als Vorzustand im Protokoll stehen")
				}
				// Das zweite Update holt die Paketlisten der alten Adressen zurück.
				if run.updates != 2 {
					t.Errorf("apt-get update lief %d-mal, erwartet 2", run.updates)
				}
				if !strings.Contains(run.output, firstLine(tc.output)) {
					t.Errorf("die Ausgabe von apt fehlt im Ergebnis:\n%s", run.output)
				}
			})
		}
	}
}

func TestHTTPSRueckstellungPrueftDieAusgabeVonApt(t *testing.T) {
	const before = "URIs: https://ftp.example/ubuntu/\n"
	const after = "URIs: http://ftp.example/ubuntu/\n"
	scripts := map[string]string{
		"skript": revertHTTPSScript([]string{"https://ftp.example/ubuntu/"}),
		"helper": helperRepoScript(t, "repos_http https://ftp.example/ubuntu/"),
	}
	for label, script := range scripts {
		t.Run(label, func(t *testing.T) {
			ok := runRepoScript(t, script, before, aptOutputOK, 0)
			if ok.exitCode != 0 || ok.sources != after || ok.leftover {
				t.Errorf("rückstellung: exit %d, quellen %q\n%s", ok.exitCode, ok.sources, ok.output)
			}
			failed := runRepoScript(t, script, before, aptOutputSourceFailed, 0)
			if failed.exitCode != 1 || failed.sources != before || failed.leftover {
				t.Errorf("kein rollback: exit %d, quellen %q\n%s", failed.exitCode, failed.sources, failed.output)
			}
		})
	}
}
