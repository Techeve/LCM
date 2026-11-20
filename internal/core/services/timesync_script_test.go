package services

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Die Zeit-Skripte laufen hier wirklich: gegen ein nachgebautes /etc/chrony
// und mit Attrappen für systemctl, rc-service, chronyc und adjtimex im PATH.

// timeSandbox ist ein Lauf eines Zeit-Skripts.
type timeSandbox struct {
	dir string
	bin string
}

// newTimeSandbox legt die Dateien (Pfad relativ zur Sandbox) und die
// Attrappen an. Jedes nicht genannte Werkzeug scheitert - sonst griffe das
// echte systemctl der Test-Maschine durch.
func newTimeSandbox(t *testing.T, stubs, files map[string]string) timeSandbox {
	t.Helper()
	sb := timeSandbox{dir: t.TempDir()}
	sb.bin = filepath.Join(sb.dir, "bin")
	for _, tool := range []string{"systemctl", "rc-service", "chronyc", "adjtimex", "timedatectl", "pgrep", "ntpq", "sleep"} {
		if _, ok := stubs[tool]; !ok {
			stubs[tool] = "exit 1"
		}
	}
	for tool, body := range stubs {
		writeSandboxFile(t, filepath.Join(sb.bin, tool), "#!/bin/sh\n"+body+"\n", 0o755)
	}
	for path, content := range files {
		writeSandboxFile(t, filepath.Join(sb.dir, path), content, 0o644)
	}
	return sb
}

func writeSandboxFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

// run führt das Skript mit umgebogenen Pfaden aus.
func (sb timeSandbox) run(t *testing.T, script string) (string, int) {
	t.Helper()
	script = strings.ReplaceAll(script, "/etc/chrony", sb.dir+"/etc/chrony")
	script = strings.ReplaceAll(script, "/etc/ntp.conf", sb.dir+"/etc/ntp.conf")
	cmd := exec.Command("sh", "-c", script)
	cmd.Env = []string{"PATH=" + sb.bin + ":/usr/bin:/bin"}
	out, err := cmd.CombinedOutput()
	if exit, ok := err.(*exec.ExitError); ok {
		return string(out), exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return string(out), 0
}

func (sb timeSandbox) read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(sb.dir, path))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// alpineChronyConf ist /etc/chrony/chrony.conf aus dem chrony-Paket von
// Alpine 3.22 - mit cmdport 0, also ohne Antwort für Nicht-root.
const alpineChronyConf = "# default config\n\npool pool.ntp.org iburst\ninitstepslew 10 pool.ntp.org\ndriftfile /var/lib/chrony/chrony.drift\nrtcsync\ncmdport 0\n"

// ubuntuChrony sind die Dateien von chrony unter Ubuntu: Die Pools stehen
// nur noch in sources.d, eigene Server in conf.d.
func ubuntuChrony() map[string]string {
	return map[string]string{
		"etc/chrony/chrony.conf":                        "confdir /etc/chrony/conf.d\nsourcedir /run/chrony-dhcp\nsourcedir /etc/chrony/sources.d\ndriftfile /var/lib/chrony/chrony.drift\n",
		"etc/chrony/sources.d/ubuntu-ntp-pools.sources": "# Use Ubuntu's ntp server as a fallback.\npool 1.ntp.ubuntu.com iburst maxsources 1\npool ntp.ubuntu.com iburst maxsources 1\n",
		"etc/chrony/conf.d/lokal.conf":                  "server zeit.intern iburst\n",
	}
}

// TestZeitZustandAlpineChrony: Unter OpenRC lief chronyd in den ntpd-Zweig -
// LCM meldete ntpd, las /etc/ntp.conf und fragte ntpq. Ohne root antwortet
// chronyc auf Alpine nicht (cmdport 0); dann zählt der Kernel-Status.
func TestZeitZustandAlpineChrony(t *testing.T) {
	sb := newTimeSandbox(t, map[string]string{
		"rc-service": `[ "$1 $2" = "chronyd status" ]`,
		"chronyc":    "echo '506 Cannot talk to daemon'; exit 1",
		"adjtimex":   "echo '    return value: 0 (clock synchronized)'",
	}, map[string]string{"etc/chrony/chrony.conf": alpineChronyConf})
	out, _ := sb.run(t, timeStateScript)
	st := parseTimeState(out)
	if st.NTPService != "chrony" || !st.NTPSync || st.NTPServers != "pool.ntp.org" {
		t.Errorf("alpine-chrony falsch gelesen: %+v\n%s", st, out)
	}
}

// TestZeitZustandBusyboxNtpdAufAlpine: Der Dienst ntpd aus busybox-openrc
// ist das ntpd von BusyBox - ohne /etc/ntp.conf gehört er nicht in den
// Zweig des klassischen ntpd.
func TestZeitZustandBusyboxNtpdAufAlpine(t *testing.T) {
	sb := newTimeSandbox(t, map[string]string{"rc-service": `[ "$1 $2" = "ntpd status" ]`}, nil)
	out, _ := sb.run(t, timeStateScript)
	if st := parseTimeState(out); st.NTPService != "busybox-ntpd" {
		t.Errorf("busybox-ntpd nicht erkannt: %+v\n%s", st, out)
	}
}

// TestZeitZustandLiestChronyDropins: Ubuntu führt seine Pools nur noch in
// sources.d - gelesen wurde allein chrony.conf, die Server-Liste blieb leer.
func TestZeitZustandLiestChronyDropins(t *testing.T) {
	sb := newTimeSandbox(t, map[string]string{
		"systemctl": `[ "$1 $2 $3" = "is-active --quiet chrony" ]`,
		"chronyc":   "echo 'Leap status     : Normal'",
	}, ubuntuChrony())
	out, _ := sb.run(t, timeStateScript)
	st := parseTimeState(out)
	if st.NTPService != "chrony" || !st.NTPSync {
		t.Errorf("chrony nicht erkannt: %+v", st)
	}
	for _, want := range []string{"1.ntp.ubuntu.com", "ntp.ubuntu.com", "zeit.intern"} {
		if !strings.Contains(st.NTPServers, want) {
			t.Errorf("server %q fehlt: %q", want, st.NTPServers)
		}
	}
}

// TestNTPSetztChronyDropinsAusser: Blieben die Pools in sources.d stehen,
// fragte chrony sie neben den Zeitservern von LCM weiter. Voll-Modus und
// Helper bekommen dasselbe Ergebnis.
func TestNTPSetztChronyDropinsAusser(t *testing.T) {
	helper := strings.Join([]string{
		"NL='\n'",
		`die() { echo "$*" >&2; exit 1; }`,
		helperFunktion(t, "matches"),
		helperFunktion(t, "set_ntp"),
		"set_ntp zeit.example.org",
	}, "\n")
	for name, script := range map[string]string{
		"voll-modus": ntpApplyScript([]string{"zeit.example.org"}),
		"helper":     helper,
	} {
		t.Run(name, func(t *testing.T) {
			sb := newTimeSandbox(t, map[string]string{
				"systemctl": "exit 0",
				"chronyc":   `case "$*" in *tracking*) echo 'Leap status     : Normal' ;; esac`,
			}, ubuntuChrony())
			if out, code := sb.run(t, script); code != 0 {
				t.Fatalf("exit %d: %s", code, out)
			}
			if conf := sb.read(t, "etc/chrony/chrony.conf"); !strings.Contains(conf, "server zeit.example.org iburst") || !strings.Contains(conf, "sourcedir /etc/chrony/sources.d") {
				t.Errorf("chrony.conf falsch:\n%s", conf)
			}
			pools := sb.read(t, "etc/chrony/sources.d/ubuntu-ntp-pools.sources")
			if strings.Contains(pools, "\npool ") || !strings.Contains(pools, "# Use Ubuntu") {
				t.Errorf("pools in sources.d nicht abgeschaltet:\n%s", pools)
			}
			if lokal := sb.read(t, "etc/chrony/conf.d/lokal.conf"); strings.Contains(lokal, "server zeit.intern") {
				t.Errorf("server in conf.d nicht abgeschaltet:\n%s", lokal)
			}
			if backup := sb.read(t, "etc/chrony/sources.d/ubuntu-ntp-pools.sources.lcm-bak"); !strings.Contains(backup, "pool ntp.ubuntu.com") {
				t.Errorf("sicherung fehlt:\n%s", backup)
			}
		})
	}
}
