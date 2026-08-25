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

// Setup initialisiert den globalen Logger. debugOverride (CLI-Flag -debug)
// erzwingt Debug-Level unabhängig von der Config. Ist logFile gesetzt, schreibt
// der Logger ZUSÄTZLICH zu stdout in eine rotierende Datei (für die dauerhafte
// Dienst-Überwachung - journald/Docker fangen stdout ohnehin ab). Gibt den
// Logger zurück und setzt ihn als slog-Default.
func Setup(level string, debugOverride bool, logFile string) *slog.Logger {
	if debugOverride {
		level = "debug"
	}
	var w io.Writer = os.Stdout
	if logFile != "" {
		rotating := newRotatingFile(logFile, logMaxSizeMB, logMaxBackups, logMaxAgeDays)
		w = io.MultiWriter(os.Stdout, rotating)
	}
	handler := slog.NewTextHandler(w, &slog.HandlerOptions{
		Level: ParseLevel(level),
	})
	logger := slog.New(handler)
	slog.SetDefault(logger)
	if debugOverride {
		logger.Debug("debug mode active (-debug flag)")
	}
	return logger
}
