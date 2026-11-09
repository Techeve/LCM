package services

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"
)

// ticketTTL ist die Lebensdauer einer Fahrkarte - lang genug für den
// unmittelbar folgenden Verbindungsaufbau, zu kurz für alles andere.
const ticketTTL = 30 * time.Second

// ticketStore ist der gemeinsame Speicher der Einmal-Fahrkarten für
// WebSocket-Verbindungen (Konsole, Ereigniskanal). Was eine Fahrkarte bindet -
// Server, Adresse, Sitzung -, bestimmt der Aufrufer über die Nutzlast P.
type ticketStore[P any] struct {
	mu      sync.Mutex
	tickets map[string]ticketEntry[P]
	now     func() time.Time
}

type ticketEntry[P any] struct {
	payload P
	expires time.Time
}

func newTicketStore[P any]() *ticketStore[P] {
	return &ticketStore[P]{tickets: map[string]ticketEntry[P]{}, now: time.Now}
}

// issue legt eine Fahrkarte für diese Nutzlast an und liefert ihren Wert.
func (s *ticketStore[P]) issue(payload P) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeLocked()
	s.tickets[token] = ticketEntry[P]{payload: payload, expires: s.now().Add(ticketTTL)}
	return token, nil
}

// redeem löst eine Fahrkarte ein, wenn sie gilt und match die Nutzlast
// annimmt. Sie gilt genau einmal; ein abgewiesener Versuch verbraucht sie nicht.
func (s *ticketStore[P]) redeem(token string, match func(P) bool) (P, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeLocked()
	entry, ok := s.tickets[token]
	if !ok || !match(entry.payload) {
		var none P
		return none, false
	}
	delete(s.tickets, token) // einmal heißt einmal
	return entry.payload, true
}

// purgeLocked räumt abgelaufene Fahrkarten weg. Erwartet den gehaltenen Mutex.
func (s *ticketStore[P]) purgeLocked() {
	now := s.now()
	for token, entry := range s.tickets {
		if now.After(entry.expires) {
			delete(s.tickets, token)
		}
	}
}

// EventTickets vergibt die Fahrkarten für den Ereigniskanal. Eine Fahrkarte
// gilt für genau eine Browser-Sitzung und deren Benutzer; mit ihr baut der Tab
// den WebSocket auf, der keine Kopfzeile mitschicken kann.
type EventTickets struct {
	store *ticketStore[eventTicket]
}

type eventTicket struct {
	userID    uint
	sessionID string
}

func NewEventTickets() *EventTickets {
	return &EventTickets{store: newTicketStore[eventTicket]()}
}

// Issue stellt eine Fahrkarte für diese Sitzung aus.
func (t *EventTickets) Issue(userID uint, sessionID string) (string, error) {
	return t.store.issue(eventTicket{userID: userID, sessionID: sessionID})
}

// Redeem löst eine Fahrkarte ein und liefert Benutzer und Sitzung, an die sie
// gebunden war.
func (t *EventTickets) Redeem(token string) (userID uint, sessionID string, ok bool) {
	ticket, ok := t.store.redeem(token, func(eventTicket) bool { return true })
	return ticket.userID, ticket.sessionID, ok
}
