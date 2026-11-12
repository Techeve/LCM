package middlewares

import (
	"testing"
	"time"
)

// TestAPIKeyLimiterCapsTable: Zufällige Schlüssel lassen die Tabelle nicht
// über ihre Grenze wachsen. Bei voller Tabelle teilen sich neue Schlüssel
// einen Zähler - das Limit greift trotzdem.
func TestAPIKeyLimiterCapsTable(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	l := newAPIKeyLimiter(3)
	for _, id := range []string{"a", "b", "c"} {
		if n, _ := l.hit(id, now); n != 1 {
			t.Fatalf("%s: Zähler %d, erwartet 1", id, n)
		}
	}
	for i, id := range []string{"d", "e", "f"} {
		if n, _ := l.hit(id, now); n != i+1 {
			t.Errorf("%s: gemeinsamer Zähler %d, erwartet %d", id, n, i+1)
		}
	}
	if len(l.buckets) != 4 {
		t.Errorf("%d Einträge, erwartet 4 (drei eigene, ein gemeinsamer)", len(l.buckets))
	}
	// Ein Schlüssel mit eigenem Fenster zählt unverändert weiter.
	if n, _ := l.hit("a", now); n != 2 {
		t.Errorf("a: Zähler %d, erwartet 2", n)
	}
}

// TestAPIKeyLimiterFreesExpiredWindows: Sind die Fenster abgelaufen, bekommt
// ein neuer Schlüssel wieder einen eigenen Zähler.
func TestAPIKeyLimiterFreesExpiredWindows(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	l := newAPIKeyLimiter(2)
	l.hit("a", now)
	l.hit("b", now)
	l.hit("c", now) // gemeinsamer Zähler
	later := now.Add(time.Minute)
	if n, _ := l.hit("d", later); n != 1 {
		t.Fatalf("d: Zähler %d, erwartet 1", n)
	}
	if _, ok := l.buckets["d"]; !ok {
		t.Error("d hat nach dem Aufräumen keinen eigenen Zähler bekommen")
	}
	if len(l.buckets) != 1 {
		t.Errorf("%d Einträge nach dem Aufräumen, erwartet 1", len(l.buckets))
	}
}
