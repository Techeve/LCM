package wire

import (
	"errors"
	"testing"
)

func seqs(frames []TTYFrame) []uint64 {
	var out []uint64
	for _, f := range frames {
		out = append(out, f.Seq)
	}
	return out
}

// Rahmen kommen über MQTT ungeordnet und manchmal doppelt an; der Sequencer
// liefert jeden genau einmal und in Reihe.
func TestSequencerOrdnet(t *testing.T) {
	var s Sequencer
	steps := []struct {
		seq  uint64
		want []uint64
	}{
		{2, nil},
		{1, []uint64{1, 2}},
		{1, nil},
		{4, nil},
		{3, []uint64{3, 4}},
		{2, nil},
		{5, []uint64{5}},
	}
	for _, step := range steps {
		got, err := s.Push(TTYFrame{Seq: step.seq})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != len(step.want) {
			t.Fatalf("push %d: %v, erwartet %v", step.seq, seqs(got), step.want)
		}
		for i := range got {
			if got[i].Seq != step.want[i] {
				t.Fatalf("push %d: %v, erwartet %v", step.seq, seqs(got), step.want)
			}
		}
	}
}

// Fehlt ein Rahmen dauerhaft, staut sich nicht beliebig viel auf.
func TestSequencerLuecke(t *testing.T) {
	var s Sequencer
	for i := uint64(2); i < 2+maxHeldFrames; i++ {
		if _, err := s.Push(TTYFrame{Seq: i}); err != nil {
			t.Fatalf("rahmen %d: %v", i, err)
		}
	}
	if _, err := s.Push(TTYFrame{Seq: 2 + maxHeldFrames}); !errors.Is(err, ErrTTYGap) {
		t.Fatalf("erwartet ErrTTYGap, bekam %v", err)
	}
}

// Ein Gegenüber, das Nummer 1 nie schickt, kann den Sequencer nicht beliebig
// füllen: Die Nutzdaten der gehaltenen Rahmen sind begrenzt, nicht nur ihre Zahl
// (vorher 256 Rahmen bis zur Paketgröße, also bis zu 2 GiB).
func TestSequencerHaeltNurBegrenzteDaten(t *testing.T) {
	var s Sequencer
	big := make([]byte, 1<<20)
	var err error
	pushed := 0
	for seq := uint64(2); err == nil && seq < 100; seq++ {
		_, err = s.Push(TTYFrame{Seq: seq, Data: big})
		pushed++
	}
	if !errors.Is(err, ErrTTYGap) {
		t.Fatalf("Fehler = %v, erwartet ErrTTYGap", err)
	}
	if pushed > maxHeldBytes/len(big)+1 {
		t.Errorf("%d MiB-Rahmen gehalten, Grenze %d Bytes", pushed-1, maxHeldBytes)
	}
	if s.heldBytes() > maxHeldBytes {
		t.Errorf("hält %d Bytes", s.heldBytes())
	}
}

// Ein doppelt zugestellter, vorausgeeilter Rahmen zählt nur einmal.
func TestSequencerDoppelterVorausRahmen(t *testing.T) {
	var s Sequencer
	for range 3 {
		if _, err := s.Push(TTYFrame{Seq: 3, Data: []byte("abc")}); err != nil {
			t.Fatal(err)
		}
	}
	if s.heldBytes() != 3 {
		t.Errorf("hält %d Bytes, erwartet 3", s.heldBytes())
	}
	got, _ := s.Push(TTYFrame{Seq: 1})
	got2, _ := s.Push(TTYFrame{Seq: 2})
	if len(got) != 1 || len(got2) != 2 || s.heldBytes() != 0 {
		t.Errorf("Lieferung %v / %v, hält %d", seqs(got), seqs(got2), s.heldBytes())
	}
}
