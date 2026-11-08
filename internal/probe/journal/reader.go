package journal

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"os"
	"time"
)

// maxLine begrenzt eine Zeile; eine Momentaufnahme der Datenbank hat
// einige zehn Kilobyte.
const maxLine = 16 << 20

// Stats zählt, was der Leser gesehen hat.
type Stats struct {
	Files   int
	Lines   int
	Skipped int // unvollständige oder unlesbare Zeilen
}

// Read liest alle Zeilen der Tagesdateien, die den Zeitraum [from, to]
// berühren, gepackt oder nicht, in der Reihenfolge der Tage. Es liefert
// ganze Dateien: Eine Momentaufnahme der Datenbank braucht die Zeilen davor
// (siehe Expander). Auf den Zeitraum filtert der Aufrufer mit Head.In. Ein
// Nullwert für from oder to begrenzt nicht.
func Read(dir string, from, to time.Time, fn func(Entry) error) (Stats, error) {
	var st Stats
	for _, f := range listFiles(dir) {
		if !dayInRange(f.day, from, to) {
			continue
		}
		st.Files++
		if err := readFile(f, &st, fn); err != nil {
			return st, err
		}
	}
	return st, nil
}

// dayInRange lässt einen Tag Rand zu: Die Datei eines Tages kann Zeilen
// knapp nach Mitternacht enthalten, wenn der Wechsel spät kam.
func dayInRange(day string, from, to time.Time) bool {
	d, err := time.ParseInLocation(dayLayout, day, time.Local)
	if err != nil {
		return false
	}
	if !from.IsZero() && d.AddDate(0, 0, 2).Before(from) {
		return false
	}
	return to.IsZero() || !d.AddDate(0, 0, -1).After(to)
}

func readFile(f dayFile, st *Stats, fn func(Entry) error) error {
	file, err := os.Open(f.path)
	if err != nil {
		return err
	}
	defer file.Close()
	var r io.Reader = file
	if f.compressed {
		zr, err := gzip.NewReader(file)
		if err != nil {
			return err
		}
		defer zr.Close()
		r = zr
	}
	br := bufio.NewReaderSize(r, 64<<10)
	for {
		line, err := readLine(br)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			st.Skipped++
			if errors.Is(err, errIncomplete) {
				return nil
			}
			continue
		}
		var e Entry
		if json.Unmarshal(line, &e.Head) != nil || e.Type == "" {
			st.Skipped++
			continue
		}
		st.Lines++
		e.Raw = line
		if err := fn(e); err != nil {
			return err
		}
	}
}

var (
	errIncomplete = errors.New("unvollständige letzte Zeile")
	errTooLong    = errors.New("zeile zu lang")
)

// readLine liest bis zum Zeilenende. Fehlt es am Dateiende, ist die Zeile
// unvollständig - lcm-probe wurde beim Schreiben unterbrochen.
func readLine(br *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		chunk, err := br.ReadSlice('\n')
		line = append(line, chunk...)
		switch {
		case err == nil:
			return line[:len(line)-1], nil
		case errors.Is(err, bufio.ErrBufferFull):
			if len(line) > maxLine {
				return skipRest(br)
			}
		case errors.Is(err, io.EOF) && len(line) > 0:
			return nil, errIncomplete
		default:
			return nil, err
		}
	}
}

func skipRest(br *bufio.Reader) ([]byte, error) {
	if _, err := br.ReadBytes('\n'); err != nil {
		return nil, errIncomplete
	}
	return nil, errTooLong
}

// Latest liefert die Wanduhr der jüngsten Zeile im Verzeichnis, ok=false
// ohne Zeilen. Gelesen wird nur die jüngste Tagesdatei.
func Latest(dir string) (time.Time, bool) {
	files := listFiles(dir)
	if len(files) == 0 {
		return time.Time{}, false
	}
	var latest int64
	var st Stats
	_ = readFile(files[len(files)-1], &st, func(e Entry) error {
		latest = max(latest, e.Wall)
		return nil
	})
	return time.UnixMilli(latest), latest > 0
}
