package remote

import (
	"errors"
	"net"
	"os"
	"sync"
	"time"

	"github.com/mochi-mqtt/server/v2/packets"

	"LCM/internal/core/domain"
)

// Verbindungsbilanz je Agent (H-2): Wie oft hat sich der Agent verbunden, wie oft
// ist er getrennt worden, und aus welchem Grund. Die Zähler leben nur im
// Speicher; ein Neustart von LCM setzt sie zurück. Ungewollte Trennungen der
// letzten Stunde bewertet die Ampel (domain.AgentLink.Unstable).

// maxKeptDrops begrenzt die gemerkten ungewollten Trennungen je Agent. Die
// Schwelle der Ampel liegt weit darunter; mehr würde nur Speicher kosten.
const maxKeptDrops = 64

type drop struct {
	at    time.Time
	cause string
}

type linkCounts struct {
	connects, disconnects int
	unintended            []drop // innerhalb des Fensters, ältere zuerst
}

// linkBook führt die Bilanz aller Agenten. now ist die Uhr (Tests setzen sie).
type linkBook struct {
	mu     sync.Mutex
	now    func() time.Time
	agents map[string]*linkCounts
	// expected sind Ursachen, die LCM vor dem Trennen selbst vermerkt hat: Das
	// Trennen erreicht den Hook des Brokers nur als Netzfehler.
	expected map[string]string
}

func newLinkBook() *linkBook {
	return &linkBook{now: time.Now, agents: map[string]*linkCounts{}, expected: map[string]string{}}
}

func (b *linkBook) counts(agentID string) *linkCounts {
	c := b.agents[agentID]
	if c == nil {
		c = &linkCounts{}
		b.agents[agentID] = c
	}
	return c
}

// connected verbucht eine Verbindung. Gab es noch eine Verbindung desselben
// Agents (previous), hat er sich neu verbunden, bevor LCM die alte als tot
// erkannte: Das ist eine ungewollte Trennung.
func (b *linkBook) connected(agentID string, previous bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.expected, agentID)
	c := b.counts(agentID)
	c.connects++
	if previous {
		b.record(c, domain.DropTakeover)
	}
}

// expect merkt eine Ursache vor, die der Hook beim nächsten Trennen von agentID
// verbuchen soll.
func (b *linkBook) expect(agentID, cause string) {
	b.mu.Lock()
	b.expected[agentID] = cause
	b.mu.Unlock()
}

// dropped verbucht das Ende einer Verbindung; err ist das, was der Broker dazu
// meldet.
func (b *linkBook) dropped(agentID string, err error) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	cause, ok := b.expected[agentID]
	if ok {
		delete(b.expected, agentID)
	} else {
		cause = classifyDrop(err)
	}
	b.record(b.counts(agentID), cause)
	return cause
}

// record trägt eine Trennung ein; Aufrufer halten b.mu.
func (b *linkBook) record(c *linkCounts, cause string) {
	c.disconnects++
	now := b.now()
	c.prune(now)
	if !domain.UnintendedDrop(cause) {
		return
	}
	c.unintended = append(c.unintended, drop{at: now, cause: cause})
	if extra := len(c.unintended) - maxKeptDrops; extra > 0 {
		c.unintended = c.unintended[extra:]
	}
}

// prune wirft ungewollte Trennungen außerhalb des Fensters weg.
func (c *linkCounts) prune(now time.Time) {
	cut := 0
	for cut < len(c.unintended) && now.Sub(c.unintended[cut].at) > domain.AgentLinkWindow {
		cut++
	}
	c.unintended = c.unintended[cut:]
}

func (b *linkBook) snapshot(agentID string) domain.AgentLink {
	b.mu.Lock()
	defer b.mu.Unlock()
	c := b.agents[agentID]
	if c == nil {
		return domain.AgentLink{}
	}
	c.prune(b.now())
	link := domain.AgentLink{Connects: c.connects, Disconnects: c.disconnects, UnintendedLastHour: len(c.unintended)}
	if n := len(c.unintended); n > 0 {
		last := c.unintended[n-1]
		link.LastCause, link.LastCauseAt = last.cause, &last.at
	}
	return link
}

// classifyDrop ordnet das Ende einer Verbindung ein, die LCM nicht selbst
// beendet hat. Ohne Fehler oder mit dem Abschied des Agents (DISCONNECT) hat der
// Agent sich abgemeldet; bleibt sein Keepalive aus, ist das ein Ausfall der
// Lebenszeichen, jeder andere Fehler gilt als Verbindungs- oder Netzfehler.
func classifyDrop(err error) string {
	switch {
	case err == nil || errors.Is(err, packets.CodeDisconnect):
		return domain.DropAgent
	case errors.Is(err, packets.ErrServerShuttingDown):
		return domain.DropLCM
	case isTimeout(err):
		return domain.DropPing
	}
	return domain.DropNetwork
}

// isTimeout: Der Broker hat die Verbindung beendet, weil innerhalb des Keepalive
// (anderthalbfache Frist des Agents) nichts mehr kam.
func isTimeout(err error) bool {
	var netErr net.Error
	return errors.Is(err, os.ErrDeadlineExceeded) || errors.Is(err, packets.ErrKeepAliveTimeout) ||
		(errors.As(err, &netErr) && netErr.Timeout())
}

// LinkStats liefert die Verbindungsbilanz des Agents (seit dem Start von LCM).
func (h *Hub) LinkStats(agentID string) domain.AgentLink {
	return h.links.snapshot(agentID)
}
