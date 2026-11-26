package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"LCM/internal/remote/wire"
)

// TestBinaryWatchErkenntAustausch: Ein Paketmanager benennt die neue Datei
// über die alte - das erkennt der Agent, ein unveränderter Pfad nicht, eine
// entfernte Datei auch nicht.
func TestBinaryWatchErkenntAustausch(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "lcm-agent")
	if err := os.WriteFile(bin, []byte("alt"), 0o755); err != nil {
		t.Fatal(err)
	}
	w, err := newBinaryWatch(bin)
	if err != nil {
		t.Fatal(err)
	}
	if w.replaced() {
		t.Fatal("unverändertes binary gilt als ersetzt")
	}
	staged := filepath.Join(dir, "lcm-agent.dpkg-new")
	if err := os.WriteFile(staged, []byte("neu"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(staged, bin); err != nil {
		t.Fatal(err)
	}
	if !w.replaced() {
		t.Fatal("ausgetauschtes binary nicht erkannt")
	}
	if err := os.Remove(bin); err != nil {
		t.Fatal(err)
	}
	if w.replaced() {
		t.Fatal("entferntes binary darf keinen wechsel auslösen")
	}
}

// TestWorkStateWechseltNurInRuhe: Der Wechsel wartet, bis kein Kommando in
// Arbeit ist und das letzte lange genug zurückliegt - danach nimmt der Agent
// keines mehr an.
func TestWorkStateWechseltNurInRuhe(t *testing.T) {
	var s workState
	if !s.begin() {
		t.Fatal("kommando abgelehnt")
	}
	if s.claimIdle(0) {
		t.Fatal("wechsel trotz laufendem kommando")
	}
	s.end()
	if s.claimIdle(time.Hour) {
		t.Fatal("wechsel direkt nach dem letzten kommando")
	}
	if !s.claimIdle(0) {
		t.Fatal("kein wechsel in ruhe")
	}
	if s.begin() {
		t.Fatal("kommando während des wechsels angenommen")
	}
}

// replacedWatch liefert eine Beobachtung, deren Binary schon ausgetauscht wurde.
func replacedWatch(t *testing.T) *binaryWatch {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "lcm-agent")
	if err := os.WriteFile(bin, []byte("alt"), 0o755); err != nil {
		t.Fatal(err)
	}
	w, err := newBinaryWatch(bin)
	if err != nil {
		t.Fatal(err)
	}
	staged := bin + ".dpkg-new"
	if err := os.WriteFile(staged, []byte("neu"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(staged, bin); err != nil {
		t.Fatal(err)
	}
	return w
}

// noticeCount zählt die Hinweiszeilen des Agents in der Konsole der Sitzung.
func noticeCount(h *ttyHarness, session string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, f := range h.frames {
		if f.Session == session && f.Op == wire.TTYData && strings.Contains(string(f.Data), "[lcm-agent]") {
			n++
		}
	}
	return n
}

// TestWechselWartetAufOffeneKonsolen (D-5): Eine offene Web-Konsole hält den
// Wechsel auf - höchstens ConsoleWait - und erfährt es in der Konsole selbst.
func TestWechselWartetAufOffeneKonsolen(t *testing.T) {
	h := newTTYHarness(t)
	c, _ := testClient()
	c.tty = h.ts
	c.timing.ConsoleWait = 300 * time.Millisecond
	w := replacedWatch(t)

	h.ts.handle(wire.TTYFrame{Session: "s1", Seq: 1, Op: wire.TTYOpen, Cols: 80, Rows: 24})
	h.waitFor(t, "s1", op(wire.TTYOpened))
	if c.readyToSwitch(w) {
		t.Fatal("Wechsel trotz offener Konsole")
	}
	h.waitFor(t, "s1", func(f wire.TTYFrame) bool {
		return f.Op == wire.TTYData && strings.Contains(string(f.Data), "[lcm-agent]")
	})
	if c.readyToSwitch(w) || noticeCount(h, "s1") != 1 {
		t.Errorf("zweite Prüfung: Hinweis %d mal, erwartet einmal", noticeCount(h, "s1"))
	}

	// Eine Konsole, die erst jetzt öffnet, sieht den Hinweis gleich.
	h.ts.handle(wire.TTYFrame{Session: "s2", Seq: 1, Op: wire.TTYOpen, Cols: 80, Rows: 24})
	h.waitFor(t, "s2", func(f wire.TTYFrame) bool {
		return f.Op == wire.TTYData && strings.Contains(string(f.Data), "[lcm-agent]")
	})

	// Nach ConsoleWait wechselt er trotzdem - mit einem letzten Hinweis, und neue
	// Konsolen weist er ab.
	time.Sleep(c.timing.ConsoleWait)
	if !c.readyToSwitch(w) {
		t.Fatal("nach ConsoleWait wechselt der Agent nicht")
	}
	deadline := time.Now().Add(time.Second)
	for noticeCount(h, "s1") < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if noticeCount(h, "s1") != 2 {
		t.Errorf("Hinweis unmittelbar vor dem Wechsel fehlt (%d)", noticeCount(h, "s1"))
	}
	h.ts.handle(wire.TTYFrame{Session: "s3", Seq: 1, Op: wire.TTYOpen, Cols: 80, Rows: 24})
	closed := h.waitFor(t, "s3", op(wire.TTYClosed))
	if closed.Error != errSwitching {
		t.Errorf("neue Konsole beim Wechsel: %q", closed.Error)
	}
}

// TestWechselNachSchliessenDerKonsole: Ist die Konsole zu, geht der Wechsel sofort.
func TestWechselNachSchliessenDerKonsole(t *testing.T) {
	h := newTTYHarness(t)
	c, _ := testClient()
	c.tty = h.ts
	w := replacedWatch(t)
	h.ts.handle(wire.TTYFrame{Session: "s1", Seq: 1, Op: wire.TTYOpen, Cols: 80, Rows: 24})
	h.waitFor(t, "s1", op(wire.TTYOpened))
	if c.readyToSwitch(w) {
		t.Fatal("Wechsel trotz offener Konsole")
	}
	h.ts.handle(wire.TTYFrame{Session: "s1", Seq: 2, Op: wire.TTYClose})
	h.waitFor(t, "s1", op(wire.TTYClosed))
	if !c.readyToSwitch(w) {
		t.Fatal("Wechsel nach dem Schließen der Konsole bleibt aus")
	}
}

// TestWechselZiel: Das Binary des Pakets startet über das Startskript, alles
// andere direkt.
func TestWechselZiel(t *testing.T) {
	c, _ := testClient()
	c.packagedPath = "/usr/bin/lcm-agent"
	if target, _ := c.switchTarget("/usr/local/bin/lcm-agent"); target != "/usr/local/bin/lcm-agent" {
		t.Errorf("Selbstinstallation: %s", target)
	}
	// Das Startskript gibt es nur auf Systemen mit dem Paket (oder hier nicht).
	if guardInstalled() {
		if target, argv := c.switchTarget("/usr/bin/lcm-agent"); target != guardScriptPath || argv[0] != guardScriptPath {
			t.Errorf("Paket: %s %v", target, argv)
		}
	} else if target, _ := c.switchTarget("/usr/bin/lcm-agent"); target != "/usr/bin/lcm-agent" {
		t.Errorf("ohne Startskript: %s", target)
	}
}

// TestWatchedBinary: Läuft der Agent als Rückfall, zählt das Binary des Pakets.
func TestWatchedBinary(t *testing.T) {
	c, _ := testClient()
	c.packagedPath = "/usr/bin/lcm-agent"
	if p, err := c.watchedBinary(); err != nil || p == "/usr/bin/lcm-agent" {
		t.Errorf("normal: %q %v", p, err)
	}
	c.asPrev = true
	if p, _ := c.watchedBinary(); p != "/usr/bin/lcm-agent" {
		t.Errorf("als Rückfall: %q", p)
	}
}
