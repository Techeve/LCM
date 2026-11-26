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

// rolloutFixture ist ein AgentUpdateService mit gefälschten Servern und Jobs.
// Die Regeln liegen in einer echten (In-Memory-)Tabelle.
type rolloutFixture struct {
	svc      *AgentUpdateService
	policies *repositories.AgentUpdateRepository
	servers  map[uint]*domain.Server

	mu       sync.Mutex
	started  []startedUpdate
	jobOK    map[uint]bool   // Ergebnis des Update-Jobs je Server (Vorgabe: Erfolg)
	reaches  map[uint]string // Version, die der Server nach dem Update meldet
	rollback map[uint]bool   // der Server meldet danach einen Rollback
	busy     map[uint]bool   // der Start scheitert wie bei belegtem Server
}

type startedUpdate struct {
	server  uint
	ceiling string
}

func newRolloutFixture(t *testing.T, names ...string) *rolloutFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.AgentUpdatePolicy{}); err != nil {
		t.Fatal(err)
	}
	f := &rolloutFixture{
		policies: repositories.NewAgentUpdateRepository(db),
		servers:  map[uint]*domain.Server{},
		jobOK:    map[uint]bool{}, reaches: map[uint]string{}, rollback: map[uint]bool{}, busy: map[uint]bool{},
	}
	for i, name := range names {
		id := uint(i + 1)
		f.servers[id] = &domain.Server{ID: id, Name: name, Transport: domain.TransportAgent, AgentVersion: "2.2.0", AgentID: name}
	}
	f.svc = &AgentUpdateService{
		policies: f.policies,
		members: func(uint) ([]domain.Server, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			out := make([]domain.Server, 0, len(f.servers))
			for id := uint(1); id <= uint(len(f.servers)); id++ {
				out = append(out, *f.servers[id])
			}
			return out, nil
		},
		reload: func(id uint) (*domain.Server, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			cp := *f.servers[id]
			return &cp, nil
		},
		connected: func(*domain.Server) bool { return true },
		start: func(id uint, _, ceiling string) (string, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.busy[id] {
				return "", ErrServerBusy
			}
			f.started = append(f.started, startedUpdate{server: id, ceiling: ceiling})
			return "job-" + f.servers[id].Name, nil
		},
		wait: func(job string) bool {
			f.mu.Lock()
			defer f.mu.Unlock()
			for id, s := range f.servers {
				if "job-"+s.Name != job {
					continue
				}
				ok := true
				if v, set := f.jobOK[id]; set {
					ok = v
				}
				if ok {
					s.AgentVersion = "2.3.0"
					if v := f.reaches[id]; v != "" {
						s.AgentVersion = v
					}
				}
				if f.rollback[id] {
					s.AgentRollback = "2.3.0"
				}
				return ok
			}
			return false
		},
		serverVersion: func() string { return "2.3.0" },
		sleep:         func(time.Duration) {},
		now:           time.Now,
		running:       map[uint]bool{},
	}
	return f
}

func (f *rolloutFixture) startedIDs() []uint {
	f.mu.Lock()
	defer f.mu.Unlock()
	ids := make([]uint, len(f.started))
	for i, s := range f.started {
		ids[i] = s.server
	}
	return ids
}

func (f *rolloutFixture) policy(t *testing.T, staged bool) {
	t.Helper()
	if _, err := f.svc.Save(1, AgentUpdateInput{Mode: domain.AgentUpdateAuto, Staged: staged, CronExpr: "0 3 * * *"}, "test"); err != nil {
		t.Fatal(err)
	}
}

// TestGestaffeltErstEinServerDannDerRest: Der erste Server (nach Namen) läuft
// allein; erst nach seinem Erfolg folgt der Rest - gedeckelt auf die Version,
// die der erste erreicht hat.
func TestGestaffeltErstEinServerDannDerRest(t *testing.T) {
	f := newRolloutFixture(t, "beta", "alpha", "gamma")
	f.policy(t, true)
	f.reaches[2] = "2.2.1" // alpha (der erste nach Namen) landet auf 2.2.1

	res := f.svc.rollout(1, "test", true)

	ids := f.startedIDs()
	if len(ids) != 3 || ids[0] != 2 {
		t.Fatalf("reihenfolge der starts = %v, erwartet alpha (2) zuerst", ids)
	}
	if res.Canary != "alpha" || res.Updated != 3 || res.Failed != 0 {
		t.Errorf("ergebnis = %+v", res)
	}
	// Der Rest wird auf die Version des ersten begrenzt, nicht auf die von LCM.
	for _, s := range f.started[1:] {
		if s.ceiling != "2.2.1" {
			t.Errorf("server %d: ceiling %q, erwartet 2.2.1", s.server, s.ceiling)
		}
	}
	if f.started[0].ceiling != "" {
		t.Errorf("der erste Server läuft ohne feste Grenze (nur die Reihe von LCM), nicht %q", f.started[0].ceiling)
	}
	p, _ := f.policies.Get(1)
	if p.ProvenFor != "2.3.0" || p.ProvenVersion != "2.2.1" || p.LastRunAt == nil || p.LastResult == "" {
		t.Errorf("stand nicht festgehalten: %+v", p)
	}
}

// TestGestaffeltErsterServerScheitertHaeltDieRegelAn: Scheitert der erste, startet
// der Rest nicht, und die Regel läuft für diese LCM-Version nicht noch einmal.
func TestGestaffeltErsterServerScheitertHaeltDieRegelAn(t *testing.T) {
	f := newRolloutFixture(t, "alpha", "beta", "gamma")
	f.policy(t, true)
	f.jobOK[1] = false

	res := f.svc.rollout(1, "test", true)
	if !res.Blocked || res.Failed != 1 {
		t.Errorf("ergebnis = %+v", res)
	}
	if ids := f.startedIDs(); len(ids) != 1 {
		t.Fatalf("der Rest startete trotz gescheitertem ersten Server: %v", ids)
	}
	p, _ := f.policies.Get(1)
	if p.BlockedFor != "2.3.0" {
		t.Errorf("BlockedFor = %q", p.BlockedFor)
	}

	// Nächster Lauf: nichts passiert.
	f.jobOK[1] = true
	again := f.svc.rollout(1, "test", true)
	if len(f.startedIDs()) != 1 || !again.Blocked {
		t.Errorf("gesperrte regel lief doch: starts %v, %+v", f.startedIDs(), again)
	}

	// Fortsetzen hebt die Sperre auf.
	if _, err := f.svc.Resume(1, "test"); err != nil {
		t.Fatal(err)
	}
	f.svc.rollout(1, "test", true)
	if len(f.startedIDs()) <= 1 {
		t.Error("nach „Fortsetzen“ lief die regel nicht")
	}
}

// TestGestaffeltEineNeueLCMVersionHebtDieSperreAuf: BlockedFor gilt nur für die
// Version, bei der es gescheitert ist.
func TestGestaffeltEineNeueLCMVersionHebtDieSperreAuf(t *testing.T) {
	f := newRolloutFixture(t, "alpha")
	f.policy(t, true)
	f.jobOK[1] = false
	f.svc.rollout(1, "test", true)

	f.svc.serverVersion = func() string { return "2.4.0" }
	f.jobOK[1] = true
	res := f.svc.rollout(1, "test", true)
	if res.Blocked || res.Updated != 1 {
		t.Errorf("mit neuer LCM-Version: %+v", res)
	}
}

// TestGestaffeltNeuerBugfixUnterGleicherLCMVersion: Alle Agenten stehen auf der
// Version von LCM, die Quelle bietet einen Bugfix. Der erste Server holt ihn
// zuerst, der Rest folgt bis genau dahin - ohne dass LCM sich ändert.
func TestGestaffeltNeuerBugfixUnterGleicherLCMVersion(t *testing.T) {
	f := newRolloutFixture(t, "alpha", "beta")
	f.policy(t, true)
	for _, s := range f.servers {
		s.AgentVersion = "2.3.0"
	}
	p, _ := f.policies.Get(1)
	p.ProvenFor, p.ProvenVersion = "2.3.0", "2.3.0"
	if err := f.policies.RecordRun(p); err != nil {
		t.Fatal(err)
	}
	f.reaches[1], f.reaches[2] = "2.3.2", "2.3.2"

	res := f.svc.rollout(1, "test", true)
	if res.Canary != "alpha" || res.Updated != 2 {
		t.Fatalf("ergebnis = %+v", res)
	}
	if f.started[1].ceiling != "2.3.2" {
		t.Errorf("der Rest läuft bis %q, erwartet 2.3.2", f.started[1].ceiling)
	}
	p, _ = f.policies.Get(1)
	if p.ProvenVersion != "2.3.2" {
		t.Errorf("ProvenVersion = %q", p.ProvenVersion)
	}
}

// TestNichtsNeuesKostetNurEinenJob: Bleibt der erste Server, wo er ist, kommt
// keiner nach, der schon so hoch steht.
func TestNichtsNeuesKostetNurEinenJob(t *testing.T) {
	f := newRolloutFixture(t, "alpha", "beta", "gamma")
	f.policy(t, true)
	for _, s := range f.servers {
		s.AgentVersion = "2.3.0"
	}
	res := f.svc.rollout(1, "test", true)
	if ids := f.startedIDs(); len(ids) != 1 {
		t.Fatalf("starts = %v, erwartet nur den ersten Server", ids)
	}
	if res.Current != 3 || res.Updated != 0 || res.Failed != 0 {
		t.Errorf("ergebnis = %+v (alle drei schon aktuell)", res)
	}
}

// TestGestaffeltNeueLCMVersionBrauchtNeuenErstenServer: Der geprüfte Stand gilt
// nur für die LCM-Version, mit der er entstand.
func TestGestaffeltNeueLCMVersionBrauchtNeuenErstenServer(t *testing.T) {
	f := newRolloutFixture(t, "alpha", "beta")
	f.policy(t, true)
	p, _ := f.policies.Get(1)
	p.ProvenFor, p.ProvenVersion = "2.2.0", "2.2.0"
	_ = f.policies.RecordRun(p)

	res := f.svc.rollout(1, "test", true)
	if res.Canary != "alpha" {
		t.Errorf("erster Server = %q, erwartet alpha", res.Canary)
	}
}

// TestNichtGestaffeltOhneGrenze: Ohne Staffelung gibt es keinen geprüften
// ersten Server und keine feste Grenze; der erste geht nur voraus, um zu sehen,
// ob es etwas Neues gibt.
func TestNichtGestaffeltOhneGrenze(t *testing.T) {
	f := newRolloutFixture(t, "alpha", "beta", "gamma")
	f.policy(t, false)
	res := f.svc.rollout(1, "test", true)
	if res.Canary != "" || res.Updated != 3 {
		t.Errorf("ergebnis = %+v", res)
	}
	for _, s := range f.started {
		if s.ceiling != "" {
			t.Errorf("grenze %q", s.ceiling)
		}
	}
}

// TestNichtGestaffeltErsterScheitertRestLaeuft: Ohne Staffelung hält ein
// gescheiterter erster Server die übrigen nicht auf (nur ein Rollback tut das).
func TestNichtGestaffeltErsterScheitertRestLaeuft(t *testing.T) {
	f := newRolloutFixture(t, "alpha", "beta")
	f.policy(t, false)
	f.jobOK[1] = false
	res := f.svc.rollout(1, "test", true)
	if res.Blocked || res.Failed != 1 || res.Updated != 1 {
		t.Errorf("ergebnis = %+v", res)
	}
}

// TestRollbackImRestHaeltDieUebrigenAn: Meldet ein Server nach dem Update einen
// Rollback, beginnt keiner mehr; die Regel hält an.
func TestRollbackImRestHaeltDieUebrigenAn(t *testing.T) {
	f := newRolloutFixture(t, "a", "b", "c", "d")
	f.policy(t, true)
	if _, err := f.svc.Save(1, AgentUpdateInput{Mode: domain.AgentUpdateAuto, Staged: true, CronExpr: "0 3 * * *", WindowMinutes: 60}, "test"); err != nil {
		t.Fatal(err)
	}
	// Das Fenster verteilt b, c, d; der Test-Schlaf gibt dem ersten Start Zeit zu enden.
	f.svc.sleep = func(time.Duration) { time.Sleep(30 * time.Millisecond) }
	f.jobOK[2] = false
	f.rollback[2] = true

	res := f.svc.rollout(1, "test", true)
	if !res.Blocked {
		t.Errorf("kein Halt nach Rollback: %+v", res)
	}
	// a (der erste Server) und b (Rollback) liefen; c und d kamen danach nicht mehr dran.
	if ids := f.startedIDs(); len(ids) != 2 || ids[0] != 1 || ids[1] != 2 {
		t.Errorf("starts = %v, erwartet [1 2]", ids)
	}
	p, _ := f.policies.Get(1)
	if p.BlockedFor != "2.3.0" {
		t.Errorf("BlockedFor = %q", p.BlockedFor)
	}
}

// TestKandidaten: Wer gilt als zu aktualisieren.
func TestKandidaten(t *testing.T) {
	servers := []domain.Server{
		{ID: 1, Name: "zeta", Transport: domain.TransportAgent, AgentVersion: "2.2.0"},
		{ID: 2, Name: "alt-aber-aus", Transport: domain.TransportAgent, AgentVersion: "2.2.0"},
		{ID: 3, Name: "aktuell", Transport: domain.TransportAgent, AgentVersion: "2.3.0"},
		{ID: 4, Name: "neuer", Transport: domain.TransportAgent, AgentVersion: "2.5.0"},
		{ID: 5, Name: "ssh", Transport: domain.TransportSSH, AgentVersion: "2.0.0"},
		{ID: 6, Name: "selbstbau", Transport: domain.TransportAgent, AgentVersion: "0.0.0-dev"},
		{ID: 7, Name: "zurueckgerollt", Transport: domain.TransportAgent, AgentVersion: "2.2.0", AgentRollback: "2.3.0"},
		{ID: 8, Name: "beta", Transport: domain.TransportAgent, AgentVersion: "2.3.0~beta.2"},
		{ID: 9, Name: "demo", Transport: domain.TransportAgent, AgentVersion: "2.0.0", IsDemo: true},
		{ID: 10, Name: "ohne-meldung", Transport: domain.TransportAgent},
		{ID: 11, Name: "mit-build", Transport: domain.TransportAgent, AgentVersion: "2.2.0 (Build 412, a1b2c3d4)"},
	}
	connected := func(s *domain.Server) bool { return s.ID != 2 }
	cands, skipped := rolloutCandidates(servers, "2.3.0", connected)
	var names []string
	for _, c := range cands {
		names = append(names, c.Name)
	}
	// Auch „aktuell“ zählt: Es kann einen Bugfix in der Reihe 2.3 geben.
	// So meldet sich ein echter Agent (version.String()): zählt ebenso.
	want := []string{"aktuell", "beta", "mit-build", "zeta"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("kandidaten = %v, erwartet %v", names, want)
	}
	if skipped != 2 { // getrennt (2) und zurückgerollt (7)
		t.Errorf("übersprungen = %d, erwartet 2", skipped)
	}
}

// TestBelegterServerWirdUebersprungenNichtGezaehltAlsFehler: Ist der Server
// belegt, holt der nächste Lauf ihn nach.
func TestBelegterServerWirdUebersprungenNichtGezaehltAlsFehler(t *testing.T) {
	f := newRolloutFixture(t, "alpha", "beta")
	f.policy(t, false)
	f.busy[2] = true
	res := f.svc.rollout(1, "test", true)
	if res.Updated != 1 || res.Skipped != 1 || res.Failed != 0 {
		t.Errorf("ergebnis = %+v", res)
	}
}

// TestRolloutBeiAusgeschalteterRegelLaeuftNurVonHand.
func TestRolloutBeiAusgeschalteterRegelLaeuftNurVonHand(t *testing.T) {
	f := newRolloutFixture(t, "alpha")
	if res := f.svc.rollout(1, "scheduler", true); len(f.startedIDs()) != 0 || res.Note == "" {
		t.Errorf("ausgeschaltete regel lief: %+v", res)
	}
	f.svc.rollout(1, "tester", false)
	if len(f.startedIDs()) != 1 {
		t.Error("lauf von Hand startete nichts")
	}
}

// TestEinLaufJeGruppe.
func TestEinLaufJeGruppe(t *testing.T) {
	f := newRolloutFixture(t, "alpha")
	if !f.svc.claim(1) {
		t.Fatal("erster claim scheiterte")
	}
	if err := f.svc.RunNow(1, "test"); !errors.Is(err, ErrRolloutRunning) {
		t.Errorf("zweiter Lauf: %v", err)
	}
	f.svc.release(1)
}

func TestInWindow(t *testing.T) {
	at := func(h, m int) time.Time { return time.Date(2026, 11, 24, h, m, 0, 0, time.UTC) }
	tests := []struct {
		name    string
		minutes int
		now     time.Time
		want    bool
	}{
		{"mitten im Fenster", 120, at(4, 0), true},
		{"genau zu Beginn", 120, at(3, 0), true},
		{"am Ende des Fensters", 120, at(5, 0), true},
		{"nach dem Fenster", 120, at(5, 1), false},
		{"vor dem Fenster", 120, at(2, 59), false},
		{"ohne Fenster: Karenz von 30 Minuten", 0, at(3, 20), true},
		{"ohne Fenster nach der Karenz", 0, at(3, 31), false},
	}
	for _, tt := range tests {
		if got := inWindow("0 3 * * *", tt.minutes, tt.now); got != tt.want {
			t.Errorf("%s: inWindow = %t, want %t", tt.name, got, tt.want)
		}
	}
	if inWindow("kaputt", 60, at(3, 0)) {
		t.Error("ungültiger Ausdruck gilt als Fenster")
	}
}

func TestRegelEingabePruefen(t *testing.T) {
	f := newRolloutFixture(t)
	bad := []struct {
		name string
		in   AgentUpdateInput
		err  error
	}{
		{"Modus", AgentUpdateInput{Mode: "manchmal"}, ErrAgentUpdateMode},
		{"Cron", AgentUpdateInput{Mode: "auto", CronExpr: "nie"}, ErrAgentUpdateCron},
		{"Fenster negativ", AgentUpdateInput{Mode: "auto", WindowMinutes: -1}, ErrAgentUpdateWindow},
		{"Fenster zu lang", AgentUpdateInput{Mode: "auto", WindowMinutes: 1441}, ErrAgentUpdateWindow},
	}
	for _, tt := range bad {
		if _, err := f.svc.Save(1, tt.in, "test"); !errors.Is(err, tt.err) {
			t.Errorf("%s: %v, erwartet %v", tt.name, err, tt.err)
		}
	}
	view, err := f.svc.Save(1, AgentUpdateInput{Mode: "auto", Staged: false}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if view.CronExpr != domain.DefaultAgentUpdateCron || view.Staged || view.NextRun == "" {
		t.Errorf("gespeichert: %+v", view)
	}
	view, err = f.svc.Save(1, AgentUpdateInput{Mode: "off", Staged: true}, "test")
	if err != nil || view.NextRun != "" || view.Mode != "off" {
		t.Errorf("ausgeschaltet: %+v, %v", view, err)
	}
}

// TestRegelOhneGespeicherteZeileIstAus: Vorgabe jeder Gruppe.
func TestRegelOhneGespeicherteZeileIstAus(t *testing.T) {
	f := newRolloutFixture(t, "alpha")
	view, err := f.svc.Get(7)
	if err != nil {
		t.Fatal(err)
	}
	if view.Mode != domain.AgentUpdateOff || !view.Staged || view.Enabled() {
		t.Errorf("vorgabe = %+v", view)
	}
	enabled, _ := f.policies.Enabled()
	if len(enabled) != 0 {
		t.Errorf("eingeschaltet: %v", enabled)
	}
}

// TestOnLCMUpdatedStartetNurImFenster.
func TestOnLCMUpdatedStartetNurImFenster(t *testing.T) {
	f := newRolloutFixture(t, "alpha")
	f.policy(t, true)
	var mu sync.Mutex
	slept := 0
	done := make(chan struct{}, 1)
	f.svc.sleep = func(time.Duration) { mu.Lock(); slept++; mu.Unlock() }
	f.svc.start = func(id uint, _, _ string) (string, error) {
		select {
		case done <- struct{}{}:
		default:
		}
		return "job-alpha", nil
	}
	f.svc.now = func() time.Time { return time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC) } // außerhalb des Fensters
	f.svc.OnLCMUpdated()
	select {
	case <-done:
		t.Fatal("Lauf startete außerhalb des Fensters")
	case <-time.After(200 * time.Millisecond):
	}
	f.svc.now = func() time.Time { return time.Date(2026, 11, 24, 3, 10, 0, 0, time.UTC) }
	f.svc.OnLCMUpdated()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Lauf startete im Fenster nicht")
	}
}
