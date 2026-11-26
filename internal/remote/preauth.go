package remote

import (
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Grenzen für Verbindungen, die sich noch nicht angemeldet haben (S-07). Ein
// Unbekannter darf einen WebSocket-Upgrade machen, ohne je ein MQTT-CONNECT zu
// senden. Ohne diese Grenzen hielte jede solche Verbindung eine Goroutine, Puffer
// und einen Socket, bis er sie schließt: mochi setzt die Lese-Frist erst nach dem
// CONNECT (Keepalive des Clients, 0 heißt keine Frist), und das erste Paket darf
// so groß sein wie jedes andere (wire.MaxPacketSize).
const (
	// preAuthDeadline ist die Zeit vom Upgrade bis zum vollständigen CONNECT.
	preAuthDeadline = 10 * time.Second
	// preAuthMaxPacket ist die größte Länge des CONNECT-Pakets (ohne Kopf).
	// Ein CONNECT trägt Protokollname, Client-ID, Benutzer, Passwort und das
	// Testament des Agents - gemessen unter 300 Byte.
	preAuthMaxPacket = 8 * 1024
	// preAuthMaxConns ist die Zahl der Verbindungen, die gleichzeitig ohne
	// Anmeldung offen sein dürfen. Eine Anmeldung dauert Millisekunden.
	preAuthMaxConns = 64
)

// errPreAuthTooLarge: Das erste Paket ist für ein CONNECT zu groß.
var errPreAuthTooLarge = errors.New("first mqtt packet too large for connect")

// errPreAuthFull: Zu viele Verbindungen warten auf ihre Anmeldung.
var errPreAuthFull = errors.New("too many connections without login")

// preAuthLimits sind die Grenzen; die Vorgaben stehen oben, Tests setzen kürzere.
type preAuthLimits struct {
	deadline  time.Duration
	maxPacket int
	maxConns  int64
}

func defaultPreAuthLimits() preAuthLimits {
	return preAuthLimits{deadline: preAuthDeadline, maxPacket: preAuthMaxPacket, maxConns: preAuthMaxConns}
}

// preAuthGate zählt die Verbindungen ohne Anmeldung.
type preAuthGate struct {
	limits preAuthLimits
	open   atomic.Int64
}

// full meldet, ob keine weitere unangemeldete Verbindung mehr Platz hat. Der
// WebSocket-Handler fragt es vor dem Upgrade, um mit 503 abzuweisen.
func (g *preAuthGate) full() bool { return g.open.Load() >= g.limits.maxConns }

// enter umhüllt die Verbindung und zählt sie mit; false, wenn kein Platz ist.
func (g *preAuthGate) enter(c net.Conn) (*preAuthConn, bool) {
	if g.open.Add(1) > g.limits.maxConns {
		g.open.Add(-1)
		return nil, false
	}
	return &preAuthConn{Conn: c, gate: g}, true
}

// preAuthConn ist die Verbindung bis zur Anmeldung: Sie prüft die Länge des
// ersten MQTT-Pakets, bevor der Broker dafür einen Puffer anlegt, und gibt ihren
// Platz im preAuthGate frei, sobald der Agent angemeldet ist oder die Verbindung
// endet. Die Lese-Frist setzt HandleConn; mochi löst sie nach dem CONNECT durch
// die Frist des Keepalive ab.
type preAuthConn struct {
	net.Conn
	gate *preAuthGate
	done sync.Once

	// head sammelt den Kopf des ersten Pakets (Typ und Längenfeld); nur Read
	// berührt ihn. checked: Die Länge ist geprüft, mehr gibt es nicht zu prüfen.
	head    []byte
	checked bool
}

// release gibt den Platz frei (einmal, egal von welcher Seite).
func (c *preAuthConn) release() {
	c.done.Do(func() { c.gate.open.Add(-1) })
}

func (c *preAuthConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 && !c.checked {
		if tooLarge := c.inspect(p[:n]); tooLarge != nil {
			return 0, tooLarge
		}
	}
	return n, err
}

// maxLengthBytes ist die Länge des Längenfelds im MQTT-Kopf (höchstens vier Byte).
const maxLengthBytes = 4

// inspect liest das Längenfeld des ersten Pakets mit. Ist es vollständig und das
// Paket zu groß, kommt der Fehler zurück, bevor der Broker die Länge sieht. Das
// Feld darf über mehrere Reads verteilt ankommen.
func (c *preAuthConn) inspect(chunk []byte) error {
	for _, b := range chunk {
		c.head = append(c.head, b)
		field := c.head[1:] // das erste Byte ist der Pakettyp
		if len(field) == 0 || b&0x80 != 0 {
			if len(field) >= maxLengthBytes && b&0x80 != 0 {
				c.checked = true // ungültiges Längenfeld: das lehnt der Broker ab
				return nil
			}
			continue
		}
		c.checked = true
		if remaining(field) > c.gate.limits.maxPacket {
			return errPreAuthTooLarge
		}
		return nil
	}
	return nil
}

// remaining dekodiert das Längenfeld (7 Bit je Byte, niedrigste zuerst).
func remaining(field []byte) int {
	n, shift := 0, 0
	for _, b := range field {
		n |= int(b&0x7f) << shift
		shift += 7
	}
	return n
}

// authenticated meldet dem Gate, dass der Agent angemeldet ist.
func (c *preAuthConn) authenticated() { c.release() }
