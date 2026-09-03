// Package logging ist der zentrale Log-Service des Templates.
//
// Er basiert auf log/slog (Go-Standardbibliothek). Das Level kommt aus
// der config.json (log_level) und kann beim Start mit dem CLI-Flag
// -debug auf "debug" gehoben werden - nützlich in der Entwicklung, ohne
// die Config anzufassen.
//
// Verwendung überall im Code (nach Setup in main):
//
//	slog.Info("user angelegt", "username", u.Username)
//	slog.Debug("cache miss", "key", key)   // nur im Debug-Modus sichtbar
package logging

import (
	"io"
	"log/slog"
	"os"
	"sync"
	"time"
)

// ParseLevel übersetzt den config-String in ein slog.Level.
// Unbekannte Werte fallen auf Info zurück (validiert die Config vorher).
func ParseLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// Rotations-Vorgaben der Logdatei: ab 10 MB rotieren und KEINE Altstände
// älter als 7 Tage aufbewahren (ältere komprimierte Dateien werden automatisch
// gelöscht). So bleibt der Verlauf für die Dienst-Überwachung (Neustarts,
// Backups, …) aktuell, ohne den Datenträger vollzuschreiben.
const (
	logMaxSizeMB   = 10
	logMaxBackups  = 7
	logMaxAgeDays  = 7
	logFilePerm    = 0o640
	logDirFilePerm = 0o750
)

// current ist das Level des laufenden Dienstes. Es steckt in einer LevelVar
// und nicht in einer Konstanten, weil es sich zur Laufzeit umschalten lassen
// soll: Wer einer Störung nachgeht, will Debug JETZT haben - nicht nach einem
// Neustart, der die Störung womöglich mitnimmt.
var current = new(slog.LevelVar)

// debugTimer dreht ein befristetes Debug wieder zurück. Nur einer gleichzeitig;
// ein zweiter Aufruf verlängert, statt einen zweiten Wecker zu stellen.
var (
	debugMu    sync.Mutex
	debugTimer *time.Timer
	debugUntil time.Time
	baseLevel  slog.Level
)

// Setup initialisiert den globalen Logger. debugOverride (CLI-Flag -debug)
// erzwingt Debug-Level unabhängig von der Config. Ist logFile gesetzt, schreibt
// der Logger ZUSÄTZLICH zu stdout in eine rotierende Datei (für die dauerhafte
// Dienst-Überwachung - journald/Docker fangen stdout ohnehin ab). Gibt den
// Logger zurück und setzt ihn als slog-Default.
func Setup(level string, debugOverride bool, logFile string) *slog.Logger {
	if debugOverride {
		level = "debug"
	}
	current.Set(ParseLevel(level))
	debugMu.Lock()
	baseLevel = ParseLevel(level)
	debugMu.Unlock()

	var w io.Writer = os.Stdout
	if logFile != "" {
		rotating := newRotatingFile(logFile, logMaxSizeMB, logMaxBackups, logMaxAgeDays)
		w = io.MultiWriter(os.Stdout, rotating)
	}
	handler := slog.NewTextHandler(w, &slog.HandlerOptions{
		Level: current,
	})
	logger := slog.New(handler)
	slog.SetDefault(logger)
	if debugOverride {
		logger.Debug("debug mode active (-debug flag)")
	}
	return logger
}

// Level liefert das aktuell wirksame Level als Zeichenkette.
func Level() string {
	switch current.Level() {
	case slog.LevelDebug:
		return "debug"
	case slog.LevelWarn:
		return "warn"
	case slog.LevelError:
		return "error"
	default:
		return "info"
	}
}

// DebugUntil liefert den Zeitpunkt, zu dem ein befristetes Debug endet -
// Nullzeit, wenn keines läuft.
func DebugUntil() time.Time {
	debugMu.Lock()
	defer debugMu.Unlock()
	if debugTimer == nil {
		return time.Time{}
	}
	return debugUntil
}

// EnableDebugFor hebt das Level für eine begrenzte Zeit auf Debug und stellt
// es danach selbsttätig zurück.
//
// Die Frist ist keine Bequemlichkeit, sondern der Punkt: Debug schreibt jedes
// SSH-Kommando samt Ausgabe mit. Bleibt es an, wächst die Logdatei auf einer
// kleinen Maschine schneller, als die Rotation sie wegräumt - und niemand
// erinnert sich nach drei Wochen daran, es abzuschalten. Ein zweiter Aufruf
// verlängert die laufende Frist, statt einen zweiten Wecker zu stellen.
func EnableDebugFor(d time.Duration) {
	debugMu.Lock()
	defer debugMu.Unlock()
	if debugTimer != nil {
		debugTimer.Stop()
	}
	current.Set(slog.LevelDebug)
	debugUntil = time.Now().Add(d)
	debugTimer = time.AfterFunc(d, func() {
		debugMu.Lock()
		defer debugMu.Unlock()
		current.Set(baseLevel)
		debugTimer = nil
		slog.Info("debug logging expired - back to configured level", "level", Level())
	})
	slog.Info("debug logging enabled", "until", debugUntil.Format(time.RFC3339))
}

// DisableDebug stellt das konfigurierte Level sofort wieder her.
func DisableDebug() {
	debugMu.Lock()
	defer debugMu.Unlock()
	if debugTimer != nil {
		debugTimer.Stop()
		debugTimer = nil
	}
	current.Set(baseLevel)
	slog.Info("debug logging disabled", "level", Level())
}
