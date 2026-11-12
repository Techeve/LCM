package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"LCM/internal/remote/wire"
)

// waitRunning wartet, bis der Runner das Kommando eingetragen hat.
func waitRunning(t *testing.T, r *Runner, id string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		_, running := r.running[id]
		r.mu.Unlock()
		if running {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("kommando %s nie gestartet", id)
}

// TestRunnerClaimVerwirftDoppelteID: Dieselbe ID wird nur einmal angenommen.
func TestRunnerClaimVerwirftDoppelteID(t *testing.T) {
	r := NewRunner()
	if !r.Claim("a") {
		t.Fatal("erste zustellung abgelehnt")
	}
	if r.Claim("a") {
		t.Fatal("zweite zustellung derselben id angenommen")
	}
	if !r.Claim("b") {
		t.Fatal("andere id abgelehnt")
	}
}

// TestIDWindowVergisstDieAeltesten: Das Fenster bleibt begrenzt.
func TestIDWindowVergisstDieAeltesten(t *testing.T) {
	w := newIDWindow(2)
	w.add("a")
	w.add("b")
	w.add("c")
	if w.has("a") || !w.has("b") || !w.has("c") || len(w.set) != 2 {
		t.Fatalf("fenster falsch: %v", w.order)
	}
}

// TestRunnerCancelTrifftWartendesKommando: Ein Kommando, das hinter einem
// anderen wartet, wird abgebrochen, bevor es startet. Vorher lief es los,
// sobald es an der Reihe war - der Abbruch war ins Leere gegangen.
func TestRunnerCancelTrifftWartendesKommando(t *testing.T) {
	r := NewRunner()
	marker := filepath.Join(t.TempDir(), "gelaufen")

	first := make(chan wire.Result, 1)
	go func() { first <- r.Run(wire.Command{ID: "erstes", Cmd: "sleep 30"}, nil) }()
	waitRunning(t, r, "erstes")

	second := make(chan wire.Result, 1)
	go func() { second <- r.Run(wire.Command{ID: "zweites", Cmd: "touch " + marker}, nil) }()
	time.Sleep(50 * time.Millisecond) // zweites wartet auf execMu
	r.Cancel("zweites")
	r.Cancel("erstes")

	for _, ch := range []chan wire.Result{first, second} {
		select {
		case <-ch:
		case <-time.After(5 * time.Second):
			t.Fatal("kommando kam nicht zurück")
		}
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("abgebrochenes wartendes kommando ist trotzdem gelaufen")
	}
}

// TestRunnerCancelVorDemKommando: Überholt der Abbruch sein Kommando (der
// Client verarbeitet MQTT-Nachrichten nebenläufig), startet es nicht.
func TestRunnerCancelVorDemKommando(t *testing.T) {
	r := NewRunner()
	marker := filepath.Join(t.TempDir(), "gelaufen")
	r.Cancel("x")
	res := r.Run(wire.Command{ID: "x", Cmd: "touch " + marker}, nil)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("kommando lief trotz vorherigem abbruch")
	}
	if !strings.Contains(res.Error, "abgebrochen") || res.ExitCode == 0 {
		t.Fatalf("ergebnis meldet den abbruch nicht: %+v", res)
	}
}
