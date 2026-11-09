package services

import (
	"strings"
	"testing"
)

func TestUpgradeList(t *testing.T) {
	tests := []struct {
		name, raw, wantOut, wantProblem string
	}{
		{"Updates vorhanden", "curl.x86_64 8.0 baseos\nLCM-EXIT:0\n", "curl.x86_64 8.0 baseos\n", ""},
		{"Keine Updates", "LCM-EXIT:0\n", "", ""},
		{"Kaputte Paketquelle", "Error: Failed to download metadata for repo 'x'\nLCM-EXIT:1\n", "", "Error: Failed to download metadata for repo 'x'"},
		{"Altes dnf ohne Updates", "Error: No matching Packages to list\nLCM-EXIT:1\n", "", ""},
		{"Verbindung weg", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cmd string
			run := func(_, c string) string { cmd = c; return tt.raw }
			out, problem := upgradeList(run, "dnf -q list --upgrades")
			if out != tt.wantOut || problem != tt.wantProblem {
				t.Errorf("bekam (%q, %q), erwartet (%q, %q)", out, problem, tt.wantOut, tt.wantProblem)
			}
			if !strings.Contains(cmd, "2>&1") {
				t.Errorf("Fehlerausgabe wird verworfen: %s", cmd)
			}
		})
	}
}

func TestScanPackagesReportsBrokenUpdateCheck(t *testing.T) {
	run := func(label, _ string) string {
		switch label {
		case "packages":
			return "curl 8.0-1\n"
		case "upgradable":
			return "Error: Failed to download metadata for repo 'x'\nLCM-EXIT:1\n"
		}
		return ""
	}
	pkgs, _, problem := scanPackagesAndRepos(pkgDnf, run)
	if len(pkgs) != 1 || pkgs[0].CandidateVersion != "" {
		t.Errorf("Paketbestand falsch: %+v", pkgs)
	}
	if !strings.Contains(problem, "Failed to download metadata") {
		t.Errorf("Fehler der Update-Prüfung fehlt: %q", problem)
	}
}

func TestScanPackagesReportsSkippedZypperRepo(t *testing.T) {
	list := "Problem loading data from 'kaputt'\nS | Repo | Name | Current | Available | Arch\n" +
		"v | main | curl | 8.0-1 | 8.1-1 | x86_64\nLCM-EXIT:0\n"
	run := func(label, _ string) string {
		switch label {
		case "packages":
			return "curl 8.0-1\n"
		case "upgradable":
			return list
		}
		return ""
	}
	pkgs, _, problem := scanPackagesAndRepos(pkgZypper, run)
	if problem != "Problem loading data from 'kaputt'" {
		t.Errorf("Übersprungene Quelle fehlt: %q", problem)
	}
	if len(pkgs) != 1 || pkgs[0].CandidateVersion == "" {
		t.Errorf("Updates der übrigen Quellen müssen bleiben: %+v", pkgs)
	}
}

func TestScanIPsCmdSkipsContainerBridges(t *testing.T) {
	for _, want := range []string{"docker", "br-", "veth", "hostname -I"} {
		if !strings.Contains(scanIPsCmd, want) {
			t.Errorf("%q fehlt in %s", want, scanIPsCmd)
		}
	}
	if strings.Contains(scanIPsCmd, "172.") {
		t.Error("Gefiltert wird nach Schnittstelle: 172.16.0.0/12 kann ein echtes Netz sein")
	}
}

func TestFail2banRepoStep(t *testing.T) {
	if got := fail2banRepoStep(pkgApt); got != ":" {
		t.Errorf("apt braucht keine Quelle: %q", got)
	}
	script := fail2banInstallScript(false, pkgDnf, "192.0.2.1", nil)
	repo := strings.Index(script, "epel-release")
	install := strings.Index(script, "LCM: $p installiert")
	if repo < 0 || install < 0 || repo > install {
		t.Errorf("EPEL muss vor der Installation stehen: %s", script)
	}
	if !strings.Contains(script, "dl.fedoraproject.org") {
		t.Error("RHEL kennt epel-release nicht und braucht die direkte Adresse")
	}
}
