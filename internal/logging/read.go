package logging

import (
	"context"
	"io"
	"os"
	"strings"
	"time"
)

// Lesen der eigenen Logdatei für die Ereignis-Ansicht.
//
// Warum überhaupt: Ein Betreiber, dem etwas auffällt, hat heute nur den Weg
// über die Shell des LCM-Hosts - `journalctl -u lcm`. Wer LCM über die
// Oberfläche bedient, bekommt von seinen eigenen Störungen nichts mit. Genau
// die Meldungen, die eine Fehlersuche abkürzen („server unreachable",
// „queued job started", „degraded"), stehen an der einen Stelle, an die er
// nicht kommt.

// Entry ist eine gelesene Logzeile. Raw bleibt immer erhalten - nicht jede
// Zeile im Strom stammt von slog (Fiber-Banner, GORM-Meldungen), und eine
// Zeile zu verschlucken, weil sie nicht ins Schema passt, wäre in einer
// Fehlersuche das Schlechteste.
type Entry struct {
	Time  string `json:"time"`
	Level string `json:"level"`
	Msg   string `json:"msg"`
	Raw   string `json:"raw"`
}

// tailBytes begrenzt, wie weit rückwärts gelesen wird. Ein Megabyte fasst
// mehrere tausend Zeilen - mehr will niemand in einer Oberfläche durchsehen,
// und die Datei kann zehn Megabyte groß sein.
const tailBytes = 1 << 20

// Tail liefert die letzten Zeilen der Logdatei, jüngste zuletzt.
//
// minLevel filtert nach Schwere ("" = alles), query nach Textbestandteil
// (Groß-/Kleinschreibung wird ignoriert). Gefiltert wird NACH dem Lesen: Der
// Filter soll die Anzeige einschränken, nicht das Fenster, in dem gesucht
// wird - sonst lieferte eine Suche nach einem seltenen Wort eine leere Liste,
// obwohl der Treffer zwei Zeilen weiter oben stand.
func Tail(path string, limit int, minLevel, query string) ([]Entry, error) {
	lines, err := lastLines(path)
	if err != nil {
		return nil, err
	}
	min := ParseLevel(strings.ToLower(minLevel))
	q := strings.ToLower(query)

	out := make([]Entry, 0, limit)
	for _, line := range lines {
		e := parseLine(line)
		if minLevel != "" && e.Level != "" && ParseLevel(strings.ToLower(e.Level)) < min {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(line), q) {
			continue
		}
		out = append(out, e)
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

// lastLines liest den Schwanz der Datei und zerlegt ihn in Zeilen. Die erste
// (womöglich angeschnittene) Zeile fällt weg.
func lastLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	start := int64(0)
	if info.Size() > tailBytes {
		start = info.Size() - tailBytes
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if start > 0 && len(lines) > 0 {
		lines = lines[1:] // angeschnittene erste Zeile verwerfen
	}
	return lines, nil
}

// parseLine zieht Zeitstempel, Level und Meldung aus einer logfmt-Zeile.
// Passt sie nicht ins Schema, bleibt nur Raw gefüllt - siehe Entry.
func parseLine(line string) Entry {
	e := Entry{Raw: line}
	e.Time = field(line, "time=")
	e.Level = field(line, "level=")
	e.Msg = quotedField(line, "msg=")
	return e
}

// field liest einen unquotierten logfmt-Wert bis zum nächsten Leerzeichen.
func field(line, key string) string {
	i := strings.Index(line, key)
	if i < 0 {
		return ""
	}
	rest := line[i+len(key):]
	if j := strings.IndexByte(rest, ' '); j >= 0 {
		return rest[:j]
	}
	return rest
}

// quotedField liest einen logfmt-Wert, der in Anführungszeichen stehen kann.
func quotedField(line, key string) string {
	i := strings.Index(line, key)
	if i < 0 {
		return ""
	}
	rest := line[i+len(key):]
	if !strings.HasPrefix(rest, `"`) {
		return field(line, key)
	}
	rest = rest[1:]
	if j := strings.IndexByte(rest, '"'); j >= 0 {
		return rest[:j]
	}
	return rest
}

// followPoll ist der Abstand, in dem auf neue Zeilen gesehen wird. Bewusst
// abgefragt statt über einen Datei-Beobachter: Eine Sekunde ist für eine
// Live-Ansicht schnell genug, und es spart eine Abhängigkeit samt ihrer
// Plattform-Eigenheiten.
const followPoll = time.Second

// Follow ruft fn für jede neu angehängte Zeile auf, bis der Kontext endet.
// Begonnen wird am AKTUELLEN Ende: Der Verlauf kommt über Tail, hier geht es
// nur um das, was ab jetzt passiert.
//
// Rotiert die Datei unter der Hand (siehe rotate.go), fängt das Lesen von
// vorn an - erkennbar daran, dass sie plötzlich kürzer ist als die Stelle, an
// der wir stehen.
func Follow(ctx context.Context, path string, fn func(Entry)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	offset, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}

	ticker := time.NewTicker(followPoll)
	defer ticker.Stop()
	var rest string
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}

		info, err := os.Stat(path)
		if err != nil {
			continue // Rotation im Gange - beim nächsten Takt erneut
		}
		if info.Size() < offset {
			// Die Datei ist kürzer als unsere Position: Sie wurde rotiert.
			neu, err := os.Open(path)
			if err != nil {
				continue
			}
			f.Close()
			f, offset, rest = neu, 0, ""
		}
		if info.Size() == offset {
			continue
		}

		data := make([]byte, info.Size()-offset)
		n, err := f.ReadAt(data, offset)
		if n == 0 {
			continue
		}
		offset += int64(n)
		rest += string(data[:n])

		// Eine angefangene letzte Zeile bleibt liegen, bis ihr Zeilenumbruch
		// da ist - sonst käme sie zerhackt in der Oberfläche an.
		lines := strings.Split(rest, "\n")
		rest = lines[len(lines)-1]
		for _, line := range lines[:len(lines)-1] {
			if line != "" {
				fn(parseLine(line))
			}
		}
		if err != nil && err != io.EOF {
			return err
		}
	}
}
