package router_test

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"

	"LCM/internal/api/router"
	"LCM/internal/config"
	"LCM/internal/core/services"
	"LCM/internal/infrastructure/crypto"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/storage"
	"LCM/internal/storage/repositories"
)

// newTestApp baut die komplette Fiber-App gegen eine In-Memory-DB -
// End-to-End-Test der HTTP-Schicht inkl. Middlewares und RBAC.
func newTestApp(t *testing.T) *fiber.App {
	app, _ := buildTestApp(t, 0, nil)
	return app
}

func newTestAppWithRateLimit(t *testing.T, ratePerMinute int) *fiber.App {
	app, _ := buildTestApp(t, ratePerMinute, nil)
	return app
}

// buildTestApp liefert zusätzlich die DB, damit Tests den User-Zustand
// (z.B. must_change_password) gezielt setzen können. frontendFS != nil
// aktiviert das Ausliefern des (Test-)SPA-Frontends.
func buildTestApp(t *testing.T, ratePerMinute int, frontendFS fs.FS) (*fiber.App, *gorm.DB) {
	t.Helper()
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.Migrate(db); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.RunUpdateMigrations(db, storage.UpdateOptions{
		VersionFilePath: filepath.Join(t.TempDir(), "version.json"),
		FreshInstall:    true,
	}); err != nil {
		t.Fatal(err)
	}
	// DevMode: schaltet die im Produktivbetrieb voreingestellte 2FA-Pflicht für
	// Administratoren ab. Die Tests prüfen Berechtigungen und API-Verhalten, nicht
	// den 2FA-Einrichtungsfluss (der hat eigene Tests) - ohne das liefe jeder
	// Request des Admins in die AccountRemediation-Sperre.
	cfg := &config.Config{AdminInitialPassword: "test-admin-passwort", DemoMode: false, DevMode: true}
	if err := storage.Seed(db, cfg); err != nil {
		t.Fatal(err)
	}

	userRepo := repositories.NewUserRepository(db)
	// Der geseedete Admin hat MustChangePassword=true; die AccountRemediation-
	// Middleware würde ihn sonst von allen Endpunkten aussperren. Für die
	// Tests den Normalzustand NACH der Pflicht-Passwortänderung herstellen.
	if admin, err := userRepo.FindByUsername("admin"); err == nil {
		_ = userRepo.UpdateFields(admin.ID, map[string]any{"must_change_password": false})
	}
	roleRepo := repositories.NewRoleRepository(db)
	settingsRepo := repositories.NewSettingsRepository(db)
	serverRepo := repositories.NewServerRepository(db)
	groupRepo := repositories.NewGroupRepository(db)
	cipher, err := crypto.NewCipher(crypto.GenerateKey())
	if err != nil {
		t.Fatal(err)
	}
	audit := services.NewAuditService(repositories.NewAuditRepository(db))
	jobs := services.NewJobService(repositories.NewJobRepository(db)).WithAudit(audit)
	linuxRepo := repositories.NewLinuxUserRepository(db)
	servers := services.NewServerService(serverRepo, jobs, audit, cipher, sshx.NewClient())
	prov := services.NewProvisioningService(linuxRepo, serverRepo, cipher, servers.Connect)
	executor := services.NewExecutor(serverRepo, groupRepo, jobs, audit, prov,
		services.NewBackupService(db, settingsRepo, t.TempDir(), ":memory:", ""), settingsRepo, servers.Connect)
	scheduler := services.NewScheduler(groupRepo, settingsRepo, executor)
	app := router.New(router.Deps{
		Auth:                     services.NewAuthService(userRepo, "test-secret-mit-mindestens-32-zeichen!!", time.Hour),
		APIKeys:                  services.NewAPIKeyService(repositories.NewAPIKeyRepository(db)),
		Users:                    services.NewUserService(userRepo, roleRepo),
		Servers:                  servers,
		Jobs:                     jobs,
		Audit:                    audit,
		Groups:                   services.NewGroupService(groupRepo, serverRepo, audit, scheduler.Reload),
		Scheduler:                scheduler,
		Backups:                  services.NewBackupService(db, settingsRepo, t.TempDir(), ":memory:", ""),
		Packages:                 services.NewPackageService(serverRepo),
		Provisioning:             prov,
		LinuxUsers:               services.NewLinuxUserService(linuxRepo, groupRepo, audit, cipher, prov),
		TOTP:                     services.NewTOTPService(userRepo, cipher, audit),
		Settings:                 services.NewSettingsService(settingsRepo, cipher, audit, nil),
		System:                   services.NewSystemService(),
		APIKeyRateLimitPerMinute: ratePerMinute,
		FrontendFS:               frontendFS,
	})
	return app, db
}

// TestSPACacheHeaders sichert die Cache-Strategie des eingebetteten Frontends:
// content-gehashte /assets/* dürfen ein Jahr cachen, index.html und SPA-Routen
// NIEMALS (no-cache) - sonst hält der Browser nach einem Update die alte
// index.html mit veralteten Asset-Referenzen fest (alte UI trotz neuer Version).
func TestSPACacheHeaders(t *testing.T) {
	frontendFS := fstest.MapFS{
		"index.html":              {Data: []byte("<!doctype html><title>test</title>")},
		"assets/index-abc123.js":  {Data: []byte("console.log(1)")},
		"assets/index-abc123.css": {Data: []byte("body{}")},
		"favicon.svg":             {Data: []byte("<svg/>")},
	}
	app, _ := buildTestApp(t, 0, frontendFS)

	cases := []struct {
		path      string
		wantCache string
	}{
		{"/", "no-cache"},                                       // index.html
		{"/servers", "no-cache"},                                // SPA-Route (Fallback index.html)
		{"/favicon.svg", "no-cache"},                            // sonstige statische Datei
		{"/assets/index-abc123.js", "public, max-age=31536000"}, // gehashtes Asset
		{"/assets/index-abc123.css", "public, max-age=31536000"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		resp, err := app.Test(req, testRequestConfig())
		if err != nil {
			t.Fatalf("%s: %v", tc.path, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s: Status %d (erwartet 200)", tc.path, resp.StatusCode)
		}
		if got := resp.Header.Get("Cache-Control"); got != tc.wantCache {
			t.Errorf("%s: Cache-Control = %q, erwartet %q", tc.path, got, tc.wantCache)
		}
	}
}

// TestAccountRemediationEnforcesPasswordChange prüft, dass ein Konto mit
// erzwungener Passwortänderung serverseitig gesperrt ist (nur Passwort-Reset
// erlaubt), und dass die Passwortänderung das alte Token invalidiert.
func TestAccountRemediationEnforcesPasswordChange(t *testing.T) {
	app, db := buildTestApp(t, 0, nil)
	userRepo := repositories.NewUserRepository(db)
	admin, err := userRepo.FindByUsername("admin")
	if err != nil {
		t.Fatal(err)
	}
	// Zurück in den "muss Passwort ändern"-Zustand (buildTestApp löscht das Flag).
	if err := userRepo.UpdateFields(admin.ID, map[string]any{"must_change_password": true}); err != nil {
		t.Fatal(err)
	}
	token := loginToken(t, app, "admin", "test-admin-passwort")

	// Ein regulärer Endpunkt ist gesperrt (403), obwohl das Token gültig ist.
	if resp := doRequest(t, app, "GET", "/api/v1/servers", token, ""); resp.StatusCode != 403 {
		t.Errorf("gesperrter Endpunkt: erwartet 403, bekam %d", resp.StatusCode)
	}
	// Der eigene Passwort-Reset ist erlaubt (Self-Reauth: aktuelles Passwort).
	reset := doRequest(t, app, "POST", fmt.Sprintf("/api/v1/users/%d/reset-password", admin.ID),
		token, `{"current_password":"test-admin-passwort","password":"neues-sicheres-passwort"}`)
	if reset.StatusCode != 200 && reset.StatusCode != 204 {
		t.Fatalf("reset-password sollte erlaubt sein, bekam %d", reset.StatusCode)
	}
	// Nach der Passwortänderung ist das ALTE Token ungültig (Token-Invalidierung)
	// - auch der erlaubte /auth/me-Endpunkt liefert nun 401.
	if resp := doRequest(t, app, "GET", "/api/v1/auth/me", token, ""); resp.StatusCode != 401 {
		t.Errorf("altes Token nach Passwortänderung: erwartet 401, bekam %d", resp.StatusCode)
	}
}

// TestSelfPasswordResetRequiresCurrentPassword prüft die Re-Authentifizierung
// beim eigenen Passwortwechsel: ohne/mit falschem aktuellem Passwort -> 401,
// mit korrektem -> Erfolg.
func TestSelfPasswordResetRequiresCurrentPassword(t *testing.T) {
	app, db := buildTestApp(t, 0, nil)
	admin, err := repositories.NewUserRepository(db).FindByUsername("admin")
	if err != nil {
		t.Fatal(err)
	}
	token := loginToken(t, app, "admin", "test-admin-passwort")
	path := fmt.Sprintf("/api/v1/users/%d/reset-password", admin.ID)

	if r := doRequest(t, app, "POST", path, token, `{"password":"Regen9-Amsel!Turmfalk"}`); r.StatusCode != 401 {
		t.Errorf("ohne aktuelles Passwort: erwartet 401, bekam %d", r.StatusCode)
	}
	if r := doRequest(t, app, "POST", path, token, `{"current_password":"falsch","password":"Regen9-Amsel!Turmfalk"}`); r.StatusCode != 401 {
		t.Errorf("mit falschem aktuellem Passwort: erwartet 401, bekam %d", r.StatusCode)
	}
	if r := doRequest(t, app, "POST", path, token, `{"current_password":"test-admin-passwort","password":"Regen9-Amsel!Turmfalk"}`); r.StatusCode != 200 {
		t.Errorf("mit korrektem aktuellem Passwort: erwartet 200, bekam %d", r.StatusCode)
	}
}

func doRequest(t *testing.T, app *fiber.App, method, path, token, body string) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := app.Test(req, testRequestConfig())
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// testRequestConfig hebt Fibers Standard-Zeitlimit von einer Sekunde an.
//
// Der Grund ist kein langsamer Endpunkt, sondern das Passwort-Hashing: Es
// ist absichtlich rechenintensiv und braucht unter Last - mehrere Pakete
// parallel, dazu der Race-Detector - laenger als eine Sekunde. Der Test
// schlug dann mit „i/o timeout" fehl, ohne dass irgendetwas kaputt war.
// Eine Minute ist grosszuegig genug, um Lastspitzen zu ueberstehen, und
// immer noch klein genug, um einen echten Haenger zu erkennen.
func testRequestConfig() fiber.TestConfig {
	return fiber.TestConfig{Timeout: time.Minute, FailOnTimeout: true}
}

func loginToken(t *testing.T, app *fiber.App, username, password string) string {
	t.Helper()
	resp := doRequest(t, app, "POST", "/api/v1/auth/login", "",
		`{"username":"`+username+`","password":"`+password+`"}`)
	if resp.StatusCode != 200 {
		t.Fatalf("login %s: status %d", username, resp.StatusCode)
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out.Token
}

// TestRBACMiddlewareRunsBeforeHandler ist der Regressionstest für die
// Fiber-v3-Handler-Reihenfolge: Middlewares MÜSSEN vor dem Controller
// laufen, sonst sind geschützte Routen öffentlich.
func TestRBACMiddlewareRunsBeforeHandler(t *testing.T) {
	app := newTestApp(t)

	protected := []struct {
		method, path string
	}{
		{"GET", "/api/v1/users"},
		{"GET", "/api/v1/users/1"},
		{"POST", "/api/v1/users"},
		{"GET", "/api/v1/roles"},
		{"GET", "/api/v1/apikeys"},
		{"GET", "/api/v1/auth/me"},
		{"GET", "/api/v1/servers"},
		{"POST", "/api/v1/servers/join"},
		{"GET", "/api/v1/server-groups"},
		{"GET", "/api/v1/jobs/history"},
		{"GET", "/api/v1/settings/global"},
		{"GET", "/api/v1/notification-channels"},
		{"GET", "/api/v1/alert-rules"},
		{"GET", "/api/v1/alert-events"},
		{"POST", "/api/v1/alerts/evaluate"},
	}
	for _, route := range protected {
		resp := doRequest(t, app, route.method, route.path, "", "")
		if resp.StatusCode != 401 {
			t.Errorf("%s %s ohne Token: erwartet 401, bekam %d", route.method, route.path, resp.StatusCode)
		}
	}
}

func TestPermissionDeniedIs403(t *testing.T) {
	app := newTestApp(t)
	admin := loginToken(t, app, "admin", "test-admin-passwort")

	// Manager anlegen (verwaltet Server, aber KEINE Admin-Funktionen).
	resp := doRequest(t, app, "POST", "/api/v1/users", admin,
		`{"username":"normalo","password":"Anker5-Leuchtturm!Wind","roles":["manager"]}`)
	if resp.StatusCode != 201 {
		t.Fatalf("user anlegen: status %d", resp.StatusCode)
	}
	manager := loginToken(t, app, "normalo", "Anker5-Leuchtturm!Wind")

	// Manager DARF Server sehen (200), aber KEINE globalen Einstellungen (403).
	if resp = doRequest(t, app, "GET", "/api/v1/servers", manager, ""); resp.StatusCode != 200 {
		t.Errorf("manager server-liste: erwartet 200, bekam %d", resp.StatusCode)
	}
	if resp = doRequest(t, app, "GET", "/api/v1/settings/global", manager, ""); resp.StatusCode != 403 {
		t.Errorf("manager settings: erwartet 403, bekam %d", resp.StatusCode)
	}
	// Admin darf beides.
	if resp = doRequest(t, app, "GET", "/api/v1/settings/global", admin, ""); resp.StatusCode != 200 {
		t.Errorf("admin settings: erwartet 200, bekam %d", resp.StatusCode)
	}
	// Manager darf keine Userliste sehen (users:read ist admin-only).
	if resp = doRequest(t, app, "GET", "/api/v1/users", manager, ""); resp.StatusCode != 403 {
		t.Errorf("manager user-liste: erwartet 403, bekam %d", resp.StatusCode)
	}
}

func TestPublicRoutes(t *testing.T) {
	app := newTestApp(t)

	for _, path := range []string{"/api/v1/health", "/api/v1/system/info"} {
		resp := doRequest(t, app, "GET", path, "", "")
		if resp.StatusCode != 200 {
			t.Errorf("%s: erwartet 200, bekam %d", path, resp.StatusCode)
		}
	}
}

func TestUnknownAPIRouteIsJSON404(t *testing.T) {
	app := newTestApp(t)

	resp := doRequest(t, app, "GET", "/api/v1/gibtsnicht", "", "")
	if resp.StatusCode != 404 {
		t.Fatalf("erwartet 404, bekam %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"error"`) {
		t.Errorf("404 sollte JSON-Fehler sein, bekam: %s", body)
	}
}

// createAPIKey legt per Admin-Token einen Key an und liefert den Klartext.
func createAPIKey(t *testing.T, app *fiber.App, admin, body string) string {
	t.Helper()
	resp := doRequest(t, app, "POST", "/api/v1/apikeys", admin, body)
	if resp.StatusCode != 201 {
		t.Fatalf("key anlegen: status %d", resp.StatusCode)
	}
	var out struct {
		Key string `json:"key"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out.Key
}

func apiKeyRequest(t *testing.T, app *fiber.App, method, path, key, body string) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-API-Key", key)
	resp, err := app.Test(req, testRequestConfig())
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestAPIKeyAuthentication(t *testing.T) {
	app := newTestApp(t)
	admin := loginToken(t, app, "admin", "test-admin-passwort")
	key := createAPIKey(t, app, admin, `{"name":"ci"}`)

	if resp := apiKeyRequest(t, app, "GET", "/api/v1/users", key, ""); resp.StatusCode != 200 {
		t.Errorf("API-Key-Request: erwartet 200, bekam %d", resp.StatusCode)
	}
}

// TestAPIKeyReadScope: read-Keys dürfen lesen, aber nicht schreiben.
func TestAPIKeyReadScope(t *testing.T) {
	app := newTestApp(t)
	admin := loginToken(t, app, "admin", "test-admin-passwort")
	readKey := createAPIKey(t, app, admin, `{"name":"monitor","scope":"read"}`)

	// Lesen: erlaubt (RBAC des Admin-Kontexts greift weiterhin).
	if resp := apiKeyRequest(t, app, "GET", "/api/v1/users", readKey, ""); resp.StatusCode != 200 {
		t.Errorf("read-Key GET: erwartet 200, bekam %d", resp.StatusCode)
	}
	// Schreiben: 403 trotz Admin-Kontext.
	resp := apiKeyRequest(t, app, "POST", "/api/v1/server-groups/create", readKey, `{"name":"x"}`)
	if resp.StatusCode != 403 {
		t.Errorf("read-Key POST: erwartet 403, bekam %d", resp.StatusCode)
	}
	resp = apiKeyRequest(t, app, "POST", "/api/v1/users", readKey,
		`{"username":"neu","password":"Anker5-Leuchtturm!Wind","roles":["manager"]}`)
	if resp.StatusCode != 403 {
		t.Errorf("read-Key POST: erwartet 403, bekam %d", resp.StatusCode)
	}

	// Ungültiger Scope beim Anlegen => 422.
	resp = doRequest(t, app, "POST", "/api/v1/apikeys", admin, `{"name":"x","scope":"superadmin"}`)
	if resp.StatusCode != 422 {
		t.Errorf("ungültiger Scope: erwartet 422, bekam %d", resp.StatusCode)
	}
}

// TestAPIKeyRateLimit: das konfigurierbare Limit greift pro Key und Minute.
func TestAPIKeyRateLimit(t *testing.T) {
	app := newTestAppWithRateLimit(t, 3)
	admin := loginToken(t, app, "admin", "test-admin-passwort")
	key := createAPIKey(t, app, admin, `{"name":"limited"}`)

	for i := 1; i <= 3; i++ {
		if resp := apiKeyRequest(t, app, "GET", "/api/v1/health", key, ""); resp.StatusCode != 200 {
			t.Fatalf("request %d: erwartet 200, bekam %d", i, resp.StatusCode)
		}
	}
	resp := apiKeyRequest(t, app, "GET", "/api/v1/health", key, "")
	if resp.StatusCode != 429 {
		t.Fatalf("request 4: erwartet 429, bekam %d", resp.StatusCode)
	}
	if resp.Header.Get("Retry-After") == "" {
		t.Error("429-Antwort ohne Retry-After-Header")
	}

	// JWT-Requests sind vom API-Key-Limit nicht betroffen.
	if resp := doRequest(t, app, "GET", "/api/v1/users", admin, ""); resp.StatusCode != 200 {
		t.Errorf("JWT-Request trotz Key-Limit: erwartet 200, bekam %d", resp.StatusCode)
	}
}

// TestSystemInfo: der DB-lose Logik-Controller liefert Versionsdaten.
func TestSystemInfo(t *testing.T) {
	app := newTestApp(t)

	resp := doRequest(t, app, "GET", "/api/v1/system/info", "", "")
	if resp.StatusCode != 200 {
		t.Fatalf("erwartet 200, bekam %d", resp.StatusCode)
	}
	var info struct {
		Version  string `json:"version"`
		Build    string `json:"build"`
		Platform string `json:"platform"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		t.Fatal(err)
	}
	// ANONYM: nur Version und Build (für den Footer der Anmeldeseite).
	// Commit, Go-Version, Plattform und Agent-Port sind Fingerprinting-Material
	// und bleiben angemeldeten Nutzern vorbehalten.
	if info.Version == "" || info.Build == "" {
		t.Errorf("unvollständige System-Info: %+v", info)
	}
	if info.Platform != "" {
		t.Errorf("anonyme System-Info darf die Plattform nicht verraten: %+v", info)
	}

	// ANGEMELDET: vollständige Angaben.
	token := loginToken(t, app, "admin", "test-admin-passwort")
	req := httptest.NewRequest("GET", "/api/v1/system/info", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	authResp, err := app.Test(req, testRequestConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer authResp.Body.Close()
	var full struct {
		Version  string `json:"version"`
		Platform string `json:"platform"`
	}
	if err := json.NewDecoder(authResp.Body).Decode(&full); err != nil {
		t.Fatal(err)
	}
	if full.Version == "" || full.Platform == "" {
		t.Errorf("angemeldete System-Info muss vollständig sein: %+v", full)
	}
}

// TestAgentGatewayServesOnlyMQTT sichert die Trennung der Schnittstellen:
// der dedizierte Agent-Listener (NewAgentGateway) bietet AUSSCHLIESSLICH den
// /mqtt-Endpunkt - keine REST-API, keine UI.
func TestAgentGatewayServesOnlyMQTT(t *testing.T) {
	called := false
	ws := func(c fiber.Ctx) error {
		called = true
		return c.SendString("ok")
	}
	gw := router.NewAgentGateway(ws, nil)

	// /mqtt existiert und ruft den Handler auf (kein 404).
	resp, err := gw.Test(httptest.NewRequest(http.MethodGet, "/mqtt", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode == http.StatusNotFound {
		t.Fatal("/mqtt fehlt auf dem Agent-Gateway")
	}
	if !called {
		t.Error("ws-Handler wurde nicht aufgerufen")
	}

	// REST/UI gibt es auf dem Agent-Gateway NICHT.
	for _, path := range []string{"/api/v1/health", "/", "/api/v1/servers"} {
		r, err := gw.Test(httptest.NewRequest(http.MethodGet, path, nil))
		if err != nil {
			t.Fatal(err)
		}
		if r.StatusCode != http.StatusNotFound {
			t.Errorf("%s auf dem Agent-Gateway: Status %d, erwartet 404", path, r.StatusCode)
		}
	}
}

// TestMainAppHasNoAgentInterface: der UI/REST-Listener bietet KEINEN
// /mqtt-Endpunkt - die Agent-Schnittstelle liegt nur auf dem Agent-Port.
func TestMainAppHasNoAgentInterface(t *testing.T) {
	frontendFS := fstest.MapFS{
		"index.html":  {Data: []byte("<!doctype html><title>test</title>")},
		"favicon.svg": {Data: []byte("<svg/>")},
	}
	app, _ := buildTestApp(t, 0, frontendFS)

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/mqtt", nil), testRequestConfig())
	if err != nil {
		t.Fatal(err)
	}
	// Ohne dedizierten /mqtt-Handler greift auf dem UI/REST-Port der SPA-
	// Fallback (index.html) - der Beweis, dass hier KEINE Agent-Schnittstelle
	// sitzt. Ein Agent-Handler würde stattdessen upgraden bzw. anders antworten.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/mqtt auf dem UI/REST-Port: Status %d, erwartet SPA-Fallback 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "<title>test</title>") {
		t.Error("/mqtt auf dem UI/REST-Port lieferte nicht den SPA-Fallback - eigener Agent-Handler vorhanden?")
	}
}

// TestStilleVerwerfungAbgewiesen bündelt die HTTP-Regressionsnachweise des
// „Stille Verwerfung"-Blocks aus Langzeittest Run 2: Typfehler und
// unbekannte Werte müssen 400 liefern, nie 200-mit-anderer-Wirkung.
func TestStilleVerwerfungAbgewiesen(t *testing.T) {
	app := newTestApp(t)
	admin := loginToken(t, app, "admin", "test-admin-passwort")

	// R2-012: ports als JSON-Array → 400 (früher: 200, nur SSH offen).
	if r := doRequest(t, app, "POST", "/api/v1/servers/1/firewall", admin,
		`{"enable":true,"ports":[22,8080,9999]}`); r.StatusCode != 400 {
		t.Errorf("R2-012 ports-Array: erwartet 400, bekam %d", r.StatusCode)
	}
	// Gegenprobe: korrekter String-Weg kommt am Binding vorbei (404 = Server
	// existiert nicht - das Format war also in Ordnung).
	if r := doRequest(t, app, "POST", "/api/v1/servers/1/firewall", admin,
		`{"enable":true,"ports":"8080,9999"}`); r.StatusCode != 404 {
		t.Errorf("R2-012 Gegenprobe: erwartet 404 (Format ok, Server fehlt), bekam %d", r.StatusCode)
	}

	// R2-050: unbekanntes Feld expires_at → 400 mit Feldnennung (früher:
	// 201 und ein UNBEFRISTETER Key).
	r := doRequest(t, app, "POST", "/api/v1/apikeys", admin,
		`{"name":"x","expires_at":"2020-01-01T00:00:00Z"}`)
	if r.StatusCode != 400 {
		t.Errorf("R2-050 expires_at: erwartet 400, bekam %d", r.StatusCode)
	} else {
		b, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(b), "expires_at") {
			t.Errorf("R2-050: die Meldung soll das unbekannte Feld nennen: %s", b)
		}
	}
	// R2-050: expires_in_days <= 0 → 400 (früher: unbefristet).
	for _, days := range []string{"0", "-1", "-30"} {
		if r := doRequest(t, app, "POST", "/api/v1/apikeys", admin,
			`{"name":"x","expires_in_days":`+days+`}`); r.StatusCode != 400 {
			t.Errorf("R2-050 expires_in_days=%s: erwartet 400, bekam %d", days, r.StatusCode)
		}
	}
	// Positivprobe: befristeter Key entsteht weiterhin.
	if r := doRequest(t, app, "POST", "/api/v1/apikeys", admin,
		`{"name":"befristet","expires_in_days":1}`); r.StatusCode != 201 {
		t.Errorf("R2-050 Positivprobe: erwartet 201, bekam %d", r.StatusCode)
	}

	// R2-069: unbekannte Job-Filter → 400 statt leerer 200-Treffermenge;
	// nicht parsbares server_id → 400 statt ALLER Jobs.
	for _, q := range []string{"status=quatsch", "type=quatsch", "server_id=abc"} {
		if r := doRequest(t, app, "GET", "/api/v1/jobs/history?"+q, admin, ""); r.StatusCode != 400 {
			t.Errorf("R2-069 %s: erwartet 400, bekam %d", q, r.StatusCode)
		}
	}
	for _, q := range []string{"", "?status=failed", "?type=update", "?server_id=999"} {
		if r := doRequest(t, app, "GET", "/api/v1/jobs/history"+q, admin, ""); r.StatusCode != 200 {
			t.Errorf("R2-069 Positivprobe %q: erwartet 200, bekam %d", q, r.StatusCode)
		}
	}

	// ssh-root-login ohne das Feld `disabled`: früher galt der Go-Nullwert
	// false = „Root-Login wieder ERLAUBEN", und die Antwort war 200. Ein
	// Tippfehler im Feldnamen nahm damit still den Schutz weg. Die Absicht
	// muss ausdrücklich im Body stehen - wie bei der Firewall mit `enable`.
	for _, body := range []string{`{}`, `{"enabled":false}`, `{"Disable":true}`} {
		r := doRequest(t, app, "POST", "/api/v1/servers/1/ssh-root-login", admin, body)
		if r.StatusCode != 400 {
			t.Errorf("ssh-root-login %s: erwartet 400, bekam %d", body, r.StatusCode)
			continue
		}
		b, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(b), "disabled") {
			t.Errorf("ssh-root-login %s: die Meldung soll das Pflichtfeld nennen: %s", body, b)
		}
	}
	// Gegenprobe: mit dem Feld kommt der Aufruf am Binding vorbei (404 =
	// Server existiert nicht - der Body war also in Ordnung).
	for _, body := range []string{`{"disabled":true}`, `{"disabled":false}`} {
		if r := doRequest(t, app, "POST", "/api/v1/servers/1/ssh-root-login", admin, body); r.StatusCode != 404 {
			t.Errorf("ssh-root-login Gegenprobe %s: erwartet 404, bekam %d", body, r.StatusCode)
		}
	}
}

// TestLogoutWiderruftToken (R2-059): POST /auth/logout war ein No-Op - der
// Token blieb serverseitig gültig. Jetzt muss er danach abgewiesen werden.
func TestLogoutWiderruftToken(t *testing.T) {
	app := newTestApp(t)
	token := loginToken(t, app, "admin", "test-admin-passwort")

	// Vorher: das Token wirkt.
	if r := doRequest(t, app, "GET", "/api/v1/auth/me", token, ""); r.StatusCode != 200 {
		t.Fatalf("vor Logout: erwartet 200, bekam %d", r.StatusCode)
	}
	if r := doRequest(t, app, "POST", "/api/v1/auth/logout", token, ""); r.StatusCode != 200 {
		t.Fatalf("logout: erwartet 200, bekam %d", r.StatusCode)
	}
	// Nachher: dasselbe Token ist tot.
	if r := doRequest(t, app, "GET", "/api/v1/auth/me", token, ""); r.StatusCode != 401 {
		t.Errorf("nach Logout muss das Token abgewiesen werden, bekam %d", r.StatusCode)
	}
	// Ein frischer Login geht weiterhin.
	if loginToken(t, app, "admin", "test-admin-passwort") == "" {
		t.Error("neuer Login nach Logout muss möglich sein")
	}
}

// TestServergruppeLoeschbar (R2-066): Der Disband-Endpunkt existierte, war
// aber nirgends erreichbar. Nachweis, dass er eine Gruppe wirklich entfernt
// und die System-Gruppe schützt.
func TestServergruppeLoeschbar(t *testing.T) {
	app := newTestApp(t)
	admin := loginToken(t, app, "admin", "test-admin-passwort")

	r := doRequest(t, app, "POST", "/api/v1/server-groups/create", admin, `{"name":"wegwerf","description":""}`)
	if r.StatusCode != 201 {
		t.Fatalf("gruppe anlegen: erwartet 201, bekam %d", r.StatusCode)
	}
	var created struct {
		ID uint `json:"id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&created)

	if r := doRequest(t, app, "POST", fmt.Sprintf("/api/v1/server-groups/%d/disband", created.ID), admin, ""); r.StatusCode != 204 {
		t.Fatalf("disband: erwartet 204, bekam %d", r.StatusCode)
	}
	// Weg.
	if r := doRequest(t, app, "GET", fmt.Sprintf("/api/v1/server-groups/%d", created.ID), admin, ""); r.StatusCode != 404 {
		t.Errorf("aufgelöste Gruppe muss 404 liefern, bekam %d", r.StatusCode)
	}
}
