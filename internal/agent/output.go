package agent

import (
	"encoding/json"
	"fmt"
	"sync"
	"unicode/utf8"

	"LCM/internal/remote/wire"
)

// limitedBuffer sammelt Output bis max Bytes: die erste Hälfte vom Anfang,
// die zweite vom Ende. Was dazwischen anfällt, wird nur gezählt. Bei apt
// steht das Wichtige vorn (was ansteht) und hinten (Fehler, Zusammenfassung).
type limitedBuffer struct {
	mu      sync.Mutex
	max     int
	head    []byte
	tail    []byte // hält bis zum Doppelten von tailMax, siehe appendTail
	written int    // alle geschriebenen Bytes, auch die verworfenen
}

// Written ist die Zahl ALLER bisher geschriebenen Bytes - auch der wegen des
// Limits verworfenen. Daran erkennt watch, ob das Kommando noch arbeitet; die
// Länge des Puffers taugt dafür nicht, weil sie nach dem Kürzen stehen bliebe
// und ein weiterhin arbeitendes Kommando als hängend erscheinen ließe.
func (b *limitedBuffer) Written() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.written
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.written += len(p)
	rest := p
	if room := b.max/2 - len(b.head); room > 0 {
		n := min(room, len(rest))
		b.head = append(b.head, rest[:n]...)
		rest = rest[n:]
	}
	b.appendTail(rest)
	// Immer len(p) melden - der Schreiber (das Kommando) soll weiterlaufen.
	return len(p), nil
}

// tailMax ist der Platz für das Ende der Ausgabe.
func (b *limitedBuffer) tailMax() int { return b.max - b.max/2 }

// appendTail hängt p an das Ende an. Gekürzt wird erst beim Doppelten des
// Platzes: Wer bei jedem Write vorn abschnitte, verschöbe bei jeder Zeile
// Megabytes.
func (b *limitedBuffer) appendTail(p []byte) {
	b.tail = append(b.tail, p...)
	if keep := b.tailMax(); len(b.tail) > 2*keep {
		b.tail = append(b.tail[:0], b.tail[len(b.tail)-keep:]...)
	}
}

// parts liefert Anfang und Ende der Ausgabe und die Zahl der Bytes, die
// dazwischen fehlen.
func (b *limitedBuffer) parts() (head, tail []byte, omitted int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	tail = b.tail
	if keep := b.tailMax(); len(tail) > keep {
		tail = tail[len(tail)-keep:]
	}
	return b.head, tail, b.written - len(b.head) - len(tail)
}

// String ist die gesammelte Ausgabe samt Hinweis auf die Lücke.
func (b *limitedBuffer) String() string {
	head, tail, omitted := b.parts()
	return joinOutput(head, tail, omitted)
}

// fitOutput setzt die Ausgabe so in res ein, dass das serialisierte Ergebnis
// höchstens limit Bytes groß ist. Gemessen wird das JSON, nicht die
// Rohausgabe: Ein Steuerzeichen wächst beim Kodieren auf sechs Bytes
// (\u001b), ein Byte ungültiges UTF-8 auf drei (U+FFFD). 4 MiB Farbcodes
// wären 24 MiB - der Broker verwürfe das Paket, die Verbindung bräche ab.
//
// Gekürzt wird in der Mitte, Anfang und Ende bleiben gleich lang stehen. Die
// passende Länge findet eine binäre Suche über echte Serialisierungen; das
// kostet nur bei übergroßer Ausgabe etwas und hängt an keiner Nachbildung
// der Escape-Regeln von encoding/json.
func fitOutput(res *wire.Result, head, tail []byte, omitted, limit int) {
	res.Output = joinOutput(head, tail, omitted)
	res.Truncated = omitted > 0
	if resultSize(*res) <= limit {
		return
	}
	lo, hi := 0, max(len(head), len(tail))
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if resultSize(withOutput(*res, cutOutput(head, tail, omitted, mid))) <= limit {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	res.Output = cutOutput(head, tail, omitted, lo)
	res.Truncated = true
}

// cutOutput behält höchstens n Bytes vom Anfang und n vom Ende, jeweils an
// einer Zeichengrenze, und vermerkt, wie viel insgesamt fehlt.
func cutOutput(head, tail []byte, omitted, n int) string {
	h := head[:runeBoundaryBack(head, min(n, len(head)))]
	t := tail[runeBoundaryForward(tail, len(tail)-min(n, len(tail))):]
	gone := omitted + len(head) - len(h) + len(tail) - len(t)
	return joinOutput(h, t, gone)
}

// joinOutput setzt Anfang und Ende zusammen, mit einem Hinweis dazwischen,
// wenn etwas fehlt.
func joinOutput(head, tail []byte, omitted int) string {
	if omitted <= 0 {
		return string(head) + string(tail)
	}
	return fmt.Sprintf("%s\n[lcm-agent: %d Bytes der Ausgabe ausgelassen]\n%s", head, omitted, tail)
}

// runeBoundaryBack rückt einen Schnitt bei i nach vorn auf den Anfang des
// Zeichens, in dem er liegt (höchstens drei Bytes - länger ist kein UTF-8).
func runeBoundaryBack(b []byte, i int) int {
	for steps := 0; i > 0 && i < len(b) && !utf8.RuneStart(b[i]) && steps < utf8.UTFMax-1; steps++ {
		i--
	}
	return i
}

// runeBoundaryForward rückt einen Schnitt bei i nach hinten auf den Anfang
// des nächsten Zeichens.
func runeBoundaryForward(b []byte, i int) int {
	for steps := 0; i < len(b) && !utf8.RuneStart(b[i]) && steps < utf8.UTFMax-1; steps++ {
		i++
	}
	return i
}

func withOutput(res wire.Result, output string) wire.Result {
	res.Output = output
	return res
}

// resultSize ist die Größe des Ergebnisses, wie es über MQTT geht.
func resultSize(res wire.Result) int {
	payload, _ := json.Marshal(res) // nur Strings, Zahlen, bools: scheitert nicht
	return len(payload)
}
