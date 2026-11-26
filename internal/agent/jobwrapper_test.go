package agent

import (
	"strconv"
	"testing"
	"time"

	"LCM/internal/remote/wire"
)

// TestAbbruchGleichNachDemStart: Ein Abbruch, der das Kommando trifft, kaum dass
// der Job angelegt ist, beendet es. Vorher setzte der Wrapper sein trap vor dem
// Start des Kommandos: Ein SIGTERM in dieser Lücke schluckte die Shell, und das
// Kommando lief danach bis zum SIGKILL nach der Frist (30 s).
func TestAbbruchGleichNachDemStart(t *testing.T) {
	for i := range 20 {
		id := "frueh-" + strconv.Itoa(i)
		r := NewRunner()
		done := make(chan wire.Result, 1)
		go func() { done <- r.Run(wire.Command{ID: id, Cmd: "sleep 30"}, nil) }()
		waitRunning(t, r, id)
		r.Cancel(id)
		select {
		case res := <-done:
			if res.ExitCode == 0 && res.Error == "" {
				t.Fatalf("%s: abgebrochenes Kommando meldet Erfolg: %+v", id, res)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: der Abbruch gleich nach dem Start beendete das Kommando nicht", id)
		}
	}
}

// TestWrapperBehaeltExitCodeUndEingabe: Das Kommando läuft im Hintergrund des
// Wrappers; Exit-Code und Eingabe kommen trotzdem unverändert an.
func TestWrapperBehaeltExitCodeUndEingabe(t *testing.T) {
	r := NewRunner()
	res := r.Run(wire.Command{ID: "eingabe", Cmd: "read x; echo \"[$x]\"; exit 7", Stdin: "hallo\n"}, nil)
	if res.ExitCode != 7 || res.Output != "[hallo]\n" {
		t.Fatalf("Ergebnis: %+v", res)
	}
}
