// Package dbmetrics misst, was die Datenbank im Betrieb tut: Wartezeit auf
// die Schreibsperre, Dauer der Anweisungen und Transaktionen, Fehler durch
// Sperren, Größe der Datei und die Läufe, die sie treiben.
//
// Alle Zähler leben im Speicher und beginnen bei jedem Start bei null. Den
// Verlauf hält das Werkzeug lcm-probe, nicht LCM - so bleibt die Datenbank
// außen vor und die Messung verfälscht nicht, was sie misst.
package dbmetrics

import "time"

// StatementBounds sind die festen Stufen für die Dauer der Anweisungen in
// Millisekunden. Fest, damit Momentaufnahmen verschiedener Stände und
// Prozesse vergleichbar bleiben.
var StatementBounds = []float64{1, 10, 100, 1000, 10000}

// LockWaitBounds sind feiner, denn aus ihnen entstehen Median und 95 % der
// Wartezeit. Oben endet die Skala bei busy_timeout (5 s): Wer länger wartet,
// bekommt „database is locked".
var LockWaitBounds = []float64{0.1, 0.5, 1, 2, 5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000}

// Histogram zählt Dauern in festen Stufen. Counts[i] zählt Werte bis
// einschließlich Bounds[i]; der letzte Eintrag nimmt alles darüber auf.
type Histogram struct {
	Bounds []float64 `json:"bounds_ms,omitempty"`
	Counts []uint64  `json:"counts"`
	Count  uint64    `json:"count"`
	SumMS  float64   `json:"sum_ms"`
	MaxMS  float64   `json:"max_ms"`
	MaxAt  time.Time `json:"max_at,omitzero"`
}

func newHistogram(bounds []float64) Histogram {
	return Histogram{Bounds: bounds, Counts: make([]uint64, len(bounds)+1)}
}

// observe trägt eine Dauer ein.
func (h *Histogram) observe(d time.Duration, at time.Time) {
	ms := float64(d) / float64(time.Millisecond)
	h.Counts[bucketIndex(h.Bounds, ms)]++
	h.Count++
	h.SumMS += ms
	if ms > h.MaxMS {
		h.MaxMS, h.MaxAt = ms, at
	}
}

func bucketIndex(bounds []float64, ms float64) int {
	for i, b := range bounds {
		if ms <= b {
			return i
		}
	}
	return len(bounds)
}

// clone liefert eine Kopie, die der Aufrufer ohne Sperre lesen darf.
func (h Histogram) clone() Histogram {
	h.Counts = append([]uint64(nil), h.Counts...)
	return h
}
