package services

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"LCM/internal/core/domain"
)

func lcmHost(restricted bool) *domain.Server {
	return &domain.Server{Name: "lcm", Host: "localhost", SSHPort: 22, PackageManager: pkgApt,
		ServiceUser: "lcm-svc", RestrictedSudo: restricted}
}

// TestLcmHostPackageScriptNurAufDemLcmHost: Abgekoppelt wird nur, was auf
// dem LCM-Host Pakete einspielt. Andere Server, der reine Paket-Scan und
// Nicht-apt-Systeme bleiben, wie sie sind.
func TestLcmHostPackageScriptNurAufDemLcmHost(t *testing.T) {
	const script = "lcm_apt -y upgrade"
	other := &domain.Server{Name: "web01", Host: "10.0.0.5", PackageManager: pkgApt}
	dnfHost := lcmHost(false)
	dnfHost.PackageManager = pkgDnf
	for name, got := range map[string]string{
		"anderer Server": lcmHostPackageScript(other, domain.RuleTypeUpdate, script),
		"Paket-Scan":     lcmHostPackageScript(lcmHost(false), domain.RuleTypePackageScan, script),
		"dnf":            lcmHostPackageScript(dnfHost, domain.RuleTypeUpdate, script),
	} {
		if got != script {
			t.Errorf("%s: Skript verändert:\n%s", name, got)
		}
	}
	for _, jobType := range []string{domain.RuleTypeUpdate, domain.RuleTypeSecurity, domain.RuleTypePackages} {
		got := lcmHostPackageScript(lcmHost(false), jobType, script)
		if !strings.Contains(got, "systemd-run --collect --unit=\"$__u\"") || !strings.Contains(got, "dpkg --audit") {
			t.Errorf("%s auf dem LCM-Host nicht abgekoppelt:\n%s", jobType, got)
		}
	}
	restricted := lcmHostPackageScript(lcmHost(true), domain.RuleTypeUpdate, script)
	if strings.Contains(restricted, "systemd-run") || !strings.Contains(restricted, "dpkg --audit") {
		t.Errorf("eingeschränkter Modus: kein systemd-run, aber dpkg --audit erwartet:\n%s", restricted)
	}
}

// TestLcmHostPackageScriptIstGueltigesSh: Beide Wege bleiben samt sudo-Hülle
// ein gültiges Shell-Skript - ein Quoting-Fehler fiele sonst erst auf dem
// LCM-Host auf.
func TestLcmHostPackageScriptIstGueltigesSh(t *testing.T) {
	for _, restricted := range []bool{false, true} {
		server := lcmHost(restricted)
		script := privRun(server, lcmHostPackageScript(server, domain.RuleTypeUpdate, aptUpgradeAllScript()))
		if out, err := exec.Command("/bin/sh", "-n", "-c", script).CombinedOutput(); err != nil {
			t.Errorf("eingeschränkt=%v: %v\n%s", restricted, err, out)
		}
	}
}

// TestRuleScriptKoppeltAufDemLcmHostAb:Auch die Paket-Regeln laufen auf dem
// LCM-Host abgekoppelt - der Lauf der Regel und ihre Vorschau lesen beide
// ruleScript.
func TestRuleScriptKoppeltAufDemLcmHostAb(t *testing.T) {
	rule := &domain.Rule{Type: domain.RuleTypeSecurity}
	script, ok := ruleScript(nil, nil, lcmHost(false), rule)
	if !ok || !strings.Contains(script, "systemd-run") {
		t.Errorf("Security-Regel auf dem LCM-Host nicht abgekoppelt:\n%s", script)
	}
}

// fakeSystemd legt systemd-run, systemctl und dpkg als Attrappen an: Die
// Unit läuft als Hintergrundprozess, systemctl meldet sie aktiv, solange er
// lebt, und dpkg --audit gibt aus, was in audit steht.
func fakeSystemd(t *testing.T, audit string) (binDir, state string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("braucht eine POSIX-Shell")
	}
	binDir, state = t.TempDir(), t.TempDir()
	files := map[string]string{
		"systemd-run": `#!/bin/sh
while [ $# -gt 0 ] && [ "$1" != /bin/sh ]; do shift; done
"$@" &
echo $! >"` + state + `/pid"
`,
		"systemctl": `#!/bin/sh
if [ -f "` + state + `/pid" ] && kill -0 "$(cat "` + state + `/pid")" 2>/dev/null; then echo active; else echo inactive; fi
`,
		"dpkg": `#!/bin/sh
cat "` + state + `/audit" 2>/dev/null
`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if audit != "" {
		if err := os.WriteFile(filepath.Join(state, "audit"), []byte(audit), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return binDir, state
}

// runDetached führt den abgekoppelten Lauf mit den Attrappen aus.
func runDetached(t *testing.T, audit, script string) (string, int) {
	t.Helper()
	binDir, _ := fakeSystemd(t, audit)
	old := hostPackageDir
	hostPackageDir = filepath.Join(t.TempDir(), "run")
	t.Cleanup(func() { hostPackageDir = old })
	cmd := exec.Command("/bin/sh", "-c", detachedHostPackageScript(script))
	cmd.Env = append(os.Environ(), "PATH="+binDir+":"+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	code := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return string(out), code
}

// TestDetachedHostPackageScriptReichtErgebnisDurch: Die Sitzung liest die
// Ausgabe der Unit mit und endet mit deren Exit-Code.
func TestDetachedHostPackageScriptReichtErgebnisDurch(t *testing.T) {
	out, code := runDetached(t, "", "echo 'Richte lcm ein'; sleep 1; echo fertig; exit 3")
	if code != 3 {
		t.Errorf("Exit-Code %d, erwartet 3\n%s", code, out)
	}
	if !strings.Contains(out, "Richte lcm ein") || !strings.Contains(out, "fertig") {
		t.Errorf("Ausgabe der Unit fehlt:\n%s", out)
	}
}

// TestDetachedHostPackageScriptPrueftDpkg: Meldet apt Erfolg, hinterlässt
// dpkg aber halb eingerichtete Pakete, ist der Lauf gescheitert.
func TestDetachedHostPackageScriptPrueftDpkg(t *testing.T) {
	out, code := runDetached(t, "The following packages are only half configured: lcm", "echo ok")
	if code == 0 {
		t.Errorf("Erfolg trotz angebrochener Pakete:\n%s", out)
	}
	if !strings.Contains(out, "half configured") {
		t.Errorf("Meldung von dpkg fehlt:\n%s", out)
	}
}

// TestHostPackageOutcomeNachNeustart: Nach dem Neustart holt LCM das
// Ergebnis des Laufs ab, der zum Job gehört. Ein Fehlschlag nach dem
// Versionswechsel bleibt ein Fehlschlag; ein Ergebnis von vor dem Job zählt
// nicht.
func TestHostPackageOutcomeNachNeustart(t *testing.T) {
	old := hostPackageDir
	hostPackageDir = t.TempDir()
	t.Cleanup(func() { hostPackageDir = old })
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(hostPackageDir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("out", "E: Sub-process /usr/bin/dpkg returned an error code (1)\n")
	write("rc", "100\n")
	recovery := &SelfUpdateRecovery{Note: "aktualisiert", Outcome: hostPackageOutcome}

	job := &domain.Job{CreatedAt: time.Now().Add(-time.Minute)}
	recovery.complete(job)
	if job.Status != domain.JobStatusFailed || job.ExitCode == nil || *job.ExitCode != 100 {
		t.Errorf("Status %q, Exit-Code %v - erwartet failed/100", job.Status, job.ExitCode)
	}
	if !strings.Contains(job.Output, "returned an error code") {
		t.Errorf("Ausgabe des Laufs fehlt im Protokoll: %q", job.Output)
	}

	later := &domain.Job{CreatedAt: time.Now().Add(time.Hour)}
	recovery.complete(later)
	if later.Status != domain.JobStatusSuccess || strings.Contains(later.Output, "error code") {
		t.Errorf("fremdes Ergebnis übernommen: %q / %q", later.Status, later.Output)
	}
}
