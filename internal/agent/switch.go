package agent

import (
	"os"
	"sync"
	"syscall"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
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
	// Ab dem Fund des neuen Binarys: seit wann der Wechsel ansteht und welche
	// Hinweise die offenen Web-Konsolen schon bekamen.
	pendingSince time.Time
	noticed      bool // „Wechsel steht an"
	noticedNow   bool // „Wechsel jetzt"
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
	path, err := c.watchedBinary()
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

// watchedBinary ist das Binary, dessen Austausch den Wechsel auslöst: das des
// Pakets. Läuft der Agent als Rückfall (lcm-agent.prev), liegt er an einem Pfad,
// den kein Paket je ersetzt - dann zählt das Binary des Pakets, damit ein
// erneutes Update ihn von dort holt.
func (c *Client) watchedBinary() (string, error) {
	if c.asPrev {
		return c.packagedPath, nil
	}
	return os.Executable()
}

// readyToSwitch: neues Binary auf der Platte und der Agent in Ruhe. Offene
// Web-Konsolen halten den Wechsel auf, höchstens ConsoleWait lang: Sie beendet
// der Wechsel, und wer gerade tippt, soll das nicht mitten im Satz erleben. Sie
// erfahren es in der Konsole selbst - beim Fund, bei später geöffneten gleich nach
// dem Öffnen, und noch einmal unmittelbar vor dem Wechsel.
func (c *Client) readyToSwitch(w *binaryWatch) bool {
	if !w.replaced() {
		return false
	}
	if w.pendingSince.IsZero() {
		w.pendingSince = time.Now()
	}
	if c.tty.count() > 0 {
		if time.Since(w.pendingSince) < c.timing.ConsoleWait {
			if !w.noticed {
				w.noticed = true
				c.tty.setNotice(switchPendingNotice(c.timing.ConsoleWait))
			}
			return false
		}
		if !w.noticedNow {
			w.noticedNow = true
			c.tty.setNotice(switchNowNotice())
		}
	}
	if !c.work.claimIdle(switchQuiet) {
		return false
	}
	c.tty.refuseNew(errSwitching)
	return true
}

// switchTarget bestimmt, was exec startet: Das Binary des Pakets startet über das
// Startskript, damit es den Start zählt und den Rückfall kennt (update.go);
// sonst (Selbstinstallation, Tests) das Binary selbst.
func (c *Client) switchTarget(path string) (string, []string) {
	if path == c.packagedPath && guardInstalled() {
		return guardScriptPath, append([]string{guardScriptPath}, os.Args[1:]...)
	}
	return path, os.Args
}

// disconnectWait ist die längste Zeit, die das Trennen vor dem exec hat. paho
// wartet bei einer hängenden Leitung auf den Broker; der Wechsel darf deshalb
// nicht von ihm abhängen - mit dem exec endet die Verbindung ohnehin.
const disconnectWait = time.Second

// disconnectWithin trennt cl sauber, wartet aber höchstens d darauf.
func disconnectWithin(cl mqtt.Client, d time.Duration) {
	if cl == nil {
		return
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		cl.Disconnect(sessionQuiesce)
	}()
	select {
	case <-done:
	case <-time.After(d):
	}
}

// logSwitch hält den Wechsel mit Grund im Journal fest. Warn, damit er auch bei
// log_level warn dort steht.
func (c *Client) logSwitch(path string) {
	c.log.Warn("disconnecting from lcm - switching to the new lcm-agent binary",
		"reason", "new binary installed", "path", path, "from_version", c.version)
}

// execNewBinary trennt sauber vom Broker und ersetzt den Prozess durch das
// neue Binary. Scheitert das exec, beendet sich der Agent - der
// Dienstverwalter startet ihn dann frisch.
func (c *Client) execNewBinary(path string) {
	c.logSwitch(path)
	// Bewusst kein Stop: Es ließe Run zurückkehren und den Prozess enden, bevor
	// das exec läuft. Ein gewolltes Trennen löst keinen Wiederaufbau aus.
	disconnectWithin(c.current(), disconnectWait)
	c.disarmWatchdog()
	target, argv := c.switchTarget(path)
	err := syscall.Exec(target, argv, os.Environ())
	if err != nil && target != path {
		c.log.Error("start script could not be executed - starting the binary directly", "error", err)
		err = syscall.Exec(path, os.Args, os.Environ())
	}
	c.log.Error("switch to the new binary failed - exiting", "error", err)
	os.Exit(1)
}
