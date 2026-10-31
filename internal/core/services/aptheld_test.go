package services

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"LCM/internal/core/domain"
)

// fakeAptOutput ist die Ausgabe von `apt-get -s --with-new-pkgs upgrade`
// (apt 2.8) mit allen drei Arten zurückgehaltener Pakete.
const fakeAptOutput = `Reading package lists...
Calculating upgrade...
The following packages have been kept back:
  openssl libfoo
The following upgrades have been deferred due to phasing:
  sosreport
The following packages will be upgraded:
  libaudit1
1 upgraded, 0 newly installed, 0 to remove and 3 not upgraded.
`

// runHeldBack führt lcm_apt_heldback gegen Attrappen von apt-get und
// apt-mark aus (openssl ist gesperrt).
func runHeldBack(t *testing.T) string {
	t.Helper()
	bin := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write("apt-get", "cat <<'X'\n"+fakeAptOutput+"X\n")
	write("apt-mark", "echo openssl\n")
	cmd := exec.Command("sh", "-c", aptHeldBackFunc+"lcm_apt_heldback")
	cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"))
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestHeldBackClassifiesAptOutput(t *testing.T) {
	got := strings.TrimSpace(runHeldBack(t))
	want := "openssl hold\nlibfoo kept\nsosreport phased"
	if got != want {
		t.Fatalf("einordnung:\n%s\nerwartet:\n%s", got, want)
	}
}

// TestApplyHeldBackOnlyMarksPendingUpdates: Der Grund hängt nur an Paketen,
// die tatsächlich ein Update haben - und nur an bekannten Gründen.
func TestApplyHeldBackOnlyMarksPendingUpdates(t *testing.T) {
	pkgs := []domain.Package{
		{Name: "sosreport", Version: "4.10", CandidateVersion: "4.11"},
		{Name: "openssl", Version: "3.0.15"},
		{Name: "libfoo", Version: "1", CandidateVersion: "2"},
	}
	applyHeldBack(pkgs, "sosreport phased\nopenssl hold\nlibfoo unsinn\n")
	for i, want := range []string{domain.HeldReasonPhased, "", ""} {
		if pkgs[i].HeldReason != want {
			t.Errorf("%s: grund %q, erwartet %q", pkgs[i].Name, pkgs[i].HeldReason, want)
		}
	}
}

// TestUpgradeAllTakesNewPackages: Kernel-Updates brauchen ein neues Paket -
// ohne --with-new-pkgs blieben sie für immer liegen.
func TestUpgradeAllTakesNewPackages(t *testing.T) {
	script := aptUpgradeAllScript()
	for _, want := range []string{"lcm_apt -y --with-new-pkgs upgrade", "lcm_apt_report", "LCM_APT_FORCE"} {
		if !strings.Contains(script, want) {
			t.Errorf("voll-upgrade ohne %q", want)
		}
	}
}
