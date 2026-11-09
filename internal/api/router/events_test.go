package router_test

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/fasthttp/websocket"
	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"

	"LCM/internal/api/router"
	"LCM/internal/core/domain"
	"LCM/internal/core/events"
)

// eventsServer ist die komplette App hinter einem echten Listener: WebSockets
// laufen nicht über app.Test.
type eventsServer struct {
	app  *fiber.App
	db   *gorm.DB
	bus  *events.Bus
	host string
}

func startEventsServer(t *testing.T) *eventsServer {
	t.Helper()
	s := &eventsServer{}
	s.app, s.db = buildTestApp(t, 0, nil, func(d *router.Deps) { s.bus = d.Events })
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s.host = ln.Addr().String()
	go func() { _ = s.app.Listener(ln, fiber.ListenConfig{DisableStartupMessage: true}) }()
	t.Cleanup(func() { _ = s.app.Shutdown() })
	return s
}

// dial verbindet; die Kopfzeilen und die Abfrage sagen, wie sich der Client ausweist.
func (s *eventsServer) dial(header http.Header, query string) (*websocket.Conn, *http.Response, error) {
	return websocket.DefaultDialer.Dial("ws://"+s.host+"/api/v1/events"+query, header)
}

// ticketFor holt wie die Oberfläche eine Fahrkarte mit dem Sitzungswert.
func (s *eventsServer) ticketFor(t *testing.T, token string) string {
	t.Helper()
	resp := doRequest(t, s.app, "POST", "/api/v1/events/ticket", token, "")
	if resp.StatusCode != 200 {
		t.Fatalf("Fahrkarte holen: %d", resp.StatusCode)
	}
	ticket, _ := decodeMap(t, resp)["ticket"].(string)
	if ticket == "" {
		t.Fatal("Antwort ohne ticket")
	}
	return ticket
}

// dialSession verbindet wie der Browser: mit frischer Fahrkarte und Origin der eigenen Seite.
func (s *eventsServer) dialSession(t *testing.T, token string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	return s.dial(http.Header{"Origin": {"http://" + s.host}}, "?ticket="+s.ticketFor(t, token))
}

func originHeader(origin string) http.Header {
	h := http.Header{}
	if origin != "" {
		h.Set("Origin", origin)
	}
	return h
}

// frames liest alle Rahmen in einen Kanal (siehe controllers/events_hub_test.go
// für den Grund).
func frames(conn *websocket.Conn) (<-chan map[string]any, <-chan error) {
	out, end := make(chan map[string]any, 256), make(chan error, 1)
	go func() {
		defer close(out)
		for {
			var f map[string]any
			if err := conn.ReadJSON(&f); err != nil {
				end <- err
				return
			}
			out <- f
		}
	}()
	return out, end
}

func nextFrame(t *testing.T, ch <-chan map[string]any, typ string) map[string]any {
	t.Helper()
	timeout := time.After(3 * time.Second)
	for {
		select {
		case f, ok := <-ch:
			if !ok {
				t.Fatalf("Verbindung beendet, kein Rahmen %q", typ)
			}
			if f["type"] == typ {
				return f
			}
		case <-timeout:
			t.Fatalf("kein Rahmen %q angekommen", typ)
		}
	}
}

func closedWith(t *testing.T, ch <-chan map[string]any, end <-chan error) int {
	t.Helper()
	timeout := time.After(3 * time.Second)
	for {
		select {
		case _, ok := <-ch:
			if ok {
				continue
			}
			var ce *websocket.CloseError
			if err := <-end; errors.As(err, &ce) {
				return ce.Code
			}
			t.Fatal("Verbindung ohne Schließcode beendet")
		case <-timeout:
			t.Fatal("die Verbindung wurde nicht geschlossen")
		}
	}
}

func TestEventsOriginPruefung(t *testing.T) {
	s := startEventsServer(t)
	token := loginToken(t, s.app, "admin", "test-admin-passwort")
	key := createAPIKey(t, s.app, token, `{"name":"ereignisse"}`)
	own := "http://" + s.host
	query := func() string { return "?ticket=" + s.ticketFor(t, token) }

	cases := []struct {
		name   string
		header http.Header
		query  string
		status int
	}{
		{"eigene Seite", originHeader(own), query(), 101},
		{"fremde Seite", originHeader("https://boese.example"), query(), 403},
		{"fremder Port", originHeader("http://127.0.0.1:1"), query(), 403},
		{"Origin null", originHeader("null"), query(), 403},
		{"Fahrkarte ohne Origin", originHeader(""), query(), 403},
		{"API-Schlüssel ohne Origin", http.Header{"X-API-Key": {key}}, "", 101},
		{"API-Schlüssel, fremde Seite", http.Header{"X-API-Key": {key}, "Origin": {"https://boese.example"}}, "", 403},
		{"ohne Anmeldung", originHeader(own), "", 401},
		{"Sitzungswert statt Fahrkarte", originHeader(own), "?ticket=" + token, 401},
		{"Sitzungswert als Kopfzeile", http.Header{"X-LCM-Session": {token}, "Origin": {own}}, "", 401},
		{"erfundene Fahrkarte", originHeader(own), "?ticket=ausgedacht", 401},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conn, resp, err := s.dial(tc.header, tc.query)
			if conn != nil {
				defer conn.Close()
			}
			got := 101
			if err != nil {
				if resp == nil {
					t.Fatalf("verbinden: %v", err)
				}
				got = resp.StatusCode
			}
			if got != tc.status {
				t.Errorf("Status %d, erwartet %d", got, tc.status)
			}
		})
	}
}

// TestEventsFahrkarteGiltEinmalUndNurMitSitzung: einmalig, nur für eine lebende
// Sitzung; ein API-Key bekommt keine (er verbindet direkt).
func TestEventsFahrkarteGiltEinmalUndNurMitSitzung(t *testing.T) {
	s := startEventsServer(t)
	token := loginToken(t, s.app, "admin", "test-admin-passwort")
	own := "http://" + s.host

	ticket := s.ticketFor(t, token)
	conn, _, err := s.dial(originHeader(own), "?ticket="+ticket)
	if err != nil {
		t.Fatalf("erstes Verbinden: %v", err)
	}
	conn.Close()
	if _, resp, err := s.dial(originHeader(own), "?ticket="+ticket); err == nil || resp.StatusCode != 401 {
		t.Errorf("zweites Einlösen: err=%v status=%v, erwartet 401", err, resp)
	}

	// Sitzung beendet, bevor die Fahrkarte eingelöst wird: wertlos.
	ticket = s.ticketFor(t, token)
	if r := doRequest(t, s.app, "POST", "/api/v1/auth/logout", token, ""); r.StatusCode != 200 {
		t.Fatalf("logout: %d", r.StatusCode)
	}
	if _, resp, err := s.dial(originHeader(own), "?ticket="+ticket); err == nil || resp.StatusCode != 401 {
		t.Errorf("Fahrkarte einer beendeten Sitzung: err=%v status=%v, erwartet 401", err, resp)
	}

	// Ohne Anmeldung und mit API-Key gibt es keine Fahrkarte.
	if r := doRequest(t, s.app, "POST", "/api/v1/events/ticket", "", ""); r.StatusCode != 401 {
		t.Errorf("Fahrkarte ohne Anmeldung: %d", r.StatusCode)
	}
	admin := loginToken(t, s.app, "admin", "test-admin-passwort")
	key := createAPIKey(t, s.app, admin, `{"name":"ereignisse"}`)
	b := newBrowser(t, s.app)
	r := b.do("POST", "/api/v1/events/ticket", "", map[string]string{"X-API-Key": key})
	if r.StatusCode != 400 || decodeMap(t, r)["code"] != "events.ticket_needs_session" {
		t.Errorf("Fahrkarte für API-Key: %d", r.StatusCode)
	}
}

// TestEventsFahrkarteMitCookieSitzung: Die Fahrkarte funktioniert in der Cookie-
// Variante genauso, mit dem Cookie statt der Kopfzeile.
func TestEventsFahrkarteMitCookieSitzung(t *testing.T) {
	s := &eventsServer{}
	s.app, s.db = buildTestApp(t, 0, nil, trustAllProxies(t), func(d *router.Deps) { s.bus = d.Events })
	setSessionMode(t, s.app, "cookie", true)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s.host = ln.Addr().String()
	go func() { _ = s.app.Listener(ln, fiber.ListenConfig{DisableStartupMessage: true}) }()
	t.Cleanup(func() { _ = s.app.Shutdown() })

	b := cookieBrowser(t, s.app)
	b.login(nil)
	resp := b.do("POST", "/api/v1/events/ticket", "", b.withCSRF())
	if resp.StatusCode != 200 {
		t.Fatalf("Fahrkarte: %d", resp.StatusCode)
	}
	ticket, _ := decodeMap(t, resp)["ticket"].(string)
	conn, _, err := s.dial(originHeader("http://"+s.host), "?ticket="+ticket)
	if err != nil {
		t.Fatalf("verbinden: %v", err)
	}
	defer conn.Close()
	ch, end := frames(conn)
	nextFrame(t, ch, "hello")

	// Abmelden schließt auch diese Verbindung.
	if r := b.do("POST", "/api/v1/auth/logout", "", b.withCSRF()); r.StatusCode != 200 {
		t.Fatalf("logout: %d", r.StatusCode)
	}
	if code := closedWith(t, ch, end); code != 4401 {
		t.Errorf("nach dem Abmelden: Schließcode %d, erwartet 4401", code)
	}
}

// TestEventsWechselDesSitzungswegsSchliesstAlles: Beim Wechsel enden alle
// Browser-Sitzungen, ihre Verbindungen werden geschlossen; ein API-Key bleibt.
func TestEventsWechselDesSitzungswegsSchliesstAlles(t *testing.T) {
	s := &eventsServer{}
	s.app, s.db = buildTestApp(t, 0, nil, trustAllProxies(t), func(d *router.Deps) { s.bus = d.Events })
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s.host = ln.Addr().String()
	go func() { _ = s.app.Listener(ln, fiber.ListenConfig{DisableStartupMessage: true}) }()
	t.Cleanup(func() { _ = s.app.Shutdown() })

	admin := loginToken(t, s.app, "admin", "test-admin-passwort")
	key := createAPIKey(t, s.app, admin, `{"name":"ereignisse"}`)
	conn, _, err := s.dialSession(t, admin)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	sessionCh, sessionEnd := frames(conn)
	nextFrame(t, sessionCh, "hello")
	keyConn, _, err := s.dial(http.Header{"X-API-Key": {key}}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer keyConn.Close()
	keyCh, _ := frames(keyConn)
	nextFrame(t, keyCh, "hello")

	resp := doRequestWithHeaders(t, s.app, "PATCH", "/api/v1/settings/global", admin, `{"browser_session_mode":"cookie"}`,
		map[string]string{"X-Forwarded-Proto": "https"})
	if resp.StatusCode != 200 {
		t.Fatalf("Wechsel: %d", resp.StatusCode)
	}
	if code := closedWith(t, sessionCh, sessionEnd); code != 4401 {
		t.Errorf("Browser-Verbindung nach dem Wechsel: Schließcode %d, erwartet 4401", code)
	}
	select {
	case f, ok := <-keyCh:
		if !ok {
			t.Errorf("die Verbindung des API-Keys wurde geschlossen")
		} else if f["type"] == "error" {
			t.Errorf("Fehlerrahmen am API-Key: %v", f)
		}
	case <-time.After(300 * time.Millisecond):
	}
}

// Ein Manager bekommt nur die Ereignisse seiner Gruppe - über den ganzen Weg
// von der Anmeldung bis zum Rahmen.
func TestEventsMandantentrennungMitEchterAnmeldung(t *testing.T) {
	s := startEventsServer(t)
	admin := loginToken(t, s.app, "admin", "test-admin-passwort")
	if r := doRequest(t, s.app, "POST", "/api/v1/users", admin,
		`{"username":"gruppe-a","password":"Anker5-Leuchtturm!Wind","roles":["manager"]}`); r.StatusCode != 201 {
		t.Fatalf("user anlegen: %d", r.StatusCode)
	}
	var manager domain.User
	if err := s.db.Where("username = ?", "gruppe-a").First(&manager).Error; err != nil {
		t.Fatal(err)
	}
	own := addCapabilityServer(t, s.db, "web-a", "apt")
	foreign := addCapabilityServer(t, s.db, "web-b", "apt")
	addServerToGroup(t, s.db, "Gruppe A", own, &manager)
	addServerToGroup(t, s.db, "Gruppe B", foreign, nil)

	token := loginToken(t, s.app, "gruppe-a", "Anker5-Leuchtturm!Wind")
	conn, _, err := s.dialSession(t, token)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ch, _ := frames(conn)
	nextFrame(t, ch, "hello")

	for _, topic := range []string{"servers", fmt.Sprintf("server:%d", foreign)} {
		_ = conn.WriteJSON(map[string]any{"type": "subscribe", "topics": []string{topic}})
	}
	if e := nextFrame(t, ch, "error"); e["code"] != "events.forbidden_topic" {
		t.Errorf("fremden Server abonnieren: %v", e)
	}

	s.bus.Publish(events.ServerBusy(events.ServerBusyData{ServerID: foreign, Busy: true}))
	s.bus.Publish(events.ServerBusy(events.ServerBusyData{ServerID: own, Busy: true}))
	ev := nextFrame(t, ch, "event")
	if data := ev["data"].(map[string]any); data["server_id"] != float64(own) {
		t.Errorf("erstes Ereignis gehört nicht zum eigenen Server: %v", ev)
	}
}

func TestEventsAbmeldungUndRollenwechselSchliessen(t *testing.T) {
	s := startEventsServer(t)
	admin := loginToken(t, s.app, "admin", "test-admin-passwort")
	for _, name := range []string{"user-a", "user-b"} {
		if r := doRequest(t, s.app, "POST", "/api/v1/users", admin,
			`{"username":"`+name+`","password":"Anker5-Leuchtturm!Wind","roles":["manager"]}`); r.StatusCode != 201 {
			t.Fatalf("user anlegen: %d", r.StatusCode)
		}
	}
	// connect meldet an und verbindet; zweiter Rückgabewert ist der Sitzungswert.
	connect := func(user string) (<-chan map[string]any, <-chan error, string) {
		token := loginToken(t, s.app, user, "Anker5-Leuchtturm!Wind")
		conn, _, err := s.dialSession(t, token)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		ch, end := frames(conn)
		nextFrame(t, ch, "hello")
		return ch, end, token
	}

	// Abmelden - nur die eigene Sitzung wird geschlossen, nicht die zweite
	// desselben Benutzers.
	chA, endA, tokenA := connect("user-a")
	chA2, _, _ := connect("user-a")
	if r := doRequest(t, s.app, "POST", "/api/v1/auth/logout", tokenA, ""); r.StatusCode != 200 {
		t.Fatalf("logout: %d", r.StatusCode)
	}
	select {
	case f, ok := <-chA2:
		if !ok || f["type"] == "error" {
			t.Errorf("die zweite Sitzung wurde mitgeschlossen: %v", f)
		}
	case <-time.After(300 * time.Millisecond):
	}
	if code := closedWith(t, chA, endA); code != 4401 {
		t.Errorf("nach dem Abmelden: Schließcode %d, erwartet 4401", code)
	}

	// Rollenwechsel und Sperren.
	var userB domain.User
	if err := s.db.Where("username = ?", "user-b").First(&userB).Error; err != nil {
		t.Fatal(err)
	}
	chB, endB, _ := connect("user-b")
	if r := doRequest(t, s.app, "PUT", fmt.Sprintf("/api/v1/users/%d/roles", userB.ID), admin, `{"roles":["admin"]}`); r.StatusCode != 200 {
		t.Fatalf("Rollen ändern: %d", r.StatusCode)
	}
	if code := closedWith(t, chB, endB); code != 4401 {
		t.Errorf("nach dem Rollenwechsel: Schließcode %d, erwartet 4401", code)
	}

	chB, endB, _ = connect("user-b")
	if r := doRequest(t, s.app, "PATCH", fmt.Sprintf("/api/v1/users/%d/active", userB.ID), admin, `{"active":false}`); r.StatusCode != 200 {
		t.Fatalf("sperren: %d", r.StatusCode)
	}
	if code := closedWith(t, chB, endB); code != 4401 {
		t.Errorf("nach dem Sperren: Schließcode %d, erwartet 4401", code)
	}
}

// Die Obergrenze gilt je Sitzung: Ein zweiter Nachweis desselben Benutzers (hier
// ein API-Schlüssel) hat sein eigenes Kontingent.
func TestEventsGrenzeJeSitzung(t *testing.T) {
	s := startEventsServer(t)
	token := loginToken(t, s.app, "admin", "test-admin-passwort")
	key := createAPIKey(t, s.app, token, `{"name":"ereignisse"}`)

	open := func(conn *websocket.Conn, err error) (<-chan map[string]any, <-chan error) {
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		return frames(conn)
	}
	session := func() (<-chan map[string]any, <-chan error) {
		conn, _, err := s.dialSession(t, token)
		return open(conn, err)
	}
	for range 20 {
		ch, _ := session()
		nextFrame(t, ch, "hello")
	}
	ch, end := session()
	if code := closedWith(t, ch, end); code != 4429 {
		t.Errorf("21. Verbindung: Schließcode %d, erwartet 4429", code)
	}
	conn, _, err := s.dial(http.Header{"X-API-Key": {key}}, "")
	ch, _ = open(conn, err)
	nextFrame(t, ch, "hello")
}
