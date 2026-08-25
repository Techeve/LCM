package health

import (
	"errors"
	"sync"
	"testing"
	"time"

	"LCM/internal/safego"
)

// TestTickHealthyNoRestart: Im Normalbetrieb darf NIEMALS ein Neustart
// ausgelöst werden - ein Fehlalarm hier würde den Dienst im Kreis starten.
func TestTickHealthyNoRestart(t *testing.T) {
	safego.Reset()
	var restarted bool
	m := NewMonitor(func() error { return nil }).
		WithRestart(func(string) { restarted = true })

	for i := 0; i < 20; i++ {
		m.tick()
	}
	if restarted {
		t.Fatal("Neustart bei gesundem Dienst ausgelöst")
	}
	if st := m.Status(); !st.Healthy || st.FailStreak != 0 {
		t.Errorf("Status falsch: %+v", st)
	}
}

// TestRestartAfterPersistentFailure: Erst nach unhealthyLimit
// aufeinanderfolgenden Fehlschlägen wird neu gestartet - vorher nicht.
func TestRestartAfterPersistentFailure(t *testing.T) {
	safego.Reset()
	var reasons []string
	m := NewMonitor(func() error { return errors.New("datenbank weg") }).
		WithRestart(func(r string) { reasons = append(reasons, r) })

	for i := 1; i < unhealthyLimit; i++ {
		m.tick()
		if len(reasons) != 0 {
			t.Fatalf("zu früh neu gestartet (nach %d Fehlversuchen)", i)
		}
	}
	m.tick() // der Tick, der die Grenze erreicht
	if len(reasons) != 1 {
		t.Fatalf("erwartete genau einen Neustart, bekam %d", len(reasons))
	}
	if st := m.Status(); st.Healthy || st.Error != "datenbank weg" {
		t.Errorf("Status falsch: %+v", st)
	}
}

// TestFailStreakResets: Ein zwischenzeitlicher Erfolg setzt die Zählung
// zurück - kurze Aussetzer (z.B. gesperrte SQLite-Datei während eines
// Backups) dürfen sich nicht bis zur Neustart-Schwelle aufaddieren.
func TestFailStreakResets(t *testing.T) {
	safego.Reset()
	var restarted bool
	var mu sync.Mutex
	healthy := false
	m := NewMonitor(func() error {
		mu.Lock()
		defer mu.Unlock()
		if healthy {
			return nil
		}
		return errors.New("kurzer aussetzer")
	}).WithRestart(func(string) { restarted = true })

	// Knapp unter die Grenze scheitern …
	for i := 1; i < unhealthyLimit; i++ {
		m.tick()
	}
	// … einmal Erfolg …
	mu.Lock()
	healthy = true
	mu.Unlock()
	m.tick()
	if st := m.Status(); st.FailStreak != 0 {
		t.Fatalf("FailStreak nach Erfolg = %d, erwartet 0", st.FailStreak)
	}
	// … dann wieder scheitern: die Zählung beginnt von vorn.
	mu.Lock()
	healthy = false
	mu.Unlock()
	for i := 1; i < unhealthyLimit; i++ {
		m.tick()
	}
	if restarted {
		t.Fatal("Neustart trotz zwischenzeitlicher Erholung ausgelöst")
	}
}

// TestRestartOnPanicFlood: Häufen sich abgefangene Panics, gilt der Prozess
// als instabil und wird neu gestartet - auch wenn die Datenbank erreichbar ist.
func TestRestartOnPanicFlood(t *testing.T) {
	safego.Reset()
	var reason string
	m := NewMonitor(func() error { return nil }).
		WithRestart(func(r string) { reason = r })

	var wg sync.WaitGroup
	for i := 0; i < panicLimit; i++ {
		wg.Add(1)
		safego.Go("test:flut", func() {
			defer wg.Done()
			panic("instabil")
		})
	}
	wg.Wait()
	// Auf die Verbuchung warten (läuft im defer nach wg.Done()).
	deadline := time.Now().Add(2 * time.Second)
	for safego.RecentCount(panicWindow) < panicLimit && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	m.tick()
	if reason == "" {
		t.Fatal("kein Neustart trotz gehäufter Panics")
	}
}

// TestWatchdogIntervalWithoutSystemd: Ohne systemd (NOTIFY_SOCKET/WATCHDOG_USEC
// nicht gesetzt) muss die Watchdog-Anbindung wirkungslos sein - LCM läuft auch
// in Docker und im Vordergrund.
func TestWatchdogIntervalWithoutSystemd(t *testing.T) {
	t.Setenv("WATCHDOG_USEC", "")
	t.Setenv("NOTIFY_SOCKET", "")
	if d := watchdogInterval(); d != 0 {
		t.Errorf("watchdogInterval() = %v, erwartet 0 ohne systemd", d)
	}
	if WatchdogActive() {
		t.Error("WatchdogActive() = true ohne systemd")
	}
	// Darf nicht panicken oder blockieren.
	NotifyReady("test")
	notifyWatchdog("test")
	NotifyStopping()
}

// TestWatchdogIntervalIsHalf: systemd gibt WATCHDOG_USEC vor; gepingt wird mit
// der halben Zeit, damit ein verzögerter Durchlauf keinen Neustart auslöst.
func TestWatchdogIntervalIsHalf(t *testing.T) {
	t.Setenv("WATCHDOG_USEC", "90000000") // 90 s
	t.Setenv("WATCHDOG_PID", "")
	if got, want := watchdogInterval(), 45*time.Second; got != want {
		t.Errorf("watchdogInterval() = %v, erwartet %v", got, want)
	}
}
