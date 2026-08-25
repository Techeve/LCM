package services_test

import (
	"errors"
	"strings"
	"testing"

	"LCM/internal/storage/repositories"
)

// TestRefreshCVEDBLeiseBeiErfolg: Der 6-Stunden-Zug der Trivy-Datenbank ist
// im Erfolgsfall leise - vier Job-Einträge pro Tag wären Rauschen im
// Protokoll. Es zählt nur, dass der Download tatsächlich angestoßen wurde.
func TestRefreshCVEDBLeiseBeiErfolg(t *testing.T) {
	env := newTestEnv(t)

	env.Executor.RefreshCVEDB("scheduler")

	if env.Scanner.UpdateCalls != 1 {
		t.Fatalf("erwartet 1 Datenbank-Download, bekam %d", env.Scanner.UpdateCalls)
	}
	_, total, err := env.Jobs.HistoryFiltered(repositories.ScopeAll(), repositories.JobFilter{Limit: 10})
	if err != nil {
		t.Fatalf("HistoryFiltered: %v", err)
	}
	if total != 0 {
		t.Errorf("erfolgreicher DB-Zug darf keinen Job erzeugen, fand %d", total)
	}
}

// TestRefreshCVEDBFehlschlagErzeugtJob: Erst ein Fehlschlag gehört ins
// Protokoll - mit der vollen Trivy-Ausgabe, damit die Ursache (Proxy,
// Rate-Limit, kein Netz) nachlesbar ist statt nur im Log zu verpuffen.
func TestRefreshCVEDBFehlschlagErzeugtJob(t *testing.T) {
	env := newTestEnv(t)
	env.Scanner.UpdateDBFunc = func() (string, error) {
		return "FATAL: dial tcp: kein netz", errors.New("trivy-datenbank-update fehlgeschlagen")
	}

	env.Executor.RefreshCVEDB("scheduler")

	jobs, total, err := env.Jobs.HistoryFiltered(repositories.ScopeAll(), repositories.JobFilter{Limit: 10})
	if err != nil {
		t.Fatalf("HistoryFiltered: %v", err)
	}
	if total != 1 {
		t.Fatalf("erwartet genau 1 Fehler-Job, bekam %d", total)
	}
	job := jobs[0]
	if job.Name != "CVE-Datenbank aktualisieren" {
		t.Errorf("unerwarteter Job-Name %q", job.Name)
	}
	if job.Status != "failed" {
		t.Errorf("Job sollte failed sein, war %q", job.Status)
	}
	if !strings.Contains(job.Output, "kein netz") {
		t.Errorf("Trivy-Ausgabe fehlt im Job-Protokoll: %q", job.Output)
	}
}

// TestRefreshCVEDBOhneScannerNoop: Eine Instanz ohne Trivy soll nicht alle
// 6 Stunden einen Fehler produzieren - der Zug ist dann ein stilles No-op.
func TestRefreshCVEDBOhneScannerNoop(t *testing.T) {
	env := newTestEnv(t)
	env.Scanner.IsAvailable = false

	env.Executor.RefreshCVEDB("scheduler")

	if env.Scanner.UpdateCalls != 0 {
		t.Errorf("ohne verfügbaren Scanner darf kein Download laufen, lief %d-mal", env.Scanner.UpdateCalls)
	}
	_, total, _ := env.Jobs.HistoryFiltered(repositories.ScopeAll(), repositories.JobFilter{Limit: 10})
	if total != 0 {
		t.Errorf("ohne Scanner darf kein Job entstehen, fand %d", total)
	}
}

// TestCVEScanZiehtDatenbankVorab: Der nächtliche Gesamtscan holt sich die
// Datenbank direkt vor dem Durchlauf - er soll mit der frischesten Ausgabe
// arbeiten, nicht mit dem Stand des letzten 6-Stunden-Zugs.
func TestCVEScanZiehtDatenbankVorab(t *testing.T) {
	env := newTestEnv(t)
	joinTestServer(t, env, "web01")

	env.Executor.RunCVEScan("scheduler")

	if env.Scanner.UpdateCalls != 1 {
		t.Errorf("erwartet 1 Datenbank-Download vor dem Scan, bekam %d", env.Scanner.UpdateCalls)
	}
	if env.Scanner.Calls == 0 {
		t.Error("der eigentliche Scan ist nicht gelaufen")
	}
}

// TestCVEScanLaeuftTrotzDBFehlerWeiter: Scheitert der Download vor dem Scan,
// wird mit dem vorhandenen Stand gescannt - und der Grund steht benannt im
// Job, statt den ganzen Lauf abzubrechen.
func TestCVEScanLaeuftTrotzDBFehlerWeiter(t *testing.T) {
	env := newTestEnv(t)
	joinTestServer(t, env, "web01")
	env.Scanner.UpdateDBFunc = func() (string, error) {
		return "", errors.New("registry nicht erreichbar")
	}

	env.Executor.RunCVEScan("scheduler")

	if env.Scanner.Calls == 0 {
		t.Fatal("der Scan muss trotz DB-Fehler laufen")
	}
	jobs, _, err := env.Jobs.HistoryFiltered(repositories.ScopeAll(), repositories.JobFilter{Type: "cve-scan", Limit: 1})
	if err != nil || len(jobs) == 0 {
		t.Fatalf("CVE-Scan-Job nicht gefunden: %v", err)
	}
	if jobs[0].Status != "success" {
		t.Errorf("Scan-Job sollte success sein, war %q", jobs[0].Status)
	}
	if !strings.Contains(jobs[0].Output, "vorhandenen Stand") {
		t.Errorf("Hinweis auf den DB-Fehlschlag fehlt im Job: %q", jobs[0].Output)
	}
}
