package remote

// Die Gegenrichtung zu compat_agent_test.go: der AKTUELLE Agent gegen den
// Server früherer Fassungen. Der Test baut cmd/app des Tags aus einem git
// worktree, startet ihn mit --demo (HTTP, eigenes Datenverzeichnis, freie Ports),
// legt über die REST-Schnittstelle einen Agent-Server an und lässt den Agent
// dieses Quellstands mit dem Token verbinden. Geprüft wird, was ein neuer Agent
// bei einem älteren LCM nicht verlieren darf: anmelden, im Inventar ankommen,
// einen Auftrag ausführen und verbunden bleiben, auch wenn der alte Server
// weder Pings schickt noch die neuen Fähigkeiten kennt. Neue Funktionen bleiben
// dabei stumm, es gibt keine Fehler.
//
// Läuft nur, wenn LCM_COMPAT_SERVER_TAGS gesetzt ist (Tags oder andere
// git-Verweise, durch Komma getrennt); gebaut wird wie beim Agent, mit demselben
// Cache LCM_COMPAT_CACHE_DIR:
//
//	LCM_COMPAT_SERVER_TAGS=v2.3.0 go test -count=1 -run TestAktuellerAgentGegenAeltereServer ./internal/remote/
//
// Nicht abgedeckt: der Abbruch eines laufenden Auftrags. Dafür braucht es einen
// Auftrag, der auf dem Agent lange läuft; die Aufträge des Servers rufen
// feste Befehle (apt, dpkg), die der Test nicht ersetzen kann.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"LCM/internal/agent"
	"LCM/internal/remote/wire"
)

const (
	compatServersEnv = "LCM_COMPAT_SERVER_TAGS"
	compatAdminPass  = "compat-admin-passwort"
)

func TestAktuellerAgentGegenAeltereServer(t *testing.T) {
	tags := os.Getenv(compatServersEnv)
	if tags == "" {
		t.Skip(compatServersEnv + " nicht gesetzt (baut den Server der Tags aus den Quellen)")
	}
	for _, tag := range strings.Split(tags, ",") {
		tag = strings.TrimSpace(tag)
		t.Run(tag, func(t *testing.T) { checkCurrentAgentAgainstServer(t, tag) })
	}
}

func checkCurrentAgentAgainstServer(t *testing.T, tag string) {
	api := startTaggedServer(t, tag)
	id, token := api.createAgentServer(t)
	tok, err := wire.ParseToken(token)
	if err != nil {
		t.Fatalf("token des alten servers nicht lesbar: %v", err)
	}

	// Der Agent dieses Quellstands, mit kurzen Zeiten: Er soll es merken, wenn
	// der alte Server keine Pings schickt, und trotzdem nicht aufgeben.
	version := currentVersion(t)
	cfg := &agent.Config{URL: "http://" + api.agentAddr, AgentID: tok.AgentID, Secret: tok.Secret}
	client := agent.NewClient(cfg, quiet, version).WithJobsDir(jobsDir(t)).WithTiming(agent.Timing{
		KeepAlive: fastKeepAlive, PingTimeout: fastPingTimeout,
		BackoffMin: fastBackoffMin, BackoffMax: fastBackoffMax,
	})
	go func() { _ = client.Run() }()
	t.Cleanup(client.Stop)

	// Inventar: Der Server kennt den Agent samt Version.
	waitUntil(t, 30*time.Second, "agent online mit Version im Inventar", func() bool {
		s := api.server(t, id)
		return s["agent_connected"] == true && s["agent_version"] == version
	})
	t.Logf("%s: Agent %s angemeldet, Fähigkeiten %q", tag, version, api.server(t, id)["agent_capabilities"])

	// Auftrag: Der alte Server liest einen neu verbundenen Agent selbst ein
	// (refresh-all), danach starten wir einen eigenen. Beide laufen über den
	// neuen Agent bis zum Ende; auf dem Testrechner dürfen sie scheitern (apt
	// fehlt), es zählt, dass die Antworten ankommen und die Sperre fällt.
	started := time.Now()
	api.waitIdle(t, id)
	api.startJob(t, id, "refresh-hardware")
	api.waitIdle(t, id)
	t.Logf("%s: beide Aufträge nach %s fertig", tag, time.Since(started).Round(time.Second))

	// Stille: Der alte Server kennt weder Ping noch jobs-v2. Der Agent bleibt
	// über mehrere Ping-Fristen verbunden und baut nicht ständig neu auf.
	time.Sleep(3 * (fastKeepAlive + fastPingTimeout))
	if s := api.server(t, id); s["agent_connected"] != true {
		t.Fatalf("agent nach der Stille nicht mehr verbunden: %v", s["agent_connected"])
	}
}

// currentVersion ist die Version dieses Quellstands (Datei VERSION).
func currentVersion(t *testing.T) string {
	t.Helper()
	repo := gitOutput(t, ".", "rev-parse", "--show-toplevel")
	raw, err := os.ReadFile(filepath.Join(repo, "VERSION"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(raw))
}

// taggedServer ist ein laufender LCM-Server früherer Fassung.
type taggedServer struct {
	base      string // http://127.0.0.1:port
	agentAddr string // 127.0.0.1:agentport
	session   string
}

// startTaggedServer baut und startet den Server des Tags und meldet sich an.
func startTaggedServer(t *testing.T, tag string) *taggedServer {
	t.Helper()
	binary := buildTaggedServer(t, tag)
	dir := t.TempDir()
	port, agentPort := freePort(t), freePort(t)
	config := fmt.Sprintf(`{"host":"127.0.0.1","port":%d,"agent_port":%d,"agent_host":"127.0.0.1","admin_initial_password":%q}`,
		port, agentPort, compatAdminPass)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	cmd := exec.Command(binary, "-config", filepath.Join(dir, "config.json"), "-data", dir, "--dev", "--demo")
	cmd.Dir = dir
	// Ohne lokale Werkzeuge im Pfad (Trivy), wie in der E2E-Umgebung.
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + dir}
	cmd.Stdout, cmd.Stderr = &logs, &logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if t.Failed() {
			t.Logf("log des servers %s:\n%s", tag, logs.String())
		}
	})

	s := &taggedServer{base: fmt.Sprintf("http://127.0.0.1:%d", port), agentAddr: fmt.Sprintf("127.0.0.1:%d", agentPort)}
	waitUntil(t, 60*time.Second, "server "+tag+" antwortet", func() bool {
		resp, err := http.Get(s.base + "/api/v1/health")
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	})
	var login struct {
		SessionToken string `json:"session_token"`
	}
	s.call(t, http.MethodPost, "/api/v1/auth/login", map[string]string{"username": "admin", "password": compatAdminPass}, &login)
	s.session = login.SessionToken
	if s.session == "" {
		t.Fatal("anmeldung ohne session_token")
	}
	return s
}

// buildTaggedServer baut cmd/app des Tags (mit Cache wie buildTaggedAgent). Die
// Oberfläche wird per go:embed eingebunden; ein leeres dist/ genügt dem Build.
func buildTaggedServer(t *testing.T, tag string) string {
	t.Helper()
	tmp := t.TempDir()
	worktree := addTagWorktree(t, tmp, tag)
	name := "app-" + strings.NewReplacer("/", "_").Replace(tag) + "-" + goPlatform()
	cacheDir := os.Getenv(compatCacheEnv)
	if cacheDir != "" {
		if _, err := os.Stat(filepath.Join(cacheDir, name)); err == nil {
			t.Logf("server %s aus dem Cache", tag)
			return filepath.Join(cacheDir, name)
		}
	}
	dist := filepath.Join(worktree, "ui", "dist")
	if err := os.MkdirAll(dist, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dist, "index.html"), []byte("<!doctype html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(tmp, "app")
	build := exec.Command("go", "build", "-o", binary, "./cmd/app")
	build.Dir = worktree
	build.Env = append(os.Environ(), "CGO_ENABLED=0", "GOWORK=off")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("server %s bauen: %v\n%s", tag, err, out)
	}
	if cacheDir == "" {
		return binary
	}
	return storeInCache(t, binary, cacheDir, name)
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port
}

// call ruft die REST-Schnittstelle auf und liest die Antwort nach out (nil: egal).
func (s *taggedServer) call(t *testing.T, method, path string, body, out any) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, s.base+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if s.session != "" {
		req.Header.Set("X-LCM-Session", s.session)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		t.Fatalf("%s %s: %s: %s", method, path, resp.Status, raw)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("%s %s: antwort nicht lesbar: %v\n%s", method, path, err, raw)
		}
	}
}

// createAgentServer legt einen Agent-Server an und liefert ID und Einrichtungs-Token.
func (s *taggedServer) createAgentServer(t *testing.T) (uint, string) {
	t.Helper()
	var res struct {
		Server struct {
			ID uint `json:"id"`
		} `json:"server"`
		Token string `json:"token"`
	}
	s.call(t, http.MethodPost, "/api/v1/servers/agent", map[string]string{"name": "compat"}, &res)
	return res.Server.ID, res.Token
}

func (s *taggedServer) server(t *testing.T, id uint) map[string]any {
	t.Helper()
	var out map[string]any
	s.call(t, http.MethodGet, fmt.Sprintf("/api/v1/servers/%d", id), nil, &out)
	return out
}

// startJob stößt einen Auftrag des Servers an (POST /servers/:id/<action>).
func (s *taggedServer) startJob(t *testing.T, id uint, action string) {
	t.Helper()
	s.call(t, http.MethodPost, fmt.Sprintf("/api/v1/servers/%d/%s", id, action), nil, nil)
}

// waitIdle wartet, bis auf dem Server kein Auftrag mehr läuft.
func (s *taggedServer) waitIdle(t *testing.T, id uint) {
	t.Helper()
	waitUntil(t, 60*time.Second, "kein Auftrag mehr aktiv", func() bool {
		var out struct {
			Job any `json:"job"`
		}
		s.call(t, http.MethodGet, fmt.Sprintf("/api/v1/servers/%d/active-job", id), nil, &out)
		return out.Job == nil
	})
}
