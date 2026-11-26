package agent

import (
	"errors"
	"io"
	"log/slog"
	"sync"
	"time"

	"LCM/internal/remote/wire"
	"LCM/internal/safego"
)

// Web-Konsole über den Agent: LCM öffnet hier eine root-Shell in einem
// Pseudo-Terminal und reicht Tastatur und Bildschirm als TTYFrames durch
// (siehe wire/tty.go). Die Shell läuft außerhalb der Kommando-Warteschlange
// des Runners - eine offene Konsole hält keinen Job auf.

// maxTerminals begrenzt die gleichzeitigen Sitzungen je Agent. LCM erlaubt
// zwei Konsolen je Server; eine dritte hier wäre ein Fehler auf der
// Serverseite und wird abgewiesen.
const maxTerminals = 2

// terminalIdleTimeout beendet eine Sitzung ohne Tastatureingabe. LCM schließt
// nach 15 Minuten Stille selbst; das hier greift, wenn LCM das Schließen
// nicht mehr schicken konnte (Verbindung weg).
const terminalIdleTimeout = 30 * time.Minute

// terminalOpenWait gibt einen Sitzungsplatz wieder frei, wenn auf den ersten
// Rahmen kein „öffnen" folgt - sonst blockierte ein verirrter Rahmen einen der
// zwei Plätze auf Dauer.
const terminalOpenWait = 30 * time.Second

// ttyPublishWait ist die längste Wartezeit auf die Bestätigung eines
// Bildschirm-Rahmens. Das Warten drosselt das Lesen aus dem Terminal auf das
// Tempo der Verbindung: Ohne es füllte `yes` oder ein großes `cat` den
// Speicher des Agents schneller, als die Leitung abträgt.
const ttyPublishWait = 10 * time.Second

// errTerminalDisabled steht im closed-Rahmen, wenn der Betreiber die Konsole
// auf dem Server abgeschaltet hat.
var errTerminalDisabled = errors.New("die web-konsole ist auf diesem server abgeschaltet (lcm-agent terminal on schaltet sie ein)")

// shellProcess ist eine laufende Shell in einem Pseudo-Terminal: Lesen liefert
// den Bildschirm, Schreiben sind die Tasten. Close beendet die Shell samt
// Kindern und gibt ein hängendes Lesen oder Schreiben frei.
type shellProcess interface {
	io.ReadWriter
	Resize(cols, rows int) error
	Close() error
}

// startShell startet die Shell (tty_linux.go); in Tests ersetzt.
var startShell = startPTYShell

// terminals verwaltet die Sitzungen eines Agents.
type terminals struct {
	// publish schickt einen Rahmen an LCM; mit wait wartet es auf die
	// Bestätigung des Brokers (höchstens ttyPublishWait).
	publish  func(f wire.TTYFrame, wait bool)
	disabled func() bool // hat der Betreiber die Konsole abgeschaltet?
	log      *slog.Logger

	mu       sync.Mutex
	sessions map[string]*terminal
	// ended merkt sich beendete Sitzungen: Ein verspäteter Rahmen soll keine
	// neue Sitzung gleichen Namens öffnen.
	ended *idWindow
	// notice ist ein Hinweis, den jede neue Konsole nach dem Öffnen sieht (der
	// Wechsel aufs neue Binary steht an, switch.go). refusing weist neue ab: Der
	// Wechsel läuft schon.
	notice   string
	refusing string
}

func newTerminals(publish func(wire.TTYFrame, bool), disabled func() bool, log *slog.Logger) *terminals {
	return &terminals{publish: publish, disabled: disabled, log: log, sessions: map[string]*terminal{}, ended: newIDWindow(commandIDWindow)}
}

// terminal ist eine Sitzung. in setzt die Rahmen von LCM in Reihe, out
// nummeriert die eigenen.
type terminal struct {
	id    string
	owner *terminals

	// order hält Einreihen und Ausführen zusammen: paho ruft handle für
	// Rahmen derselben Sitzung nebenläufig auf, und ohne die Sperre könnte
	// der später eingereihte Rahmen vor dem früheren ausgeführt werden.
	order sync.Mutex
	// sendMu hält Nummer und Absenden zusammen (deliver).
	sendMu sync.Mutex

	mu     sync.Mutex
	in     wire.Sequencer
	out    wire.Counter
	shell  shellProcess
	closed bool
	idle   *time.Timer
}

// handle nimmt einen Rahmen von LCM an.
func (ts *terminals) handle(f wire.TTYFrame) {
	if f.Session == "" {
		return
	}
	t := ts.session(f.Session)
	if t == nil {
		return
	}
	// Schließen ohne Einreihen: Hängt ein Schreibvorgang in der Shell (die
	// gerade nichts liest), hält er die Reihe auf - das Schließen muss trotzdem
	// durchkommen, und Close gibt den hängenden Schreibvorgang frei.
	if f.Op == wire.TTYClose {
		t.finish(nil)
		return
	}
	t.order.Lock()
	defer t.order.Unlock()
	t.mu.Lock()
	ready, err := t.in.Push(f)
	t.mu.Unlock()
	if err != nil {
		t.finish(err)
		return
	}
	for _, frame := range ready {
		t.apply(frame)
	}
}

// session liefert die Sitzung zum Namen und legt sie beim ersten Rahmen an.
// Nil, wenn sie schon beendet ist oder schon zu viele offen sind (dann
// erfährt LCM es über einen closed-Rahmen).
func (ts *terminals) session(id string) *terminal {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if t := ts.sessions[id]; t != nil {
		return t
	}
	if ts.ended.has(id) {
		return nil
	}
	t := &terminal{id: id, owner: ts}
	if ts.refusing != "" || len(ts.sessions) >= maxTerminals {
		reason := "zu viele konsolen auf diesem server offen"
		if ts.refusing != "" {
			reason = ts.refusing
		}
		ts.ended.add(id)
		safego.Go("agent-tty-refuse", func() { t.send(wire.TTYFrame{Op: wire.TTYClosed, Error: reason}) })
		return nil
	}
	t.idle = time.AfterFunc(terminalOpenWait, func() { t.finish(errors.New("sitzung nicht geöffnet")) })
	ts.sessions[id] = t
	return t
}

// apply führt einen Rahmen in Reihe aus.
func (t *terminal) apply(f wire.TTYFrame) {
	// Ein Panic (etwa beim Start der Shell) beendet nur diese Sitzung.
	defer safego.Recover("agent-tty-frame", t.finish)
	switch f.Op {
	case wire.TTYOpen:
		t.open(f)
	case wire.TTYData:
		t.write(f.Data)
	case wire.TTYResize:
		t.mu.Lock()
		shell := t.shell
		t.mu.Unlock()
		if shell != nil {
			_ = shell.Resize(f.Cols, f.Rows)
		}
	case wire.TTYClose:
		t.finish(nil)
	}
}

func (t *terminal) open(f wire.TTYFrame) {
	if t.owner.disabled() {
		t.finish(errTerminalDisabled)
		return
	}
	shell, err := startShell(f.Term, f.Cols, f.Rows)
	if err != nil {
		t.finish(err)
		return
	}
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		_ = shell.Close()
		return
	}
	t.shell = shell
	t.idle.Stop()
	t.idle = time.AfterFunc(terminalIdleTimeout, func() { t.finish(errors.New("sitzung ohne eingabe beendet")) })
	t.mu.Unlock()
	t.owner.log.Info("terminal opened", "session", t.id)
	t.send(wire.TTYFrame{Op: wire.TTYOpened})
	if notice := t.owner.currentNotice(); notice != "" {
		t.send(noticeFrame(notice))
	}
	safego.Go("agent-tty-read:"+t.id, t.pump)
}

// pump reicht den Bildschirm an LCM, bis die Shell endet.
func (t *terminal) pump() {
	buf := make([]byte, wire.MaxTTYChunk)
	for {
		n, err := t.shell.Read(buf)
		if n > 0 {
			t.sendWait(wire.TTYFrame{Op: wire.TTYData, Data: append([]byte(nil), buf[:n]...)})
		}
		if err != nil {
			t.finish(nil)
			return
		}
	}
}

func (t *terminal) write(data []byte) {
	// Der Schalter gilt auch für offene Konsolen: Wer lcm-agent terminal off
	// setzt, will keine root-Shell mehr von außen, auch keine laufende.
	if t.owner.disabled() {
		t.finish(errTerminalDisabled)
		return
	}
	t.mu.Lock()
	shell := t.shell
	if shell != nil {
		t.idle.Reset(terminalIdleTimeout)
	}
	t.mu.Unlock()
	if shell != nil && len(data) > 0 {
		_, _ = shell.Write(data)
	}
}

// send nummeriert einen Rahmen und schickt ihn an LCM, ohne zu warten.
func (t *terminal) send(f wire.TTYFrame) { t.deliver(f, false) }

// sendWait wie send, wartet aber auf die Bestätigung (Drosselung der Ausgabe).
func (t *terminal) sendWait(f wire.TTYFrame) { t.deliver(f, true) }

// deliver nummeriert und sendet in einem Zug - sonst könnten zwei Absender die
// Nummern in anderer Reihenfolge abschicken, als sie sie bekommen haben.
func (t *terminal) deliver(f wire.TTYFrame, wait bool) {
	t.sendMu.Lock()
	defer t.sendMu.Unlock()
	t.mu.Lock()
	f.Session = t.id
	f.Seq = t.out.Next()
	t.mu.Unlock()
	t.owner.publish(f, wait)
}

// finish beendet die Sitzung einmalig: Shell beenden, LCM Bescheid geben,
// austragen. cause ist der Grund, falls es keiner auf Wunsch war.
func (t *terminal) finish(cause error) {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return
	}
	t.closed = true
	shell := t.shell
	if t.idle != nil {
		t.idle.Stop()
	}
	t.mu.Unlock()
	if shell != nil {
		_ = shell.Close()
	}
	closed := wire.TTYFrame{Op: wire.TTYClosed}
	if cause != nil {
		closed.Error = cause.Error()
	}
	t.send(closed)
	t.owner.forget(t.id)
	t.owner.log.Info("terminal closed", "session", t.id, "reason", closed.Error)
}

func (ts *terminals) forget(id string) {
	ts.mu.Lock()
	delete(ts.sessions, id)
	ts.ended.add(id)
	ts.mu.Unlock()
}

// closeAll beendet alle Sitzungen - die Verbindung zu LCM ist weg, und eine
// Shell ohne Gegenüber soll nicht weiterlaufen.
func (ts *terminals) closeAll(cause error) {
	ts.mu.Lock()
	open := make([]*terminal, 0, len(ts.sessions))
	for _, t := range ts.sessions {
		open = append(open, t)
	}
	ts.mu.Unlock()
	for _, t := range open {
		t.finish(cause)
	}
}

// capabilities meldet LCM, ob hier Konsolen möglich sind.
func (ts *terminals) capabilities() []string {
	if ts.disabled() {
		return []string{wire.CapabilityTTYOff}
	}
	return []string{wire.CapabilityTTY}
}

// count ist die Zahl der offenen (und gerade öffnenden) Konsolen.
func (ts *terminals) count() int {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return len(ts.sessions)
}

// noticeFrame ist eine Zeile Text im Bildschirmstrom der Konsole: Sie geht an den
// Browser, nicht in die Shell.
func noticeFrame(text string) wire.TTYFrame {
	return wire.TTYFrame{Op: wire.TTYData, Data: []byte("\r\n" + text + "\r\n")}
}

// setNotice merkt einen Hinweis für Konsolen, die erst jetzt öffnen, und zeigt
// ihn den offenen. (Die Reihenfolge der Rahmen ordnet deliver.)
func (ts *terminals) setNotice(text string) {
	ts.mu.Lock()
	ts.notice = text
	ts.mu.Unlock()
	ts.announce(text)
}

func (ts *terminals) currentNotice() string {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.notice
}

// announce schreibt eine Zeile in jede geöffnete Konsole.
func (ts *terminals) announce(text string) {
	ts.mu.Lock()
	open := make([]*terminal, 0, len(ts.sessions))
	for _, t := range ts.sessions {
		open = append(open, t)
	}
	ts.mu.Unlock()
	for _, t := range open {
		t.mu.Lock()
		running := t.shell != nil && !t.closed
		t.mu.Unlock()
		if running {
			t.send(noticeFrame(text))
		}
	}
}

// refuseNew weist neue Konsolen mit dem Grund ab (der Wechsel aufs neue Binary läuft).
func (ts *terminals) refuseNew(reason string) {
	ts.mu.Lock()
	ts.refusing = reason
	ts.mu.Unlock()
}
