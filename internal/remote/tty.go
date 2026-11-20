package remote

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"

	"github.com/google/uuid"

	"LCM/internal/infrastructure/sshx"
	"LCM/internal/remote/wire"
	"LCM/internal/safego"
)

// Web-Konsole über den Agent: agentConn erfüllt sshx.TerminalConn. Die
// Sitzung reist als TTYFrames über die Topics tty (zum Agent) und ttyo (vom
// Agent); Ticket, Berechtigung, Konsolen-Limit, Audit und Mitschnitt bleiben
// beim TerminalService - für ihn sieht die Sitzung aus wie eine über SSH.

// terminalOpenTimeout ist die Wartezeit auf die Shell des Agents.
const terminalOpenTimeout = 15 * time.Second

// terminalOpenRetry wiederholt den Öffnen-Rahmen, bis der Agent antwortet. Der
// Hub hält einen Agent schon beim Verbindungsaufbau für erreichbar, das
// tty-Topic abonniert der Agent aber erst danach - ein Öffnen in diesem
// Moment ginge sonst verloren. Wiederholungen verwirft der Agent an der
// Nummer.
const terminalOpenRetry = 2 * time.Second

// maxTerminalBacklog begrenzt den Bildschirm, der noch nicht gelesen wurde.
// Der Browser liest normalerweise sofort; staut es sich trotzdem so weit,
// hängt die Gegenseite, und die Sitzung endet statt LCM den Speicher zu füllen.
const maxTerminalBacklog = 8 * 1024 * 1024

var errTerminalBacklog = errors.New("konsole beendet: der browser liest die ausgabe nicht ab")

// agentTerminal ist eine Sitzung aus Sicht von LCM.
type agentTerminal struct {
	hub     *Hub
	agentID string
	id      string

	sendMu sync.Mutex // Nummer vergeben und senden in einem Zug
	out    wire.Counter
	// order hält Einreihen und Verarbeiten der Agent-Rahmen zusammen, falls
	// der Broker sie nebenläufig zustellt.
	order sync.Mutex

	mu      sync.Mutex
	in      wire.Sequencer
	backlog []byte
	ended   bool
	cause   error
	notify  chan struct{} // gepuffert (1): neuer Bildschirm oder Ende
	opened  chan error    // gepuffert (1): Shell läuft (nil) oder Absage
	sentEnd bool
	// closedHere: LCM hat selbst geschlossen - Lesen endet sofort, später
	// eintreffende Ausgabe wird verworfen.
	closedHere bool
}

// Terminal öffnet eine root-Shell auf dem Server des Agents.
func (c *agentConn) Terminal(term string, cols, rows int) (sshx.Terminal, error) {
	c.mu.Lock()
	closed, dead := c.closed, c.dead
	c.mu.Unlock()
	if dead {
		return nil, ErrAgentOffline
	}
	if closed {
		return nil, errors.New("verbindung geschlossen")
	}
	t := &agentTerminal{
		hub:     c.hub,
		agentID: c.agentID,
		id:      uuid.NewString(),
		notify:  make(chan struct{}, 1),
		opened:  make(chan error, 1),
	}
	c.hub.mu.Lock()
	c.hub.ttys[t.id] = t
	c.hub.mu.Unlock()

	open, err := t.frame(wire.TTYFrame{Op: wire.TTYOpen, Term: term, Cols: cols, Rows: rows})
	if err == nil {
		err = t.publish(open)
	}
	if err != nil {
		t.end(err)
		return nil, fmt.Errorf("konsole öffnen: %w", err)
	}
	timer := time.NewTimer(terminalOpenTimeout)
	defer timer.Stop()
	retry := time.NewTicker(terminalOpenRetry)
	defer retry.Stop()
	for {
		select {
		case err := <-t.opened:
			if err != nil {
				t.end(err)
				return nil, err
			}
			return t, nil
		case <-retry.C:
			_ = t.publish(open)
		case <-c.done:
			_ = t.Close()
			return nil, ErrAgentOffline
		case <-timer.C:
			_ = t.Close()
			return nil, fmt.Errorf("der agent hat die konsole nicht binnen %s geöffnet", terminalOpenTimeout)
		}
	}
}

// frame nummeriert einen Rahmen und kodiert ihn, ohne ihn zu senden (für den
// wiederholten Öffnen-Rahmen).
func (t *agentTerminal) frame(f wire.TTYFrame) ([]byte, error) {
	t.sendMu.Lock()
	defer t.sendMu.Unlock()
	return t.encode(f)
}

// encode vergibt die nächste Nummer; der Aufrufer hält sendMu.
func (t *agentTerminal) encode(f wire.TTYFrame) ([]byte, error) {
	f.Session = t.id
	f.Seq = t.out.Next()
	return json.Marshal(f)
}

func (t *agentTerminal) publish(payload []byte) error {
	return t.hub.broker.Publish(wire.TopicTTY(t.agentID), payload, false, 1)
}

// send nummeriert einen Rahmen und schickt ihn an den Agent. Nummer und
// Absenden in einem Zug, sonst könnten zwei Absender (Tastatur, Fenstergröße)
// die Nummern vertauscht abschicken.
func (t *agentTerminal) send(f wire.TTYFrame) error {
	t.sendMu.Lock()
	defer t.sendMu.Unlock()
	payload, err := t.encode(f)
	if err != nil {
		return err
	}
	return t.publish(payload)
}

// Read liefert den Bildschirm; io.EOF, sobald die Sitzung zu Ende ist und
// alles gelesen wurde.
func (t *agentTerminal) Read(p []byte) (int, error) {
	for {
		t.mu.Lock()
		if t.closedHere {
			t.mu.Unlock()
			return 0, io.EOF
		}
		if len(t.backlog) > 0 {
			n := copy(p, t.backlog)
			t.backlog = t.backlog[n:]
			t.mu.Unlock()
			return n, nil
		}
		ended := t.ended
		t.mu.Unlock()
		if ended {
			return 0, io.EOF
		}
		<-t.notify
	}
}

// Write schickt Tastatureingaben, in Rahmen von höchstens MaxTTYChunk.
func (t *agentTerminal) Write(p []byte) (int, error) {
	if t.isEnded() {
		return 0, io.ErrClosedPipe
	}
	for sent := 0; sent < len(p); {
		n := min(len(p)-sent, wire.MaxTTYChunk)
		if err := t.send(wire.TTYFrame{Op: wire.TTYData, Data: p[sent : sent+n]}); err != nil {
			return sent, err
		}
		sent += n
	}
	return len(p), nil
}

// Resize meldet dem Agent die neue Fenstergröße.
func (t *agentTerminal) Resize(cols, rows int) error {
	if t.isEnded() {
		return io.ErrClosedPipe
	}
	return t.send(wire.TTYFrame{Op: wire.TTYResize, Cols: cols, Rows: rows})
}

// Close beendet die Sitzung; der Agent beendet daraufhin die Shell.
func (t *agentTerminal) Close() error {
	t.mu.Lock()
	tell := !t.sentEnd && !t.ended
	t.sentEnd = true
	// Wer selbst schließt, will nichts mehr lesen. Endet die Shell dagegen
	// beim Agent, bleibt ihre letzte Ausgabe (etwa „logout") lesbar.
	t.closedHere = true
	t.backlog = nil
	t.mu.Unlock()
	if tell {
		_ = t.send(wire.TTYFrame{Op: wire.TTYClose})
	}
	t.end(nil)
	return nil
}

func (t *agentTerminal) isEnded() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.ended
}

// end markiert die Sitzung als beendet, trägt sie aus und weckt Leser und den
// wartenden Öffner. Mehrfacher Aufruf schadet nicht.
func (t *agentTerminal) end(cause error) {
	t.mu.Lock()
	if t.ended {
		t.mu.Unlock()
		return
	}
	t.ended = true
	t.cause = cause
	t.mu.Unlock()
	t.hub.mu.Lock()
	delete(t.hub.ttys, t.id)
	t.hub.mu.Unlock()
	t.wake()
	if cause == nil {
		cause = errors.New("konsole beendet")
	}
	select {
	case t.opened <- cause:
	default:
	}
}

func (t *agentTerminal) wake() {
	select {
	case t.notify <- struct{}{}:
	default:
	}
}

// receive verarbeitet einen Rahmen des Agents (in Reihe gebracht).
func (t *agentTerminal) receive(f wire.TTYFrame) {
	t.order.Lock()
	defer t.order.Unlock()
	t.mu.Lock()
	ready, err := t.in.Push(f)
	t.mu.Unlock()
	if err != nil {
		t.end(err)
		return
	}
	for _, frame := range ready {
		switch frame.Op {
		case wire.TTYOpened:
			select {
			case t.opened <- nil:
			default:
			}
		case wire.TTYData:
			t.mu.Lock()
			if t.closedHere {
				t.mu.Unlock()
				return
			}
			t.backlog = append(t.backlog, frame.Data...)
			overflow := len(t.backlog) > maxTerminalBacklog
			t.mu.Unlock()
			if overflow {
				_ = t.send(wire.TTYFrame{Op: wire.TTYClose})
				t.end(errTerminalBacklog)
				return
			}
			t.wake()
		case wire.TTYClosed:
			var cause error
			if frame.Error != "" {
				cause = errors.New(frame.Error)
			}
			t.mu.Lock()
			t.sentEnd = true // der Agent hat schon beendet, kein close nötig
			t.mu.Unlock()
			t.end(cause)
			return
		}
	}
}

// handleTTY stellt einen Rahmen des Agents seiner Sitzung zu. Wie bei den
// Ergebnissen nur, wenn er vom Agent dieser Sitzung kommt: Ein Agent kann dank
// ACL nur auf sein eigenes ttyo-Topic schreiben, den Sitzungsnamen eines
// fremden aber raten.
func (h *Hub) handleTTY(_ *mqtt.Client, _ packets.Subscription, pk packets.Packet) {
	defer safego.Recover("mqtt:handle-tty", nil)
	var f wire.TTYFrame
	if err := json.Unmarshal(pk.Payload, &f); err != nil || f.Session == "" {
		return
	}
	agentID := wire.AgentIDFromTopic(pk.TopicName)
	h.mu.Lock()
	t := h.ttys[f.Session]
	h.mu.Unlock()
	if t == nil || t.agentID != agentID {
		return
	}
	t.receive(f)
}

// terminalsOf liefert die offenen Konsolen eines Agents; der Aufrufer hält h.mu.
func (h *Hub) terminalsOf(agentID string) []*agentTerminal {
	var out []*agentTerminal
	for _, t := range h.ttys {
		if t.agentID == agentID {
			out = append(out, t)
		}
	}
	return out
}
