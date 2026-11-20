package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/safego"
	"LCM/internal/storage/repositories"
)

// SessionContext beschreibt, in welchem Zusammenhang eine SSH-Verbindung
// geöffnet wird - die Metadaten des Protokolls.
type SessionContext struct {
	ServerID uint    // 0 = (noch) kein Server (Onboarding vor dem Persistieren)
	JobID    *string // zugehörige Job-Ausführung (UUID), falls vorhanden
	Actor    string  // auslösender Benutzer oder "scheduler"
	Purpose  string  // "rule:Update", "sync", "harden-ssh", "join", …
	Host     string
	User     string
}

// SSHRecorder erzeugt zu jeder SSH-Verbindung eine Session und protokolliert
// darüber JEDES ausgeführte Kommando (mit Redaction). Wird als transparenter
// Decorator um eine sshx.Conn gelegt - die Services müssen nur beim Verbinden
// einmalig Record(...) aufrufen und wie gewohnt conn.Close() nutzen.
//
// Nil-sicher: Ein nicht gesetzter Recorder (z.B. in schlanken Tests) gibt die
// Verbindung unverändert zurück, sodass nichts protokolliert wird.
type SSHRecorder struct {
	logs *repositories.SSHLogRepository
	// jobs verbindet eine Verbindung mit ihrem Job: AttachCloser macht sie
	// abbrechbar (beim Abort wird sie zwangsweise geschlossen), MarkActivity
	// meldet ihre Lebenszeichen an den Watchdog. Optional (nil = keine
	// Job-Kopplung; Tests).
	jobs JobLink
}

// JobLink ist die Kopplung einer SSH-Verbindung an den Job, für den sie
// geöffnet wurde - der Ausschnitt des JobService, den der Recorder braucht.
type JobLink interface {
	// AttachCloser registriert die Verbindung zum Zwangs-Schließen beim Abbruch.
	AttachCloser(jobID string, conn io.Closer)
	// MarkActivity meldet ein Lebenszeichen des laufenden Kommandos.
	MarkActivity(jobID string)
}

func NewSSHRecorder(logs *repositories.SSHLogRepository) *SSHRecorder {
	return &SSHRecorder{logs: logs}
}

// WithJobs verdrahtet die Kopplung geöffneter Verbindungen an ihren Job
// (Abbruch + Lebenszeichen für den Watchdog). Optional.
func (r *SSHRecorder) WithJobs(jobs JobLink) *SSHRecorder {
	r.jobs = jobs
	return r
}

// Record legt eine Session an und umhüllt die Verbindung mit dem Protokoll-
// Decorator. Der Rückgabewert ersetzt die Original-Verbindung. Gehört die
// Verbindung zu einem Job, wird sie dort registriert (Abbruch-Mechanik).
func (r *SSHRecorder) Record(conn sshx.Conn, ctx SessionContext) sshx.Conn {
	if r == nil || r.logs == nil || conn == nil {
		return conn
	}
	if ctx.JobID != nil && r.jobs != nil {
		jobID := *ctx.JobID
		r.jobs.AttachCloser(jobID, conn)
		// Ab hier gilt der Job als überwacht: Er hat ein Kommando auf der
		// Gegenseite, das hängen kann - und mit den Lebenszeichen dieser
		// Verbindung die Grundlage, das zu erkennen.
		conn.OnActivity(func() { r.jobs.MarkActivity(jobID) })
	}
	now := time.Now()
	sess := &domain.SSHSession{
		JobID:    ctx.JobID,
		Purpose:  ctx.Purpose,
		Actor:    ctx.Actor,
		Host:     ctx.Host,
		User:     ctx.User,
		OpenedAt: now,
	}
	if ctx.ServerID != 0 {
		sess.ServerID = &ctx.ServerID
	}
	if err := r.logs.CreateSession(sess); err != nil {
		// Protokollierung darf die eigentliche Operation nie verhindern.
		return conn
	}
	return &recordingConn{inner: conn, logs: r.logs, sess: sess}
}

// Link ordnet eine zunächst serverlose Session (Onboarding) nachträglich
// Server und Job zu. No-op, wenn conn keine protokollierte Verbindung ist.
func (r *SSHRecorder) Link(conn sshx.Conn, serverID uint, jobID *string) {
	if r == nil || r.logs == nil {
		return
	}
	rc, ok := conn.(*recordingConn)
	if !ok {
		return
	}
	rc.sess.ServerID = &serverID
	rc.sess.JobID = jobID
	_ = r.logs.LinkSession(rc.sess.ID, serverID, jobID)
}

// CleanupOlderThan löscht Protokolle jenseits der Aufbewahrungsfrist.
func (r *SSHRecorder) CleanupOlderThan(cutoff time.Time) (int64, error) {
	if r == nil || r.logs == nil {
		return 0, nil
	}
	return r.logs.DeleteOlderThan(cutoff)
}

// DeleteRoutineCommandsOlderThan entfernt die Kommandos der Health-Check-
// Sitzungen nach der kurzen Frist für Routine-Protokolle.
func (r *SSHRecorder) DeleteRoutineCommandsOlderThan(cutoff time.Time) (int64, error) {
	if r == nil || r.logs == nil {
		return 0, nil
	}
	return r.logs.DeleteCommandsOlderThan(domain.HealthCheckPurpose, cutoff)
}

// trivialCommands sind Kommandos, deren Protokollzeile nichts trägt.
//
// Bisher hinterließ jeder Health-Ping je Server und Viertelstunde eine Zeile
// mit Kommando und Ausgabe, aufbewahrt so lange wie jedes andere Protokoll -
// bei dreihundert Servern über neunzig Tage einige Millionen Zeilen für ein
// „echo". Die Sitzung selbst bleibt erhalten: Auf derselben Verbindung laufen
// die Grundsatz-Regeln, die Speichermessung und liegengebliebene
// Benutzer-Abgleiche, und die gehören sehr wohl ins Protokoll.
var trivialCommands = map[string]bool{healthProbeCmd: true}

// LiveOutputInterval ist der Takt, in dem die Ausgabe eines laufenden Kommandos
// ins Protokoll wandert. Bewusst eine exportierte Variable: Tests setzen ihn
// herunter, statt in Echtzeit zu warten (wie RebootSettleDelay).
var LiveOutputInterval = 2 * time.Second

// recordingConn ist der Protokoll-Decorator um eine echte SSH-Verbindung.
// Die Lebenszeichen reicht er unverändert an die innere Verbindung durch -
// sie allein sieht den Ausgabestrom, während das Kommando noch läuft.
type recordingConn struct {
	inner    sshx.Conn
	logs     *repositories.SSHLogRepository
	sess     *domain.SSHSession
	seq      int
	hadError bool
}

func (c *recordingConn) Run(cmd string) (string, int, error) {
	return c.RunStdin(cmd, "")
}

func (c *recordingConn) OnActivity(fn func()) { c.inner.OnActivity(fn) }

// KeyExchange reicht das Verfahren der darunterliegenden Verbindung durch -
// der Mitschnitt verändert den Handshake nicht.
func (c *recordingConn) KeyExchange() string { return c.inner.KeyExchange() }

// RunStdin protokolliert wie Run, speist dem Kommando aber stdin ein. Der
// stdin-Inhalt (z.B. ein sudo-Passwort) wird BEWUSST nicht mitgeschrieben -
// nur das Kommando (redigiert) landet im Protokoll.
func (c *recordingConn) RunStdin(cmd, stdin string) (string, int, error) {
	if trivialCommands[strings.TrimSpace(cmd)] {
		out, code, err := c.inner.RunStdin(cmd, stdin)
		// Nicht mitschreiben - aber Fehler zählen weiter: Scheitert
		// ausgerechnet der Ping, ist das die Aussage der ganzen Sitzung.
		if err != nil || code != 0 {
			c.hadError = true
		}
		return out, code, err
	}
	c.seq++
	entry := &domain.SSHCommand{
		SSHSessionID: c.sess.ID,
		Seq:          c.seq,
		StartedAt:    time.Now(),
		Command:      redactSecrets(cmd),
		ExitCode:     -1,
		Running:      true,
	}
	stored := c.logs.StartCommand(entry) == nil
	stop := c.watchOutput(entry, stored)
	out, code, err := c.inner.RunStdin(cmd, stdin)
	stop()

	entry.DurationMs = time.Since(entry.StartedAt).Milliseconds()
	entry.Output = truncateOutput(redactSecrets(out))
	entry.ExitCode = code
	entry.Running = false
	if err != nil {
		entry.Err = err.Error()
		c.hadError = true
	}
	if code != 0 {
		c.hadError = true
	}
	if stored {
		_ = c.logs.FinishCommand(entry)
	} else {
		entry.ID = ""
		_ = c.logs.AddCommand(entry)
	}

	// Auf log_level=debug jedes Kommando auch ins Anwendungs-Log.
	//
	// Bisher erzeugte die Debug-Stufe KEINE einzige DEBUG-Zeile: Fehler an der
	// SSH-/Provisionierungsstrecke - also genau dort, wo LCM arbeitet - waren
	// aus den Logs heraus nicht diagnostizierbar, ein fehlgeschlagener Join
	// hinterließ nur eine 502-Request-Zeile (BUG-011). Geschrieben wird die
	// bereits für die Datenbank redigierte und gekürzte Fassung, damit weder
	// Passwörter noch Schlüsselmaterial ins Log geraten; stdin bleibt wie im
	// SSH-Protokoll grundsätzlich außen vor.
	if slog.Default().Enabled(context.Background(), slog.LevelDebug) {
		attrs := []any{
			"host", c.sess.Host, "user", c.sess.User,
			"seq", c.seq, "exit_code", code,
			"duration_ms", entry.DurationMs,
			"command", entry.Command,
		}
		if entry.Output != "" {
			attrs = append(attrs, "output", entry.Output)
		}
		if err != nil {
			attrs = append(attrs, "error", err)
		}
		slog.Debug("ssh command", attrs...)
	}
	return out, code, err
}

// watchOutput schreibt die Ausgabe des laufenden Kommandos regelmäßig ins
// Protokoll, bis die zurückgegebene Funktion aufgerufen wird. Es gehen nur
// fertige Zeilen durch die Redaction: Ein mitten im Geheimnis abgeschnittener
// Block entginge sonst ihren Mustern.
func (c *recordingConn) watchOutput(entry *domain.SSHCommand, stored bool) (stop func()) {
	reader, ok := c.inner.(sshx.PartialOutputReader)
	if !ok || !stored {
		return func() {}
	}
	done := make(chan struct{})
	finished := make(chan struct{})
	safego.Go("ssh-live-output", func() {
		defer close(finished)
		ticker := time.NewTicker(LiveOutputInterval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				c.storePartialOutput(entry, reader.PartialOutput())
			}
		}
	})
	return func() {
		close(done)
		<-finished
	}
}

func (c *recordingConn) storePartialOutput(entry *domain.SSHCommand, partial string) {
	lines := partial[:strings.LastIndexByte(partial, '\n')+1]
	output := truncateOutput(redactSecrets(lines))
	if output == entry.Output {
		return
	}
	entry.Output = output
	_ = c.logs.UpdateCommandOutput(entry)
}

func (c *recordingConn) Close() error {
	_ = c.logs.FinishSession(c.sess.ID, time.Now(), c.seq, c.hadError)
	return c.inner.Close()
}

// --- Terminal-Mitschnitt ------------------------------------------------------

// ErrTerminalUnsupported: Über diesen Transport gibt es keine Konsole. Trifft
// den Agent-Weg (MQTT kennt nur Frage und Antwort) und jede Verbindung, die
// kein PTY führen kann.
var ErrTerminalUnsupported = errors.New("dieser server unterstützt keine konsole")

// Terminal öffnet eine interaktive Sitzung UND schneidet sie mit.
//
// Der Mitschnitt ist keine Zutat, sondern Bedingung: LCMs Zusage lautet, dass
// jedes Kommando im Protokoll steht. Eine Konsole, die daran vorbeiführt,
// hätte ausgerechnet dort ein Loch, wo am meisten passieren kann.
//
// Aufgezeichnet wird der AUSGABE-Strom, nicht die Tastenanschläge. Das ist
// nicht weniger, sondern genauer: Weil das Terminal die Eingaben spiegelt,
// steht beides in der richtigen Reihenfolge darin - und was die Gegenseite
// bewusst NICHT spiegelt, etwa ein Passwort an einer sudo-Abfrage, bleibt
// auch aus dem Protokoll heraus.
func (c *recordingConn) Terminal(term string, cols, rows int) (sshx.Terminal, error) {
	inner, ok := c.inner.(sshx.TerminalConn)
	if !ok {
		return nil, ErrTerminalUnsupported
	}
	t, err := inner.Terminal(term, cols, rows)
	if err != nil {
		return nil, err
	}
	c.seq++
	return &recordingTerminal{
		Terminal: t, logs: c.logs, sess: c.sess,
		seq: c.seq, started: time.Now(),
	}, nil
}

// recordingTerminal hängt sich in den Ausgabe-Strom und schreibt am Ende einen
// Protokolleintrag.
type recordingTerminal struct {
	sshx.Terminal
	logs    *repositories.SSHLogRepository
	sess    *domain.SSHSession
	seq     int
	started time.Time

	mu     sync.Mutex
	out    sessionTranscript
	closed bool
}

func (t *recordingTerminal) Read(p []byte) (int, error) {
	n, err := t.Terminal.Read(p)
	if n > 0 {
		t.mu.Lock()
		t.out.write(p[:n])
		t.mu.Unlock()
	}
	return n, err
}

// Close beendet die Sitzung und schreibt den Mitschnitt - einmal, auch wenn
// mehrere Seiten gleichzeitig schließen.
func (t *recordingTerminal) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	transcript := t.out.String()
	t.mu.Unlock()

	err := t.Terminal.Close()
	entry := &domain.SSHCommand{
		SSHSessionID: t.sess.ID,
		Seq:          t.seq,
		StartedAt:    t.started,
		DurationMs:   time.Since(t.started).Milliseconds(),
		Command:      "[interaktive konsole]",
		Output:       truncateOutput(redactSecrets(transcript)),
	}
	if logErr := t.logs.AddCommand(entry); logErr != nil {
		slog.Error("terminal transcript could not be stored", "session", t.sess.ID, "error", logErr)
	}
	return err
}

// Der Mitschnitt behält Anfang und Ende einer Sitzung, auch wenn sie
// stundenlang läuft: den Anfang bis transcriptHeadBytes, danach gleitend die
// letzten transcriptTailBytes. Gerade das Ende gehört ins Audit - dort steht,
// was zuletzt geschah. Was dazwischen lag, ersetzt ein Vermerk mit der Zahl
// der ausgelassenen Bytes. Zusammen bleibt alles unter maxOutputBytes, die
// Kürzung in truncateOutput schneidet also nicht noch einmal hinein.
const (
	transcriptHeadBytes = keepHeadBytes
	transcriptTailBytes = keepTailBytes
	transcriptGapNotice = "\n\n… [Mitschnitt gekürzt: %d Bytes ausgelassen] …\n\n"
)

// sessionTranscript sammelt den Ausgabestrom einer Konsolensitzung mit fester
// Obergrenze im Speicher.
type sessionTranscript struct {
	head  []byte
	tail  []byte // gleitendes Ende, wächst höchstens aufs Doppelte
	total int64  // alle gelesenen Bytes
}

func (s *sessionTranscript) write(p []byte) {
	s.total += int64(len(p))
	if room := transcriptHeadBytes - len(s.head); room > 0 {
		n := min(room, len(p))
		s.head = append(s.head, p[:n]...)
		p = p[n:]
	}
	s.tail = append(s.tail, p...)
	// Nicht bei jedem Lesen umkopieren, erst wenn das Doppelte erreicht ist.
	if len(s.tail) > 2*transcriptTailBytes {
		s.tail = append(s.tail[:0], s.tail[len(s.tail)-transcriptTailBytes:]...)
	}
}

// String setzt Anfang, Vermerk und Ende zusammen.
func (s *sessionTranscript) String() string {
	tail := s.tail
	if len(tail) > transcriptTailBytes {
		tail = tail[len(tail)-transcriptTailBytes:]
	}
	dropped := s.total - int64(len(s.head)+len(tail))
	if dropped == 0 {
		return string(s.head) + string(tail)
	}
	return string(s.head) + fmt.Sprintf(transcriptGapNotice, dropped) + string(tail)
}
