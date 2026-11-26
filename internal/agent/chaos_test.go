package agent

// Fehlerinjektion im Agent selbst (Paket A2 aus
// doc/plan-2026-11-23-agent-robustheit.md), soweit sie über den Hub nicht
// auslösbar ist: ein Panic im Rahmen-Handler (B3) und das Lebenszeichen
// wartender Kommandos (B7). Rot heißt: der Befund besteht noch.

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/listeners"
	"github.com/mochi-mqtt/server/v2/packets"

	"LCM/internal/remote/wire"
)

// panicHelperEnv schaltet den Test-Prozess in den Hilfsmodus: Er ist dann der
// Agent, der den Panic erleidet (os.Args-Muster wie in os/exec).
const panicHelperEnv = "LCM_CHAOS_PANIC_HELPER"

// panicHelperID ist die Agent-ID im Hilfsprozess.
const panicHelperID = "00000000-0000-4000-8000-00000000b003"

// TestChaosPanicInFrameHandler: Ein Panic beim Bearbeiten eines Rahmens der
// Web-Konsole läuft im paho-Callback, ohne recover. Soll: Der Agent bleibt am
// Leben, denn er ist oft der einzige Zugang zum Server (B3). Der Panic muss in
// einem eigenen Prozess passieren, er risse sonst den Testlauf mit.
func TestChaosPanicInFrameHandler(t *testing.T) {
	if testing.Short() {
		t.Skip("startet einen Unterprozess")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestChaosPanicHelper$", "-test.v")
	cmd.Env = append(os.Environ(), panicHelperEnv+"=1")
	done := make(chan struct{})
	var out []byte
	var err error
	go func() {
		out, err = cmd.CombinedOutput()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(40 * time.Second):
		_ = cmd.Process.Kill()
		<-done
		t.Fatalf("hilfsprozess hängt:\n%s", tail(out))
	}
	if err != nil || !strings.Contains(string(out), "AGENT-LEBT") {
		t.Fatalf("B3: der agent-prozess ist nach einem panic im rahmen-handler beendet (%v):\n%s", err, tail(out))
	}
}

// TestChaosPanicHelper ist der Agent im Hilfsprozess (nur mit panicHelperEnv).
// Er hängt an einem eigenen Broker, bekommt einen „öffnen"-Rahmen, dessen
// Shell-Start panict, und meldet nach einer Weile, dass er noch läuft.
func TestChaosPanicHelper(t *testing.T) {
	if os.Getenv(panicHelperEnv) == "" {
		t.Skip("nur im Hilfsprozess von TestChaosPanicInFrameHandler")
	}
	startShell = func(string, int, int) (shellProcess, error) { panic("kaputte shell") }

	broker, addr := startHelperBroker(t)
	var online atomic.Bool
	if err := broker.Subscribe(wire.TopicStatus(panicHelperID), 1, func(*mqtt.Client, packets.Subscription, packets.Packet) { online.Store(true) }); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{URL: "http://" + addr, AgentID: panicHelperID, Secret: "egal"}
	go func() { _ = NewClient(cfg, slog.New(slog.DiscardHandler), "test").Run() }()
	waitHelper(t, "agent meldet sich", func() bool { return online.Load() })

	open, _ := json.Marshal(wire.TTYFrame{Session: "s1", Seq: 1, Op: wire.TTYOpen, Term: "xterm", Cols: 80, Rows: 24})
	if err := broker.Publish(wire.TopicTTY(panicHelperID), open, false, 1); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Second)
	fmt.Println("AGENT-LEBT")
}

// startHelperBroker startet einen Broker mit WebSocket-Listener auf einem
// freien Port, der jeden Zugang erlaubt.
func startHelperBroker(t *testing.T) (*mqtt.Server, string) {
	t.Helper()
	return startBrokerWith(t, new(allowAll))
}

// startBrokerWith wie startHelperBroker mit einem eigenen Zugangs-Hook.
func startBrokerWith(t *testing.T, access mqtt.Hook) (*mqtt.Server, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	broker := mqtt.New(&mqtt.Options{InlineClient: true, Logger: slog.New(slog.DiscardHandler)})
	if err := broker.AddHook(access, nil); err != nil {
		t.Fatal(err)
	}
	if err := broker.AddListener(listeners.NewWebsocket(listeners.Config{ID: "ws", Address: addr})); err != nil {
		t.Fatal(err)
	}
	if err := broker.Serve(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = broker.Close() })
	return broker, addr
}

// allowAll erlaubt jeden Zugang (mochis auth-Paket brächte eine weitere
// Abhängigkeit ins Modul).
type allowAll struct{ mqtt.HookBase }

func (*allowAll) ID() string { return "allow-all" }

func (*allowAll) Provides(b byte) bool {
	return b == mqtt.OnConnectAuthenticate || b == mqtt.OnACLCheck
}

func (*allowAll) OnConnectAuthenticate(*mqtt.Client, packets.Packet) bool { return true }

func (*allowAll) OnACLCheck(*mqtt.Client, string, bool) bool { return true }

func waitHelper(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("zeitüberschreitung: %s", what)
}

// tail kürzt die Ausgabe des Hilfsprozesses auf das Ende.
func tail(out []byte) string {
	const keep = 1500
	if len(out) > keep {
		out = out[len(out)-keep:]
	}
	return string(out)
}

// TestChaosQueuedCommandSendsProgress: Ein Kommando, das hinter einem
// laufenden wartet, schickt kein Lebenszeichen. Der Server rechnet die Stille
// ab dem Absenden und bricht es ab, obwohl es nur wartet (B7). Soll: Auch
// wartende Kommandos melden sich im Takt von wire.ProgressInterval.
func TestChaosQueuedCommandSendsProgress(t *testing.T) {
	if testing.Short() {
		t.Skip("braucht über ein Lebenszeichen-Intervall")
	}
	r := NewRunner()
	blockerDone := make(chan struct{})
	go func() {
		r.Run(wire.Command{ID: "vorne", Cmd: fmt.Sprintf("sleep %d", int(wire.ProgressInterval.Seconds())+3)}, nil)
		close(blockerDone)
	}()
	time.Sleep(500 * time.Millisecond) // "vorne" hat execMu

	var beats atomic.Int32
	waiting := make(chan struct{})
	go func() {
		r.Run(wire.Command{ID: "wartend", Cmd: "true"}, func(int) { beats.Add(1) })
		close(waiting)
	}()
	<-blockerDone
	<-waiting
	if beats.Load() == 0 {
		t.Errorf("B7: wartendes kommando sendete in %s hinter einem laufenden kein einziges lebenszeichen", wire.ProgressInterval+3*time.Second)
	}
}
