package remote

// Fehlerinjektion für die Strecke Agent ↔ LCM (Paket A2 aus
// doc/plan-2026-11-23-agent-robustheit.md). Zwischen echtem Agent und echtem
// Broker sitzt ein faultProxy (faultproxy_test.go), der die Leitung kappt,
// verzögert oder halb offen hängen lässt.
//
// Die Tests beschreiben das SOLL-Verhalten aus dem Plan. Ein roter Test belegt
// einen der Befunde B1…B15 und ist vor dem Fix gewollt; er wird erst mit dem
// zugehörigen Paket grün. Jeder Test hat eine Obergrenze - ein Hänger zeigt
// sich als Fehler, nicht als stehender Testlauf.

import (
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"

	"github.com/gofiber/fiber/v3"

	"LCM/internal/agent"
	"LCM/internal/core/domain"
	"LCM/internal/netfilter"
	"LCM/internal/remote/wire"
	"LCM/internal/storage"
	"LCM/internal/storage/repositories"
)

// Kurze Zeiten für Tests, die auf Ping-Erkennung und Backoff warten.
const (
	fastKeepAlive   = 2 * time.Second
	fastPingTimeout = 2 * time.Second
	fastBackoffMin  = 500 * time.Millisecond
	fastBackoffMax  = 2 * time.Second
)

// quiet schluckt das Protokoll: Hunderte Abbrüche würden den Testlauf fluten,
// und Agenten, die nach dem Test noch eine Weile laufen, dürfen nicht mehr in
// t.Log schreiben.
var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// chaosEnv ist die LCM-Seite eines Fehlerinjektions-Tests: Datenbank, Hub mit
// Fiber-Listener (neu startbar) und beliebig viele Agenten dahinter, jeder über
// seinen eigenen faultProxy.
type chaosEnv struct {
	t       *testing.T
	servers *repositories.ServerRepository

	mu  sync.Mutex
	hub *Hub
	app *fiber.App

	// inventories zählt die Inventare, die LCM erhalten hat - der Agent
	// schickt sie erst, wenn er sich bereit meldet.
	inventories atomic.Int64
	target      string // Adresse des aktuellen Listeners
	agents      []*chaosAgent
	pingEvery   time.Duration // Takt der Pings von LCM, 0 = Vorgabe
}

// chaosAgent ist ein echter Agent samt Proxy und Serverzeile.
type chaosAgent struct {
	env    *chaosEnv
	server *domain.Server
	proxy  *faultProxy
	client *agent.Client
	cfg    *agent.Config
	jobs   string // Job-Ablage, bleibt über Neustarts des Agents
}

func newChaosEnv(t *testing.T) *chaosEnv { return newChaosEnvPinging(t, 0) }

// newChaosEnvPinging ist newChaosEnv mit kürzerem Takt der Pings von LCM.
func newChaosEnvPinging(t *testing.T, pingEvery time.Duration) *chaosEnv {
	t.Helper()
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.Migrate(db); err != nil {
		t.Fatal(err)
	}
	e := &chaosEnv{t: t, servers: repositories.NewServerRepository(db), pingEvery: pingEvery}
	e.startServer()
	t.Cleanup(e.stopServer)
	return e
}

// startServer startet Hub und Listener (neu). Die Agenten behalten ihre Proxys
// und werden dort auf den neuen Port umgelenkt.
func (e *chaosEnv) startServer() {
	e.t.Helper()
	hub := New(e.servers, quiet)
	if e.pingEvery > 0 {
		hub.WithPingInterval(e.pingEvery)
	}
	hub.WithOnAgentOnline(func(*domain.Server) { e.inventories.Add(1) })
	if err := hub.Start(); err != nil {
		e.t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		e.t.Fatal(err)
	}
	app := fiber.New()
	// Jede Verbindung kommt von einer anderen Adresse: Das Ratelimit des
	// Upgrades (10 je Minute und Adresse) würde sonst jeden Test mit vielen
	// Verbindungsabbrüchen aussperren.
	var next atomic.Int64
	app.Use(func(c fiber.Ctx) error {
		n := next.Add(1)
		c.Request().Header.Set(fiber.HeaderXForwardedFor, fmt.Sprintf("10.%d.%d.%d", n>>16&255, n>>8&255, n&255))
		return c.Next()
	})
	trusted, err := netfilter.Parse([]string{"127.0.0.1/32"})
	if err != nil {
		e.t.Fatal(err)
	}
	trust := netfilter.NewProxyTrustHolder(netfilter.ProxyTrust{Enabled: true, Proxies: trusted})
	app.Get(wire.WSPath, WSHandler(hub, trust))
	go func() { _ = app.Listener(ln, fiber.ListenConfig{DisableStartupMessage: true}) }()

	e.mu.Lock()
	e.hub, e.app = hub, app
	e.mu.Unlock()
	e.target = ln.Addr().String()
	for _, a := range e.agents {
		a.proxy.SetTarget(e.target)
	}
}

// stopServer fährt Listener und Hub herunter, in der Reihenfolge des Dienstes.
func (e *chaosEnv) stopServer() {
	e.mu.Lock()
	hub, app := e.hub, e.app
	e.mu.Unlock()
	_ = app.ShutdownWithTimeout(3 * time.Second)
	hub.Close()
}

// restartServer ist ein Neustart von LCM: Alle Verbindungen und alle
// wartenden Aufrufe sind weg, die Datenbank bleibt.
func (e *chaosEnv) restartServer() {
	e.stopServer()
	e.startServer()
}

func (e *chaosEnv) currentHub() *Hub {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.hub
}

// newAgent legt eine Serverzeile und einen Proxy an. Gestartet wird der Agent
// mit start.
func (e *chaosEnv) newAgent() *chaosAgent {
	e.t.Helper()
	n := len(e.agents) + 1
	secret := fmt.Sprintf("chaos-secret-%d", n)
	server := &domain.Server{
		Name: fmt.Sprintf("chaos%d", n), Transport: domain.TransportAgent, ServiceUser: "root",
		AgentID:        fmt.Sprintf("00000000-0000-4000-8000-%012d", n),
		AgentTokenHash: wire.HashSecret(secret),
	}
	if err := e.servers.Create(server); err != nil {
		e.t.Fatal(err)
	}
	proxy, err := newFaultProxy(e.target)
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(proxy.Close)
	cfg := &agent.Config{URL: "http://" + proxy.Addr(), AgentID: server.AgentID, Secret: secret}
	a := &chaosAgent{env: e, server: server, proxy: proxy, cfg: cfg, jobs: jobsDir(e.t)}
	a.client = agent.NewClient(cfg, quiet, "test").WithJobsDir(a.jobs)
	e.t.Cleanup(func() { a.client.Stop() })
	e.agents = append(e.agents, a)
	return a
}

// jobsDir legt die Job-Ablage eines Agents an. t.TempDir scheitert beim
// Aufräumen, wenn der Agent im selben Augenblick noch ein Ergebnis bestätigt und
// die Liste der bestätigten IDs schreibt (Runner.Ack); hier wird wiederholt.
func jobsDir(t *testing.T) string {
	dir, err := os.MkdirTemp("", "lcm-chaos-jobs-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for range 20 {
			if os.RemoveAll(dir) == nil {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	})
	return dir
}

// start startet den Agent mit den Betriebszeiten (production) oder kurzen
// Zeiten.
func (a *chaosAgent) start(fast bool) {
	if fast {
		a.client.WithTiming(agent.Timing{
			KeepAlive: fastKeepAlive, PingTimeout: fastPingTimeout,
			BackoffMin: fastBackoffMin, BackoffMax: fastBackoffMax,
		})
	}
	go func() { _ = a.client.Run() }()
}

// restart beendet den Agent und startet einen neuen auf derselben Job-Ablage:
// wie ein Neustart des Dienstes, laufende Kommandos eingeschlossen.
func (a *chaosAgent) restart(fast bool) {
	a.client.Stop()
	a.client = agent.NewClient(a.cfg, quiet, "test").WithJobsDir(a.jobs)
	a.start(fast)
}

// startReady startet den Agent und wartet, bis er Kommandos ausführt.
func (a *chaosAgent) startReady(fast bool) {
	a.start(fast)
	a.requireReady(15 * time.Second)
}

// run führt ein Kommando über den Hub aus. Dauert es länger als timeout, wird
// die Verbindung geschlossen (Abbruch im Agent) und das als Fehler gemeldet.
func (a *chaosAgent) run(cmd string, timeout time.Duration) (string, int, error) {
	conn, err := a.env.currentHub().Conn(a.server)
	if err != nil {
		return "", -1, err
	}
	type outcome struct {
		out  string
		code int
		err  error
	}
	done := make(chan outcome, 1)
	go func() {
		out, code, err := conn.Run(cmd)
		done <- outcome{out, code, err}
	}()
	select {
	case r := <-done:
		_ = conn.Close()
		return r.out, r.code, r.err
	case <-time.After(timeout):
		_ = conn.Close()
		<-done
		return "", -1, fmt.Errorf("keine antwort binnen %s", timeout)
	}
}

// requireReady verlangt, dass der Agent binnen within ein Kommando ausführt,
// und liefert, wie lange das gedauert hat.
func (a *chaosAgent) requireReady(within time.Duration) time.Duration {
	a.env.t.Helper()
	start := time.Now()
	var last error
	for time.Since(start) < within {
		out, code, err := a.run("echo ready", 3*time.Second)
		if err == nil && code == 0 && out == "ready\n" {
			return time.Since(start)
		}
		last = fmt.Errorf("(%q, %d, %v)", out, code, err)
		time.Sleep(200 * time.Millisecond)
	}
	a.env.t.Fatalf("agent nicht wieder bereit binnen %s, letzte antwort %v", within, last)
	return 0
}

// TestChaosNetworkCut5s: Das Netz ist 5 Sekunden weg und kommt zurück (Zeiten
// wie im Betrieb). Der Agent muss von selbst wieder Kommandos ausführen.
func TestChaosNetworkCut5s(t *testing.T) {
	if testing.Short() {
		t.Skip("fehlerinjektion (echter broker, echte wartezeiten)")
	}
	e := newChaosEnv(t)
	a := e.newAgent()
	a.startReady(false)

	a.proxy.Outage()
	time.Sleep(5 * time.Second)
	a.proxy.Heal()
	took := a.requireReady(30 * time.Second)
	t.Logf("nach 5 s Ausfall wieder bereit nach %s", took.Round(100*time.Millisecond))
}

// TestChaosNetworkCut2min: 2 Minuten ohne Netz. Plan: Der Agent ist binnen 2
// Minuten (Backoff-Obergrenze) plus Jitter wieder bereit - gerechnet ab dem
// Ende des Ausfalls.
func TestChaosNetworkCut2min(t *testing.T) {
	if testing.Short() {
		t.Skip("fehlerinjektion, braucht über 2 minuten")
	}
	e := newChaosEnv(t)
	a := e.newAgent()
	a.startReady(false)

	a.proxy.Outage()
	time.Sleep(2 * time.Minute)
	a.proxy.Heal()
	took := a.requireReady(2*time.Minute + 15*time.Second)
	t.Logf("nach 2 min Ausfall wieder bereit nach %s", took.Round(100*time.Millisecond))
}

// TestChaosHalfOpen: Die Verbindung hängt halb offen - der Proxy liest weiter,
// leitet aber nichts mehr weiter, und keiner sieht ein Schließen. Plan: erkannt
// und neu aufgebaut binnen 3 Ping-Intervallen (B2).
func TestChaosHalfOpen(t *testing.T) {
	if testing.Short() {
		t.Skip("fehlerinjektion (echter broker, echte wartezeiten)")
	}
	cases := []struct {
		name     string
		up, down bool
	}{
		{"nur agent nach lcm", true, false},
		{"nur lcm nach agent", false, true},
		{"beide richtungen", true, true},
	}
	// 3 Ping-Intervalle für das Erkennen, dazu der Backoff vor dem ersten
	// Wiederaufbau und etwas Luft.
	bound := 3*fastKeepAlive + fastPingTimeout + 3*time.Second
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newChaosEnv(t)
			a := e.newAgent()
			a.startReady(true)

			before := a.proxy.Accepted()
			a.proxy.Blackhole(tc.up, tc.down)
			start := time.Now()
			waitUntil(t, bound, "agent baut eine neue verbindung auf", func() bool { return a.proxy.Accepted() > before })
			t.Logf("neue Verbindung nach %s", time.Since(start).Round(100*time.Millisecond))
			a.requireReady(bound - time.Since(start))
		})
	}
}

// TestChaosServerRestartDuringCommand: LCM startet neu, während ein Kommando
// im Agent läuft. Der wartende Aufruf endet mit einem Fehler (er lebte im
// alten Prozess), der Agent verbindet sich neu und ist wieder bereit.
func TestChaosServerRestartDuringCommand(t *testing.T) {
	if testing.Short() {
		t.Skip("fehlerinjektion (echter broker, echte wartezeiten)")
	}
	e := newChaosEnv(t)
	a := e.newAgent()
	a.startReady(true)

	type outcome struct {
		out string
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		out, _, err := a.run("sleep 3; echo fertig", 30*time.Second)
		done <- outcome{out, err}
	}()
	time.Sleep(time.Second)
	e.restartServer()

	select {
	case r := <-done:
		if r.err == nil {
			t.Fatalf("aufruf des alten LCM meldet erfolg: %q", r.out)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("aufruf hängt nach dem Neustart von LCM")
	}
	a.requireReady(20 * time.Second)
}

// TestChaosCutDuringCommand: Das Netz reißt 1 Sekunde ab, während ein Kommando
// im Agent läuft. Soll: Das Kommando läuft weiter, der Agent verbindet sich neu,
// das Ergebnis kommt beim wartenden Aufruf an. Ist: Der Hub lässt den Aufruf
// beim Trennen sofort scheitern und verwirft das spätere Ergebnis (B6).
func TestChaosCutDuringCommand(t *testing.T) {
	if testing.Short() {
		t.Skip("fehlerinjektion (echter broker, echte wartezeiten)")
	}
	e := newChaosEnv(t)
	a := e.newAgent()
	a.startReady(true)

	done := runInBackground(a, "sleep 4; echo fertig", 40*time.Second)
	time.Sleep(time.Second)
	a.proxy.Outage()
	time.Sleep(time.Second)
	a.proxy.Heal()

	requireResult(t, done, "fertig\n")
}

// TestChaosCutWhileSendingResult: Das Ergebnis geht verloren, weil die Leitung
// halb offen hängt (Agent → LCM verschluckt), und die Verbindung reißt erst
// danach ab. Soll: Der Agent stellt das Ergebnis nach dem Wiederaufbau zu, der
// Aufruf bekommt es (B6).
func TestChaosCutWhileSendingResult(t *testing.T) {
	if testing.Short() {
		t.Skip("fehlerinjektion (echter broker, echte wartezeiten)")
	}
	e := newChaosEnv(t)
	a := e.newAgent()
	a.startReady(true)

	done := runInBackground(a, "sleep 2; echo fertig", 60*time.Second)
	time.Sleep(500 * time.Millisecond)
	a.proxy.Blackhole(true, false) // das Ergebnis geht ins Leere
	time.Sleep(3 * time.Second)    // Kommando ist fertig, Ergebnis verschluckt
	a.proxy.Outage()
	time.Sleep(time.Second)
	a.proxy.Heal()

	requireResult(t, done, "fertig\n")
}

// TestChaosAgentRestartDuringCommand: Der Agent wird neu gestartet (Stopp, Absturz
// oder Update), während ein Kommando läuft. Soll: Das Kommando läuft weiter, der
// neue Agent nimmt es aus der Job-Ablage wieder auf, und der wartende Aufruf bekommt
// das Ergebnis (C-1, C-2, C-3).
func TestChaosAgentRestartDuringCommand(t *testing.T) {
	if testing.Short() {
		t.Skip("fehlerinjektion (echter broker, echte wartezeiten)")
	}
	e := newChaosEnv(t)
	a := e.newAgent()
	a.startReady(true)

	marker := filepath.Join(t.TempDir(), "lief")
	done := runInBackground(a, "sleep 3; touch "+marker+"; echo fertig", 60*time.Second)
	time.Sleep(time.Second)
	a.restart(true)

	requireResult(t, done, "fertig\n")
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("das kommando lief nicht zu Ende")
	}
	a.requireReady(20 * time.Second)
}

// TestChaosServerRestartWhileSendingResult: Wie oben, aber statt der Leitung
// stirbt LCM. Das Ergebnis kann keinen wartenden Aufruf mehr finden, der Agent
// muss dabei weder hängen noch abstürzen und für das nächste Kommando bereit
// sein.
func TestChaosServerRestartWhileSendingResult(t *testing.T) {
	if testing.Short() {
		t.Skip("fehlerinjektion (echter broker, echte wartezeiten)")
	}
	e := newChaosEnv(t)
	a := e.newAgent()
	a.startReady(true)

	done := runInBackground(a, "sleep 2; echo fertig", 30*time.Second)
	time.Sleep(500 * time.Millisecond)
	a.proxy.Blackhole(true, false)
	time.Sleep(3 * time.Second)
	e.restartServer()

	// Verbindet sich der Agent vor dem Neustart über eine neue (nicht
	// verschluckte) Verbindung, liefert er sein Ergebnis noch dem alten LCM -
	// dann ist Erfolg richtig, aber nur mit der echten Ausgabe.
	if r := <-done; r.err == nil && !strings.Contains(r.out, "fertig") {
		t.Fatalf("aufruf des alten LCM meldet erfolg ohne ergebnis: %q", r.out)
	}
	a.requireReady(20 * time.Second)
}

// subscribeFault ist ein Broker-Hook, der Abonnements ablehnt oder aufhält.
type subscribeFault struct {
	mqtt.HookBase
	reject  atomic.Bool  // Abonnement mit Fehlercode ablehnen
	stall   atomic.Int32 // so viele Abonnements nie beantworten
	release chan struct{}
	once    sync.Once
}

func newSubscribeFault() *subscribeFault { return &subscribeFault{release: make(chan struct{})} }

func (h *subscribeFault) ID() string { return "chaos-subscribe" }

func (h *subscribeFault) Provides(b byte) bool { return b == mqtt.OnSubscribe }

func (h *subscribeFault) OnSubscribe(_ *mqtt.Client, pk packets.Packet) packets.Packet {
	if h.stall.Load() > 0 && h.stall.Add(-1) >= 0 {
		<-h.release // Lesepfad des Clients steht, kein SUBACK
		return pk
	}
	if h.reject.Load() {
		// Ungültiger Filter: Der Broker antwortet mit einem Fehlercode.
		filters := make(packets.Subscriptions, len(pk.Filters))
		for i, f := range pk.Filters {
			f.Filter = "a/#/b"
			filters[i] = f
		}
		pk.Filters = filters
	}
	return pk
}

// Release lässt aufgehaltene Abonnements los (idempotent).
func (h *subscribeFault) Release() { h.once.Do(func() { close(h.release) }) }

// newFaultyEnv baut die Umgebung mit dem Hook, bevor ein Agent sich verbindet.
func newFaultyEnv(t *testing.T) (*chaosEnv, *subscribeFault) {
	t.Helper()
	e := newChaosEnv(t)
	hook := newSubscribeFault()
	if err := e.currentHub().broker.AddHook(hook, nil); err != nil {
		t.Fatal(err)
	}
	// Vor stopServer (LIFO): Ein aufgehaltener Client ließe den Broker nie enden.
	t.Cleanup(hook.Release)
	return e, hook
}

// TestChaosSubscribeRejected: Der Broker lehnt das Abonnement des
// Kommando-Topics ab. Der Agent darf sich dann nicht bereit melden - LCM
// würde Kommandos schicken, die nie ankommen (B1) - und muss es nach
// Behebung des Fehlers von selbst nachholen.
func TestChaosSubscribeRejected(t *testing.T) {
	if testing.Short() {
		t.Skip("fehlerinjektion (echter broker, echte wartezeiten)")
	}
	e, hook := newFaultyEnv(t)
	hook.reject.Store(true)
	a := e.newAgent()
	a.start(true)

	time.Sleep(3 * time.Second)
	if n := e.inventories.Load(); n > 0 {
		t.Errorf("B1: agent meldet sich bereit (inventar von lcm empfangen), obwohl sein abonnement abgelehnt wurde")
	}

	hook.reject.Store(false)
	a.requireReady(15 * time.Second)
}

// TestChaosSubscribeNeverAcknowledged: Der Broker beantwortet das Abonnement
// nie (sein Lesepfad steht). Der Agent darf sich nicht bereit melden und muss
// die Verbindung nach spätestens ein paar Ping-Intervallen neu aufbauen, statt
// ewig auf die Antwort zu warten (B1, B2).
func TestChaosSubscribeNeverAcknowledged(t *testing.T) {
	if testing.Short() {
		t.Skip("fehlerinjektion (echter broker, echte wartezeiten)")
	}
	e, hook := newFaultyEnv(t)
	hook.stall.Store(1)
	a := e.newAgent()
	a.start(true)

	time.Sleep(time.Second)
	if n := e.inventories.Load(); n > 0 {
		t.Errorf("B1: agent meldet sich bereit, obwohl sein abonnement nie bestätigt wurde")
	}
	a.requireReady(3*fastKeepAlive + fastPingTimeout + 3*time.Second)
}

// TestChaosRandomCutCycles: 200 zufällige Abbrüche in Folge. Danach ist der
// Agent bereit, und die Zahl der Goroutinen ist über die Abbrüche nicht
// gewachsen.
func TestChaosRandomCutCycles(t *testing.T) {
	if testing.Short() {
		t.Skip("fehlerinjektion, dauert mehrere minuten")
	}
	const warmup, cycles = 20, 180
	seed := uint64(time.Now().UnixNano())
	t.Logf("seed %d", seed)
	rng := rand.New(rand.NewPCG(seed, 1))

	e := newChaosEnv(t)
	a := e.newAgent()
	a.startReady(true)

	cutCycles(a, rng, warmup)
	a.proxy.Heal()
	a.requireReady(20 * time.Second)
	base := settledGoroutines()

	cutCycles(a, rng, cycles)
	a.proxy.Heal()
	a.requireReady(20 * time.Second)
	if out, code, err := a.run("echo danach", 5*time.Second); err != nil || code != 0 || out != "danach\n" {
		t.Fatalf("kommando nach %d abbrüchen: (%q, %d, %v)", warmup+cycles, out, code, err)
	}

	if grown := settledGoroutines() - base; grown > 5 {
		t.Errorf("%d goroutinen mehr nach %d weiteren abbrüchen (leck)", grown, cycles)
	}
}

// cutCycles kappt und stellt das Netz n-mal in zufälligen Abständen wieder her.
func cutCycles(a *chaosAgent, rng *rand.Rand, n int) {
	for range n {
		a.proxy.Outage()
		time.Sleep(randomUpTo(rng, 300*time.Millisecond))
		a.proxy.Heal()
		time.Sleep(randomUpTo(rng, 600*time.Millisecond))
	}
}

func randomUpTo(rng *rand.Rand, max time.Duration) time.Duration {
	return time.Duration(rng.Int64N(int64(max)))
}

// settledGoroutines gibt den Goroutinen Zeit, sich zu beenden, und zählt dann.
func settledGoroutines() int {
	n := runtime.NumGoroutine()
	for range 20 {
		time.Sleep(250 * time.Millisecond)
		next := runtime.NumGoroutine()
		if next >= n {
			return next
		}
		n = next
	}
	return n
}

// TestChaosReconnectHasJitter: Fällt LCM aus, verbinden sich alle Agenten im
// gleichen Takt wieder, wenn der Backoff keinen Jitter hat - nach einem
// Neustart ein Ansturm im Gleichschritt (B13).
func TestChaosReconnectHasJitter(t *testing.T) {
	if testing.Short() {
		t.Skip("fehlerinjektion (echter broker, echte wartezeiten)")
	}
	const agents = 6
	e := newChaosEnv(t)
	all := make([]*chaosAgent, agents)
	for i := range all {
		all[i] = e.newAgent()
		all[i].start(false)
	}
	for _, a := range all {
		a.requireReady(15 * time.Second)
	}

	cut := time.Now()
	for _, a := range all {
		a.proxy.Outage()
	}
	time.Sleep(12 * time.Second)

	// Verbindungsversuche je Agent nach dem Abriss; die ersten zwei gelten dem
	// ersten Backoff-Schritt, danach wächst die Wartezeit.
	var spread time.Duration
	for i := 1; i <= 3; i++ {
		lo, hi := time.Time{}, time.Time{}
		for _, a := range all {
			at := attemptsAfter(a.proxy.Attempts(), cut)
			if len(at) <= i {
				t.Fatalf("agent versuchte nur %d mal, erwartet mehr als %d", len(at), i)
			}
			if lo.IsZero() || at[i].Before(lo) {
				lo = at[i]
			}
			if at[i].After(hi) {
				hi = at[i]
			}
		}
		spread = max(spread, hi.Sub(lo))
	}
	t.Logf("größte Streuung der Wiederverbindungsversuche über %d Agenten: %s", agents, spread.Round(time.Millisecond))
	if spread < 200*time.Millisecond {
		t.Errorf("B13: %d agenten versuchen es im gleichen takt (streuung %s) - kein jitter im backoff", agents, spread.Round(time.Millisecond))
	}
}

func attemptsAfter(all []time.Time, t0 time.Time) []time.Time {
	var out []time.Time
	for _, at := range all {
		if at.After(t0) {
			out = append(out, at)
		}
	}
	return out
}

// TestChaosNoGoroutineLeak: Ein kompletter Durchlauf (Verbinden, Abriss,
// Kommando, Abbau) hinterlässt keine Goroutinen, bis auf die, die der Agent
// nicht beenden kann: Der Wächter für das Binary läuft bis zum Prozessende.
func TestChaosNoGoroutineLeak(t *testing.T) {
	if testing.Short() {
		t.Skip("fehlerinjektion (echter broker, echte wartezeiten)")
	}
	base := settledGoroutines()
	t.Run("lauf", func(t *testing.T) {
		e := newChaosEnv(t)
		a := e.newAgent()
		a.startReady(true)
		for range 3 {
			a.proxy.Outage()
			time.Sleep(300 * time.Millisecond)
			a.proxy.Heal()
			a.requireReady(15 * time.Second)
		}
	})
	const agentOwn = 1 // switchOnNewBinary
	if grown := settledGoroutines() - base; grown > agentOwn+3 {
		t.Errorf("%d goroutinen mehr als vor dem Test (erlaubt %d)", grown, agentOwn+3)
	}
}

// ---- Hilfen --------------------------------------------------------------------

type runOutcome struct {
	out  string
	code int
	err  error
}

// runInBackground startet ein Kommando und liefert den Kanal mit dem Ausgang.
func runInBackground(a *chaosAgent, cmd string, timeout time.Duration) <-chan runOutcome {
	done := make(chan runOutcome, 1)
	go func() {
		out, code, err := a.run(cmd, timeout)
		done <- runOutcome{out, code, err}
	}()
	return done
}

// requireResult verlangt, dass der Aufruf mit Ausgabe want und Erfolg endet.
func requireResult(t *testing.T, done <-chan runOutcome, want string) {
	t.Helper()
	select {
	case r := <-done:
		if r.err != nil || r.code != 0 || r.out != want {
			t.Fatalf("B6: ergebnis ging verloren: (%q, %d, %v), erwartet (%q, 0, nil)", r.out, r.code, r.err, want)
		}
	case <-time.After(70 * time.Second):
		t.Fatal("aufruf hängt")
	}
}

// waitUntil wartet höchstens timeout auf die Bedingung.
func waitUntil(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("zeitüberschreitung (%s): %s", timeout, what)
}
