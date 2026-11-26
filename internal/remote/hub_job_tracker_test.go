package remote

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/mochi-mqtt/server/v2/packets"

	"LCM/internal/core/domain"
	"LCM/internal/remote/wire"
)

// fakeTracker hält fest, was der Hub dem JobTracker meldet.
type fakeTracker struct {
	mu   sync.Mutex
	sent [][2]string // jobID, reqID
	done [][2]string
	late []lateCall
}

type lateCall struct {
	agentID string
	res     wire.Result
}

func (f *fakeTracker) CommandSent(jobID, reqID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, [2]string{jobID, reqID})
}

func (f *fakeTracker) CommandDone(jobID, reqID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.done = append(f.done, [2]string{jobID, reqID})
}

func (f *fakeTracker) LateResult(agentID string, res wire.Result) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.late = append(f.late, lateCall{agentID, res})
	return true
}

func (f *fakeTracker) snapshot() (sent, done [][2]string, late []lateCall) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append(sent, f.sent...), append(done, f.done...), append(late, f.late...)
}

// bindable ist die Conn des Hubs mit der Fähigkeit, einen Job zu kennen.
type bindable interface {
	BindJob(jobID string)
}

// TestSpaetesErgebnisErreichtDenJobTracker: LCM gibt nach maxSilence auf (oder
// wurde neu gestartet); kommt das Ergebnis danach, geht es mit der Kennung des
// Jobs an den Tracker - und CommandDone wurde nie gemeldet (B18).
func TestSpaetesErgebnisErreichtDenJobTracker(t *testing.T) {
	tracker := &fakeTracker{}
	hub, server, _ := newTestHubWith(t, func(h *Hub) {
		h.WithJobTracker(tracker).WithMaxSilence(150 * time.Millisecond)
	})
	a := newJobsAgent(t, hub, server.AgentID, true)
	conn, err := hub.Conn(server)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.(bindable).BindJob("job-1")

	if _, _, err := conn.Run("langsam"); err != ErrResultUnknown {
		t.Fatalf("Run muss mit ErrResultUnknown enden, got %v", err)
	}
	cmd := a.command(t, 0)
	a.send(t, wire.Result{ID: cmd.ID, Output: "fertig", ExitCode: 0, Recovered: true})

	waitFor(t, func() bool { _, _, late := tracker.snapshot(); return len(late) == 1 })
	sent, done, late := tracker.snapshot()
	if len(sent) != 1 || sent[0] != [2]string{"job-1", cmd.ID} {
		t.Errorf("CommandSent = %v, want job-1/%s", sent, cmd.ID)
	}
	if len(done) != 0 {
		t.Errorf("CommandDone nach ErrResultUnknown gemeldet: %v", done)
	}
	if late[0].agentID != server.AgentID || late[0].res.ID != cmd.ID || late[0].res.Output != "fertig" {
		t.Errorf("LateResult = %+v", late[0])
	}
}

// TestNormaleZustellungMeldetCommandDone: Kommt das Ergebnis beim Wartenden an,
// meldet der Hub CommandDone und nie LateResult.
func TestNormaleZustellungMeldetCommandDone(t *testing.T) {
	tracker := &fakeTracker{}
	hub, server, _ := newTestHubWith(t, func(h *Hub) { h.WithJobTracker(tracker) })
	a := newJobsAgent(t, hub, server.AgentID, true)
	conn, err := hub.Conn(server)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.(bindable).BindJob("job-2")

	done := runAsync(conn, "ls")
	cmd := a.command(t, 0)
	a.send(t, wire.Result{ID: cmd.ID, Output: "ok", ExitCode: 0})
	if r := <-done; r.err != nil || r.out != "ok" {
		t.Fatalf("Run = %+v", r)
	}

	sent, finished, late := tracker.snapshot()
	if len(sent) != 1 || len(finished) != 1 || finished[0] != [2]string{"job-2", cmd.ID} {
		t.Errorf("sent=%v done=%v, want je ein Aufruf für job-2/%s", sent, finished, cmd.ID)
	}
	if len(late) != 0 {
		t.Errorf("LateResult trotz Wartendem: %v", late)
	}
}

// TestKommandoOhneJobBleibtUnverfolgt: Eine Conn ohne Job (Scan, Health-Check)
// und ein Agent ohne Jobs melden dem Tracker nichts.
func TestKommandoOhneJobBleibtUnverfolgt(t *testing.T) {
	tracker := &fakeTracker{}
	hub, server, _ := newTestHubWith(t, func(h *Hub) { h.WithJobTracker(tracker) })
	a := newJobsAgent(t, hub, server.AgentID, true)
	conn, err := hub.Conn(server)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	done := runAsync(conn, "ls")
	cmd := a.command(t, 0)
	a.send(t, wire.Result{ID: cmd.ID, ExitCode: 0})
	<-done

	sent, finished, _ := tracker.snapshot()
	if len(sent) != 0 || len(finished) != 0 {
		t.Errorf("Conn ohne Job gemeldet: sent=%v done=%v", sent, finished)
	}
}

// TestErgebnisEinesAnderenAgentsWirdNichtZugestellt: Ein Ergebnis mit der
// Kennung eines wartenden Aufrufs, aber vom Topic eines anderen Agents, erreicht
// den Wartenden nicht; der Tracker bekommt es mit der ID des ANDEREN Agents und
// kann es abweisen.
func TestErgebnisEinesAnderenAgentsWirdNichtZugestellt(t *testing.T) {
	tracker := &fakeTracker{}
	hub, server, _ := newTestHubWith(t, func(h *Hub) { h.WithJobTracker(tracker) })
	a := newJobsAgent(t, hub, server.AgentID, true)
	conn, err := hub.Conn(server)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.(bindable).BindJob("job-3")

	done := runAsync(conn, "ls")
	cmd := a.command(t, 0)

	const other = "ffffffff-aaaa-bbbb-cccc-1234567890ab"
	payload, _ := json.Marshal(wire.Result{ID: cmd.ID, Output: "gefälscht", ExitCode: 0})
	hub.handleResult(nil, packets.Subscription{}, packets.Packet{TopicName: wire.TopicRes(other), Payload: payload})

	requirePending(t, done, 100*time.Millisecond)
	_, _, late := tracker.snapshot()
	if len(late) != 1 || late[0].agentID != other {
		t.Fatalf("LateResult = %+v, want ein Aufruf mit agentID %s", late, other)
	}

	a.send(t, wire.Result{ID: cmd.ID, Output: "echt", ExitCode: 0})
	if r := <-done; r.out != "echt" {
		t.Errorf("Wartender bekam %q, want echt", r.out)
	}
}

// trackedCall startet einen Aufruf des Jobs jobID an einen Agent mit Jobs und
// wartet, bis das Kommando dort angekommen ist.
func trackedCall(t *testing.T, hub *Hub, server *domain.Server, a *jobsAgent, jobID string) (<-chan callOutcome, wire.Command) {
	t.Helper()
	conn, err := hub.Conn(server)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	conn.(bindable).BindJob(jobID)
	done := runAsync(conn, "apt-get upgrade")
	return done, a.command(t, 0)
}

func requireOutcome(t *testing.T, done <-chan callOutcome) callOutcome {
	t.Helper()
	select {
	case r := <-done:
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("der Aufruf endete nicht")
		return callOutcome{}
	}
}

// TestHubCloseLaesstDieKennungStehen: Beendet sich LCM, während ein Kommando
// läuft, bleibt die Kennung im Job, damit das Ergebnis nach dem Neustart
// nachgetragen wird (B18).
func TestHubCloseLaesstDieKennungStehen(t *testing.T) {
	tracker := &fakeTracker{}
	hub, server, _ := newTestHubWith(t, func(h *Hub) { h.WithJobTracker(tracker) })
	a := newJobsAgent(t, hub, server.AgentID, true)
	done, _ := trackedCall(t, hub, server, a, "job-close")

	hub.Close() // das Cleanup von newTestHubWith ruft Close erneut

	if r := requireOutcome(t, done); !errors.Is(r.err, errHubClosed) {
		t.Fatalf("err = %v, want errHubClosed", r.err)
	}
	if _, finished, _ := tracker.snapshot(); len(finished) != 0 {
		t.Errorf("CommandDone nach Hub.Close gemeldet: %v", finished)
	}
}

// TestZugangAusLaesstDieKennungStehen: Auch das Abschalten des Agent-Zugangs
// beendet nur das Warten, nicht das Kommando.
func TestZugangAusLaesstDieKennungStehen(t *testing.T) {
	tracker := &fakeTracker{}
	hub, server, _ := newTestHubWith(t, func(h *Hub) { h.WithJobTracker(tracker) })
	a := newJobsAgent(t, hub, server.AgentID, true)
	done, _ := trackedCall(t, hub, server, a, "job-off")

	hub.SetAccepting(false)

	if r := requireOutcome(t, done); !errors.Is(r.err, errAgentAccessOff) {
		t.Fatalf("err = %v, want errAgentAccessOff", r.err)
	}
	if _, finished, _ := tracker.snapshot(); len(finished) != 0 {
		t.Errorf("CommandDone nach SetAccepting(false) gemeldet: %v", finished)
	}
}

// TestSchweigenLaesstDieKennungStehen: Gibt der Hub auf, weil der verbundene
// Agent schweigt, ist das nur eine Vermutung. Der Fehler gilt als
// ErrCommandUnknown, die Kennung bleibt aber stehen.
func TestSchweigenLaesstDieKennungStehen(t *testing.T) {
	tracker := &fakeTracker{}
	hub, server, _ := newTestHubWith(t, func(h *Hub) {
		h.WithJobTracker(tracker).WithJobSilence(100 * time.Millisecond)
	})
	a := newJobsAgent(t, hub, server.AgentID, true)
	done, _ := trackedCall(t, hub, server, a, "job-silent")

	r := requireOutcome(t, done)
	if !errors.Is(r.err, ErrCommandUnknown) {
		t.Fatalf("err = %v, want ErrCommandUnknown", r.err)
	}
	if _, finished, _ := tracker.snapshot(); len(finished) != 0 {
		t.Errorf("CommandDone nach Schweigen gemeldet: %v", finished)
	}
}

// TestUnbekanntesKommandoNimmtDieKennungAus: Meldet der Agent in seinem Inventar,
// das Kommando nicht zu kennen, ist der Ausgang sicher (nie angekommen): die
// Kennung wird abgeholt.
func TestUnbekanntesKommandoNimmtDieKennungAus(t *testing.T) {
	tracker := &fakeTracker{}
	hub, server, _ := newTestHubWith(t, func(h *Hub) { h.WithJobTracker(tracker) })
	a := newJobsAgent(t, hub, server.AgentID, true)
	done, cmd := trackedCall(t, hub, server, a, "job-unknown")
	a.disconnect()
	requirePending(t, done, 100*time.Millisecond)

	a.connectWith(inventoryJSON([]string{"anderes-kommando"}))

	if r := requireOutcome(t, done); !errors.Is(r.err, ErrCommandUnknown) {
		t.Fatalf("err = %v, want ErrCommandUnknown", r.err)
	}
	waitFor(t, func() bool { _, finished, _ := tracker.snapshot(); return len(finished) == 1 })
	if _, finished, _ := tracker.snapshot(); finished[0] != [2]string{"job-unknown", cmd.ID} {
		t.Errorf("CommandDone = %v, want job-unknown/%s", finished, cmd.ID)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("Bedingung wurde nie erfüllt")
}
