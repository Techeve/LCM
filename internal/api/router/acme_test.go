package router_test

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"

	"LCM/internal/api/router"
	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/infrastructure/acmex"
	"LCM/internal/infrastructure/crypto"
	"LCM/internal/storage/repositories"
)

// noListener ersetzt den Listener für HTTP-01: Die Tests öffnen keinen Port.
type noListener struct{ running bool }

func (l *noListener) Start(string) error { l.running = true; return nil }
func (l *noListener) Stop()              { l.running = false }
func (l *noListener) Running() bool      { return l.running }

func acmeApp(t *testing.T) (*fiber.App, *gorm.DB) { return acmeAppExec(t, "") }

// acmeAppExec setzt acme_dns_exec der config.json.
func acmeAppExec(t *testing.T, execPath string) (*fiber.App, *gorm.DB) {
	t.Helper()
	return buildTestAppDB(t, 0, nil, func(d *router.Deps, db *gorm.DB) {
		audit := services.NewAuditService(repositories.NewAuditRepository(db))
		cipher, err := crypto.NewCipher(crypto.GenerateKey())
		if err != nil {
			t.Fatal(err)
		}
		dataDir := t.TempDir()
		settings := repositories.NewSettingsRepository(db)
		// Die Namen kommen aus den öffentlichen Adressen: zwei verschiedene Namen.
		if err := settings.UpdateFields(map[string]any{
			"public_base_url": "https://lcm.example.com", "public_agent_url": "https://agent.example.com"}); err != nil {
			t.Fatal(err)
		}
		addresses := services.NewSettingsService(settings, cipher, audit, func() error { return nil }).
			WithAddressConfig(services.AddressConfig{Scheme: "https", FallbackHost: "localhost"})
		d.ACME = services.NewACMEService(settings, audit, acmex.NewManager(dataDir),
			acmex.NewChallengeStore(), &noListener{}, ":8080", func() {}, cipher,
			acmex.DNSEnv{DataDir: dataDir, ExecPath: execPath}).WithNames(addresses.ACMENames)
	})
}

type acmeBody struct {
	Enabled       bool     `json:"enabled"`
	DirectoryURL  string   `json:"directory_url"`
	Names         []string `json:"names"`
	Mode          string   `json:"mode"`
	Challenge     string   `json:"challenge"`
	TermsAccepted bool     `json:"terms_accepted"`
	HTTPListen    string   `json:"http_listen"`
	Running       bool     `json:"running"`
	LastError     string   `json:"last_error"`
	Certificates  []any    `json:"certificates"`
	Notice        *struct {
		Old        []string `json:"old"`
		New        []string `json:"new"`
		WasPerName bool     `json:"was_per_name"`
	} `json:"names_changed_notice"`
}

func getACME(t *testing.T, app *fiber.App, token string) acmeBody {
	t.Helper()
	resp := doRequest(t, app, "GET", "/api/v1/system/acme", token, "")
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("GET /system/acme: %d %s", resp.StatusCode, raw)
	}
	var out acmeBody
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("%v: %s", err, raw)
	}
	return out
}

func TestACMEGetShowsDefaults(t *testing.T) {
	app, _ := acmeApp(t)
	admin := loginToken(t, app, "admin", "test-admin-passwort")

	got := getACME(t, app, admin)
	if got.Enabled || got.DirectoryURL != services.DefaultACMEDirectory || got.Mode != "single" ||
		got.Challenge != "http-01" || got.HTTPListen != ":8080" || got.Certificates == nil {
		t.Errorf("Vorgaben: %+v", got)
	}
}

// system:endpoints gehört allein admin: Ein Manager mit settings:manage kommt
// nicht durch, weder lesend noch schreibend, und stößt auch keinen Lauf an.
func TestACMENeedsSystemEndpointsPermission(t *testing.T) {
	app, db := acmeApp(t)
	roles := repositories.NewRoleRepository(db)
	role := &domain.Role{Name: "betreiber", Description: "Einstellungen ohne Endpunkte"}
	if err := roles.Create(role); err != nil {
		t.Fatal(err)
	}
	perm, err := roles.EnsurePermission(domain.PermSettingsManage, "x")
	if err != nil {
		t.Fatal(err)
	}
	if err := roles.SetPermissions(role, []domain.Permission{*perm}); err != nil {
		t.Fatal(err)
	}
	users := services.NewUserService(repositories.NewUserRepository(db), roles)
	if _, err := users.CreateUser("betreiber", "", "Regen9-Amsel!Turmfalk", "", "", []string{"betreiber"}, "test"); err != nil {
		t.Fatal(err)
	}
	operator := loginToken(t, app, "betreiber", "Regen9-Amsel!Turmfalk")

	body := `{"enabled":true,"names":["lcm.example.com"],"accept_terms":true}`
	for _, call := range []struct{ method, path, body string }{
		{"GET", "/api/v1/system/acme", ""},
		{"PUT", "/api/v1/system/acme", body},
		{"POST", "/api/v1/system/acme/issue", ""},
		{"POST", "/api/v1/system/acme/dns-test", ""},
		{"POST", "/api/v1/system/acme/names-notice/ack", ""},
	} {
		if r := doRequest(t, app, call.method, call.path, operator, call.body); r.StatusCode != 403 {
			t.Errorf("%s %s als Betreiber: %d, erwartet 403", call.method, call.path, r.StatusCode)
		}
	}
	admin := loginToken(t, app, "admin", "test-admin-passwort")
	if got := getACME(t, app, admin); got.Enabled {
		t.Error("ein abgewiesener Aufruf hat ACME eingeschaltet")
	}
}

func TestACMEUpdateRejectsBadRequests(t *testing.T) {
	app, _ := acmeApp(t)
	admin := loginToken(t, app, "admin", "test-admin-passwort")

	cases := []struct {
		name, body, code string
		status           int
	}{
		{"enabled fehlt", `{"names":["lcm.example.com"]}`, "request.missing_fields", 400},
		{"ohne accept_terms", `{"enabled":true,"names":["lcm.example.com"]}`, "acme.terms_required", 422},
		{"unbekannte Challenge", `{"enabled":true,"names":["lcm.example.com"],"challenge":"tls-alpn-01","accept_terms":true}`, "acme.invalid", 422},
		{"http statt https", `{"enabled":true,"names":["lcm.example.com"],"directory_url":"http://ca.example/dir","accept_terms":true}`, "acme.invalid", 422},
	}
	for _, tc := range cases {
		r := doRequest(t, app, "PUT", "/api/v1/system/acme", admin, tc.body)
		if r.StatusCode != tc.status {
			t.Errorf("%s: %d, erwartet %d", tc.name, r.StatusCode, tc.status)
			continue
		}
		assertErrorCode(t, r, tc.code)
	}
	if got := getACME(t, app, admin); got.Enabled {
		t.Error("eine abgelehnte Änderung wurde gespeichert")
	}
}

func TestACMEUpdateSavesAndAudits(t *testing.T) {
	app, db := acmeApp(t)
	admin := loginToken(t, app, "admin", "test-admin-passwort")

	r := doRequest(t, app, "PUT", "/api/v1/system/acme", admin,
		`{"enabled":true,"email":"it@example.com","accept_terms":true}`)
	if r.StatusCode != 200 {
		raw, _ := io.ReadAll(r.Body)
		t.Fatalf("PUT: %d %s", r.StatusCode, raw)
	}
	got := getACME(t, app, admin)
	if !got.Enabled || !got.TermsAccepted || got.Mode != "single" || len(got.Names) != 2 || got.Names[0] != "lcm.example.com" {
		t.Errorf("Stand: %+v", got)
	}
	var n int64
	if err := db.Model(&domain.AuditLog{}).Where("action = ?", "settings.acme.update").Count(&n).Error; err != nil || n != 1 {
		t.Errorf("Audit settings.acme.update: %d, %v", n, err)
	}
}

// Ältere Anfragen schicken noch names und mode: Beides wird überlesen, ohne
// Fehler. Die Namen kommen aus den Adressen, und es gibt immer ein Zertifikat
// für alle.
func TestACMEUpdateIgnoresNamesAndMode(t *testing.T) {
	app, _ := acmeApp(t)
	admin := loginToken(t, app, "admin", "test-admin-passwort")

	for _, body := range []string{
		`{"enabled":true,"names":["andere.example.net"],"mode":"per_name","accept_terms":true}`,
		`{"enabled":true,"names":["192.0.2.1"],"mode":"unsinn","accept_terms":true}`,
	} {
		if r := doRequest(t, app, "PUT", "/api/v1/system/acme", admin, body); r.StatusCode != 200 {
			raw, _ := io.ReadAll(r.Body)
			t.Fatalf("PUT %s: %d %s", body, r.StatusCode, raw)
		}
		got := getACME(t, app, admin)
		if got.Mode != "single" || strings.Join(got.Names, ",") != "lcm.example.com,agent.example.com" {
			t.Errorf("Stand: modus %q, namen %v", got.Mode, got.Names)
		}
	}
}

// Der Hinweis zum Umstieg kommt aus dem alten Stand, lässt sich bestätigen und
// kommt danach nicht wieder.
func TestACMENamesNoticeCanBeAcknowledged(t *testing.T) {
	app, db := acmeApp(t)
	admin := loginToken(t, app, "admin", "test-admin-passwort")
	if got := getACME(t, app, admin); got.Notice != nil {
		t.Fatalf("Hinweis ohne alten Stand: %+v", got.Notice)
	}
	err := repositories.NewSettingsRepository(db).UpdateFields(map[string]any{
		"acme_enabled": true, "acme_names": "alt.example.com,lcm.example.com", "acme_mode": "per_name"})
	if err != nil {
		t.Fatal(err)
	}
	got := getACME(t, app, admin)
	if got.Notice == nil || !got.Notice.WasPerName || strings.Join(got.Notice.Old, ",") != "alt.example.com,lcm.example.com" ||
		strings.Join(got.Notice.New, ",") != "lcm.example.com,agent.example.com" {
		t.Fatalf("Hinweis: %+v", got.Notice)
	}

	r := doRequest(t, app, "POST", "/api/v1/system/acme/names-notice/ack", admin, "")
	if r.StatusCode != 200 {
		t.Fatalf("Bestätigen: %d", r.StatusCode)
	}
	if got := getACME(t, app, admin); got.Notice != nil {
		t.Errorf("Hinweis nach dem Bestätigen: %+v", got.Notice)
	}
}

func TestACMEIssueStartsRunInBackground(t *testing.T) {
	app, _ := acmeApp(t)
	admin := loginToken(t, app, "admin", "test-admin-passwort")

	r := doRequest(t, app, "POST", "/api/v1/system/acme/issue", admin, "")
	if r.StatusCode != 409 {
		t.Fatalf("ausgeschaltet: %d, erwartet 409", r.StatusCode)
	}
	assertErrorCode(t, r, "acme.not_enabled")

	put := `{"enabled":true,"names":["lcm.example.com"],"directory_url":"https://127.0.0.1:1/directory","accept_terms":true}`
	if r := doRequest(t, app, "PUT", "/api/v1/system/acme", admin, put); r.StatusCode != 200 {
		t.Fatalf("PUT: %d", r.StatusCode)
	}
	if r := doRequest(t, app, "POST", "/api/v1/system/acme/issue", admin, ""); r.StatusCode != 202 {
		t.Fatalf("eingeschaltet: %d, erwartet 202", r.StatusCode)
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if got := getACME(t, app, admin); !got.Running {
			if got.LastError == "" {
				t.Error("der Lauf gegen eine fehlende CA meldet keinen Fehler")
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("der Lauf ist nicht fertig geworden")
}

type dnsField struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Required bool     `json:"required"`
	Options  []string `json:"options"`
}

type dnsProviderBody struct {
	ID     string     `json:"id"`
	Fields []dnsField `json:"fields"`
}

type dnsBody struct {
	Challenge    string            `json:"challenge"`
	DNSProvider  string            `json:"dns_provider"`
	DNSValues    map[string]any    `json:"dns_values"`
	SecretsSet   map[string]bool   `json:"dns_secrets_set"`
	CheckServers []string          `json:"dns_check_nameservers"`
	ListenerRuns bool              `json:"listener_running"`
	Names        []string          `json:"names"`
	DNSProviders []dnsProviderBody `json:"dns_providers"`
}

func getDNS(t *testing.T, app *fiber.App, token string) (dnsBody, string) {
	t.Helper()
	resp := doRequest(t, app, "GET", "/api/v1/system/acme", token, "")
	raw, _ := io.ReadAll(resp.Body)
	var out dnsBody
	if err := json.Unmarshal(raw, &out); err != nil || resp.StatusCode != 200 {
		t.Fatalf("GET: %d %s %v", resp.StatusCode, raw, err)
	}
	return out, string(raw)
}

// Die Liste der Anbieter trägt Felder mit Art, Pflicht und Auswahl; exec
// erscheint nur, wenn die config.json ein Programm nennt.
func TestACMEGetListsDNSProviders(t *testing.T) {
	app, _ := acmeApp(t)
	admin := loginToken(t, app, "admin", "test-admin-passwort")
	got, _ := getDNS(t, app, admin)

	var ids []string
	fields := map[string][]dnsField{}
	for _, p := range got.DNSProviders {
		ids = append(ids, p.ID)
		fields[p.ID] = p.Fields
	}
	if strings.Join(ids, ",") != "rfc2136,acmedns,httpreq,cloudflare,hetzner,pdns,inwx,ionos" {
		t.Fatalf("Anbieter: %v", ids)
	}
	var algorithm *dnsField
	for i, f := range fields["rfc2136"] {
		if f.Name == "tsig_algorithm" {
			algorithm = &fields["rfc2136"][i]
		}
	}
	if algorithm == nil || algorithm.Type != "select" || len(algorithm.Options) == 0 {
		t.Errorf("rfc2136: %+v", fields["rfc2136"])
	}
	if f := fields["cloudflare"][0]; f.Name != "api_token" || f.Type != "secret" || !f.Required {
		t.Errorf("cloudflare: %+v", fields["cloudflare"])
	}

	execApp, _ := acmeAppExec(t, "/usr/local/bin/lcm-dns-hook")
	got, _ = getDNS(t, execApp, loginToken(t, execApp, "admin", "test-admin-passwort"))
	if !slices.ContainsFunc(got.DNSProviders, func(p dnsProviderBody) bool { return p.ID == "exec" }) {
		t.Error("exec fehlt trotz acme_dns_exec")
	}
}

// Über die API: Geheimnisse kommen nie zurück, ein Update ohne Geheimfeld
// behält sie, ein leerer Text löscht sie; Zahl und Wahrheitswert gehen als JSON.
func TestACMEDNSSettingsRoundTripNeverReturnsSecrets(t *testing.T) {
	app, _ := acmeApp(t)
	admin := loginToken(t, app, "admin", "test-admin-passwort")
	put := func(body string) *http.Response {
		t.Helper()
		return doRequest(t, app, "PUT", "/api/v1/system/acme", admin, body)
	}

	r := put(`{"enabled":true,"accept_terms":true,"challenge":"dns-01","names":["*.example.com","example.com"],
		"dns_provider":"inwx","dns_check_nameservers":["9.9.9.9"],
		"dns_values":{"username":"benutzer","password":"streng-geheim","sandbox":true,"propagation_timeout":90}}`)
	raw, _ := io.ReadAll(r.Body)
	if r.StatusCode != 200 || strings.Contains(string(raw), "streng-geheim") {
		t.Fatalf("PUT: %d %s", r.StatusCode, raw)
	}
	got, body := getDNS(t, app, admin)
	if strings.Contains(body, "streng-geheim") {
		t.Fatalf("GET liefert ein Geheimnis: %s", body)
	}
	if got.DNSProvider != "inwx" || got.DNSValues["sandbox"] != true || got.DNSValues["propagation_timeout"] != float64(90) ||
		got.DNSValues["username"] != "benutzer" || !got.SecretsSet["password"] || got.SecretsSet["totp_secret"] ||
		len(got.CheckServers) != 1 || got.ListenerRuns {
		t.Errorf("Stand: %+v", got)
	}

	// Ohne Geheimfeld: bleibt.
	if r := put(`{"enabled":true,"challenge":"dns-01","names":["example.com"],"dns_provider":"inwx","dns_values":{"username":"neu"}}`); r.StatusCode != 200 {
		t.Fatalf("PUT ohne Geheimfeld: %d", r.StatusCode)
	}
	if got, _ = getDNS(t, app, admin); !got.SecretsSet["password"] || got.DNSValues["username"] != "neu" {
		t.Errorf("Geheimnis ging verloren: %+v", got)
	}
	// Leer: löschen, und das Passwort ist Pflicht.
	r = put(`{"enabled":true,"challenge":"dns-01","names":["example.com"],"dns_provider":"inwx","dns_values":{"username":"neu","password":""}}`)
	if r.StatusCode != 422 {
		t.Fatalf("PUT mit leerem Pflichtgeheimnis: %d", r.StatusCode)
	}
	assertErrorCode(t, r, "acme.field_missing")
}

// Das Programm für exec steht in der config.json und nirgends in der API.
func TestACMEDNSExecCannotBeSetThroughAPI(t *testing.T) {
	app, _ := acmeApp(t)
	admin := loginToken(t, app, "admin", "test-admin-passwort")
	for name, body := range map[string]string{
		"ohne config.json": `{"enabled":true,"accept_terms":true,"challenge":"dns-01","names":["example.com"],"dns_provider":"exec"}`,
		"Programm in den Werten": `{"enabled":true,"accept_terms":true,"challenge":"dns-01","names":["example.com"],"dns_provider":"httpreq",
			"dns_values":{"endpoint":"https://x.example","program":"/bin/sh"}}`,
	} {
		r := doRequest(t, app, "PUT", "/api/v1/system/acme", admin, body)
		if r.StatusCode != 422 {
			t.Errorf("%s: %d, erwartet 422", name, r.StatusCode)
			continue
		}
		assertErrorCode(t, r, "acme.invalid")
	}
	if got, _ := getDNS(t, app, admin); got.DNSProvider != "" {
		t.Errorf("eine abgelehnte Änderung wurde gespeichert: %+v", got)
	}
}

func TestACMEDNSTestReportsErrors(t *testing.T) {
	app, db := acmeApp(t)
	admin := loginToken(t, app, "admin", "test-admin-passwort")

	r := doRequest(t, app, "POST", "/api/v1/system/acme/dns-test", admin, "")
	if r.StatusCode != 422 {
		t.Fatalf("ohne Anbieter: %d", r.StatusCode)
	}
	assertErrorCode(t, r, "acme.field_missing")

	// Ein Endpunkt, an dem nichts lauscht: Der Anbieter meldet den Fehler.
	body := `{"names":["lcm.example.com"],"dns_provider":"httpreq","dns_values":{"endpoint":"http://127.0.0.1:1/hook"}}`
	r = doRequest(t, app, "POST", "/api/v1/system/acme/dns-test", admin, body)
	if r.StatusCode != 422 {
		t.Fatalf("Probe gegen toten Endpunkt: %d", r.StatusCode)
	}
	assertErrorCode(t, r, "acme.dns_test_failed")
	var n int64
	if err := db.Model(&domain.AuditLog{}).Where("action = ?", "settings.acme.dns_test").Count(&n).Error; err != nil || n != 1 {
		t.Errorf("Audit settings.acme.dns_test: %d, %v", n, err)
	}
}
