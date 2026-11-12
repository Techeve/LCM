package agent

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"LCM/internal/remote/wire"
)

// escapeHeavy ist Ausgabe, die beim JSON-Kodieren wächst: Anführungszeichen
// und Backslash (2 Bytes), Steuerzeichen und <>& (6 Bytes), ungültiges UTF-8
// (3 Bytes für U+FFFD).
func escapeHeavy(n int) []byte {
	pattern := []byte("\"\\\x1b[31m\x00<&>\xff\xfe\x01")
	return bytes.Repeat(pattern, n/len(pattern)+1)[:n]
}

// TestFitOutputBleibtUnterDemLimit: Das serialisierte Ergebnis passt unter
// das Limit, obwohl die Rohausgabe es nur knapp überschreitet und beim
// Kodieren um ein Vielfaches wächst. Anfang und Ende bleiben, dazwischen
// steht der Hinweis.
func TestFitOutputBleibtUnterDemLimit(t *testing.T) {
	const limit = 64 * 1024
	b := &limitedBuffer{max: 48 * 1024} // roh unter dem Limit, kodiert weit darüber
	_, _ = b.Write([]byte("ANFANG"))
	_, _ = b.Write(escapeHeavy(200 * 1024))
	_, _ = b.Write([]byte("ENDE"))

	res := wire.Result{ID: "id-1", ExitCode: 0}
	head, tail, omitted := b.parts()
	fitOutput(&res, head, tail, omitted, limit)

	payload, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	if len(payload) > limit {
		t.Fatalf("serialisiert %d Bytes, Limit %d", len(payload), limit)
	}
	if len(payload) < limit/2 {
		t.Fatalf("unnötig stark gekürzt: %d von %d Bytes", len(payload), limit)
	}
	if !res.Truncated {
		t.Fatal("Truncated fehlt")
	}
	if !strings.HasPrefix(res.Output, "ANFANG") || !strings.HasSuffix(res.Output, "ENDE") {
		t.Fatal("anfang oder ende der ausgabe fehlt")
	}
	if !strings.Contains(res.Output, "Bytes der Ausgabe ausgelassen") {
		t.Fatal("hinweis auf die lücke fehlt")
	}
	var back wire.Result
	if err := json.Unmarshal(payload, &back); err != nil || back.ID != "id-1" {
		t.Fatalf("ergebnis nicht lesbar: %v", err)
	}
}

// TestFitOutputLaesstKleineAusgabeUnveraendert: Passt das Ergebnis, bleibt
// die Ausgabe Byte für Byte, wie sie war.
func TestFitOutputLaesstKleineAusgabeUnveraendert(t *testing.T) {
	res := wire.Result{ID: "id"}
	fitOutput(&res, []byte("eins\n"), []byte("zwei\n"), 0, 1024)
	if res.Output != "eins\nzwei\n" || res.Truncated {
		t.Fatalf("got %q truncated=%v", res.Output, res.Truncated)
	}
}

// TestCutOutputSchneidetAnZeichengrenzen: Ein Schnitt zerteilt kein
// Mehrbyte-Zeichen.
func TestCutOutputSchneidetAnZeichengrenzen(t *testing.T) {
	text := []byte(strings.Repeat("äöü€", 10))
	for n := 0; n <= len(text); n++ {
		got := cutOutput(text, text, 0, n)
		if !utf8.ValidString(got) {
			t.Fatalf("n=%d: ungültiges UTF-8 %q", n, got)
		}
	}
}

// TestRunnerErgebnisPasstInsPaket: Ein echtes Kommando, das das Output-Limit
// mit Escape-Sequenzen füllt. Vorher wurden daraus über 24 MiB JSON - der
// Broker verwarf das Paket, und das Ergebnis kam nie an.
func TestRunnerErgebnisPasstInsPaket(t *testing.T) {
	if testing.Short() {
		t.Skip("erzeugt 5 MB Ausgabe")
	}
	r := NewRunner()
	res := r.Run(wire.Command{ID: "esc", Cmd: `head -c 5000000 /dev/zero | tr '\0' '\033'; echo SCHLUSS`}, nil)
	payload, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	if len(payload) > wire.MaxResultBytes {
		t.Fatalf("serialisiert %d Bytes, erlaubt %d", len(payload), wire.MaxResultBytes)
	}
	if !res.Truncated || !strings.HasSuffix(res.Output, "SCHLUSS\n") {
		t.Fatalf("ende fehlt oder Truncated nicht gesetzt (truncated=%v)", res.Truncated)
	}
}
