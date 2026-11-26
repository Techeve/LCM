package agent

import (
	"encoding/json"
	"fmt"
	"unicode/utf8"

	"LCM/internal/remote/wire"
)

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
	// Truncated gehört in die gemessene Größe: Wer es erst nach der Suche setzte,
	// läge um die Länge von ,"truncated":true über dem Limit.
	res.Truncated = true
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
