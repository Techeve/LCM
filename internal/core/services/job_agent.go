package services

import (
	"errors"
	"log/slog"
	"strconv"

	"LCM/internal/core/domain"
	"LCM/internal/remote/wire"
	"LCM/internal/storage/repositories"
)

// Agent-Kommandos eines Jobs, deren Ergebnis erst nach seinem Ende eintrifft:
// Der Agent führt ein Kommando auch dann zu Ende, wenn LCM neu startet oder
// aufgibt zu warten. Der Hub (remote.JobTracker) meldet dazu, welches Kommando
// zu welchem Job gehört; kommt das Ergebnis ohne Wartenden, trägt LateResult es
// im Job nach.

// CommandSent hält fest, dass der Job ein Kommando an den Agent schickt - noch
// vor dem Senden, damit ein Neustart dazwischen nichts verliert.
func (s *JobService) CommandSent(jobID, reqID string) {
	if err := s.jobs.SetAgentRequestID(jobID, reqID); err != nil {
		slog.Error("agent command could not be recorded on job", "job", jobID, "error", err)
	}
}

// CommandDone nimmt das Kommando aus dem Job, sobald sein Ausgang bekannt ist
// (Ergebnis erhalten, nie angekommen oder abgebrochen).
func (s *JobService) CommandDone(jobID, reqID string) {
	if _, err := s.jobs.ClearAgentRequestID(jobID, reqID); err != nil {
		slog.Error("agent command could not be cleared from job", "job", jobID, "error", err)
	}
}

// LateResult trägt das Ergebnis eines Agent-Kommandos im Job nach, der es
// abgeschickt hat. Gibt zurück, ob ein Job es übernommen hat.
//
// Nur ein bereits fehlgeschlagener Job nimmt es an: Ein laufender hat seinen
// eigenen Wartenden, und alles andere ist anderweitig abgeschlossen. Der Agent
// muss der des Job-Servers sein - die Kennung allein genügt nicht, sonst könnte
// ein Agent in fremde Jobs schreiben. Wie beim Neustart-Abschluss
// (FailInterruptedOnStartup) meldet das Nachtragen kein Ergebnis an die
// Betriebsfähigkeit des Servers.
func (s *JobService) LateResult(agentID string, res wire.Result) bool {
	job, err := s.jobs.FindByAgentRequestID(res.ID)
	if err != nil {
		if !errors.Is(err, repositories.ErrNotFound) {
			slog.Error("job for late agent result could not be looked up", "id", res.ID, "error", err)
		}
		return false
	}
	if job.Server == nil || job.Server.AgentID != agentID || job.Status != domain.JobStatusFailed {
		return false
	}
	// Die Kennung zuerst abholen: Trifft dasselbe Ergebnis zweimal ein, trägt
	// nur das erste nach.
	claimed, err := s.jobs.ClearAgentRequestID(job.ID, res.ID)
	if err != nil || !claimed {
		return false
	}
	applyLateResult(job, res)
	if err := s.jobs.Update(job); err != nil {
		slog.Error("late agent result could not be saved", "job", job.ID, "error", err)
		// Die Kennung zurückgeben, damit eine Wiederholung des Ergebnisses
		// den Job noch findet.
		if restoreErr := s.jobs.SetAgentRequestID(job.ID, res.ID); restoreErr != nil {
			slog.Error("agent command id could not be restored on job", "job", job.ID, "error", restoreErr)
		}
		return false
	}
	slog.Info("late agent result recorded on job", "job", job.ID, "exit", res.ExitCode, "error", res.Error)
	return true
}

// singleCommandJobTypes sind die Job-Typen, deren Agent-Kommando der Kern des
// Jobs ist: Nach ihm folgt höchstens das Einlesen des Bestands (Paket- oder
// Docker-Inventar, CVE-Bewertung), das der nächste Scan nachholt. Fehlt ein Typ
// hier, gilt ein später eintreffendes Ergebnis nie als Erfolg des Jobs - im
// Zweifel bleibt er fehlgeschlagen.
//
// Nicht dazu gehören Jobs mit mehreren Schritten, die aufeinander aufbauen
// (agent-update: Paketliste, Installation, Probe; custom: Kommandoliste;
// docker-*-update: je Image), und solche, deren Zweck der Folgeschritt ist
// (package-scan: das Einlesen der Paketliste).
var singleCommandJobTypes = map[string]bool{
	domain.RuleTypeScript:      true,
	domain.RuleTypeUpdate:      true,
	domain.RuleTypePackages:    true,
	domain.RuleTypeSecurity:    true,
	domain.RuleTypeAutoremove:  true,
	domain.RuleTypeDockerPrune: true,
}

// applyLateResult schreibt Ausgang und Ausgabe des Agent-Kommandos in den Job.
// Erfolg gilt nur, wenn das Kommando der einzige Schritt des Jobs war und mit
// Exit-Code 0 endete; sonst blieben Schritte ungetan, und der Job bleibt
// fehlgeschlagen.
func applyLateResult(job *domain.Job, res wire.Result) {
	code := res.ExitCode
	if code == 0 && res.Error != "" {
		code = 1 // ein Fehler ist nie Exit-Code 0 (siehe complete)
	}
	single := singleCommandJobTypes[job.Type]
	job.Output += lateResultNote(res, code, single)
	job.ExitCode = &code
	job.Status = domain.JobStatusFailed
	if code == 0 && single {
		job.Status = domain.JobStatusSuccess
	}
}

// lateResultNote ist der Textblock, den der Job zum nachgetragenen Ergebnis
// bekommt. Die Ausgabe läuft durch dieselbe Schwärzung wie jede Job-Ausgabe.
func lateResultNote(res wire.Result, code int, single bool) string {
	note := "\n\nNACHGETRAGEN: Der Agent hat das Kommando zu Ende geführt, nachdem LCM die Verbindung verloren hatte" +
		" (Exit-Code " + strconv.Itoa(code) + ")."
	switch {
	case single:
		note += " Es war der letzte Schritt dieses Jobs; der nächste Scan bringt den Bestand auf Stand."
	case code == 0:
		note += " Das Kommando war erfolgreich, die folgenden Schritte dieses Jobs liefen aber nicht;" +
			" der Job bleibt fehlgeschlagen. Der nächste Scan bringt den Bestand auf Stand."
	default:
		note += " Die folgenden Schritte dieses Jobs liefen nicht; der nächste Scan bringt den Bestand auf Stand."
	}
	note += "\n" + redactSecrets(res.Output)
	if res.Truncated {
		note += "\n[Ausgabe vom Agent gekürzt - Limit überschritten]"
	}
	if res.Error != "" {
		note += "\nFEHLER: " + redactSecrets(res.Error)
	}
	return note
}
