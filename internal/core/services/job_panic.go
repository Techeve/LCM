package services

import (
	"fmt"

	"LCM/internal/core/domain"
)

// jobPanicCleanup liefert die Aufräumfunktion für safego.GoCleanup rund um
// einen Job-Runner.
//
// Warum das nötig ist: Jobs halten eine Sperre auf ihren Server
// (JobService.HasRunningForServer) - solange ein Job als „läuft" gilt, lehnt
// LCM jede weitere Aktion auf diesem Server ab. Bricht ein Runner durch einen
// Panic ab, ohne den Job abzuschließen, bliebe der Server für ALLE Aktionen
// blockiert, bis der Dienst neu startet (erst dann räumt
// FailInterruptedOnStartup auf).
//
// Der abgefangene Panic beendet den Job daher sauber als fehlgeschlagen: Die
// Sperre fällt, der Fehler steht in der Job-Historie, und der Anwender sieht
// in der Oberfläche, dass die Aktion nicht durchgelaufen ist - statt eines
// ewig „laufenden" Jobs.
func jobPanicCleanup(jobs *JobService, job *domain.Job) func(error) {
	return func(err error) {
		jobs.Complete(job, "", nil,
			fmt.Errorf("interner fehler - die aktion wurde abgebrochen: %w", err))
	}
}
