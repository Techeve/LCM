package router_test

import (
	"testing"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"

	"LCM/internal/api/router"
	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/infrastructure/crypto"
	"LCM/internal/netfilter"
	"LCM/internal/storage/repositories"
)

// addressApp ist die Test-App mit einem Port aus der Umgebung (UI 9500, gesperrt).
func addressApp(t *testing.T) (do func(method, path, body string) map[string]any, status func() int) {
	t.Helper()
	app, _ := buildTestAppDB(t, 0, nil, func(d *router.Deps, db *gorm.DB) {
		cipher, err := crypto.NewCipher(crypto.GenerateKey())
		if err != nil {
			t.Fatal(err)
		}
		audit := services.NewAuditService(repositories.NewAuditRepository(db))
		d.Settings = services.NewSettingsService(repositories.NewSettingsRepository(db), cipher, audit, nil).
			WithAddressConfig(services.AddressConfig{
				Scheme: "https", FallbackHost: "localhost", UI: services.FixedPort{Set: true, Value: 9500},
			})
	})
	token := loginToken(t, app, "admin", adminPassword)
	var last int
	do = func(method, path, body string) map[string]any {
		resp := doRequest(t, app, method, path, token, body)
		last = resp.StatusCode
		return decodeMap(t, resp)
	}
	return do, func() int { return last }
}

// GET und PATCH tragen Modus, Ports, address_locks und effective_urls.
func TestEinstellungenAdressmodusJSON(t *testing.T) {
	do, status := addressApp(t)

	got := do("GET", "/api/v1/settings/global", "")
	if got["address_mode"] != "separate" || got["ui_port"] != float64(9500) || got["agent_port"] != float64(9320) ||
		got["mcp_port"] != float64(9330) || got["shared_tls_port"] != float64(443) {
		t.Errorf("Modus und Ports: %v", got)
	}
	locks, _ := got["address_locks"].(map[string]any)
	if locks["ui_port"] != true || locks["agent_port"] != false || locks["mcp_port"] != false || locks["shared_tls_port"] != false {
		t.Errorf("address_locks = %v", locks)
	}
	urls, _ := got["effective_urls"].(map[string]any)
	if urls["ui"] != "https://localhost:9500" || urls["agent"] != "https://localhost:9320" {
		t.Errorf("effective_urls = %v", urls)
	}

	got = do("PATCH", "/api/v1/settings/global",
		`{"address_mode":"one_name","public_host":"lcm.example.org","agent_port":9321,"mcp_port":9331}`)
	if status() != 200 {
		t.Fatalf("PATCH: %d %v", status(), got)
	}
	urls, _ = got["effective_urls"].(map[string]any)
	if urls["agent"] != "https://lcm.example.org:9321" || urls["mcp"] != "https://lcm.example.org:9331" ||
		urls["ui"] != "https://lcm.example.org:9500" {
		t.Errorf("effective_urls nach PATCH = %v", urls)
	}
	if _, has := got["ui_url"]; has {
		t.Errorf("ui_url steht, obwohl der UI-Port gleich blieb: %v", got["ui_url"])
	}
}

func TestEinstellungenAdressmodusAblehnungen(t *testing.T) {
	do, status := addressApp(t)
	for name, body := range map[string]string{
		"gesperrter UI-Port":    `{"ui_port":9501}`,
		"Port doppelt":          `{"agent_port":9330}`,
		"Port ausser Bereich":   `{"agent_port":70000}`,
		"Modus B ohne Namen":    `{"address_mode":"one_name"}`,
		"Name mit Port":         `{"address_mode":"one_name","public_host":"lcm.example.org:9310"}`,
		"unbekannter Modus":     `{"address_mode":"alles"}`,
		"Modus C ohne Namen":    `{"address_mode":"shared_port"}`,
		"Agent auf dem UI-Port": `{"ui_port":9500,"agent_port":9500}`,
	} {
		got := do("PATCH", "/api/v1/settings/global", body)
		if status() < 400 || status() >= 500 {
			t.Errorf("%s: %d %v, erwartet 4xx", name, status(), got)
		}
	}
	// Derselbe Wert wie gezeigt ist bei einem gesperrten Port kein Fehler.
	do("PATCH", "/api/v1/settings/global", `{"ui_port":9500}`)
	if status() != 200 {
		t.Errorf("gesperrter Port mit gezeigtem Wert: %d", status())
	}
}

// Der Schritt des Einrichtungsassistenten: /auth/me nennt den Stand, der PATCH
// setzt ihn vor und zurück zwischen session und subscription oder auf leer.
func TestEinstellungenEinrichtungsschritt(t *testing.T) {
	app, db := buildTestAppDB(t, 0, nil, func(*router.Deps, *gorm.DB) {})
	if err := db.Model(&domain.GlobalSettings{}).Where("id = ?", 1).Update("setup_step", domain.SetupStepAddresses).Error; err != nil {
		t.Fatal(err)
	}
	token := loginToken(t, app, "admin", adminPassword)
	call := func(method, path, body string) (int, map[string]any) {
		resp := doRequest(t, app, method, path, token, body)
		return resp.StatusCode, decodeMap(t, resp)
	}

	if _, me := call("GET", "/api/v1/auth/me", ""); me["setup_step"] != "addresses" {
		t.Errorf("auth/me: setup_step = %v, erwartet addresses", me["setup_step"])
	}
	if _, got := call("GET", "/api/v1/settings/global", ""); got["setup_step"] != "addresses" {
		t.Errorf("GET /settings/global: setup_step = %v", got["setup_step"])
	}
	status, got := call("PATCH", "/api/v1/settings/global", `{"setup_step":"certificate"}`)
	if status != 200 || got["setup_step"] != "certificate" {
		t.Fatalf("vorwärts: %d %v", status, got)
	}
	if status, got = call("PATCH", "/api/v1/settings/global", `{"setup_step":"session"}`); status != 200 || got["setup_step"] != "session" {
		t.Fatalf("zurück: %d %v", status, got)
	}
	call("PATCH", "/api/v1/settings/global", `{"setup_step":"certificate"}`)
	for _, bad := range []string{`"alles"`, `"open"`} {
		if status, got := call("PATCH", "/api/v1/settings/global", `{"setup_step":`+bad+`}`); status < 400 || status >= 500 {
			t.Errorf("setup_step=%s: %d %v, erwartet 4xx", bad, status, got)
		}
	}
	if _, me := call("GET", "/api/v1/auth/me", ""); me["setup_step"] != "certificate" {
		t.Errorf("auth/me: %v", me["setup_step"])
	}
	status, got = call("PATCH", "/api/v1/settings/global", `{"setup_step":""}`)
	if status != 200 || got["setup_step"] != "" {
		t.Fatalf("erledigt: %d %v", status, got)
	}
	if _, me := call("GET", "/api/v1/auth/me", ""); me["setup_step"] != nil {
		t.Errorf("auth/me nach dem Erledigen: %v, erwartet kein Feld", me["setup_step"])
	}
	if status, _ := call("PATCH", "/api/v1/settings/global", `{"setup_step":"addresses"}`); status < 400 || status >= 500 {
		t.Errorf("erledigten Assistenten wieder öffnen: %d, erwartet 4xx", status)
	}
}

// proxyTrustApp ist die Test-App mit Vertrauensregel und Netzbereichen. Der
// Test-Harness meldet 0.0.0.0 als Gegenstelle.
func proxyTrustApp(t *testing.T) (app *fiber.App, token string) {
	t.Helper()
	app, _ = buildTestAppDB(t, 0, nil, func(d *router.Deps, db *gorm.DB) {
		cipher, err := crypto.NewCipher(crypto.GenerateKey())
		if err != nil {
			t.Fatal(err)
		}
		audit := services.NewAuditService(repositories.NewAuditRepository(db))
		d.ProxyTrust = netfilter.NewProxyTrustHolder(netfilter.ProxyTrust{})
		settings, err := services.NewSettingsService(repositories.NewSettingsRepository(db), cipher, audit, nil).
			WithProxyTrust(d.ProxyTrust)
		if err != nil {
			t.Fatal(err)
		}
		d.Settings = settings
		d.Network, err = services.NewNetworkAccessService(repositories.NewSettingsRepository(db), audit)
		if err != nil {
			t.Fatal(err)
		}
	})
	return app, loginToken(t, app, "admin", adminPassword)
}

// trust_proxy_header und trusted_proxies wirken ohne Neustart: X-Forwarded-For
// zählt erst, wenn die Gegenstelle als Proxy eingetragen ist, und nicht mehr,
// wenn man den Schalter ausschaltet. Ungültiges lehnt der PATCH ab.
func TestEinstellungenProxyVertrauenWirktSofort(t *testing.T) {
	app, token := proxyTrustApp(t)
	clientSeenWith := func(xff string) string {
		resp := doRequestWithHeaders(t, app, "GET", "/api/v1/system/network", token, "", map[string]string{"X-Forwarded-For": xff})
		got, _ := decodeMap(t, resp)["client_address"].(string)
		return got
	}
	patch := func(body string) (int, map[string]any) {
		resp := doRequest(t, app, "PATCH", "/api/v1/settings/global", token, body)
		return resp.StatusCode, decodeMap(t, resp)
	}

	if got := clientSeenWith("203.0.113.7"); got != "0.0.0.0" {
		t.Fatalf("ohne Vertrauen: %q, erwartet die Adresse der Verbindung", got)
	}
	status, got := patch(`{"trust_proxy_header":true,"trusted_proxies":["192.0.2.1"]}`)
	if status != 200 || got["trust_proxy_header"] != true {
		t.Fatalf("Proxy eintragen: %d %v", status, got)
	}
	if proxies, _ := got["trusted_proxies"].([]any); len(proxies) != 1 || proxies[0] != "192.0.2.1/32" {
		t.Errorf("trusted_proxies = %v", got["trusted_proxies"])
	}
	if got := clientSeenWith("203.0.113.7"); got != "0.0.0.0" {
		t.Errorf("fremde Gegenstelle mit X-Forwarded-For: %q, erwartet die Adresse der Verbindung", got)
	}

	if status, got := patch(`{"trusted_proxies":["0.0.0.0/32"]}`); status != 200 {
		t.Fatalf("Gegenstelle als Proxy: %d %v", status, got)
	}
	if got := clientSeenWith("203.0.113.7"); got != "203.0.113.7" {
		t.Errorf("vertrauter Proxy: %q, erwartet die weitergeleitete Adresse", got)
	}

	for name, body := range map[string]string{
		"alle Adressen":             `{"trusted_proxies":["0.0.0.0/0"]}`,
		"alle Adressen IPv6":        `{"trusted_proxies":["::/0"]}`,
		"ungültiger Eintrag":        `{"trusted_proxies":["nicht-ip"]}`,
		"Liste leeren bei Schalter": `{"trusted_proxies":[]}`,
	} {
		if status, got := patch(body); status != 400 {
			t.Errorf("%s: %d %v, erwartet 400", name, status, got)
		}
	}
	if got := clientSeenWith("203.0.113.7"); got != "203.0.113.7" {
		t.Errorf("eine abgelehnte Änderung wirkte: %q", got)
	}

	if status, _ := patch(`{"trust_proxy_header":false}`); status != 200 {
		t.Fatalf("ausschalten: %d", status)
	}
	if got := clientSeenWith("203.0.113.7"); got != "0.0.0.0" {
		t.Errorf("nach dem Ausschalten: %q, erwartet die Adresse der Verbindung", got)
	}
}
