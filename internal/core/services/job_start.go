package services

import (
	"LCM/internal/core/domain"
	"LCM/internal/safego"
)

// startJob führt fn als Runner des bereits angelegten Jobs im Hintergrund
// aus und gibt dem Aufrufer eine Momentaufnahme des Jobs zurück.
//
// Warum eine Kopie: Der Runner schreibt Status, Ausgabe und Ende in den
// übergebenen *Job. Reichte der Aufrufer (Controller, Antwort-JSON) denselben
// Zeiger weiter, läse er die Felder, während der Runner sie beschreibt - ein
// Datenwettlauf. Die Kopie entsteht VOR dem Start der Goroutine, sonst
// wäre schon sie ein Wettlauf. Die Zeigerfelder (ExitCode, StartedAt,
// FinishedAt) teilen sich Kopie und Original, doch der Runner ersetzt sie nur
// durch neue Zeiger und verändert die Werte dahinter nie.
//
// Neue Jobs starten immer hierüber, nie mit eigenem safego.GoCleanup.
func startJob(jobs *JobService, name string, job *domain.Job, fn func()) *domain.Job {
	snapshot := *job
	safego.GoCleanup(name, jobPanicCleanup(jobs, job), fn)
	return &snapshot
}
