package services_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

// selfUpdateEnv baut den Dienst samt aller Vorbedingungen: LCM gilt als aus
// dem Paket installiert, der LCM-Host steht in der Verwaltung, und im
// Paketkanal liegt eine neuere Version. Die Rückgabe ist der Dienst plus ein
// Zeiger auf den zuletzt gestarteten Job-Aufruf.
type startedJob struct {
	mu      sync.Mutex
	jobType string
	name    string
	script  string
	calls   int
}

func (s *startedJob) snapshot() startedJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	return startedJob{jobType: s.jobType, name: s.name, script: s.script, calls: s.calls}
}

func selfUpdateEnv(t *testing.T, env *testEnv, latest string) (*services.SelfUpdateService, *startedJob) {
	t.Helper()
	// Diese Installation als Paket-Installation ausgeben: Das laufende
	// Test-Binary ist „das Binary aus dem Paket", die Unit-Datei legen wir an.
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("eigenes binary: %v", err)
	}
	unit := filepath.Join(t.TempDir(), "lcm.service")
	if err := os.WriteFile(unit, []byte("[Unit]\n"), 0o600); err != nil {
		t.Fatalf("unit-datei: %v", err)
	}
	services.SetSelfUpdatePathsForTest(exe, unit)
	t.Cleanup(func() { services.SetSelfUpdatePathsForTest("/usr/bin/lcm", "/lib/systemd/system/lcm.service") })

	poll, limit := services.SelfUpdatePoll, services.SelfUpdateWaitLimit
	services.SelfUpdatePoll = 5 * time.Millisecond
	t.Cleanup(func() { services.SelfUpdatePoll, services.SelfUpdateWaitLimit = poll, limit })

	lcmHostServer(t, env)

	started := &startedJob{}
	runJob := func(jobType, name, script, _ string, _ func(bool)) (*domain.Job, error) {
		started.mu.Lock()
		started.jobType, started.name, started.script = jobType, name, script
		started.calls++
		started.mu.Unlock()
		return &domain.Job{ID: "job-selbst-update"}, nil
	}
	status := func() (*services.UpdateStatus, error) {
		return &services.UpdateStatus{
			CurrentVersion: "1.26.0", LatestVersion: latest, UpdateAvailable: latest != "",
		}, nil
	}
	svc := services.NewSelfUpdateService(
		repositories.NewJobRepository(env.DB()), repositories.NewServerRepository(env.DB()),
		nil, runJob, status)
	// Die Tests gelten dem Host-Fall; im CI-Container wäre sonst schon die
	// erste Vorbedingung verletzt und jeder Test grün aus dem falschen Grund.
	svc.SetContainerCheckForTest(func() bool { return false })
	return svc, started
}

// TestSelfUpdateScriptSurvivesTheRestart hält den Kern des Entwurfs fest: Das
// Paket startet LCM neu und schneidet damit die SSH-Sitzung ab, aus der der
// Lauf kommt. Mit vollem sudo hängt der Lauf deshalb in einer eigenen
// systemd-Unit - sonst stürbe dpkg mitten im Einspielen.
func TestSelfUpdateScriptSurvivesTheRestart(t *testing.T) {
	script := services.SelfUpdateScriptForTest(false)
	for _, want := range []string{"systemd-run", "--unit=lcm-self-update", "apt-get", "--only-upgrade -y lcm"} {
		if !strings.Contains(script, want) {
			t.Errorf("skript enthält %q nicht:\n%s", want, script)
		}
	}

	// Im eingeschränkten Modus ist systemd-run nicht freigegeben - dort bleibt
	// es beim direkten apt-Lauf, und der Verbindungsabbruch gehört zum Ablauf.
	restricted := services.SelfUpdateScriptForTest(true)
	if strings.Contains(restricted, "systemd-run") {
		t.Errorf("eingeschränkter Modus darf systemd-run nicht verwenden:\n%s", restricted)
	}
	if !strings.Contains(restricted, "--only-upgrade -y lcm") {
		t.Errorf("eingeschränkter Modus aktualisiert das lcm-Paket nicht:\n%s", restricted)
	}
}

// TestSelfUpdateWaitsForRunningJobs ist die Zusage an den Bediener: Ein
// laufender Job wird NICHT vom Neustart mitgenommen. Solange er läuft, sagt
// der Zustand an, worauf gewartet wird; erst danach startet das Update.
func TestSelfUpdateWaitsForRunningJobs(t *testing.T) {
	env := newTestEnv(t)
	svc, started := selfUpdateEnv(t, env, "1.27.0")

	other := joinTestServer(t, env, "web-01")
	job, err := env.Jobs.Start(&other, nil, domain.RuleTypeUpdate, "Alle Pakete aktualisieren", "admin")
	if err != nil {
		t.Fatalf("job-start: %v", err)
	}

	if _, err := svc.Start("admin", false); err != nil {
		t.Fatalf("selbst-update anfordern: %v", err)
	}

	// Solange der fremde Job läuft, darf nichts eingespielt werden.
	waitFor(t, func() bool {
		st := svc.Status()
		return st.Phase == services.SelfUpdateWaiting && len(st.WaitingFor) == 1
	})
	if st := svc.Status(); st.WaitingFor[0] != "Alle Pakete aktualisieren" {
		t.Errorf("wartet auf %q, erwartet den Namen des laufenden Jobs", st.WaitingFor[0])
	}
	if started.snapshot().calls != 0 {
		t.Fatal("das Update wurde gestartet, obwohl noch ein Job lief")
	}

	env.Jobs.Complete(job, "fertig", ptrIntT(0), nil)

	waitFor(t, func() bool { return svc.Status().Phase == services.SelfUpdateRunning })
	call := started.snapshot()
	if call.jobType != domain.RuleTypeUpdate {
		// Nur diese Art wertet der Wiederanlauf als „durch das Update
		// abgeschnitten, aber erfolgreich" (self_update.go).
		t.Errorf("job-art = %q, erwartet %q", call.jobType, domain.RuleTypeUpdate)
	}
	if !strings.Contains(call.name, "1.27.0") {
		t.Errorf("job-name %q nennt die Zielversion nicht", call.name)
	}
}

// TestSelfUpdateNeedsANewerVersion: Ohne neuere Version gibt es nichts
// einzuspielen - das ist eine klare Absage, kein Lauf ins Leere.
func TestSelfUpdateNeedsANewerVersion(t *testing.T) {
	env := newTestEnv(t)
	svc, started := selfUpdateEnv(t, env, "")

	if _, err := svc.Start("admin", false); err == nil {
		t.Fatal("ohne neuere Version sollte das Selbst-Update abgelehnt werden")
	}
	if started.snapshot().calls != 0 {
		t.Error("es wurde trotzdem ein Job gestartet")
	}
}

// TestSelfUpdateOnlyForPackageInstalls: Ein selbst gebautes Binary bekommt
// kein apt-Update. Statt einer Schaltfläche, die scheitern muss, nennt der
// Zustand den Grund.
func TestSelfUpdateOnlyForPackageInstalls(t *testing.T) {
	env := newTestEnv(t)
	svc, _ := selfUpdateEnv(t, env, "1.27.0")
	services.SetSelfUpdatePathsForTest("/usr/bin/lcm", "/lib/systemd/system/lcm.service")

	st := svc.Status()
	if st.Supported {
		t.Fatal("eine Installation außerhalb des Pakets darf kein Selbst-Update anbieten")
	}
	if !strings.Contains(st.Reason, "Debian-Paket") {
		t.Errorf("grund = %q, sollte auf das Debian-Paket verweisen", st.Reason)
	}
	if _, err := svc.Start("admin", false); err == nil {
		t.Error("Start sollte mit demselben Grund abgelehnt werden")
	}
}

// TestSelfUpdateSichertVorher: die Zusage aus der Oberfläche - erst sichern,
// dann aktualisieren. Wer sein eigenes Verwaltungssystem aktualisiert, hat im
// Fehlerfall kein zweites, das ihm hilft.
func TestSelfUpdateSichertVorher(t *testing.T) {
	env := newTestEnv(t)
	svc, started := selfUpdateEnv(t, env, "1.27.0")

	var backupCalls int
	svc.WithBackup(func(string) (string, error) {
		backupCalls++
		return "lcm-backup-2026-08-23.tar.gz", nil
	})

	if _, err := svc.Start("admin", true); err != nil {
		t.Fatalf("selbst-update anfordern: %v", err)
	}
	waitFor(t, func() bool { return svc.Status().Phase == services.SelfUpdateRunning })

	if backupCalls != 1 {
		t.Errorf("Sicherung lief %d-mal, erwartet genau einmal", backupCalls)
	}
	if st := svc.Status(); st.BackupFile != "lcm-backup-2026-08-23.tar.gz" {
		t.Errorf("Sicherung steht nicht im Status: %q", st.BackupFile)
	}
	if started.snapshot().calls != 1 {
		t.Error("das Update wurde nach der Sicherung nicht eingespielt")
	}
}

// TestSelfUpdateBrichtOhneSicherungAb: Schlägt die Sicherung fehl, wird NICHT
// aktualisiert - sonst stünde man ohne Rückfallebene da, und zwar genau in dem
// Moment, in dem man sie am ehesten braucht.
func TestSelfUpdateBrichtOhneSicherungAb(t *testing.T) {
	env := newTestEnv(t)
	svc, started := selfUpdateEnv(t, env, "1.27.0")
	svc.WithBackup(func(string) (string, error) {
		return "", errors.New("keine backup-passphrase")
	})

	if _, err := svc.Start("admin", true); err != nil {
		t.Fatalf("selbst-update anfordern: %v", err)
	}
	waitFor(t, func() bool { return svc.Status().Phase == services.SelfUpdateFailed })

	if started.snapshot().calls != 0 {
		t.Error("trotz gescheiterter Sicherung wurde aktualisiert")
	}
	st := svc.Status()
	if !strings.Contains(st.Error, "keine backup-passphrase") {
		t.Errorf("die Ursache fehlt in der Meldung: %q", st.Error)
	}
	if !strings.Contains(st.Error, "NICHT eingespielt") {
		t.Errorf("die Meldung sagt nicht, dass nicht aktualisiert wurde: %q", st.Error)
	}
}
