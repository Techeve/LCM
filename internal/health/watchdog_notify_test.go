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
// fest: Das Lebenszeichen meldet, dass dieser Prozess noch läuft - nicht, dass
// die Datenbank antwortet.
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
	m.ping()
	m.tick()
	m.ping()

	// Mindestens eines je Durchgang - der erste Fehlschlag schickt zusätzlich
	// sofort eines hinaus, weil er ein Zustandswechsel ist.
	msgs := collect()
	if len(msgs) < 2 {
		t.Fatalf("%d Lebenszeichen bei zwei Fehlschlägen, erwartet mindestens 2: %q", len(msgs), msgs)
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

// TestHaengendePruefungIstEinBefund: Eine Prüfung, die nicht zurückkehrt, war
// früher schlicht Stille - und Stille beantwortet systemd mit SIGKILL. Genau
// das ist auf dem LCM-Host jede Nacht passiert, wenn der CVE-Scan die Maschine
// ausgehungert hat.
//
// Jetzt läuft die Prüfung in ein eigenes Zeitlimit und wird damit zu einem
// Befund: Der Dienst meldet weiter, sagt aber „degraded", und über den
// Neustart entscheidet er selbst - kontrolliert und mit Begründung.
func TestHaengendePruefungIstEinBefund(t *testing.T) {
	safego.Reset()
	_, collect := notifyListener(t)

	alt := checkTimeout
	checkTimeout = 20 * time.Millisecond
	t.Cleanup(func() { checkTimeout = alt })

	blocked := make(chan struct{})
	t.Cleanup(func() { close(blocked) })

	var reasons []string
	m := NewMonitor(func() error { <-blocked; return nil }).
		WithRestart(func(r string) { reasons = append(reasons, r) })

	for i := 1; i < unhealthyLimit; i++ {
		m.tick()
		m.ping()
		if len(reasons) != 0 {
			t.Fatalf("zu früh neu gestartet (nach %d hängenden Prüfungen)", i)
		}
	}

	msgs := collect()
	if len(msgs) < unhealthyLimit-1 {
		t.Fatalf("%d Lebenszeichen bei hängender Prüfung, erwartet mindestens %d: %q",
			len(msgs), unhealthyLimit-1, msgs)
	}
	for _, msg := range msgs {
		if !strings.Contains(msg, "degraded:") {
			t.Errorf("hängende Prüfung muss als Störung gemeldet werden: %q", msg)
		}
	}

	// Der Tick, der die Grenze erreicht: jetzt - und erst jetzt - der Neustart.
	m.tick()
	if len(reasons) != 1 {
		t.Fatalf("erwartete genau einen Neustart, bekam %d", len(reasons))
	}
	if !strings.Contains(reasons[0], "health check") {
		t.Errorf("Neustart-Begründung nennt die Ursache nicht: %q", reasons[0])
	}
}

// TestLebenszeichenImNormalbetrieb: die Gegenprobe - läuft alles, meldet der
// Status genau das.
func TestLebenszeichenImNormalbetrieb(t *testing.T) {
	safego.Reset()
	_, collect := notifyListener(t)

	m := NewMonitor(func() error { return nil }).WithRestart(func(string) {})
	m.tick()
	m.ping()

	msgs := collect()
	if len(msgs) != 1 || !strings.Contains(msgs[0], "STATUS=operational") {
		t.Errorf("erwartet ein Lebenszeichen mit STATUS=operational, bekam %q", msgs)
	}
}

// TestZustandswechselGehtSofortHinaus: Die Trennung von Prüfung und
// Lebenszeichen hat einen Preis - beide ticken unabhängig, also könnte der
// Ping noch den Befund des vorigen Durchgangs tragen. Im Testlauf auf der
// Testumgebung stand deshalb fast eine Minute lang „operational" in
// `systemctl status`, obwohl die Datenbank längst gesperrt war.
//
// Ein WECHSEL des Zustands geht deshalb sofort hinaus, ohne den nächsten
// Ping-Takt abzuwarten.
func TestZustandswechselGehtSofortHinaus(t *testing.T) {
	safego.Reset()
	_, collect := notifyListener(t)

	kaputt := errors.New("datenbank gesperrt")
	var fehler error
	m := NewMonitor(func() error { return fehler }).WithRestart(func(string) {})

	// Erster Durchgang gesund: kein Wechsel (Nullwert bleibt Nullwert).
	m.tick()
	if msgs := collect(); len(msgs) != 0 {
		t.Fatalf("ohne Zustandswechsel darf nichts hinausgehen: %q", msgs)
	}

	// Jetzt die Störung - der Wechsel muss sofort gemeldet werden.
	fehler = kaputt
	m.tick()
	msgs := collect()
	if len(msgs) != 1 {
		t.Fatalf("Zustandswechsel muss genau ein Lebenszeichen auslösen, waren %d: %q", len(msgs), msgs)
	}
	if !strings.Contains(msgs[0], "degraded: datenbank gesperrt") {
		t.Errorf("die Meldung nennt die Störung nicht: %q", msgs[0])
	}

	// Bleibt es dabei, wird nicht erneut gedrängelt - das erledigt der Takt.
	m.tick()
	if msgs := collect(); len(msgs) != 0 {
		t.Errorf("unveränderter Zustand darf kein zusätzliches Lebenszeichen auslösen: %q", msgs)
	}

	// Und die Rückkehr ist ebenfalls ein Wechsel.
	fehler = nil
	m.tick()
	msgs = collect()
	if len(msgs) != 1 || !strings.Contains(msgs[0], "operational") {
		t.Errorf("die Erholung muss sofort gemeldet werden: %q", msgs)
	}
}
