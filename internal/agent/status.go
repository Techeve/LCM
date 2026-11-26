package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"LCM/internal/i18n"
	"LCM/internal/safego"
)

// Zustandsdatei des Agents (H-1): Er schreibt seinen Zustand alle StatusEvery und bei
// jedem Wechsel nach StatusDir/StatusFile; `lcm-agent status` liest sie und gibt
// sie lesbar aus. Eine Datei statt eines Sockets, damit kein neuer Zugang
// entsteht. Sie enthält keine Geheimnisse und ist für alle lesbar (0644).

const (
	// StatusDir ist das Laufzeitverzeichnis des Dienstes (systemd:
	// RuntimeDirectory=lcm-agent, OpenRC: start_pre).
	StatusDir  = "/run/lcm-agent"
	StatusFile = "status.json"
	// StatusStale: Ist die Datei älter, läuft der Agent vermutlich nicht mehr.
	StatusStale = 30 * time.Second
)

// Verbindungszustände in der Datei.
const (
	ConnConnected    = "connected"
	ConnConnecting   = "connecting"
	ConnDisconnected = "disconnected"
)

// Status ist der Inhalt von status.json.
type Status struct {
	Version string    `json:"version"`
	Started time.Time `json:"started"`
	Updated time.Time `json:"updated"`
	// Connection ist ConnConnected, ConnConnecting oder ConnDisconnected, Since der
	// Beginn dieses Zustands.
	Connection string    `json:"connection"`
	Since      time.Time `json:"since"`
	// LastContact ist das jüngste Lebenszeichen von LCM (Verbindung, Ping, Kommando).
	LastContact *time.Time `json:"last_contact,omitempty"`
	// Reconnects zählt die Wiederverbindungen seit dem Start des Prozesses.
	Reconnects     int         `json:"reconnects"`
	LastDisconnect *Disconnect `json:"last_disconnect,omitempty"`
	// Jobs sind die laufenden und die fertigen, aber von LCM noch nicht bestätigten.
	Jobs []StatusJob `json:"jobs"`
	// Unproven: Dieses Binary kam mit einem Update und hat sich noch nicht bewährt.
	Unproven   bool        `json:"unproven"`
	RolledBack *RolledBack `json:"rolled_back,omitempty"`
}

// Disconnect ist die jüngste Trennung von LCM.
type Disconnect struct {
	At     time.Time `json:"at"`
	Reason string    `json:"reason"`
}

// StatusJob ist ein Job der Ablage: running oder finished (Ergebnis unbestätigt).
type StatusJob struct {
	ID      string    `json:"id"`
	State   string    `json:"state"`
	Started time.Time `json:"started"`
}

// linkState hält fest, was status.json über die Verbindung sagt.
type linkState struct {
	mu          sync.Mutex
	state       string
	since       time.Time
	lastContact time.Time
	connects    int
	lastDrop    *Disconnect
	// changed weckt den Schreiber (gepuffert, ein Wecken genügt).
	changed chan struct{}
}

func newLinkState(now time.Time) *linkState {
	return &linkState{state: ConnConnecting, since: now, changed: make(chan struct{}, 1)}
}

func (l *linkState) kick() {
	select {
	case l.changed <- struct{}{}:
	default:
	}
}

func (l *linkState) set(state string, now time.Time) {
	l.mu.Lock()
	l.state, l.since = state, now
	l.mu.Unlock()
	l.kick()
}

func (l *linkState) connecting(now time.Time) { l.set(ConnConnecting, now) }

// ready: Verbindung steht und beide Abonnements sind bestätigt.
func (l *linkState) ready(now time.Time) {
	l.mu.Lock()
	l.connects++
	l.lastContact = now
	l.mu.Unlock()
	l.set(ConnConnected, now)
}

// lost vermerkt das Ende eines Verbindungsversuchs. Als Trennung samt Grund
// zählt nur das Ende einer bereiten Verbindung: Ein gescheiterter Aufbau (etwa
// ein abgewiesener erster Versuch, solange LCM noch startet) trennt nichts -
// sonst stünde „Letzte Trennung" da, bevor je eine Verbindung bestand.
func (l *linkState) lost(now time.Time, reason string) {
	l.mu.Lock()
	if l.state == ConnConnected {
		l.lastDrop = &Disconnect{At: now, Reason: reason}
	}
	l.mu.Unlock()
	l.set(ConnDisconnected, now)
}

// contact vermerkt ein Lebenszeichen von LCM. Es weckt den Schreiber nicht: Der
// Takt der Datei genügt.
func (l *linkState) contact(now time.Time) {
	l.mu.Lock()
	l.lastContact = now
	l.mu.Unlock()
}

// lastDisconnect liefert die jüngste Trennung (leer, wenn es keine gab).
func (l *linkState) lastDisconnect() Disconnect {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.lastDrop == nil {
		return Disconnect{}
	}
	return *l.lastDrop
}

// reconnected meldet, ob die nächste bereite Verbindung eine Wiederverbindung ist.
func (l *linkState) reconnected() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.connects > 0
}

func (l *linkState) fill(s *Status) {
	l.mu.Lock()
	defer l.mu.Unlock()
	s.Connection, s.Since = l.state, l.since
	if !l.lastContact.IsZero() {
		contact := l.lastContact
		s.LastContact = &contact
	}
	s.Reconnects = max(l.connects-1, 0)
	s.LastDisconnect = l.lastDrop
}

// WithStatusDir schaltet die Zustandsdatei ein: Der Agent schreibt sie nach dir
// (der Dienst: StatusDir). Ohne Aufruf schreibt er keine.
func (c *Client) WithStatusDir(dir string) *Client {
	c.statusDir = dir
	return c
}

// snapshot sammelt den Zustand für die Datei.
func (c *Client) snapshot(now time.Time) Status {
	s := Status{
		Version: c.version, Started: c.started, Updated: now,
		Jobs: c.runner.Jobs(), Unproven: c.unproven.Load(), RolledBack: c.rolledBack(),
	}
	c.link.fill(&s)
	return s
}

// writeStatus schreibt die Datei; Fehler meldet es einmal und danach nur noch im
// Debug-Protokoll (etwa /run ohne Schreibrecht), damit das Journal nicht volläuft.
func (c *Client) writeStatus() {
	if c.statusDir == "" {
		return
	}
	err := WriteStatus(c.statusDir, c.snapshot(time.Now()))
	switch {
	case err == nil:
		c.statusFailed = false
	case !c.statusFailed:
		c.statusFailed = true
		c.log.Warn("status file could not be written - lcm-agent status will not work", "dir", c.statusDir, "error", err)
	default:
		c.log.Debug("status file could not be written", "error", err)
	}
}

// statusLoop schreibt die Datei im Takt und bei jedem Wechsel, bis Stop kommt.
func (c *Client) statusLoop() {
	ticker := time.NewTicker(c.timing.StatusEvery)
	defer ticker.Stop()
	for {
		c.writeStatus()
		select {
		case <-c.stop:
			return
		case <-ticker.C:
		case <-c.link.changed:
		}
	}
}

func (c *Client) startStatus() {
	if c.statusDir == "" {
		return
	}
	c.statusDone = make(chan struct{})
	safego.Go("agent-status", func() {
		defer close(c.statusDone)
		c.statusLoop()
	})
}

// writeFinalStatus schreibt den letzten Stand (getrennt, mit dem Grund), wenn
// der Schreiber im Takt beendet ist. Liefe er noch, schrieben zwei gleichzeitig -
// und sein später Stand könnte den letzten überschreiben.
func (c *Client) writeFinalStatus() {
	if c.statusDone != nil {
		<-c.statusDone
	}
	c.writeStatus()
}

// WriteStatus schreibt die Zustandsdatei atomar (temporäre Datei, dann umbenennen)
// mit den Rechten 0644, unabhängig von der umask.
func WriteStatus(dir string, s Status) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, StatusFile+".tmp")
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o644); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, filepath.Join(dir, StatusFile)); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// ReadStatus liest die Zustandsdatei roh und geparst.
func ReadStatus(dir string) (Status, []byte, error) {
	raw, err := os.ReadFile(filepath.Join(dir, StatusFile))
	if err != nil {
		return Status{}, nil, err
	}
	var s Status
	if err := json.Unmarshal(raw, &s); err != nil {
		return Status{}, raw, fmt.Errorf("%s: %w", StatusFile, err)
	}
	return s, raw, nil
}

// Stale meldet, ob die Datei so alt ist, dass der Agent vermutlich nicht läuft.
func (s Status) Stale(now time.Time) bool { return now.Sub(s.Updated) > StatusStale }

// FormatStatus gibt den Zustand lesbar aus (Sprache nach i18n). Ist die Datei
// veraltet, steht der Hinweis davor.
func FormatStatus(s Status, now time.Time) string {
	var b strings.Builder
	if s.Stale(now) {
		b.WriteString(i18n.Tf("The agent does not seem to be running - the status is %s old. Last known state:\n\n",
			"Der Agent scheint nicht zu laufen - der Stand ist %s alt. Zuletzt bekannt:\n\n", age(now.Sub(s.Updated))))
	}
	row := func(label, value string) { fmt.Fprintf(&b, "%-18s %s\n", label+":", value) }
	row("lcm-agent", s.Version)
	row(i18n.T("Connection", "Verbindung"), connectionText(s, now))
	row(i18n.T("Last contact", "Letzter Kontakt"), contactText(s, now))
	row(i18n.T("Reconnects", "Reconnects"), i18n.Tf("%d since start", "%d seit dem Start", s.Reconnects))
	row(i18n.T("Last disconnect", "Letzte Trennung"), disconnectText(s, now))
	row(i18n.T("Jobs", "Jobs"), jobsHeadline(s))
	for _, j := range s.Jobs {
		fmt.Fprintf(&b, "%-18s %s\n", "", jobText(j, now))
	}
	row("Binary", binaryText(s))
	if rb := s.RolledBack; rb != nil {
		row(i18n.T("Rolled back", "Rueckfall"), i18n.Tf("version %s failed (%s), running %s", "Version %s ist gescheitert (%s), es laeuft %s", rb.From, rb.Reason, rb.To))
	}
	return b.String()
}

func connectionText(s Status, now time.Time) string {
	since := age(now.Sub(s.Since))
	switch s.Connection {
	case ConnConnected:
		return i18n.Tf("connected for %s", "verbunden seit %s", since)
	case ConnConnecting:
		return i18n.Tf("connecting for %s", "baut Verbindung auf seit %s", since)
	}
	return i18n.Tf("disconnected for %s", "getrennt seit %s", since)
}

func contactText(s Status, now time.Time) string {
	if s.LastContact == nil {
		return i18n.T("none yet", "noch keiner")
	}
	return i18n.Tf("%s ago", "vor %s", age(now.Sub(*s.LastContact)))
}

func disconnectText(s Status, now time.Time) string {
	if s.LastDisconnect == nil {
		return i18n.T("none since start", "keine seit dem Start")
	}
	return fmt.Sprintf("%s - %s", i18n.Tf("%s ago", "vor %s", age(now.Sub(s.LastDisconnect.At))), s.LastDisconnect.Reason)
}

func jobsHeadline(s Status) string {
	if len(s.Jobs) == 0 {
		return i18n.T("none", "keine")
	}
	return fmt.Sprintf("%d", len(s.Jobs))
}

func jobText(j StatusJob, now time.Time) string {
	since := age(now.Sub(j.Started))
	if j.State == jobRunning {
		return i18n.Tf("%s running for %s", "%s laeuft seit %s", j.ID, since)
	}
	return i18n.Tf("%s finished, result not yet confirmed by LCM (started %s ago)",
		"%s fertig, Ergebnis von LCM noch nicht bestaetigt (gestartet vor %s)", j.ID, since)
}

func binaryText(s Status) string {
	if s.Unproven {
		return i18n.T("unproven - new after an update, not yet confirmed healthy", "unbewaehrt - neu nach einem Update, noch nicht als gesund bestaetigt")
	}
	return i18n.T("proven", "bewaehrt")
}

// age schreibt eine Dauer kurz: 5 s, 12 min, 3 h, 2 d.
func age(d time.Duration) string {
	d = max(d, 0)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%d s", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d h", int(d.Hours()))
	}
	return fmt.Sprintf("%d d", int(d.Hours()/24))
}
