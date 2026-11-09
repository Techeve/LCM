package services_test

import (
	"sync"
	"testing"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/storage/repositories"
)

// laufendeConn hält ein Kommando offen, bis der Test es freigibt.
type laufendeConn struct {
	sshx.Conn
	gestartet chan struct{}
	frei      chan struct{}

	mu      sync.Mutex
	bisher  string
	endlich string
}

func (c *laufendeConn) RunStdin(string, string) (string, int, error) {
	close(c.gestartet)
	<-c.frei
	return c.endlich, 0, nil
}

func (c *laufendeConn) Run(cmd string) (string, int, error) { return c.RunStdin(cmd, "") }

func (c *laufendeConn) OnActivity(func()) {}

func (c *laufendeConn) Close() error { return nil }

func (c *laufendeConn) PartialOutput() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.bisher
}

func (c *laufendeConn) schreibe(ausgabe string) {
	c.mu.Lock()
	c.bisher = ausgabe
	c.mu.Unlock()
}

func erstesKommando(t *testing.T, logs *repositories.SSHLogRepository, jobID string) (domain.SSHSession, domain.SSHCommand) {
	t.Helper()
	sessions, err := logs.SessionsForJob(jobID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || len(sessions[0].Commands) != 1 {
		t.Fatalf("erwartet eine Sitzung mit einem Kommando: %+v", sessions)
	}
	return sessions[0], sessions[0].Commands[0]
}

// TestRecorderShowsRunningCommand: Das Protokoll zeigt ein Kommando, solange es
// läuft, samt mitgezähltem Kommando und fertigen Zeilen, nicht erst am Ende.
func TestRecorderShowsRunningCommand(t *testing.T) {
	env := newTestEnv(t)
	alterTakt := services.LiveOutputInterval
	services.LiveOutputInterval = 5 * time.Millisecond
	t.Cleanup(func() { services.LiveOutputInterval = alterTakt })

	logs := repositories.NewSSHLogRepository(env.DB())
	inner := &laufendeConn{gestartet: make(chan struct{}), frei: make(chan struct{}), endlich: "erste\nzweite\n"}
	jobID := "job-live"
	conn := services.NewSSHRecorder(logs).Record(inner, services.SessionContext{JobID: &jobID, Purpose: "rule:Update"})

	fertig := make(chan struct{})
	go func() {
		defer close(fertig)
		_, _, _ = conn.Run("apt-get upgrade")
	}()
	<-inner.gestartet

	session, kommando := erstesKommando(t, logs, jobID)
	if session.CommandCount != 1 || !kommando.Running || kommando.Command != "apt-get upgrade" || kommando.ExitCode != -1 {
		t.Fatalf("laufendes Kommando fehlt: count=%d %+v", session.CommandCount, kommando)
	}

	inner.schreibe("erste\nhalbe Ze")
	frist := time.Now().Add(2 * time.Second)
	for kommando.Output != "erste\n" {
		if time.Now().After(frist) {
			t.Fatalf("Ausgabe kam nicht an: %q", kommando.Output)
		}
		time.Sleep(5 * time.Millisecond)
		_, kommando = erstesKommando(t, logs, jobID)
	}

	close(inner.frei)
	<-fertig
	_, kommando = erstesKommando(t, logs, jobID)
	if kommando.Running || kommando.ExitCode != 0 || kommando.Output != "erste\nzweite\n" {
		t.Fatalf("Ergebnis nicht nachgetragen: %+v", kommando)
	}
}
