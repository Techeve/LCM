package wire

import "errors"

// Web-Konsole über den Agent. Ein Terminal ist ein Strom in beide Richtungen,
// MQTT liefert aber einzelne Nachrichten - und der Agent verarbeitet sie
// nebenläufig (paho mit OrderMatters=false), die Reihenfolge ist also nicht
// garantiert. Jede Richtung einer Sitzung zählt deshalb ihre Rahmen ab 1, und
// die Gegenseite setzt sie mit einem Sequencer wieder in Reihe. QoS 1 stellt
// mindestens einmal zu; eine Wiederholung erkennt der Sequencer an der
// Nummer und verwirft sie.

// Arten eines TTYFrame.
const (
	TTYOpen   = "open"   // Server→Agent: Sitzung öffnen (Term, Cols, Rows)
	TTYData   = "data"   // beide Richtungen: Tastatur bzw. Bildschirm
	TTYResize = "resize" // Server→Agent: neue Fenstergröße
	TTYClose  = "close"  // Server→Agent: Sitzung beenden
	TTYOpened = "opened" // Agent→Server: Shell läuft
	TTYClosed = "closed" // Agent→Server: Sitzung zu Ende (Error: warum)
)

// MaxTTYChunk begrenzt die Nutzdaten eines Rahmens. Größere Blöcke teilt der
// Absender; so bleibt jedes Paket weit unter MaxPacketSize, und eine große
// Ausgabe (cat einer Logdatei) hält die Tastatur nicht auf.
const MaxTTYChunk = 32 * 1024

// maxHeldFrames begrenzt, wie viele vorausgeeilte Rahmen ein Sequencer
// aufhebt (höchstens 256 × MaxTTYChunk = 8 MB). Fehlt eine Nummer so lange,
// ist sie verloren - die Sitzung lässt sich dann nicht mehr richtig
// zusammensetzen. Über eine stehende Verbindung kommt das praktisch nicht vor
// (QoS 1 über eine TCP-Verbindung); reißt sie ab, enden die Sitzungen ohnehin.
const maxHeldFrames = 256

// maxHeldBytes begrenzt die Nutzdaten, die ein Sequencer insgesamt zurückhält.
// Die Zahl der Rahmen allein reicht nicht: Ein Rahmen darf bis zur Paketgröße
// (MaxPacketSize) reichen, 256 davon wären 2 GiB, die ein Gegenüber mit
// absichtlich fehlender Nummer 1 aufstauen könnte. Ein ehrlicher Absender
// schickt höchstens MaxTTYChunk je Rahmen, die Grenze liegt also genau bei
// maxHeldFrames solchen Rahmen.
const maxHeldBytes = maxHeldFrames * MaxTTYChunk

// ErrTTYGap: Ein Rahmen fehlt dauerhaft, die Sitzung ist nicht mehr in Reihe.
var ErrTTYGap = errors.New("terminal-daten unvollständig (rahmen verloren)")

// TTYFrame ist ein Rahmen einer Terminal-Sitzung. Bewusst kurze Feldnamen:
// Bei Tastatureingaben ist der Rahmen größer als die Nutzlast.
type TTYFrame struct {
	Session string `json:"s"`
	Seq     uint64 `json:"n"`
	Op      string `json:"op"`
	Data    []byte `json:"d,omitempty"`
	Term    string `json:"t,omitempty"`
	Cols    int    `json:"c,omitempty"`
	Rows    int    `json:"r,omitempty"`
	Error   string `json:"e,omitempty"`
}

// Sequencer setzt die Rahmen einer Richtung wieder in Reihe. Nicht
// nebenläufig nutzbar - der Aufrufer schützt ihn.
type Sequencer struct {
	next uint64
	held map[uint64]TTYFrame
	// bytes sind die Nutzdaten aller gehaltenen Rahmen.
	bytes int
}

// Push nimmt einen Rahmen an und liefert alle, die jetzt in Reihe sind (auch
// keinen). Doppelte und bereits gelieferte Nummern verwirft er.
func (s *Sequencer) Push(f TTYFrame) ([]TTYFrame, error) {
	if s.next == 0 {
		s.next = 1
		s.held = map[uint64]TTYFrame{}
	}
	if f.Seq < s.next {
		return nil, nil
	}
	if f.Seq > s.next {
		if _, dup := s.held[f.Seq]; dup {
			return nil, nil
		}
		if len(s.held) >= maxHeldFrames || s.bytes+len(f.Data) > maxHeldBytes {
			return nil, ErrTTYGap
		}
		s.held[f.Seq] = f
		s.bytes += len(f.Data)
		return nil, nil
	}
	ready := []TTYFrame{f}
	s.next++
	for {
		held, ok := s.held[s.next]
		if !ok {
			return ready, nil
		}
		delete(s.held, s.next)
		s.bytes -= len(held.Data)
		ready = append(ready, held)
		s.next++
	}
}

// Counter vergibt die Nummern einer Richtung (ab 1). Nicht nebenläufig
// nutzbar - der Aufrufer schützt ihn.
type Counter struct{ last uint64 }

// Next liefert die nächste Nummer.
func (c *Counter) Next() uint64 {
	c.last++
	return c.last
}

// heldBytes sind die Nutzdaten, die der Sequencer gerade zurückhält (Tests).
func (s *Sequencer) heldBytes() int { return s.bytes }
