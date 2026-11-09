package controllers

import (
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/fasthttp/websocket"
	"github.com/gofiber/fiber/v3"

	"LCM/internal/api/apierr"
	"LCM/internal/api/middlewares"
	"LCM/internal/core/domain"
	"LCM/internal/core/events"
	"LCM/internal/core/services"
	"LCM/internal/safego"
	"LCM/internal/storage/repositories"
)

// Der Ereigniskanal: GET /api/v1/events (WebSocket).
//
// REST bleibt die Quelle der Wahrheit. Der Kanal meldet nur, DASS sich etwas
// geändert hat; der Tab holt die Einzelheiten über die normale, rechtegeprüfte
// API nach. Jeder Tab hat seine eigene Verbindung und abonniert, was er gerade
// anzeigt.
//
// Anmeldung: Der Browser kann an einem WebSocket keine Kopfzeile setzen. Der Tab
// holt deshalb vor jedem Verbindungsaufbau über POST /api/v1/events/ticket (mit
// seiner Sitzung) eine Einmal-Fahrkarte und verbindet mit ?ticket=…; sie gilt
// 30 Sekunden, einmal, für Benutzer und Sitzung (services.EventTickets). Skripte
// verbinden mit X-API-Key, ohne Fahrkarte. Ein Sitzungs-Cookie allein genügt
// am Kanal nicht.
//
// Protokoll (JSON-Textrahmen):
//
//	Client → Server  {"type":"subscribe","topics":["servers","server:42","jobs","job:<uuid>","alerts","bulk","system","dashboard"]}
//	                 {"type":"unsubscribe","topics":[…]}
//	Server → Client  {"type":"hello","version":"1.49.1","build":"123"}      nach dem Verbinden
//	                 {"type":"subscribed","topics":[…]}                     Antwort auf subscribe/unsubscribe: aktuelle Themen
//	                 {"type":"event","event":"server.status","topic":"server:42","data":{…}}
//	                 {"type":"resync"}                                      Ereignisse gingen verloren: alles neu laden
//	                 {"type":"ping"}                                        alle 30 s, der Client antwortet nichts
//	                 {"type":"error","code":"events.bad_topic","error":"…","topic":"…"}
//
// Zusätzlich schickt der Server alle 30 s einen WebSocket-Ping (Steuerrahmen,
// vom Browser selbstständig beantwortet): Damit erkennt der SERVER einen toten
// Client; das JSON-Ping lässt den CLIENT-Code einen toten Server erkennen,
// denn JavaScript sieht Steuerrahmen nicht.
//
// Ein Ereignis wird einmal geschickt, auch wenn es zu mehreren abonnierten
// Themen passt; „topic" nennt das spezifischste (server:/job: vor den
// allgemeinen).
//
// Schließcodes: 4401 = Anmeldung ungültig oder Rechte geändert (neu anmelden
// bzw. neu verbinden), 4429 = zu viele Verbindungen dieser Sitzung.
const (
	maxEventConnsPerSession = 20
	maxEventTopics          = 64
	maxEventMessageBytes    = 4096

	closeUnauthorized       = 4401
	closeTooManyConnections = 4429

	reasonSessionInvalid = "events.session_invalid"
	reasonAccessChanged  = "events.access_changed"
	reasonTooMany        = "events.too_many_connections"
	reasonGoingAway      = "events.server_stopping"

	eventWriteTimeout = 10 * time.Second
	// eventReadTimeout: zwei verpasste Pings plus Reserve.
	eventReadTimeout = 75 * time.Second
	// missRefreshGap begrenzt, wie oft ein Ereignis für einen unbekannten
	// Server die Sichtbarkeit neu laden darf (etwa nach einer Zuweisung).
	missRefreshGap = 5 * time.Second
	// eventShutdownWait ist die Frist, in der Close auf das Ende der
	// Verbindungen wartet.
	eventShutdownWait = 2 * time.Second
)

var (
	errOriginForbidden       = apierr.Define(403, "events.origin_forbidden", "cross-origin WebSocket connections are not allowed")
	errUpgradeFailed         = apierr.Define(400, "events.upgrade_failed", "WebSocket upgrade failed")
	errEventsTicketInvalid   = apierr.Define(401, "events.ticket_invalid", "invalid or expired event channel ticket")
	errEventsTicketNoSession = apierr.Define(400, "events.ticket_needs_session", "an event channel ticket needs a browser session; scripts connect with X-API-Key")
)

// credentialKind sagt, womit sich eine Verbindung angemeldet hat.
type credentialKind int

const (
	credAPIKey  credentialKind = iota // Kopfzeile X-API-Key
	credSession                       // Browser-Sitzung, über eine Fahrkarte
)

// credential ist der Nachweis, mit dem eine Verbindung angemeldet wurde und
// den die Prüfung im Takt erneut vorlegt: der API-Key, oder die öffentliche
// ID der Browser-Sitzung (nie der Sitzungswert selbst).
type credential struct {
	kind  credentialKind
	value string
}

// connectionKey identifiziert die Sitzung einer Verbindung für die Obergrenze:
// die Sitzungs-ID, bei einem API-Key sein SHA-256. Der Nachweis selbst wird nie
// gespeichert oder protokolliert.
func connectionKey(cred credential) string {
	if cred.kind == credSession {
		return cred.value
	}
	sum := sha256.Sum256([]byte(cred.value))
	return hex.EncodeToString(sum[:])
}

// EventHub nimmt die Verbindungen des Ereigniskanals an und verteilt die
// Ereignisse des Busses an sie.
type EventHub struct {
	bus *events.Bus
	// validate prüft einen Nachweis erneut - dasselbe wie die Anmelde-Middleware.
	validate func(credential) (*domain.User, error)
	// tickets vergibt die Fahrkarten, mit denen Browser verbinden.
	tickets *services.EventTickets
	// visibleIDs liefert die Server, die ein Sichtbereich sehen darf.
	visibleIDs func(repositories.AccessScope) ([]uint, error)
	// buildInfo liefert Version und Build für den Hello-Rahmen; optional.
	buildInfo func() (version, build string)
	// publicHost liefert den Host der öffentlichen Basis-Adresse; optional.
	publicHost func() string

	// Takte; Variablen, damit Tests sie auf Millisekunden drehen können.
	pingEvery       time.Duration
	revalidateEvery time.Duration

	wg     sync.WaitGroup // laufende Verbindungen
	mu     sync.Mutex
	conns  map[*eventConn]struct{}
	perKey map[string]int
}

func newEventHub(bus *events.Bus, validate func(credential) (*domain.User, error),
	visibleIDs func(repositories.AccessScope) ([]uint, error)) *EventHub {
	return &EventHub{
		bus: bus, validate: validate, visibleIDs: visibleIDs, tickets: services.NewEventTickets(),
		pingEvery: 30 * time.Second, revalidateEvery: 60 * time.Second,
		conns: map[*eventConn]struct{}{}, perKey: map[string]int{},
	}
}

// NewEventHub verdrahtet den Kanal mit den Diensten, die Anmeldung prüfen und
// Sichtbereiche auflösen.
func NewEventHub(bus *events.Bus, auth *services.AuthService, apiKeys *services.APIKeyService, servers *services.ServerService) *EventHub {
	validate := func(cred credential) (*domain.User, error) {
		if cred.kind == credSession {
			// Nur prüfen, nicht verlängern: Eine offene Verbindung ist keine
			// Aktivität und darf die Sitzung nicht wachhalten.
			return auth.CheckSession(cred.value)
		}
		user, key, err := apiKeys.Validate(cred.value)
		if err == nil && key.IsMCP() {
			err = apierr.Forbidden // MCP-Schlüssel gelten nur am MCP-Listener
		}
		return user, err
	}
	return newEventHub(bus, validate, servers.VisibleServerIDs)
}

// WithBuildInfo hinterlegt, woher Version und Build für den Hello-Rahmen kommen.
func (h *EventHub) WithBuildInfo(fn func() (version, build string)) *EventHub {
	h.buildInfo = fn
	return h
}

// WithPublicHost hinterlegt den Host der öffentlichen Basis-Adresse. Hinter
// einem Reverse-Proxy, der den Host-Header verändert, ist das der Origin, den
// der Browser tatsächlich schickt.
func (h *EventHub) WithPublicHost(fn func() string) *EventHub {
	h.publicHost = fn
	return h
}

// CloseSession schließt die Verbindungen einer Browser-Sitzung (Abmelden).
// Andere Sitzungen desselben Benutzers, etwa auf einem anderen Gerät, bleiben
// bestehen: Sie würden sonst fälschlich zur Anmeldung geschickt. nil-sicher.
func (h *EventHub) CloseSession(sessionID string) {
	if h == nil || sessionID == "" {
		return
	}
	h.closeMatching(func(c *eventConn) bool { return c.cred.kind == credSession && c.key == sessionID },
		closeUnauthorized, reasonSessionInvalid)
}

// CloseBrowserSessions schließt die Verbindungen aller Browser-Sitzungen
// (Wechsel von browser_session_mode). API-Key-Verbindungen bleiben. nil-sicher.
func (h *EventHub) CloseBrowserSessions() {
	if h == nil {
		return
	}
	h.closeMatching(func(c *eventConn) bool { return c.cred.kind == credSession },
		closeUnauthorized, reasonSessionInvalid)
}

// CloseUser schließt alle Verbindungen eines Benutzers (Rollen- oder
// Statuswechsel, neues Passwort). Der Tab verbindet danach mit seinen neuen
// Rechten neu - oder geht zur Anmeldung, wenn er keine mehr hat. nil-sicher.
func (h *EventHub) CloseUser(userID uint) {
	if h == nil {
		return
	}
	h.closeMatching(func(c *eventConn) bool { return c.userID == userID }, closeUnauthorized, reasonAccessChanged)
}

// Close beendet alle Verbindungen (Dienst-Stopp) und wartet kurz darauf.
func (h *EventHub) Close() error {
	h.closeMatching(func(*eventConn) bool { return true }, websocket.CloseGoingAway, reasonGoingAway)
	done := make(chan struct{})
	go func() { h.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(eventShutdownWait):
	}
	return nil
}

func (h *EventHub) closeMatching(match func(*eventConn) bool, code int, reason string) {
	h.mu.Lock()
	var hit []*eventConn
	for c := range h.conns {
		if match(c) {
			hit = append(hit, c)
		}
	}
	h.mu.Unlock()
	for _, c := range hit {
		c.requestClose(code, reason)
	}
}

// register nimmt eine Verbindung auf; false, wenn ihre Sitzung schon das
// Maximum nutzt.
func (h *EventHub) register(c *eventConn) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.perKey[c.key] >= maxEventConnsPerSession {
		return false
	}
	h.perKey[c.key]++
	h.conns[c] = struct{}{}
	return true
}

func (h *EventHub) unregister(c *eventConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.conns, c)
	if h.perKey[c.key]--; h.perKey[c.key] <= 0 {
		delete(h.perKey, c.key)
	}
}

// Ticket - POST /api/v1/events/ticket (angemeldet)
// Stellt dem Tab die Einmal-Fahrkarte für den nächsten Verbindungsaufbau aus.
// Skripte mit API-Key haben keine Sitzung und verbinden direkt.
func (h *EventHub) Ticket(c fiber.Ctx) error {
	sessionID := middlewares.CurrentSessionID(c)
	if sessionID == "" {
		return errEventsTicketNoSession
	}
	token, err := h.tickets.Issue(middlewares.CurrentUser(c).ID, sessionID)
	if err != nil {
		return apierr.Internal
	}
	return c.JSON(fiber.Map{"ticket": token, "expires_in_seconds": 30})
}

// authorize bestimmt, wer verbindet: ein API-Key (Kopfzeile) oder eine Browser-
// Sitzung, die sich mit einer Fahrkarte ausweist. Die Sitzung wird beim Einlösen
// noch einmal geprüft; die Fahrkarte allein macht keine Anmeldung.
func (h *EventHub) authorize(c fiber.Ctx) (connParams, error) {
	if user := middlewares.CurrentUser(c); user != nil && middlewares.CurrentAPIKeyPrefix(c) != "" {
		cred := credential{credAPIKey, c.Get("X-API-Key")}
		return connParams{user: user, cred: cred, key: connectionKey(cred), viaAPIKey: true}, nil
	}
	userID, sessionID, ok := h.tickets.Redeem(c.Query("ticket"))
	if !ok {
		middlewares.SecurityEvent(c, "events.ticket.rejected")
		return connParams{}, errEventsTicketInvalid
	}
	cred := credential{credSession, sessionID}
	user, err := h.validate(cred)
	if err != nil || user.ID != userID {
		middlewares.SecurityEvent(c, "events.ticket.rejected")
		return connParams{}, errEventsTicketInvalid
	}
	return connParams{user: user, cred: cred, key: connectionKey(cred)}, nil
}

// Connect - GET /api/v1/events (WebSocket; API-Key oder Fahrkarte)
//
// Die Origin-Prüfung ist Pflicht: Ein Browser schickt Cookies auch bei einem
// WebSocket, der von einer fremden Seite aufgebaut wird (Cross-Site-WebSocket-
// Hijacking). Ohne die Prüfung könnte jede Webseite, die der Benutzer offen hat,
// seinen Ereigniskanal mitlesen. Die Fahrkarte kommt zusätzlich: Eine fremde
// Seite kommt an keine, weil POST /events/ticket Sitzung und CSRF-Schutz verlangt.
func (h *EventHub) Connect(c fiber.Ctx) error {
	// Alles Nötige vorab aus dem Request ziehen: Der Rückruf läuft nach dem
	// Upgrade in einer eigenen Goroutine, und der Kontext ist dann schon
	// wiederverwendet.
	p, err := h.authorize(c)
	if err != nil {
		return err
	}
	if err := h.checkOrigin(c, p.viaAPIKey); err != nil {
		middlewares.SecurityEvent(c, "events.origin.rejected", "origin", c.Get(fiber.HeaderOrigin), "user", p.user.Username)
		return err
	}

	// Den Ursprung hat checkOrigin oben geprüft - mit mehr Wissen als die
	// Bibliothek (öffentliche Basis-Adresse). Deren eigene Prüfung (Origin gleich
	// Host) würde eine zugelassene öffentliche Adresse wieder abweisen; ohne
	// Origin-Kopfzeile lässt sie die Verbindung durch.
	c.Request().Header.Del(fiber.HeaderOrigin)
	upgrader := websocket.FastHTTPUpgrader{}
	upErr := upgrader.Upgrade(c.RequestCtx(), func(ws *websocket.Conn) {
		// Ab hier läuft die Verbindung außerhalb der recover-Middleware.
		defer safego.Recover("events-ws", nil)
		h.serve(ws, p)
	})
	if upErr != nil {
		return errUpgradeFailed
	}
	return nil
}

// checkOrigin lässt nur Verbindungen von der eigenen Seite zu: Der Host des
// Origin muss dem Host der Anfrage oder dem der öffentlichen Basis-Adresse
// entsprechen. Ohne Origin (kein Browser) ist nur ein API-Schlüssel genug.
func (h *EventHub) checkOrigin(c fiber.Ctx, viaAPIKey bool) error {
	origin := c.Get(fiber.HeaderOrigin)
	if origin == "" {
		if viaAPIKey {
			return nil
		}
		return errOriginForbidden
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return errOriginForbidden
	}
	host := strings.ToLower(u.Host)
	if host == strings.ToLower(c.Host()) {
		return nil
	}
	if h.publicHost != nil {
		if public := h.publicHost(); public != "" && host == strings.ToLower(public) {
			return nil
		}
	}
	return errOriginForbidden
}

// connParams ist, was eine Verbindung vom Request mitbringt.
type connParams struct {
	user      *domain.User
	cred      credential
	key       string
	viaAPIKey bool
}

// serve betreibt eine Verbindung bis zu ihrem Ende. Es kehrt erst zurück, wenn
// beide Goroutinen der Verbindung beendet sind.
func (h *EventHub) serve(ws *websocket.Conn, p connParams) {
	h.wg.Add(1)
	defer h.wg.Done()
	defer ws.Close()

	c := newEventConn(h, ws, p)
	if !h.register(c) {
		c.writeClose(closeTooManyConnections, reasonTooMany)
		return
	}
	defer h.unregister(c)
	sub := h.bus.Subscribe()
	defer sub.Close()

	c.loadAccess(p.user)
	c.sendHello()
	readerDone := make(chan struct{})
	safego.Go("events-read", func() {
		defer close(readerDone)
		c.readLoop()
	})
	c.writeLoop(sub, readerDone)

	// Den Leser wecken: Close allein tut es auf echten Verbindungen nicht
	// zuverlässig, die abgelaufene Lesefrist schon.
	_ = ws.SetReadDeadline(time.Now())
	_ = ws.Close()
	<-readerDone
}

// logAccessError meldet, dass die Sichtbarkeit nicht geladen werden konnte. Die
// Verbindung bleibt auf dem alten Stand (leer, wenn es keinen gibt): lieber zu
// wenig melden als zu viel.
func logAccessError(userID uint, err error) {
	slog.Warn("events: visible servers could not be loaded", "user", userID, "error", err)
}
