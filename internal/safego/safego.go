// Package safego startet Hintergrund-Goroutinen so, dass ein Programmierfehler
// darin nicht den gesamten Dienst mitreißt.
//
// Hintergrund: In Go beendet ein unbehandelter Panic IMMER den kompletten
// Prozess - auch wenn er in einer beliebigen Hintergrund-Goroutine auftritt.
// Für LCM ist das besonders heikel, weil die Job-Runner Ausgaben FREMDER
// Server parsen: Ein Server, der unerwartet antwortet (fehlende Spalte, leere
// Ausgabe), könnte über einen Index-Zugriff sonst die gesamte Verwaltung
// aller anderen Server abschießen.
//
// Deshalb läuft jede Hintergrund-Goroutine über Go bzw. GoCleanup. Ein Panic
// wird dort mit vollem Stacktrace protokolliert, gezählt und in einen Fehler
// verwandelt - der Dienst läuft weiter.
//
// Die HTTP-Ebene ist separat abgesichert (Fibers recover-Middleware in
// api/router und mcp), die geplanten Läufe über cron.Recover im Scheduler.
package safego

import (
	"fmt"
	"log/slog"
	"runtime"
	"sync"
	"time"
)

// stackBufSize begrenzt den mitprotokollierten Stacktrace (64 KiB reichen für
// jede realistische Aufrufkette und verhindern ein Log-Fluten).
const stackBufSize = 64 << 10

// Go startet fn nebenläufig und fängt einen Panic ab. name benennt die
// Aufgabe im Protokoll (z.B. "job:hardware-refresh").
func Go(name string, fn func()) {
	go func() {
		defer Recover(name, nil)
		fn()
	}()
}

// GoCleanup verhält sich wie Go, ruft bei einem Panic aber zusätzlich cleanup
// mit dem entstandenen Fehler auf. Das ist der Regelfall für Job-Runner: Ohne
// Aufräumen bliebe der Job auf „läuft" stehen und mit ihm die Server-Sperre -
// der betroffene Server wäre bis zum Neustart für alle Aktionen blockiert.
//
// cleanup läuft selbst geschützt: Ein Panic dort wird protokolliert, statt den
// Prozess doch noch zu beenden.
func GoCleanup(name string, cleanup func(error), fn func()) {
	go func() {
		defer Recover(name, cleanup)
		fn()
	}()
}

// Recover ist der defer-Baustein hinter Go/GoCleanup. Direkt verwendbar, wenn
// eine Goroutine nicht über dieses Paket gestartet werden kann (z.B. weil ein
// Framework sie erzeugt):
//
//	defer safego.Recover("mqtt-hook", nil)
func Recover(name string, cleanup func(error)) {
	r := recover()
	if r == nil {
		return
	}
	buf := make([]byte, stackBufSize)
	buf = buf[:runtime.Stack(buf, false)]
	err, ok := r.(error)
	if !ok {
		err = fmt.Errorf("%v", r)
	}
	record(name)
	slog.Error("PANIC in background task recovered - the service keeps running",
		"task", name, "error", err, "stack", string(buf))
	if cleanup == nil {
		return
	}
	// Das Aufräumen darf den Prozess nicht doch noch beenden.
	defer func() {
		if r2 := recover(); r2 != nil {
			record(name + ":cleanup")
			slog.Error("PANIC during cleanup after a panic",
				"task", name, "error", fmt.Sprintf("%v", r2))
		}
	}()
	cleanup(err)
}

// --- Instabilitäts-Erkennung -------------------------------------------------
//
// Ein einzelner abgefangener Panic ist ein Fehler, den der Dienst wegsteckt.
// Häufen sie sich, ist der Prozess aber nicht mehr vertrauenswürdig (typisch
// bei aufgebrauchtem Speicher oder beschädigtem Zustand). Dann ist ein
// kontrollierter Neustart durch die Dienstverwaltung die ehrlichere Antwort
// als ein Weiterlaufen mit unklarem Zustand - siehe health.Monitor.

var (
	mu      sync.Mutex
	history []panicEvent
	total   uint64
)

type panicEvent struct {
	name string
	at   time.Time
}

// historyWindow ist der Zeitraum, über den Panics für die
// Instabilitäts-Bewertung vorgehalten werden.
const historyWindow = 15 * time.Minute

func record(name string) {
	mu.Lock()
	defer mu.Unlock()
	total++
	now := time.Now()
	history = append(history, panicEvent{name: name, at: now})
	// Alte Einträge verwerfen - die Liste bleibt dadurch von Natur aus klein.
	cutoff := now.Add(-historyWindow)
	keep := history[:0]
	for _, e := range history {
		if e.at.After(cutoff) {
			keep = append(keep, e)
		}
	}
	history = keep
}

// Total liefert die Gesamtzahl abgefangener Panics seit dem Start - sie
// erscheint im Health-Endpunkt, damit ein Monitoring sie beobachten kann.
func Total() uint64 {
	mu.Lock()
	defer mu.Unlock()
	return total
}

// RecentCount liefert die Anzahl der Panics innerhalb von window (maximal
// historyWindow). Grundlage der Instabilitäts-Bewertung.
func RecentCount(window time.Duration) int {
	if window > historyWindow {
		window = historyWindow
	}
	mu.Lock()
	defer mu.Unlock()
	cutoff := time.Now().Add(-window)
	n := 0
	for _, e := range history {
		if e.at.After(cutoff) {
			n++
		}
	}
	return n
}

// Reset verwirft die Panic-Historie (nur für Tests).
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	history = nil
	total = 0
}
