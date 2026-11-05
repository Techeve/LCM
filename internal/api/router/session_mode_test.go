package router_test

import (
	"fmt"
	"testing"
)

const httpsHeader = "X-Forwarded-Proto"

// TestSitzungswegStandardUndAnzeige: Ohne Wert gilt der Arbeitsspeicher; die
// Einstellungen melden, ob diese Anfrage über HTTPS kam.
func TestSitzungswegStandardUndAnzeige(t *testing.T) {
	app, _ := buildTestApp(t, 0, nil, trustAllProxies(t))
	token := loginToken(t, app, "admin", adminPassword)

	settings := decodeMap(t, doRequest(t, app, "GET", "/api/v1/settings/global", token, ""))
	if mode := settings["browser_session_mode"]; mode != "" {
		t.Errorf("browser_session_mode = %v, erwartet leer (heißt memory)", mode)
	}
	if settings["browser_session_mode_confirmed"] != false {
		t.Errorf("browser_session_mode_confirmed = %v, erwartet false", settings["browser_session_mode_confirmed"])
	}
	if settings["browser_session_cookie_available"] != false {
		t.Errorf("über HTTP: browser_session_cookie_available = %v, erwartet false", settings["browser_session_cookie_available"])
	}
	viaHTTPS := decodeMap(t, doRequestWithHeaders(t, app, "GET", "/api/v1/settings/global", token, "", map[string]string{httpsHeader: "https"}))
	if viaHTTPS["browser_session_cookie_available"] != true {
		t.Errorf("über HTTPS: browser_session_cookie_available = %v, erwartet true", viaHTTPS["browser_session_cookie_available"])
	}
}

// TestSitzungswegStrengValidiert: nur memory oder cookie, und cookie nur über HTTPS.
func TestSitzungswegStrengValidiert(t *testing.T) {
	app, _ := buildTestApp(t, 0, nil, trustAllProxies(t))
	token := loginToken(t, app, "admin", adminPassword)
	https := map[string]string{httpsHeader: "https"}

	for _, mode := range []string{"", "Cookie", "token", "memory ", "header"} {
		resp := doRequestWithHeaders(t, app, "PATCH", "/api/v1/settings/global", token, fmt.Sprintf(`{"browser_session_mode":%q}`, mode), https)
		if resp.StatusCode != 422 {
			t.Errorf("%q: erwartet 422, bekam %d", mode, resp.StatusCode)
			continue
		}
		assertErrorCode(t, resp, "settings.browser_session_mode_invalid")
	}

	resp := doRequest(t, app, "PATCH", "/api/v1/settings/global", token, `{"browser_session_mode":"cookie"}`)
	if resp.StatusCode != 422 {
		t.Fatalf("cookie über HTTP: erwartet 422, bekam %d", resp.StatusCode)
	}
	assertErrorCode(t, resp, "settings.session_cookie_needs_https")
	// Abgelehnt heißt: nichts geändert, die Sitzung lebt.
	if r := doRequest(t, app, "GET", "/api/v1/auth/me", token, ""); r.StatusCode != 200 {
		t.Errorf("Sitzung nach abgelehntem Wechsel: %d", r.StatusCode)
	}

	// memory geht über HTTP.
	if r := doRequest(t, app, "PATCH", "/api/v1/settings/global", token, `{"browser_session_mode":"memory"}`); r.StatusCode != 200 {
		t.Errorf("memory über HTTP: %d", r.StatusCode)
	}
}

// TestSitzungswegWechselBeendetAlleSitzungen: auch die eigene und die anderer
// Benutzer; bleibt der Weg gleich (nur "bestätigt"), bleibt die Sitzung.
func TestSitzungswegWechselBeendetAlleSitzungen(t *testing.T) {
	app, _ := buildTestApp(t, 0, nil, trustAllProxies(t))
	admin := loginToken(t, app, "admin", adminPassword)
	const pw = "Anker5-Leuchtturm!Wind"
	if r := doRequest(t, app, "POST", "/api/v1/users", admin, `{"username":"zweiter","password":"`+pw+`","roles":["manager"]}`); r.StatusCode != 201 {
		t.Fatalf("user anlegen: %d", r.StatusCode)
	}
	other := loginToken(t, app, "zweiter", pw)
	https := map[string]string{httpsHeader: "https"}

	// Gleicher Weg, nur bestätigt: nichts endet.
	if r := doRequest(t, app, "PATCH", "/api/v1/settings/global", admin, `{"browser_session_mode":"memory","browser_session_mode_confirmed":true}`); r.StatusCode != 200 {
		t.Fatalf("bestätigen: %d", r.StatusCode)
	}
	for name, token := range map[string]string{"admin": admin, "zweiter": other} {
		if r := doRequest(t, app, "GET", "/api/v1/auth/me", token, ""); r.StatusCode != 200 {
			t.Errorf("%s nach gleichem Weg: %d", name, r.StatusCode)
		}
	}

	// Wechsel: alle Sitzungen enden, auch die des Aufrufers.
	if r := doRequestWithHeaders(t, app, "PATCH", "/api/v1/settings/global", admin, `{"browser_session_mode":"cookie"}`, https); r.StatusCode != 200 {
		t.Fatalf("Wechsel: %d", r.StatusCode)
	}
	for name, token := range map[string]string{"admin": admin, "zweiter": other} {
		if r := doRequest(t, app, "GET", "/api/v1/auth/me", token, ""); r.StatusCode != 401 {
			t.Errorf("%s nach dem Wechsel: erwartet 401, bekam %d", name, r.StatusCode)
		}
	}
	// Neu anmelden geht, und der Wert ist gespeichert.
	b := cookieBrowser(t, app)
	b.login(nil)
	settings := decodeMap(t, b.do("GET", "/api/v1/settings/global", "", nil))
	if settings["browser_session_mode"] != "cookie" {
		t.Errorf("browser_session_mode = %v, erwartet cookie", settings["browser_session_mode"])
	}
}

// TestKonsoleBrauchtDieSitzungKopfzeile: Die Fahrkarte der Web-Konsole wird mit
// der Kopfzeilen-Sitzung geholt (der Browser setzt sie an den fetch).
func TestKonsoleFahrkarteMitKopfzeilenSitzung(t *testing.T) {
	app, db := buildTestApp(t, 0, nil)
	token := loginToken(t, app, "admin", adminPassword)
	id := addCapabilityServer(t, db, "konsole", "apt")

	resp := doRequest(t, app, "POST", fmt.Sprintf("/api/v1/servers/%d/terminal/ticket", id), token, "")
	if resp.StatusCode != 200 {
		t.Fatalf("Konsolen-Fahrkarte: %d", resp.StatusCode)
	}
	if ticket, _ := decodeMap(t, resp)["ticket"].(string); ticket == "" {
		t.Error("Antwort ohne ticket")
	}
}
