// Package journal schreibt und liest die Tagesdateien von lcm-probe: eine
// Zeile JSON je Messung (JSON Lines), eine Datei je Tag.
//
// Jede Zeile wird mit einem einzigen write an die mit O_APPEND geöffnete
// Datei gehängt und danach mit fdatasync auf die Platte gebracht. Nach einem
// Absturz fehlt höchstens die angefangene letzte Zeile; der Leser überspringt
// sie. Die Datei ist reiner Text ohne Sperre und jederzeit kopierbar.
package journal

import (
	"encoding/json"
	"strings"
	"time"
)

// Zeilentypen.
const (
	TypeStart   = "start"   // lcm-probe ist gestartet
	TypeSources = "sources" // Herkunft der Hostwerte, am Anfang jeder Datei
	TypeHost    = "host"    // Hostwerte, alle 2 Sekunden
	TypeDB      = "db"      // Momentaufnahme der Datenbank, jede Minute
	TypeLCMDown = "lcm_down"
	TypeStop    = "stop"
)

// Head steht in jeder Zeile: Typ, Wanduhr und monotone Uhr in Millisekunden.
// Klafft die monotone Uhr zwischen zwei Zeilen, stand der Rechner oder der
// Container; springt nur die Wanduhr, wurde sie gestellt.
type Head struct {
	Type string `json:"t"`
	Wall int64  `json:"ts"`
	Mono int64  `json:"mono"`
}

// Time liefert die Wanduhr der Zeile.
func (h Head) Time() time.Time { return time.UnixMilli(h.Wall) }

// In sagt, ob die Zeile in [from, to] liegt; Nullwerte begrenzen nicht.
func (h Head) In(from, to time.Time) bool {
	t := h.Time()
	return (from.IsZero() || !t.Before(from)) && (to.IsZero() || !t.After(to))
}

// Entry ist eine gelesene Zeile: der Kopf und der ganze Text zum Weiterlesen.
type Entry struct {
	Head
	Raw json.RawMessage
}

// Decode liest die ganze Zeile in v.
func (e Entry) Decode(v any) error { return json.Unmarshal(e.Raw, v) }

const (
	filePrefix = "lcm-probe-"
	fileSuffix = ".jsonl"
	gzSuffix   = ".gz"
	dayLayout  = "2006-01-02"
)

// fileName liefert den Namen der Tagesdatei.
func fileName(day string) string { return filePrefix + day + fileSuffix }

// dayOf liest den Tag aus einem Dateinamen; ok=false für fremde Dateien.
func dayOf(name string) (day string, compressed, ok bool) {
	rest, found := strings.CutPrefix(name, filePrefix)
	if !found {
		return "", false, false
	}
	rest, compressed = strings.CutSuffix(rest, gzSuffix)
	day, found = strings.CutSuffix(rest, fileSuffix)
	if !found {
		return "", false, false
	}
	if _, err := time.ParseInLocation(dayLayout, day, time.Local); err != nil {
		return "", false, false
	}
	return day, compressed, true
}
