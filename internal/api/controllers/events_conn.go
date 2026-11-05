package controllers

import (
	"encoding/json"
	"maps"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fasthttp/websocket"

	"LCM/internal/api/apierr"
	"LCM/internal/core/domain"
	"LCM/internal/core/events"
)

// eventConn ist eine Verbindung des Ereigniskanals. Zwei Goroutinen arbeiten an
// ihr: der Leser nimmt Abonnements entgegen, der Schreiber verteilt Ereignisse,
// hält die Verbindung am Leben und prüft regelmäßig die Anmeldung. Gemeinsame
// Zustände (Themen, Sichtbarkeit) sind unveränderliche Schnappschüsse hinter
// atomaren Zeigern.
type eventConn struct {
	hub    *EventHub
	ws     *websocket.Conn
	userID uint
	key    string
	cred   credential

	access atomic.Pointer[access]
	topics atomic.Pointer[map[string]topic] // nur der Leser ersetzt sie

	writeMu  sync.Mutex // ein Schreiber zugleich
	closeReq chan closeRequest
}

type closeRequest struct {
	code   int
	reason string
}

func newEventConn(h *EventHub, ws *websocket.Conn, p connParams) *eventConn {
	c := &eventConn{
		hub: h, ws: ws, userID: p.user.ID, key: p.key, cred: p.cred,
		closeReq: make(chan closeRequest, 1),
	}
	empty := map[string]topic{}
	c.topics.Store(&empty)
	return c
}

// requestClose bittet den Schreiber, die Verbindung zu schließen. Blockiert nie.
func (c *eventConn) requestClose(code int, reason string) {
	select {
	case c.closeReq <- closeRequest{code, reason}:
	default: // eine Bitte steht schon an
	}
}

// writeClose schickt den Schließrahmen. WriteControl darf neben anderen
// Schreibern laufen.
func (c *eventConn) writeClose(code int, reason string) {
	msg := websocket.FormatCloseMessage(code, reason)
	_ = c.ws.WriteControl(websocket.CloseMessage, msg, time.Now().Add(eventWriteTimeout))
}

// --- Sichtbarkeit -------------------------------------------------------------

// loadAccess bestimmt Rechte und sichtbare Server für den User neu und ersetzt
// den Schnappschuss. Scheitert das Laden, bleibt der alte Stand - beim ersten
// Mal ein leerer, der nichts durchlässt.
func (c *eventConn) loadAccess(user *domain.User) {
	next := &access{user: user, scope: scopeOfUser(user), loaded: time.Now()}
	if !next.scope.All {
		ids, err := c.hub.visibleIDs(next.scope)
		if err != nil {
			logAccessError(user.ID, err)
			if old := c.access.Load(); old != nil {
				next.visible = old.visible
			}
		} else {
			next.visible = toSet(ids)
		}
	}
	c.access.Store(next)
}

func toSet(ids []uint) map[uint]struct{} {
	set := make(map[uint]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	return set
}

// revalidate prüft die Anmeldung erneut und lädt Rechte und Sichtbarkeit neu.
// false: Die Verbindung ist geschlossen worden.
func (c *eventConn) revalidate() bool {
	user, err := c.hub.validate(c.cred)
	if err != nil || user.ID != c.userID {
		c.writeClose(closeUnauthorized, reasonSessionInvalid)
		return false
	}
	c.loadAccess(user)
	return true
}

// --- Leser --------------------------------------------------------------------

// clientMessage ist eine Nachricht des Browsers.
type clientMessage struct {
	Type   string   `json:"type"`
	Topics []string `json:"topics"`
}

// readLoop nimmt Abonnement-Nachrichten entgegen, bis die Verbindung endet.
// Die Lesefrist wird von jeder Nachricht und jedem Pong verlängert; verstummt
// ein Client ganz, endet die Verbindung von selbst.
func (c *eventConn) readLoop() {
	c.ws.SetReadLimit(maxEventMessageBytes)
	extend := func() error { return c.ws.SetReadDeadline(time.Now().Add(eventReadTimeout)) }
	_ = extend()
	c.ws.SetPongHandler(func(string) error { return extend() })
	for {
		kind, data, err := c.ws.ReadMessage()
		if err != nil {
			return
		}
		_ = extend()
		if kind != websocket.TextMessage {
			c.sendError(errBadMessage, "")
			continue
		}
		c.handleMessage(data)
	}
}

func (c *eventConn) handleMessage(data []byte) {
	var msg clientMessage
	if err := json.Unmarshal(data, &msg); err != nil || len(msg.Topics) > maxEventTopics {
		c.sendError(errBadMessage, "")
		return
	}
	switch msg.Type {
	case "subscribe":
		c.subscribe(msg.Topics)
	case "unsubscribe":
		c.unsubscribe(msg.Topics)
	default:
		c.sendError(errBadMessage, "")
	}
}

// subscribe nimmt die Themen an, die gültig und erlaubt sind; für jedes andere
// geht ein Fehlerrahmen zurück. Danach folgt die Liste der aktuellen Themen.
func (c *eventConn) subscribe(raw []string) {
	next := maps.Clone(*c.topics.Load())
	for _, r := range raw {
		t, rejected := parseTopic(r)
		if rejected == nil {
			rejected = c.admit(t, len(next))
		}
		if rejected != nil {
			c.sendError(rejected, r)
			continue
		}
		next[t.String()] = t
	}
	c.topics.Store(&next)
	c.sendSubscribed(next)
}

// admit entscheidet, ob ein Thema abonniert werden darf.
func (c *eventConn) admit(t topic, have int) *apierr.Error {
	acc := c.access.Load()
	switch {
	case have >= maxEventTopics:
		return errTooManyTopics
	case !acc.user.HasPermission(t.subscribePermission()):
		return errForbiddenTopic
	case t.kind == topicServer && !c.mayWatchServer(acc, t.serverID):
		return errForbiddenTopic
	}
	return nil
}

// mayWatchServer prüft die Sichtbarkeit eines Servers; ist er im Schnappschuss
// nicht enthalten, wird (höchstens alle missRefreshGap) frisch nachgesehen -
// eine eben erfolgte Zuweisung soll nicht erst nach einer Minute wirken.
func (c *eventConn) mayWatchServer(acc *access, serverID uint) bool {
	if acc.canSee(serverID) {
		return true
	}
	if time.Since(acc.loaded) < missRefreshGap {
		return false
	}
	ids, err := c.hub.visibleIDs(acc.scope)
	if err != nil {
		logAccessError(acc.user.ID, err)
		return false
	}
	return slices.Contains(ids, serverID)
}

func (c *eventConn) unsubscribe(raw []string) {
	next := maps.Clone(*c.topics.Load())
	for _, r := range raw {
		if t, rejected := parseTopic(r); rejected == nil {
			delete(next, t.String())
		}
	}
	c.topics.Store(&next)
	c.sendSubscribed(next)
}

// --- Schreiber ----------------------------------------------------------------

// writeLoop verteilt Ereignisse und hält die Verbindung, bis sie endet.
func (c *eventConn) writeLoop(sub *events.Subscription, readerDone <-chan struct{}) {
	ping := time.NewTicker(c.hub.pingEvery)
	defer ping.Stop()
	check := time.NewTicker(c.hub.revalidateEvery)
	defer check.Stop()
	for {
		var ok bool
		select {
		case e := <-sub.Events():
			ok = c.forward(e)
		case <-sub.Resync():
			c.loadAccess(c.access.Load().user)
			ok = c.send(frame{Type: "resync"})
		case <-ping.C:
			ok = c.keepAlive()
		case <-check.C:
			ok = c.revalidate()
		case req := <-c.closeReq:
			c.writeClose(req.code, req.reason)
			return
		case <-readerDone:
			return
		}
		if !ok {
			return
		}
	}
}

// forward schickt ein Ereignis an den Tab, wenn er es abonniert hat und sehen
// darf. false: Schreiben ist gescheitert.
func (c *eventConn) forward(e events.Event) bool {
	if e.All && e.Type == events.TypeServerStatus && c.wants(e) {
		// Das Verwerfen aller Ampeln folgt auf jede ändernde Anfrage - auch auf
		// Zuweisungen von Servern an Gruppen. Hier wird der Sichtbereich frisch.
		c.loadAccess(c.access.Load().user)
	}
	t, found := c.route(e)
	if !found {
		return true
	}
	return c.send(frame{Type: "event", Event: e.Type, Topic: t.String(), Data: e.Data})
}

// wants meldet, ob der Tab irgendein Thema abonniert hat, zu dem das Ereignis passt.
func (c *eventConn) wants(e events.Event) bool {
	for _, t := range *c.topics.Load() {
		if t.matches(e) {
			return true
		}
	}
	return false
}

// route wählt das Thema, unter dem der Tab das Ereignis bekommt. Gehört das
// Ereignis zu einem Server, der im Schnappschuss fehlt, wird einmal frisch
// nachgesehen (höchstens alle missRefreshGap).
func (c *eventConn) route(e events.Event) (topic, bool) {
	topics := *c.topics.Load()
	acc := c.access.Load()
	best, matched, found := pick(acc, topics, e)
	if !found && matched && e.ServerID != 0 && !acc.canSee(e.ServerID) &&
		time.Since(acc.loaded) >= missRefreshGap {
		c.loadAccess(acc.user)
		best, _, found = pick(c.access.Load(), topics, e)
	}
	return best, found
}

// pick sucht unter den abonnierten Themen das spezifischste, das dem Benutzer
// das Ereignis liefern darf. matched: Es gab mindestens ein passendes Thema.
func pick(acc *access, topics map[string]topic, e events.Event) (best topic, matched, found bool) {
	for _, t := range topics {
		if !t.matches(e) {
			continue
		}
		matched = true
		if !acc.deliverable(t, e) {
			continue
		}
		if !found || moreSpecific(t, best) {
			best, found = t, true
		}
	}
	return best, matched, found
}

// moreSpecific ordnet Themen: spezifische vor allgemeinen, sonst nach Namen -
// damit die Wahl nicht von der Reihenfolge der Map abhängt.
func moreSpecific(a, b topic) bool {
	if a.specific() != b.specific() {
		return a.specific()
	}
	return a.String() < b.String()
}

// keepAlive schickt Ping (Steuerrahmen und JSON).
func (c *eventConn) keepAlive() bool {
	c.writeMu.Lock()
	_ = c.ws.SetWriteDeadline(time.Now().Add(eventWriteTimeout))
	err := c.ws.WriteMessage(websocket.PingMessage, nil)
	c.writeMu.Unlock()
	return err == nil && c.send(frame{Type: "ping"})
}

// --- Rahmen -------------------------------------------------------------------

// frame ist eine Nachricht an den Browser.
type frame struct {
	Type    string   `json:"type"`
	Event   string   `json:"event,omitempty"`
	Topic   string   `json:"topic,omitempty"`
	Data    any      `json:"data,omitempty"`
	Topics  []string `json:"topics,omitempty"`
	Version string   `json:"version,omitempty"`
	Build   string   `json:"build,omitempty"`
	Code    string   `json:"code,omitempty"`
	Error   string   `json:"error,omitempty"`
}

// send schreibt einen Rahmen; false, wenn die Verbindung kaputt ist.
func (c *eventConn) send(f frame) bool {
	data, err := json.Marshal(f)
	if err != nil {
		return true // ein nicht darstellbarer Rahmen ist kein Verbindungsfehler
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.ws.SetWriteDeadline(time.Now().Add(eventWriteTimeout))
	return c.ws.WriteMessage(websocket.TextMessage, data) == nil
}

func (c *eventConn) sendHello() {
	f := frame{Type: "hello"}
	if c.hub.buildInfo != nil {
		f.Version, f.Build = c.hub.buildInfo()
	}
	c.send(f)
}

func (c *eventConn) sendSubscribed(topics map[string]topic) {
	names := make([]string, 0, len(topics))
	for name := range topics {
		names = append(names, name)
	}
	slices.Sort(names)
	c.send(frame{Type: "subscribed", Topics: names})
}

func (c *eventConn) sendError(e *apierr.Error, topic string) {
	c.send(frame{Type: "error", Code: e.Code, Error: e.Message, Topic: topic})
}
