package wire

import "testing"

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
	if _, err := s.Push(TTYFrame{Seq: 2 + maxHeldFrames}); err != ErrTTYGap {
		t.Fatalf("erwartet ErrTTYGap, bekam %v", err)
	}
}
