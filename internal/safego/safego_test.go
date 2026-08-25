package safego_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"LCM/internal/safego"
)

// TestGoSurvivesPanic: Der springende Punkt des Pakets - ein Panic in einer
// Hintergrund-Goroutine darf den Prozess NICHT beenden. Läuft dieser Test
// durch (statt den Testlauf abzubrechen), ist der Schutz wirksam.
func TestGoSurvivesPanic(t *testing.T) {
	safego.Reset()
	var wg sync.WaitGroup
	wg.Add(1)
	safego.Go("test:panic", func() {
		defer wg.Done()
		panic("absichtlicher testfehler")
	})
	wg.Wait()
	// Kurz warten, bis das Recover den Panic verbucht hat (es läuft im defer
	// NACH wg.Done()).
	waitFor(t, func() bool { return safego.Total() == 1 })
}

// TestGoCleanupRunsOnPanic: Bei einem Panic MUSS die Aufräumfunktion laufen -
// daran hängt in der Anwendung die Freigabe der Server-Sperre.
func TestGoCleanupRunsOnPanic(t *testing.T) {
	safego.Reset()
	done := make(chan error, 1)
	safego.GoCleanup("test:cleanup", func(err error) { done <- err }, func() {
		panic(errors.New("kaputt"))
	})
	select {
	case err := <-done:
		if err == nil || err.Error() != "kaputt" {
			t.Fatalf("cleanup bekam den falschen Fehler: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cleanup wurde nach einem Panic nicht aufgerufen - die Server-Sperre bliebe hängen")
	}
}

// TestGoCleanupNotRunOnSuccess: Ohne Panic darf das Aufräumen NICHT laufen,
// sonst würde ein erfolgreicher Job nachträglich als fehlgeschlagen markiert.
func TestGoCleanupNotRunOnSuccess(t *testing.T) {
	safego.Reset()
	var called bool
	var mu sync.Mutex
	done := make(chan struct{})
	safego.GoCleanup("test:ok", func(error) {
		mu.Lock()
		called = true
		mu.Unlock()
	}, func() { close(done) })
	<-done
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if called {
		t.Fatal("cleanup lief obwohl kein Panic auftrat")
	}
}

// TestCleanupPanicIsContained: Panickt die Aufräumfunktion selbst, darf auch
// das den Prozess nicht beenden.
func TestCleanupPanicIsContained(t *testing.T) {
	safego.Reset()
	var wg sync.WaitGroup
	wg.Add(1)
	safego.GoCleanup("test:cleanup-panic", func(error) {
		defer wg.Done()
		panic("auch das aufräumen scheitert")
	}, func() {
		panic("erster fehler")
	})
	wg.Wait()
}

// TestRecentCountWindow prüft die Grundlage der Instabilitäts-Erkennung.
func TestRecentCountWindow(t *testing.T) {
	safego.Reset()
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		safego.Go("test:mehrfach", func() {
			defer wg.Done()
			panic("wiederholt")
		})
	}
	wg.Wait()
	waitFor(t, func() bool { return safego.RecentCount(time.Minute) == 3 })
	if got := safego.Total(); got != 3 {
		t.Errorf("Total() = %d, erwartet 3", got)
	}
	// Ein Fenster in der Vergangenheit darf nichts zählen.
	if n := safego.RecentCount(time.Nanosecond); n != 0 {
		t.Errorf("RecentCount(1ns) = %d, erwartet 0", n)
	}
}

// waitFor pollt eine Bedingung, statt auf feste Schlafzeiten zu setzen.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("Bedingung wurde nicht rechtzeitig erfüllt")
}
