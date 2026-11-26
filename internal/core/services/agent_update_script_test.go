package services

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"LCM/internal/core/agentcaps"
	"LCM/internal/version"
)

// fakeAgentHost baut ein Verzeichnis mit gefälschten dpkg-query, apt-cache und
// apt-get: das Paket lcm-agent ist in installed installiert (leer: nicht
// installiert), apt-cache madison nennt madison, und jeder Aufruf steht in calls.
func fakeAgentHost(t *testing.T, installed, madison string, aptExit int) (bin, calls string) {
	t.Helper()
	bin, calls = fakeDpkg(t, "")
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write("dpkg-query", `case "$*" in
  *Status-Abbrev*) if [ -n "`+installed+`" ]; then printf 'ii '; else printf 'un '; fi ;;
  *Version*) printf '%s' '`+installed+`' ;;
esac
exit 0
`)
	write("apt-cache", "cat <<'EOF'\n"+madison+"\nEOF\n")
	write("apt-get", "echo \"apt-get $*\" >>"+calls+"\nexit "+strconv.Itoa(aptExit)+"\n")
	return bin, calls
}

func runAgentShell(t *testing.T, bin, script string) (string, int) {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", script)
	cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("skript nicht startbar: %v", err)
	}
	return string(out), code
}

const madisonSample = ` lcm-agent | 2.4.0~beta.2 | https://repo.techeve.de beta/main amd64 Packages
 lcm-agent | 2.3.0 | https://repo.techeve.de community/main amd64 Packages
 lcm-agent | 2.2.1 | https://repo.techeve.de stable/main amd64 Packages
 lcm-agent | 2.2.1 | https://repo.techeve.de community/main amd64 Packages`

// TestAgentVersionsSkriptLiestInstalliertUndVerfuegbar: Das Skript meldet die
// installierte Version und die Versionen der Quellen, LCM wertet beides aus.
func TestAgentVersionsSkriptLiestInstalliertUndVerfuegbar(t *testing.T) {
	bin, calls := fakeAgentHost(t, "2.2.0", madisonSample, 0)
	out, code := runAgentShell(t, bin, agentVersionsScript(true))
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	got := parseAgentVersions(pkgApt, out)
	if got.Installed != "2.2.0" || len(got.Available) != 3 {
		t.Fatalf("ausgewertet: %+v\nausgabe:\n%s", got, out)
	}
	if got.Sources["2.3.0"] != "https://repo.techeve.de community/main amd64 Packages" {
		t.Errorf("quelle der 2.3.0 = %q", got.Sources["2.3.0"])
	}
	// Mit refresh: erst die Paketlisten erneuern (apt-get update), dann lesen.
	log, _ := os.ReadFile(calls)
	if !strings.Contains(string(log), "apt-get update") {
		t.Errorf("paketlisten nicht erneuert: %q", log)
	}
}

// TestAgentVersionsSkriptOhneRefreshLaesstDieListenInRuhe: Die Vorschau
// (nur lesend) ruft kein apt-get auf.
func TestAgentVersionsSkriptOhneRefreshLaesstDieListenInRuhe(t *testing.T) {
	bin, calls := fakeAgentHost(t, "2.2.0", madisonSample, 0)
	if _, code := runAgentShell(t, bin, agentVersionsScript(false)); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if log, _ := os.ReadFile(calls); strings.Contains(string(log), "apt-get") {
		t.Errorf("die vorschau rief apt-get auf: %q", log)
	}
}

// TestAgentVersionsSkriptMeldetNichtPaketierteAgenten: Ein von Hand kopierter
// Agent ist kein Paket - das Skript sagt es, statt ein Update zu versuchen.
func TestAgentVersionsSkriptMeldetNichtPaketierteAgenten(t *testing.T) {
	bin, _ := fakeAgentHost(t, "", madisonSample, 0)
	out, code := runAgentShell(t, bin, agentVersionsScript(true))
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	plan := planAgentUpdate(parseAgentVersions(pkgApt, out), "2.3.0")
	if plan.Action != agentActionUnsupported || plan.Reason != agentReasonNotPackaged {
		t.Errorf("plan = %+v", plan)
	}
}

// TestAgentVersionsSkriptBeiAptFehlerLiestTrotzdem: Scheitert das Erneuern der
// Listen (eine Quelle antwortet nicht), gilt der Stand der Listen - mit Marke.
func TestAgentVersionsSkriptBeiAptFehlerLiestTrotzdem(t *testing.T) {
	bin, _ := fakeAgentHost(t, "2.2.0", madisonSample, 1)
	out, code := runAgentShell(t, bin, agentVersionsScript(true))
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	got := parseAgentVersions(pkgApt, out)
	if !got.RefreshFailed || len(got.Available) == 0 {
		t.Errorf("ausgewertet: %+v", got)
	}
}

// TestAgentInstallSkript: genau diese Version, unter dem Sperren-Vorspann, mit
// Downgrade- und Hold-Freigabe nur für diesen Aufruf.
func TestAgentInstallSkript(t *testing.T) {
	script := agentInstallScript("2.3.0")
	for _, want := range []string{
		"lcm_apt install -y --allow-downgrades --allow-change-held-packages lcm-agent=2.3.0",
		"lcm_apt_wait", // Teil des Vorspanns
	} {
		if !strings.Contains(script, want) {
			t.Errorf("skript enthält %q nicht", want)
		}
	}
	bin, calls := fakeAgentHost(t, "2.3.0", madisonSample, 0)
	out, code := runAgentShell(t, bin, script)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	log, _ := os.ReadFile(calls)
	if !strings.Contains(string(log), "install -y --allow-downgrades --allow-change-held-packages lcm-agent=2.3.0") {
		t.Errorf("apt-get-aufruf: %q", log)
	}
	if !strings.Contains(out, agentMarkInstalled+"2.3.0") {
		t.Errorf("installierte version nicht gemeldet: %q", out)
	}
}

// TestAgentInstallSkriptGibtDenExitCodeWeiter: Ein Fehler von apt bleibt ein
// Fehler des Jobs.
func TestAgentInstallSkriptGibtDenExitCodeWeiter(t *testing.T) {
	bin, _ := fakeAgentHost(t, "2.2.0", madisonSample, 100)
	if _, code := runAgentShell(t, bin, agentInstallScript("2.3.0")); code != 100 {
		t.Errorf("exit = %d, want 100", code)
	}
}

// TestAgentUpdateInstalliertNichtVorDerAuswahl: Die Skripte der Auswahl
// enthalten kein Installationsverb - der Lesevorgang ändert nichts.
func TestAgentUpdateInstalliertNichtVorDerAuswahl(t *testing.T) {
	body := agentVersionsScript(true)[len(aptPrelude):]
	if strings.Contains(body, "install") {
		t.Errorf("die versionsabfrage enthält ein installationsverb:\n%s", body)
	}
}

// TestPlanAgentUpdate: die Einordnung aus Ausgabe und LCM-Version.
func TestPlanAgentUpdate(t *testing.T) {
	old := version.Version
	t.Cleanup(func() { version.Version = old })
	version.Version = "2.3.0"
	read := func(installed, madison string) agentVersions {
		bin, _ := fakeAgentHost(t, installed, madison, 0)
		out, _ := runAgentShell(t, bin, agentVersionsScript(false))
		return parseAgentVersions(pkgApt, out)
	}
	tests := []struct {
		name      string
		installed string
		ceiling   string
		action    string
		target    string
	}{
		{"hinterher", "2.2.0", "", "update", "2.3.0"},
		{"aktuell", "2.3.0", "", "current", "2.3.0"},
		{"gestaffelt unter LCM", "2.2.0", "2.2.1", "update", "2.2.1"},
		{"neuer als LCM", "2.5.0", "", "newer", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := planAgentUpdate(read(tt.installed, madisonSample), tt.ceiling)
			if plan.Action != tt.action || plan.Target != tt.target {
				t.Fatalf("plan = %+v, want %s %q", plan, tt.action, tt.target)
			}
		})
	}
	// LCM ist ein Entwicklungsstand: keine Zielversion.
	version.Version = "0.0.0-dev"
	if plan := planAgentUpdate(read("2.2.0", madisonSample), ""); plan.Action != string(agentcaps.ActionNone) || plan.Reason != agentReasonUnknownVersion {
		t.Errorf("dev-LCM: %+v", plan)
	}
}
