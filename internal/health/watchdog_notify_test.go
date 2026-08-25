package health

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"LCM/internal/safego"
)

// notifyListener fängt die Meldungen an systemd ab. Der Socket liegt bewusst
// unter /tmp und nicht in t.TempDir(): Unix-Socket-Pfade sind auf rund 100
// Zeichen begrenzt, und der Pfad von t.TempDir() reißt diese Grenze auf macOS.
func notifyListener(t *testing.T) (*net.UnixConn, func() []string) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "lcmwd")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	addr := &net.UnixAddr{Name: filepath.Join(dir, "n.sock"), Net: "unixgram"}
	conn, err := net.ListenUnixgram("unixgram", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	t.Setenv("NOTIFY_SOCKET", addr.Name)

	return conn, func() []string {
		var out []string
		buf := make([]byte, 4096)
		for {
			if err := conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
				return out
			}
			n, err := conn.Read(buf)
			if err != nil {
				return out
			}
			out = append(out, string(buf[:n]))
		}
	}
}

// TestLebenszeichenHaengtNichtAmDatenbankPing hält den Kern der Selbstaufsicht
// fest: Das Lebenszeichen meldet, dass dieser Ablauf noch vorankommt - nicht,
// dass die Datenbank antwortet.
//
// Andersherum wäre die Toleranz aus unhealthyLimit wertlos: Der Ping-Takt ist
// WatchdogSec/2, zwei ausgefallene Pings genügen systemd. Wer beim ersten
// Fehlschlag schweigt, wird nach 90 Sekunden abgeräumt und kommt nie bis zur
// eigenen Neustart-Entscheidung.
func TestLebenszeichenHaengtNichtAmDatenbankPing(t *testing.T) {
	safego.Reset()
	_, collect := notifyListener(t)

	var restarts int
	m := NewMonitor(func() error { return errors.New("datenbank gesperrt") }).
		WithRestart(func(string) { restarts++ })

	// Zwei Fehlschläge - der Bereich, in dem systemd bereits zugeschlagen
	// hätte, wenn das Lebenszeichen ausbliebe.
	m.tick()
	m.tick()

	msgs := collect()
	if len(msgs) != 2 {
		t.Fatalf("%d Lebenszeichen bei zwei Fehlschlägen, erwartet 2: %q", len(msgs), msgs)
	}
	for _, msg := range msgs {
		if !strings.HasPrefix(msg, "WATCHDOG=1") {
			t.Errorf("keine Watchdog-Meldung: %q", msg)
		}
		if !strings.Contains(msg, "degraded: datenbank gesperrt") {
			t.Errorf("der Status nennt die Störung nicht: %q", msg)
		}
	}
	if restarts != 0 {
		t.Errorf("nach zwei Fehlschlägen bereits %d Neustarts - unhealthyLimit ist %d", restarts, unhealthyLimit)
	}
}

// TestLebenszeichenBleibtBeiHaengenderPruefungAus: Der Fall, für den der
// Watchdog überhaupt da ist. Kommt die Prüfung nicht zurück, kommt auch das
// Lebenszeichen nicht - ein blockierter Prozess stürzt nicht ab und wäre
// sonst von niemandem zu bemerken.
func TestLebenszeichenBleibtBeiHaengenderPruefungAus(t *testing.T) {
	safego.Reset()
	_, collect := notifyListener(t)

	blocked := make(chan struct{})
	m := NewMonitor(func() error { <-blocked; return nil }).
		WithRestart(func(string) {})

	done := make(chan struct{})
	go func() { m.tick(); close(done) }()

	if msgs := collect(); len(msgs) != 0 {
		t.Errorf("Lebenszeichen trotz hängender Prüfung: %q", msgs)
	}
	close(blocked)
	<-done
}

// TestLebenszeichenImNormalbetrieb: die Gegenprobe - läuft alles, meldet der
// Status genau das.
func TestLebenszeichenImNormalbetrieb(t *testing.T) {
	safego.Reset()
	_, collect := notifyListener(t)

	NewMonitor(func() error { return nil }).WithRestart(func(string) {}).tick()

	msgs := collect()
	if len(msgs) != 1 || !strings.Contains(msgs[0], "STATUS=operational") {
		t.Errorf("erwartet ein Lebenszeichen mit STATUS=operational, bekam %q", msgs)
	}
}
