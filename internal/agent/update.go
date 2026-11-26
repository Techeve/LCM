package agent

import (
	"bufio"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Rückfall auf das vorige Binary (Paket D-4).
//
// Ein defektes neues Binary kann sich nicht selbst zurückrollen, es startet
// womöglich nicht einmal. Deshalb liegt der Rückfall im Startskript des Dienstes
// (packaging/lcm-agent-start.sh): Es zählt die Starts, die nicht „gesund"
// wurden, und startet nach drei davon das vorige Binary. Der Agent liefert dem
// Skript zwei Dinge: Er meldet sich gesund (healthy: bereit und stabil, Zähler
// auf 0) und sagt von sich aus „nicht bereit" (failed: Zähler auf die Grenze,
// das Skript rollt beim nächsten Start sofort zurück). Beide teilen sich ein
// Verzeichnis mit einfachen Dateien:
//
//	lcm-agent.prev  das vorige, bewährte Binary
//	starts          Starts in Folge ohne „gesund"
//	mode            normal | prev - welches Binary läuft
//	cycles          wie oft beide nacheinander scheiterten (Wartezeit)
//	fail-reason     Grund, den der Agent beim Aufgeben nennt
//	rolled-back     from= to= reason= at= - solange das vorige Binary läuft
//	bin-id          nur das Skript: welches /usr/bin/lcm-agent der Stand meint
//
// Das vorige Binary legt zuerst das preinst-Skript des neuen Pakets ab (nur es
// sieht das alte, bevor der Paketmanager es ersetzt; ein alter Agent kann sich
// nicht selbst sichern), danach jeder gesunde Agent von sich aus.

// UpdateDir ist das Verzeichnis dieses Zustands. Eine Variable, damit Tests es
// umlenken.
var UpdateDir = UpdateDirDefault

// UpdateDirDefault ist der Ort im Betrieb (auch im Startskript).
const UpdateDirDefault = "/var/lib/lcm-agent/update"

const (
	prevBinaryName = "lcm-agent.prev"
	// maxStarts muss zu MAX_STARTS im Startskript passen: So viele ungesunde
	// Starts in Folge sind erlaubt, der nächste rollt zurück.
	maxStarts = 3
	// guardScriptPath ist das Startskript des Pakets.
	guardScriptPath = "/usr/lib/lcm-agent/lcm-agent-start"
)

// Gründe, die ein Rückfall nennt (rolled-back, reason=).
const (
	reasonCrashLoop = "crash-loop" // das Skript zählte drei Starts in Folge ohne „gesund"
	reasonNotReady  = "not-ready"  // der Agent war nach der Frist nicht bereit und gab selbst auf
)

// RolledBack beschreibt einen Rückfall: Das neue Binary kam nicht hoch, es läuft
// das vorige.
type RolledBack struct {
	// From ist die Version des gescheiterten, To die des laufenden Binarys
	// („unknown", wenn sie sich nicht ermitteln ließ).
	From   string `json:"from"`
	To     string `json:"to"`
	Reason string `json:"reason"`
	// At ist der Zeitpunkt (Unix-Sekunden).
	At int64 `json:"at,omitempty"`
}

// updateState liest und schreibt das Verzeichnis.
type updateState struct{ dir string }

func (s updateState) path(name string) string { return filepath.Join(s.dir, name) }

func (s updateState) prevPath() string { return s.path(prevBinaryName) }

// starts liefert den Zähler (0, wenn er fehlt oder unbrauchbar ist).
func (s updateState) starts() int {
	n, err := strconv.Atoi(strings.TrimSpace(s.read("starts")))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// mode ist „normal" oder „prev".
func (s updateState) mode() string {
	if strings.TrimSpace(s.read("mode")) == "prev" {
		return "prev"
	}
	return "normal"
}

func (s updateState) read(name string) string {
	b, err := os.ReadFile(s.path(name))
	if err != nil {
		return ""
	}
	return string(b)
}

// write schreibt eine Datei atomar (0600) und legt das Verzeichnis (0700) an.
func (s updateState) write(name, content string) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	tmp := s.path(name + ".tmp")
	if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path(name))
}

// healthy trägt ein, dass dieser Agent bereit und stabil war: kein ungesunder
// Start mehr, keine Wartezeit mehr. Ein Agent, der nicht das vorige Binary ist,
// ist außerdem der „normale": Ein alter Rückfall gilt nicht mehr.
func (s updateState) healthy(asPrev bool) error {
	_ = os.Remove(s.path("cycles"))
	_ = os.Remove(s.path("fail-reason"))
	if !asPrev {
		_ = os.Remove(s.path("rolled-back"))
	}
	return s.write("starts", "0\n")
}

// failed trägt ein, dass dieser Agent aufgibt: Das Startskript rollt beim
// nächsten Start sofort zurück.
func (s updateState) failed(reason string) error {
	if err := s.write("fail-reason", reason+"\n"); err != nil {
		return err
	}
	return s.write("starts", strconv.Itoa(maxStarts)+"\n")
}

// rolledBack liest den Rückfall-Vermerk des Startskripts (nil: keiner).
func (s updateState) rolledBack() *RolledBack {
	f, err := os.Open(s.path("rolled-back"))
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	rb := RolledBack{From: "unknown", To: "unknown", Reason: reasonCrashLoop}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		key, val, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
		if !ok {
			continue
		}
		switch key {
		case "from":
			rb.From = val
		case "to":
			rb.To = val
		case "reason":
			rb.Reason = val
		case "at":
			rb.At, _ = strconv.ParseInt(val, 10, 64)
		}
	}
	return &rb
}

// savePrev legt eine Kopie des Binarys src als vorigen Rückfall ab, wenn dort
// nicht schon dasselbe liegt. Gibt zurück, ob kopiert wurde.
func (s updateState) savePrev(src string) (bool, error) {
	same, err := sameContent(src, s.prevPath())
	if err != nil || same {
		return false, err
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return false, err
	}
	if err := copyFile(src, s.prevPath(), 0o755); err != nil {
		return false, err
	}
	return true, nil
}

// prevDiffers meldet, ob es ein vorheriges Binary gibt, das nicht dasselbe ist
// wie src: Nur dann ist src „unbewährt", und nur dann gibt es einen Rückfall.
func (s updateState) prevDiffers(src string) bool {
	if _, err := os.Stat(s.prevPath()); err != nil {
		return false
	}
	same, err := sameContent(src, s.prevPath())
	return err == nil && !same
}

// sameContent vergleicht zwei Dateien über ihren SHA-256. Fehlt b, sind sie
// nicht gleich (kein Fehler).
func sameContent(a, b string) (bool, error) {
	hb, err := fileHash(b)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	ha, err := fileHash(a)
	if err != nil {
		return false, err
	}
	return ha == hb, nil
}

func fileHash(path string) ([32]byte, error) {
	var sum [32]byte
	f, err := os.Open(path)
	if err != nil {
		return sum, err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return sum, err
	}
	copy(sum[:], h.Sum(nil))
	return sum, nil
}

// guardInstalled meldet, ob das Startskript des Pakets da ist - und damit der
// Rückfall.
func guardInstalled() bool {
	fi, err := os.Stat(guardScriptPath)
	return err == nil && fi.Mode().IsRegular() && fi.Mode()&0o111 != 0
}
