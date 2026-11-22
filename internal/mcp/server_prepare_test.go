package mcp_test

import (
	"net"
	"strconv"
	"testing"
	"time"

	"LCM/internal/mcp"
)

func newPrepareServer(t *testing.T) *mcp.Server {
	t.Helper()
	server := newServer()
	t.Cleanup(server.Shutdown)
	return server
}

func answers(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for range 100 {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal(what)
}

// Der Wechsel auf einen neuen Port: bis commit läuft der alte, danach der neue.
func TestPreparePortWechsel(t *testing.T) {
	server := newPrepareServer(t)
	oldAddr := net.JoinHostPort("127.0.0.1", strconv.Itoa(freePort(t)))
	_ = server.Apply(true, "127.0.0.1", mustAtoi(oldAddr), false)
	waitFor(t, "alter Port", func() bool { return answers(oldAddr) })

	newPort := freePort(t)
	newAddr := net.JoinHostPort("127.0.0.1", strconv.Itoa(newPort))
	commit, _, err := server.Prepare(true, "127.0.0.1", newPort, false)
	if err != nil {
		t.Fatal(err)
	}
	if !answers(oldAddr) {
		t.Error("der alte Port ist vor commit schon zu")
	}
	commit()
	waitFor(t, "neuer Port", func() bool { return answers(newAddr) })
	waitFor(t, "alter Port schließt", func() bool { return !answers(oldAddr) })
}

// Ist der neue Port belegt, läuft der alte weiter und der Fehler kommt zurück.
func TestPrepareBelegterPortBehaeltDenAlten(t *testing.T) {
	server := newPrepareServer(t)
	oldPort := freePort(t)
	oldAddr := net.JoinHostPort("127.0.0.1", strconv.Itoa(oldPort))
	_ = server.Apply(true, "127.0.0.1", oldPort, false)
	waitFor(t, "alter Port", func() bool { return answers(oldAddr) })

	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = taken.Close() }()

	if _, _, err := server.Prepare(true, "127.0.0.1", taken.Addr().(*net.TCPAddr).Port, false); err == nil {
		t.Fatal("kein Fehler für einen belegten Port")
	}
	if !answers(oldAddr) {
		t.Error("der alte Listener wurde trotz des Fehlers gestoppt")
	}
	// Apply tut dasselbe und meldet nur im Log.
	_ = server.Apply(true, "127.0.0.1", taken.Addr().(*net.TCPAddr).Port, false)
	if !answers(oldAddr) {
		t.Error("Apply hat den alten Listener gestoppt")
	}
}

func TestPrepareAbortGibtDenPortFrei(t *testing.T) {
	server := newPrepareServer(t)
	port := freePort(t)
	_, abort, err := server.Prepare(true, "127.0.0.1", port, false)
	if err != nil {
		t.Fatal(err)
	}
	abort()
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		t.Fatalf("Port nach abort nicht frei: %v", err)
	}
	_ = ln.Close()
}

func TestPrepareAusSchaltetAb(t *testing.T) {
	server := newPrepareServer(t)
	port := freePort(t)
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	_ = server.Apply(true, "127.0.0.1", port, false)
	waitFor(t, "Listener", func() bool { return answers(addr) })

	commit, _, err := server.Prepare(false, "127.0.0.1", port, false)
	if err != nil {
		t.Fatal(err)
	}
	commit()
	waitFor(t, "Listener schließt", func() bool { return !answers(addr) })
}

func mustAtoi(addr string) int {
	_, port, _ := net.SplitHostPort(addr)
	n, _ := strconv.Atoi(port)
	return n
}
