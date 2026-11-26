package services

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"LCM/internal/core/agentcaps"
)

// readFixture liest eine echte Ausgabe des Versionsskripts (aufgenommen mit
// testing/agent-pkg-e2e.sh in Containern der Distribution).
func readFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "agent-pkg", name+".txt"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestParseAgentVersionsEchteAusgaben: dnf4, dnf5, zypper, apk 2 und 3, pacman.
// Installiert ist 2.3.0; die Quelle führt (außer pacman) 2.2.1 bis 2.4.0~beta.2.
func TestParseAgentVersionsEchteAusgaben(t *testing.T) {
	all := []string{"2.4.0~beta.2", "2.4.0~beta.1", "2.3.1", "2.3.0", "2.2.1"}
	tests := []struct {
		fixture, mgr string
		want         []string
		source       string
	}{
		{"dnf4-alma9", pkgDnf, all, "techeve-lcm"},
		{"dnf4-alma10", pkgDnf, all, "techeve-lcm"},
		{"dnf5-fedora", pkgDnf, all, "techeve-lcm"},
		{"zypper-leap", pkgZypper, all, "techeve-lcm"},
		{"zypper-tumbleweed", pkgZypper, all, "techeve-lcm"},
		{"apk2-alpine322", pkgApk, all, "file:///srv/repo"},
		{"apk3-alpine324", pkgApk, all, "file:///srv/repo"},
		{"pacman-arch", pkgPacman, []string{"2.4.0~beta.2"}, "techeve"},
	}
	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			got := parseAgentVersions(tt.mgr, readFixture(t, tt.fixture))
			if got.Installed != "2.3.0" || got.NoManager || got.NotPackaged || got.RefreshFailed {
				t.Fatalf("ausgewertet: %+v", got)
			}
			if !sameSet(got.Available, tt.want) {
				t.Errorf("verfügbar = %v, erwartet %v", got.Available, tt.want)
			}
			if src := got.Sources[tt.want[0]]; src != tt.source {
				t.Errorf("quelle = %q, erwartet %q", src, tt.source)
			}
		})
	}
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]bool{}
	for _, v := range a {
		seen[v] = true
	}
	for _, v := range b {
		if !seen[v] {
			return false
		}
	}
	return true
}

// TestParseAgentVersionsZypperOhneSystemZeile: Eine installierte Version, die
// keine Quelle führt, steht als „(System Packages)“ in der Tabelle und ist
// keine Version zur Auswahl.
func TestParseAgentVersionsZypperOhneSystemZeile(t *testing.T) {
	out := "LCM_AGENT_INSTALLED=2.3.5-1\nLCM_AGENT_VERSIONS\n" +
		"S  | Name      | Type    | Version | Arch   | Repository\n" +
		"---+-----------+---------+---------+--------+-----------------\n" +
		"i+ | lcm-agent | package | 2.3.5-1 | x86_64 | (System Packages)\n" +
		"v  | lcm-agent | package | 2.4.0-1 | x86_64 | techeve-lcm\n"
	got := parseAgentVersions(pkgZypper, out)
	if got.Installed != "2.3.5" || !sameSet(got.Available, []string{"2.4.0"}) {
		t.Errorf("ausgewertet: %+v", got)
	}
}

// TestParseAgentVersionsApkOhneRepoZeile: apk führt den installierten Stand
// unter lib/apk/db/installed; ohne Repository ist er keine Auswahl.
func TestParseAgentVersionsApkOhneRepoZeile(t *testing.T) {
	out := "LCM_AGENT_INSTALLED=2.3.5-r0\nLCM_AGENT_VERSIONS\nlcm-agent policy:\n  2.3.5-r0:\n    lib/apk/db/installed\n  2.4.0_rc1:\n    https://repo.techeve.de/apk/community\n"
	got := parseAgentVersions(pkgApk, out)
	if got.Installed != "2.3.5" || !sameSet(got.Available, []string{"2.4.0~rc.1"}) {
		t.Errorf("ausgewertet: %+v", got)
	}
}

// TestParseAgentVersionsMarken: Marken der Skripte für alle Familien.
func TestParseAgentVersionsMarken(t *testing.T) {
	for _, mgr := range []string{pkgApt, pkgDnf, pkgYum, pkgZypper, pkgApk, pkgPacman} {
		if v := parseAgentVersions(mgr, agentMarkNoManager+"\n"); !v.NoManager {
			t.Errorf("%s: keine Paketverwaltung nicht erkannt", mgr)
		}
		if v := parseAgentVersions(mgr, agentMarkNotPackaged+"\n"); !v.NotPackaged {
			t.Errorf("%s: kein Paket nicht erkannt", mgr)
		}
		if v := parseAgentVersions(mgr, agentMarkRefreshFail+"4\nLCM_AGENT_INSTALLED=2.3.0\n"); !v.RefreshFailed || v.refreshExitRaw != "4" {
			t.Errorf("%s: Fehler beim Erneuern nicht erkannt: %+v", mgr, v)
		}
	}
	if v := parseAgentVersions(pkgApt, agentMarkNoApt+"\n"); !v.NoManager {
		t.Error("die apt-Marke gilt weiter")
	}
}

// TestAgentVersionMapping: Schreibweisen der Paketformate ↔ apt-Schreibweise.
func TestAgentVersionMapping(t *testing.T) {
	tests := []struct {
		family, native, canon, back string
	}{
		{pkgDnf, "2.3.0-1", "2.3.0", "2.3.0"},
		{pkgDnf, "2.4.0~beta.1-1", "2.4.0~beta.1", "2.4.0~beta.1"},
		{pkgZypper, "2.4.0~beta.12-1", "2.4.0~beta.12", "2.4.0~beta.12"},
		{pkgApk, "2.3.0", "2.3.0", "2.3.0"},
		{pkgApk, "2.3.0-r0", "2.3.0", "2.3.0"},
		{pkgApk, "2.4.0_beta1", "2.4.0~beta.1", "2.4.0_beta1"},
		{pkgApk, "2.4.0_beta12-r1", "2.4.0~beta.12", "2.4.0_beta12"},
		{pkgApk, "2.4.0_rc1", "2.4.0~rc.1", "2.4.0_rc1"},
		{pkgPacman, "2.3.0-1", "2.3.0", "2.3.0"},
		{pkgPacman, "2.4.0beta.1-1", "2.4.0~beta.1", "2.4.0beta.1"},
		{pkgPacman, "2.4.0beta.10-1", "2.4.0~beta.10", "2.4.0beta.10"},
		{pkgApt, "2.4.0~beta.1", "2.4.0~beta.1", "2.4.0~beta.1"},
	}
	for _, tt := range tests {
		canon := agentCanonVersion(tt.family, tt.native)
		if canon != tt.canon {
			t.Errorf("%s %s: kanonisch %q, erwartet %q", tt.family, tt.native, canon, tt.canon)
		}
		if back := agentNativeVersion(tt.family, canon); back != tt.back {
			t.Errorf("%s %s: nativ %q, erwartet %q", tt.family, canon, back, tt.back)
		}
		if !agentcaps.SameVersion(canon, agentCanonVersion(tt.family, tt.native)) {
			t.Errorf("%s: %s vergleicht sich nicht mit sich selbst", tt.family, canon)
		}
	}
	// Eine Beta liegt vor dem Finale, und die zehnte hinter der neunten.
	if !agentcaps.SameVersion("2.4.0~beta.1", agentCanonVersion(pkgApk, "2.4.0_beta1")) {
		t.Error("apk-Beta und apt-Beta sind dieselbe Version")
	}
}

// TestAgentSkripteSindGueltigesShell: Alle Skripte jeder Familie parsen unter sh
// (sh -n) und dash/bash, soweit vorhanden.
func TestAgentSkripteSindGueltigesShell(t *testing.T) {
	for _, mgr := range []string{pkgDnf, pkgYum, pkgZypper, pkgApk, pkgPacman} {
		scripts := map[string]string{
			"versions": agentVersionsScriptFor(mgr, false),
			"refresh":  agentVersionsScriptFor(mgr, true),
		}
		if mgr != pkgYum {
			scripts["install"] = mustInstallScript(t, mgr, "2.4.0~beta.1")
			scripts["install-up"] = mustInstallScript(t, mgr, "2.3.1")
		}
		for name, script := range scripts {
			for _, sh := range []string{"sh", "bash"} {
				if _, err := exec.LookPath(sh); err != nil {
					continue
				}
				if out, err := exec.Command(sh, "-n", "-c", script).CombinedOutput(); err != nil {
					t.Errorf("%s/%s unter %s: %v\n%s\n%s", mgr, name, sh, err, out, script)
				}
			}
		}
	}
}

// mustInstallScript baut das Installationsskript einer gültigen Version.
func mustInstallScript(t *testing.T, mgr, ver string) string {
	t.Helper()
	script, err := agentInstallScriptFor(mgr, ver)
	if err != nil {
		t.Fatalf("%s %s: %v", mgr, ver, err)
	}
	return script
}

// TestAgentInstallScriptFor: die Kommandos je Familie, Schreibweise der Version
// und der Weg über die Schlösser.
func TestAgentInstallScriptFor(t *testing.T) {
	tests := []struct {
		mgr, ver string
		want     []string
	}{
		{pkgDnf, "2.4.0~beta.1", []string{"dnf -y --setopt=disable_excludes=techeve-lcm install lcm-agent-2.4.0~beta.1"}},
		{pkgZypper, "2.3.1", []string{
			"zypper -n removelock lcm-agent", "zypper -n install --oldpackage lcm-agent=2.3.1",
			"trap 'zypper -n addlock lcm-agent >/dev/null 2>&1' EXIT", "case $rc in 102|103|106) rc=0;; esac",
		}},
		{pkgApk, "2.4.0~beta.1", []string{"apk add lcm-agent=2.4.0_beta1"}},
		{pkgPacman, "2.4.0~beta.1", []string{
			"base=$(pacman-conf -r techeve Server | head -n 1)",
			`pacman -U --noconfirm "$base/lcm-agent-2.4.0beta.1-1-$(uname -m).pkg.tar.zst"`,
		}},
	}
	for _, tt := range tests {
		script := mustInstallScript(t, tt.mgr, tt.ver)
		for _, want := range tt.want {
			if !strings.Contains(script, want) {
				t.Errorf("%s: skript enthält %q nicht:\n%s", tt.mgr, want, script)
			}
		}
		if !strings.Contains(script, "echo "+agentMarkInstalled) || !strings.HasSuffix(script, "exit $rc") {
			t.Errorf("%s: das Skript meldet die Version nicht zurück oder gibt den Exit-Code nicht weiter:\n%s", tt.mgr, script)
		}
	}
	// pacman installiert nie mit -S (das zöge die Paketlisten) und rührt sie nicht an.
	if script := mustInstallScript(t, pkgPacman, "2.3.1"); strings.Contains(script, "pacman -S") {
		t.Errorf("pacman -S im Installationsskript:\n%s", script)
	}
	// zypper: Das Schloss setzt allein die Falle, nicht noch ein Aufruf am Ende.
	zy := mustInstallScript(t, pkgZypper, "2.3.1")
	if n := strings.Count(zy, "addlock"); n != 1 {
		t.Errorf("addlock %d-mal im Skript, erwartet einmal (in der Falle):\n%s", n, zy)
	}
	if strings.Index(zy, "removelock") > strings.Index(zy, "trap ") || strings.Index(zy, "trap ") > strings.Index(zy, "install --oldpackage") {
		t.Errorf("Reihenfolge removelock, trap, install verletzt:\n%s", zy)
	}
	// apt bleibt, wie es war.
	apt, err := agentInstallScriptFor(pkgApt, "2.3.0")
	if err != nil || apt != agentInstallScript("2.3.0") || agentVersionsScriptFor("", true) != agentVersionsScript(true) {
		t.Error("apt-Skripte unverändert erwartet")
	}
}

// TestAgentInstallScriptForLehntFremdeVersionenAb: Die Version geht als root in
// ein Shell-Skript; was keine Paketversion ist, wird abgelehnt (die laufende
// Version meldet der Agent selbst).
func TestAgentInstallScriptForLehntFremdeVersionenAb(t *testing.T) {
	bad := []string{"2.3.0;id", "$(id)", "2.3.0 && id", "`id`", "2.3.0\nid", "", "-rf", "2.3.0'", `2.3.0"`, "2.3.0|id", "~2.3.0"}
	for _, mgr := range []string{pkgApt, pkgDnf, pkgZypper, pkgApk, pkgPacman} {
		for _, ver := range bad {
			if script, err := agentInstallScriptFor(mgr, ver); err == nil || script != "" {
				t.Errorf("%s %q: erwartet Fehler, Skript %q", mgr, ver, script)
			}
		}
		for _, ver := range []string{"2.3.0", "2.4.0~beta.1", "1:2.3.0+b1"} {
			if _, err := agentInstallScriptFor(mgr, ver); err != nil {
				t.Errorf("%s %q: %v", mgr, ver, err)
			}
		}
	}
}

// fakeZypperLog schreibt ein gefälschtes zypper, das seine Aufrufe in log
// vermerkt, bei install mit code endet und bei addlock mit lockCode.
func fakeZypperHost(t *testing.T, log string, code, lockCode int) string {
	t.Helper()
	return fakeFamilyHost(t, map[string]string{
		"zypper": fmt.Sprintf(`echo "$*" >>%q
case "$*" in
*" install "*) exit %d ;;
*addlock*) exit %d ;;
esac
exit 0
`, log, code, lockCode),
		"rpm": "printf '2.3.1-1'\n",
	})
}

// TestAgentInstallScriptZypperSchlossUndExitCodes: Das Schloss kommt nach jedem
// Ausgang zurück (auch nach einem Fehlschlag), 102, 103 und 106 gelten als
// Erfolg, jeder andere Exit-Code bleibt erhalten, und ein scheiternder addlock
// verändert den Exit-Code nicht.
func TestAgentInstallScriptZypperSchlossUndExitCodes(t *testing.T) {
	script := mustInstallScript(t, pkgZypper, "2.3.1")
	tests := []struct {
		code, lockCode, want int
	}{
		{0, 0, 0}, {102, 0, 0}, {103, 0, 0}, {106, 0, 0},
		{104, 0, 104}, {1, 0, 1}, {107, 0, 107},
		{0, 5, 0}, {104, 5, 104}, {102, 5, 0},
	}
	for _, tt := range tests {
		log := filepath.Join(t.TempDir(), "calls")
		out, code := runFamilyShell(t, fakeZypperHost(t, log, tt.code, tt.lockCode), script)
		if code != tt.want {
			t.Errorf("zypper %d (addlock %d): exit %d, erwartet %d\n%s", tt.code, tt.lockCode, code, tt.want, out)
		}
		if !strings.Contains(out, agentMarkInstalled+"2.3.1-1") {
			t.Errorf("zypper %d: Rücklesung fehlt: %q", tt.code, out)
		}
		calls, _ := os.ReadFile(log)
		lines := strings.Split(strings.TrimSpace(string(calls)), "\n")
		if len(lines) != 3 || !strings.Contains(lines[0], "removelock") || !strings.Contains(lines[1], "install") || !strings.Contains(lines[2], "addlock") {
			t.Errorf("zypper %d: Aufrufe %q, erwartet removelock, install, addlock", tt.code, lines)
		}
	}
}

// TestAgentVersionsScriptPacmanFasstSystemListenNichtAn: Mit Erneuern holt das
// Skript die Datenbank des Repos in ein Wegwerf-Verzeichnis (kein pacman -Sy gegen
// die Datenbanken des Systems, das wäre ein Teil-Update), liest daraus und räumt auf.
func TestAgentVersionsScriptPacmanFasstSystemListenNichtAn(t *testing.T) {
	fixture := readFixture(t, "pacman-arch")
	list := fixture[strings.Index(fixture, agentMarkVersions)+len(agentMarkVersions)+1:]
	log := filepath.Join(t.TempDir(), "calls")
	bin := fakeFamilyHost(t, map[string]string{
		"pacman": fmt.Sprintf(`echo "$*" >>%q
case " $*" in
*" -Sy"*) exit 0 ;;
*" -Si "*) cat <<'EOF'
%sEOF
exit 0 ;;
*" -Q "*) echo "lcm-agent 2.3.0-1"; exit 0 ;;
esac
exit 0
`, log, list),
		"pacman-conf": "echo 'Server = file:///srv/repo/x86_64'\n",
	})
	out, code := runFamilyShell(t, bin, agentVersionsScriptFor(pkgPacman, true))
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	got := parseAgentVersions(pkgPacman, out)
	if got.RefreshFailed || got.Installed != "2.3.0" || !sameSet(got.Available, []string{"2.4.0~beta.2"}) {
		t.Errorf("ausgewertet: %+v\n%s", got, out)
	}
	raw, _ := os.ReadFile(log)
	var dbpath string
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if !strings.Contains(line, "-Sy") && !strings.Contains(line, "-Si") {
			continue
		}
		fields := strings.Fields(line)
		for i, f := range fields {
			if f == "--dbpath" && i+1 < len(fields) {
				dbpath = fields[i+1]
			}
		}
		if !strings.Contains(line, "--dbpath") || !strings.Contains(line, "--config") {
			t.Errorf("Aufruf gegen die Datenbanken des Systems: %q", line)
		}
	}
	if dbpath == "" {
		t.Fatalf("kein Aufruf mit --dbpath im Protokoll:\n%s", raw)
	}
	if _, err := os.Stat(filepath.Dir(dbpath)); err == nil {
		t.Errorf("das Wegwerf-Verzeichnis %s bleibt liegen", filepath.Dir(dbpath))
	}
	// Ohne Erneuern bleibt es beim Stand des Systems, ganz ohne -Sy.
	for _, mgr := range []string{pkgPacman} {
		if strings.Contains(agentVersionsScriptFor(mgr, false), "-Sy") {
			t.Error("die Abfrage ohne Erneuern enthält -Sy")
		}
	}
}

// TestAgentVersionsScriptPacmanErneuernScheitert: Scheitert das Wegwerf-Erneuern,
// steht die Marke, und die Liste kommt aus dem Stand des Systems.
func TestAgentVersionsScriptPacmanErneuernScheitert(t *testing.T) {
	log := filepath.Join(t.TempDir(), "calls")
	bin := fakeFamilyHost(t, map[string]string{
		"pacman": fmt.Sprintf(`echo "$*" >>%q
case " $*" in
*" -Sy"*) exit 3 ;;
*" -Si "*) case "$*" in *--dbpath*) exit 1 ;; esac; printf 'Repository : techeve\nVersion : 2.3.1-1\n'; exit 0 ;;
*" -Q "*) echo "lcm-agent 2.3.0-1"; exit 0 ;;
esac
`, log),
		"pacman-conf": "echo 'Server = file:///srv/repo/x86_64'\n",
	})
	out, code := runFamilyShell(t, bin, agentVersionsScriptFor(pkgPacman, true))
	got := parseAgentVersions(pkgPacman, out)
	if code != 0 || !got.RefreshFailed || got.refreshExitRaw != "3" || !sameSet(got.Available, []string{"2.3.1"}) {
		t.Errorf("exit %d, ausgewertet: %+v\n%s", code, got, out)
	}
}

// fakePacmanLandlock ist ein pacman, dessen Downloads (-Sy, -U) mit der
// Landlock-Meldung von pacman 7 scheitern, solange kein --disable-sandbox
// dabei ist. failure ersetzt die Meldung (Exit 1) für andere Fehler.
func fakePacmanLandlock(t *testing.T, log, failure string) string {
	t.Helper()
	msg := "error: restricting filesystem access failed because Landlock is not supported by the kernel!"
	if failure != "" {
		msg = failure
	}
	return fakeFamilyHost(t, map[string]string{
		"pacman": fmt.Sprintf(`echo "$*" >>%q
case " $*" in
*" -Q "*) echo "lcm-agent 2.3.0-1"; exit 0 ;;
*" -Si "*) printf 'Repository : techeve\nVersion : 2.3.1-1\n'; exit 0 ;;
esac
case "$*" in *--disable-sandbox*) echo "downloaded"; exit 0 ;; esac
echo 'fortschritt'
echo %q >&2
exit 1
`, log, msg),
		"pacman-conf": "echo 'Server = file:///srv/repo/x86_64'\n",
	})
}

func pacmanCalls(t *testing.T, log string) []string {
	t.Helper()
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}

// TestAgentPacmanLandlockWiederholtMitDisableSandbox: Fehlt dem Kernel Landlock
// (Arch im LXC-Container), scheitert jeder pacman-Download. Das Agent-Update
// wiederholt genau diesen Aufruf einmal mit --disable-sandbox, beim Erneuern der
// Datenbank wie beim Installieren, und gibt den Exit-Code des zweiten Laufs weiter.
func TestAgentPacmanLandlockWiederholtMitDisableSandbox(t *testing.T) {
	t.Run("erneuern", func(t *testing.T) {
		log := filepath.Join(t.TempDir(), "calls")
		bin := fakePacmanLandlock(t, log, "")
		out, code := runFamilyShell(t, bin, agentVersionsScriptFor(pkgPacman, true))
		got := parseAgentVersions(pkgPacman, out)
		if code != 0 || got.RefreshFailed || !sameSet(got.Available, []string{"2.3.1"}) {
			t.Fatalf("exit %d, ausgewertet: %+v\n%s", code, got, out)
		}
		var sy []string
		for _, line := range pacmanCalls(t, log) {
			if strings.Contains(line, "-Sy") {
				sy = append(sy, line)
			}
		}
		if len(sy) != 2 || strings.Contains(sy[0], "--disable-sandbox") || !strings.HasPrefix(sy[1], "--disable-sandbox ") ||
			strings.TrimPrefix(sy[1], "--disable-sandbox ") != sy[0] {
			t.Errorf("erwartet -Sy und derselbe Aufruf mit --disable-sandbox, war %q", sy)
		}
	})
	t.Run("installieren", func(t *testing.T) {
		log := filepath.Join(t.TempDir(), "calls")
		bin := fakePacmanLandlock(t, log, "")
		out, code := runFamilyShell(t, bin, mustInstallScript(t, pkgPacman, "2.3.1"))
		if code != 0 || !strings.Contains(out, "downloaded") || strings.Contains(out, "Landlock") {
			t.Fatalf("exit %d, ausgabe:\n%s", code, out)
		}
		var u []string
		for _, line := range pacmanCalls(t, log) {
			if strings.Contains(line, "-U ") {
				u = append(u, line)
			}
		}
		if len(u) != 2 || strings.Contains(u[0], "--disable-sandbox") || strings.TrimPrefix(u[1], "--disable-sandbox ") != u[0] {
			t.Errorf("erwartet -U und derselbe Aufruf mit --disable-sandbox, war %q", u)
		}
	})
}

// TestAgentPacmanAndereFehlerBleibenUnveraendert: Ein anderer Fehler wird nicht
// wiederholt: kein --disable-sandbox, Exit-Code und Fehlermeldung bleiben.
func TestAgentPacmanAndereFehlerBleibenUnveraendert(t *testing.T) {
	log := filepath.Join(t.TempDir(), "calls")
	bin := fakePacmanLandlock(t, log, "error: failed retrieving file 'x' from host: Could not resolve host")
	out, code := runFamilyShell(t, bin, mustInstallScript(t, pkgPacman, "2.3.1"))
	if code != 1 || !strings.Contains(out, "Could not resolve host") || !strings.Contains(out, "fortschritt") {
		t.Errorf("exit %d, ausgabe:\n%s", code, out)
	}
	for _, line := range pacmanCalls(t, log) {
		if strings.Contains(line, "--disable-sandbox") {
			t.Errorf("Wiederholung ohne Landlock-Meldung: %q", line)
		}
	}
	n := 0
	for _, line := range pacmanCalls(t, log) {
		if strings.HasPrefix(line, "-U ") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("-U %d-mal aufgerufen, erwartet einmal", n)
	}

	// Beim Erneuern bleibt es bei der Marke samt Exit-Code, ohne Wiederholung.
	log = filepath.Join(t.TempDir(), "calls")
	bin = fakePacmanLandlock(t, log, "error: failed to synchronize all databases")
	out, code = runFamilyShell(t, bin, agentVersionsScriptFor(pkgPacman, true))
	got := parseAgentVersions(pkgPacman, out)
	if code != 0 || !got.RefreshFailed || got.refreshExitRaw != "1" {
		t.Errorf("exit %d, ausgewertet: %+v\n%s", code, got, out)
	}
	for _, line := range pacmanCalls(t, log) {
		if strings.Contains(line, "--disable-sandbox") {
			t.Errorf("Wiederholung ohne Landlock-Meldung: %q", line)
		}
	}
}

// TestAgentPacmanSkriptDeaktiviertDieSandboxNurImFehlerfall: Weder der Text der
// Skripte noch die Konfiguration kennt DisableSandbox; --disable-sandbox steht
// allein in der Wiederholung der Funktion.
func TestAgentPacmanSkriptDeaktiviertDieSandboxNurImFehlerfall(t *testing.T) {
	scripts := []string{agentVersionsScriptFor(pkgPacman, true), mustInstallScript(t, pkgPacman, "2.3.1")}
	for _, script := range scripts {
		if strings.Contains(script, "DisableSandbox") || strings.Contains(script, "pacman.conf >>") {
			t.Errorf("Skript schreibt die Sandbox-Einstellung in die Konfiguration:\n%s", script)
		}
		if n := strings.Count(script, "--disable-sandbox"); n != 1 {
			t.Errorf("--disable-sandbox %d-mal im Skript, erwartet einmal (Wiederholung):\n%s", n, script)
		}
		if !strings.Contains(script, "lcm_pacman() {") {
			t.Errorf("lcm_pacman fehlt im Skript:\n%s", script)
		}
	}
	if strings.Contains(agentVersionsScriptFor(pkgPacman, false), "lcm_pacman") {
		t.Error("die Abfrage ohne Erneuern braucht kein lcm_pacman")
	}
}

// TestAgentUpdateYumOhneDnfUnterstuetztNicht: Auf CentOS 7 oder Amazon Linux 2
// (yum ohne dnf) gibt es kein Agent-Update: Das Versionsskript meldet die Marke
// ohne etwas auszuführen, der Plan nennt den Grund no_manager, und es entsteht
// kein Installationsskript.
func TestAgentUpdateYumOhneDnfUnterstuetztNicht(t *testing.T) {
	for _, refresh := range []bool{false, true} {
		script := agentVersionsScriptFor(pkgYum, refresh)
		bin := fakeFamilyHost(t, map[string]string{"yum": "echo yum-called >&2\nexit 1\n", "rpm": "echo 2.3.0-1\n"})
		out, code := runFamilyShell(t, bin, script)
		if code != 0 || strings.TrimSpace(out) != agentMarkNoManager {
			t.Fatalf("refresh=%v: exit %d, ausgabe %q", refresh, code, out)
		}
		plan := planAgentUpdate(parseAgentVersions(pkgYum, out), "")
		if plan.Action != agentActionUnsupported || plan.Reason != agentReasonNoManager {
			t.Errorf("plan: %+v", plan)
		}
	}
	if script, err := agentInstallScriptFor(pkgYum, "2.3.1"); err == nil || script != "" {
		t.Errorf("Installationsskript für yum: %q, %v", script, err)
	}
}

// TestAgentVersionsScriptForZypperLockHinweis: Das Zypper-Skript liest nur, es
// installiert nichts.
func TestAgentVersionsScriptForInstalliertNichts(t *testing.T) {
	for _, mgr := range []string{pkgDnf, pkgZypper, pkgApk, pkgPacman} {
		body := agentVersionsScriptFor(mgr, true)
		for _, verb := range []string{" install ", " add ", " -S ", " -U ", "addlock"} {
			if strings.Contains(body, verb) {
				t.Errorf("%s: die versionsabfrage enthält %q:\n%s", mgr, verb, body)
			}
		}
	}
}

// fakeFamilyHost baut ein Verzeichnis mit gefälschten Programmen: bin enthält
// nur, was files nennt (Name → Inhalt des Skripts). Mit Pfad nur dort liefert
// ein fehlendes Programm "nicht gefunden".
func fakeFamilyHost(t *testing.T, files map[string]string) string {
	t.Helper()
	bin := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return bin
}

// TestAgentVersionsSkriptOhnePaketverwaltungUndOhnePaket: Marken auf einem Server
// ohne die Verwaltung und mit einem Agent, der kein Paket ist.
func TestAgentVersionsSkriptOhnePaketverwaltungUndOhnePaket(t *testing.T) {
	for _, mgr := range []string{pkgDnf, pkgZypper, pkgApk, pkgPacman} {
		out, code := runFamilyShell(t, t.TempDir(), agentVersionsScriptFor(mgr, false))
		if code != 0 || !strings.Contains(out, agentMarkNoManager) {
			t.Errorf("%s ohne Verwaltung: exit %d, %q", mgr, code, out)
		}
	}
	// Mit der Verwaltung, aber das Paket ist nicht installiert (alle Abfragen scheitern).
	failing := map[string]string{
		"dnf": "exit 1\n", "rpm": "exit 1\n", "zypper": "exit 1\n", "apk": "exit 1\n", "pacman": "exit 1\n",
	}
	for _, mgr := range []string{pkgDnf, pkgZypper, pkgApk, pkgPacman} {
		out, code := runFamilyShell(t, fakeFamilyHost(t, failing), agentVersionsScriptFor(mgr, true))
		if code != 0 || !strings.Contains(out, agentMarkNotPackaged) || strings.Contains(out, agentMarkVersions) {
			t.Errorf("%s ohne Paket: exit %d, %q", mgr, code, out)
		}
	}
}

// runFamilyShell führt das Skript mit genau diesem Verzeichnis (und /bin für
// sh-Werkzeuge wie sed, cut) im Pfad aus; fehlende Programme der Verwaltung
// fallen so auf die Marke.
func runFamilyShell(t *testing.T, bin, script string) (string, int) {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", script)
	cmd.Env = []string{"PATH=" + bin + ":/usr/bin:/bin"}
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("skript nicht startbar: %v", err)
	}
	return string(out), code
}

// TestAgentVersionsSkriptErneuertUndMeldetFehlschlag: Scheitert das Erneuern der
// Listen, steht die Marke davor, und die Versionen werden trotzdem gelesen.
func TestAgentVersionsSkriptErneuertUndMeldetFehlschlag(t *testing.T) {
	fixture := readFixture(t, "dnf5-fedora")
	list := fixture[strings.Index(fixture, agentMarkVersions)+len(agentMarkVersions)+1:]
	bin := fakeFamilyHost(t, map[string]string{
		"rpm": `case "$*" in *--qf*) printf '2.3.0-1' ;; esac; exit 0` + "\n",
		"dnf": `case "$*" in *makecache*) exit 7 ;; esac; cat <<'EOF'` + "\n" + list + "EOF\n",
	})
	out, code := runFamilyShell(t, bin, agentVersionsScriptFor(pkgDnf, true))
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	got := parseAgentVersions(pkgDnf, out)
	if !got.RefreshFailed || got.refreshExitRaw != "7" || got.Installed != "2.3.0" || len(got.Available) != 5 {
		t.Errorf("ausgewertet: %+v\n%s", got, out)
	}
}

// TestRealignPacmanTargetAusDemLaufendenAgent: Die Datenbank des Arch-Repos
// führt nur die neueste Version; zum Nachziehen genügt die laufende.
func TestCheckInstalledAgent(t *testing.T) {
	tests := []struct {
		name, mgr, out, target string
		wantErr                bool
	}{
		{"ohne Meldung gilt es nicht", pkgDnf, "Complete!\n", "2.3.1", false},
		{"Meldung stimmt", pkgDnf, "x\nLCM_AGENT_INSTALLED=2.3.1-1\n", "2.3.1", false},
		{"apk-Beta", pkgApk, "LCM_AGENT_INSTALLED=2.4.0_beta1\n", "2.4.0~beta.1", false},
		{"pacman-Beta", pkgPacman, "LCM_AGENT_INSTALLED=2.4.0beta.1-1\n", "2.4.0~beta.1", false},
		{"nichts getauscht", pkgZypper, "LCM_AGENT_INSTALLED=2.3.0-1\n", "2.3.1", true},
		{"apt", pkgApt, "LCM_AGENT_INSTALLED=2.3.0\n", "2.3.1", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := checkInstalledAgent(tt.mgr, tt.out, tt.target); (err != nil) != tt.wantErr {
				t.Errorf("fehler = %v, erwartet %v", err, tt.wantErr)
			}
		})
	}
}
