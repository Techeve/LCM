package router_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"

	"LCM/internal/api/router"
	"LCM/internal/infrastructure/totp"
	"LCM/internal/netfilter"
	"LCM/internal/storage/repositories"
)

const adminPassword = "test-admin-passwort"

// fakeClock ist die einstellbare Uhr der Sitzungen.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock { return &fakeClock{t: time.Now()} }

func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.t
}

func (f *fakeClock) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.t = f.t.Add(d)
}

// browser ist ein Cookie-Behälter: sammelt die Set-Cookie-Antworten und
// schickt sie wie ein Browser zurück. Den session_token einer Login-Antwort
// hält er wie die Oberfläche im Speicher und schickt ihn als X-LCM-Session.
type browser struct {
	t       *testing.T
	app     *fiber.App
	cookies map[string]*http.Cookie
	headers map[string]string
	// https lässt jede Anfrage so aussehen, als käme sie über einen vertrauten
	// Proxy per HTTPS (siehe cookieApp).
	https bool
}

func newBrowser(t *testing.T, app *fiber.App) *browser {
	return &browser{t: t, app: app, cookies: map[string]*http.Cookie{}, headers: map[string]string{}}
}

func (b *browser) do(method, path, body string, extra map[string]string) *http.Response {
	b.t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, c := range b.cookies {
		req.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
	}
	if b.https {
		req.Header.Set("X-Forwarded-Proto", "https")
	}
	for k, v := range b.headers {
		req.Header.Set(k, v)
	}
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	resp, err := b.app.Test(req, testRequestConfig())
	if err != nil {
		b.t.Fatal(err)
	}
	for _, c := range resp.Cookies() {
		if c.MaxAge < 0 {
			delete(b.cookies, c.Name)
		} else {
			b.cookies[c.Name] = c
		}
	}
	return resp
}

// withCSRF schickt das CSRF-Token wie die Oberfläche: aus dem lesbaren Cookie.
func (b *browser) withCSRF() map[string]string {
	return map[string]string{"X-CSRF-Token": b.cookies[csrfCookie].Value}
}

// loginAs meldet an und übernimmt einen session_token als Kopfzeile.
func (b *browser) loginAs(username, password string, extraHeaders map[string]string) (*http.Response, map[string]any) {
	b.t.Helper()
	resp := b.do("POST", "/api/v1/auth/login", `{"username":"`+username+`","password":"`+password+`"}`, extraHeaders)
	if resp.StatusCode != 200 {
		b.t.Fatalf("login: status %d", resp.StatusCode)
	}
	body := decodeMap(b.t, resp)
	b.adoptSession(body)
	return resp, body
}

func (b *browser) login(extraHeaders map[string]string) (*http.Response, map[string]any) {
	b.t.Helper()
	return b.loginAs("admin", adminPassword, extraHeaders)
}

func (b *browser) adoptSession(body map[string]any) {
	if token, _ := body["session_token"].(string); token != "" {
		b.headers[sessionHeader] = token
	}
}

const (
	sessionCookie = "__Host-lcm_session"
	csrfCookie    = "__Host-lcm_csrf"
	sessionHeader = "X-LCM-Session"
)

// trustAllProxies lässt X-Forwarded-Proto von jeder Gegenstelle gelten: Die
// Testanfragen haben keine echte Peer-Adresse eines Proxys.
func trustAllProxies(t *testing.T) func(*router.Deps) {
	t.Helper()
	all, err := netfilter.Parse([]string{"0.0.0.0/0", "::/0"})
	if err != nil {
		t.Fatal(err)
	}
	return func(d *router.Deps) { d.ProxyTrust = netfilter.ProxyTrust{Enabled: true, Proxies: all} }
}

// cookieApp ist eine App, deren Betreiber die Cookie-Variante gewählt hat. Die
// Browser dazu (cookieBrowser) kommen "über HTTPS".
func cookieApp(t *testing.T, opts ...func(*router.Deps)) *fiber.App {
	t.Helper()
	app, _ := buildTestApp(t, 0, nil, append([]func(*router.Deps){trustAllProxies(t)}, opts...)...)
	setSessionMode(t, app, "cookie", true)
	return app
}

func cookieBrowser(t *testing.T, app *fiber.App) *browser {
	b := newBrowser(t, app)
	b.https = true
	return b
}

// setSessionMode stellt den Sitzungsweg über die API ein (als Admin, per
// Kopfzeilen-Sitzung) und prüft, dass es geklappt hat. Das beendet alle
// Sitzungen, auch die des Aufrufs.
func setSessionMode(t *testing.T, app *fiber.App, mode string, https bool) {
	t.Helper()
	resp := patchSessionMode(t, app, mode, https)
	if resp.StatusCode != 200 {
		t.Fatalf("browser_session_mode=%s: status %d", mode, resp.StatusCode)
	}
}

func patchSessionMode(t *testing.T, app *fiber.App, mode string, https bool) *http.Response {
	t.Helper()
	b := newBrowser(t, app)
	b.https = https
	b.login(nil)
	var extra map[string]string
	if b.cookies[csrfCookie] != nil {
		extra = b.withCSRF()
	}
	return b.do("PATCH", "/api/v1/settings/global", `{"browser_session_mode":"`+mode+`","browser_session_mode_confirmed":true}`, extra)
}

func decodeMap(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func cookieByName(resp *http.Response, name string) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// TestMemoryLoginLiefertSessionTokenOhneCookie: Die Standard-Variante setzt
// keinerlei Cookie; der Sitzungswert steht im Antwortrumpf und gilt als
// X-LCM-Session.
func TestMemoryLoginLiefertSessionTokenOhneCookie(t *testing.T) {
	app, _ := buildTestApp(t, 0, nil)
	b := newBrowser(t, app)
	resp, body := b.login(nil)

	if got := resp.Header.Values("Set-Cookie"); len(got) != 0 {
		t.Errorf("Set-Cookie in der Arbeitsspeicher-Variante: %v", got)
	}
	token, _ := body["session_token"].(string)
	if token == "" {
		t.Fatal("session_token fehlt")
	}
	for _, key := range []string{"token", "csrf_token"} {
		if _, has := body[key]; has {
			t.Errorf("Login-Antwort enthält %q", key)
		}
	}

	// Gilt über die Kopfzeile, auch für ändernde Anfragen ohne CSRF-Token.
	me := b.do("GET", "/api/v1/auth/me", "", nil)
	if me.StatusCode != 200 {
		t.Fatalf("/auth/me per X-LCM-Session: %d", me.StatusCode)
	}
	meBody := decodeMap(t, me)
	if meBody["session_mode"] != "memory" {
		t.Errorf("session_mode = %v, erwartet memory", meBody["session_mode"])
	}
	if _, has := meBody["csrf_token"]; has {
		t.Error("csrf_token in der Arbeitsspeicher-Variante")
	}
	if r := b.do("PATCH", "/api/v1/auth/me/preferences", `{"language":"en"}`, nil); r.StatusCode != 200 {
		t.Errorf("ändernde Anfrage ohne CSRF-Token: %d", r.StatusCode)
	}
	if got := me.Header.Values("Set-Cookie"); len(got) != 0 {
		t.Errorf("Set-Cookie bei einer Anfrage: %v", got)
	}
}

// TestBearerGiltNichtAlsAnmeldung: Authorization: Bearer ist an der REST-API keine
// Anmeldung - weder mit dem Sitzungswert noch mit einem JWT (der 2FA-Challenge).
func TestBearerGiltNichtAlsAnmeldung(t *testing.T) {
	app, _ := buildTestApp(t, 0, nil)
	sessionValue := loginToken(t, app, "admin", adminPassword)
	enable2FA(t, app)
	step := newBrowser(t, app).do("POST", "/api/v1/auth/login", `{"username":"admin","password":"`+adminPassword+`"}`, nil)
	challenge, _ := decodeMap(t, step)["challenge"].(string)
	if challenge == "" {
		t.Fatal("keine Challenge")
	}

	for name, token := range map[string]string{"Challenge-JWT": challenge, "Sitzungswert": sessionValue} {
		b := newBrowser(t, app)
		for _, path := range []string{"/api/v1/auth/me", "/api/v1/servers"} {
			if r := b.do("GET", path, "", map[string]string{"Authorization": "Bearer " + token}); r.StatusCode != 401 {
				t.Errorf("%s als Bearer auf %s: erwartet 401, bekam %d", name, path, r.StatusCode)
			}
		}
	}
	// Der Sitzungswert selbst gilt weiter, über seine Kopfzeile.
	if r := doRequest(t, app, "GET", "/api/v1/auth/me", sessionValue, ""); r.StatusCode != 200 {
		t.Errorf("Sitzungswert als X-LCM-Session: %d", r.StatusCode)
	}
}

func TestMemoryLoginRaeumtRestCookiesAb(t *testing.T) {
	app, _ := buildTestApp(t, 0, nil)

	// Ohne Rest-Cookie: keine einzige Set-Cookie-Zeile.
	if resp, _ := newBrowser(t, app).login(nil); len(resp.Header.Values("Set-Cookie")) != 0 {
		t.Errorf("Set-Cookie ohne Rest-Cookie: %v", resp.Header.Values("Set-Cookie"))
	}

	// Mit Rest-Cookies einer früheren Cookie-Sitzung: sie werden gelöscht.
	b := newBrowser(t, app)
	b.cookies[sessionCookie] = &http.Cookie{Name: sessionCookie, Value: "alt"}
	b.cookies[csrfCookie] = &http.Cookie{Name: csrfCookie, Value: "alt"}
	resp, _ := b.login(nil)
	for _, name := range []string{sessionCookie, csrfCookie} {
		if c := cookieByName(resp, name); c == nil || c.MaxAge >= 0 || c.Value != "" {
			t.Errorf("Rest-Cookie %s nicht gelöscht: %+v", name, c)
		}
	}
	if got := len(resp.Header.Values("Set-Cookie")); got != 2 {
		t.Errorf("%d Set-Cookie-Zeilen, erwartet genau die zwei Löschungen", got)
	}
}

// TestCookieLoginSetztSitzungUndCSRF: Die Cookie-Variante setzt __Host-Cookies mit
// den vorgeschriebenen Attributen; der Sitzungswert steht NICHT im Antwortrumpf.
func TestCookieLoginSetztSitzungUndCSRF(t *testing.T) {
	app := cookieApp(t)
	b := cookieBrowser(t, app)
	resp, body := b.login(nil)

	sess, csrf := cookieByName(resp, sessionCookie), cookieByName(resp, csrfCookie)
	if sess == nil || csrf == nil {
		t.Fatalf("Cookies fehlen: session=%v csrf=%v", sess, csrf)
	}
	if !sess.HttpOnly || !sess.Secure || sess.SameSite != http.SameSiteStrictMode || sess.Path != "/" || sess.Domain != "" {
		t.Errorf("%s: HttpOnly=%v Secure=%v SameSite=%v Path=%q Domain=%q", sessionCookie, sess.HttpOnly, sess.Secure, sess.SameSite, sess.Path, sess.Domain)
	}
	if csrf.HttpOnly || !csrf.Secure || csrf.SameSite != http.SameSiteStrictMode || csrf.Path != "/" || csrf.Domain != "" {
		t.Errorf("%s: HttpOnly=%v Secure=%v SameSite=%v Path=%q Domain=%q", csrfCookie, csrf.HttpOnly, csrf.Secure, csrf.SameSite, csrf.Path, csrf.Domain)
	}
	if _, has := body["session_token"]; has {
		t.Error("session_token in der Cookie-Variante")
	}
	if body["csrf_token"] != csrf.Value {
		t.Errorf("csrf_token der Antwort %v != Cookie %q", body["csrf_token"], csrf.Value)
	}
	if sess.Value == csrf.Value || strings.Contains(csrf.Value, sess.Value) {
		t.Error("das CSRF-Token darf den Sitzungswert nicht verraten")
	}

	me := b.do("GET", "/api/v1/auth/me", "", nil)
	if me.StatusCode != 200 {
		t.Fatalf("/auth/me per Cookie: %d", me.StatusCode)
	}
	meBody := decodeMap(t, me)
	if meBody["csrf_token"] != csrf.Value || meBody["session_mode"] != "cookie" {
		t.Errorf("/auth/me: csrf_token=%v session_mode=%v", meBody["csrf_token"], meBody["session_mode"])
	}
}

// TestCookieLoginUeberHTTPFaelltAufMemoryZurueck: Steht die Einstellung auf
// "cookie", der Login kommt aber über reines HTTP (HTTPS wurde abgebaut), sperrt
// das niemanden aus: Die Sitzung läuft als Arbeitsspeicher-Sitzung, ohne Cookie.
func TestCookieLoginUeberHTTPFaelltAufMemoryZurueck(t *testing.T) {
	app := cookieApp(t)
	b := newBrowser(t, app) // ohne X-Forwarded-Proto
	resp, body := b.login(nil)
	if len(resp.Header.Values("Set-Cookie")) != 0 {
		t.Errorf("Set-Cookie über HTTP: %v", resp.Header.Values("Set-Cookie"))
	}
	if token, _ := body["session_token"].(string); token == "" {
		t.Fatal("kein session_token im Rückfall")
	}
	if r := b.do("GET", "/api/v1/auth/me", "", nil); r.StatusCode != 200 {
		t.Errorf("Anfrage mit dem Rückfall-Token: %d", r.StatusCode)
	}
}

// TestCookieNurUeberVertrautenHTTPSProxy: "HTTPS" gilt nur bei echtem TLS oder
// X-Forwarded-Proto von einem vertrauten Proxy. Die Kopfzeile allein reicht nicht.
func TestCookieNurUeberVertrautenHTTPSProxy(t *testing.T) {
	app, _ := buildTestApp(t, 0, nil, trustAllProxies(t))
	setSessionMode(t, app, "cookie", true)

	untrusted, _ := buildTestApp(t, 0, nil) // kein Proxy-Vertrauen
	if r := patchSessionMode(t, untrusted, "cookie", true); r.StatusCode != 422 {
		t.Errorf("Cookie-Modus mit Kopfzeile ohne vertrauten Proxy: erwartet 422, bekam %d", r.StatusCode)
	}
}

func enable2FA(t *testing.T, app *fiber.App) string {
	t.Helper()
	token := loginToken(t, app, "admin", adminPassword)
	r := doRequest(t, app, "POST", "/api/v1/auth/2fa/setup", token, "")
	var setup struct {
		Secret string `json:"secret"`
	}
	if err := json.NewDecoder(r.Body).Decode(&setup); err != nil {
		t.Fatal(err)
	}
	code, err := totp.Code(setup.Secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if r := doRequest(t, app, "POST", "/api/v1/auth/2fa/enable", token, `{"code":"`+code+`"}`); r.StatusCode != 200 {
		t.Fatalf("2fa enable: %d", r.StatusCode)
	}
	return setup.Secret
}

// TestLoginMitZweitfaktor: Nach dem Passwort gibt es noch KEINE Sitzung, erst der
// zweite Schritt liefert den session_token.
func TestLoginMitZweitfaktor(t *testing.T) {
	app, _ := buildTestApp(t, 0, nil)
	secret := enable2FA(t, app)
	b := newBrowser(t, app)

	step := b.do("POST", "/api/v1/auth/login", `{"username":"admin","password":"`+adminPassword+`"}`, nil)
	stepBody := decodeMap(t, step)
	challenge, _ := stepBody["challenge"].(string)
	if _, has := stepBody["session_token"]; challenge == "" || has || len(b.cookies) != 0 {
		t.Fatalf("nach dem Passwort: challenge=%q, session_token=%v, Cookies=%d (erwartet keine Sitzung)", challenge, has, len(b.cookies))
	}
	if r := b.do("GET", "/api/v1/auth/me", "", nil); r.StatusCode != 401 {
		t.Errorf("Challenge ist keine Sitzung: erwartet 401, bekam %d", r.StatusCode)
	}

	// Der Code der Aktivierung ist verbraucht, die Nachbarperiode noch frei.
	code, err := totp.Code(secret, time.Now().Add(30*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	resp := b.do("POST", "/api/v1/auth/login/2fa", `{"challenge":"`+challenge+`","code":"`+code+`"}`, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("2. Schritt: %d", resp.StatusCode)
	}
	if len(resp.Header.Values("Set-Cookie")) != 0 {
		t.Errorf("2. Schritt setzt Cookies: %v", resp.Header.Values("Set-Cookie"))
	}
	body := decodeMap(t, resp)
	b.adoptSession(body)
	if token, _ := body["session_token"].(string); token == "" {
		t.Fatal("2. Schritt liefert keinen session_token")
	}
	if r := b.do("GET", "/api/v1/auth/me", "", nil); r.StatusCode != 200 {
		t.Errorf("/auth/me nach 2FA-Login: %d", r.StatusCode)
	}

	// Gleich noch einmal anmelden: Die App zeigt denselben Code, und der gilt nur
	// einmal. Die Antwort sagt das, statt ihn falsch zu nennen.
	time.Sleep(1100 * time.Millisecond) // sonst gleicht die Challenge bis aufs Zeichen der verbrauchten
	again := newBrowser(t, app)
	step2 := decodeMap(t, again.do("POST", "/api/v1/auth/login", `{"username":"admin","password":"`+adminPassword+`"}`, nil))
	retry, _ := step2["challenge"].(string)
	reused := again.do("POST", "/api/v1/auth/login/2fa", `{"challenge":"`+retry+`","code":"`+code+`"}`, nil)
	if reused.StatusCode != 401 {
		t.Fatalf("zweite Verwendung: %d", reused.StatusCode)
	}
	if got := decodeMap(t, reused)["code"]; got != "auth.totp_reused" {
		t.Errorf("Fehlercode = %v", got)
	}
}

func TestCSRFBeiAendernderCookieAnfrage(t *testing.T) {
	app := cookieApp(t)
	b := cookieBrowser(t, app)
	b.login(nil)
	const prefs = `{"language":"en"}`

	cases := []struct {
		name   string
		header map[string]string
		want   int
	}{
		{"ohne Token", nil, 403},
		{"leeres Token", map[string]string{"X-CSRF-Token": ""}, 403},
		{"falsches Token", map[string]string{"X-CSRF-Token": "falsch"}, 403},
		{"Token einer anderen Sitzung", map[string]string{"X-CSRF-Token": otherSessionCSRF(t, app)}, 403},
		{"richtiges Token", b.withCSRF(), 200},
	}
	for _, tc := range cases {
		resp := b.do("PATCH", "/api/v1/auth/me/preferences", prefs, tc.header)
		if resp.StatusCode != tc.want {
			t.Errorf("%s: erwartet %d, bekam %d", tc.name, tc.want, resp.StatusCode)
		}
		if tc.want == 403 {
			if code := decodeMap(t, resp)["code"]; code != "auth.csrf_invalid" {
				t.Errorf("%s: code = %v, erwartet auth.csrf_invalid", tc.name, code)
			}
		}
	}
	// Lesende Anfragen brauchen kein Token.
	if r := b.do("GET", "/api/v1/auth/me", "", nil); r.StatusCode != 200 {
		t.Errorf("GET ohne Token: %d", r.StatusCode)
	}
}

func otherSessionCSRF(t *testing.T, app *fiber.App) string {
	t.Helper()
	other := cookieBrowser(t, app)
	other.login(nil)
	return other.cookies[csrfCookie].Value
}

// TestCSRFNichtFuerKopfzeilenSitzungUndAPIKey: Was der Browser nicht von selbst
// mitschickt, braucht kein CSRF-Token.
func TestCSRFNichtFuerKopfzeilenSitzungUndAPIKey(t *testing.T) {
	app := cookieApp(t)
	token := loginToken(t, app, "admin", adminPassword) // über HTTP: Rückfall, Kopfzeilen-Sitzung

	if r := doRequest(t, app, "PATCH", "/api/v1/auth/me/preferences", token, `{"language":"en"}`); r.StatusCode != 200 {
		t.Errorf("Kopfzeilen-Sitzung: erwartet 200, bekam %d", r.StatusCode)
	}
	created := doRequest(t, app, "POST", "/api/v1/apikeys", token, `{"name":"csrf-test"}`)
	if created.StatusCode != 201 {
		t.Fatalf("apikey anlegen: %d", created.StatusCode)
	}
	key, _ := decodeMap(t, created)["key"].(string)
	b := cookieBrowser(t, app)
	if r := b.do("PATCH", "/api/v1/auth/me/preferences", `{"language":"de"}`, map[string]string{"X-API-Key": key}); r.StatusCode != 200 {
		t.Errorf("API-Key: erwartet 200, bekam %d", r.StatusCode)
	}
	// Eine Kopfzeilen-Sitzung neben einem (fremden) Sitzungs-Cookie bleibt Kopfzeile.
	b.login(nil)
	resp := b.do("PATCH", "/api/v1/auth/me/preferences", `{"language":"en"}`, map[string]string{sessionHeader: token})
	if resp.StatusCode != 200 {
		t.Errorf("Kopfzeile mit Cookie daneben: erwartet 200, bekam %d", resp.StatusCode)
	}
}

// TestSitzungGiltNurUeberIhrenTransport: Ein Cookie, das zu einer Kopfzeilen-
// Sitzung gehört, ist keine Anmeldung - und umgekehrt.
func TestSitzungGiltNurUeberIhrenTransport(t *testing.T) {
	app := cookieApp(t)

	cookieB := cookieBrowser(t, app)
	cookieB.login(nil)
	cookieValue := cookieB.cookies[sessionCookie].Value
	headerValue := loginToken(t, app, "admin", adminPassword) // HTTP: Kopfzeilen-Sitzung

	asHeader := newBrowser(t, app)
	if r := asHeader.do("GET", "/api/v1/auth/me", "", map[string]string{sessionHeader: cookieValue}); r.StatusCode != 401 {
		t.Errorf("Cookie-Sitzung als Kopfzeile: erwartet 401, bekam %d", r.StatusCode)
	}
	asCookie := cookieBrowser(t, app)
	asCookie.cookies[sessionCookie] = &http.Cookie{Name: sessionCookie, Value: headerValue}
	if r := asCookie.do("GET", "/api/v1/auth/me", "", nil); r.StatusCode != 401 {
		t.Errorf("Kopfzeilen-Sitzung als Cookie: erwartet 401, bekam %d", r.StatusCode)
	}
	// Die Fehlversuche haben die echten Sitzungen nicht beendet.
	if r := cookieB.do("GET", "/api/v1/auth/me", "", nil); r.StatusCode != 200 {
		t.Errorf("Cookie-Sitzung danach: %d", r.StatusCode)
	}
	if r := doRequest(t, app, "GET", "/api/v1/auth/me", headerValue, ""); r.StatusCode != 200 {
		t.Errorf("Kopfzeilen-Sitzung danach: %d", r.StatusCode)
	}
}

func TestLogoutEntwertetDieSitzung(t *testing.T) {
	app := cookieApp(t)
	b := cookieBrowser(t, app)
	b.login(nil)
	old := *b.cookies[sessionCookie]

	// Logout ist ändernd: ohne CSRF-Token abgewiesen, die Sitzung lebt weiter.
	if r := b.do("POST", "/api/v1/auth/logout", "", nil); r.StatusCode != 403 {
		t.Fatalf("Logout ohne CSRF: erwartet 403, bekam %d", r.StatusCode)
	}
	resp := b.do("POST", "/api/v1/auth/logout", "", b.withCSRF())
	if resp.StatusCode != 200 {
		t.Fatalf("Logout: %d", resp.StatusCode)
	}
	for _, name := range []string{sessionCookie, csrfCookie} {
		if c := cookieByName(resp, name); c == nil || c.MaxAge >= 0 || c.Value != "" {
			t.Errorf("%s nicht gelöscht: %+v", name, c)
		}
	}
	// Der alte Cookie-Wert, z. B. von einem Angreifer mitgeschnitten, ist tot.
	replay := cookieBrowser(t, app)
	replay.cookies[sessionCookie] = &old
	if r := replay.do("GET", "/api/v1/auth/me", "", nil); r.StatusCode != 401 {
		t.Errorf("alter Cookie nach Logout: erwartet 401, bekam %d", r.StatusCode)
	}
}

// TestLogoutOhneCookieSetztKeinCookie: In der Arbeitsspeicher-Variante endet die
// Sitzung serverseitig, und es kommt kein Set-Cookie.
func TestLogoutOhneCookieSetztKeinCookie(t *testing.T) {
	app, _ := buildTestApp(t, 0, nil)
	b := newBrowser(t, app)
	b.login(nil)
	token := b.headers[sessionHeader]

	resp := b.do("POST", "/api/v1/auth/logout", "", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("Logout: %d", resp.StatusCode)
	}
	if got := resp.Header.Values("Set-Cookie"); len(got) != 0 {
		t.Errorf("Set-Cookie beim Logout: %v", got)
	}
	if r := doRequest(t, app, "GET", "/api/v1/auth/me", token, ""); r.StatusCode != 401 {
		t.Errorf("Sitzungswert nach Logout: erwartet 401, bekam %d", r.StatusCode)
	}
}

func TestSitzungLeerlaufUndVerlaengerung(t *testing.T) {
	clock := newFakeClock()
	app, _ := buildTestApp(t, 0, nil, func(d *router.Deps) { d.Auth.WithClock(clock.Now) })
	b := newBrowser(t, app)
	b.login(nil)

	// Aktivität verlängert: die Zeit seit dem Login überschreitet die
	// Leerlauf-Frist (1 h), die Zeit seit der letzten Anfrage nie.
	for range 4 {
		clock.Advance(40 * time.Minute)
		if r := b.do("GET", "/api/v1/auth/me", "", nil); r.StatusCode != 200 {
			t.Fatalf("aktive Sitzung nach %v: %d", clock.Now(), r.StatusCode)
		}
	}
	// Ohne Aktivität läuft sie ab - und bleibt abgelaufen.
	clock.Advance(61 * time.Minute)
	if r := b.do("GET", "/api/v1/auth/me", "", nil); r.StatusCode != 401 {
		t.Fatalf("Leerlauf: erwartet 401, bekam %d", r.StatusCode)
	}
	clock.Advance(-30 * time.Minute)
	if r := b.do("GET", "/api/v1/auth/me", "", nil); r.StatusCode != 401 {
		t.Errorf("abgelaufene Sitzung darf nicht wieder aufleben: %d", r.StatusCode)
	}
}

func TestSitzungAbsoluteObergrenze(t *testing.T) {
	clock := newFakeClock()
	app := cookieApp(t, func(d *router.Deps) { d.Auth.WithClock(clock.Now) })
	b := cookieBrowser(t, app)
	b.login(nil)
	// Alle 50 Minuten aktiv: nie im Leerlauf, aber nach 12 h ist Schluss.
	var lastOK time.Duration
	start := clock.Now()
	for range 20 {
		clock.Advance(50 * time.Minute)
		if b.do("GET", "/api/v1/auth/me", "", nil).StatusCode != 200 {
			break
		}
		lastOK = clock.Now().Sub(start)
	}
	if lastOK < 11*time.Hour || lastOK > 12*time.Hour {
		t.Errorf("letzte erfolgreiche Anfrage nach %v, erwartet knapp unter 12 h", lastOK)
	}
}

// TestSitzungEndetBeiAenderungAmKonto: Passwortwechsel, Deaktivierung und
// Rollenwechsel beenden die Sitzung des betroffenen Nutzers - in beiden Varianten.
func TestSitzungEndetBeiAenderungAmKonto(t *testing.T) {
	for _, variant := range []string{"memory", "cookie"} {
		t.Run(variant, func(t *testing.T) {
			var app *fiber.App
			newSessionBrowser := func() *browser { return newBrowser(t, app) }
			if variant == "cookie" {
				app = cookieApp(t)
				newSessionBrowser = func() *browser { return cookieBrowser(t, app) }
			} else {
				app, _ = buildTestApp(t, 0, nil)
			}
			admin := loginToken(t, app, "admin", adminPassword)
			const pw = "Anker5-Leuchtturm!Wind"
			create := doRequest(t, app, "POST", "/api/v1/users", admin,
				`{"username":"sitzung","password":"`+pw+`","roles":["manager"]}`)
			if create.StatusCode != 201 {
				t.Fatalf("user anlegen: %d", create.StatusCode)
			}
			id := uint(decodeMap(t, create)["id"].(float64))

			login := func() *browser {
				b := newSessionBrowser()
				b.loginAs("sitzung", pw, nil)
				return b
			}
			alive := func(b *browser) bool {
				return b.do("GET", "/api/v1/auth/me", "", nil).StatusCode == 200
			}
			path := func(suffix string) string { return fmt.Sprintf("/api/v1/users/%d/%s", id, suffix) }

			b := login()
			if !alive(b) {
				t.Fatal("frische Sitzung muss leben")
			}
			if r := doRequest(t, app, "PUT", path("roles"), admin, `{"roles":["admin"]}`); r.StatusCode != 200 {
				t.Fatalf("Rollen setzen: %d", r.StatusCode)
			}
			if alive(b) {
				t.Error("Rollenwechsel beendet die Sitzung nicht")
			}

			b = login()
			if r := doRequest(t, app, "PATCH", path("active"), admin, `{"active":false}`); r.StatusCode != 200 {
				t.Fatalf("deaktivieren: %d", r.StatusCode)
			}
			if alive(b) {
				t.Error("Deaktivierung beendet die Sitzung nicht")
			}
			doRequest(t, app, "PATCH", path("active"), admin, `{"active":true}`)

			b = login()
			time.Sleep(10 * time.Millisecond)
			if r := doRequest(t, app, "POST", path("reset-password"), admin, `{"password":"Birke7-Zaunkoenig!Moor"}`); r.StatusCode != 200 {
				t.Fatalf("reset-password: %d", r.StatusCode)
			}
			if alive(b) {
				t.Error("Passwortwechsel beendet die Sitzung nicht")
			}
		})
	}
}

func TestSpracheAmKonto(t *testing.T) {
	app, _ := buildTestApp(t, 0, nil)
	token := loginToken(t, app, "admin", adminPassword)

	me := decodeMap(t, doRequest(t, app, "GET", "/api/v1/auth/me", token, ""))
	if lang, ok := me["language"]; !ok || lang != "" {
		t.Errorf("language ohne Auswahl = %v (vorhanden=%v), erwartet leer", lang, ok)
	}

	for _, lang := range []string{"de", "en", "en-GB", "zh-Hant", "fil", "de-CH-1996"} {
		r := doRequest(t, app, "PATCH", "/api/v1/auth/me/preferences", token, `{"language":"`+lang+`"}`)
		if r.StatusCode != 200 {
			t.Errorf("%q: erwartet 200, bekam %d", lang, r.StatusCode)
		}
	}
	for _, lang := range []string{"", "e", "EN", "en_US", "abcd", "abcdefghijk", "en-", "en--GB", "1a", "en GB", "de\\u0000"} {
		r := doRequest(t, app, "PATCH", "/api/v1/auth/me/preferences", token, `{"language":"`+lang+`"}`)
		if r.StatusCode != 400 {
			t.Errorf("%q: erwartet 400, bekam %d", lang, r.StatusCode)
			continue
		}
		if code := decodeMap(t, r)["code"]; code != "auth.language_invalid" {
			t.Errorf("%q: code = %v", lang, code)
		}
	}
	if r := doRequest(t, app, "PATCH", "/api/v1/auth/me/preferences", token, `kaputt`); r.StatusCode != 400 {
		t.Errorf("kaputtes JSON: erwartet 400, bekam %d", r.StatusCode)
	}
	if r := doRequest(t, app, "PATCH", "/api/v1/auth/me/preferences", "", `{"language":"en"}`); r.StatusCode != 401 {
		t.Errorf("ohne Anmeldung: erwartet 401, bekam %d", r.StatusCode)
	}

	// Gespeichert: gilt für jede neue Anmeldung, auf jedem Gerät.
	if r := doRequest(t, app, "PATCH", "/api/v1/auth/me/preferences", token, `{"language":"en"}`); r.StatusCode != 200 {
		t.Fatal("speichern")
	}
	again := loginToken(t, app, "admin", adminPassword)
	me = decodeMap(t, doRequest(t, app, "GET", "/api/v1/auth/me", again, ""))
	if me["language"] != "en" {
		t.Errorf("language nach erneuter Anmeldung = %v", me["language"])
	}
}

// TestMeLiefertAllesFuerDieOberflaeche: Felder, mit denen die Oberfläche Menüs
// und Pflichtschritte steuert.
func TestMeLiefertAllesFuerDieOberflaeche(t *testing.T) {
	app, db := buildTestApp(t, 0, nil)
	b := newBrowser(t, app)
	b.login(nil)
	me := decodeMap(t, b.do("GET", "/api/v1/auth/me", "", nil))

	for _, key := range []string{"id", "username", "email", "first_name", "last_name", "display_name",
		"roles", "permissions", "totp_enabled", "must_change_password", "must_setup_2fa", "language", "session_mode"} {
		if _, ok := me[key]; !ok {
			t.Errorf("/auth/me ohne Feld %q", key)
		}
	}
	if me["username"] != "admin" {
		t.Errorf("username = %v", me["username"])
	}
	if roles, _ := me["roles"].([]any); len(roles) == 0 {
		t.Errorf("roles leer: %v", me["roles"])
	}
	if perms, _ := me["permissions"].([]any); len(perms) == 0 {
		t.Errorf("permissions leer: %v", me["permissions"])
	}
	if me["totp_enabled"] != false || me["must_change_password"] != false || me["must_setup_2fa"] != false {
		t.Errorf("Zustand: totp=%v change=%v setup2fa=%v", me["totp_enabled"], me["must_change_password"], me["must_setup_2fa"])
	}

	// API-Key: Profil ja, aber weder Sitzungsweg noch csrf_token (keine Sitzung dahinter).
	key := createAPIKey(t, app, b.headers[sessionHeader], `{"name":"me"}`)
	viaKey := decodeMap(t, newBrowser(t, app).do("GET", "/api/v1/auth/me", "", map[string]string{"X-API-Key": key}))
	for _, field := range []string{"csrf_token", "session_mode"} {
		if _, has := viaKey[field]; has {
			t.Errorf("%s bei API-Key-Anmeldung", field)
		}
	}

	// Der Remediation-Zustand erscheint im Profil.
	admin, err := repositories.NewUserRepository(db).FindByUsername("admin")
	if err != nil {
		t.Fatal(err)
	}
	setRemediation(t, db, admin.ID)
	me = decodeMap(t, b.do("GET", "/api/v1/auth/me", "", nil))
	if me["must_change_password"] != true {
		t.Errorf("must_change_password = %v, erwartet true", me["must_change_password"])
	}

}

func setRemediation(t *testing.T, db *gorm.DB, userID uint) {
	t.Helper()
	if err := repositories.NewUserRepository(db).UpdateFields(userID, map[string]any{"must_change_password": true}); err != nil {
		t.Fatal(err)
	}
}

// TestLoginAntwortform: Arbeitsspeicher-Variante liefert session_token, Cookie-
// Variante csrf_token; ein JWT-"token" gibt es nicht mehr.
func TestLoginAntwortform(t *testing.T) {
	check := func(name string, body map[string]any, want, absent []string) {
		for _, key := range want {
			if _, ok := body[key]; !ok {
				t.Errorf("%s: Login-Antwort ohne %q", name, key)
			}
		}
		for _, key := range append(absent, "token", "next") {
			if _, has := body[key]; has {
				t.Errorf("%s: Login-Antwort enthält %q", name, key)
			}
		}
	}
	app, _ := buildTestApp(t, 0, nil)
	_, body := newBrowser(t, app).login(nil)
	check("memory", body, []string{"session_token", "user", "must_setup_2fa"}, []string{"csrf_token"})

	_, body = cookieBrowser(t, cookieApp(t)).login(nil)
	check("cookie", body, []string{"csrf_token", "user", "must_setup_2fa"}, []string{"session_token"})
}
