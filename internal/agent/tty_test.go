package agent

import (
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"LCM/internal/remote/wire"
)

// fakeShell spiegelt jede Eingabe als Ausgabe zurück (wie ein Echo).
type fakeShell struct {
	r       *io.PipeReader
	w       *io.PipeWriter
	mu      sync.Mutex
	size    [2]int
	closed  bool
	written strings.Builder
}

func newFakeShell(cols, rows int) *fakeShell {
	r, w := io.Pipe()
	return &fakeShell{r: r, w: w, size: [2]int{cols, rows}}
}

func (f *fakeShell) Read(b []byte) (int, error) { return f.r.Read(b) }
func (f *fakeShell) Write(b []byte) (int, error) {
	f.mu.Lock()
	f.written.Write(b)
	f.mu.Unlock()
	return f.w.Write(b)
}
func (f *fakeShell) Resize(cols, rows int) error {
	f.mu.Lock()
	f.size = [2]int{cols, rows}
	f.mu.Unlock()
	return nil
}
func (f *fakeShell) Close() error {
	f.mu.Lock()
	f.closed = true
	f.mu.Unlock()
	return f.w.Close()
}

// ttyHarness sammelt die Rahmen, die der Agent an LCM schickt.
type ttyHarness struct {
	mu       sync.Mutex
	frames   []wire.TTYFrame
	disabled bool
	shells   []*fakeShell
	ts       *terminals
}

func newTTYHarness(t *testing.T) *ttyHarness {
	h := &ttyHarness{}
	h.ts = newTerminals(func(f wire.TTYFrame, _ bool) {
		h.mu.Lock()
		h.frames = append(h.frames, f)
		h.mu.Unlock()
	}, func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		return h.disabled
	}, slog.New(slog.DiscardHandler))
	old := startShell
	startShell = func(_ string, cols, rows int) (shellProcess, error) {
		s := newFakeShell(cols, rows)
		h.mu.Lock()
		h.shells = append(h.shells, s)
		h.mu.Unlock()
		return s, nil
	}
	t.Cleanup(func() { startShell = old })
	return h
}

// waitFor wartet, bis ein Rahmen der Sitzung die Bedingung erfüllt.
func (h *ttyHarness) waitFor(t *testing.T, session string, match func(wire.TTYFrame) bool) wire.TTYFrame {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		for _, f := range h.frames {
			if f.Session == session && match(f) {
				h.mu.Unlock()
				return f
			}
		}
		h.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("rahmen für %s nicht gekommen; bisher: %+v", session, h.frames)
	return wire.TTYFrame{}
}

func op(name string) func(wire.TTYFrame) bool {
	return func(f wire.TTYFrame) bool { return f.Op == name }
}

// Rahmen kommen ungeordnet an; der Agent öffnet trotzdem zuerst, schreibt in
// Reihe und spiegelt die Ausgabe nummeriert zurück.
func TestTerminalOeffnetSchreibtSchliesst(t *testing.T) {
	h := newTTYHarness(t)
	h.ts.handle(wire.TTYFrame{Session: "s1", Seq: 2, Op: wire.TTYData, Data: []byte("ls\n")})
	h.ts.handle(wire.TTYFrame{Session: "s1", Seq: 3, Op: wire.TTYResize, Cols: 120, Rows: 40})
	h.ts.handle(wire.TTYFrame{Session: "s1", Seq: 1, Op: wire.TTYOpen, Cols: 80, Rows: 24})

	opened := h.waitFor(t, "s1", op(wire.TTYOpened))
	if opened.Seq != 1 {
		t.Errorf("opened mit nummer %d, erwartet 1", opened.Seq)
	}
	data := h.waitFor(t, "s1", op(wire.TTYData))
	if string(data.Data) != "ls\n" {
		t.Errorf("ausgabe %q, erwartet ls", data.Data)
	}
	shell := h.shells[0]
	shell.mu.Lock()
	size := shell.size
	shell.mu.Unlock()
	if size != [2]int{120, 40} {
		t.Errorf("fenstergröße %v, erwartet 120x40", size)
	}

	h.ts.handle(wire.TTYFrame{Session: "s1", Seq: 4, Op: wire.TTYClose})
	closed := h.waitFor(t, "s1", op(wire.TTYClosed))
	if closed.Error != "" {
		t.Errorf("geschlossen mit fehler %q", closed.Error)
	}
	if !shell.closed {
		t.Error("shell nicht beendet")
	}
	// Ein verspäteter Rahmen öffnet keine neue Sitzung gleichen Namens.
	h.ts.handle(wire.TTYFrame{Session: "s1", Seq: 1, Op: wire.TTYOpen})
	if len(h.shells) != 1 {
		t.Errorf("%d shells gestartet, erwartet 1", len(h.shells))
	}
}

// Hat der Betreiber die Konsole abgeschaltet, startet keine Shell.
func TestTerminalAbgeschaltet(t *testing.T) {
	h := newTTYHarness(t)
	h.disabled = true
	if caps := h.ts.capabilities(); len(caps) != 1 || caps[0] != wire.CapabilityTTYOff {
		t.Errorf("fähigkeiten %v, erwartet tty-off", caps)
	}
	h.ts.handle(wire.TTYFrame{Session: "s1", Seq: 1, Op: wire.TTYOpen})
	closed := h.waitFor(t, "s1", op(wire.TTYClosed))
	if !strings.Contains(closed.Error, "abgeschaltet") {
		t.Errorf("grund %q", closed.Error)
	}
	if len(h.shells) != 0 {
		t.Error("shell trotz abschaltung gestartet")
	}
}

// Mehr als zwei Sitzungen weist der Agent ab; Verbindungsverlust beendet alle.
func TestTerminalGrenzeUndVerbindungsverlust(t *testing.T) {
	h := newTTYHarness(t)
	for _, id := range []string{"a", "b", "c"} {
		h.ts.handle(wire.TTYFrame{Session: id, Seq: 1, Op: wire.TTYOpen})
	}
	h.waitFor(t, "a", op(wire.TTYOpened))
	h.waitFor(t, "b", op(wire.TTYOpened))
	if refused := h.waitFor(t, "c", op(wire.TTYClosed)); refused.Error == "" {
		t.Error("dritte sitzung ohne grund abgewiesen")
	}
	h.ts.closeAll(io.ErrUnexpectedEOF)
	h.waitFor(t, "a", op(wire.TTYClosed))
	h.waitFor(t, "b", op(wire.TTYClosed))
	for _, s := range h.shells {
		if !s.closed {
			t.Error("shell nach verbindungsverlust nicht beendet")
		}
	}
}

// blockingShell liest nie: Jeder Schreibversuch hängt, bis Close kommt - wie
// eine Shell mit `sleep` im Vordergrund und einem großen Einfügen.
type blockingShell struct {
	*fakeShell
	release chan struct{}
}

func (b *blockingShell) Write([]byte) (int, error) {
	<-b.release
	return 0, io.ErrClosedPipe
}

func (b *blockingShell) Close() error {
	close(b.release)
	return b.fakeShell.Close()
}

// Ein hängender Schreibvorgang hält das Schließen nicht auf.
func TestTerminalSchliesstTrotzHaengenderEingabe(t *testing.T) {
	h := newTTYHarness(t)
	startShell = func(_ string, cols, rows int) (shellProcess, error) {
		return &blockingShell{fakeShell: newFakeShell(cols, rows), release: make(chan struct{})}, nil
	}
	h.ts.handle(wire.TTYFrame{Session: "s1", Seq: 1, Op: wire.TTYOpen})
	h.waitFor(t, "s1", op(wire.TTYOpened))
	go h.ts.handle(wire.TTYFrame{Session: "s1", Seq: 2, Op: wire.TTYData, Data: []byte("viel text")})
	time.Sleep(50 * time.Millisecond)
	h.ts.handle(wire.TTYFrame{Session: "s1", Seq: 3, Op: wire.TTYClose})
	h.waitFor(t, "s1", op(wire.TTYClosed))
}

// lcm-agent terminal off beendet auch eine offene Konsole bei der nächsten Eingabe.
func TestTerminalAbschaltenBeendetOffeneSitzung(t *testing.T) {
	h := newTTYHarness(t)
	h.ts.handle(wire.TTYFrame{Session: "s1", Seq: 1, Op: wire.TTYOpen})
	h.waitFor(t, "s1", op(wire.TTYOpened))
	h.mu.Lock()
	h.disabled = true
	h.mu.Unlock()
	h.ts.handle(wire.TTYFrame{Session: "s1", Seq: 2, Op: wire.TTYData, Data: []byte("id\n")})
	closed := h.waitFor(t, "s1", op(wire.TTYClosed))
	if !strings.Contains(closed.Error, "abgeschaltet") {
		t.Errorf("grund %q", closed.Error)
	}
}

// Der Schalter ist eine eigene Datei; im Zweifel gilt die Konsole als aus.
func TestTerminalSchalterDatei(t *testing.T) {
	dir := t.TempDir()
	if TerminalDisabled(dir) {
		t.Fatal("ohne datei abgeschaltet")
	}
	if err := SetTerminal(dir, false); err != nil {
		t.Fatal(err)
	}
	if !TerminalDisabled(dir) {
		t.Fatal("terminal off wirkt nicht")
	}
	if err := SetTerminal(dir, true); err != nil {
		t.Fatal(err)
	}
	if TerminalDisabled(dir) {
		t.Fatal("terminal on wirkt nicht")
	}
	if err := SetTerminal(dir, true); err != nil {
		t.Fatalf("zweimal on: %v", err)
	}
}
