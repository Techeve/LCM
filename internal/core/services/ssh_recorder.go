package services

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/sshx"
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

// trivialCommands sind Kommandos, deren Protokollzeile nichts trägt.
//
// Bisher hinterließ jeder Health-Ping je Server und Viertelstunde eine Zeile
// mit Kommando und Ausgabe, aufbewahrt so lange wie jedes andere Protokoll -
// bei dreihundert Servern über neunzig Tage einige Millionen Zeilen für ein
// „echo". Die Sitzung selbst bleibt erhalten: Auf derselben Verbindung laufen
// die Grundsatz-Regeln, die Speichermessung und liegengebliebene
// Benutzer-Abgleiche, und die gehören sehr wohl ins Protokoll.
var trivialCommands = map[string]bool{healthProbeCmd: true}

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

// RunStdin protokolliert wie Run, speist dem Kommando aber stdin ein. Der
// stdin-Inhalt (z.B. ein sudo-Passwort) wird BEWUSST nicht mitgeschrieben -
// nur das Kommando (redigiert) landet im Protokoll.
func (c *recordingConn) RunStdin(cmd, stdin string) (string, int, error) {
	start := time.Now()
	out, code, err := c.inner.RunStdin(cmd, stdin)
	if trivialCommands[strings.TrimSpace(cmd)] {
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
		StartedAt:    start,
		DurationMs:   time.Since(start).Milliseconds(),
		Command:      redactSecrets(cmd),
		Output:       truncateOutput(redactSecrets(out)),
		ExitCode:     code,
	}
	if err != nil {
		entry.Err = err.Error()
		c.hadError = true
	}
	if code != 0 {
		c.hadError = true
	}
	_ = c.logs.AddCommand(entry)

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
	buf    strings.Builder
	closed bool
}

// transcriptCap begrenzt, wie viel einer Sitzung im Speicher gehalten wird.
// Das Doppelte der gespeicherten Menge - so bleiben Anfang und Ende erhalten,
// wenn truncateOutput am Ende mittig kürzt, ohne dass eine stundenlange
// Sitzung den Arbeitsspeicher füllt.
const transcriptCap = 2 * maxOutputBytes

func (t *recordingTerminal) Read(p []byte) (int, error) {
	n, err := t.Terminal.Read(p)
	if n > 0 {
		t.mu.Lock()
		if t.buf.Len() < transcriptCap {
			t.buf.Write(p[:n])
		}
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
	transcript := t.buf.String()
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
