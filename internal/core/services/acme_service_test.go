package services_test

import (
	"errors"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/infrastructure/acmex"
	"LCM/internal/infrastructure/crypto"
	"LCM/internal/storage/repositories"
)

// fakeListener spielt den Listener für HTTP-01.
type fakeListener struct {
	mu      sync.Mutex
	started []string
	running bool
	err     error
	// panicMsg lässt Stop einen Panic auslösen (ein Fehler im Lauf).
	panicMsg string
	stops    int
}

// starts zählt die Aufrufe von Start.
func (f *fakeListener) starts() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.started)
}

func (f *fakeListener) Start(addr string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.started = append(f.started, addr)
	f.running = true
	return nil
}

func (f *fakeListener) Stop() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stops++
	f.running = false
	if f.panicMsg != "" {
		panic(f.panicMsg)
	}
}

// stopped zählt die Aufrufe von Stop.
func (f *fakeListener) stopped() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stops
}

func (f *fakeListener) Running() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.running
}

type acmeFixture struct {
	svc      *services.ACMEService
	listener *fakeListener
	env      *testEnv
	settings *repositories.SettingsRepository
	cipher   *crypto.Cipher
	dataDir  string
	reloads  atomic.Int32
	// names sind die Namen aus den Adressen, die der Dienst erfragt.
	names []string
}

func newACMEFixture(t *testing.T) *acmeFixture { return newACMEFixtureExec(t, "") }

// newACMEFixtureExec setzt acme_dns_exec der config.json.
func newACMEFixtureExec(t *testing.T, execPath string) *acmeFixture {
	t.Helper()
	env := newTestEnv(t)
	cipher, err := crypto.NewCipher(crypto.GenerateKey())
	if err != nil {
		t.Fatal(err)
	}
	f := &acmeFixture{env: env, listener: &fakeListener{}, settings: repositories.NewSettingsRepository(env.DB()),
		cipher: cipher, dataDir: t.TempDir(), names: []string{"lcm.example.com", "agent.example.com"}}
	audit := services.NewAuditService(repositories.NewAuditRepository(env.DB()))
	f.svc = services.NewACMEService(f.settings, audit,
		acmex.NewManager(f.dataDir), acmex.NewChallengeStore(), f.listener, ":8080", func() { f.reloads.Add(1) },
		cipher, acmex.DNSEnv{DataDir: f.dataDir, ExecPath: execPath}).
		WithNames(func(*domain.GlobalSettings) []string { return f.names })
	return f
}

func validUpdate() services.ACMEUpdate {
	return services.ACMEUpdate{Enabled: true, AcceptTerms: true}
}

func TestACMEViewDefaults(t *testing.T) {
	f := newACMEFixture(t)
	v, err := f.svc.View()
	if err != nil {
		t.Fatal(err)
	}
	if v.Enabled || v.DirectoryURL != services.DefaultACMEDirectory || v.Mode != services.ACMEModeSingle ||
		v.Challenge != services.ACMEChallengeHTTP01 || v.TermsAccepted || v.HTTPListen != ":8080" ||
		len(v.Certificates) != 0 || v.Running || v.ListenerRunning || v.NamesChangedNotice != nil {
		t.Fatalf("Vorgaben: %+v", v)
	}
	if strings.Join(v.Names, ",") != "lcm.example.com,agent.example.com" {
		t.Errorf("Namen aus den Adressen: %v", v.Names)
	}
}

// Der Listener auf Port 80 ist nie wegen der Einstellungen offen, nur während
// einer Prüfung.
func TestACMEUpdateStoresSettingsAndLeavesPort80Closed(t *testing.T) {
	f := newACMEFixture(t)
	in := validUpdate()
	in.Email = "admin@example.com"

	v, err := f.svc.Update(in, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if !v.Enabled || !v.TermsAccepted || v.Mode != services.ACMEModeSingle ||
		strings.Join(v.Names, ",") != "lcm.example.com,agent.example.com" ||
		v.DirectoryURL != services.DefaultACMEDirectory || v.Email != "admin@example.com" {
		t.Fatalf("Stand: %+v", v)
	}
	if f.listener.Running() || f.listener.starts() != 0 || v.ListenerRunning {
		t.Errorf("Listener nach dem Speichern offen: %+v", f.listener)
	}
	if auditCount(t, f.env, "settings.acme.update") != 1 {
		t.Error("kein Audit-Eintrag settings.acme.update")
	}
}

// Ohne einen DNS-Namen in den Adressen gibt es nichts auszustellen: Einschalten
// scheitert, Ausschalten nicht.
func TestACMEEnableNeedsANameFromTheAddresses(t *testing.T) {
	f := newACMEFixture(t)
	f.names = nil
	_, err := f.svc.Update(validUpdate(), "admin")
	var invalid *services.ACMEInvalidError
	if !errors.As(err, &invalid) || invalid.Field != "names" {
		t.Fatalf("ohne Namen: %v", err)
	}
	off := validUpdate()
	off.Enabled = false
	if _, err := f.svc.Update(off, "admin"); err != nil {
		t.Errorf("Ausschalten ohne Namen: %v", err)
	}
}

// LCM registriert nie ein Konto, ohne dass der Admin die Nutzungsbedingungen
// der CA angenommen hat - einmal je CA.
func TestACMEEnableNeedsAcceptedTermsOncePerCA(t *testing.T) {
	f := newACMEFixture(t)
	in := validUpdate()
	in.AcceptTerms = false
	if _, err := f.svc.Update(in, "admin"); !errors.Is(err, services.ErrACMETermsRequired) {
		t.Fatalf("ohne accept_terms: %v", err)
	}
	if f.listener.Running() {
		t.Error("abgelehnte Änderung hat den Listener gestartet")
	}

	in.AcceptTerms = true
	if _, err := f.svc.Update(in, "admin"); err != nil {
		t.Fatal(err)
	}
	in.AcceptTerms = false
	if _, err := f.svc.Update(in, "admin"); err != nil {
		t.Errorf("bei einer CA, deren Bedingungen schon angenommen sind, genügt es: %v", err)
	}

	in.DirectoryURL = "https://acme-staging-v02.api.letsencrypt.org/directory"
	if _, err := f.svc.Update(in, "admin"); !errors.Is(err, services.ErrACMETermsRequired) {
		t.Errorf("eine andere CA verlangt die Annahme von neuem: %v", err)
	}
}

func TestACMEUpdateValidation(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*services.ACMEUpdate)
		field  string
	}{
		{"unbekannte Challenge", func(u *services.ACMEUpdate) { u.Challenge = "tls-alpn-01" }, "challenge"},
		{"Verzeichnis ohne https", func(u *services.ACMEUpdate) { u.DirectoryURL = "http://ca.example/dir" }, "directory_url"},
		{"Verzeichnis mit Zugangsdaten", func(u *services.ACMEUpdate) { u.DirectoryURL = "https://user:pw@ca.example/dir" }, "directory_url"},
		{"Verzeichnis ohne Adresse", func(u *services.ACMEUpdate) { u.DirectoryURL = "https://" }, "directory_url"},
		{"ungültige E-Mail", func(u *services.ACMEUpdate) { u.Email = "kein-mail" }, "email"},
	}
	f := newACMEFixture(t) // eine abgelehnte Änderung lässt nichts zurück
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := validUpdate()
			tc.mutate(&in)
			_, err := f.svc.Update(in, "admin")
			var invalid *services.ACMEInvalidError
			if !errors.As(err, &invalid) || invalid.Field != tc.field {
				t.Fatalf("Fehler %v, erwartet ungültiges Feld %s", err, tc.field)
			}
			if v, _ := f.svc.View(); v.Enabled {
				t.Error("eine abgelehnte Änderung wurde gespeichert")
			}
		})
	}
}

func TestACMEStagingAndCustomCAAreAccepted(t *testing.T) {
	f := newACMEFixture(t)
	for _, dir := range []string{
		"https://acme-staging-v02.api.letsencrypt.org/directory",
		"https://ca.internal:9000/acme/acme/directory",
	} {
		in := validUpdate()
		in.DirectoryURL = dir
		if v, err := f.svc.Update(in, "admin"); err != nil || v.DirectoryURL != dir {
			t.Errorf("%s: %v, %+v", dir, err, v)
		}
	}
}

func TestACMEDisableRemovesCertificates(t *testing.T) {
	f := newACMEFixture(t)
	if _, err := f.svc.Update(validUpdate(), "admin"); err != nil {
		t.Fatal(err)
	}
	off := validUpdate()
	off.Enabled = false
	v, err := f.svc.Update(off, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if v.Enabled || v.ListenerRunning {
		t.Errorf("nach dem Ausschalten: %+v", v)
	}
	if f.reloads.Load() == 0 {
		t.Error("der Pool wurde nach dem Entfernen der Zertifikate nicht neu geladen")
	}
}

func TestACMEIssueNowNeedsEnabled(t *testing.T) {
	f := newACMEFixture(t)
	if err := f.svc.IssueNow("admin"); !errors.Is(err, services.ErrACMENotActive) {
		t.Fatalf("ausgeschaltet: %v", err)
	}
}

// Scheitert ein Lauf, steht der Grund im Zustand, im Audit-Log und als
// Meldung für den Alarm.
func TestACMERunFailureIsRecordedAndRaisesAlertText(t *testing.T) {
	f := newACMEFixture(t)
	if _, err := f.svc.Update(validUpdate(), "admin"); err != nil {
		t.Fatal(err)
	}
	if msg := f.svc.RenewalFailure(); msg != "" {
		t.Fatalf("vor dem ersten Lauf: %q", msg)
	}

	// Nichts lauscht bei der CA: Der Lauf scheitert dort.
	in := validUpdate()
	in.DirectoryURL = "https://127.0.0.1:1/directory"
	if _, err := f.svc.Update(in, "admin"); err != nil {
		t.Fatal(err)
	}
	err := f.svc.Run("admin")
	if err == nil || !strings.Contains(err.Error(), "127.0.0.1:1") {
		t.Fatalf("Lauf: %v", err)
	}
	v, _ := f.svc.View()
	if v.LastRunAt == nil || !strings.Contains(v.LastError, "127.0.0.1:1") {
		t.Errorf("Zustand: %+v", v)
	}
	if msg := f.svc.RenewalFailure(); !strings.Contains(msg, "127.0.0.1:1") {
		t.Errorf("Meldung für den Alarm: %q", msg)
	}
	if auditCount(t, f.env, "settings.acme.failed") != 1 {
		t.Error("kein Audit-Eintrag settings.acme.failed")
	}

	off := validUpdate()
	off.Enabled = false
	if _, err := f.svc.Update(off, "admin"); err != nil {
		t.Fatal(err)
	}
	if msg := f.svc.RenewalFailure(); msg != "" {
		t.Errorf("ausgeschaltet gibt es keinen Alarm: %q", msg)
	}
}

// Der Alarm „Zertifikat-Erneuerung fehlgeschlagen" folgt der Meldung - einmal,
// ohne Serverbezug; ohne Meldung bleibt er still.
func TestAlertTLSRenewalFailed(t *testing.T) {
	env := newAlertEnv(t)
	env.createServer(t, "web-01", nil)
	failure := ""
	env.alerts.WithRenewalFailure(func() string { return failure })
	if _, err := env.alerts.Create(services.AlertRuleInput{
		Name: "Erneuerung", Type: domain.AlertTypeTLSRenewalFailed, Enabled: true,
	}, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.alerts.Evaluate("test"); err != nil {
		t.Fatal(err)
	}
	if ev, _ := env.alerts.ListEvents(10); len(ev) != 0 {
		t.Fatalf("ohne Fehler kein Alarm erwartet, bekam %d", len(ev))
	}

	failure = "lcm.example.com: ratenlimit erreicht"
	if _, err := env.alerts.Evaluate("test"); err != nil {
		t.Fatal(err)
	}
	events, _ := env.alerts.ListEvents(10)
	if len(events) != 1 || events[0].ServerID != nil || events[0].Type != domain.AlertTypeTLSRenewalFailed ||
		!strings.Contains(events[0].Description, "Erneuerung fehlgeschlagen") {
		t.Fatalf("Alarme: %+v", events)
	}
}

// waitIdle wartet, bis kein Lauf mehr aktiv ist.
func waitIdle(t *testing.T, svc *services.ACMEService) services.ACMEView {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if v, _ := svc.View(); !v.Running {
			return v
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("der Hintergrundlauf ist nicht fertig geworden")
	return services.ACMEView{}
}

func TestACMEIssueNowRunsInBackgroundAndRecordsFailure(t *testing.T) {
	f := newACMEFixture(t)
	in := validUpdate()
	in.DirectoryURL = "https://127.0.0.1:1/directory" // nichts lauscht: der Lauf scheitert schnell
	if _, err := f.svc.Update(in, "admin"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.IssueNow("admin"); err != nil {
		t.Fatal(err)
	}
	v := waitIdle(t, f.svc)
	if v.LastRunAt == nil || v.LastError == "" {
		t.Errorf("der Lauf gegen eine fehlende CA hat keinen Fehler gemeldet: %+v", v)
	}
}

// holdConnections lauscht und antwortet nie: ein Lauf gegen diese CA hängt,
// bis der Test die Verbindungen schließt.
func holdConnections(t *testing.T) (addr string, release func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var conns []net.Conn
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, conn)
			mu.Unlock()
		}
	}()
	return ln.Addr().String(), func() {
		_ = ln.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			_ = c.Close()
		}
	}
}

// Es läuft immer nur ein Lauf; währenddessen lässt sich ACME auch nicht
// ausschalten (der Lauf legte die Zertifikate wieder an).
func TestACMEOnlyOneRunAtATime(t *testing.T) {
	f := newACMEFixture(t)
	addr, release := holdConnections(t)
	defer release()
	in := validUpdate()
	in.DirectoryURL = "https://" + addr + "/directory"
	if _, err := f.svc.Update(in, "admin"); err != nil {
		t.Fatal(err)
	}

	if err := f.svc.IssueNow("admin"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.IssueNow("admin"); !errors.Is(err, services.ErrACMERunning) {
		t.Errorf("zweiter Lauf: %v, erwartet ErrACMERunning", err)
	}
	off := in
	off.Enabled = false
	if _, err := f.svc.Update(off, "admin"); !errors.Is(err, services.ErrACMERunning) {
		t.Errorf("Ausschalten während des Laufs: %v, erwartet ErrACMERunning", err)
	}

	release()
	waitIdle(t, f.svc)
}

// Ein Panic im Lauf endet als Fehler des Laufs: running ist zurückgesetzt, der
// Grund steht im Zustand und im Alarm, und der nächste Lauf - so wie ihn die
// tägliche Schleife startet - läuft wieder. Der Panic kommt hier aus dem
// Schließen des Listeners am Ende des Laufs; LCM hat keine Naht für die CA
// selbst.
func TestACMEPanicInRunEndsAsRunFailure(t *testing.T) {
	f := newACMEFixture(t)
	in := validUpdate()
	in.DirectoryURL = "https://127.0.0.1:1/directory" // nichts lauscht: ein Lauf scheitert schnell
	if _, err := f.svc.Update(in, "admin"); err != nil {
		t.Fatal(err)
	}

	f.listener.panicMsg = "simulierter Panic: LEGO_CA_CERTIFICATES nicht lesbar"
	if err := f.svc.IssueNow("admin"); err != nil {
		t.Fatal(err)
	}
	v := waitIdle(t, f.svc)
	if v.Running || v.LastRunAt == nil || !strings.Contains(v.LastError, "LEGO_CA_CERTIFICATES") {
		t.Fatalf("nach dem Panic: %+v", v)
	}
	if msg := f.svc.RenewalFailure(); !strings.Contains(msg, "LEGO_CA_CERTIFICATES") {
		t.Errorf("Meldung für den Alarm: %q", msg)
	}
	if auditCount(t, f.env, "settings.acme.failed") != 1 {
		t.Error("kein Audit-Eintrag settings.acme.failed")
	}

	f.listener.panicMsg = ""
	err := f.svc.Run("system")
	if err == nil || strings.Contains(err.Error(), "LEGO_CA_CERTIFICATES") {
		t.Fatalf("nächster Lauf: %v, erwartet der Fehler der CA", err)
	}
	v, _ = f.svc.View()
	if v.Running || strings.Contains(v.LastError, "LEGO_CA_CERTIFICATES") {
		t.Errorf("Zustand nach dem nächsten Lauf: %+v", v)
	}
}

// Port 80 geht erst auf, wenn die CA eine Prüfung verlangt: Ein Lauf, der die
// CA nicht erreicht (oder bei dem nichts fällig ist), öffnet ihn nie, schließt
// ihn aber am Ende sicher.
func TestACMERunDoesNotOpenPort80WithoutAChallenge(t *testing.T) {
	f := newACMEFixture(t)
	in := validUpdate()
	in.DirectoryURL = "https://127.0.0.1:1/directory"
	if _, err := f.svc.Update(in, "admin"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Run("admin"); err == nil {
		t.Fatal("der Lauf gegen eine fehlende CA gelang")
	}
	if f.listener.starts() != 0 || f.listener.Running() {
		t.Errorf("Port 80 war ohne Prüfung offen: Starts %d", f.listener.starts())
	}
	if f.listener.stopped() == 0 {
		t.Error("der Lauf hat den Listener am Ende nicht geschlossen")
	}
}

// Ohne einen Namen aus den Adressen stellt ein Lauf nichts aus und sagt das.
func TestACMERunWithoutNamesFailsClearly(t *testing.T) {
	f := newACMEFixture(t)
	if _, err := f.svc.Update(validUpdate(), "admin"); err != nil {
		t.Fatal(err)
	}
	f.names = nil
	err := f.svc.Run("admin")
	if err == nil || !strings.Contains(err.Error(), "DNS-Name") {
		t.Errorf("Lauf ohne Namen: %v", err)
	}
}
