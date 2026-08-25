package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/crypto"
	"LCM/internal/storage/repositories"
)

// newSubscriptionEnv baut den Dienst mit In-Memory-DB und echtem Cipher.
func newSubscriptionEnv(t *testing.T) (*SubscriptionService, *repositories.SettingsRepository) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("test-db: %v", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	if err := db.AutoMigrate(&domain.Server{}, &domain.GlobalSettings{}, &domain.AuditLog{}); err != nil {
		t.Fatalf("migration: %v", err)
	}
	settings := repositories.NewSettingsRepository(db)
	if err := settings.Save(&domain.GlobalSettings{}); err != nil {
		t.Fatalf("einstellungen anlegen: %v", err)
	}
	cipher, err := crypto.NewCipher(make([]byte, 32))
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	audit := NewAuditService(repositories.NewAuditRepository(db))
	svc := NewSubscriptionService(settings, repositories.NewServerRepository(db), cipher, audit)
	return svc, settings
}

// fakeSubscriptionServer stellt den Anbieter-Dienst nach - mit dem echten
// Protokoll (JSON-Formen des Dienstes aus techeve/lcm-subscription-service).
func fakeSubscriptionServer(t *testing.T) (*httptest.Server, *struct {
	ActivateStatus int
	VerifyStatus   int
	VerifyBody     map[string]any
	LastActivate   map[string]any
	LastVerify     map[string]any
}) {
	t.Helper()
	state := &struct {
		ActivateStatus int
		VerifyStatus   int
		VerifyBody     map[string]any
		LastActivate   map[string]any
		LastVerify     map[string]any
	}{ActivateStatus: 200, VerifyStatus: 200,
		VerifyBody: map[string]any{"status": "active", "customer": "Musterfirma", "expires_at": "2027-08-05", "key_prefix": "LCM-E-ABCDE…", "included_servers": 25}}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/subscription/v1/activate":
			state.LastActivate = body
			w.WriteHeader(state.ActivateStatus)
			if state.ActivateStatus == 200 {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"key_prefix": "LCM-E-ABCDE…", "customer": "Musterfirma", "status": "active",
					"expires_at": "2027-08-05", "included_servers": 25, "repo_url": srv0URL(r) + "/enterprise",
					"repo_login": body["instance_id"], "repo_password": "deadbeef00112233445566778899aabbccddeeff00112233",
				})
			} else {
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "dieser Subscription-Key ist bereits an eine andere LCM-Instanz gebunden"})
			}
		case "/subscription/v1/verify":
			state.LastVerify = body
			w.WriteHeader(state.VerifyStatus)
			if state.VerifyStatus == 200 {
				_ = json.NewEncoder(w).Encode(state.VerifyBody)
			} else {
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "unbekannter oder widerrufener Zugangsschlüssel"})
			}
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, state
}

// srv0URL rekonstruiert die Basis-URL aus dem Request (für repo_url).
func srv0URL(r *http.Request) string { return "http://" + r.Host }

// TestSubscriptionAktivierungSpeichertVerschluesselt: der Kern-Fluss -
// Key rein, Zugangsschlüssel raus, beides NUR verschlüsselt in der DB,
// Instanz-ID und Meldedaten gehen an den Dienst.
func TestSubscriptionAktivierungSpeichertVerschluesselt(t *testing.T) {
	svc, settingsRepo := newSubscriptionEnv(t)
	srv, state := fakeSubscriptionServer(t)
	if err := settingsRepo.UpdateFields(map[string]any{"subscription_service_url": srv.URL}); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Activate("LCM-E-ABCDE-FGHJK-MNPQR-STUVW", "admin"); err != nil {
		t.Fatalf("aktivierung: %v", err)
	}

	st, _ := settingsRepo.Get()
	if st.InstanceID == "" {
		t.Fatal("die Instanz-ID muss bei der Aktivierung entstehen")
	}
	if got := state.LastActivate["instance_id"]; got != st.InstanceID {
		t.Errorf("an den Dienst gemeldete Instanz-ID %v != gespeicherte %q", got, st.InstanceID)
	}
	if !st.SubscriptionConfigured() || st.SubscriptionStatus != "active" || st.SubscriptionCustomer != "Musterfirma" {
		t.Errorf("gespeicherter Stand unvollständig: %+v", st.SubscriptionStatus)
	}
	if st.SubscriptionIncludedServers != 25 {
		t.Errorf("Server-Kontingent nicht übernommen: %d", st.SubscriptionIncludedServers)
	}
	// Geheimnisse dürfen NIE im Klartext in der Spalte stehen.
	if strings.Contains(st.SubscriptionKeyEnc, "LCM-E-ABCDE") {
		t.Error("der Subscription-Key liegt im Klartext in der Datenbank")
	}
	if strings.Contains(st.SubscriptionAccessKeyEnc, "deadbeef") {
		t.Error("der Zugangsschlüssel liegt im Klartext in der Datenbank")
	}
	if st.SubscriptionLastCheckAt == nil {
		t.Error("der Aktivierungszeitpunkt zählt als erste Prüfung")
	}
}

// TestSubscriptionAktivierungKonflikt: die Absage des Dienstes (409, an
// andere Instanz gebunden) erreicht den Aufrufer im Klartext, und der
// lokale Zustand bleibt unangetastet.
func TestSubscriptionAktivierungKonflikt(t *testing.T) {
	svc, settingsRepo := newSubscriptionEnv(t)
	srv, state := fakeSubscriptionServer(t)
	_ = settingsRepo.UpdateFields(map[string]any{"subscription_service_url": srv.URL})
	state.ActivateStatus = 409

	_, err := svc.Activate("LCM-E-ABCDE-FGHJK-MNPQR-STUVW", "admin")
	if err == nil || !strings.Contains(err.Error(), "andere LCM-Instanz") {
		t.Fatalf("erwartete die Klartext-Absage des Dienstes, bekam: %v", err)
	}
	if st, _ := settingsRepo.Get(); st.SubscriptionConfigured() {
		t.Error("nach einer Absage darf lokal nichts gespeichert sein")
	}
}

// TestSubscriptionVerifyUebernimmtStand: das Lebenszeichen übernimmt den
// aktuellen Vertragsstand; 401 wird ehrlich als „revoked" abgelegt und
// ein Transportfehler als „unreachable" (unbekannt ≠ schlecht).
func TestSubscriptionVerifyUebernimmtStand(t *testing.T) {
	svc, settingsRepo := newSubscriptionEnv(t)
	srv, state := fakeSubscriptionServer(t)
	_ = settingsRepo.UpdateFields(map[string]any{"subscription_service_url": srv.URL})
	if _, err := svc.Activate("LCM-E-ABCDE-FGHJK-MNPQR-STUVW", "admin"); err != nil {
		t.Fatal(err)
	}

	// 1. Regulär: der Dienst meldet einen neuen Stand (verlängert und
	// per 10er-Paket erweitert).
	state.VerifyBody["expires_at"] = "2028-01-01"
	state.VerifyBody["included_servers"] = 35
	if _, err := svc.Verify("admin"); err != nil {
		t.Fatalf("verify: %v", err)
	}
	st, _ := settingsRepo.Get()
	if st.SubscriptionExpiresAt != "2028-01-01" || st.SubscriptionLastError != "" {
		t.Errorf("neuer Stand nicht übernommen: %q / %q", st.SubscriptionExpiresAt, st.SubscriptionLastError)
	}
	if st.SubscriptionIncludedServers != 35 {
		t.Errorf("erweitertes Kontingent nicht übernommen: %d", st.SubscriptionIncludedServers)
	}
	if state.LastVerify["access_key"] == "" || state.LastVerify["instance_id"] != st.InstanceID {
		t.Error("das Lebenszeichen muss Zugangsschlüssel und Instanz-ID melden")
	}

	// 2. 401: Zugangsschlüssel gilt nicht mehr → revoked mit Handlungshinweis.
	state.VerifyStatus = 401
	if _, err := svc.Verify(""); err != nil {
		t.Fatal(err)
	}
	st, _ = settingsRepo.Get()
	if st.SubscriptionStatus != "revoked" || !strings.Contains(st.SubscriptionLastError, "neu aktivieren") {
		t.Errorf("401 nicht ehrlich abgelegt: %q / %q", st.SubscriptionStatus, st.SubscriptionLastError)
	}

	// 3. Dienst weg: Status unreachable, KEIN Sturz auf revoked/expired.
	srv.Close()
	if _, err := svc.Verify(""); err != nil {
		t.Fatal(err)
	}
	st, _ = settingsRepo.Get()
	if st.SubscriptionStatus != subscriptionStatusUnreachable {
		t.Errorf("Transportfehler muss als unreachable erscheinen, nicht %q", st.SubscriptionStatus)
	}
}

// TestSubscriptionRemove: lokal restlos weg - inklusive der Geheimnisse.
func TestSubscriptionRemove(t *testing.T) {
	svc, settingsRepo := newSubscriptionEnv(t)
	srv, _ := fakeSubscriptionServer(t)
	_ = settingsRepo.UpdateFields(map[string]any{"subscription_service_url": srv.URL})
	if _, err := svc.Activate("LCM-E-ABCDE-FGHJK-MNPQR-STUVW", "admin"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Remove("admin"); err != nil {
		t.Fatal(err)
	}
	st, _ := settingsRepo.Get()
	if st.SubscriptionConfigured() || st.SubscriptionKeyEnc != "" || st.SubscriptionStatus != "" {
		t.Errorf("nach Remove darf nichts übrig bleiben: %+v", st.SubscriptionStatus)
	}
	if st.SubscriptionIncludedServers != 0 {
		t.Errorf("nach Remove darf kein Kontingent übrig bleiben: %d", st.SubscriptionIncludedServers)
	}
	if st.InstanceID == "" {
		t.Error("die Instanz-ID bleibt - sie ist die Identität der Installation, nicht der Subscription")
	}
}

// TestInstanzIDBleibtStabil: EnsureInstanceID darf eine vorhandene ID nie
// überschreiben - sonst verlöre die Installation ihre Bindung.
func TestInstanzIDBleibtStabil(t *testing.T) {
	svc, settingsRepo := newSubscriptionEnv(t)
	if err := svc.EnsureInstanceID(); err != nil {
		t.Fatal(err)
	}
	st, _ := settingsRepo.Get()
	first := st.InstanceID
	if first == "" {
		t.Fatal("keine Instanz-ID erzeugt")
	}
	if err := svc.EnsureInstanceID(); err != nil {
		t.Fatal(err)
	}
	if st, _ = settingsRepo.Get(); st.InstanceID != first {
		t.Errorf("Instanz-ID wurde überschrieben: %q -> %q", first, st.InstanceID)
	}
}

// TestSubscriptionKontingentHinweis: mehr verwaltete Server als im Vertrag
// enthalten ⇒ die Status-Sicht meldet die Überschreitung als HINWEIS -
// angelegt werden darf trotzdem beliebig viel (keine Durchsetzung). Ohne
// hinterlegtes Kontingent (0) gibt es keinen Hinweis.
func TestSubscriptionKontingentHinweis(t *testing.T) {
	svc, settingsRepo := newSubscriptionEnv(t)
	srv, state := fakeSubscriptionServer(t)
	_ = settingsRepo.UpdateFields(map[string]any{"subscription_service_url": srv.URL})
	if _, err := svc.Activate("LCM-E-ABCDE-FGHJK-MNPQR-STUVW", "admin"); err != nil {
		t.Fatal(err)
	}

	// 3 Server bei Kontingent 2: Überschreitung wird gemeldet, mehr nicht.
	state.VerifyBody["included_servers"] = 2
	if _, err := svc.Verify(""); err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"} {
		if err := svc.servers.Create(&domain.Server{Name: "srv-" + host, Host: host, SSHPort: 22}); err != nil {
			t.Fatalf("server anlegen: %v", err)
		}
	}
	view, err := svc.Status()
	if err != nil {
		t.Fatal(err)
	}
	if view.IncludedServers != 2 || view.ManagedServers != 3 || !view.QuotaExceeded {
		t.Errorf("Überschreitung nicht gemeldet: %+v", view)
	}

	// Kontingent per 10er-Paket erweitert: der Hinweis verschwindet.
	state.VerifyBody["included_servers"] = 12
	if _, err := svc.Verify(""); err != nil {
		t.Fatal(err)
	}
	if view, _ = svc.Status(); view.QuotaExceeded {
		t.Errorf("nach Erweiterung darf nichts mehr gemeldet werden: %+v", view)
	}

	// Kein Kontingent hinterlegt (0): kein Vergleich, kein Hinweis.
	state.VerifyBody["included_servers"] = 0
	if _, err := svc.Verify(""); err != nil {
		t.Fatal(err)
	}
	if view, _ = svc.Status(); view.QuotaExceeded || view.IncludedServers != 0 {
		t.Errorf("ohne Kontingent darf es keinen Hinweis geben: %+v", view)
	}
}

// TestSubscriptionDaysLeft: Resttage zählen bis zum ENDE des letzten
// gültigen Tags.
func TestSubscriptionDaysLeft(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.Local)
	if d, ok := subscriptionDaysLeft("2026-08-17", now); !ok || d != 12 {
		t.Errorf("12 Tage erwartet, bekam %d (ok=%v)", d, ok)
	}
	if d, ok := subscriptionDaysLeft("2026-08-05", now); !ok || d != 0 {
		t.Errorf("Ablauftag selbst: 0 volle Tage, bekam %d (ok=%v)", d, ok)
	}
	if _, ok := subscriptionDaysLeft("", now); ok {
		t.Error("unbefristet hat keine Resttage")
	}
	if _, ok := subscriptionDaysLeft("kaputt", now); ok {
		t.Error("unlesbares Datum darf keine Resttage melden")
	}
}

// TestEnterpriseChannelScriptRedaction: der Zugangsschlüssel steht im
// Umstell-Skript - im SSH-Protokoll darf er NICHT auftauchen, auch nicht
// in der sudo-verpackten Variante mit escapten Quotes.
func TestEnterpriseChannelScriptRedaction(t *testing.T) {
	const secret = "deadbeef00112233445566778899aabbccddeeff00112233"
	script := enterpriseChannelScript("https://repo.example/enterprise", "11111111-aaaa-4bbb-8ccc-000000000001", secret)

	if !strings.Contains(script, secret) {
		t.Fatal("Vorbedingung: das Skript enthält den Schlüssel")
	}
	red := redactSecrets(script)
	if strings.Contains(red, secret) {
		t.Error("redactSecrets lässt den Zugangsschlüssel im Klartext durch")
	}
	if !strings.Contains(red, "11111111-aaaa") {
		t.Error("die Instanz-ID (kein Geheimnis) sollte lesbar bleiben")
	}

	// sudo-Verpackung: einfache Quotes werden zu '\'' escaped (wie in privRun).
	wrapped := "sudo sh -c '" + strings.ReplaceAll(script, "'", `'\''`) + "'"
	if !strings.Contains(wrapped, secret) {
		t.Fatal("Vorbedingung: auch verpackt enthält das Kommando den Schlüssel")
	}
	if strings.Contains(redactSecrets(wrapped), secret) {
		t.Error("redactSecrets lässt den Schlüssel in der sudo-Verpackung durch")
	}
}

// TestChannelScriptsRollback: das Umstell-Skript muss den Rückbau bei
// fehlgeschlagener Prüfung enthalten, der Rückweg die Leere-Quellen-Sperre.
func TestChannelScriptsRollback(t *testing.T) {
	script := enterpriseChannelScript("https://repo.example/enterprise", "i", "s")
	for _, must := range []string{"mv \"$COM\" \"$COM.disabled\"", "rm -f \"$AUTH\" \"$ENT\"", "chmod 0600", "Dir::Etc::sourcelist"} {
		if !strings.Contains(script, must) {
			t.Errorf("Umstell-Skript ohne %q", must)
		}
	}
	revert := communityRevertScript()
	if !strings.Contains(revert, "exit 1") || !strings.Contains(revert, "ohne LCM-Paketquelle") {
		t.Error("der Rückweg muss abbrechen, wenn keine Community-Quelle da ist - nie ohne Paketquelle zurücklassen")
	}
}
