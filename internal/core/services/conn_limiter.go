package services

import (
	"errors"
	"sync"
	"time"

	"LCM/internal/infrastructure/sshx"
)

// ErrTooManyConnections: Das Verbindungs-Limit des Servers ist erreicht
// (max. 1 ausführende + 1 lesende Verbindung gleichzeitig).
var ErrTooManyConnections = errors.New("verbindungs-limit des servers erreicht - bitte warten, bis die laufende verbindung abgeschlossen ist")

// ErrTooManyConsoles: Auf dem Server sind schon so viele Konsolen offen, wie
// er Plätze dafür hat.
var ErrTooManyConsoles = errors.New("auf diesem server sind bereits zu viele konsolen offen")

// terminalSlotsPerServer begrenzt die gleichzeitigen Konsolen je Server.
// Konsolen haben eigene Plätze: Belegten sie den ausführenden Slot, lief
// neben einer offenen Shell kein einziger Job auf dem Server - eine
// vergessene Konsole nahm ihn still aus der Verwaltung. Gleichzeitige
// apt-Läufe fängt die Sperren-Behandlung ab (aptlock.go).
const terminalSlotsPerServer = 2

// Wartezeiten beim Belegen eines Verbindungs-Slots: kurze Kollisionen
// (z.B. Health-Check trifft auf gerade endende Aktion) werden ausgesessen,
// statt sofort zu scheitern.
const (
	execSlotWait = 30 * time.Second
	readSlotWait = 10 * time.Second
)

// ConnLimiter begrenzt die gleichzeitigen SSH-Verbindungen PRO SERVER auf
// maximal zwei: eine ausführende (schreibende - Jobs, Aktionen, Sync) und
// eine rein lesende (z.B. Paketversions-Abfrage). Ergänzt den Job-Lock des
// JobService: der verhindert überlappende Jobs, der Limiter deckelt
// zusätzlich alles, was daran vorbei eine Verbindung öffnet.
type ConnLimiter struct {
	mu    sync.Mutex
	slots map[uint]*connSlots
}

// connSlots sind die Semaphoren eines Servers (exec/read je 1, Konsolen
// terminalSlotsPerServer).
type connSlots struct {
	exec     chan struct{}
	read     chan struct{}
	terminal chan struct{}
}

func NewConnLimiter() *ConnLimiter {
	return &ConnLimiter{slots: map[uint]*connSlots{}}
}

func (l *ConnLimiter) slotsFor(serverID uint) *connSlots {
	l.mu.Lock()
	defer l.mu.Unlock()
	s, ok := l.slots[serverID]
	if !ok {
		s = &connSlots{exec: make(chan struct{}, 1), read: make(chan struct{}, 1),
			terminal: make(chan struct{}, terminalSlotsPerServer)}
		l.slots[serverID] = s
	}
	return s
}

// AcquireExec belegt den ausführenden Slot eines Servers (wartet begrenzt).
// Die gelieferte release-Funktion MUSS nach dem Schließen der Verbindung
// aufgerufen werden (idempotent).
func (l *ConnLimiter) AcquireExec(serverID uint) (func(), error) {
	return acquire(l.slotsFor(serverID).exec, execSlotWait)
}

// AcquireRead belegt den lesenden Slot eines Servers (wartet begrenzt).
func (l *ConnLimiter) AcquireRead(serverID uint) (func(), error) {
	return acquire(l.slotsFor(serverID).read, readSlotWait)
}

// AcquireTerminal belegt einen Konsolen-Platz - ohne zu warten: Wer eine
// Konsole öffnet, sitzt davor und braucht sofort eine Antwort.
func (l *ConnLimiter) AcquireTerminal(serverID uint) (func(), error) {
	release, err := acquire(l.slotsFor(serverID).terminal, 0)
	if err != nil {
		return nil, ErrTooManyConsoles
	}
	return release, nil
}

func acquire(slot chan struct{}, wait time.Duration) (func(), error) {
	select {
	case slot <- struct{}{}:
	default:
		// Slot belegt: begrenzt warten statt sofort abzuweisen.
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case slot <- struct{}{}:
		case <-timer.C:
			return nil, ErrTooManyConnections
		}
	}
	var once sync.Once
	return func() { once.Do(func() { <-slot }) }, nil
}

// limitedConn koppelt die Slot-Freigabe an das Schließen der Verbindung -
// die Services nutzen conn.Close() wie gewohnt.
type limitedConn struct {
	sshx.Conn
	release func()
}

// Terminal reicht die Konsolen-Fähigkeit durch, sofern die innere Verbindung
// sie hat. Der Slot bleibt belegt, bis die Verbindung geschlossen wird - also
// die ganze Sitzung über (bei Konsolen ein eigener Konsolen-Platz, siehe
// AcquireTerminal).
func (c *limitedConn) Terminal(term string, cols, rows int) (sshx.Terminal, error) {
	inner, ok := c.Conn.(sshx.TerminalConn)
	if !ok {
		return nil, ErrTerminalUnsupported
	}
	return inner.Terminal(term, cols, rows)
}

func (c *limitedConn) Close() error {
	err := c.Conn.Close()
	c.release()
	return err
}
