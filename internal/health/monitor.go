package health

import (
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"LCM/internal/safego"
)

// Grenzwerte der Instabilitäts-Erkennung. Bewusst großzügig gewählt: Ein
// Neustart ist ein grober Eingriff und darf erst greifen, wenn der Prozess
// wirklich nicht mehr arbeitsfähig ist - niemals bei einem einzelnen
// Aussetzer.
const (
	// defaultInterval ist der Prüfabstand ohne systemd-Watchdog. Mit Watchdog
	// gibt systemd über WATCHDOG_USEC einen (meist kürzeren) Takt vor.
	defaultInterval = 30 * time.Second

	// unhealthyLimit: so viele Prüfungen in Folge müssen fehlschlagen, bevor
	// der Prozess als dauerhaft gestört gilt - bei 30 s Takt 2,5 Minuten,
	// unter systemd (WatchdogSec/2 = 45 s) knapp 4. Kurze Aussetzer, etwa
	// eine gesperrte SQLite-Datei während eines Backups oder eines
	// CVE-Scans, laufen dadurch folgenlos durch.
	unhealthyLimit = 5

	// panicWindow/panicLimit: so viele abgefangene Panics in diesem Zeitraum
	// gelten als Instabilität. Einzelne Panics fängt safego ab und der Dienst
	// arbeitet weiter; erst die Häufung deutet auf beschädigten Zustand hin.
	panicWindow = 5 * time.Minute
	panicLimit  = 10
)

// checkTimeout begrenzt eine EINZELNE Prüfung. Der Wert liegt deutlich über
// dem Zeitlimit, das die Prüffunktion selbst mitbringt (5 s Datenbank-Ping) -
// er greift also nur, wenn sie ihr eigenes Limit nicht einhält, etwa weil sie
// auf eine gesperrte Datei wartet. Variable, damit Tests sie auf
// Millisekunden herunterdrehen können (wie RebootSettleDelay & Co.).
var checkTimeout = 15 * time.Second

// Befunde, die keine Antwort der Prüffunktion sind, sondern ihr Ausbleiben.
// Sie zählen wie ein Fehlschlag auf unhealthyLimit - der entscheidende
// Unterschied ist, dass es sie überhaupt gibt: Vorher war eine hängende
// Prüfung schlicht Stille, und Stille beantwortet systemd mit SIGKILL.
var (
	errCheckTimeout = errors.New("health check did not return in time")
	errCheckStuck   = errors.New("previous health check still running")
)

// ErrNotWritable meldet: Die Datenbank ist erreichbar, nimmt aber keine
// Schreibvorgänge an - typischerweise, weil ein fremder Vorgang ihre
// Schreibsperre hält.
//
// Der Zustand gehört gemeldet, aber NICHT mit einem Neustart beantwortet: Die
// Sperre liegt außerhalb dieses Prozesses, ein neu gestarteter stünde vor
// derselben. Der Neustart wäre dann kein Heilmittel, sondern ein zweiter
// Ausfall obendrauf - er würde die gerade laufenden Jobs mitnehmen. Der
// Dienst meldet stattdessen dauerhaft „degraded", bis sich die Lage klärt.
var ErrNotWritable = errors.New("database is reachable but not writable")

// Monitor prüft den eigenen Prozess in festem Takt und hält das Ergebnis für
// den Health-Endpunkt bereit.
type Monitor struct {
	// check meldet nil, wenn der Dienst arbeitsfähig ist (in der Praxis ein
	// Datenbank-Ping - ohne Datenbank kann LCM nichts Sinnvolles tun).
	check func() error
	// restart wird aufgerufen, wenn der Prozess als instabil gilt. Standard
	// ist ein kontrollierter Abbruch mit Fehlercode, damit die Neustart-Regel
	// von systemd (`Restart=always`) bzw. Docker (`restart: unless-stopped`)
	// greift.
	restart func(reason string)

	// interval ist der tatsächliche Prüftakt - unter systemd gibt ihn
	// WATCHDOG_USEC vor, sonst gilt defaultInterval. Er steht hier, damit die
	// Neustart-Meldung die wirklich verstrichene Zeit nennt und nicht eine
	// gerechnete, die zufällig danebenliegt.
	interval time.Duration

	mu        sync.Mutex
	lastErr   error
	lastCheck time.Time
	failStrk  int
	started   time.Time

	// probing hält fest, ob noch eine Prüfung unterwegs ist. Ohne diese
	// Sperre würde eine hängende Prüfung bei jedem Takt eine weitere
	// Goroutine hinterlassen.
	probing atomic.Bool
}

// NewMonitor erzeugt die Überwachung. check darf nicht nil sein.
func NewMonitor(check func() error) *Monitor {
	return &Monitor{check: check, started: time.Now(), restart: exitForRestart, interval: defaultInterval}
}

// WithRestart ersetzt die Neustart-Aktion (für Tests).
func (m *Monitor) WithRestart(fn func(reason string)) *Monitor {
	m.restart = fn
	return m
}

// Start meldet systemd die Betriebsbereitschaft und beginnt die Überwachung
// in ZWEI eigenen (panic-geschützten) Goroutinen.
//
// Die Trennung ist der Kern dieser Überwachung. Vorher lief beides in einem
// Ablauf: erst die Prüfung, dann das Lebenszeichen. Damit hing die Aussage
// „dieser Prozess läuft noch" an der Datenbank - wurde die zäh, verstummte
// der Dienst und systemd räumte ihn ab, obwohl er arbeitete.
//
// Jetzt beantworten die beiden Abläufe zwei verschiedene Fragen:
//
//   - pingLoop: Läuft dieser Prozess noch? Er tickt und meldet, sonst nichts.
//     Bleibt er aus, bekommt der Prozess keine Rechenzeit mehr - dann ist ein
//     Abräumen durch systemd richtig.
//   - checkLoop: Ist der Dienst arbeitsfähig? Er prüft mit eigenem Zeitlimit
//     und entscheidet über den Neustart - kontrolliert und mit Begründung im
//     Protokoll, statt per SIGKILL.
func (m *Monitor) Start(readyStatus string) {
	NotifyReady(readyStatus)
	interval := watchdogInterval()
	if interval > 0 {
		slog.Info("self-monitoring: systemd watchdog active", "ping_interval", interval.String())
	} else {
		interval = defaultInterval
		slog.Debug("self-monitoring active (without systemd watchdog)", "interval", interval.String())
	}
	m.interval = interval
	safego.Go("health-watchdog", func() { m.pingLoop(interval) })
	safego.Go("health-monitor", func() { m.checkLoop(interval) })
}

// pingLoop meldet systemd im festen Takt, dass dieser Prozess noch läuft. Er
// ruft NICHTS auf, was blockieren könnte - er liest nur den zuletzt
// festgestellten Zustand und schickt ihn weg.
func (m *Monitor) pingLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		m.ping()
	}
}

// ping schickt genau ein Lebenszeichen mit dem zuletzt festgestellten Zustand.
func (m *Monitor) ping() { notifyWatchdog(m.watchdogStatus()) }

// watchdogStatus ist der Text, der in `systemctl status` neben dem Dienst
// steht - „operational" oder der Grund, warum gerade nicht.
func (m *Monitor) watchdogStatus() string {
	m.mu.Lock()
	err := m.lastErr
	m.mu.Unlock()
	if err != nil {
		return "degraded: " + err.Error()
	}
	return "operational"
}

func (m *Monitor) checkLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		m.tick()
	}
}

// probe führt die Prüfung mit eigenem Zeitlimit aus.
//
// Eine Prüfung, die nicht zurückkehrt, ist damit ein BEFUND und kein
// Schweigen: Sie zählt auf unhealthyLimit, und bis zur Grenze meldet der
// Dienst weiter „degraded" statt gar nichts. Läuft die vorherige Prüfung noch,
// wird keine zweite gestartet - sonst stapelten sich die Goroutinen.
func (m *Monitor) probe() error {
	if !m.probing.CompareAndSwap(false, true) {
		return errCheckStuck
	}
	done := make(chan error, 1) // gepuffert: die Prüfung endet auch nach einer Zeitüberschreitung sauber
	safego.Go("health-probe", func() {
		err := m.check()
		// ERST freigeben, dann melden: Andersherum könnte der nächste Takt
		// die Sperre noch gesetzt sehen, obwohl das Ergebnis schon da ist.
		m.probing.Store(false)
		done <- err
	})
	timer := time.NewTimer(checkTimeout)
	defer timer.Stop()
	select {
	case err := <-done:
		return err
	case <-timer.C:
		return errCheckTimeout
	}
}

// tick führt eine Prüfung durch und entscheidet über den Neustart. Das
// Lebenszeichen an systemd hängt NICHT an diesem Ablauf (siehe pingLoop).
func (m *Monitor) tick() {
	err := m.probe()

	m.mu.Lock()
	prev := m.lastErr
	m.lastErr = err
	m.lastCheck = time.Now()
	if err != nil {
		m.failStrk++
	} else {
		m.failStrk = 0
	}
	streak := m.failStrk
	m.mu.Unlock()

	// Ein Zustandswechsel geht SOFORT hinaus, statt auf den nächsten
	// Ping-Takt zu warten.
	//
	// Die Trennung der beiden Abläufe hat einen Preis: Sie ticken unabhängig,
	// also kann der Ping noch den Befund des vorigen Durchgangs tragen. Im
	// Test stand deshalb fast eine Minute lang „operational" in
	// `systemctl status`, obwohl die Datenbank längst gesperrt war. Für die
	// Frage „lebt der Prozess?" ist das gleichgültig - für den Menschen, der
	// dort nachsieht, nicht.
	if errText(prev) != errText(err) {
		m.ping()
	}

	if err != nil {
		slog.Warn("self-monitoring: service not operational",
			"error", err, "consecutive_failures", streak, "limit", unhealthyLimit)
		// Ein Neustart hilft nur, wenn er die Ursache beseitigen kann - siehe
		// ErrNotWritable.
		if streak >= unhealthyLimit && !errors.Is(err, ErrNotWritable) {
			m.restart("health check failing for " + (time.Duration(streak) * m.interval).String() +
				": " + err.Error())
		}
		return
	}

	// Arbeitsfähig - aber häufen sich abgefangene Panics, ist der Zustand des
	// Prozesses trotzdem nicht mehr vertrauenswürdig.
	if n := safego.RecentCount(panicWindow); n >= panicLimit {
		m.restart("unstable: " + itoa(n) + " recovered panics in " + panicWindow.String())
	}
}

// Probe führt die Gesundheitsprüfung sofort aus - für den /health-Endpunkt,
// der eine aktuelle Aussage braucht und nicht den bis zu einen Takt alten
// Stand der Hintergrundüberwachung.
func (m *Monitor) Probe() error { return m.check() }

// Status ist die Momentaufnahme für den Health-Endpunkt.
type Status struct {
	Healthy        bool      `json:"healthy"`
	Error          string    `json:"error,omitempty"`
	LastCheck      time.Time `json:"last_check,omitempty"`
	FailStreak     int       `json:"fail_streak"`
	PanicsTotal    uint64    `json:"panics_total"`
	PanicsRecent   int       `json:"panics_recent"`
	WatchdogActive bool      `json:"watchdog_active"`
	UptimeSeconds  int64     `json:"uptime_seconds"`
}

// Status liefert den aktuellen Überwachungsstand.
func (m *Monitor) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := Status{
		Healthy:        m.lastErr == nil,
		LastCheck:      m.lastCheck,
		FailStreak:     m.failStrk,
		PanicsTotal:    safego.Total(),
		PanicsRecent:   safego.RecentCount(panicWindow),
		WatchdogActive: WatchdogActive(),
		UptimeSeconds:  int64(time.Since(m.started).Seconds()),
	}
	if m.lastErr != nil {
		st.Error = m.lastErr.Error()
	}
	return st
}

// errText macht zwei Befunde vergleichbar - auch der Wechsel von einer
// Störung zu einer anderen ist ein Wechsel.
func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// itoa vermeidet einen strconv-Import für eine einzige Zahl im Klartext.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
