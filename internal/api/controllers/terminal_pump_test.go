package controllers

import (
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/fasthttp/websocket"
)

// fakeWS ist ein WebSocket, dessen Eingabe der Test steuert: ReadMessage
// liefert, was in input ankommt, und scheitert, sobald input zu ist oder die
// Lesefrist abgelaufen ist.
//
// Close weckt ein wartendes ReadMessage bewusst NICHT: Auf der echten
// Verbindung (fasthttp, TLS) tat es das im Live-Test nicht - die Konsole blieb
// nach `exit` offen. Nur die abgelaufene Lesefrist weckt zuverlässig.
type fakeWS struct {
	input      chan []byte
	expired    chan struct{}
	closed     chan struct{}
	closeOnce  sync.Once
	expireOnce sync.Once
	mu         sync.Mutex
	closeSent  bool
}

func newFakeWS() *fakeWS {
	return &fakeWS{input: make(chan []byte), expired: make(chan struct{}), closed: make(chan struct{})}
}

func (w *fakeWS) ReadMessage() (int, []byte, error) {
	select {
	case data, ok := <-w.input:
		if !ok {
			return 0, nil, errors.New("browser weg")
		}
		return 2, data, nil
	case <-w.expired:
		return 0, nil, errors.New("lesefrist abgelaufen")
	}
}

func (w *fakeWS) WriteMessage(kind int, _ []byte) error {
	if kind == websocket.CloseMessage {
		w.mu.Lock()
		w.closeSent = true
		w.mu.Unlock()
	}
	return nil
}

func (w *fakeWS) SetReadDeadline(t time.Time) error {
	if !t.After(time.Now()) {
		w.expireOnce.Do(func() { close(w.expired) })
	}
	return nil
}

func (w *fakeWS) Close() error {
	w.closeOnce.Do(func() { close(w.closed) })
	return nil
}

// fakeTerm ist eine ruhige Shell: Read blockiert, bis die Sitzung
// geschlossen wird oder exit ankommt.
type fakeTerm struct {
	exit      chan struct{}
	closed    chan struct{}
	closeOnce sync.Once
}

func newFakeTerm() *fakeTerm {
	return &fakeTerm{exit: make(chan struct{}), closed: make(chan struct{})}
}

func (t *fakeTerm) Read([]byte) (int, error) {
	select {
	case <-t.exit:
	case <-t.closed:
	}
	return 0, io.EOF
}

func (t *fakeTerm) Write(p []byte) (int, error) { return len(p), nil }
func (t *fakeTerm) Resize(int, int) error       { return nil }
func (t *fakeTerm) Close() error {
	t.closeOnce.Do(func() { close(t.closed) })
	return nil
}

func (t *fakeTerm) isClosed() bool {
	select {
	case <-t.closed:
		return true
	default:
		return false
	}
}

// runPump startet pump und meldet, ob es binnen einer Sekunde endet.
func runPump(t *testing.T, ws *fakeWS, term *fakeTerm, end func()) {
	t.Helper()
	finished := make(chan struct{})
	go func() {
		pump(ws, term)
		close(finished)
	}()
	ws.input <- []byte("ls\n") // die Sitzung läuft
	end()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("pump endet nicht - die sitzung bliebe offen")
	}
}

// TestPumpClosesWhenBrowserLeaves: „Beenden" (oder die Leerlauf-Frist, die
// ReadMessage genauso scheitern lässt) schließt die Shell, auch wenn sie
// gerade nichts ausgibt.
func TestPumpClosesWhenBrowserLeaves(t *testing.T) {
	ws, term := newFakeWS(), newFakeTerm()
	runPump(t, ws, term, func() { close(ws.input) })
	if !term.isClosed() {
		t.Error("sitzung nicht geschlossen")
	}
}

// TestPumpClosesWhenShellExits: Endet die Shell, wird auch der WebSocket
// geschlossen - die Eingabe wartet nicht auf den nächsten Tastendruck.
func TestPumpClosesWhenShellExits(t *testing.T) {
	ws, term := newFakeWS(), newFakeTerm()
	runPump(t, ws, term, func() { close(term.exit) })
	select {
	case <-ws.closed:
	default:
		t.Error("websocket nach exit nicht geschlossen")
	}
	ws.mu.Lock()
	sent := ws.closeSent
	ws.mu.Unlock()
	if !sent {
		t.Error("dem Browser wurde kein Close gemeldet")
	}
	if !term.isClosed() {
		t.Error("sitzung nicht geschlossen")
	}
}
