package remote

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"

	"LCM/internal/remote/wire"
)

// Tests für Paket C (C-3, C-4): Aufrufe überstehen die Trennung eines Agents mit
// wire.CapabilityJobs, ohne dass LCM etwas nach Zeit anhält. Der Agent ist hier
// ein simulierter Client des Brokers.

// jobsAgent verbindet den Agent des Test-Servers und meldet ihn mit oder ohne
// CapabilityJobs an.
type jobsAgent struct {
	hub    *Hub
	id     string
	cl     *mqtt.Client
	mu     sync.Mutex
	cmds   []wire.Command
	rawCmd [][]byte
}

func newJobsAgent(t *testing.T, hub *Hub, agentID string, jobs bool) *jobsAgent {
	t.Helper()
	a := subscribedAgent(t, hub, agentID)
	a.connect(jobs)
	return a
}

// subscribedAgent legt den simulierten Agent an und lässt ihn sein Kommando-
// Topic abonnieren, ohne ihn zu verbinden.
func subscribedAgent(t *testing.T, hub *Hub, agentID string) *jobsAgent {
	t.Helper()
	a := &jobsAgent{hub: hub, id: agentID}
	if err := hub.broker.Subscribe(wire.TopicCmd(agentID), 99, func(_ *mqtt.Client, _ packets.Subscription, pk packets.Packet) {
		var cmd wire.Command
		if json.Unmarshal(pk.Payload, &cmd) != nil || cmd.Ping {
			return
		}
		a.mu.Lock()
		a.cmds = append(a.cmds, cmd)
		a.rawCmd = append(a.rawCmd, append([]byte(nil), pk.Payload...))
		a.mu.Unlock()
	}); err != nil {
		t.Fatal(err)
	}
	return a
}

// connect lässt den Agent sich (neu) verbinden und sein Inventar senden.
func (a *jobsAgent) connect(jobs bool) {
	inv := wire.Inventory{AgentVersion: "test"}
	if jobs {
		inv.Capabilities = []string{wire.CapabilityJobs}
	}
	payload, _ := json.Marshal(inv)
	a.connectWith(payload)
}

// connectWith verbindet den Agent und sendet das Inventar genau so, wie es
// übergeben wird (Bytes, wie sie ein Agent einer älteren Fassung schickt).
func (a *jobsAgent) connectWith(inventory []byte) {
	a.cl = a.hub.broker.NewClient(nil, "test", a.id, false)
	a.hub.agentConnected(a.cl)
	a.hub.handleInventory(nil, packets.Subscription{}, packets.Packet{TopicName: wire.TopicInv(a.id), Payload: inventory})
}

func (a *jobsAgent) disconnect() { a.hub.agentDisconnected(a.cl, nil) }

func (a *jobsAgent) send(t *testing.T, res wire.Result) {
	t.Helper()
	payload, _ := json.Marshal(res)
	if err := a.hub.broker.Publish(wire.TopicRes(a.id), payload, false, 1); err != nil {
		t.Fatal(err)
	}
}

// lastCommand wartet auf das n-te Kommando (ohne Cancel, wenn cancel false).
func (a *jobsAgent) command(t *testing.T, n int) wire.Command {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		a.mu.Lock()
		if len(a.cmds) > n {
			cmd := a.cmds[n]
			a.mu.Unlock()
			return cmd
		}
		a.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("kommando %d kam nie beim agenten an", n)
	return wire.Command{}
}

type callOutcome struct {
	out  string
	code int
	err  error
}

func runAsync(conn interface {
	Run(string) (string, int, error)
}, cmd string) <-chan callOutcome {
	done := make(chan callOutcome, 1)
	go func() {
		out, code, err := conn.Run(cmd)
		done <- callOutcome{out, code, err}
	}()
	return done
}

func requirePending(t *testing.T, done <-chan callOutcome, d time.Duration) {
	t.Helper()
	select {
	case r := <-done:
		t.Fatalf("der aufruf endete, obwohl er warten sollte: %+v (%v)", r, r.err)
	case <-time.After(d):
	}
}

// TestAufrufWartetUeberDieTrennungEinesAgentsMitJobs: Der Agent trennt sich
// mitten im Kommando und kommt zurück. Der Aufruf scheitert nicht und bekommt
// das Ergebnis (B6).
func TestAufrufWartetUeberDieTrennungEinesAgentsMitJobs(t *testing.T) {
	hub, server, _ := newTestHub(t)
	a := newJobsAgent(t, hub, server.AgentID, true)
	conn, err := hub.Conn(server)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

	done := runAsync(conn, "apt-get upgrade")
	cmd := a.command(t, 0)
	a.disconnect()
	requirePending(t, done, 300*time.Millisecond)

	// Ein neuer Aufruf auf der Conn des getrennten Agents scheitert sofort.
	if _, _, err := conn.Run("noch eins"); !errors.Is(err, ErrAgentOffline) {
		t.Fatalf("neuer aufruf bei getrenntem agent: %v", err)
	}

	a.connect(true)
	a.send(t, wire.Result{ID: cmd.ID, Output: "fertig\n", ExitCode: 0, Recovered: true})
	select {
	case r := <-done:
		if r.err != nil || r.out != "fertig\n" || r.code != 0 {
			t.Fatalf("ergebnis nach der rückkehr: %+v", r)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("das ergebnis kam nie beim aufruf an")
	}

	// Nach der Rückkehr arbeitet dieselbe Conn weiter (ein Scan über mehrere Schritte).
	next := runAsync(conn, "zweiter schritt")
	second := a.command(t, 1)
	a.send(t, wire.Result{ID: second.ID, Output: "ok"})
	if r := <-next; r.err != nil || r.out != "ok" {
		t.Fatalf("folgekommando nach der rückkehr: %+v", r)
	}
}

// TestAgentOhneJobsBehaeltDasAlteVerhalten: Ohne Capability scheitert der
// Aufruf bei der Trennung, und die Conn bleibt tot - wie bisher.
func TestAgentOhneJobsBehaeltDasAlteVerhalten(t *testing.T) {
	hub, server, _ := newTestHub(t)
	a := newJobsAgent(t, hub, server.AgentID, false)
	conn, _ := hub.Conn(server)
	defer func() { _ = conn.Close() }()

	done := runAsync(conn, "hängt")
	a.command(t, 0)
	a.disconnect()
	select {
	case r := <-done:
		if r.err == nil {
			t.Fatal("aufruf an einen alten agenten überlebte die trennung")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("die trennung entsperrte den aufruf nicht")
	}
	a.connect(false)
	if _, _, err := conn.Run("danach"); err == nil {
		t.Fatal("die conn eines alten agenten lebt nach der trennung weiter")
	}
}

// TestStilleFristHaengtVomAgentAb: Ein Agent mit Jobs bekommt keine Stille-Frist,
// ein älterer die eingestellte (seine Notbremse deckt den Verbindungsverlust ab).
func TestStilleFristHaengtVomAgentAb(t *testing.T) {
	for _, tt := range []struct {
		name string
		jobs bool
		want int
	}{
		{"agent mit jobs", true, 0},
		{"alter agent", false, 7200},
	} {
		t.Run(tt.name, func(t *testing.T) {
			hub, server, _ := newTestHubWith(t, func(h *Hub) {
				h.WithIdleTimeout(func() time.Duration { return 2 * time.Hour })
			})
			a := newJobsAgent(t, hub, server.AgentID, tt.jobs)
			conn, _ := hub.Conn(server)
			defer func() { _ = conn.Close() }()
			go func() { _, _, _ = conn.Run("x") }()
			cmd := a.command(t, 0)
			if cmd.IdleTimeoutSec != tt.want {
				t.Errorf("IdleTimeoutSec = %d, erwartet %d", cmd.IdleTimeoutSec, tt.want)
			}
			a.mu.Lock()
			raw := string(a.rawCmd[0])
			a.mu.Unlock()
			if tt.jobs && strings.Contains(raw, "idle_timeout_sec") {
				t.Errorf("ein agent mit jobs bekommt keine stille-frist, payload: %s", raw)
			}
		})
	}
}

// TestAlterAgentOhneEinstellungBekommtDieVorgabe: Ohne verdrahtete Einstellung
// gilt defaultIdleTimeout - wie vor den dauerhaften Jobs.
func TestAlterAgentOhneEinstellungBekommtDieVorgabe(t *testing.T) {
	hub, server, _ := newTestHub(t)
	a := newJobsAgent(t, hub, server.AgentID, false)
	conn, _ := hub.Conn(server)
	defer func() { _ = conn.Close() }()
	go func() { _, _, _ = conn.Run("x") }()

	if got, want := a.command(t, 0).IdleTimeoutSec, int(defaultIdleTimeout.Seconds()); got != want {
		t.Errorf("IdleTimeoutSec = %d, erwartet %d", got, want)
	}
}

// TestAlterAgentWirdNachStilleAbgebrochen: Ohne Lebenszeichen bricht LCM das
// Kommando eines älteren Agents ab (Cancel) - seine Notbremse wie früher.
func TestAlterAgentWirdNachStilleAbgebrochen(t *testing.T) {
	hub, server, _ := newTestHubWith(t, func(h *Hub) {
		h.WithIdleTimeout(func() time.Duration { return 300 * time.Millisecond })
	})
	a := newJobsAgent(t, hub, server.AgentID, false)
	conn, _ := hub.Conn(server)
	defer func() { _ = conn.Close() }()

	done := runAsync(conn, "hängt")
	cmd := a.command(t, 0)
	select {
	case r := <-done:
		if r.err == nil || !strings.Contains(r.err.Error(), "stille abgebrochen") {
			t.Fatalf("erwartet Abbruch wegen Stille, bekommen: %v", r.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("die Stille-Frist brach den Aufruf nicht ab")
	}
	if cancel := a.command(t, 1); !cancel.Cancel || cancel.ID != cmd.ID {
		t.Errorf("zweites Kommando = %+v, erwartet Cancel für %s", cancel, cmd.ID)
	}
}

// TestLebenszeichenStelltDieStilleUhrZurueck: Solange ein älterer Agent sich
// meldet, läuft sein Kommando beliebig lange.
func TestLebenszeichenStelltDieStilleUhrZurueck(t *testing.T) {
	hub, server, _ := newTestHubWith(t, func(h *Hub) {
		h.WithIdleTimeout(func() time.Duration { return 400 * time.Millisecond })
	})
	a := newJobsAgent(t, hub, server.AgentID, false)
	conn, _ := hub.Conn(server)
	defer func() { _ = conn.Close() }()

	done := runAsync(conn, "arbeitet")
	cmd := a.command(t, 0)
	for range 4 {
		time.Sleep(200 * time.Millisecond)
		a.send(t, wire.Result{ID: cmd.ID, Progress: true})
	}
	requirePending(t, done, 100*time.Millisecond)
	a.send(t, wire.Result{ID: cmd.ID, Output: "fertig"})
	if r := <-done; r.err != nil || r.out != "fertig" {
		t.Fatalf("ergebnis: %+v", r)
	}
}

// TestAgentMitJobsWirdNieNachStilleAbgebrochen: Dieselbe Einstellung hält das
// Kommando eines Agents mit Jobs nicht an.
func TestAgentMitJobsWirdNieNachStilleAbgebrochen(t *testing.T) {
	hub, server, _ := newTestHubWith(t, func(h *Hub) {
		h.WithIdleTimeout(func() time.Duration { return 100 * time.Millisecond })
	})
	a := newJobsAgent(t, hub, server.AgentID, true)
	conn, _ := hub.Conn(server)
	defer func() { _ = conn.Close() }()

	done := runAsync(conn, "läuft lange")
	cmd := a.command(t, 0)
	requirePending(t, done, 500*time.Millisecond)
	a.send(t, wire.Result{ID: cmd.ID, Output: "fertig"})
	if r := <-done; r.err != nil || r.out != "fertig" {
		t.Fatalf("ergebnis: %+v", r)
	}
}

// TestObergrenzeBeendetNurDasWarten: Meldet sich ein getrennter Agent über die
// Obergrenze hinaus nicht, endet der Aufruf mit ErrResultUnknown. Es geht kein
// Abbruch hinaus, und ein später eintreffendes Ergebnis wird protokolliert.
func TestObergrenzeBeendetNurDasWarten(t *testing.T) {
	hub, server, _ := newTestHub(t)
	var logs syncBuffer
	hub.log = slog.New(slog.NewTextHandler(&logs, nil))
	hub.WithMaxSilence(300 * time.Millisecond)
	a := newJobsAgent(t, hub, server.AgentID, true)
	conn, _ := hub.Conn(server)
	defer func() { _ = conn.Close() }()

	done := runAsync(conn, "lang")
	cmd := a.command(t, 0)
	a.disconnect()
	select {
	case r := <-done:
		if !errors.Is(r.err, ErrResultUnknown) {
			t.Fatalf("erwartet ErrResultUnknown, got %+v", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("der aufruf blockiert über die obergrenze hinaus")
	}

	a.connect(true)
	time.Sleep(100 * time.Millisecond)
	a.mu.Lock()
	for _, c := range a.cmds {
		if c.Cancel {
			t.Errorf("nach der obergrenze ging ein abbruch hinaus: %+v", c)
		}
	}
	a.mu.Unlock()

	a.send(t, wire.Result{ID: cmd.ID, Output: "viel zu spät", ExitCode: 0, Recovered: true})
	waitUntil(t, 3*time.Second, "verspätetes ergebnis im protokoll", func() bool {
		return strings.Contains(logs.String(), "late result") && strings.Contains(logs.String(), cmd.ID)
	})
}

// TestLebenszeichenVerlaengernDieWartezeitUndLiefernVitals: Solange der Agent
// Lebenszeichen schickt, gibt der Aufruf nicht auf - auch über die Obergrenze
// hinaus; die Beobachtungen des Agents sind abrufbar.
func TestLebenszeichenVerlaengernDieWartezeitUndLiefernVitals(t *testing.T) {
	hub, server, _ := newTestHub(t)
	hub.WithMaxSilence(400 * time.Millisecond)
	a := newJobsAgent(t, hub, server.AgentID, true)
	conn, _ := hub.Conn(server)
	defer func() { _ = conn.Close() }()

	done := runAsync(conn, "arbeitet")
	cmd := a.command(t, 0)
	for i := 0; i < 8; i++ { // 800 ms, doppelt so lang wie die Obergrenze
		time.Sleep(100 * time.Millisecond)
		a.send(t, wire.Result{ID: cmd.ID, Progress: true, OutputBytes: i,
			Vitals: &wire.Vitals{Alive: true, Procs: 3, CPUMillis: 120, SilentSec: 42}})
	}
	st, ok := hub.CommandStatus(server.AgentID)
	if !ok || st.Vitals == nil || st.Vitals.SilentSec != 42 || st.Vitals.Procs != 3 || !st.Online || st.At.IsZero() {
		t.Fatalf("CommandStatus = %+v (%v)", st, ok)
	}
	a.send(t, wire.Result{ID: cmd.ID, Output: "fertig"})
	if r := <-done; r.err != nil || r.out != "fertig" {
		t.Fatalf("ergebnis = %+v", r)
	}
	if _, ok := hub.CommandStatus(server.AgentID); ok {
		t.Fatal("CommandStatus meldet ein Kommando, das fertig ist")
	}
}

// TestAbbruchBeiGetrenntemAgentKommtNachDerRueckkehr: Ein ausdrücklicher
// Abbruch (Close) bei getrenntem Agent geht nicht verloren, sondern hinaus,
// sobald der Agent sein Inventar gesendet hat.
func TestAbbruchBeiGetrenntemAgentKommtNachDerRueckkehr(t *testing.T) {
	hub, server, _ := newTestHub(t)
	a := newJobsAgent(t, hub, server.AgentID, true)
	conn, _ := hub.Conn(server)

	done := runAsync(conn, "läuft")
	cmd := a.command(t, 0)
	a.disconnect()
	requirePending(t, done, 100*time.Millisecond)
	_ = conn.Close() // der Anwender bricht ab
	if r := <-done; r.err == nil {
		t.Fatal("abgebrochener aufruf meldet erfolg")
	}
	a.mu.Lock()
	for _, c := range a.cmds {
		if c.Cancel {
			t.Fatal("ein abbruch ging an einen getrennten agenten")
		}
	}
	a.mu.Unlock()

	a.connect(true)
	cancel := a.command(t, 1)
	if !cancel.Cancel || cancel.ID != cmd.ID {
		t.Fatalf("nach der rückkehr: %+v, erwartet abbruch für %s", cancel, cmd.ID)
	}
}

// TestAusschaltenDesAgentZugangsLaesstWartendeScheitern: Wird der Zugang
// ausgeschaltet, kommt kein Agent zurück - die Aufrufe scheitern, statt bis zur
// Obergrenze zu warten.
func TestAusschaltenDesAgentZugangsLaesstWartendeScheitern(t *testing.T) {
	hub, server, _ := newTestHub(t)
	a := newJobsAgent(t, hub, server.AgentID, true)
	conn, _ := hub.Conn(server)
	defer func() { _ = conn.Close() }()
	done := runAsync(conn, "läuft")
	a.command(t, 0)
	a.disconnect()
	requirePending(t, done, 100*time.Millisecond)
	hub.SetAccepting(false)
	select {
	case r := <-done:
		if r.err == nil {
			t.Fatal("aufruf überlebte das ausschalten")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("aufruf wartet trotz ausgeschaltetem zugang")
	}
}

// TestTokenWiderrufLaesstWartendeScheitern: Hub.Disconnect (Widerruf, Löschen des
// Servers) ist gewollt - der Agent kommt nicht wieder.
func TestTokenWiderrufLaesstWartendeScheitern(t *testing.T) {
	hub, server, _ := newTestHub(t)
	a := newJobsAgent(t, hub, server.AgentID, true)
	conn, _ := hub.Conn(server)
	defer func() { _ = conn.Close() }()
	done := runAsync(conn, "läuft")
	a.command(t, 0)
	hub.Disconnect(server.AgentID)
	select {
	case r := <-done:
		if r.err == nil {
			t.Fatal("aufruf überlebte den widerruf")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("aufruf wartet trotz widerruf")
	}
}

// syncBuffer ist ein Puffer, den der Logger und der Test gleichzeitig benutzen.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}
