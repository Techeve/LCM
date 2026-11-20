package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestBinaryWatchErkenntAustausch: Ein Paketmanager benennt die neue Datei
// über die alte - das erkennt der Agent, ein unveränderter Pfad nicht, eine
// entfernte Datei auch nicht.
func TestBinaryWatchErkenntAustausch(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "lcm-agent")
	if err := os.WriteFile(bin, []byte("alt"), 0o755); err != nil {
		t.Fatal(err)
	}
	w, err := newBinaryWatch(bin)
	if err != nil {
		t.Fatal(err)
	}
	if w.replaced() {
		t.Fatal("unverändertes binary gilt als ersetzt")
	}
	staged := filepath.Join(dir, "lcm-agent.dpkg-new")
	if err := os.WriteFile(staged, []byte("neu"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(staged, bin); err != nil {
		t.Fatal(err)
	}
	if !w.replaced() {
		t.Fatal("ausgetauschtes binary nicht erkannt")
	}
	if err := os.Remove(bin); err != nil {
		t.Fatal(err)
	}
	if w.replaced() {
		t.Fatal("entferntes binary darf keinen wechsel auslösen")
	}
}

// TestWorkStateWechseltNurInRuhe: Der Wechsel wartet, bis kein Kommando in
// Arbeit ist und das letzte lange genug zurückliegt - danach nimmt der Agent
// keines mehr an.
func TestWorkStateWechseltNurInRuhe(t *testing.T) {
	var s workState
	if !s.begin() {
		t.Fatal("kommando abgelehnt")
	}
	if s.claimIdle(0) {
		t.Fatal("wechsel trotz laufendem kommando")
	}
	s.end()
	if s.claimIdle(time.Hour) {
		t.Fatal("wechsel direkt nach dem letzten kommando")
	}
	if !s.claimIdle(0) {
		t.Fatal("kein wechsel in ruhe")
	}
	if s.begin() {
		t.Fatal("kommando während des wechsels angenommen")
	}
}
