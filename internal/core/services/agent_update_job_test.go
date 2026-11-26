package services_test

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/storage/repositories"
	"LCM/internal/version"
)

// agentHost spielt einen Agent-Server: Er antwortet auf die Skripte des
// Update-Jobs und meldet sich nach der Installation - wie der Hub beim
// Inventar - mit neuer Version, mit Rollback oder gar nicht.
type agentHost struct {
	mu        sync.Mutex
	env       *testEnv
	serverID  uint
	installed string
	madison   string
	notAPT    bool
	// reaction läuft, wenn die Installation beendet ist: Er schreibt, was der Hub
	// beim nächsten Inventar des Agents schreiben würde.
	reaction func(repo *repositories.ServerRepository, id uint)
	commands []string
	// script antwortet vor allem anderen (Paketverwaltungen außer apt); ok sagt, ob es zuständig war.
	script func(cmd string) (out string, code int, ok bool)
}

func (h *agentHost) Conn(*domain.Server) (sshx.Conn, error) { return &agentHostConn{h: h}, nil }
func (h *agentHost) Online(string) bool                     { return true }
func (h *agentHost) Disconnect(string)                      {}
func (h *agentHost) LinkStats(string) domain.AgentLink      { return domain.AgentLink{} }

type agentHostConn struct{ h *agentHost }

func (c *agentHostConn) Run(cmd string) (string, int, error) { return c.RunStdin(cmd, "") }
func (c *agentHostConn) RunStdin(cmd, _ string) (string, int, error) {
	h := c.h
	h.mu.Lock()
	h.commands = append(h.commands, cmd)
	h.mu.Unlock()
	if h.script != nil {
		if out, code, ok := h.script(cmd); ok {
			return out, code, nil
		}
	}
	switch {
	case strings.Contains(cmd, "apt-cache madison"):
		if h.notAPT {
			return "LCM_AGENT_NOT_PACKAGED\n", 0, nil
		}
		return "LCM_AGENT_INSTALLED=" + h.installed + "\nLCM_AGENT_VERSIONS\n" + h.madison + "\n", 0, nil
	case strings.Contains(cmd, "lcm_apt install"):
		if h.reaction != nil {
			h.reaction(repositories.NewServerRepository(h.env.DB()), h.serverID)
		}
		return "Setting up lcm-agent\n", 0, nil
	case strings.Contains(cmd, "echo lcm-agent-ok"):
		return "lcm-agent-ok\n", 0, nil
	}
	return "", 0, errors.New("unerwartetes kommando: " + cmd)
}
func (c *agentHostConn) KeyExchange() string { return "" }
func (c *agentHostConn) OnActivity(func())   {}
func (c *agentHostConn) Close() error        { return nil }

const hostMadison = ` lcm-agent | 2.3.0 | https://repo.techeve.de community/main amd64 Packages
 lcm-agent | 2.2.0 | https://repo.techeve.de community/main amd64 Packages`

// agentUpdateEnv legt einen Agent-Server mit der Version installed an und setzt
// LCM auf lcmVersion.
func agentUpdateEnv(t *testing.T, installed, lcmVersion string) (*testEnv, *agentHost, *domain.Server) {
	t.Helper()
	env := newTestEnv(t)
	old, wait, poll := version.Version, services.AgentUpdateMaxWait, services.AgentUpdatePoll
	version.Version = lcmVersion
	services.AgentUpdateMaxWait, services.AgentUpdatePoll = 300*time.Millisecond, time.Millisecond
	t.Cleanup(func() {
		version.Version, services.AgentUpdateMaxWait, services.AgentUpdatePoll = old, wait, poll
	})
	server, _, err := env.Servers.CreateAgentServer("agent01", "tester", "lcm.example")
	if err != nil {
		t.Fatal(err)
	}
	repo := repositories.NewServerRepository(env.DB())
	if err := repo.UpdateFields(server.ID, map[string]any{"agent_version": installed, "reachable": true}); err != nil {
		t.Fatal(err)
	}
	host := &agentHost{env: env, serverID: server.ID, installed: installed, madison: hostMadison}
	env.Servers.WithAgentHub(host)
	return env, host, server
}

// agentReports schreibt, was handleInventory beim Connect des Agents schreibt.
func agentReports(ver, rolledBack string) func(*repositories.ServerRepository, uint) {
	return func(repo *repositories.ServerRepository, id uint) {
		_ = repo.UpdateFields(id, map[string]any{
			"agent_version": ver, "agent_rollback": rolledBack, "agent_rollback_reason": map[bool]string{true: "crash-loop"}[rolledBack != ""], "agent_reported_at": time.Now().Add(time.Millisecond),
		})
	}
}

func TestAgentUpdateJobInstalliertDieZielversionUndWartetAufDenAgent(t *testing.T) {
	env, host, server := agentUpdateEnv(t, "2.2.0", "2.3.0")
	host.reaction = agentReports("2.3.0", "")

	job, err := env.Servers.StartAgentUpdate(repositories.ScopeAll(), server.ID, "tester")
	if err != nil {
		t.Fatal(err)
	}
	if job.Type != domain.JobTypeAgentUpdate {
		t.Errorf("job-typ = %q", job.Type)
	}
	done := waitForJob(t, env, job.ID)
	if done.Status != domain.JobStatusSuccess {
		t.Fatalf("status %s: %s", done.Status, done.Output)
	}
	for _, want := range []string{"Zielversion: 2.3.0", "Installiert. Der Agent wechselt", "zurückgemeldet", "antwortet auf Kommandos"} {
		if !strings.Contains(done.Output, want) {
			t.Errorf("protokoll enthält %q nicht:\n%s", want, done.Output)
		}
	}
	host.mu.Lock()
	defer host.mu.Unlock()
	var install string
	for _, c := range host.commands {
		if strings.Contains(c, "lcm_apt install") {
			install = c
		}
	}
	if !strings.Contains(install, "lcm-agent=2.3.0") {
		t.Errorf("installationsbefehl: %q", install)
	}
}

// TestAgentUpdateJobEnterpriseLCMZiehtNieUeberSichSelbst: Ein Enterprise-LCM
// auf 2.2.0 nimmt die 2.2.0, obwohl Community 2.3.0 führt.
func TestAgentUpdateJobEnterpriseLCMZiehtNieUeberSichSelbst(t *testing.T) {
	env, host, server := agentUpdateEnv(t, "2.0.0", "2.2.0")
	host.reaction = agentReports("2.2.0", "")
	job, err := env.Servers.StartAgentUpdate(repositories.ScopeAll(), server.ID, "tester")
	if err != nil {
		t.Fatal(err)
	}
	done := waitForJob(t, env, job.ID)
	if done.Status != domain.JobStatusSuccess || !strings.Contains(done.Output, "Zielversion: 2.2.0") {
		t.Fatalf("status %s: %s", done.Status, done.Output)
	}
}

func TestAgentUpdateJobOhneAenderungWennAktuell(t *testing.T) {
	env, host, server := agentUpdateEnv(t, "2.3.0", "2.3.0")
	job, err := env.Servers.StartAgentUpdate(repositories.ScopeAll(), server.ID, "tester")
	if err != nil {
		t.Fatal(err)
	}
	done := waitForJob(t, env, job.ID)
	if done.Status != domain.JobStatusSuccess || !strings.Contains(done.Output, "Nichts zu tun") {
		t.Fatalf("status %s: %s", done.Status, done.Output)
	}
	host.mu.Lock()
	defer host.mu.Unlock()
	for _, c := range host.commands {
		if strings.Contains(c, "lcm_apt install") {
			t.Errorf("trotz aktuellem Agent installiert: %q", c)
		}
	}
}

// TestAgentUpdateJobZeigtRollbackAlsGescheitert: Meldet der Agent ein Rollback,
// steht der Job als gescheitert in der Historie und nennt die Versionen.
func TestAgentUpdateJobZeigtRollbackAlsGescheitert(t *testing.T) {
	env, host, server := agentUpdateEnv(t, "2.2.0", "2.3.0")
	host.reaction = agentReports("2.2.0", "2.3.0")
	job, err := env.Servers.StartAgentUpdate(repositories.ScopeAll(), server.ID, "tester")
	if err != nil {
		t.Fatal(err)
	}
	done := waitForJob(t, env, job.ID)
	if done.Status != domain.JobStatusFailed {
		t.Fatalf("status %s, erwartet failed:\n%s", done.Status, done.Output)
	}
	if !strings.Contains(done.Output, "Rollback") || !strings.Contains(done.Output, "2.3.0") || !strings.Contains(done.Output, "stürzte wiederholt ab") {
		t.Errorf("protokoll nennt den rollback nicht:\n%s", done.Output)
	}
}

// TestAgentUpdateJobOhneRueckmeldungScheitert: Der Agent meldet sich nicht.
func TestAgentUpdateJobOhneRueckmeldungScheitert(t *testing.T) {
	env, _, server := agentUpdateEnv(t, "2.2.0", "2.3.0")
	job, err := env.Servers.StartAgentUpdate(repositories.ScopeAll(), server.ID, "tester")
	if err != nil {
		t.Fatal(err)
	}
	done := waitForJob(t, env, job.ID)
	if done.Status != domain.JobStatusFailed || !strings.Contains(done.Output, "Keine Rückmeldung mit Version 2.3.0") {
		t.Fatalf("status %s:\n%s", done.Status, done.Output)
	}
}

// TestAgentUpdateJobAltesInventarGiltNicht: Ein Inventar von VOR dem Start des
// Jobs beweist nichts - der Agent steht dort schon auf der Zielversion nur
// scheinbar.
func TestAgentUpdateJobAltesInventarGiltNicht(t *testing.T) {
	env, host, server := agentUpdateEnv(t, "2.2.0", "2.3.0")
	repo := repositories.NewServerRepository(env.DB())
	_ = repo.UpdateFields(server.ID, map[string]any{"agent_version": "2.3.0", "agent_reported_at": time.Now().Add(-time.Hour)})
	host.installed = "2.2.0"
	job, err := env.Servers.StartAgentUpdate(repositories.ScopeAll(), server.ID, "tester")
	if err != nil {
		t.Fatal(err)
	}
	if done := waitForJob(t, env, job.ID); done.Status != domain.JobStatusFailed {
		t.Fatalf("status %s:\n%s", done.Status, done.Output)
	}
}

func TestAgentUpdateJobNichtPaketierterAgentScheitertMitKlarerMeldung(t *testing.T) {
	env, host, server := agentUpdateEnv(t, "2.2.0", "2.3.0")
	host.notAPT = true
	job, err := env.Servers.StartAgentUpdate(repositories.ScopeAll(), server.ID, "tester")
	if err != nil {
		t.Fatal(err)
	}
	done := waitForJob(t, env, job.ID)
	if done.Status != domain.JobStatusFailed || !strings.Contains(done.Output, "nicht als Paket installiert") {
		t.Fatalf("status %s:\n%s", done.Status, done.Output)
	}
}

func TestAgentUpdateAbgelehntFuerSSH(t *testing.T) {
	env, _, _ := agentUpdateEnv(t, "2.2.0", "2.3.0")
	ssh := joinTestServer(t, env, "web01")
	if _, err := env.Servers.StartAgentUpdate(repositories.ScopeAll(), ssh, "tester"); !errors.Is(err, services.ErrNotAgentServer) {
		t.Errorf("ssh-server: %v", err)
	}
}

// TestAgentUpdatePlanZeigtVonNachOhneZuAendern: Die Vorschau des Knopfes.
func TestAgentUpdatePlanZeigtVonNachOhneZuAendern(t *testing.T) {
	env, host, server := agentUpdateEnv(t, "2.2.0", "2.3.0")
	plan, err := env.Servers.AgentUpdatePlan(repositories.ScopeAll(), server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Action != "update" || plan.Installed != "2.2.0" || plan.Target != "2.3.0" || plan.ServerVersion != "2.3.0" || len(plan.Available) != 2 {
		t.Errorf("plan = %+v", plan)
	}
	host.mu.Lock()
	defer host.mu.Unlock()
	for _, c := range host.commands {
		if strings.Contains(c, "lcm_apt install") || strings.Contains(c, "\nlcm_apt_update\n") {
			t.Errorf("die vorschau änderte etwas: %q", c)
		}
	}
}
