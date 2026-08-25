package services

import (
	"context"
	"io"
	"log/slog"
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
	// attachJob registriert die Verbindung beim zugehörigen Job (optional
	// verdrahtet; JobService.AttachCloser) - Grundlage für den Job-Abbruch:
	// beim Abort wird die Verbindung zwangsweise geschlossen.
	attachJob func(jobID string, conn io.Closer)
}

func NewSSHRecorder(logs *repositories.SSHLogRepository) *SSHRecorder {
	return &SSHRecorder{logs: logs}
}

// WithJobAttach verdrahtet die Job-Registrierung geöffneter Verbindungen
// (für Abbruch/Watchdog). Optional.
func (r *SSHRecorder) WithJobAttach(fn func(jobID string, conn io.Closer)) *SSHRecorder {
	r.attachJob = fn
	return r
}

// Record legt eine Session an und umhüllt die Verbindung mit dem Protokoll-
// Decorator. Der Rückgabewert ersetzt die Original-Verbindung. Gehört die
// Verbindung zu einem Job, wird sie dort registriert (Abbruch-Mechanik).
func (r *SSHRecorder) Record(conn sshx.Conn, ctx SessionContext) sshx.Conn {
	if r == nil || r.logs == nil || conn == nil {
		return conn
	}
	if ctx.JobID != nil && r.attachJob != nil {
		r.attachJob(*ctx.JobID, conn)
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

// recordingConn ist der Protokoll-Decorator um eine echte SSH-Verbindung.
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

// RunStdin protokolliert wie Run, speist dem Kommando aber stdin ein. Der
// stdin-Inhalt (z.B. ein sudo-Passwort) wird BEWUSST nicht mitgeschrieben -
// nur das Kommando (redigiert) landet im Protokoll.
func (c *recordingConn) RunStdin(cmd, stdin string) (string, int, error) {
	start := time.Now()
	out, code, err := c.inner.RunStdin(cmd, stdin)
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
