package sshx

import (
	"fmt"
	"io"
	"sync"

	"golang.org/x/crypto/ssh"
)

// Interaktive Terminal-Sitzungen (Web-Konsole).
//
// Bis hierher kannte diese Schicht nur „ein Kommando, ein Ergebnis": Run
// schickt eine Zeile, wartet, liefert Ausgabe und Exit-Code. Ein Terminal ist
// das Gegenteil davon - eine offene Sitzung mit Pseudo-Terminal, in der Ein-
// und Ausgabe unabhängig voneinander fließen und die Gegenseite wissen muss,
// wie groß das Fenster ist.

// Terminal ist eine laufende interaktive Sitzung. Lesen liefert, was auf dem
// Bildschirm erschiene; Schreiben sind die Tastenanschläge.
type Terminal interface {
	io.ReadWriteCloser
	// Resize meldet der Gegenseite eine neue Fenstergröße. Ohne das rechnet
	// sie weiter mit 80×24, und alles, was den Bildschirm selbst aufteilt
	// (top, less, vim), zeichnet an der falschen Stelle.
	Resize(cols, rows int) error
}

// TerminalConn ist die optionale Fähigkeit einer Verbindung, ein Terminal zu
// öffnen.
//
// Bewusst NICHT Teil von Conn: Der Agent-Transport (MQTT, Frage und Antwort)
// kann keinen Strom führen. Ein Interface, das ihn zu einer Attrappe zwänge,
// würde genau den Unterschied verschweigen, auf den es hier ankommt - der
// Aufrufer prüft die Fähigkeit und sagt dem Benutzer sonst klar, dass es auf
// diesem Server keine Konsole gibt.
type TerminalConn interface {
	Terminal(term string, cols, rows int) (Terminal, error)
}

// terminalSession ist die Umsetzung über eine SSH-Session mit PTY.
type terminalSession struct {
	session *ssh.Session
	stdin   io.WriteCloser
	stdout  io.Reader

	mu     sync.Mutex
	closed bool
}

// Terminal öffnet eine interaktive Sitzung mit Pseudo-Terminal.
func (c *clientConn) Terminal(term string, cols, rows int) (Terminal, error) {
	session, err := c.client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("ssh-session: %w", err)
	}
	if term == "" {
		term = "xterm-256color"
	}
	cols, rows = clampWindow(cols, rows)

	// ECHO an: Die Gegenseite spiegelt die Tastenanschläge, wie in jedem
	// Terminal. Die Baudraten sind Konvention - sie beeinflussen nichts, aber
	// manche Server erwarten sie in der Anfrage.
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := session.RequestPty(term, rows, cols, modes); err != nil {
		session.Close()
		return nil, fmt.Errorf("pty anfordern: %w", err)
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		session.Close()
		return nil, fmt.Errorf("stdin: %w", err)
	}
	// Nur stdout lesen: Mit einem PTY laufen stdout und stderr der Gegenseite
	// ohnehin über dasselbe Terminal zusammen - genau wie an einer echten
	// Konsole.
	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		return nil, fmt.Errorf("stdout: %w", err)
	}
	if err := session.Shell(); err != nil {
		session.Close()
		return nil, fmt.Errorf("shell starten: %w", err)
	}
	return &terminalSession{session: session, stdin: stdin, stdout: stdout}, nil
}

func (t *terminalSession) Read(p []byte) (int, error)  { return t.stdout.Read(p) }
func (t *terminalSession) Write(p []byte) (int, error) { return t.stdin.Write(p) }

func (t *terminalSession) Resize(cols, rows int) error {
	cols, rows = clampWindow(cols, rows)
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil
	}
	return t.session.WindowChange(rows, cols)
}

// Close beendet die Sitzung. Erst stdin schließen: Eine Shell beendet sich
// daraufhin von selbst und räumt ihre Kindprozesse ab; ein sofortiges
// Abwürgen der Session ließe sie unter Umständen stehen.
func (t *terminalSession) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	t.mu.Unlock()

	_ = t.stdin.Close()
	return t.session.Close()
}

// Fenstergrenzen. Ein Browser meldet gelegentlich 0 (Tab im Hintergrund) oder
// absurde Werte; beides an die Gegenseite durchzureichen, macht die Anzeige
// dort kaputt.
const (
	minWindow  = 1
	maxCols    = 500
	maxRows    = 300
	defaultCol = 80
	defaultRow = 24
)

func clampWindow(cols, rows int) (int, int) {
	if cols < minWindow {
		cols = defaultCol
	}
	if rows < minWindow {
		rows = defaultRow
	}
	return min(cols, maxCols), min(rows, maxRows)
}
