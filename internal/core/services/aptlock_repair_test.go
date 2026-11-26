package services

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeDpkg legt ein dpkg in ein Verzeichnis, das `dpkg --audit` mit audit
// beantwortet und jeden Aufruf in calls festhält.
func fakeDpkg(t *testing.T, audit string) (bin, calls string) {
	t.Helper()
	bin = t.TempDir()
	calls = filepath.Join(t.TempDir(), "calls")
	script := "#!/bin/sh\necho \"$*\" >>" + calls + "\n" +
		"[ \"$1\" = --audit ] && printf '%s' '" + audit + "'\nexit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "dpkg"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	// Ein timeout, das den Aufruf festhält und ihn dann ausführt (macOS hat keins;
	// auf dem Server ist es das von coreutils oder busybox).
	timeout := "#!/bin/sh\necho \"timeout $*\" >>" + calls + "\n[ \"$1\" = -k ] && shift 2\nshift\nexec \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "timeout"), []byte(timeout), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, calls
}

// runPrelude führt snippet hinter dem Vorspann aus, mit dem gefälschten dpkg im
// Suchpfad.
func runPrelude(t *testing.T, bin, snippet string) string {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", aptPrelude+snippet)
	cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("skript scheiterte: %v\n%s", err, out)
	}
	return string(out)
}

// TestDpkgRepairSchliesstAngebrocheneInstallationenAb: Meldet dpkg --audit
// Probleme, läuft dpkg --configure -a, und das Protokoll des Jobs sagt es.
func TestDpkgRepairSchliesstAngebrocheneInstallationenAb(t *testing.T) {
	bin, calls := fakeDpkg(t, "Die folgenden Pakete sind nur teilweise installiert: foo")
	out := runPrelude(t, bin, "lcm_dpkg_repair")
	if !strings.Contains(out, "dpkg --configure -a") || !strings.Contains(out, "teilweise installiert") {
		t.Errorf("job-protokoll sagt nichts über die reparatur: %q", out)
	}
	got, _ := os.ReadFile(calls)
	if !strings.Contains(string(got), "--configure -a") {
		t.Errorf("dpkg --configure -a lief nicht, aufrufe: %q", got)
	}
}

// TestDpkgRepairLaesstSauberenZustandInRuhe: Ohne Befund bleibt es beim
// Audit - kein --configure, keine Ausgabe.
func TestDpkgRepairLaesstSauberenZustandInRuhe(t *testing.T) {
	bin, calls := fakeDpkg(t, "")
	if out := runPrelude(t, bin, "lcm_dpkg_repair"); out != "" {
		t.Errorf("unerwartete ausgabe bei sauberem dpkg: %q", out)
	}
	got, _ := os.ReadFile(calls)
	if strings.Contains(string(got), "--configure") {
		t.Errorf("dpkg --configure lief ohne befund: %q", got)
	}
}

// TestAptRepariertVorDemEigentlichenLauf: lcm_apt wartet erst auf die Sperren,
// repariert dann und beginnt erst danach mit apt-get.
func TestAptRepariertVorDemEigentlichenLauf(t *testing.T) {
	body := aptPrelude[strings.Index(aptPrelude, "lcm_apt() {"):]
	wait := strings.Index(body, "lcm_apt_wait")
	repair := strings.Index(body, "lcm_dpkg_repair")
	run := strings.Index(body, "apt-get")
	if wait < 0 || repair < wait || run < repair {
		t.Errorf("reihenfolge in lcm_apt falsch (wait=%d, repair=%d, apt-get=%d)", wait, repair, run)
	}
}

// TestDpkgRepairLaeuftMitZeitgrenzeUndForceConfold: Ein hängendes
// Konfigurationsskript hält den Lauf nicht ewig fest, und dpkg fragt nie nach
// Konfigurationsdateien.
func TestDpkgRepairLaeuftMitZeitgrenzeUndForceConfold(t *testing.T) {
	bin, calls := fakeDpkg(t, "Die folgenden Pakete sind nur teilweise installiert: foo")
	runPrelude(t, bin, "lcm_dpkg_repair")
	got, _ := os.ReadFile(calls)
	if !strings.Contains(string(got), "timeout -k 30 1800 dpkg --force-confold --configure -a") {
		t.Errorf("dpkg --configure ohne Zeitgrenze oder --force-confold, aufrufe: %q", got)
	}
}

const (
	auditMess = "The following packages are in a mess due to serious problems during\n" +
		"installation.  They must be reinstalled for them (and any packages\n" +
		"that depend on them) to function properly:\n foo  defekt\n"
	auditHalfConfigured = "The following packages are only half-configured, probably due to\n" +
		"problems configuring them the first time.  The configuration\n" +
		"should be retried using dpkg --configure <package>:\n bar  halb\n"
)

// TestDpkgRepairUeberspringtNurReinstreq: Pakete im Zustand reinstreq muss man
// neu installieren, dpkg --configure hilft nicht - es bleibt beim Protokoll.
// Kommt ein anderer Befund dazu, läuft die Reparatur.
func TestDpkgRepairUeberspringtNurReinstreq(t *testing.T) {
	bin, calls := fakeDpkg(t, auditMess)
	out := runPrelude(t, bin, "lcm_dpkg_repair")
	if !strings.Contains(out, "reinstreq") || !strings.Contains(out, "foo  defekt") {
		t.Errorf("job-protokoll nennt die Pakete nicht: %q", out)
	}
	if got, _ := os.ReadFile(calls); strings.Contains(string(got), "--configure") {
		t.Errorf("dpkg --configure lief für reinstreq-Pakete: %q", got)
	}

	bin, calls = fakeDpkg(t, auditMess+"\n"+auditHalfConfigured)
	runPrelude(t, bin, "lcm_dpkg_repair")
	if got, _ := os.ReadFile(calls); !strings.Contains(string(got), "--configure -a") {
		t.Errorf("dpkg --configure lief trotz halb konfigurierter Pakete nicht: %q", got)
	}
}

// TestAptMutiertNurBeiAenderndenBefehlen: Erkannt wird das Verb hinter den
// Optionen; Simulationen und lesende Befehle zählen nicht, alles andere schon.
func TestAptMutiertNurBeiAenderndenBefehlen(t *testing.T) {
	cases := []struct {
		args string
		want bool
	}{
		{"-y --with-new-pkgs upgrade", true},
		{"-f install -y", true},
		{"install --only-upgrade -y foo", true},
		{"-y remove foo bar", true},
		{"-y purge foo", true},
		{"-y autoremove", true},
		{"-y dist-upgrade", true},
		{"-o Dpkg::Options::=--force-confnew install foo", true},
		{"-s upgrade", false},
		{"--simulate install foo", false},
		{"-y --dry-run upgrade", false},
		{"update", false},
		{"list --upgradable", false},
		{"", false},
	}
	for _, tt := range cases {
		out := runPrelude(t, t.TempDir(), "lcm_apt_mutates "+tt.args+"; echo $?")
		if got := strings.TrimSpace(out) == "0"; got != tt.want {
			t.Errorf("lcm_apt_mutates %q = %v, erwartet %v", tt.args, got, tt.want)
		}
	}
}

// TestAptRepariertNurVorVeraendernden: lcm_apt ruft die Reparatur vor install
// auf, vor update oder einer Simulation nicht.
func TestAptRepariertNurVorVeraendernden(t *testing.T) {
	bin, calls := fakeDpkg(t, auditHalfConfigured)
	apt := "#!/bin/sh\necho \"apt-get $*\" >>" + calls + "\n"
	if err := os.WriteFile(filepath.Join(bin, "apt-get"), []byte(apt), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		args   string
		repair bool
	}{{"-y install foo", true}, {"-s upgrade", false}, {"update", false}} {
		if err := os.WriteFile(calls, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		runPrelude(t, bin, "lcm_apt_wait() { :; }\nlcm_apt "+tt.args)
		got, _ := os.ReadFile(calls)
		if repaired := strings.Contains(string(got), "--configure"); repaired != tt.repair {
			t.Errorf("lcm_apt %s: Reparatur=%v, erwartet %v (aufrufe: %q)", tt.args, repaired, tt.repair, got)
		}
		if !strings.Contains(string(got), "apt-get") {
			t.Errorf("lcm_apt %s rief apt-get nicht auf: %q", tt.args, got)
		}
	}
}
