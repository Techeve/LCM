package acmex

import (
	"crypto/x509"
	"math/big"
	"time"
)

// renewalTime ist der Zeitpunkt, ab dem LCM ein Zertifikat erneuert.
//
// Mit ARI-Antwort der CA liegt er im empfohlenen Fenster. Die Wahl im
// Fenster hängt von der Seriennummer des Zertifikats ab: Sie verteilt die
// Erneuerungen verschiedener Installationen über das Fenster (RFC 9773
// verlangt eine zufällige Wahl), bleibt aber von Lauf zu Lauf dieselbe, damit
// der angezeigte Zeitpunkt nicht springt.
//
// Ohne ARI erneuert LCM, sobald weniger als ein Drittel der Laufzeit übrig ist.
func renewalTime(leaf *x509.Certificate, win *window) time.Time {
	if win != nil && win.End.After(win.Start) {
		span := win.End.Sub(win.Start)
		offset := time.Duration(new(big.Int).Mod(leaf.SerialNumber, big.NewInt(int64(span))).Int64())
		return win.Start.Add(offset)
	}
	return leaf.NotAfter.Add(-leaf.NotAfter.Sub(leaf.NotBefore) / 3)
}
