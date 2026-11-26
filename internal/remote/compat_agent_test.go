package remote

// Abwärtskompatibilität mit den ECHTEN Agenten früherer Fassungen: Der Test baut
// den Agent eines Release-Tags aus dessen Quellen, lässt ihn sich am aktuellen Hub
// anmelden und prüft Befehl, Eingabe, Abbruch, Ergebnis nach einem
// Verbindungsabbruch, Konsole und das, was LCM über ihn speichert.
//
// Er braucht git, Go und beim ersten Mal Netz für die Abhängigkeiten des Tags,
// deshalb läuft er nur, wenn LCM_COMPAT_AGENT_TAGS gesetzt ist (Tags oder andere
// git-Verweise, durch Komma getrennt):
//
//	LCM_COMPAT_AGENT_TAGS=v2.0.0,v2.1.0,v2.2.0,v2.3.0 go test -count=1 -run TestEchteAeltereAgenten ./internal/remote/
//
// Gebaut wird in einem eigenen git worktree unter dem Temp-Verzeichnis (ein
// kleines Hauptprogramm um agent.NewClient, denn `lcm-agent run` liest seine
// Konfiguration aus /etc); der Worktree verschwindet nach dem Test wieder. Ist
// LCM_COMPAT_CACHE_DIR gesetzt, bleiben die gebauten Binaries dort liegen (je Tag
// eine Datei) und der Bau entfällt beim nächsten Lauf; die CI hält das Verzeichnis
// im Cache (Paket E-3). Die Liste der Tags wächst mit jedem Release.
//
// Die Stromunterbrechung nutzt den faultProxy der Chaos-Tests, deshalb liegt der
// Test im Paket remote.

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"LCM/internal/core/agentcaps"
	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/remote/wire"
)

const (
	compatTagsEnv  = "LCM_COMPAT_AGENT_TAGS"
	compatCacheEnv = "LCM_COMPAT_CACHE_DIR"
)

// compatMain ist das Hauptprogramm, das im Worktree des Tags gebaut wird. Es
// benutzt nur, was jede 2.x-Fassung hat: agent.Config und agent.NewClient. Die
// Job-Ablage (agent.JobsDir) gibt es erst in neueren Fassungen; compatJobsDir
// setzt sie dort, damit der Agent nicht nach /var/lib schreibt.
const compatMain = `package main

import (
	"log/slog"
	"os"

	"LCM/internal/agent"
)

func main() {
//JOBSDIR
	cfg := &agent.Config{URL: os.Getenv("LCM_URL"), AgentID: os.Getenv("LCM_AGENT_ID"), Secret: os.Getenv("LCM_SECRET")}
	_ = agent.NewClient(cfg, slog.Default(), os.Getenv("LCM_AGENT_VERSION")).Run()
}
`

const compatJobsDir = "\tagent.JobsDir = os.Getenv(\"LCM_JOBS_DIR\")"

func TestEchteAeltereAgenten(t *testing.T) {
	tags := os.Getenv(compatTagsEnv)
	if tags == "" {
		t.Skip(compatTagsEnv + " nicht gesetzt (baut die Agenten der Tags aus den Quellen)")
	}
	for _, tag := range strings.Split(tags, ",") {
		tag = strings.TrimSpace(tag)
		t.Run(tag, func(t *testing.T) { checkTaggedAgent(t, tag) })
	}
}

// compatAgent ist ein gebauter Agent als eigener Prozess hinter dem faultProxy.
type compatAgent struct {
	*chaosAgent
	version string
}

func checkTaggedAgent(t *testing.T, tag string) {
	binary := buildTaggedAgent(t, tag)
	ca := startTaggedAgent(t, binary, strings.TrimPrefix(tag, "v"))
	ca.requireReady(30 * time.Second)

	stored := ca.checkInventory(t)
	caps := stored.Agent()
	durable := caps.Has(wire.CapabilityJobs)
	t.Logf("%s: Protokoll %d, Fähigkeiten %q", tag, stored.AgentProtocol, stored.AgentCapabilities)

	ca.checkCommands(t)
	ca.checkAbort(t)
	ca.checkResultAfterReconnect(t, durable)
	if caps.Has(wire.CapabilityTTY) {
		ca.checkConsole(t)
	}
}

// startTaggedAgent legt Serverzeile und Proxy an und startet das Binary mit der
// Adresse des Proxys. Der Prozess endet mit dem Test.
func startTaggedAgent(t *testing.T, binary, version string) *compatAgent {
	t.Helper()
	e := newChaosEnv(t)
	a := e.newAgent()
	cmd := exec.Command(binary)
	cmd.Env = append(os.Environ(), "LCM_URL="+a.cfg.URL, "LCM_AGENT_ID="+a.cfg.AgentID, "LCM_SECRET="+a.cfg.Secret,
		"LCM_AGENT_VERSION="+version, "LCM_JOBS_DIR="+a.jobs)
	if testing.Verbose() {
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return &compatAgent{chaosAgent: a, version: version}
}

// checkInventory: Das Inventar ist angekommen - Version, Protokoll und die
// Fähigkeiten des Tags, und der Agent liegt über der Mindestversion.
func (a *compatAgent) checkInventory(t *testing.T) *domain.Server {
	t.Helper()
	waitUntil(t, 10*time.Second, "inventar verbucht", func() bool {
		s, err := a.env.servers.FindByAgentID(a.server.AgentID)
		return err == nil && s.AgentVersion == a.version
	})
	stored, err := a.env.servers.FindByAgentID(a.server.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.AgentProtocol < 1 {
		t.Errorf("Protokoll = %d, ein Agent ohne das Feld spricht Protokoll 1", stored.AgentProtocol)
	}
	caps := stored.Agent()
	if caps.BelowMinimum() {
		t.Errorf("%s liegt unter der Mindestversion %s", a.version, agentcaps.MinAgentVersion)
	}
	if !caps.Has(wire.CapabilityPing) && !caps.Has(wire.CapabilityJobs) && stored.AgentProtocol != 1 {
		t.Errorf("Agent ohne neue Fähigkeiten meldet Protokoll %d, erwartet 1", stored.AgentProtocol)
	}
	return stored
}

// checkCommands: Grundbefehl und Eingabe samt Exit-Code.
func (a *compatAgent) checkCommands(t *testing.T) {
	t.Helper()
	conn, err := a.env.currentHub().Conn(a.server)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	out, code, err := conn.Run("echo kompatibel")
	if err != nil || code != 0 || out != "kompatibel\n" {
		t.Fatalf("echo: (%q, %d, %v)", out, code, err)
	}
	out, code, err = conn.RunStdin("cat; exit 5", "durchgereicht")
	if err != nil || code != 5 || out != "durchgereicht" {
		t.Fatalf("stdin: (%q, %d, %v)", out, code, err)
	}
}

// checkAbort: LCM bricht ab, der Agent hält das Kommando samt Kindern an.
func (a *compatAgent) checkAbort(t *testing.T) {
	t.Helper()
	conn, err := a.env.currentHub().Conn(a.server)
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "durchgelaufen")
	done := make(chan error, 1)
	go func() {
		_, _, err := conn.Run("sleep 2; touch " + marker)
		done <- err
	}()
	time.Sleep(500 * time.Millisecond)
	_ = conn.Close()
	if err := <-done; err == nil {
		t.Fatal("abgebrochenes Kommando meldet Erfolg")
	}
	time.Sleep(2500 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("das Kommando lief nach dem Abbruch im Agent weiter")
	}
	a.requireReady(10 * time.Second)
}

// checkResultAfterReconnect: Die Leitung reißt mitten im Kommando und kommt
// zurück. Ein Agent mit dauerhaften Jobs (jobs-v2) stellt das Ergebnis danach zu;
// ein älterer lässt den Aufruf sauber scheitern (Fehler statt Hänger oder
// falschem Erfolg) und ist danach wieder für Kommandos bereit.
func (a *compatAgent) checkResultAfterReconnect(t *testing.T, durable bool) {
	t.Helper()
	done := runInBackground(a.chaosAgent, "sleep 4; echo fertig", 70*time.Second)
	time.Sleep(time.Second)
	a.proxy.Outage()
	time.Sleep(time.Second)
	a.proxy.Heal()

	if durable {
		requireResult(t, done, "fertig\n")
		return
	}
	select {
	case r := <-done:
		if r.err == nil {
			t.Fatalf("ohne dauerhafte Jobs erwartet LCM einen Fehler nach dem Abriss, bekam (%q, %d)", r.out, r.code)
		}
	case <-time.After(75 * time.Second):
		t.Fatal("aufruf hängt nach dem Verbindungsabriss")
	}
	a.requireReady(90 * time.Second)
}

// checkConsole: Web-Konsole über den Agent - Fenstergröße und Rechnung kommen
// zurück. Nur unter Linux als root (PTY, Arbeitsverzeichnis /root wie im Dienst).
func (a *compatAgent) checkConsole(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		t.Log("Konsole übersprungen: braucht linux und root")
		return
	}
	conn, err := a.env.currentHub().Conn(a.server)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	term, err := conn.(sshx.TerminalConn).Terminal("xterm", 90, 20)
	if err != nil {
		t.Fatalf("terminal: %v", err)
	}
	if _, err := term.Write([]byte("stty size; echo tty-$((40+2))\n")); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	buf := make([]byte, 4096)
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(out.String(), "tty-42") && time.Now().Before(deadline) {
		n, err := term.Read(buf)
		out.Write(buf[:n])
		if err != nil {
			break
		}
	}
	if !strings.Contains(out.String(), "20 90") || !strings.Contains(out.String(), "tty-42") {
		t.Fatalf("konsole: ausgabe ohne fenstergröße oder rechnung: %q", out.String())
	}
	if err := term.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := term.Read(buf); err != io.EOF {
		t.Fatalf("nach close erwartet EOF, bekam %v", err)
	}
}

// buildTaggedAgent baut den Agent des Tags und liefert den Pfad des Binaries.
// Mit LCM_COMPAT_CACHE_DIR wird das Binary dort abgelegt und beim nächsten Mal
// wiederverwendet. Der Dateiname trägt Tag, Plattform und die Prüfsumme des
// Hauptprogramms.
func buildTaggedAgent(t *testing.T, tag string) string {
	t.Helper()
	tmp := t.TempDir()
	worktree := addTagWorktree(t, tmp, tag)

	source := compatSource(worktree)
	sum := sha256.Sum256([]byte(source))
	name := strings.NewReplacer("/", "_").Replace(tag) + "-" + goPlatform() + "-" + hex.EncodeToString(sum[:4])
	cacheDir := os.Getenv(compatCacheEnv)
	if cacheDir != "" {
		if _, err := os.Stat(filepath.Join(cacheDir, name)); err == nil {
			t.Logf("agent %s aus dem Cache", tag)
			return filepath.Join(cacheDir, name)
		}
	}

	mainDir := filepath.Join(worktree, "cmd", "compat-agent")
	if err := os.MkdirAll(mainDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mainDir, "main.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(tmp, "compat-agent")
	build := exec.Command("go", "build", "-o", binary, "./cmd/compat-agent")
	build.Dir = worktree
	build.Env = append(os.Environ(), "CGO_ENABLED=0", "GOWORK=off")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("agent %s bauen: %v\n%s", tag, err, out)
	}
	if cacheDir == "" {
		return binary
	}
	return storeInCache(t, binary, cacheDir, name)
}

// addTagWorktree legt für den Tag einen git worktree unter dir an; er
// verschwindet mit dem Test.
func addTagWorktree(t *testing.T, dir, tag string) string {
	t.Helper()
	repo := gitOutput(t, ".", "rev-parse", "--show-toplevel")
	worktree := filepath.Join(dir, "src")
	gitOutput(t, repo, "worktree", "add", "--detach", worktree, tag)
	t.Cleanup(func() {
		gitOutput(t, repo, "worktree", "remove", "--force", worktree)
	})
	return worktree
}

func goPlatform() string { return runtime.GOOS + "-" + runtime.GOARCH }

// compatSource ist das Hauptprogramm für den Worktree: Mit Job-Ablage, wenn
// diese Fassung eine hat.
func compatSource(worktree string) string {
	jobs, _ := os.ReadFile(filepath.Join(worktree, "internal", "agent", "jobs.go"))
	line := ""
	if strings.Contains(string(jobs), "var JobsDir") {
		line = compatJobsDir
	}
	return strings.Replace(compatMain, "//JOBSDIR", line, 1)
}

// storeInCache legt das Binary im Cache ab (erst unter anderem Namen, dann
// umbenannt, damit ein abgebrochener Lauf keine halbe Datei hinterlässt).
func storeInCache(t *testing.T, binary, dir, name string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, name)
	partial := target + ".tmp"
	if err := os.WriteFile(partial, data, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(partial, target); err != nil {
		t.Fatal(err)
	}
	return target
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}
