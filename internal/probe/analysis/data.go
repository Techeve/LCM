// Package analysis wertet die Tagesdateien von lcm-probe aus: Textbericht,
// HTML mit Diagrammen und Abfrage einzelner Felder. Es läuft überall, wo die
// Dateien liegen, auch auf einem Mac mit kopierten Tagen.
package analysis

import (
	"encoding/json"
	"slices"
	"time"

	"LCM/internal/probe/journal"
)

// Data sind die Zeilen eines Zeitraums, nach Typ getrennt.
type Data struct {
	From, To time.Time
	Stats    journal.Stats
	Starts   []journal.StartLine
	Stops    []journal.StopLine
	Sources  []journal.SourcesLine
	Hosts    []journal.HostLine
	DBs      []DBPoint
	Downs    []journal.DownLine
}

// DBPoint ist eine vollständige Momentaufnahme der Datenbank.
type DBPoint struct {
	journal.Head
	FetchMS float64
	Snap    snapshot
}

// Load liest die Zeilen in [from, to] aus dir.
func Load(dir string, from, to time.Time) (*Data, error) {
	d := &Data{From: from, To: to}
	var expander journal.Expander
	st, err := journal.Read(dir, from, to, func(e journal.Entry) error {
		// Auch Momentaufnahmen vor dem Zeitraum gehen durch den Expander:
		// Die Zeilen im Zeitraum bauen auf ihnen auf.
		if e.Type == journal.TypeDB {
			return d.addSnapshot(e, &expander, e.In(from, to))
		}
		if !e.In(from, to) {
			return nil
		}
		return d.add(e)
	})
	d.Stats = st
	if err != nil {
		return nil, err
	}
	d.sort()
	return d, nil
}

func (d *Data) add(e journal.Entry) error {
	switch e.Type {
	case journal.TypeHost:
		return appendDecoded(e, &d.Hosts)
	case journal.TypeStart:
		return appendDecoded(e, &d.Starts)
	case journal.TypeStop:
		return appendDecoded(e, &d.Stops)
	case journal.TypeSources:
		return appendDecoded(e, &d.Sources)
	case journal.TypeLCMDown:
		return appendDecoded(e, &d.Downs)
	}
	return nil // unbekannte Zeilen späterer Fassungen
}

// appendDecoded hängt eine Zeile an; unlesbare zählen nicht als Fehler,
// der Bericht soll trotzdem entstehen.
func appendDecoded[T any](e journal.Entry, list *[]T) error {
	var v T
	if e.Decode(&v) == nil {
		*list = append(*list, v)
	}
	return nil
}

func (d *Data) addSnapshot(e journal.Entry, expander *journal.Expander, inRange bool) error {
	var line journal.DBLine
	if e.Decode(&line) != nil {
		return nil
	}
	full, err := expander.Expand(line.DB)
	if err != nil || !inRange {
		return nil
	}
	p := DBPoint{Head: line.Head, FetchMS: line.FetchMS}
	if json.Unmarshal(full, &p.Snap) == nil {
		d.DBs = append(d.DBs, p)
	}
	return nil
}

// sort ordnet nach Wanduhr; Dateien kommen nach Tagen, innerhalb eines Tages
// ist die Reihenfolge schon die des Schreibens.
func (d *Data) sort() {
	slices.SortStableFunc(d.Hosts, func(a, b journal.HostLine) int { return cmpWall(a.Head, b.Head) })
	slices.SortStableFunc(d.DBs, func(a, b DBPoint) int { return cmpWall(a.Head, b.Head) })
}

func cmpWall(a, b journal.Head) int {
	switch {
	case a.Wall < b.Wall:
		return -1
	case a.Wall > b.Wall:
		return 1
	}
	return 0
}

// runs teilt die Momentaufnahmen nach Prozessen von LCM: Die Zähler beginnen
// bei jedem Start bei null und sind nur innerhalb eines Laufs vergleichbar.
func (d *Data) runs() [][]DBPoint {
	var out [][]DBPoint
	for _, p := range d.DBs {
		if n := len(out); n > 0 && out[n-1][0].Snap.StartedAt.Equal(p.Snap.StartedAt) {
			out[n-1] = append(out[n-1], p)
			continue
		}
		out = append(out, []DBPoint{p})
	}
	return out
}
