package services

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"LCM/internal/core/domain"
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
		helperFunktion(t, "https_switch_file"),
		helperFunktion(t, "repos_https"),
		helperFunktion(t, "repos_http"),
		"valid_repo_url() { return 0; }",
		"valid_http_repo_url() { return 0; }",
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

// TestHTTPSUmstellungNurDerAuswahl: Gewählt ist nur der Sicherheits-Spiegel.
// Der Landesspiegel ohne HTTPS bleibt auf http - und ebenso eine Quelle, deren
// URL mit der gewählten nur beginnt.
func TestHTTPSUmstellungNurDerAuswahl(t *testing.T) {
	const before = "deb http://de.archive.ubuntu.com/ubuntu noble main\n" +
		"deb http://security.ubuntu.com/ubuntu noble-security main\n" +
		"deb http://security.ubuntu.com/ubuntu-old noble main\n" +
		"URIs: http://security.ubuntu.com/ubuntu\n"
	const after = "deb http://de.archive.ubuntu.com/ubuntu noble main\n" +
		"deb https://security.ubuntu.com/ubuntu noble-security main\n" +
		"deb http://security.ubuntu.com/ubuntu-old noble main\n" +
		"URIs: https://security.ubuntu.com/ubuntu\n"
	chosen := "http://security.ubuntu.com/ubuntu"
	scripts := map[string]string{
		"skript": secureHTTPSURLsScript([]string{chosen}),
		"helper": helperRepoScript(t, "repos_https "+chosen),
	}
	for label, script := range scripts {
		t.Run(label, func(t *testing.T) {
			ok := runRepoScript(t, script, before, aptOutputOK, 0)
			if ok.exitCode != 0 || ok.sources != after || !ok.backup || ok.leftover {
				t.Errorf("umstellung: exit %d, sicherung %v, quellen %q\n%s", ok.exitCode, ok.backup, ok.sources, ok.output)
			}
			failed := runRepoScript(t, script, before, aptOutputSourceFailed, 0)
			if failed.exitCode != 1 || failed.sources != before || failed.backup || failed.leftover {
				t.Errorf("kein rollback: exit %d, sicherung %v, quellen %q\n%s", failed.exitCode, failed.backup, failed.sources, failed.output)
			}
		})
	}
}

// TestHTTPSUmstellungAuswahlNichtGefunden: Steht keine der genannten URLs in
// den Quellen, ändert sich nichts, und apt-get update läuft gar nicht erst.
func TestHTTPSUmstellungAuswahlNichtGefunden(t *testing.T) {
	const before = "deb http://de.archive.ubuntu.com/ubuntu noble main\n"
	run := runRepoScript(t, secureHTTPSURLsScript([]string{"http://security.ubuntu.com/ubuntu"}), before, aptOutputOK, 0)
	if run.exitCode != 0 || run.sources != before || run.updates != 0 || run.leftover {
		t.Errorf("exit %d, updates %d, quellen %q\n%s", run.exitCode, run.updates, run.sources, run.output)
	}
}

// TestHTTPSPruefungLiestDieProben: Jede Quelle bekommt ein Ergebnis - auch
// die, zu der der Server nichts gemeldet hat. Die Empfehlung gibt es nur für
// Quellen ohne HTTPS, und nie die gerade gescheiterte Adresse selbst.
func TestHTTPSPruefungLiestDieProben(t *testing.T) {
	probes := []domain.HTTPSProbe{
		{URI: "http://de.archive.ubuntu.com/ubuntu", URL: "https://de.archive.ubuntu.com/ubuntu/dists/noble/Release"},
		{URI: "http://security.ubuntu.com/ubuntu", URL: "https://security.ubuntu.com/ubuntu/dists/noble-security/Release"},
		{URI: "http://archive.ubuntu.com/ubuntu", URL: "https://archive.ubuntu.com/ubuntu/dists/noble/Release"},
		{URI: "http://repo.example/apt", URL: ""},
		{URI: "http://still.example/apt", URL: "https://still.example/apt/dists/x/Release"},
	}
	output := "LCM-HTTPS fail http://de.archive.ubuntu.com/ubuntu\n" +
		"Warnung: irgendwas\n" +
		"LCM-HTTPS ok http://security.ubuntu.com/ubuntu\n" +
		"LCM-HTTPS fail http://archive.ubuntu.com/ubuntu\n" +
		"LCM-HTTPS unknown http://repo.example/apt\n"
	got := parseHTTPSCheck(output, probes)
	want := []RepoHTTPSResult{
		{URI: "http://de.archive.ubuntu.com/ubuntu", State: RepoHTTPSNone, Alternative: "https://archive.ubuntu.com/ubuntu"},
		{URI: "http://security.ubuntu.com/ubuntu", State: RepoHTTPSOK},
		{URI: "http://archive.ubuntu.com/ubuntu", State: RepoHTTPSNone},
		{URI: "http://repo.example/apt", State: RepoHTTPSUnknown},
		{URI: "http://still.example/apt", State: RepoHTTPSUnknown},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ergebnis\n%+v\nerwartet\n%+v", got, want)
	}
}

// TestHTTPSPruefskriptLaeuft: Das Prüfskript läuft wirklich - mit einer
// apt-helper-Attrappe, die nur security.ubuntu.com bedient. Eine Quelle ohne
// ableitbare Adresse wird gar nicht erst abgerufen.
func TestHTTPSPruefskriptLaeuft(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("kein sh")
	}
	dir := t.TempDir()
	helper := filepath.Join(dir, "apt-helper")
	stub := "#!/bin/sh\necho \"$@\" >> " + dir + "/calls\ncase \"$4\" in https://security.ubuntu.com/*) echo ok > \"$5\" ;; *) exit 100 ;; esac\n"
	if err := os.WriteFile(helper, []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	probes := []domain.HTTPSProbe{
		{URI: "http://de.archive.ubuntu.com/ubuntu", URL: "https://de.archive.ubuntu.com/ubuntu/dists/noble/Release"},
		{URI: "http://security.ubuntu.com/ubuntu", URL: "https://security.ubuntu.com/ubuntu/dists/noble-security/Release"},
		{URI: "http://repo.example/apt", URL: ""},
	}
	// timeout gibt es auf macOS nicht; die Attrappe reicht nur durch.
	if err := os.WriteFile(filepath.Join(dir, "timeout"), []byte("#!/bin/sh\nshift\nexec \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	certs := filepath.Join(dir, "ca-certificates.crt")
	if err := os.WriteFile(certs, []byte("zertifikate"), 0o644); err != nil {
		t.Fatal(err)
	}
	sandbox := func(script string) string { return strings.Replace(script, systemCertBundle, certs, 1) }
	script := sandbox(strings.Replace(httpsCheckScript(probes), "/usr/lib/apt/apt-helper", helper, 1))
	cmd := exec.Command(sh, "-c", script)
	cmd.Env = []string{"PATH=" + dir + ":/usr/bin:/bin"}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	got := parseHTTPSCheck(string(out), probes)
	states := []string{got[0].State, got[1].State, got[2].State}
	if !reflect.DeepEqual(states, []string{RepoHTTPSNone, RepoHTTPSOK, RepoHTTPSUnknown}) {
		t.Errorf("zustände %v\n%s", states, out)
	}
	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	// security: Release genügt; de.archive: Release und InRelease scheitern;
	// die Quelle ohne Adresse wird nicht abgerufen.
	if strings.Count(string(calls), "download-file") != 3 {
		t.Errorf("erwartet drei Abrufe:\n%s", calls)
	}

	// Ohne apt-helper (sehr altes apt) ist nichts prüfbar - und nichts gilt als
	// gescheitert.
	missing := sandbox(strings.Replace(httpsCheckScript(probes), "/usr/lib/apt/apt-helper", filepath.Join(dir, "fehlt"), 1))
	out, err = exec.Command(sh, "-c", missing).CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, result := range parseHTTPSCheck(string(out), probes) {
		if result.State != RepoHTTPSUnknown {
			t.Errorf("ohne apt-helper: %+v", result)
		}
	}
}

// TestHTTPSPruefungOhneZertifikate: Fehlen dem Server die Stammzertifikate,
// scheitert jede HTTPS-Verbindung. Das sagt nichts über die Spiegel - keine
// Quelle gilt dann als ohne HTTPS, und die Prüfung nennt den Grund.
func TestHTTPSPruefungOhneZertifikate(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("kein sh")
	}
	probes := []domain.HTTPSProbe{
		{URI: "http://security.ubuntu.com/ubuntu", URL: "https://security.ubuntu.com/ubuntu/dists/noble-security/Release"},
	}
	script := strings.Replace(httpsCheckScript(probes), systemCertBundle, filepath.Join(t.TempDir(), "fehlt.crt"), 1)
	out, err := exec.Command(sh, "-c", script).CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(string(out), httpsNoCerts) {
		t.Fatalf("keine Meldung zu fehlenden Zertifikaten:\n%s", out)
	}
	if got := parseHTTPSCheck(string(out), probes); got[0].State != RepoHTTPSUnknown {
		t.Errorf("ohne zertifikate: %+v", got[0])
	}
}

// TestHTTPSUmstellungMitSchraegstrich: deb822 schreibt die URL mit „/" am Ende,
// die Auswahl kann ohne kommen - und umgekehrt. Beides trifft, der
// Schrägstrich bleibt erhalten. Ändert sich nichts, meldet die Umstellung
// keinen Erfolg und lässt apt-get update aus.
func TestHTTPSUmstellungMitSchraegstrich(t *testing.T) {
	const before = "URIs: http://security.ubuntu.com/ubuntu/\n" +
		"deb http://archive.ubuntu.com/ubuntu noble main\n" +
		"URIs: http://de.archive.ubuntu.com/ubuntu/\n"
	const after = "URIs: https://security.ubuntu.com/ubuntu/\n" +
		"deb https://archive.ubuntu.com/ubuntu noble main\n" +
		"URIs: http://de.archive.ubuntu.com/ubuntu/\n"
	chosen := []string{"http://security.ubuntu.com/ubuntu", "http://archive.ubuntu.com/ubuntu/"}
	scripts := map[string]string{
		"skript": secureHTTPSURLsScript(chosen),
		"helper": helperRepoScript(t, "repos_https "+strings.Join(chosen, " ")),
	}
	for label, script := range scripts {
		t.Run(label, func(t *testing.T) {
			run := runRepoScript(t, script, before, aptOutputOK, 0)
			if run.exitCode != 0 || run.sources != after || !run.backup || run.leftover {
				t.Errorf("exit %d, sicherung %v, quellen %q\n%s", run.exitCode, run.backup, run.sources, run.output)
			}
		})
	}

	// Steht die URL nur als Teil einer anderen da, ändert sich nichts.
	const other = "deb http://security.ubuntu.com/ubuntu-old noble main\n"
	for label, script := range map[string]string{
		"skript": secureHTTPSURLsScript([]string{"http://security.ubuntu.com/ubuntu"}),
		"helper": helperRepoScript(t, "repos_https http://security.ubuntu.com/ubuntu"),
	} {
		t.Run(label+"-nichts", func(t *testing.T) {
			run := runRepoScript(t, script, other, aptOutputOK, 0)
			if run.exitCode != 0 || run.sources != other || run.updates != 0 || run.backup || run.leftover {
				t.Errorf("exit %d, updates %d, sicherung %v, quellen %q\n%s", run.exitCode, run.updates, run.backup, run.sources, run.output)
			}
		})
	}
}

// TestHTTPSPruefungMitInReleaseUndOhneTimeout: Ein Spiegel, der nur noch
// InRelease führt, spricht trotzdem HTTPS. Und fehlt timeout auf dem Server,
// läuft die Prüfung ohne - statt jede Quelle als gescheitert zu melden.
func TestHTTPSPruefungMitInReleaseUndOhneTimeout(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("kein sh")
	}
	dir := t.TempDir()
	helper := filepath.Join(dir, "apt-helper")
	stub := "#!/bin/sh\ncase \"$4\" in *InRelease) echo ok > \"$5\" ;; *) exit 100 ;; esac\n"
	if err := os.WriteFile(helper, []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	certs := filepath.Join(dir, "ca-certificates.crt")
	if err := os.WriteFile(certs, []byte("zertifikate"), 0o644); err != nil {
		t.Fatal(err)
	}
	probes := []domain.HTTPSProbe{
		{URI: "http://security.ubuntu.com/ubuntu", URL: "https://security.ubuntu.com/ubuntu/dists/noble-security/Release"},
	}
	script := strings.Replace(httpsCheckScript(probes), "/usr/lib/apt/apt-helper", helper, 1)
	script = strings.Replace(script, systemCertBundle, certs, 1)
	// Kein timeout im PATH: nur das Verzeichnis mit der Attrappe und /bin für sh-Bordmittel.
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"mktemp", "rm"} {
		path, err := exec.LookPath(tool)
		if err != nil {
			t.Skip("kein " + tool)
		}
		if err := os.Symlink(path, filepath.Join(bin, tool)); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(sh, "-c", script)
	cmd.Env = []string{"PATH=" + bin}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := parseHTTPSCheck(string(out), probes); got[0].State != RepoHTTPSOK {
		t.Errorf("nur InRelease, ohne timeout: %+v\n%s", got[0], out)
	}
}
