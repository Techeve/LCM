package services_test

import (
	"io"
	"strings"
	"testing"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/remote/wire"
	"LCM/internal/storage/repositories"
)

const lateAgentID = "0e0a6cbb-aaaa-bbbb-cccc-1234567890ab"

// startAgentJob legt einen laufenden Update-Job auf einem Agent-Server an, der
// gerade das Kommando "req-1" ausführt.
func startAgentJob(t *testing.T, env *testEnv) *domain.Job {
	t.Helper()
	return startAgentJobOfType(t, env, domain.RuleTypeUpdate)
}

// startAgentJobOfType wie startAgentJob für einen beliebigen Job-Typ.
func startAgentJobOfType(t *testing.T, env *testEnv, jobType string) *domain.Job {
	t.Helper()
	serverID := joinTestServer(t, env, "agent01")
	if err := env.DB().Model(&domain.Server{}).Where("id = ?", serverID).Update("agent_id", lateAgentID).Error; err != nil {
		t.Fatalf("agent-id setzen: %v", err)
	}
	job, err := env.Jobs.Start(&serverID, nil, jobType, "Job @ agent01", "admin")
	if err != nil {
		t.Fatalf("job-start: %v", err)
	}
	env.Jobs.CommandSent(job.ID, "req-1")
	return job
}

// interruptedAgentJob ist ein Job, den ein LCM-Neustart unterbrochen hat, während
// der Agent sein Kommando ausführte.
func interruptedAgentJob(t *testing.T, env *testEnv) domain.Job {
	t.Helper()
	return interruptedAgentJobOfType(t, env, domain.RuleTypeUpdate)
}

// interruptedAgentJobOfType wie interruptedAgentJob für einen beliebigen Job-Typ.
func interruptedAgentJobOfType(t *testing.T, env *testEnv, jobType string) domain.Job {
	t.Helper()
	job := startAgentJobOfType(t, env, jobType)
	env.Jobs.FailInterruptedOnStartup(nil)
	return jobStatus(t, env, job.ID)
}

// TestNeustartLaesstAgentKommandoNachtragbar: Ein Job mit laufendem Agent-
// Kommando bekommt beim Neustart den Hinweis "UNTERBROCHEN" und behält die
// Kennung, damit das Ergebnis ihn findet (B18).
func TestNeustartLaesstAgentKommandoNachtragbar(t *testing.T) {
	env := newTestEnv(t)
	job := interruptedAgentJob(t, env)

	if job.Status != domain.JobStatusFailed {
		t.Errorf("status = %q, want failed (Server-Sperre muss frei werden)", job.Status)
	}
	if !strings.Contains(job.Output, "UNTERBROCHEN") || strings.Contains(job.Output, "ABGEBROCHEN") {
		t.Errorf("output = %q", job.Output)
	}
	if job.AgentRequestID != "req-1" {
		t.Errorf("AgentRequestID = %q, want req-1", job.AgentRequestID)
	}
}

// TestJobUpdateLoeschtAgentKennungNicht: Der Abschluss eines Jobs aus dem
// Speicher (ohne Kennung) darf die in der Datenbank gesetzte nicht löschen.
func TestJobUpdateLoeschtAgentKennungNicht(t *testing.T) {
	env := newTestEnv(t)
	job := startAgentJob(t, env)

	env.Jobs.Complete(job, "fertig", ptrExit(0), nil)

	if got := jobStatus(t, env, job.ID); got.AgentRequestID != "req-1" {
		t.Errorf("AgentRequestID = %q, want req-1", got.AgentRequestID)
	}
}

func ptrExit(code int) *int { return &code }

func TestSpaetesErgebnisErfolgWirdNachgetragen(t *testing.T) {
	env := newTestEnv(t)
	job := interruptedAgentJob(t, env)

	if !env.Jobs.LateResult(lateAgentID, wire.Result{ID: "req-1", Output: "3 Pakete aktualisiert", ExitCode: 0}) {
		t.Fatal("LateResult hat das Ergebnis nicht übernommen")
	}

	got := jobStatus(t, env, job.ID)
	if got.Status != domain.JobStatusSuccess || got.ExitCode == nil || *got.ExitCode != 0 {
		t.Errorf("status = %q, exit = %v", got.Status, got.ExitCode)
	}
	for _, want := range []string{"UNTERBROCHEN", "NACHGETRAGEN", "Exit-Code 0", "3 Pakete aktualisiert"} {
		if !strings.Contains(got.Output, want) {
			t.Errorf("output ohne %q: %q", want, got.Output)
		}
	}
	if got.AgentRequestID != "" {
		t.Errorf("AgentRequestID = %q, want leer", got.AgentRequestID)
	}
}

// TestSpaetesErgebnisEinzelschrittSagtNichtDassSchritteFehlten: Bei einem Job, dessen
// Kommando der letzte Schritt war, behauptet der Hinweis keine übersprungenen Schritte.
func TestSpaetesErgebnisEinzelschrittSagtNichtDassSchritteFehlten(t *testing.T) {
	env := newTestEnv(t)
	job := interruptedAgentJob(t, env)

	env.Jobs.LateResult(lateAgentID, wire.Result{ID: "req-1", ExitCode: 0})

	got := jobStatus(t, env, job.ID)
	if strings.Contains(got.Output, "liefen") || strings.Contains(got.Output, "bleibt fehlgeschlagen") {
		t.Errorf("output behauptet übersprungene Schritte: %q", got.Output)
	}
}

// TestSpaetesErgebnisMehrschrittJobBleibtFehlgeschlagen: Ein Job mit weiteren
// Schritten (hier: Agent-Update, Paketliste vor Installation und Probe) ist nach
// dem späten Erfolg des ersten Kommandos nicht erfolgreich - die übrigen
// Schritte liefen nie. Exit-Code und Ausgabe stehen trotzdem im Job.
func TestSpaetesErgebnisMehrschrittJobBleibtFehlgeschlagen(t *testing.T) {
	env := newTestEnv(t)
	job := interruptedAgentJobOfType(t, env, domain.JobTypeAgentUpdate)

	res := wire.Result{ID: "req-1", Output: "Paketliste aktuell", ExitCode: 0}
	if !env.Jobs.LateResult(lateAgentID, res) {
		t.Fatal("LateResult hat das Ergebnis nicht übernommen")
	}

	got := jobStatus(t, env, job.ID)
	if got.Status != domain.JobStatusFailed || got.ExitCode == nil || *got.ExitCode != 0 {
		t.Errorf("status = %q, exit = %v, erwartet failed mit Exit-Code 0", got.Status, got.ExitCode)
	}
	for _, want := range []string{"NACHGETRAGEN", "Exit-Code 0", "Paketliste aktuell", "erfolgreich", "Schritte dieses Jobs liefen aber nicht", "bleibt fehlgeschlagen"} {
		if !strings.Contains(got.Output, want) {
			t.Errorf("output ohne %q: %q", want, got.Output)
		}
	}
	if got.AgentRequestID != "" {
		t.Errorf("AgentRequestID = %q, want leer", got.AgentRequestID)
	}
}

// TestSpaetesErgebnisErfolgNurFuerEinzelschrittTypen: Welche Job-Typen ein spätes
// Ergebnis zum Erfolg machen - und dass alles Unbekannte fehlgeschlagen bleibt.
func TestSpaetesErgebnisErfolgNurFuerEinzelschrittTypen(t *testing.T) {
	for jobType, wantSuccess := range map[string]bool{
		domain.RuleTypeScript:      true,
		domain.RuleTypeUpdate:      true,
		domain.RuleTypePackages:    true,
		domain.RuleTypeSecurity:    true,
		domain.RuleTypeAutoremove:  true,
		domain.RuleTypeDockerPrune: true,

		domain.JobTypeAgentUpdate:          false,
		domain.RuleTypeCustom:              false,
		domain.RuleTypePackageScan:         false,
		domain.RuleTypeHealth:              false,
		domain.RuleTypeDockerUpdateUnused:  false,
		domain.RuleTypeDockerComposeUpdate: false,
		domain.JobTypeJoin:                 false,
		"unbekannter-typ":                  false,
	} {
		t.Run(jobType, func(t *testing.T) {
			env := newTestEnv(t)
			job := interruptedAgentJobOfType(t, env, jobType)

			env.Jobs.LateResult(lateAgentID, wire.Result{ID: "req-1", ExitCode: 0})

			got := jobStatus(t, env, job.ID)
			if (got.Status == domain.JobStatusSuccess) != wantSuccess {
				t.Errorf("status = %q, Erfolg erwartet: %v", got.Status, wantSuccess)
			}
			if got.ExitCode == nil || *got.ExitCode != 0 {
				t.Errorf("exit = %v, erwartet 0", got.ExitCode)
			}
		})
	}
}

func TestSpaetesErgebnisFehlerBehaeltExitCode(t *testing.T) {
	env := newTestEnv(t)
	job := interruptedAgentJob(t, env)

	res := wire.Result{ID: "req-1", Output: "E: Sperre", ExitCode: 1, Truncated: true, Error: "exit status 1"}
	if !env.Jobs.LateResult(lateAgentID, res) {
		t.Fatal("LateResult hat das Ergebnis nicht übernommen")
	}

	got := jobStatus(t, env, job.ID)
	if got.Status != domain.JobStatusFailed || got.ExitCode == nil || *got.ExitCode != 1 {
		t.Errorf("status = %q, exit = %v", got.Status, got.ExitCode)
	}
	for _, want := range []string{"E: Sperre", "Exit-Code 1", "gekürzt", "exit status 1"} {
		if !strings.Contains(got.Output, want) {
			t.Errorf("output ohne %q: %q", want, got.Output)
		}
	}
}

// TestSpaetesErgebnisNurEinmal: Dasselbe Ergebnis, zweimal zugestellt, wird nur
// einmal eingetragen.
func TestSpaetesErgebnisNurEinmal(t *testing.T) {
	env := newTestEnv(t)
	job := interruptedAgentJob(t, env)
	res := wire.Result{ID: "req-1", Output: "einmal", ExitCode: 0}

	env.Jobs.LateResult(lateAgentID, res)
	if env.Jobs.LateResult(lateAgentID, res) {
		t.Error("zweites Ergebnis wurde übernommen")
	}
	if got := jobStatus(t, env, job.ID); strings.Count(got.Output, "einmal") != 1 {
		t.Errorf("output = %q", got.Output)
	}
}

func TestSpaetesErgebnisFremderAgentRuehrtJobNichtAn(t *testing.T) {
	env := newTestEnv(t)
	job := interruptedAgentJob(t, env)

	if env.Jobs.LateResult("ffffffff-aaaa-bbbb-cccc-1234567890ab", wire.Result{ID: "req-1", ExitCode: 0}) {
		t.Fatal("Ergebnis eines fremden Agents wurde übernommen")
	}

	got := jobStatus(t, env, job.ID)
	if got.Status != domain.JobStatusFailed || got.AgentRequestID != "req-1" || strings.Contains(got.Output, "NACHGETRAGEN") {
		t.Errorf("job verändert: %q / %q / %q", got.Status, got.AgentRequestID, got.Output)
	}
}

func TestSpaetesErgebnisLaufenderJobBleibtUnberuehrt(t *testing.T) {
	env := newTestEnv(t)
	job := startAgentJob(t, env)

	if env.Jobs.LateResult(lateAgentID, wire.Result{ID: "req-1", ExitCode: 0}) {
		t.Fatal("laufender Job hat ein spätes Ergebnis übernommen")
	}

	got := jobStatus(t, env, job.ID)
	if got.Status != domain.JobStatusRunning || got.AgentRequestID != "req-1" {
		t.Errorf("job verändert: %q / %q", got.Status, got.AgentRequestID)
	}
}

func TestSpaetesErgebnisUnbekannteKennung(t *testing.T) {
	env := newTestEnv(t)
	interruptedAgentJob(t, env)

	if env.Jobs.LateResult(lateAgentID, wire.Result{ID: "unbekannt", ExitCode: 0}) {
		t.Error("unbekanntes Kommando wurde übernommen")
	}
}

// TestSpaetesErgebnisOhneKennungTrifftNie: Ein Job ohne Kommando hat eine leere
// Kennung; ein Ergebnis mit leerer ID darf ihn nie finden.
func TestSpaetesErgebnisOhneKennungTrifftNie(t *testing.T) {
	env := newTestEnv(t)
	job := interruptedAgentJob(t, env)
	env.Jobs.CommandDone(job.ID, "req-1")

	if env.Jobs.LateResult(lateAgentID, wire.Result{ID: "", Output: "x", ExitCode: 0}) {
		t.Fatal("Ergebnis mit leerer ID wurde übernommen")
	}
	if got := jobStatus(t, env, job.ID); strings.Contains(got.Output, "NACHGETRAGEN") {
		t.Errorf("output = %q", got.Output)
	}
}

// TestSpaetesErgebnisSchwaerztFehlertext: Auch der Fehlertext des Agents läuft
// durch die Schwärzung.
func TestSpaetesErgebnisSchwaerztFehlertext(t *testing.T) {
	env := newTestEnv(t)
	job := interruptedAgentJob(t, env)

	res := wire.Result{ID: "req-1", ExitCode: 1, Error: "curl -u admin:geheim123 https://x"}
	if !env.Jobs.LateResult(lateAgentID, res) {
		t.Fatal("LateResult hat das Ergebnis nicht übernommen")
	}
	if got := jobStatus(t, env, job.ID); strings.Contains(got.Output, "geheim123") {
		t.Errorf("Passwort im Output: %q", got.Output)
	}
}

// TestSpaetesErgebnisGibtKennungBeiSpeicherfehlerZurueck: Scheitert das
// Speichern, bleibt die Kennung stehen, und eine Wiederholung trägt nach.
func TestSpaetesErgebnisGibtKennungBeiSpeicherfehlerZurueck(t *testing.T) {
	env := newTestEnv(t)
	job := interruptedAgentJob(t, env)
	// Update schreibt die Ausgabe; das Setzen der Kennung berührt sie nicht.
	trigger := `CREATE TRIGGER no_output_update BEFORE UPDATE OF output ON jobs
		BEGIN SELECT RAISE(ABORT, 'gesperrt'); END`
	if err := env.DB().Exec(trigger).Error; err != nil {
		t.Fatalf("trigger: %v", err)
	}
	res := wire.Result{ID: "req-1", Output: "fertig", ExitCode: 0}

	if env.Jobs.LateResult(lateAgentID, res) {
		t.Fatal("LateResult meldet Erfolg trotz Speicherfehler")
	}
	if got := jobStatus(t, env, job.ID); got.AgentRequestID != "req-1" {
		t.Fatalf("AgentRequestID = %q, want req-1", got.AgentRequestID)
	}

	if err := env.DB().Exec("DROP TRIGGER no_output_update").Error; err != nil {
		t.Fatal(err)
	}
	if !env.Jobs.LateResult(lateAgentID, res) {
		t.Error("Wiederholung trug das Ergebnis nicht nach")
	}
}

// TestCommandDoneLoeschtNurDieEigeneKennung: Ein späteres Kommando desselben Jobs
// bleibt stehen, wenn der Abschluss des früheren eintrifft.
func TestCommandDoneLoeschtNurDieEigeneKennung(t *testing.T) {
	env := newTestEnv(t)
	job := startAgentJob(t, env)
	env.Jobs.CommandSent(job.ID, "req-2")

	env.Jobs.CommandDone(job.ID, "req-1")
	if got := jobStatus(t, env, job.ID); got.AgentRequestID != "req-2" {
		t.Errorf("AgentRequestID = %q, want req-2", got.AgentRequestID)
	}
	env.Jobs.CommandDone(job.ID, "req-2")
	if got := jobStatus(t, env, job.ID); got.AgentRequestID != "" {
		t.Errorf("AgentRequestID = %q, want leer", got.AgentRequestID)
	}
}

// bindConn ist eine Verbindung, die ihren Job erfahren will (wie die des Agents).
type bindConn struct {
	sshx.Conn
	jobID string
}

func (c *bindConn) BindJob(jobID string) { c.jobID = jobID }
func (c *bindConn) OnActivity(func())    {}

// nopJobLink nimmt Verbindung und Lebenszeichen eines Jobs entgegen.
type nopJobLink struct{}

func (nopJobLink) AttachCloser(string, io.Closer) {}
func (nopJobLink) MarkActivity(string)            {}

// TestRecorderBindetVerbindungAnJob: Der Recorder sagt einer Verbindung, für
// welchen Job sie geöffnet wurde - Grundlage dafür, dass der Hub ihre Kommandos
// dem Job zuordnet.
func TestRecorderBindetVerbindungAnJob(t *testing.T) {
	env := newTestEnv(t)
	recorder := services.NewSSHRecorder(repositories.NewSSHLogRepository(env.DB())).WithJobs(nopJobLink{})
	conn := &bindConn{}
	jobID := "job-9"

	recorder.Record(conn, services.SessionContext{JobID: &jobID, Purpose: "test"})

	if conn.jobID != jobID {
		t.Errorf("BindJob = %q, want %q", conn.jobID, jobID)
	}
}
