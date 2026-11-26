package agent

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

// TestPaketSkripte führt packaging/scripts/agent-scripts-test.sh aus: Die
// Paket-Skripte stoppen den Agenten nur beim Entfernen und starten ihn nach
// einem Upgrade nie aus einem seiner eigenen Kommandos heraus neu - für deb,
// rpm, apk und pacman. Hier, damit es mit go test in jeder Pipeline läuft.
func TestPaketSkripte(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("kein sh")
	}
	out, err := exec.Command(sh, "../../packaging/scripts/agent-scripts-test.sh").CombinedOutput()
	if err != nil {
		t.Fatalf("paket-skripte: %v\n%s", err, out)
	}
}

// TestPaketVersion prüft die Schreibweise der Version je Paketformat. Die
// erwarteten Werte sind an echtem nfpm 2.48, apk, pacman, rpm und dpkg
// nachgemessen: Jede Beta ist gültig und sortiert vor ihrem Finale.
func TestPaketVersion(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("kein sh")
	}
	cases := []struct{ format, in, want string }{
		{"deb", "2.0.0-beta.16", "2.0.0-beta.16"},
		{"rpm", "2.0.0-beta.16", "2.0.0-beta.16"},
		{"apk", "2.0.0-beta.16", "2.0.0-beta16"},
		{"apk", "2.0.0-rc.1", "2.0.0-rc1"},
		{"apk", "2.0.0", "2.0.0"},
		{"archlinux", "2.0.0-beta.16", "2.0.0beta.16"},
		{"archlinux", "2.0.0", "2.0.0"},
	}
	for _, c := range cases {
		out, err := exec.Command(sh, "../../packaging/pkg-version.sh", c.format, c.in).CombinedOutput()
		if err != nil {
			t.Fatalf("%s %s: %v\n%s", c.format, c.in, err, out)
		}
		if got := strings.TrimSpace(string(out)); got != c.want {
			t.Errorf("%s %s: %q, erwartet %q", c.format, c.in, got, c.want)
		}
	}
	if err := exec.Command(sh, "../../packaging/pkg-version.sh", "snap", "2.0.0").Run(); err == nil {
		t.Error("unbekanntes Format muss scheitern")
	}
}

// unitSections liest packaging/lcm-agent.service als Abschnitt → Schlüssel →
// Wert.
func unitSections(t *testing.T) map[string]map[string]string {
	t.Helper()
	raw, err := os.ReadFile("../../packaging/lcm-agent.service")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]map[string]string{}
	section := ""
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "" || strings.HasPrefix(line, "#"):
		case strings.HasPrefix(line, "["):
			section = strings.Trim(line, "[]")
			out[section] = map[string]string{}
		default:
			key, value, ok := strings.Cut(line, "=")
			if !ok || section == "" {
				t.Fatalf("zeile %q gehört zu keinem abschnitt", line)
			}
			out[section][key] = value
		}
	}
	return out
}

// TestSystemdUnit sichert die Selbstheilung (Paket B-3): Wächter, den der
// Agent selbst einrichtet (NotifyAccess=main, keine WatchdogSec=), Neustart
// ohne Ende, und nur Optionen, die systemd 247 (Debian 11)
// kennt - eine neuere Option ließe die Unit dort stillschweigend halb
// wirkungslos oder stürzte die Verwaltung ab.
func TestSystemdUnit(t *testing.T) {
	unit := unitSections(t)
	want := []struct{ section, key, value string }{
		{"Unit", "StartLimitIntervalSec", "0"}, // in [Unit], nicht in [Service]
		{"Service", "Type", "simple"},
		{"Service", "NotifyAccess", "main"},
		{"Service", "Restart", "always"},
		{"Service", "RestartSec", "10"},
		// Startskript statt Binary: Es zählt Starts und fällt zurück (D-4).
		{"Service", "ExecStart", guardScriptPath + " run"},
		// Zustandsdatei für `lcm-agent status` (H-1): bleibt bei einem Neustart stehen.
		{"Service", "RuntimeDirectory", "lcm-agent"},
		{"Service", "RuntimeDirectoryMode", "0755"},
		{"Service", "RuntimeDirectoryPreserve", "restart"},
	}
	for _, w := range want {
		if got := unit[w.section][w.key]; got != w.value {
			t.Errorf("[%s] %s=%q, erwartet %q", w.section, w.key, got, w.value)
		}
	}
	// WatchdogSec= in der Unit würde beim Paket-Update den alten, nicht
	// fütternden Agent samt laufendem apt töten (siehe Kommentar in der Unit).
	if _, ok := unit["Service"]["WatchdogSec"]; ok {
		t.Error("WatchdogSec gehört nicht in die Unit - der Agent richtet den Wächter selbst ein")
	}
	if _, ok := unit["Service"]["KillMode"]; ok {
		t.Error("KillMode gehört zu Paket C - hier unverändert (Vorgabe control-group)")
	}

	// Alle Optionen, die systemd 247 kennt und die die Unit nutzen darf.
	known := map[string]map[string]bool{
		"Unit": {
			"Description": true, "Documentation": true, "After": true, "Wants": true,
			"ConditionPathExists": true, "StartLimitIntervalSec": true,
		},
		"Service": {
			"Type": true, "NotifyAccess": true, "ExecStart": true,
			"Restart": true, "RestartSec": true, "User": true,
			"RuntimeDirectory": true, "RuntimeDirectoryMode": true, "RuntimeDirectoryPreserve": true,
		},
		"Install": {"WantedBy": true},
	}
	for section, keys := range unit {
		for key := range keys {
			if !known[section][key] {
				t.Errorf("[%s] %s ist nicht als systemd-247-tauglich freigegeben (packaging_test.go)", section, key)
			}
		}
	}
}

// TestOpenRCDienst: Ohne systemd hält supervise-daemon den Agent am Leben - mit
// Neustart ohne Ende, wie Restart=always.
func TestOpenRCDienst(t *testing.T) {
	raw, err := os.ReadFile("../../packaging/lcm-agent.openrc")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{
		`supervisor="supervise-daemon"`,
		`respawn_delay=10`,
		`respawn_max=0`, // 0 = nie aufgeben
		`command_args="run"`,
		`command="` + guardScriptPath + `"`, // das Startskript, nicht das Binary (D-4)
	} {
		if !strings.Contains(string(raw), line+"\n") {
			t.Errorf("openrc-dienst ohne %s", line)
		}
	}
}

// TestSelbstinstallationsUnit: Die Unit, die enroll bei einem von Hand
// installierten Binary schreibt, trägt dieselben Selbstheilungs-Optionen wie die
// des Pakets.
func TestSelbstinstallationsUnit(t *testing.T) {
	for _, line := range []string{"Type=simple", "NotifyAccess=main", "StartLimitIntervalSec=0", "Restart=always"} {
		if !strings.Contains(unitTemplate, line+"\n") {
			t.Errorf("unitTemplate (enroll.go) ohne %s", line)
		}
	}
	if strings.Contains(unitTemplate, "\nWatchdogSec=") {
		t.Error("unitTemplate (enroll.go) darf keine WatchdogSec= setzen")
	}
}

// TestPaketInhalt (D-1, D-4): Das Paket bringt das Startskript an dem Ort mit,
// den Unit, OpenRC-Dienst und Agent nennen, die Ausnahme für needrestart und die
// Skripte für Sicherung des alten Binarys und Pin.
func TestPaketInhalt(t *testing.T) {
	raw, err := os.ReadFile("../../packaging/nfpm-agent.yaml")
	if err != nil {
		t.Fatal(err)
	}
	nfpm := string(raw)
	for _, want := range []string{
		"src: ./packaging/lcm-agent-start.sh\n    dst: " + guardScriptPath + "\n",
		"dst: /etc/needrestart/conf.d/lcm-agent.conf\n    packager: deb\n",
		"preinstall: ./packaging/scripts/agent-preinstall.sh\n",
		"postinstall: ./packaging/scripts/agent-postinstall.sh\n",
		"preupgrade: ./packaging/scripts/agent-preinstall.sh\n", // apk und pacman rufen preinstall beim Upgrade nicht
	} {
		if !strings.Contains(nfpm, want) {
			t.Errorf("nfpm-agent.yaml ohne %q", want)
		}
	}
	if n := strings.Count(nfpm, "preupgrade: ./packaging/scripts/agent-preinstall.sh"); n != 2 {
		t.Errorf("preupgrade für apk und archlinux: %d Einträge, erwartet 2", n)
	}
	for _, f := range []string{"lcm-agent-start.sh", "scripts/agent-preinstall.sh", "scripts/agent-postinstall.sh", "scripts/agent-postremove.sh"} {
		fi, err := os.Stat("../../packaging/" + f)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm()&0o100 == 0 {
			t.Errorf("%s ist nicht ausführbar im Repository", f)
		}
	}
	conf, err := os.ReadFile("../../packaging/needrestart-lcm-agent.conf")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(conf), "$nrconf{override_rc}{qr(^lcm-agent)} = 0;\n") {
		t.Error("needrestart-Ausnahme fehlt")
	}
}

// TestStartskriptPasstZumAgenten: Die Grenze der ungesunden Starts und die
// Dateinamen stehen im Skript und im Agenten - sie müssen übereinstimmen.
func TestStartskriptPasstZumAgenten(t *testing.T) {
	raw, err := os.ReadFile("../../packaging/lcm-agent-start.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	for _, want := range []string{
		"MAX_STARTS=" + strconv.Itoa(maxStarts) + "\n",
		`PREV="$STATE/` + prevBinaryName + `"`,
		`STATE="${LCM_AGENT_STATE:-` + UpdateDirDefault + `}"`,
		`BIN="${LCM_AGENT_BIN:-` + debBinaryPath + `}"`,
		`reason=crash-loop`,
	} {
		if !strings.Contains(script, want) {
			t.Errorf("lcm-agent-start.sh ohne %q", want)
		}
	}
	for _, name := range []string{"starts", "mode", "cycles", "fail-reason", "rolled-back"} {
		if !strings.Contains(script, name) {
			t.Errorf("lcm-agent-start.sh kennt %q nicht", name)
		}
	}
}
