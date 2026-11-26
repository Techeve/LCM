package services

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"LCM/internal/core/agentcaps"
	"LCM/internal/version"
)

// Prüft die Agent-Skripte gegen echte Paketverwaltungen in Containern: einrichten
// wie ein Anwender, Versionen lesen, Systemupdate (der Agent bleibt stehen),
// gezielt hoch, Beta, Nachziehen nach unten, Fehlerfälle.
//
// Läuft nur über testing/agent-pkg-e2e.sh (setzt LCM_AGENT_PKG_DOCKER=1 und
// LCM_AGENT_PKG_DIR mit den gebauten Paketen); sonst übersprungen.
// LCM_AGENT_PKG_RECORD=<Verzeichnis> schreibt die rohen Ausgaben der Skripte für
// die Fixtures in testdata/agent-pkg.

type dockerPkgCase struct {
	name, image, mgr, prep string
	// onlyNewest: Die Paketquelle führt nur die neueste Version (pacman).
	onlyNewest bool
}

var dockerPkgCases = []dockerPkgCase{
	{name: "dnf4-alma9", image: "almalinux:9", mgr: pkgDnf, prep: "prep-dnf.sh"},
	{name: "dnf4-alma10", image: "almalinux:10", mgr: pkgDnf, prep: "prep-dnf.sh"},
	{name: "dnf5-fedora", image: "fedora:latest", mgr: pkgDnf, prep: "prep-dnf.sh"},
	{name: "zypper-leap", image: "opensuse/leap:latest", mgr: pkgZypper, prep: "prep-zypper.sh"},
	{name: "zypper-tumbleweed", image: "opensuse/tumbleweed:latest", mgr: pkgZypper, prep: "prep-zypper.sh"},
	{name: "apk2-alpine322", image: "alpine:3.22", mgr: pkgApk, prep: "prep-apk.sh"},
	{name: "apk3-alpine324", image: "alpine:3.24", mgr: pkgApk, prep: "prep-apk.sh"},
	{name: "pacman-arch", image: "archlinux:latest", mgr: pkgPacman, prep: "prep-pacman.sh", onlyNewest: true},
}

func TestAgentPkgDocker(t *testing.T) {
	if os.Getenv("LCM_AGENT_PKG_DOCKER") != "1" {
		t.Skip("nur über testing/agent-pkg-e2e.sh (LCM_AGENT_PKG_DOCKER=1)")
	}
	pkgs := os.Getenv("LCM_AGENT_PKG_DIR")
	if pkgs == "" {
		t.Fatal("LCM_AGENT_PKG_DIR fehlt")
	}
	prepDir, err := filepath.Abs("../../../testing/agent-pkg")
	if err != nil {
		t.Fatal(err)
	}
	only := os.Getenv("LCM_AGENT_PKG_ONLY")
	for _, c := range dockerPkgCases {
		if only != "" && !strings.Contains(c.name, only) {
			continue
		}
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			ct := startPkgContainer(t, c, pkgs, prepDir)
			ct.run(t, c)
		})
	}
}

type pkgContainer struct{ name, record string }

// startPkgContainer startet den Container und richtet das Repository samt
// Erstinstallation (lcm-agent 2.3.0) ein.
func startPkgContainer(t *testing.T, c dockerPkgCase, pkgs, prepDir string) *pkgContainer {
	t.Helper()
	name := "lcm-agent-pkg-" + c.name
	_ = exec.Command("docker", "rm", "-f", name).Run()
	out, err := exec.Command("docker", "run", "-d", "--name", name,
		"-v", pkgs+":/pkgs:ro", "-v", prepDir+":/prep:ro", "-e", "PKGS=/pkgs", c.image, "sleep", "3600").CombinedOutput()
	if err != nil {
		t.Fatalf("container nicht gestartet: %v\n%s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", name).Run() })
	ct := &pkgContainer{name: name, record: os.Getenv("LCM_AGENT_PKG_RECORD")}
	if out, code := ct.exec(t, "sh /prep/"+c.prep); code != 0 {
		t.Fatalf("einrichten gescheitert (exit %d):\n%s", code, out)
	}
	return ct
}

// exec führt das Skript im Container aus, wie es der Agent täte (sh -c).
func (ct *pkgContainer) exec(t *testing.T, script string) (string, int) {
	t.Helper()
	cmd := exec.Command("docker", "exec", ct.name, "sh", "-c", script)
	out, err := cmd.CombinedOutput()
	if ee, ok := err.(*exec.ExitError); ok {
		return string(out), ee.ExitCode()
	}
	if err != nil {
		t.Fatalf("docker exec: %v", err)
	}
	return string(out), 0
}

// save legt die rohe Ausgabe für die Fixtures ab.
func (ct *pkgContainer) save(t *testing.T, label, out string) {
	t.Helper()
	if ct.record == "" {
		return
	}
	if err := os.WriteFile(filepath.Join(ct.record, label+".txt"), []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
}

// versions liest, was der Server führt (mit Erneuern der Listen).
func (ct *pkgContainer) versions(t *testing.T, c dockerPkgCase, label string) agentVersions {
	t.Helper()
	out, code := ct.exec(t, agentVersionsScriptFor(c.mgr, true))
	if code != 0 {
		t.Fatalf("versionsskript: exit %d\n%s", code, out)
	}
	ct.save(t, c.name+"-"+label, out)
	return parseAgentVersions(c.mgr, out)
}

// install installiert genau diese Version und liefert Ausgabe und Exit-Code.
func (ct *pkgContainer) install(t *testing.T, c dockerPkgCase, ver, label string) (string, int) {
	t.Helper()
	script, err := agentInstallScriptFor(c.mgr, ver)
	if err != nil {
		t.Fatal(err)
	}
	out, code := ct.exec(t, script)
	ct.save(t, c.name+"-"+label, out)
	return out, code
}

// wantInstalled prüft die installierte Version über das Versionsskript (ohne
// Erneuern der Listen).
func (ct *pkgContainer) wantInstalled(t *testing.T, c dockerPkgCase, want, step string) {
	t.Helper()
	out, _ := ct.exec(t, agentVersionsScriptFor(c.mgr, false))
	got := parseAgentVersions(c.mgr, out).Installed
	if !agentcaps.SameVersion(got, want) {
		t.Fatalf("%s: installiert ist %q, erwartet %q\n%s", step, got, want, out)
	}
}

func (ct *pkgContainer) run(t *testing.T, c dockerPkgCase) {
	found := ct.versions(t, c, "versions")
	if found.NoManager || found.NotPackaged || found.RefreshFailed {
		t.Fatalf("Ausgangslage: %+v", found)
	}
	if found.Installed != "2.3.0" {
		t.Fatalf("installiert = %q, erwartet 2.3.0", found.Installed)
	}
	wantAvail := []string{"2.2.1", "2.3.0", "2.3.1", "2.4.0~beta.1", "2.4.0~beta.2"}
	if c.onlyNewest {
		wantAvail = []string{"2.4.0~beta.2"}
	}
	if strings.Join(sortedVersions(found.Available), " ") != strings.Join(wantAvail, " ") {
		t.Fatalf("verfügbar = %v, erwartet %v", found.Available, wantAvail)
	}

	// Ein Systemupdate lässt den Agenten stehen.
	if out, code := ct.exec(t, pkgUpgradeAllScript(c.mgr)); code != 0 {
		t.Fatalf("systemupdate: exit %d\n%s", code, tail(out))
	}
	ct.wantInstalled(t, c, "2.3.0", "nach dem Systemupdate")

	// Gezielt hoch (bei pacman die neueste der Datenbank), dann hält er wieder.
	up := "2.3.1"
	if c.onlyNewest {
		up = "2.4.0~beta.2"
	}
	if out, code := ct.install(t, c, up, "install-up"); code != 0 {
		t.Fatalf("installation %s: exit %d\n%s", up, code, tail(out))
	}
	ct.wantInstalled(t, c, up, "nach der Installation")
	if out, code := ct.exec(t, pkgUpgradeAllScript(c.mgr)); code != 0 {
		t.Fatalf("systemupdate: exit %d\n%s", code, tail(out))
	}
	ct.wantInstalled(t, c, up, "nach dem zweiten Systemupdate")

	// Nachziehen nach unten (nach einem Rollback) und eine Beta nach oben.
	if !c.onlyNewest {
		for _, v := range []string{"2.4.0~beta.1", "2.3.1"} {
			if out, code := ct.install(t, c, v, "install-"+v); code != 0 {
				t.Fatalf("installation %s: exit %d\n%s", v, code, tail(out))
			}
			ct.wantInstalled(t, c, v, "Wechsel auf "+v)
		}
	} else {
		if out, code := ct.install(t, c, "2.3.1", "install-down"); code != 0 {
			t.Fatalf("nachziehen auf 2.3.1: exit %d\n%s", code, tail(out))
		}
		ct.wantInstalled(t, c, "2.3.1", "nach dem Nachziehen")
	}
	if out, code := ct.exec(t, pkgUpgradeAllScript(c.mgr)); code != 0 {
		t.Fatalf("systemupdate: exit %d\n%s", code, tail(out))
	}
	ct.wantInstalled(t, c, "2.3.1", "nach dem dritten Systemupdate")

	// Eine Version, die es nicht gibt, scheitert und ändert nichts.
	if out, code := ct.install(t, c, "9.9.9", "install-missing"); code == 0 {
		t.Fatalf("installation von 9.9.9 meldet Erfolg\n%s", out)
	}
	ct.wantInstalled(t, c, "2.3.1", "nach der fehlgeschlagenen Installation")
}

func sortedVersions(v []string) []string {
	out := append([]string(nil), v...)
	sort.Slice(out, func(i, j int) bool { return version.Compare(out[i], out[j]) < 0 })
	return out
}

func tail(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > 30 {
		lines = lines[len(lines)-30:]
	}
	return strings.Join(lines, "\n")
}
