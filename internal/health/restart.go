package health

import (
	"log/slog"
	"os"
	"time"
)

// exitRestartCode ist der Beendigungscode für einen selbst ausgelösten
// Neustart. Ungleich 0, damit ihn jede Neustart-Regel als Fehlerfall wertet -
// auch das restriktivere `Restart=on-failure`.
const exitRestartCode = 70 // EX_SOFTWARE (sysexits.h): interner Softwarefehler

// exitForRestart beendet den Prozess kontrolliert, damit die Dienstverwaltung
// ihn frisch startet. Das ist die letzte Stufe der Selbstheilung: Sie greift
// erst, wenn der Prozess nachweislich nicht mehr arbeitsfähig ist.
//
// Ein Neustart ist hier ehrlicher als Weiterlaufen - LCM verwaltet fremde
// Server, und ein Dienst mit beschädigtem Zustand könnte dort falsche
// Entscheidungen treffen (etwa Server fälschlich als „nicht erreichbar"
// markieren und Alarme auslösen).
//
// WICHTIG: Läuft LCM ohne Dienstverwaltung (manueller Start im Vordergrund),
// bleibt der Dienst nach diesem Aufruf beendet. Das ist beabsichtigt und
// deutlich protokolliert - ein stiller Zombie-Prozess wäre schlimmer.
func exitForRestart(reason string) {
	slog.Error("=== LCM SERVICE IS RESTARTING ITSELF (self-monitoring) ===",
		"reason", reason,
		"hint", "the service is restarted automatically by systemd/Docker; "+
			"without a service manager it stays stopped")
	NotifyStopping()
	// Kurz warten, damit die Protokollzeilen sicher geschrieben sind (die
	// Logdatei wird gepuffert) - sonst fehlt im Journal ausgerechnet die
	// Begründung für den Neustart.
	time.Sleep(250 * time.Millisecond)
	os.Exit(exitRestartCode)
}
