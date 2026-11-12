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
	"LCM/internal/core/events"
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
	after   func(ok bool)
}

// waitForAfter wartet, bis der Update-Job angestoßen ist, und liefert seine
// Abschlussfunktion. Die Phase steht schon vor dem Anstoß auf running, ein
// Blick nur auf die Phase reicht deshalb auf einem langsamen Runner nicht.
func (s *startedJob) waitForAfter(t *testing.T) func(bool) {
	t.Helper()
	var after func(bool)
	waitFor(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		after = s.after
		return after != nil
	})
	return after
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
	runJob := func(jobType, name, script, _ string, after func(bool)) (*domain.Job, error) {
		started.mu.Lock()
		started.jobType, started.name, started.script = jobType, name, script
		started.after = after
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

	// Im eingeschränkten Modus darf der Service-User kein systemd-run - der
	// Lauf geht deshalb über das Helper-Unterkommando, das denselben Ablauf
	// als root trägt (siehe lcm-helper.sh, host-self-update).
	restricted := services.SelfUpdateScriptForTest(true)
	if restricted != "lcm-helper host-self-update" {
		t.Errorf("eingeschränkter Modus muss über den Helper laufen, bekam:\n%s", restricted)
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
	started.waitForAfter(t)
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
	started.waitForAfter(t)

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

// Jeder Phasenwechsel geht als system.build auf den Bus - damit die Oberfläche
// den Fortschritt nicht alle fünf Sekunden abfragen muss.
func TestSelfUpdateMeldetPhasenwechsel(t *testing.T) {
	env := newTestEnv(t)
	svc, _ := selfUpdateEnv(t, env, "1.27.0")
	bus := events.NewBus()
	svc.WithEvents(bus)
	sub := bus.Subscribe()
	defer sub.Close()

	if _, err := svc.Start("admin", false); err != nil {
		t.Fatal(err)
	}

	var phases []string
	deadline := time.After(2 * time.Second)
	for len(phases) == 0 || phases[len(phases)-1] != services.SelfUpdateRunning {
		select {
		case e := <-sub.Events():
			if e.Type == events.TypeSystemMaintenance {
				continue // die Sperre für alle - eigener Test
			}
			if e.Type != events.TypeSystemBuild {
				t.Fatalf("unerwartetes Ereignis %s", e.Type)
			}
			phases = append(phases, e.Data.(events.SystemBuildData).Phase)
		case <-deadline:
			t.Fatalf("Phasen bisher: %v", phases)
		}
	}
	if phases[0] != services.SelfUpdateWaiting {
		t.Errorf("erste Phase = %s, erwartet waiting (alle: %v)", phases[0], phases)
	}
}

// TestSelfUpdateSperrtNeueJobsBisZumNeustart: Vom Anfordern an startet kein
// neuer Job - sonst käme hinter dem letzten wartenden Job immer noch einer
// nach, und der Neustart träfe ihn. Der Update-Lauf selbst kommt durch.
func TestSelfUpdateSperrtNeueJobsBisZumNeustart(t *testing.T) {
	env := newTestEnv(t)
	svc, _ := selfUpdateEnv(t, env, "1.27.0")
	svc.WithMaintenanceGate(env.Jobs)

	other := joinTestServer(t, env, "web-01")
	running, err := env.Jobs.Start(&other, nil, domain.RuleTypeUpdate, "läuft schon", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Start("admin", false); err != nil {
		t.Fatal(err)
	}
	if !svc.Locked() || !env.Jobs.InMaintenance() {
		t.Fatal("nach dem Anfordern muss gesperrt sein")
	}
	if _, err := env.Jobs.Start(&other, nil, domain.RuleTypeReboot, "neuer Job", "kollege"); !errors.Is(err, services.ErrMaintenance) {
		t.Fatalf("neuer Job während des Wartens: %v", err)
	}

	env.Jobs.Complete(running, "", ptrIntTest(0), nil)
	waitFor(t, func() bool { return svc.Status().Phase == services.SelfUpdateRunning })
	if !env.Jobs.InMaintenance() {
		t.Error("auch während des Update-Laufs bleibt gesperrt")
	}
}

// TestSelfUpdateAbbrechenHebtDieSperreAuf: Solange das Update wartet, lässt es
// sich abbrechen, danach nicht mehr.
func TestSelfUpdateAbbrechenHebtDieSperreAuf(t *testing.T) {
	env := newTestEnv(t)
	svc, started := selfUpdateEnv(t, env, "1.27.0")
	svc.WithMaintenanceGate(env.Jobs)

	other := joinTestServer(t, env, "web-01")
	running, err := env.Jobs.Start(&other, nil, domain.RuleTypeUpdate, "läuft schon", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Start("admin", false); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Cancel("admin"); err != nil {
		t.Fatalf("abbrechen: %v", err)
	}
	if svc.Locked() || env.Jobs.InMaintenance() {
		t.Error("nach dem Abbruch darf nichts mehr gesperrt sein")
	}
	env.Jobs.Complete(running, "", ptrIntTest(0), nil)
	time.Sleep(30 * time.Millisecond)
	if started.snapshot().calls != 0 {
		t.Error("ein abgebrochenes Update darf nicht mehr starten")
	}
	if _, err := svc.Cancel("admin"); !errors.Is(err, services.ErrSelfUpdateNotCancelable) {
		t.Errorf("ohne wartendes Update: %v", err)
	}
}

// TestSelfUpdateGescheiterterLaufHebtDieSperreAuf: Scheitert der Update-Job,
// läuft LCM in der alten Version weiter - dann darf es nicht gesperrt bleiben.
func TestSelfUpdateGescheiterterLaufHebtDieSperreAuf(t *testing.T) {
	env := newTestEnv(t)
	svc, started := selfUpdateEnv(t, env, "1.27.0")
	svc.WithMaintenanceGate(env.Jobs)
	if _, err := svc.Start("admin", false); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return svc.Status().Phase == services.SelfUpdateRunning })
	after := started.waitForAfter(t)
	after(false)
	if st := svc.Status(); st.Phase != services.SelfUpdateFailed || st.Error == "" {
		t.Errorf("erwartet failed mit Begründung, bekam %+v", st)
	}
	if env.Jobs.InMaintenance() {
		t.Error("nach dem Fehlschlag muss die Sperre fallen")
	}
}

// TestSelfUpdateOhneNeustartHebtDieSperreAuf: Gelingt der Anstoß, aber LCM
// startet nicht neu (apt in der eigenen Unit gescheitert), fällt die Sperre
// nach der Frist.
func TestSelfUpdateOhneNeustartHebtDieSperreAuf(t *testing.T) {
	env := newTestEnv(t)
	svc, started := selfUpdateEnv(t, env, "1.27.0")
	svc.WithMaintenanceGate(env.Jobs)
	limit := services.SelfUpdateRestartLimit
	services.SelfUpdateRestartLimit = 10 * time.Millisecond
	t.Cleanup(func() { services.SelfUpdateRestartLimit = limit })

	if _, err := svc.Start("admin", false); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return svc.Status().Phase == services.SelfUpdateRunning })
	after := started.waitForAfter(t)
	after(true)
	if st := svc.Status(); st.Phase != services.SelfUpdateFailed || !strings.Contains(st.Error, "journalctl") {
		t.Errorf("erwartet failed mit Hinweis aufs Protokoll, bekam %+v", st)
	}
	if env.Jobs.InMaintenance() {
		t.Error("ohne Neustart muss die Sperre nach der Frist fallen")
	}
}

// TestSelfUpdateAbbruchUndNeustartLaufenNichtDoppelt: Wer abbricht und gleich
// neu anfordert, bekommt genau einen Lauf. Der alte darf weder weitermachen
// noch die Sperre des neuen aufheben.
func TestSelfUpdateAbbruchUndNeustartLaufenNichtDoppelt(t *testing.T) {
	env := newTestEnv(t)
	svc, started := selfUpdateEnv(t, env, "1.27.0")
	svc.WithMaintenanceGate(env.Jobs)

	other := joinTestServer(t, env, "web-01")
	running, err := env.Jobs.Start(&other, nil, domain.RuleTypeUpdate, "läuft schon", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Start("admin", false); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Cancel("admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Start("admin", false); err != nil {
		t.Fatal(err)
	}
	if !env.Jobs.InMaintenance() {
		t.Fatal("der neue Lauf muss sperren")
	}

	env.Jobs.Complete(running, "", ptrIntTest(0), nil)
	waitFor(t, func() bool { return svc.Status().Phase == services.SelfUpdateRunning })
	started.waitForAfter(t)
	time.Sleep(30 * time.Millisecond) // dem alten Lauf Gelegenheit geben, sich (fälschlich) zu melden
	if n := started.snapshot().calls; n != 1 {
		t.Errorf("erwartet genau einen Update-Lauf, gestartet wurden %d", n)
	}
	if !env.Jobs.InMaintenance() {
		t.Error("die Sperre muss bis zum Neustart stehen")
	}
}
