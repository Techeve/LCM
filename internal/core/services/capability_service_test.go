package services

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// transitionLog sammelt, was OnTransition meldet.
type transitionLog struct {
	mu     sync.Mutex
	events []transitionEvent
}

type transitionEvent struct {
	serverID  uint
	category  string
	recovered bool
}

func (l *transitionLog) record(serverID uint, c domain.ServerCapability, recovered bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, transitionEvent{serverID, c.Category, recovered})
}

func (l *transitionLog) all() []transitionEvent {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]transitionEvent(nil), l.events...)
}

// DB direkt aufbauen: storage importiert services, ein storage-Import hier
// wäre ein Import-Zyklus (wie in job_panic_test.go).
func newCapabilityDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("test-db: %v", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	if err := db.AutoMigrate(&domain.Server{}, &domain.Job{}, &domain.ServerCapability{}); err != nil {
		t.Fatalf("migration: %v", err)
	}
	return db
}

func newCapabilityTest(t *testing.T) (*CapabilityService, *repositories.CapabilityRepository, *transitionLog) {
	t.Helper()
	repo := repositories.NewCapabilityRepository(newCapabilityDB(t))
	svc := NewCapabilityService(repo)
	log := &transitionLog{}
	svc.OnTransition(log.record)
	return svc, repo, log
}

const testServer uint = 7

func jobOutcome(jobType, status, output string, exit *int) JobOutcome {
	id := testServer
	return JobOutcome{Job: &domain.Job{
		ID: jobType + "-" + status, ServerID: &id, Type: jobType, Name: jobType + " @ web01",
		Status: status, Output: output, ExitCode: exit,
	}}
}

func failedJob(jobType, output string, exit int) JobOutcome {
	return jobOutcome(jobType, domain.JobStatusFailed, output+"\nFEHLER: x", &exit)
}

func successJob(jobType, output string) JobOutcome {
	zero := 0
	return jobOutcome(jobType, domain.JobStatusSuccess, output, &zero)
}

func storedCapability(t *testing.T, repo *repositories.CapabilityRepository, category string) *domain.ServerCapability {
	t.Helper()
	c, err := repo.Find(testServer, category)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func requireState(t *testing.T, repo *repositories.CapabilityRepository, category, state string) *domain.ServerCapability {
	t.Helper()
	c := storedCapability(t, repo, category)
	if c == nil {
		t.Fatalf("Kategorie %s hat keine Zeile", category)
	}
	if c.State != state {
		t.Fatalf("Kategorie %s: State = %q, erwartet %q (%+v)", category, c.State, state, c)
	}
	return c
}

const brokenDepsOutput = `The following packages have unmet dependencies:
 nginx : Depends: libfoo (>= 2) but it is not going to be installed
E: Unable to correct problems, you have held broken packages.`

const lockBusyOutput = `LCM: apt ist seit 10 Minuten belegt (PID 1234 (unattended-upgr), laeuft seit 10:00: x) - Lauf abgebrochen`

func TestHealthPingHeiltKeinFehlgeschlagenesUpdate(t *testing.T) {
	svc, repo, _ := newCapabilityTest(t)

	svc.RecordJob(failedJob(domain.RuleTypeUpdate, brokenDepsOutput, 100))
	failed := requireState(t, repo, domain.CapabilityPackages, domain.CapabilityFailed)
	if failed.LastErrorClass != domain.ErrClassBrokenDeps || failed.ConsecutiveFailures != 1 || failed.FailingSince == nil {
		t.Fatalf("Fehlschlag falsch festgehalten: %+v", failed)
	}
	if failed.LastJobType != domain.RuleTypeUpdate || failed.LastJobName == "" || failed.LastJobID == "" {
		t.Errorf("Job nicht festgehalten: %+v", failed)
	}

	svc.RecordJob(successJob(domain.RuleTypeHealth, "lcm-health-ok"))

	requireState(t, repo, domain.CapabilityPackages, domain.CapabilityFailed)
	requireState(t, repo, domain.CapabilityConnection, domain.CapabilityOK)
}

func TestPaketlisteHeiltKeinUpdateSetztAberLetztenErfolg(t *testing.T) {
	svc, repo, _ := newCapabilityTest(t)
	svc.RecordJob(failedJob(domain.RuleTypeUpdate, brokenDepsOutput, 100))

	svc.RecordJob(successJob(domain.RuleTypePackageScan, "Reading package lists... Done"))

	c := requireState(t, repo, domain.CapabilityPackages, domain.CapabilityFailed)
	if c.LastSuccessAt == nil {
		t.Error("LastSuccessAt fehlt - die Stillstandsprüfung misst den letzten Erfolg")
	}
}

func TestUpdateHeiltFehlgeschlagenesSecurity(t *testing.T) {
	svc, repo, log := newCapabilityTest(t)
	svc.RecordJob(failedJob(domain.RuleTypeSecurity, brokenDepsOutput, 100))

	svc.RecordJob(successJob(domain.RuleTypeUpdate, "done"))

	c := requireState(t, repo, domain.CapabilityPackages, domain.CapabilityOK)
	if c.FailingSince != nil || c.ConsecutiveFailures != 0 || c.LastErrorClass != "" || c.LastErrorMessage != "" {
		t.Errorf("Fehlerfelder nicht zurückgesetzt: %+v", c)
	}
	want := []transitionEvent{
		{testServer, domain.CapabilityPackages, false},
		{testServer, domain.CapabilityPackages, true},
	}
	assertEvents(t, log, want)
}

func TestWiederholterFehlschlagZaehltWeiterOhneFailingSinceZuAendern(t *testing.T) {
	svc, repo, _ := newCapabilityTest(t)
	svc.RecordJob(failedJob(domain.RuleTypeUpdate, brokenDepsOutput, 100))
	first := storedCapability(t, repo, domain.CapabilityPackages).FailingSince

	svc.RecordJob(failedJob(domain.RuleTypeUpdate, brokenDepsOutput, 100))

	c := requireState(t, repo, domain.CapabilityPackages, domain.CapabilityFailed)
	if c.ConsecutiveFailures != 2 {
		t.Errorf("ConsecutiveFailures = %d, erwartet 2", c.ConsecutiveFailures)
	}
	if !c.FailingSince.Equal(*first) {
		t.Errorf("FailingSince hat sich geändert: %v -> %v", first, c.FailingSince)
	}
}

func TestWatchdogAbbruchZaehltHandAbbruchNicht(t *testing.T) {
	svc, repo, _ := newCapabilityTest(t)

	manual := jobOutcome(domain.RuleTypeUpdate, domain.JobStatusAborted, "ABGEBROCHEN: Manuell", nil)
	svc.RecordJob(manual)
	if c := storedCapability(t, repo, domain.CapabilityPackages); c != nil {
		t.Fatalf("manueller Abbruch hat die Kategorie verändert: %+v", c)
	}

	watchdog := jobOutcome(domain.RuleTypeUpdate, domain.JobStatusAborted, "Setting up foo\nABGEBROCHEN: Ohne Lebenszeichen", nil)
	watchdog.ByWatchdog = true
	svc.RecordJob(watchdog)

	c := requireState(t, repo, domain.CapabilityPackages, domain.CapabilityFailed)
	if c.LastErrorClass != domain.ErrClassTimeout {
		t.Errorf("Klasse = %q, erwartet timeout", c.LastErrorClass)
	}
}

func TestBlockierteUndFremdeJobsZaehlenNicht(t *testing.T) {
	svc, repo, _ := newCapabilityTest(t)

	svc.RecordJob(jobOutcome(domain.RuleTypeUpdate, domain.JobStatusBlocked, "Blockiert", nil))
	svc.RecordJob(failedJob(domain.RuleTypeBackup, "boom", 1)) // Typ ohne Kategorie, keine Verbindung
	svc.RecordJob(JobOutcome{Job: &domain.Job{Type: domain.RuleTypeUpdate, Status: domain.JobStatusFailed}})
	svc.RecordJob(JobOutcome{})

	rows, err := repo.FindForServer(testServer)
	if err != nil || len(rows) != 0 {
		t.Fatalf("erwartet keine Zeilen, bekam %d (%v)", len(rows), err)
	}
}

func TestSperreAlarmiertErstAbDemZweitenFehlschlag(t *testing.T) {
	svc, repo, log := newCapabilityTest(t)

	svc.RecordJob(failedJob(domain.RuleTypeUpdate, lockBusyOutput, 75))
	c := requireState(t, repo, domain.CapabilityPackages, domain.CapabilityFailed)
	if c.LastErrorClass != domain.ErrClassLockBusy || c.LastErrorParams["process"] != "unattended-upgr" {
		t.Fatalf("Sperre nicht erkannt: %+v", c)
	}
	assertEvents(t, log, nil)

	svc.RecordJob(failedJob(domain.RuleTypeUpdate, lockBusyOutput, 75))
	assertEvents(t, log, []transitionEvent{{testServer, domain.CapabilityPackages, false}})

	// Ein dritter Fehlschlag meldet nichts mehr: Der Alarm steht schon.
	svc.RecordJob(failedJob(domain.RuleTypeUpdate, lockBusyOutput, 75))
	assertEvents(t, log, []transitionEvent{{testServer, domain.CapabilityPackages, false}})

	svc.RecordJob(successJob(domain.RuleTypeUpdate, "done"))
	assertEvents(t, log, []transitionEvent{
		{testServer, domain.CapabilityPackages, false},
		{testServer, domain.CapabilityPackages, true},
	})
}

// TestExit75NurBeiPaketJobsIstSperre: Ein Skript darf 75 aus eigenem Grund
// liefern (EX_TEMPFAIL); nur bei Paket-Jobs heißt es „apt ist belegt".
func TestExit75NurBeiPaketJobsIstSperre(t *testing.T) {
	svc, repo, _ := newCapabilityTest(t)

	svc.RecordJob(failedJob(domain.RuleTypeScript, "setup: Dienst noch nicht bereit", 75))
	c := requireState(t, repo, domain.CapabilityRules, domain.CapabilityFailed)
	if c.LastErrorClass != domain.ErrClassUnknown {
		t.Errorf("Skript mit Exit 75: Klasse = %q, erwartet unknown", c.LastErrorClass)
	}

	svc.RecordJob(failedJob(domain.RuleTypeUpdate, "", 75))
	c = requireState(t, repo, domain.CapabilityPackages, domain.CapabilityFailed)
	if c.LastErrorClass != domain.ErrClassLockBusy {
		t.Errorf("Paket-Job mit Exit 75: Klasse = %q, erwartet lock_busy", c.LastErrorClass)
	}
}

func TestHeilungEinerNieAlarmiertenSperreMeldetNichts(t *testing.T) {
	svc, _, log := newCapabilityTest(t)
	svc.RecordJob(failedJob(domain.RuleTypeUpdate, lockBusyOutput, 75))
	svc.RecordJob(successJob(domain.RuleTypeUpdate, "done"))
	assertEvents(t, log, nil)
}

func TestVerbindungsfehlerGiltFuerJedenJobTyp(t *testing.T) {
	svc, repo, log := newCapabilityTest(t)

	svc.RecordJob(jobOutcome(domain.RuleTypeUpdate, domain.JobStatusFailed,
		"FEHLER: verbindung: dial tcp 10.0.0.1:22: i/o timeout", nil))

	if c := storedCapability(t, repo, domain.CapabilityPackages); c != nil {
		t.Errorf("packages darf nicht betroffen sein: %+v", c)
	}
	c := requireState(t, repo, domain.CapabilityConnection, domain.CapabilityFailed)
	if c.LastErrorClass != domain.ErrClassUnknown || c.LastErrorMessage != "verbindung: dial tcp 10.0.0.1:22: i/o timeout" {
		t.Errorf("Verbindungsfehler falsch festgehalten: %+v", c)
	}
	assertEvents(t, log, []transitionEvent{{testServer, domain.CapabilityConnection, false}})

	// Auch ein Job ohne Kategorie meldet die Verbindung.
	svc.RecordJob(jobOutcome(JobTypeHardwareRefresh, domain.JobStatusFailed,
		"FEHLER: verbindung: dial tcp: lookup web01: Temporary failure resolving 'web01'", nil))
	c = requireState(t, repo, domain.CapabilityConnection, domain.CapabilityFailed)
	if c.LastErrorClass != domain.ErrClassNetwork || c.ConsecutiveFailures != 2 {
		t.Errorf("Netzfehler nicht erkannt: %+v", c)
	}

	// Ein beliebiger Erfolg heilt die Verbindung.
	svc.RecordJob(successJob(domain.RuleTypeHealth, "lcm-health-ok"))
	requireState(t, repo, domain.CapabilityConnection, domain.CapabilityOK)
}

// TestUebersprungenerErfolgZaehltNicht: Ein Job, der nichts ausgeführt hat
// (JobService.CompleteSkipped), heilt weder die Verbindung noch Pakete und
// schreibt keinen „letzten Erfolg" fort.
func TestUebersprungenerErfolgZaehltNicht(t *testing.T) {
	svc, repo, _ := newCapabilityTest(t)
	svc.RecordJob(jobOutcome(domain.RuleTypeUpdate, domain.JobStatusFailed,
		"FEHLER: verbindung: dial tcp 10.0.0.1:22: i/o timeout", nil))
	svc.RecordJob(failedJob(domain.RuleTypeSecurity, brokenDepsOutput, 100))

	for _, jobType := range []string{domain.RuleTypeHealth, domain.RuleTypeSecurity} {
		skipped := successJob(jobType, "im eingeschränkten Modus übersprungen")
		skipped.Skipped = true
		svc.RecordJob(skipped)
	}

	requireState(t, repo, domain.CapabilityConnection, domain.CapabilityFailed)
	c := requireState(t, repo, domain.CapabilityPackages, domain.CapabilityFailed)
	if c.LastSuccessAt != nil {
		t.Errorf("LastSuccessAt = %v, erwartet nil", c.LastSuccessAt)
	}
}

func TestVerbindungsMarkerNurAlsLetzteFehlerzeile(t *testing.T) {
	// Ein Skript, das den Marker selbst ausgibt, macht aus einem
	// Paketfehler keinen Verbindungsfehler.
	out := "FEHLER: verbindung: nur Ausgabe des Skripts\n" + brokenDepsOutput + "\nFEHLER: paket-update endete mit exit-code 100"
	if line, ok := connectionFailure(out); ok {
		t.Errorf("Marker mitten in der Ausgabe erkannt: %q", line)
	}
	if _, ok := connectionFailure("FEHLER: verbindung mit altem key: x"); ok {
		t.Error("verbindung mit altem key ist kein Verbindungsfehler")
	}
	if line, ok := connectionFailure("FEHLER: verbindung: a\nzweite Zeile"); !ok || line != "verbindung: a" {
		t.Errorf("Zeile = %q, %v", line, ok)
	}
}

func TestVerbindungsHeilungSchreibtNichtBeiJedemPing(t *testing.T) {
	svc, repo, _ := newCapabilityTest(t)
	svc.RecordJob(successJob(domain.RuleTypeHealth, "lcm-health-ok"))
	first := requireState(t, repo, domain.CapabilityConnection, domain.CapabilityOK)

	svc.RecordJob(successJob(domain.RuleTypeHealth, "lcm-health-ok"))
	second := storedCapability(t, repo, domain.CapabilityConnection)
	if !second.LastSuccessAt.Equal(*first.LastSuccessAt) {
		t.Errorf("zweiter Ping schrieb LastSuccessAt neu: %v -> %v", first.LastSuccessAt, second.LastSuccessAt)
	}

	// Ist der letzte Erfolg alt genug, wird er fortgeschrieben.
	old := time.Now().Add(-2 * connectionHealInterval)
	second.LastSuccessAt = &old
	if err := repo.Save(second); err != nil {
		t.Fatal(err)
	}
	svc.RecordJob(successJob(domain.RuleTypeHealth, "lcm-health-ok"))
	third := storedCapability(t, repo, domain.CapabilityConnection)
	if !third.LastSuccessAt.After(old.Add(connectionHealInterval)) {
		t.Errorf("alter Erfolg wurde nicht fortgeschrieben: %v", third.LastSuccessAt)
	}
}

func TestPaketlisteSetztQuellenWarnung(t *testing.T) {
	svc, repo, _ := newCapabilityTest(t)
	partial := `Hit:1 http://archive.ubuntu.com/ubuntu jammy InRelease
Hit:2 http://archive.ubuntu.com/ubuntu jammy-updates InRelease
Err:3 https://ppa.example.org/foo/ubuntu jammy InRelease
  404  Not Found [IP: 1.2.3.4 443]
Reading package lists...
E: The repository 'https://ppa.example.org/foo/ubuntu jammy InRelease' is not signed.`
	svc.RecordJob(successJob(domain.RuleTypePackageScan, partial))

	c := requireState(t, repo, domain.CapabilityPackages, domain.CapabilityOK)
	if c.WarningClass != domain.ErrClassRepoError || !strings.Contains(c.WarningMessage, "https://ppa.example.org/foo/ubuntu jammy") {
		t.Fatalf("Quellenwarnung fehlt: %q / %q", c.WarningClass, c.WarningMessage)
	}

	// Bloße apt-Warnung: anderer Hinweis.
	svc.RecordJob(successJob(domain.RuleTypePackageScan,
		"Hit:1 http://archive.ubuntu.com/ubuntu jammy InRelease\nW: Skipping acquire of configured file 'main/binary-i386/Packages'\n"))
	c = storedCapability(t, repo, domain.CapabilityPackages)
	if c.WarningClass != warningAptNotice || !strings.HasPrefix(c.WarningMessage, "W: Skipping") {
		t.Errorf("apt-Warnung falsch: %q / %q", c.WarningClass, c.WarningMessage)
	}

	// Anderer Paket-Job lässt die Warnung stehen ...
	svc.RecordJob(successJob(domain.RuleTypeUpdate, "done"))
	if c = storedCapability(t, repo, domain.CapabilityPackages); c.WarningClass != warningAptNotice {
		t.Errorf("Update hat die Warnung verändert: %q", c.WarningClass)
	}
	// ... ein sauberer Paketlisten-Lauf räumt sie ab.
	svc.RecordJob(successJob(domain.RuleTypePackageScan, "Hit:1 http://archive.ubuntu.com/ubuntu jammy InRelease\n"))
	if c = storedCapability(t, repo, domain.CapabilityPackages); c.WarningClass != "" || c.WarningMessage != "" {
		t.Errorf("Warnung nicht abgeräumt: %q / %q", c.WarningClass, c.WarningMessage)
	}
}

func TestZypperPaketlisteSetztQuellenWarnung(t *testing.T) {
	svc, repo, _ := newCapabilityTest(t)
	svc.RecordJob(successJob(domain.RuleTypePackageScan, zypperRefreshPartialOutput))

	c := requireState(t, repo, domain.CapabilityPackages, domain.CapabilityOK)
	if c.WarningClass != domain.ErrClassRepoError || c.WarningMessage != "kaputt" {
		t.Fatalf("Quellenwarnung fehlt: %q / %q", c.WarningClass, c.WarningMessage)
	}

	// Ein sauberer Lauf räumt sie ab.
	svc.RecordJob(successJob(domain.RuleTypePackageScan, "Repository 'repo-oss (16.0)' is up to date.\nAll repositories have been refreshed.\n"))
	if c = storedCapability(t, repo, domain.CapabilityPackages); c.WarningClass != "" || c.WarningMessage != "" {
		t.Errorf("Warnung nicht abgeräumt: %q / %q", c.WarningClass, c.WarningMessage)
	}
}

func TestRegelFehlschlagIstGelbUndAlarmwuerdig(t *testing.T) {
	svc, repo, log := newCapabilityTest(t)
	svc.RecordJob(failedJob(domain.RuleTypeScript, "boom", 3))

	c := requireState(t, repo, domain.CapabilityRules, domain.CapabilityFailed)
	if c.Effective() != domain.CapabilityWarning {
		t.Errorf("Effective = %q", c.Effective())
	}
	assertEvents(t, log, []transitionEvent{{testServer, domain.CapabilityRules, false}})
}

func TestRecordCVEScan(t *testing.T) {
	svc, repo, log := newCapabilityTest(t)

	svc.RecordCVEScan(testServer, errCVEScannerUnavailable)
	c := requireState(t, repo, domain.CapabilityCVEScan, domain.CapabilityFailed)
	if c.LastErrorClass != domain.ErrClassScannerUnavailable || c.LastJobType != domain.RuleTypeCVEScan {
		t.Errorf("Scanner-Fehler falsch festgehalten: %+v", c)
	}

	svc.RecordCVEScan(testServer, errors.New("trivy: boom"))
	c = requireState(t, repo, domain.CapabilityCVEScan, domain.CapabilityFailed)
	if c.ConsecutiveFailures != 2 || c.LastErrorClass != domain.ErrClassUnknown {
		t.Errorf("zweiter Fehlschlag: %+v", c)
	}

	svc.RecordCVEScan(testServer, nil)
	c = requireState(t, repo, domain.CapabilityCVEScan, domain.CapabilityOK)
	if c.LastSuccessAt == nil {
		t.Error("LastSuccessAt fehlt")
	}
	assertEvents(t, log, []transitionEvent{
		{testServer, domain.CapabilityCVEScan, false},
		{testServer, domain.CapabilityCVEScan, true},
	})
}

// TestFehlenderScannerAlarmiertNichtJeServer: Der Zustand bleibt je Server
// stehen (gelbe Ampel, Plan), aber eine action_failed-Regel bekäme sonst eine
// Meldung je Server für ein Problem des LCM-Hosts.
func TestFehlenderScannerAlarmiertNichtJeServer(t *testing.T) {
	svc, repo, log := newCapabilityTest(t)

	svc.RecordCVEScan(testServer, errCVEScannerUnavailable)
	c := requireState(t, repo, domain.CapabilityCVEScan, domain.CapabilityFailed)
	if c.Effective() != domain.CapabilityWarning {
		t.Errorf("Effective = %q, erwartet warning", c.Effective())
	}
	if alertable(c) {
		t.Error("scanner_unavailable darf nicht alarmwürdig sein")
	}

	svc.RecordCVEScan(testServer, nil)
	requireState(t, repo, domain.CapabilityCVEScan, domain.CapabilityOK)
	assertEvents(t, log, nil)
}

func ruleJob(jobType, status string, ruleID uint, name string) JobOutcome {
	o := jobOutcome(jobType, status, "", nil)
	o.Job.RuleID, o.Job.Name = &ruleID, name
	if status == domain.JobStatusFailed {
		o.Job.Output = "boom\nFEHLER: x"
		exit := 3
		o.Job.ExitCode = &exit
	}
	return o
}

// TestAndereRegelGleichenTypsHeiltNicht: Zwei Skript-Regeln tun Verschiedenes;
// der Erfolg der einen darf den Fehler der anderen nicht verdecken.
func TestAndereRegelGleichenTypsHeiltNicht(t *testing.T) {
	svc, repo, log := newCapabilityTest(t)
	svc.RecordJob(ruleJob(domain.RuleTypeScript, domain.JobStatusFailed, 1, "Regel A"))
	c := requireState(t, repo, domain.CapabilityRules, domain.CapabilityFailed)
	if c.LastRuleID == nil || *c.LastRuleID != 1 {
		t.Fatalf("LastRuleID = %v, erwartet 1", c.LastRuleID)
	}

	svc.RecordJob(ruleJob(domain.RuleTypeScript, domain.JobStatusSuccess, 2, "Regel B"))
	c = requireState(t, repo, domain.CapabilityRules, domain.CapabilityFailed)
	if c.LastSuccessAt == nil {
		t.Error("LastSuccessAt fehlt - der Erfolg von Regel B zählt trotzdem")
	}

	svc.RecordJob(ruleJob(domain.RuleTypeScript, domain.JobStatusSuccess, 1, "Regel A"))
	requireState(t, repo, domain.CapabilityRules, domain.CapabilityOK)
	assertEvents(t, log, []transitionEvent{
		{testServer, domain.CapabilityRules, false},
		{testServer, domain.CapabilityRules, true},
	})
}

// TestAdHocSkriptHeiltAnderesAdHocSkriptNicht: Ohne Regel unterscheidet nur
// der Jobname - das CrowdSec-Einrichtungsskript bleibt rot, bis es selbst läuft.
func TestAdHocSkriptHeiltAnderesAdHocSkriptNicht(t *testing.T) {
	svc, repo, _ := newCapabilityTest(t)
	failedSetup := failedJob(domain.RuleTypeScript, "boom", 1)
	failedSetup.Job.Name = "CrowdSec einrichten @ web01"
	svc.RecordJob(failedSetup)

	other := successJob(domain.RuleTypeScript, "ok")
	other.Job.Name = "Logs rotieren @ web01"
	svc.RecordJob(other)
	requireState(t, repo, domain.CapabilityRules, domain.CapabilityFailed)

	retry := successJob(domain.RuleTypeScript, "ok")
	retry.Job.Name = "CrowdSec einrichten @ web01"
	svc.RecordJob(retry)
	requireState(t, repo, domain.CapabilityRules, domain.CapabilityOK)
}

func TestEinzelnerCVEScanOhneScannerIstFehlschlag(t *testing.T) {
	svc, repo, _ := newCapabilityTest(t)
	svc.RecordJob(jobOutcome(domain.RuleTypeCVEScan, domain.JobStatusFailed,
		"FEHLER: "+errCVEScannerUnavailable.Error(), nil))
	c := requireState(t, repo, domain.CapabilityCVEScan, domain.CapabilityFailed)
	if c.LastErrorClass != domain.ErrClassScannerUnavailable {
		t.Errorf("Klasse = %q", c.LastErrorClass)
	}

	// Ein nicht erreichbarer Server ist eine bewusste Auslassung.
	svc2, repo2, _ := newCapabilityTest(t)
	svc2.RecordJob(jobOutcome(domain.RuleTypeCVEScan, domain.JobStatusFailed,
		"FEHLER: "+ErrCVEScanUnreachable.Error()+" - übersprungen", nil))
	if c := storedCapability(t, repo2, domain.CapabilityCVEScan); c != nil {
		t.Errorf("nicht erreichbarer Server darf cve_scan nicht färben: %+v", c)
	}
}

func assertEvents(t *testing.T, log *transitionLog, want []transitionEvent) {
	t.Helper()
	got := log.all()
	if len(got) != len(want) {
		t.Fatalf("Wechsel = %+v, erwartet %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Wechsel %d = %+v, erwartet %+v", i, got[i], want[i])
		}
	}
}

// Ein Health-Ping, der den Server erreicht, aber am Regel-Abgleich scheitert,
// ist ein Regel-Fehler: Die Verbindung steht. Heilen kann ihn nur ein späterer
// Ping mit sauberem Abgleich, kein beliebiger anderer Regel-Erfolg.
func TestHealthPingMitRegelFehlerZaehltAlsRegel(t *testing.T) {
	svc, repo, _ := newCapabilityTest(t)

	svc.RecordJob(failedJob(domain.RuleTypeHealth, "firewall: ufw nicht gefunden", 0))
	requireState(t, repo, domain.CapabilityRules, domain.CapabilityFailed)
	if c := storedCapability(t, repo, domain.CapabilityConnection); c != nil && c.State == domain.CapabilityFailed {
		t.Fatalf("Verbindung fälschlich als gescheitert markiert: %+v", c)
	}

	svc.RecordJob(successJob(domain.RuleTypeScript, "ok"))
	requireState(t, repo, domain.CapabilityRules, domain.CapabilityFailed)

	svc.RecordJob(successJob(domain.RuleTypeHealth, "lcm-health-ok"))
	requireState(t, repo, domain.CapabilityRules, domain.CapabilityOK)
}

// Derselbe Typ heilt nicht über Quellen hinweg: Der Erfolg eines anderen
// Health-Pings lässt den Regel-Fehler stehen, nur die eigene Regel heilt ihn.
func TestAnderesHealthPingHeiltRegelFehlerNicht(t *testing.T) {
	svc, repo, _ := newCapabilityTest(t)

	svc.RecordJob(ruleJob(domain.RuleTypeHealth, domain.JobStatusFailed, 1, "Health A"))
	requireState(t, repo, domain.CapabilityRules, domain.CapabilityFailed)

	svc.RecordJob(ruleJob(domain.RuleTypeHealth, domain.JobStatusSuccess, 2, "Health B"))
	requireState(t, repo, domain.CapabilityRules, domain.CapabilityFailed)

	svc.RecordJob(ruleJob(domain.RuleTypeHealth, domain.JobStatusSuccess, 1, "Health A"))
	requireState(t, repo, domain.CapabilityRules, domain.CapabilityOK)
}

func TestVomWatchdogBeendeterPingHeiltBeimNaechstenErfolg(t *testing.T) {
	svc, repo, _ := newCapabilityTest(t)
	hung := jobOutcome(domain.RuleTypeHealth, domain.JobStatusAborted, "ABGEBROCHEN: Stille", nil)
	hung.ByWatchdog = true
	svc.RecordJob(hung)

	c := requireState(t, repo, domain.CapabilityConnection, domain.CapabilityFailed)
	if c.LastErrorClass != domain.ErrClassTimeout {
		t.Errorf("Klasse = %q, erwartet timeout", c.LastErrorClass)
	}
	if rules := storedCapability(t, repo, domain.CapabilityRules); rules != nil && rules.State == domain.CapabilityFailed {
		t.Errorf("ein hängender Ping ist kein Regel-Fehler: %+v", rules)
	}

	// Ein Ping aus anderer Quelle, etwa von Hand gestartet, genügt.
	manual := successJob(domain.RuleTypeHealth, "ok")
	manual.Job.Name = "Health-Check von Hand"
	svc.RecordJob(manual)
	requireState(t, repo, domain.CapabilityConnection, domain.CapabilityOK)
}

func TestScanOhneOffeneUpdatesHeiltGescheiterteInstallation(t *testing.T) {
	for _, jobType := range []string{domain.RuleTypeUpdate, domain.RuleTypeSecurity, domain.RuleTypePackages} {
		for _, scanType := range []string{domain.RuleTypePackageScan, JobTypeFullRefresh} {
			svc, repo, _ := newCapabilityTest(t)
			svc.WithNothingPending(func(uint) (bool, error) { return true, nil })
			svc.RecordJob(failedJob(jobType, brokenDepsOutput, 100))

			svc.RecordJob(successJob(scanType, "Reading package lists... Done"))

			requireState(t, repo, domain.CapabilityPackages, domain.CapabilityOK)
		}
	}
}

func TestScanMitOffenenUpdatesHeiltNicht(t *testing.T) {
	svc, repo, _ := newCapabilityTest(t)
	svc.WithNothingPending(func(uint) (bool, error) { return false, nil })
	svc.RecordJob(failedJob(domain.RuleTypeUpdate, brokenDepsOutput, 100))

	svc.RecordJob(successJob(domain.RuleTypePackageScan, "Reading package lists... Done"))

	requireState(t, repo, domain.CapabilityPackages, domain.CapabilityFailed)
}

func TestScanHeiltKeineGescheiterteBereinigung(t *testing.T) {
	svc, repo, _ := newCapabilityTest(t)
	svc.WithNothingPending(func(uint) (bool, error) { return true, nil })
	svc.RecordJob(failedJob(domain.RuleTypeAutoremove, brokenDepsOutput, 100))

	svc.RecordJob(successJob(domain.RuleTypePackageScan, "Reading package lists... Done"))

	requireState(t, repo, domain.CapabilityPackages, domain.CapabilityFailed)
}

func TestScanMitAusgefallenerQuelleHeiltNicht(t *testing.T) {
	svc, repo, _ := newCapabilityTest(t)
	svc.WithNothingPending(func(uint) (bool, error) { return true, nil })
	svc.RecordJob(failedJob(domain.RuleTypeUpdate, brokenDepsOutput, 100))

	svc.RecordJob(successJob(domain.RuleTypePackageScan, zypperRefreshPartialOutput))

	requireState(t, repo, domain.CapabilityPackages, domain.CapabilityFailed)
}

// TestAktualisierenHeiltGescheitertePaketliste: Scheitert der tägliche Lauf
// „Paketliste aktualisieren“ einmal (Netz weg), heilt ihn ein erfolgreiches
// „Alles aktualisieren“ - es liest die Pakete ebenso. Eine gescheiterte
// Installation und eine ausgefallene Quelle bleiben stehen.
func TestAktualisierenHeiltGescheitertePaketliste(t *testing.T) {
	svc, repo, _ := newCapabilityTest(t)
	svc.RecordJob(failedJob(domain.RuleTypePackageScan, "FEHLER: DSM-Anmeldung fehlgeschlagen: context deadline exceeded", 1))
	requireState(t, repo, domain.CapabilityPackages, domain.CapabilityFailed)

	svc.RecordJob(successJob(JobTypeFullRefresh, "Pakete: 12 installiert"))
	requireState(t, repo, domain.CapabilityPackages, domain.CapabilityOK)

	svc, repo, _ = newCapabilityTest(t)
	svc.RecordJob(failedJob(domain.RuleTypeUpdate, brokenDepsOutput, 100))
	svc.RecordJob(successJob(JobTypeFullRefresh, "Reading package lists... Done"))
	requireState(t, repo, domain.CapabilityPackages, domain.CapabilityFailed)

	svc, repo, _ = newCapabilityTest(t)
	svc.RecordJob(failedJob(domain.RuleTypePackageScan, "x", 1))
	svc.RecordJob(successJob(JobTypeFullRefresh, zypperRefreshPartialOutput))
	requireState(t, repo, domain.CapabilityPackages, domain.CapabilityFailed)
}
