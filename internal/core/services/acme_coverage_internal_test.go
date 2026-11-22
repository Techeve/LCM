package services

import (
	"errors"
	"testing"
	"time"
)

// Die Wartezeit nach einem gescheiterten Lauf von selbst gilt eine Stunde, nur
// für dieselben Namen, und ein Erfolg beendet sie.
func TestAutoBackoffLastsAnHourForTheSameNames(t *testing.T) {
	now := time.Date(2026, 11, 22, 9, 0, 0, 0, time.UTC)
	svc := &ACMEService{clock: func() time.Time { return now }}
	key := autoRunKey("https://ca.example/dir", []string{"b.example.org", "a.example.org"})

	if svc.inBackoff(key) {
		t.Fatal("vor jedem Lauf gilt keine Wartezeit")
	}
	svc.noteAutoRun(key, errors.New("ratenlimit"))
	if !svc.inBackoff(key) {
		t.Error("direkt nach dem Fehlschlag gilt keine Wartezeit")
	}
	// Die Reihenfolge der Namen zählt nicht, andere Namen oder eine andere CA schon.
	if !svc.inBackoff(autoRunKey("https://ca.example/dir", []string{"a.example.org", "b.example.org"})) {
		t.Error("dieselben Namen in anderer Reihenfolge gelten als andere")
	}
	if svc.inBackoff(autoRunKey("https://ca.example/dir", []string{"a.example.org", "b.example.org", "c.example.org"})) {
		t.Error("andere Namen erben die Wartezeit")
	}
	if svc.inBackoff(autoRunKey("https://other.example/dir", []string{"a.example.org", "b.example.org"})) {
		t.Error("eine andere CA erbt die Wartezeit")
	}

	now = now.Add(59 * time.Minute)
	if !svc.inBackoff(key) {
		t.Error("nach 59 Minuten ist die Wartezeit schon vorbei")
	}
	now = now.Add(2 * time.Minute)
	if svc.inBackoff(key) {
		t.Error("nach über einer Stunde gilt die Wartezeit noch")
	}

	svc.noteAutoRun(key, errors.New("wieder"))
	svc.noteAutoRun(key, nil)
	if svc.inBackoff(key) {
		t.Error("ein Erfolg beendet die Wartezeit nicht")
	}
}
