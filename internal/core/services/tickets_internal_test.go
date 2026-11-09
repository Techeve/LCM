package services

import (
	"testing"
	"time"
)

// TestEventTicketIsSingleUse: Die Fahrkarte des Ereigniskanals gilt genau einmal
// und trägt Benutzer und Sitzung, an die sie gebunden wurde.
func TestEventTicketIsSingleUse(t *testing.T) {
	tickets := NewEventTickets()
	token, err := tickets.Issue(7, "sitzung-a")
	if err != nil {
		t.Fatal(err)
	}
	userID, sessionID, ok := tickets.Redeem(token)
	if !ok || userID != 7 || sessionID != "sitzung-a" {
		t.Fatalf("erstes Einlösen: user=%d session=%q ok=%v", userID, sessionID, ok)
	}
	if _, _, ok := tickets.Redeem(token); ok {
		t.Error("die Fahrkarte wurde ein zweites Mal angenommen")
	}
	if _, _, ok := tickets.Redeem("ausgedacht"); ok {
		t.Error("eine erfundene Fahrkarte wurde angenommen")
	}
}

// TestEventTicketExpires: nach 30 Sekunden ist sie wertlos.
func TestEventTicketExpires(t *testing.T) {
	tickets := NewEventTickets()
	now := time.Now()
	tickets.store.now = func() time.Time { return now }
	token, err := tickets.Issue(7, "sitzung-a")
	if err != nil {
		t.Fatal(err)
	}

	now = now.Add(ticketTTL - time.Second)
	// Noch gültig - der Test verbraucht sie hier nicht, sondern gibt eine zweite aus.
	if _, err := tickets.Issue(8, "sitzung-b"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	if _, _, ok := tickets.Redeem(token); ok {
		t.Error("eine abgelaufene Fahrkarte wurde angenommen")
	}
}
