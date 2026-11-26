package agent

import (
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"
)

// notifySocket öffnet einen Unix-Datagram-Socket wie den, den systemd für
// NOTIFY_SOCKET anlegt. Der Pfad bleibt kurz: Unix-Sockets vertragen unter
// macOS nur gut 100 Zeichen, t.TempDir wäre zu lang.
func notifySocket(t *testing.T) (string, *net.UnixConn) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "lcmn")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "notify")
	return path, listenDatagram(t, path)
}

func listenDatagram(t *testing.T, name string) *net.UnixConn {
	t.Helper()
	conn, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: name, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// receive liest das nächste Datagramm; "" nach within ohne Meldung.
func receive(conn *net.UnixConn, within time.Duration) string {
	_ = conn.SetReadDeadline(time.Now().Add(within))
	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	if err != nil {
		return ""
	}
	return string(buf[:n])
}

func TestNotifierSchicktDatagramme(t *testing.T) {
	path, conn := notifySocket(t)
	t.Setenv("NOTIFY_SOCKET", path)
	n := notifierFromEnv()
	if n == nil {
		t.Fatal("kein notifier trotz NOTIFY_SOCKET")
	}
	for _, msg := range []string{"READY=1", "WATCHDOG=1", "STOPPING=1"} {
		if err := n.send(msg); err != nil {
			t.Fatal(err)
		}
		if got := receive(conn, time.Second); got != msg {
			t.Fatalf("gesendet %q, empfangen %q", msg, got)
		}
	}
}

// TestNotifierAbstrakterSocket: systemd nennt abstrakte Sockets mit führendem
// "@"; Go versteht das unter Linux selbst.
func TestNotifierAbstrakterSocket(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("abstrakte Unix-Sockets gibt es nur unter Linux")
	}
	name := "@lcm-agent-notify-test-" + strconv.Itoa(os.Getpid())
	conn := listenDatagram(t, name)
	t.Setenv("NOTIFY_SOCKET", name)
	n := notifierFromEnv()
	if n == nil {
		t.Fatal("kein notifier für abstrakten Socket")
	}
	if err := n.send("READY=1"); err != nil {
		t.Fatal(err)
	}
	if got := receive(conn, time.Second); got != "READY=1" {
		t.Fatalf("empfangen %q", got)
	}
}

// TestNotifierOhneSocket: Unter OpenRC oder von Hand gibt es keinen Socket -
// alles bleibt ein No-op.
func TestNotifierOhneSocket(t *testing.T) {
	for _, env := range []string{"", "vsock:2:1234"} {
		t.Setenv("NOTIFY_SOCKET", env)
		n := notifierFromEnv()
		if n != nil {
			t.Errorf("notifier für NOTIFY_SOCKET=%q", env)
		}
		if err := n.send("READY=1"); err != nil {
			t.Errorf("send ohne notifier: %v", err)
		}
	}
}

func TestWatchdogIntervalAusUmgebung(t *testing.T) {
	cases := []struct {
		usec, pid string
		want      time.Duration
		ok        bool
	}{
		{"", "", 0, false},
		{"0", "", 0, false},
		{"abc", "", 0, false},
		{"90000000", "", 45 * time.Second, true},
		{"90000000", strconv.Itoa(os.Getpid()), 45 * time.Second, true},
		{"90000000", "1", 0, false}, // gilt einem anderen Prozess
	}
	for _, c := range cases {
		t.Setenv("WATCHDOG_USEC", c.usec)
		t.Setenv("WATCHDOG_PID", c.pid)
		got, ok := watchdogInterval()
		if got != c.want || ok != c.ok {
			t.Errorf("usec=%q pid=%q: (%s, %v), erwartet (%s, %v)", c.usec, c.pid, got, ok, c.want, c.ok)
		}
	}
}

// TestWatchdogFuettertNurSolangeDieSchleifeLebt: Die Verbindungsschleife
// verlängert ihre Frist (heartbeat); läuft sie ab, bleibt das Futter aus.
func TestWatchdogFuettertNurSolangeDieSchleifeLebt(t *testing.T) {
	path, conn := notifySocket(t)
	c := NewClient(&Config{AgentID: "a"}, nil, "test")
	c.notify = &notifier{socket: path}
	t.Cleanup(c.Stop)
	c.beat.extend(time.Minute)
	go c.feedWatchdog(40 * time.Millisecond)

	if got := receive(conn, time.Second); got != "WATCHDOG=1" {
		t.Fatalf("gesunder agent: %q statt WATCHDOG=1", got)
	}
	c.beat.extend(-time.Second) // Schleife hängt: Frist ist abgelaufen
	receive(conn, 100*time.Millisecond)
	if got := receive(conn, 300*time.Millisecond); got != "" {
		t.Fatalf("hängender agent füttert weiter: %q", got)
	}
	c.beat.extend(time.Minute) // Schleife läuft wieder
	if got := receive(conn, time.Second); got != "WATCHDOG=1" {
		t.Fatalf("erholter agent: %q statt WATCHDOG=1", got)
	}
}

// TestRunMeldetSystemdUndFuettertAuchOhneVerbindung: READY=1 sobald die
// Hauptschleife läuft - auch wenn LCM nicht erreichbar ist -, WATCHDOG=1 im
// Takt, solange der Agent Verbindungsversuche macht, STOPPING=1 beim Beenden.
func TestRunMeldetSystemdUndFuettertAuchOhneVerbindung(t *testing.T) {
	path, conn := notifySocket(t)
	t.Setenv("NOTIFY_SOCKET", path)
	t.Setenv("WATCHDOG_USEC", "100000") // alle 50 ms
	t.Setenv("WATCHDOG_PID", "")

	// Eine Adresse, an der niemand lauscht.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	c := NewClient(&Config{URL: "http://" + addr, AgentID: "agent", Secret: "x"}, nil, "test").
		WithTiming(Timing{BackoffMin: 20 * time.Millisecond, BackoffMax: 100 * time.Millisecond})
	done := make(chan error, 1)
	go func() { done <- c.Run() }()

	if got := receive(conn, 2*time.Second); got != "READY=1" {
		t.Fatalf("erste meldung %q statt READY=1", got)
	}
	for i := range 3 {
		if got := receive(conn, 2*time.Second); got != "WATCHDOG=1" {
			t.Fatalf("meldung %d ohne verbindung: %q statt WATCHDOG=1", i, got)
		}
	}
	c.Stop()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run nach Stop: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run kehrt nach Stop nicht zurück")
	}
	for range 10 { // übrige WATCHDOG-Meldungen überspringen
		if receive(conn, 500*time.Millisecond) == "STOPPING=1" {
			return
		}
	}
	t.Fatal("kein STOPPING=1 beim Beenden")
}

// TestWatchdogRichtetSichSelbstEin: Ohne WatchdogSec= in der Unit meldet der
// Agent systemd seine eigene Frist und füttert in einem Drittel davon; vor dem
// Wechsel auf ein anderes Binary hebt er sie wieder auf.
func TestWatchdogRichtetSichSelbstEin(t *testing.T) {
	path, conn := notifySocket(t)
	t.Setenv("WATCHDOG_USEC", "")
	c := NewClient(&Config{AgentID: "a"}, nil, "test")
	c.notify = &notifier{socket: path}

	every, ok := c.watchdog()
	if !ok || every != runtimeWatchdog/3 {
		t.Fatalf("takt (%s, %v), erwartet (%s, true)", every, ok, runtimeWatchdog/3)
	}
	if got := receive(conn, time.Second); got != "WATCHDOG_USEC=90000000" {
		t.Fatalf("meldung %q statt WATCHDOG_USEC=90000000", got)
	}
	c.disarmWatchdog()
	if got := receive(conn, time.Second); got != "WATCHDOG_USEC=0" {
		t.Fatalf("meldung %q statt WATCHDOG_USEC=0", got)
	}
}

// TestWatchdogOhneSystemd: Ohne NOTIFY_SOCKET gibt es keinen Wächter.
func TestWatchdogOhneSystemd(t *testing.T) {
	t.Setenv("WATCHDOG_USEC", "")
	c := NewClient(&Config{AgentID: "a"}, nil, "test")
	if _, ok := c.watchdog(); ok {
		t.Fatal("wächter ohne systemd")
	}
}
