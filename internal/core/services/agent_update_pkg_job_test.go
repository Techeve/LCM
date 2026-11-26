package services_test

import (
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// pkgHostScript spielt einen Server mit dnf, zypper, apk oder pacman: Das
// Versionsskript antwortet mit list, die Installation (Kommando enthält
// installMark) mit installedAfter; reaction läuft dabei wie bei apt.
func pkgHostScript(host *agentHost, versions, installMark, installedAfter string, installCode int) {
	host.script = func(cmd string) (string, int, bool) {
		switch {
		case strings.Contains(cmd, "LCM_AGENT_VERSIONS"):
			return versions, 0, true
		case strings.Contains(cmd, installMark):
			if host.reaction != nil {
				host.reaction(repositories.NewServerRepository(host.env.DB()), host.serverID)
			}
			return "installiert\nLCM_AGENT_INSTALLED=" + installedAfter + "\n", installCode, true
		}
		return "", 0, false
	}
}

const dnfVersions = "LCM_AGENT_INSTALLED=2.2.0-1\nLCM_AGENT_VERSIONS\nlcm-agent 2.3.1-1 techeve-lcm\n\nlcm-agent 2.3.0-1 techeve-lcm\n\nlcm-agent 2.2.0-1 techeve-lcm\n"

// TestAgentUpdateJobDnf: Auf einem dnf-Server wählt LCM aus der rohen Ausgabe die
// höchste Version der Reihe und installiert genau sie mit der Ausnahme vom
// exclude.
func TestAgentUpdateJobDnf(t *testing.T) {
	env, host, server := agentUpdateEnv(t, "2.2.0", "2.3.0")
	setPackageManager(t, env, server.ID, "dnf")
	host.reaction = agentReports("2.3.1", "")
	var install string
	pkgHostScript(host, dnfVersions, "install lcm-agent-", "2.3.1-1", 0)
	inner := host.script
	host.script = func(cmd string) (string, int, bool) {
		if strings.Contains(cmd, "install lcm-agent-") {
			install = cmd
		}
		return inner(cmd)
	}

	job, err := env.Servers.StartAgentUpdate(repositories.ScopeAll(), server.ID, "tester")
	if err != nil {
		t.Fatal(err)
	}
	done := waitForJob(t, env, job.ID)
	if done.Status != domain.JobStatusSuccess {
		t.Fatalf("status %s: %s", done.Status, done.Output)
	}
	if !strings.Contains(install, "dnf -y --setopt=disable_excludes=techeve-lcm install lcm-agent-2.3.1") {
		t.Errorf("installationskommando: %q", install)
	}
	for _, want := range []string{"Installiert: 2.2.0", "Verfügbar: 2.3.1 (techeve-lcm)", "Zielversion: 2.3.1"} {
		if !strings.Contains(done.Output, want) {
			t.Errorf("protokoll enthält %q nicht:\n%s", want, done.Output)
		}
	}
}

// TestAgentUpdateJobInstallationOhneWechselScheitertSofort: Meldet die
// Paketverwaltung nach der Installation die alte Version (zypper ging über ein
// Schloss hinweg), scheitert der Job gleich und wartet nicht 45 Minuten.
func TestAgentUpdateJobInstallationOhneWechselScheitertSofort(t *testing.T) {
	env, host, server := agentUpdateEnv(t, "2.2.0", "2.3.0")
	setPackageManager(t, env, server.ID, "zypper")
	versions := "LCM_AGENT_INSTALLED=2.2.0-1\nLCM_AGENT_VERSIONS\nS | Name | Type | Version | Arch | Repository\n--+-----+----+----+----+---\n  | lcm-agent | package | 2.3.1-1 | x86_64 | techeve-lcm\n"
	pkgHostScript(host, versions, "zypper -n install", "2.2.0-1", 0)

	job, err := env.Servers.StartAgentUpdate(repositories.ScopeAll(), server.ID, "tester")
	if err != nil {
		t.Fatal(err)
	}
	done := waitForJob(t, env, job.ID)
	if done.Status != domain.JobStatusFailed || !strings.Contains(done.Output, "statt 2.3.1") {
		t.Fatalf("status %s:\n%s", done.Status, done.Output)
	}
}

// TestAgentUpdateJobPacmanNachziehenHoltDieAeltereDatei: Nach einem Rollback
// kennt die Datenbank des Arch-Repos die laufende Version nicht mehr - das Paket
// wird mit pacman -U aus dem Repo-Verzeichnis angeglichen (wie jede Installation).
func TestAgentUpdateJobPacmanNachziehenHoltDieAeltereDatei(t *testing.T) {
	env, host, server := agentUpdateEnv(t, "2.2.0", "2.3.0")
	setPackageManager(t, env, server.ID, "pacman")
	host.reaction = agentReports("2.2.0", "2.3.1")
	versions := "LCM_AGENT_INSTALLED=2.2.0-1\nLCM_AGENT_VERSIONS\nRepository      : techeve\nName            : lcm-agent\nVersion         : 2.3.1-1\n"
	var installs []string
	host.script = func(cmd string) (string, int, bool) {
		switch {
		case strings.Contains(cmd, "LCM_AGENT_VERSIONS"):
			return versions, 0, true
		case strings.Contains(cmd, "pacman -U --noconfirm"):
			installs = append(installs, cmd)
			host.reaction(repositories.NewServerRepository(host.env.DB()), host.serverID)
			return "LCM_AGENT_INSTALLED=2.3.1-1\n", 0, true
		}
		return "", 0, false
	}

	job, err := env.Servers.StartAgentUpdate(repositories.ScopeAll(), server.ID, "tester")
	if err != nil {
		t.Fatal(err)
	}
	done := waitForJob(t, env, job.ID)
	if done.Status != domain.JobStatusFailed || !strings.Contains(done.Output, "zurückgerollt") {
		t.Fatalf("status %s:\n%s", done.Status, done.Output)
	}
	if len(installs) != 2 || !strings.Contains(installs[1], `lcm-agent-2.2.0-1-$(uname -m).pkg.tar.zst`) {
		t.Errorf("zwei installationen erwartet, die zweite mit pacman -U auf 2.2.0: %q", installs)
	}
	if !strings.Contains(done.Output, "Gleiche das Paket an das laufende Programm an: lcm-agent 2.2.0") {
		t.Errorf("protokoll:\n%s", done.Output)
	}
}

// TestAgentUpdateJobPacmanNachziehenLehntFremdeVersionAb: Die laufende Version
// meldet der Agent selbst. Ist sie keine Paketversion, läuft kein Skript damit:
// das Angleichen wird mit einem Hinweis übersprungen.
func TestAgentUpdateJobPacmanNachziehenLehntFremdeVersionAb(t *testing.T) {
	for _, running := range []string{"2.2.0;id", "$(id)"} {
		env, host, server := agentUpdateEnv(t, "2.2.0", "2.3.0")
		setPackageManager(t, env, server.ID, "pacman")
		host.reaction = agentReports(running, "2.3.1")
		versions := "LCM_AGENT_INSTALLED=2.2.0-1\nLCM_AGENT_VERSIONS\nRepository      : techeve\nName            : lcm-agent\nVersion         : 2.3.1-1\n"
		var installs []string
		host.script = func(cmd string) (string, int, bool) {
			switch {
			case strings.Contains(cmd, "LCM_AGENT_VERSIONS"):
				return versions, 0, true
			case strings.Contains(cmd, "pacman -U --noconfirm"):
				installs = append(installs, cmd)
				host.reaction(repositories.NewServerRepository(host.env.DB()), host.serverID)
				return "LCM_AGENT_INSTALLED=2.3.1-1\n", 0, true
			}
			return "", 0, false
		}

		job, err := env.Servers.StartAgentUpdate(repositories.ScopeAll(), server.ID, "tester")
		if err != nil {
			t.Fatal(err)
		}
		done := waitForJob(t, env, job.ID)
		if done.Status != domain.JobStatusFailed {
			t.Fatalf("%q: status %s:\n%s", running, done.Status, done.Output)
		}
		if len(installs) != 1 || strings.Contains(installs[0], running) {
			t.Errorf("%q: nur die Installation der Zielversion erwartet: %q", running, installs)
		}
		if !strings.Contains(done.Output, "Nicht möglich: ungültige Agent-Version") {
			t.Errorf("%q: protokoll:\n%s", running, done.Output)
		}
	}
}

// TestAgentUpdatePlanOhnePaketverwaltung: Fehlt die Verwaltung der erkannten
// Familie, ist die Antwort „nicht unterstützt“ und kein Fehler.
func TestAgentUpdatePlanOhnePaketverwaltung(t *testing.T) {
	env, host, server := agentUpdateEnv(t, "2.2.0", "2.3.0")
	setPackageManager(t, env, server.ID, "apk")
	host.script = func(cmd string) (string, int, bool) { return "LCM_AGENT_NO_MANAGER\n", 0, true }
	plan, err := env.Servers.AgentUpdatePlan(repositories.ScopeAll(), server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Action != "unsupported" || plan.Reason != "no_manager" {
		t.Errorf("plan = %+v", plan)
	}
}
