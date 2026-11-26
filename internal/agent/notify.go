package agent

import (
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Zusammenarbeit mit systemd (sd_notify ohne Abhängigkeit): Mit
// NotifyAccess=main richtet der Agent den Wächter selbst ein (siehe watchdog),
// meldet READY=1 und füttert den Wächter (WATCHDOG=1) - aber nur, solange er
// gesund ist (siehe heartbeat). Hängt er, bleibt das Futter aus, und systemd
// startet ihn neu. Ohne NOTIFY_SOCKET (OpenRC, von Hand gestartet) tut nichts
// davon etwas, der Agent läuft wie bisher.

// notifier schreibt Zustandsmeldungen an den Socket, den systemd in
// NOTIFY_SOCKET nennt. Ein nil-notifier ist ein No-op.
type notifier struct{ socket string }

// notifierFromEnv liefert nil, wenn der Agent nicht unter systemd mit
// Benachrichtigung läuft.
func notifierFromEnv() *notifier {
	socket := os.Getenv("NOTIFY_SOCKET")
	// Nur Unix-Sockets: Pfad oder abstrakt ("@name", das versteht Go unter Linux
	// selbst). Alles andere (vsock:…) kennt der Agent nicht.
	if socket == "" || (socket[0] != '/' && socket[0] != '@') {
		return nil
	}
	return &notifier{socket: socket}
}

// send schickt eine Meldung ("READY=1", "WATCHDOG=1", "STOPPING=1") als ein
// Datagramm. Der Fehler ist nur fürs Protokoll gedacht: systemd merkt es
// ohnehin, wenn das Futter ausbleibt.
func (n *notifier) send(msg string) error {
	if n == nil {
		return nil
	}
	conn, err := net.DialTimeout("unixgram", n.socket, time.Second)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetWriteDeadline(time.Now().Add(time.Second))
	_, err = conn.Write([]byte(msg))
	return err
}

// watchdogInterval liest, wie oft der Agent den Wächter füttern soll: die
// Hälfte der Frist, die systemd in WATCHDOG_USEC nennt (so rät es
// sd_watchdog_enabled(3)). false, wenn systemd keinen Wächter eingerichtet hat
// oder er einem anderen Prozess gilt.
func watchdogInterval() (time.Duration, bool) {
	usec, err := strconv.ParseInt(os.Getenv("WATCHDOG_USEC"), 10, 64)
	if err != nil || usec <= 0 {
		return 0, false
	}
	if pid := strings.TrimSpace(os.Getenv("WATCHDOG_PID")); pid != "" && pid != strconv.Itoa(os.Getpid()) {
		return 0, false
	}
	return time.Duration(usec) * time.Microsecond / 2, true
}

// runtimeWatchdog ist die Frist, die der Agent selbst bei systemd einrichtet,
// wenn die Unit keine WatchdogSec= nennt.
const runtimeWatchdog = 90 * time.Second

// watchdog liefert den Takt, in dem der Agent den Wächter füttert. Die Unit
// des Pakets setzt bewusst keine WatchdogSec=: Beim Paket-Update lädt
// postinst die Units neu, während noch der alte Agent läuft (womöglich mitten
// im apt-Lauf, der das Update einspielt). Mit WatchdogSec= in der Unit würde
// systemd diesen alten Agent, der nicht füttert, nach Ablauf der Frist töten
// und apt mitreißen. Stattdessen richtet der Agent den Wächter zur Laufzeit
// selbst ein (WATCHDOG_USEC=, ab systemd 236). Nennt eine eigene Unit doch
// WatchdogSec=, gilt deren Frist.
func (c *Client) watchdog() (time.Duration, bool) {
	if every, ok := watchdogInterval(); ok {
		return every, true
	}
	if c.notify == nil {
		return 0, false
	}
	c.tell("WATCHDOG_USEC=" + strconv.FormatInt(runtimeWatchdog.Microseconds(), 10))
	return runtimeWatchdog / 3, true
}

// disarmWatchdog hebt den selbst eingerichteten Wächter auf. Vor dem Wechsel
// auf ein anderes Binary nötig: Die Frist gilt für die PID und überlebt das
// exec. Ein älteres Binary, das nicht füttert, würde sonst getötet.
func (c *Client) disarmWatchdog() {
	if c.notify != nil {
		c.tell("WATCHDOG_USEC=0")
	}
}

// heartbeat ist das Lebenszeichen der Verbindungsschleife an den Wächter: Die
// Schleife verlängert die Frist, solange sie vorankommt (Verbindungsaufbau,
// Warten auf den nächsten Versuch, Prüfung einer bestehenden Verbindung).
// Hängt sie, läuft die Frist ab, und der Agent gilt als ungesund.
//
// Der Wächter prüft damit nur die Schleife, nicht den Weg der Nachrichten
// (paho-Handler). Den deckt die Lebendprüfung ab: Beantwortet der Handler die
// Pings von LCM nicht mehr, endet die Sitzung (liveness.silent, hold) und die
// Schleife baut sie neu auf; hängt dabei etwas, bleibt ihr Lebenszeichen aus, und
// der Wächter greift. Ein eigener Rundlauf des Agents wäre nur mit einem Topic
// möglich, das er selbst schreiben und lesen darf - die ACL erlaubt ihm das
// nicht, und sie für einen Eigentest zu öffnen ist die Sache nicht wert. Bleibt
// eine Lücke: Gegenüber einem LCM ohne Pings (älterer Stand) merkt niemand, dass
// der Handler hängt, solange die Schleife läuft.
type heartbeat struct {
	mu    sync.Mutex
	until time.Time
}

// extend vertraut der Schleife für die nächste Zeit d.
func (h *heartbeat) extend(d time.Duration) {
	h.mu.Lock()
	h.until = time.Now().Add(d)
	h.mu.Unlock()
}

// alive meldet, ob die Frist noch läuft.
func (h *heartbeat) alive() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return time.Now().Before(h.until)
}
