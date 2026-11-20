package agent

import (
	"os"
	"sync"
	"syscall"
	"time"
)

// Nach einem Paket-Upgrade läuft noch das alte Binary. Ein Neustart von
// außen, aus dem postinst, risse ein laufendes Kommando mit - im schlimmsten
// Fall genau das apt, das das Upgrade gerade einspielt: systemd beendet beim
// Stopp die ganze Control-Group der Unit. Deshalb wechselt der Agent selbst
// auf das neue Binary, sobald er untätig ist: kein Kommando läuft oder
// wartet, jedes Ergebnis ist zugestellt, und seit switchQuiet kam kein neues
// (ein Job schickt seine Schritte dicht hintereinander). Er ersetzt sich per
// exec, die PID bleibt - systemd und supervise-daemon sehen denselben Dienst.
const (
	binaryCheckInterval = 10 * time.Second
	switchQuiet         = 30 * time.Second
)

// binaryWatch erkennt, dass das Binary auf der Platte ausgetauscht wurde.
// Paketmanager schreiben eine neue Datei und benennen sie über die alte -
// der Pfad zeigt danach auf eine andere Inode als der laufende Prozess.
type binaryWatch struct {
	path string
	orig os.FileInfo
}

func newBinaryWatch(path string) (*binaryWatch, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	return &binaryWatch{path: path, orig: fi}, nil
}

// replaced meldet ein neues Binary. Fehlt die Datei (Paket wird entfernt),
// ist das kein Grund zu wechseln.
func (w *binaryWatch) replaced() bool {
	fi, err := os.Stat(w.path)
	return err == nil && !os.SameFile(w.orig, fi)
}

// workState zählt die Kommandos in Arbeit, die Zustellung des Ergebnisses
// eingeschlossen.
type workState struct {
	mu        sync.Mutex
	busy      int
	lastDone  time.Time
	switching bool // Wechsel aufs neue Binary läuft - nichts mehr annehmen
}

// begin nimmt ein Kommando in Arbeit; false, wenn der Wechsel schon läuft.
func (s *workState) begin() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.switching {
		return false
	}
	s.busy++
	return true
}

// end schließt ein Kommando ab (nach der Zustellung des Ergebnisses).
func (s *workState) end() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.busy--
	s.lastDone = time.Now()
}

// claimIdle meldet true und sperrt weitere Kommandos, wenn nichts in Arbeit
// ist und das letzte seit quiet fertig ist.
func (s *workState) claimIdle(quiet time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy > 0 || time.Since(s.lastDone) < quiet {
		return false
	}
	s.switching = true
	return true
}

// switchOnNewBinary prüft im Takt, ob ein neues Binary da ist, und wechselt
// in Ruhe darauf. Kehrt nur zurück, wenn der Pfad des Binaries unbekannt ist.
func (c *Client) switchOnNewBinary() {
	path, err := os.Executable()
	var w *binaryWatch
	if err == nil {
		w, err = newBinaryWatch(path)
	}
	if err != nil {
		c.log.Warn("binary path unknown - no automatic switch after an upgrade", "error", err)
		return
	}
	ticker := time.NewTicker(binaryCheckInterval)
	defer ticker.Stop()
	for range ticker.C {
		if c.readyToSwitch(w) {
			c.execNewBinary(w.path)
		}
	}
}

// readyToSwitch: neues Binary auf der Platte und der Agent in Ruhe.
func (c *Client) readyToSwitch(w *binaryWatch) bool {
	return w.replaced() && c.work.claimIdle(switchQuiet)
}

// execNewBinary trennt sauber vom Broker und ersetzt den Prozess durch das
// neue Binary. Scheitert das exec, beendet sich der Agent - der
// Dienstverwalter startet ihn dann frisch.
func (c *Client) execNewBinary(path string) {
	c.log.Info("new lcm-agent binary installed - switching to it", "path", path)
	if c.mqtt != nil {
		c.mqtt.Disconnect(250)
	}
	err := syscall.Exec(path, os.Args, os.Environ())
	c.log.Error("switch to the new binary failed - exiting", "error", err)
	os.Exit(1)
}
