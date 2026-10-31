package services

import (
	"errors"
	"testing"
)

// TestConsoleDoesNotBlockJobs: Eine offene Konsole belegt einen eigenen
// Platz - der ausführende Slot für LCMs Jobs bleibt frei. Mehr als
// terminalSlotsPerServer Konsolen werden sofort abgewiesen.
func TestConsoleDoesNotBlockJobs(t *testing.T) {
	l := NewConnLimiter()
	for range terminalSlotsPerServer {
		if _, err := l.AcquireTerminal(7); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := l.AcquireTerminal(7); !errors.Is(err, ErrTooManyConsoles) {
		t.Fatalf("weitere konsole angenommen: %v", err)
	}
	release, err := l.AcquireExec(7)
	if err != nil {
		t.Fatalf("job-slot durch konsolen blockiert: %v", err)
	}
	release()
}

// TestConsoleRegistryUnregistersOnce: Austragen ist idempotent - das Ende
// einer Sitzung kann von beiden Seiten kommen.
func TestConsoleRegistryUnregistersOnce(t *testing.T) {
	r := newConsoleRegistry()
	done := r.add(3, "tony")
	other := r.add(4, "anna")
	if got := r.list(); len(got) != 2 || got[0].Actor != "tony" {
		t.Fatalf("offen: %+v", got)
	}
	done()
	done()
	if got := r.list(); len(got) != 1 || got[0].ServerID != 4 {
		t.Fatalf("nach dem austragen: %+v", got)
	}
	other()
	if len(r.list()) != 0 {
		t.Fatal("register nicht leer")
	}
}
