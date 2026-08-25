package health

import (
	"log/slog"
	"sync"
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
// in einer eigenen (panic-geschützten) Goroutine.
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
	safego.Go("health-monitor", func() { m.loop(interval) })
}

func (m *Monitor) loop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		m.tick()
	}
}

// tick führt eine Prüfung durch und entscheidet über Lebenszeichen bzw.
// Neustart.
func (m *Monitor) tick() {
	err := m.check()

	m.mu.Lock()
	m.lastErr = err
	m.lastCheck = time.Now()
	if err != nil {
		m.failStrk++
	} else {
		m.failStrk = 0
	}
	streak := m.failStrk
	m.mu.Unlock()

	// Das Lebenszeichen sagt systemd nur eines: Dieser Ablauf kommt noch
	// voran. Es hängt bewusst NICHT am Ergebnis der Prüfung - über eine
	// unerreichbare Datenbank entscheidet unhealthyLimit weiter unten, mit
	// der Toleranz, die dort steht.
	//
	// Andersherum wäre diese Toleranz nämlich unerreichbar: Der Ping-Takt ist
	// WatchdogSec/2, zwei ausgefallene Pings genügen systemd. Beim ersten
	// Fehlschlag zu schweigen hieße, nach 90 Sekunden abgeräumt zu werden -
	// die Zählung bis unhealthyLimit käme nie zustande. Genau das ist auf dem
	// LCM-Host jede Nacht passiert, wenn Zeitplan-Last und CVE-Scan die
	// Datenbank ausgebremst haben.
	//
	// Der Fall „dieser Ablauf selbst hängt" bleibt gedeckt: Das Lebenszeichen
	// steht HINTER m.check(). Kommt der Aufruf nicht zurück, bleibt es aus.
	if err != nil {
		notifyWatchdog("degraded: " + err.Error())
		slog.Warn("self-monitoring: service not operational",
			"error", err, "consecutive_failures", streak, "limit", unhealthyLimit)
		if streak >= unhealthyLimit {
			m.restart("database unreachable for " + (time.Duration(streak) * m.interval).String() +
				": " + err.Error())
		}
		return
	}
	notifyWatchdog("operational")

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
