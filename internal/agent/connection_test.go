package agent

import (
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"

	"LCM/internal/remote/wire"
)

// TestBackoffStreutUndDeckeltDieWartezeit: jede Wartezeit liegt im Streuband um
// ihre Basis, die Folge verdoppelt sich bis zur Obergrenze, und die Obergrenze
// gilt auch nach der Streuung (B13).
func TestBackoffStreutUndDeckeltDieWartezeit(t *testing.T) {
	const lo, hi = 500 * time.Millisecond, 2 * time.Minute
	distinct := map[time.Duration]bool{}
	for range 200 {
		b := newBackoff(lo, hi)
		base := lo
		for step := range 12 {
			got := b.next()
			distinct[got] = true
			low, high := time.Duration(float64(base)*0.75), min(time.Duration(float64(base)*1.25), hi)
			if got < low || got > high {
				t.Fatalf("schritt %d: %s außerhalb von [%s, %s]", step, got, low, high)
			}
			base = min(2*base, hi)
		}
	}
	if len(distinct) < 100 {
		t.Fatalf("nur %d verschiedene wartezeiten - keine streuung", len(distinct))
	}
}

func TestBackoffBeginntNachResetVonVorn(t *testing.T) {
	b := newBackoff(time.Second, time.Minute)
	for range 6 {
		b.next()
	}
	b.reset()
	if got := b.next(); got > 1250*time.Millisecond {
		t.Fatalf("erste wartezeit nach reset: %s", got)
	}
}

func TestJitteredRandwerte(t *testing.T) {
	if got := jittered(time.Second, 0); got != 750*time.Millisecond {
		t.Errorf("r=0: %s", got)
	}
	if got := jittered(time.Second, 0.5); got != time.Second {
		t.Errorf("r=0.5: %s", got)
	}
	if got := jittered(time.Second, 0.999999); got < 1249*time.Millisecond || got > 1250*time.Millisecond {
		t.Errorf("r~1: %s", got)
	}
}

// TestPanicImKommandoGibtFehlerergebnis: Stürzt der Agent beim Ausführen ab,
// bekommt LCM trotzdem ein Ergebnis (B17), der Agent läuft weiter und zählt
// das Kommando als erledigt.
func TestPanicImKommandoGibtFehlerergebnis(t *testing.T) {
	c, fake := testClient()
	c.run = func(wire.Command, func(int)) wire.Result { panic("kaputt") }
	c.handleCommand(commandPayload(t, wire.Command{ID: "p1", Cmd: "true"}))
	waitResults(t, fake, 1)

	fake.mu.Lock()
	res := fake.results[0]
	fake.mu.Unlock()
	if res.ID != "p1" || res.ExitCode != -1 || res.Error == "" || res.Progress {
		t.Fatalf("ergebnis nach panic: %+v", res)
	}
	deadline := time.Now().Add(2 * time.Second)
	for !c.work.claimIdle(0) {
		if time.Now().After(deadline) {
			t.Fatal("kommando nach dem panic noch in arbeit")
		}
		time.Sleep(10 * time.Millisecond)
	}
	c.work.mu.Lock()
	c.work.switching = false
	c.work.mu.Unlock()

	c.run = c.runner.Run
	c.handleCommand(commandPayload(t, wire.Command{ID: "p2", Cmd: "echo ok"}))
	waitResults(t, fake, 2)
}

func TestLivenessVerlangtPingsErstNachDemErsten(t *testing.T) {
	var l liveness
	t0 := time.Now()
	l.connected(t0)
	limit := time.Minute
	if l.silent(t0.Add(time.Hour), limit) {
		t.Fatal("ohne je ein Ping gesehen zu haben, darf nichts ausbleiben (älteres LCM)")
	}
	l.pinged(t0.Add(time.Second))
	if l.silent(t0.Add(time.Second+limit), limit) {
		t.Fatal("genau am Limit noch nicht still")
	}
	if !l.silent(t0.Add(time.Second+limit+time.Second), limit) {
		t.Fatal("nach dem Limit still")
	}
	// Neue Verbindung: Die Uhr beginnt neu, das Ping-Wissen bleibt.
	l.connected(t0.Add(time.Hour))
	if l.silent(t0.Add(time.Hour+limit), limit) {
		t.Fatal("neue verbindung hat die volle frist")
	}
	if !l.silent(t0.Add(time.Hour+limit+time.Second), limit) {
		t.Fatal("neue verbindung ohne ping bleibt nach der frist still")
	}
	// Das war eine Verbindung ganz ohne Ping: LCM pingt wohl nicht mehr.
	l.connected(t0.Add(2 * time.Hour))
	if l.silent(t0.Add(3*time.Hour), limit) {
		t.Fatal("nach einer pinglosen verbindung verlangt der agent keine pings mehr")
	}
}

// pingRig ist ein Agent an einem echten Broker, der seine Verbindungen zählt.
type pingRig struct {
	broker   *mqtt.Server
	client   *Client
	connects atomic.Int32
	pongs    atomic.Int32
	done     chan struct{}
}

func newPingRig(t *testing.T) *pingRig {
	t.Helper()
	const id = "00000000-0000-4000-8000-00000000b005"
	r := &pingRig{done: make(chan struct{})}
	var addr string
	r.broker, addr = startHelperBroker(t)
	sub := func(topic string, fn func(packets.Packet)) {
		if err := r.broker.Subscribe(topic, 1, func(_ *mqtt.Client, _ packets.Subscription, pk packets.Packet) { fn(pk) }); err != nil {
			t.Fatal(err)
		}
	}
	sub(wire.TopicStatus(id), func(packets.Packet) { r.connects.Add(1) })
	sub(wire.TopicRes(id), func(pk packets.Packet) {
		var res wire.Result
		if json.Unmarshal(pk.Payload, &res) == nil && res.Pong {
			r.pongs.Add(1)
		}
	})
	cfg := &Config{URL: "http://" + addr, AgentID: id, Secret: "egal"}
	r.client = NewClient(cfg, slog.New(slog.DiscardHandler), "test").WithTiming(Timing{
		ServerPing: 150 * time.Millisecond, // Grenze: 450 ms ohne Ping
		BackoffMin: 20 * time.Millisecond, BackoffMax: 100 * time.Millisecond,
	})
	go func() { _ = r.client.Run(); close(r.done) }()
	t.Cleanup(func() {
		r.client.Stop()
		select {
		case <-r.done:
		case <-time.After(5 * time.Second):
			t.Error("Run kehrt nach Stop nicht zurück")
		}
	})
	waitHelper(t, "agent meldet sich", func() bool { return r.connects.Load() == 1 })
	return r
}

func (r *pingRig) ping(t *testing.T) {
	t.Helper()
	payload, _ := json.Marshal(wire.Command{Ping: true})
	id := r.client.cfg.AgentID
	if err := r.broker.Publish(wire.TopicCmd(id), payload, false, 0); err != nil {
		t.Fatal(err)
	}
}

func TestAgentBeantwortetPings(t *testing.T) {
	r := newPingRig(t)
	r.ping(t)
	waitHelper(t, "pong", func() bool { return r.pongs.Load() >= 1 })
}

// TestAusbleibendePingsBauenDieVerbindungNeuAuf: Solange LCM pingt, bleibt die
// Verbindung; bleiben die Pings aus, baut der Agent sie von selbst neu auf (B2).
func TestAusbleibendePingsBauenDieVerbindungNeuAuf(t *testing.T) {
	r := newPingRig(t)

	var stop atomic.Bool
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for !stop.Load() {
			r.ping(t)
			time.Sleep(100 * time.Millisecond)
		}
	}()
	time.Sleep(1500 * time.Millisecond) // mehr als drei Grenzen
	if n := r.connects.Load(); n != 1 {
		t.Fatalf("verbindung trotz Pings neu aufgebaut (%d Verbindungen)", n)
	}
	stop.Store(true)
	wg.Wait()

	waitHelper(t, "agent baut nach ausbleibenden pings neu auf", func() bool { return r.connects.Load() >= 2 })
}

// TestOhnePingsBleibtDieVerbindung: Ein älteres LCM pingt nie - der Agent darf
// deshalb nicht dauernd neu verbinden.
func TestOhnePingsBleibtDieVerbindung(t *testing.T) {
	r := newPingRig(t)
	time.Sleep(1500 * time.Millisecond)
	if n := r.connects.Load(); n != 1 {
		t.Fatalf("agent baut ohne Pings %d-mal auf", n)
	}
}
