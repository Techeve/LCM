package sshx

import (
	"fmt"
	"sync"
	"time"
)

// FakeDialer ist eine In-Memory-Dialer-Implementierung für Tests und den
// Demo-Modus. Sie kontaktiert nie echte Server, sondern liefert
// vorprogrammierte Antworten pro Kommando.
type FakeDialer struct {
	// mu schuetzt die Mitschriften (Commands/Stdins/DialedPorts/KeyPEMs/
	// LoginAuth). Noetig, weil der Executor Gruppen-Regeln parallel ueber
	// mehrere Server faehrt (ruleParallelism): Ohne Sperre schrieben mehrere
	// Goroutinen gleichzeitig in dieselben Slices - der Race-Detector
	// meldete das, und im schlimmsten Fall gingen Eintraege verloren,
	// wodurch Assertions ohne erkennbaren Grund fehlschlugen.
	mu sync.Mutex

	Fingerprint string
	KeyType     string
	// Responses bildet Kommando-Substrings auf (Output, ExitCode) ab.
	// Das erste passende Präfix gewinnt; Default ist ("", 0).
	Responses map[string]FakeResponse
	// FailProbe/FailPassword erzwingen Verbindungsfehler.
	FailProbe    error
	FailPassword error
	FailKey      error
	// FailKeyPort lässt DialKey NUR für diesen Port scheitern (0 = aus) -
	// für den Portwechsel-Test: die Verifikation auf dem neuen Port schlägt
	// fehl, die bestehende Verbindung auf dem alten Port bleibt möglich.
	FailKeyPort int
	// DialedPorts protokolliert die Ports aller DialKey-Aufrufe.
	DialedPorts []int
	// Commands zeichnet alle ausgeführten Kommandos auf (Test-Assertions).
	Commands []string
	// Delay verzögert jedes Kommando. Nur für Tests, die Gleichzeitigkeit
	// prüfen: Ohne Dauer ist der Fake so schnell, dass sich zwei Läufe nie
	// überlappen und ein Höchststand nichts aussagt.
	Delay time.Duration
	// PeakConns ist der Höchststand gleichzeitig offener Verbindungen,
	// openConns der laufende Zählstand (siehe openConn).
	PeakConns int
	openConns int
	// Stdins zeichnet den je Kommando eingespeisten stdin auf (parallel zu
	// Commands) - z.B. das an `sudo -S` übergebene Passwort. "" = kein stdin.
	Stdins []string
	// LoginAuth hält fest, mit welcher Methode der letzte Login-Dial erfolgte
	// ("password"|"key") - für Test-Assertions zum Onboarding-Auth-Zweig.
	LoginAuth string
	// KeyPEMs sammelt alle an DialKey übergebenen Private Keys in Reihenfolge
	// (KeyPEMs[0] ist der Login-Key beim Key-Onboarding).
	KeyPEMs []string
}

// FakeResponse ist die programmierte Antwort auf ein Kommando.
type FakeResponse struct {
	Output   string
	ExitCode int
}

// NewFakeDialer erstellt einen FakeDialer mit Standard-Fingerprint.
func NewFakeDialer() *FakeDialer {
	return &FakeDialer{
		Fingerprint: "SHA256:FAKEfakeFAKEfakeFAKEfakeFAKEfakeFAKEfake123",
		KeyType:     "ssh-ed25519",
		Responses:   map[string]FakeResponse{},
	}
}

func (d *FakeDialer) Probe(host string, port int) (string, string, error) {
	if d.FailProbe != nil {
		return "", "", d.FailProbe
	}
	return d.Fingerprint, d.KeyType, nil
}

func (d *FakeDialer) DialPassword(host string, port int, user, password, expectedFingerprint string) (Conn, error) {
	d.mu.Lock()
	d.LoginAuth = "password"
	d.mu.Unlock()
	if d.FailPassword != nil {
		// Wie der echte Client: einen "keine Passwort-Methode"-Fehler in die
		// klare Meldung übersetzen.
		if isNoPasswordAuthError(d.FailPassword) {
			return nil, ErrPasswordAuthUnavailable
		}
		return nil, d.FailPassword
	}
	if expectedFingerprint != "" && expectedFingerprint != d.Fingerprint {
		return nil, ErrHostKeyMismatch
	}
	d.openConn()
	return &fakeConn{dialer: d}, nil
}

func (d *FakeDialer) DialKey(host string, port int, user, privateKeyPEM, expectedFingerprint string) (Conn, error) {
	// Der Service-User-Key-Login (nach Provisionierung) läuft ebenfalls hier;
	// KeyPEMs[0] ist der Login-Key beim Key-Onboarding.
	d.mu.Lock()
	d.LoginAuth = "key"
	d.KeyPEMs = append(d.KeyPEMs, privateKeyPEM)
	d.DialedPorts = append(d.DialedPorts, port)
	d.mu.Unlock()
	if d.FailKey != nil {
		return nil, d.FailKey
	}
	if d.FailKeyPort != 0 && port == d.FailKeyPort {
		return nil, fmt.Errorf("fake: port %d nicht erreichbar", port)
	}
	if expectedFingerprint != "" && expectedFingerprint != d.Fingerprint {
		return nil, ErrHostKeyMismatch
	}
	d.openConn()
	return &fakeConn{dialer: d}, nil
}

type fakeConn struct {
	dialer     *FakeDialer
	closed     bool
	onActivity func()
}

// OnActivity hinterlegt den Lebenszeichen-Rückruf; die Fake-Verbindung löst
// ihn wie die echte zu Beginn und Ende jedes Kommandos aus.
func (c *fakeConn) OnActivity(fn func()) { c.onActivity = fn }

func (c *fakeConn) activity() {
	if c.onActivity != nil {
		c.onActivity()
	}
}

func (c *fakeConn) Run(cmd string) (string, int, error) {
	return c.RunStdin(cmd, "")
}

// RunStdin liefert die Antwort des Schlüssels, dessen Muster im Kommando
// vorkommt. Passen mehrere, gewinnt der LÄNGSTE - also der spezifischste.
//
// Diese Regel ist wichtig: Go iteriert Maps in zufälliger Reihenfolge, „der
// erste Treffer gewinnt" wäre bei überlappenden Mustern also nicht
// reproduzierbar (z.B. "sudo -n" und "sudo -n id -u"). Mit der Längenregel
// kann ein Test einen allgemeinen Standard setzen und einzelne Kommandos
// gezielt davon ausnehmen.
func (c *fakeConn) RunStdin(cmd, stdin string) (string, int, error) {
	c.activity()
	defer c.activity()
	c.dialer.record(cmd, stdin)
	if d := c.dialer.Delay; d > 0 {
		time.Sleep(d)
	}
	best, found := "", false
	for pattern := range c.dialer.Responses {
		if contains(cmd, pattern) && (!found || len(pattern) > len(best)) {
			best, found = pattern, true
		}
	}
	if found {
		resp := c.dialer.Responses[best]
		return resp.Output, resp.ExitCode, nil
	}
	return "", 0, nil
}

func (c *fakeConn) Close() error {
	if !c.closed {
		c.closed = true
		c.dialer.closeConn()
	}
	return nil
}

// openConn/closeConn führen mit, wie viele Verbindungen GLEICHZEITIG offen
// sind, und merken sich den Höchststand. Damit lässt sich prüfen, dass eine
// Parallelitätsgrenze auch wirklich greift - ohne den Höchststand wäre nur zu
// sehen, dass am Ende alles gelaufen ist, nicht wie gedrängt.
func (d *FakeDialer) openConn() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.openConns++
	if d.openConns > d.PeakConns {
		d.PeakConns = d.openConns
	}
}

func (d *FakeDialer) closeConn() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.openConns--
}

// Peak liefert den Höchststand gleichzeitig offener Verbindungen.
func (d *FakeDialer) Peak() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.PeakConns
}

// contains ist eine minimale Substring-Prüfung (Zero-Bloat).
func contains(haystack, needle string) bool {
	if needle == "" {
		return false
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// assert, dass FakeDialer das Interface erfüllt.
var _ Dialer = (*FakeDialer)(nil)

// String macht FakeResponse im Test-Log lesbar.
func (r FakeResponse) String() string { return fmt.Sprintf("exit=%d out=%q", r.ExitCode, r.Output) }

// record schreibt ein ausgefuehrtes Kommando samt stdin in die Mitschrift.
func (d *FakeDialer) record(cmd, stdin string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.Commands = append(d.Commands, cmd)
	d.Stdins = append(d.Stdins, stdin)
}

// Recorded liefert eine Kopie der Mitschriften. Tests, die waehrend eines
// laufenden Gruppen-Laufs lesen, muessen diese Methode nutzen statt direkt
// auf die Slices zuzugreifen.
func (d *FakeDialer) Recorded() (commands, stdins []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.Commands...), append([]string(nil), d.Stdins...)
}

// Reset leert die Mitschriften. Tests, die einen zweiten Abschnitt getrennt
// beobachten wollen, muessen das hierueber tun statt die Slices direkt zu
// nullen - sonst schreibt ein noch laufender nebenlaeufiger Abgleich
// gleichzeitig hinein.
func (d *FakeDialer) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.Commands = nil
	d.Stdins = nil
}
