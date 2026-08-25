// Package health überwacht den eigenen Prozess zur Laufzeit: Es prüft
// regelmäßig, ob der Dienst noch arbeitsfähig ist, meldet das an die
// Dienstverwaltung (systemd-Watchdog) und erzwingt einen Neustart, wenn der
// Prozess nachweislich instabil geworden ist.
//
// Zusammenspiel der Schutzebenen:
//   - safego fängt Panics in Hintergrund-Goroutinen ab (Dienst läuft weiter),
//   - dieses Paket erkennt, wenn sich solche Fehler häufen oder die Datenbank
//     unerreichbar wird, und übergibt dann kontrolliert an die
//     Neustart-Regel von systemd bzw. Docker.
package health

import (
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// sd_notify-Protokoll (systemd). Bewusst selbst implementiert statt einer
// zusätzlichen Abhängigkeit: Es sind wenige Zeilen über einen
// Unix-Datagramm-Socket, dessen Adresse in NOTIFY_SOCKET steht.
//
// Läuft der Dienst NICHT unter systemd (Docker, manueller Start, Entwicklung),
// ist die Variable nicht gesetzt und sämtliche Aufrufe hier sind wirkungslos.

// notify sendet eine Statusmeldung an systemd. Fehler sind bewusst nicht
// relevant: Ohne systemd gibt es keinen Empfänger, und ein nicht zustellbarer
// Status darf den Dienst niemals beeinträchtigen.
func notify(state string) {
	addr := os.Getenv("NOTIFY_SOCKET")
	if addr == "" {
		return
	}
	// Ein führendes '@' kennzeichnet einen abstrakten Socket (Linux); im
	// Adressfeld wird daraus ein NUL-Byte.
	if strings.HasPrefix(addr, "@") {
		addr = "\x00" + addr[1:]
	}
	conn, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: addr, Net: "unixgram"})
	if err != nil {
		return
	}
	defer conn.Close()
	_, _ = conn.Write([]byte(state))
}

// Die STATUS=-Texte sind bewusst IMMER englisch - genau wie Description= in
// der Unit-Datei. Sie erscheinen in `systemctl status` direkt neben der
// englischen Beschreibung; eine je nach Systemsprache wechselnde Mischung wäre
// dort inkonsistent und im Support mehrdeutig. Für die Ausgaben, die der
// Anwender liest (Installation, Konsole beim Start), gilt weiterhin
// Englisch-mit-Deutsch-bei-deutschem-System über internal/i18n.

// NotifyReady meldet systemd, dass der Start abgeschlossen ist und der Dienst
// Anfragen annimmt. Erforderlich für `Type=notify` in der Unit-Datei: Erst
// danach gilt der Dienst als aktiv, und erst danach läuft der Watchdog an.
func NotifyReady(status string) {
	notify("READY=1\nSTATUS=" + status)
}

// NotifyStopping meldet ein beabsichtigtes Beenden - so unterscheidet systemd
// einen geordneten Halt von einem Absturz.
func NotifyStopping() {
	notify("STOPPING=1\nSTATUS=Service is shutting down")
}

// notifyWatchdog ist das Lebenszeichen. Bleibt es länger als WatchdogSec aus,
// wertet systemd den Dienst als hängend und startet ihn neu - genau der Fall,
// den ein reines `Restart=on-failure` NICHT abdeckt, weil ein blockierter
// Prozess technisch weiterläuft.
func notifyWatchdog(status string) {
	notify("WATCHDOG=1\nSTATUS=" + status)
}

// watchdogInterval liest das von systemd vorgegebene Watchdog-Intervall
// (WATCHDOG_USEC) und liefert den empfohlenen Ping-Abstand: die Hälfte davon,
// damit ein einzelner verzögerter Durchlauf noch keinen Neustart auslöst.
// Ohne aktiven Watchdog liefert es 0.
func watchdogInterval() time.Duration {
	usec := os.Getenv("WATCHDOG_USEC")
	if usec == "" {
		return 0
	}
	n, err := strconv.ParseInt(usec, 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	// WATCHDOG_PID begrenzt den Watchdog auf einen bestimmten Prozess; ist sie
	// gesetzt und meint einen anderen, gilt der Watchdog nicht für uns.
	if pid := os.Getenv("WATCHDOG_PID"); pid != "" {
		if p, err := strconv.Atoi(pid); err == nil && p != os.Getpid() {
			return 0
		}
	}
	return time.Duration(n) * time.Microsecond / 2
}

// WatchdogActive meldet, ob dieser Prozess unter systemd-Watchdog-Aufsicht
// läuft - für die Anzeige im Health-Endpunkt.
func WatchdogActive() bool { return watchdogInterval() > 0 }
