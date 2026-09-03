// Package remote implementiert LCM Remote: den eingebetteten MQTT-Broker
// (mochi-mqtt, ohne eigene Netz-Listener - Verbindungen kommen als
// WebSocket-Upgrades über den bestehenden HTTPS-Listener herein) und den
// AgentHub, der verbundene lcm-agents verwaltet und Kommandos als
// Request/Response über MQTT ausführt.
//
// Der Hub implementiert services.AgentConnector: sein Conn(server) liefert
// eine sshx.Conn, damit Executor, Health-Check, Provisioning und die
// SSH-Protokollierung (Recorder-Dekorator) für Agent-Server unverändert
// funktionieren.
package remote

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"

	"github.com/google/uuid"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/remote/wire"
	"LCM/internal/safego"
	"LCM/internal/storage/repositories"
)

// listenerID kennzeichnet die über Fiber hereingereichten WebSocket-
// Verbindungen im Broker (rein informativ, es gibt keinen echten Listener).
const listenerID = "ws-fiber"

// defaultIdleTimeout ist die erlaubte Stille eines Agent-Kommandos, falls
// keine Einstellung verdrahtet ist - bewusst über dem Watchdog-Vorgabewert,
// damit im Regelfall zuerst der Job-Watchdog greift und sauber abbricht.
const defaultIdleTimeout = 45 * time.Minute

// ErrAgentOffline: der Agent des Servers ist nicht mit dem Broker verbunden.
// (Eigene Definition statt services.ErrAgentOffline, um keinen Importzyklus
// zu erzeugen - der Fehlertext trägt die Information.)
var ErrAgentOffline = errors.New("Agent nicht verbunden - der Server ist offline oder der lcm-agent läuft nicht")

// Hub ist Broker + Registry + RPC-Vermittler in einem.
type Hub struct {
	servers *repositories.ServerRepository
	log     *slog.Logger
	broker  *mqtt.Server

	// idleTimeout liefert die erlaubte Stille je Kommando (aus den globalen
	// Einstellungen + Puffer). Optional.
	idleTimeout func() time.Duration
	// onOnline wird nach dem Connect eines Agents asynchron aufgerufen,
	// sobald sein Inventar eintrifft (= cmd-Topic sicher abonniert) - z.B.
	// für den Erst-Scan eines frisch enrollten Servers. Optional.
	onOnline func(server *domain.Server)

	mu sync.Mutex
	// online hält je AgentID den aktuell verbundenen Broker-Client. Der
	// Client-Zeiger (statt bool) schützt gegen Session-Takeover-Rennen:
	// verbindet sich derselbe Agent neu, feuert der Disconnect des ALTEN
	// Clients evtl. nach dem Connect des neuen - ausgetragen wird nur,
	// wenn noch derselbe Client eingetragen ist.
	online  map[string]*mqtt.Client
	pending map[string]*pendingReq // Request-ID → Warteplatz
	// conns hält je AgentID die offenen agentConns. Trennt sich der Agent,
	// werden ALLE seine Conns geschlossen - sonst würde ein laufender
	// Mehr-Kommando-Ablauf (z.B. Scan) beim nächsten Kommando bis zum
	// vollen Timeout auf den toten Agenten warten, statt sofort zu scheitern.
	conns map[string]map[*agentConn]struct{}
}

// pendingReq ist ein wartender Kommando-Aufruf (Korrelation über die
// Request-ID im Result-Payload).
type pendingReq struct {
	agentID string
	ch      chan wire.Result // gepuffert (1), Zustellung blockiert nie
	// progress meldet die Lebenszeichen des laufenden Kommandos (siehe
	// wire.Result.Progress). Gepuffert (1) und mit select gespeist: Ein
	// Lebenszeichen darf die Broker-Goroutine nie aufhalten, und ein
	// verworfenes ist folgenlos - das nächste kommt in 30 Sekunden.
	progress chan struct{}
}

// New erstellt den Hub (Broker startet erst mit Start()).
func New(servers *repositories.ServerRepository, log *slog.Logger) *Hub {
	if log == nil {
		log = slog.Default()
	}
	return &Hub{
		servers: servers,
		log:     log,
		online:  map[string]*mqtt.Client{},
		pending: map[string]*pendingReq{},
		conns:   map[string]map[*agentConn]struct{}{},
	}
}

// WithIdleTimeout verdrahtet die erlaubte Stille je Kommando (globale
// Einstellung + Puffer). Optional.
func (h *Hub) WithIdleTimeout(fn func() time.Duration) *Hub {
	h.idleTimeout = fn
	return h
}

// WithOnAgentOnline verdrahtet den Connect-Callback (Erst-Scan). Optional.
func (h *Hub) WithOnAgentOnline(fn func(server *domain.Server)) *Hub {
	h.onOnline = fn
	return h
}

// Start erstellt und startet den eingebetteten Broker (ohne Listener) und
// richtet die Inline-Subscriptions für Ergebnisse und Inventar ein.
func (h *Hub) Start() error {
	caps := mqtt.NewDefaultServerCapabilities()
	caps.MaximumPacketSize = wire.MaxPacketSize // große apt-Ausgaben
	h.broker = mqtt.New(&mqtt.Options{
		InlineClient: true,
		Capabilities: caps,
		Logger:       h.log.With("comp", "mqtt"),
	})
	if err := h.broker.AddHook(&agentHook{hub: h}, nil); err != nil {
		return fmt.Errorf("mqtt-hook registrieren: %w", err)
	}
	if err := h.broker.Serve(); err != nil {
		return fmt.Errorf("mqtt-broker starten: %w", err)
	}
	// Ergebnisse und Inventar aller Agents zentral einsammeln (Inline-
	// Client, umgeht die ACL-Prüfung der echten Clients).
	if err := h.broker.Subscribe("lcm/a/+"+wire.TopicResSuf, 1, h.handleResult); err != nil {
		return err
	}
	if err := h.broker.Subscribe("lcm/a/+"+wire.TopicInvSuf, 2, h.handleInventory); err != nil {
		return err
	}
	return nil
}

// Close fährt den Broker herunter (trennt alle Agents).
func (h *Hub) Close() {
	if h.broker != nil {
		_ = h.broker.Close()
	}
}

// HandleConn reicht eine (bereits als WebSocket-net.Conn adaptierte)
// Verbindung in den Broker - blockiert bis zum Disconnect des Clients.
func (h *Hub) HandleConn(c net.Conn) error {
	return h.broker.EstablishConnection(listenerID, c)
}

// ---- services.AgentConnector ---------------------------------------------------

// Online meldet, ob der Agent aktuell verbunden ist.
func (h *Hub) Online(agentID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.online[agentID] != nil
}

// Disconnect trennt eine aktive Agent-Session (Token-Revocation,
// Server-Löschung). No-op, wenn der Agent nicht verbunden ist.
func (h *Hub) Disconnect(agentID string) {
	if h.broker == nil {
		return
	}
	if cl, ok := h.broker.Clients.Get(agentID); ok {
		_ = h.broker.DisconnectClient(cl, packets.ErrNotAuthorized)
	}
}

// Conn liefert die sshx.Conn zum Agent des Servers - Fehler, wenn der
// Agent offline ist. Die Verbindung ist leichtgewichtig (kein Handshake);
// ConnLimiter und SSHRecorder legt der ServerService darüber.
func (h *Hub) Conn(server *domain.Server) (sshx.Conn, error) {
	if server.AgentID == "" {
		return nil, errors.New("server hat keine agent-id")
	}
	if !h.Online(server.AgentID) {
		return nil, ErrAgentOffline
	}
	c := &agentConn{hub: h, agentID: server.AgentID, done: make(chan struct{})}
	h.mu.Lock()
	if h.conns[server.AgentID] == nil {
		h.conns[server.AgentID] = map[*agentConn]struct{}{}
	}
	h.conns[server.AgentID][c] = struct{}{}
	h.mu.Unlock()
	return c, nil
}

// forgetConn entfernt eine geschlossene Conn aus der Registry.
func (h *Hub) forgetConn(c *agentConn) {
	h.mu.Lock()
	if m := h.conns[c.agentID]; m != nil {
		delete(m, c)
		if len(m) == 0 {
			delete(h.conns, c.agentID)
		}
	}
	h.mu.Unlock()
}

// ---- Registry & Zustellung -----------------------------------------------------

// register legt einen Warteplatz für eine Request-ID an.
func (h *Hub) register(reqID, agentID string) *pendingReq {
	req := &pendingReq{
		agentID:  agentID,
		ch:       make(chan wire.Result, 1),
		progress: make(chan struct{}, 1),
	}
	h.mu.Lock()
	h.pending[reqID] = req
	h.mu.Unlock()
	return req
}

func (h *Hub) unregister(reqID string) {
	h.mu.Lock()
	delete(h.pending, reqID)
	h.mu.Unlock()
}

// handleResult stellt ein Agent-Ergebnis dem wartenden Aufrufer zu.
// Ergebnisse ohne Warteplatz (z.B. nach LCM-Neustart oder Timeout) werden
// verworfen - der zugehörige Job ist dann bereits fehlgeschlagen.
func (h *Hub) handleResult(cl *mqtt.Client, sub packets.Subscription, pk packets.Packet) {
	// Verarbeitet Nutzdaten eines fremden Agents in einer Broker-Goroutine -
	// ohne diesen Schutz wäre ein fehlerhaftes Paket ein Dienst-Ausfall.
	defer safego.Recover("mqtt:handle-result", nil)
	var res wire.Result
	if err := json.Unmarshal(pk.Payload, &res); err != nil || res.ID == "" {
		return
	}
	agentID := wire.AgentIDFromTopic(pk.TopicName)
	h.mu.Lock()
	req, ok := h.pending[res.ID]
	// Zustellung nur, wenn das Ergebnis vom richtigen Agent kommt - ein
	// Agent kann dank ACL ohnehin nur auf sein eigenes res-Topic schreiben,
	// aber die Request-ID könnte er raten; der Abgleich schließt das aus.
	if ok && req.agentID == agentID {
		// Ein Lebenszeichen schließt den Auftrag NICHT ab - der Warteplatz
		// bleibt bestehen, nur die Stille-Uhr wird zurückgesetzt.
		if !res.Progress {
			delete(h.pending, res.ID)
		}
	} else {
		ok = false
	}
	h.mu.Unlock()
	if !ok {
		return
	}
	if res.Progress {
		select {
		case req.progress <- struct{}{}:
		default:
		}
		return
	}
	req.ch <- res // gepuffert, blockiert nie
}

// handleInventory übernimmt Version/Hostinfo eines frisch verbundenen Agents.
func (h *Hub) handleInventory(cl *mqtt.Client, sub packets.Subscription, pk packets.Packet) {
	defer safego.Recover("mqtt:handle-inventory", nil)
	var inv wire.Inventory
	if err := json.Unmarshal(pk.Payload, &inv); err != nil {
		return
	}
	agentID := wire.AgentIDFromTopic(pk.TopicName)
	server, err := h.servers.FindByAgentID(agentID)
	if err != nil {
		return
	}
	_ = h.servers.UpdateFields(server.ID, map[string]any{
		"agent_version": inv.AgentVersion,
	})
	// Erst-Scan hier (nicht schon bei OnSessionEstablished) auslösen: das
	// Inventar sendet der Agent NACH dem Abonnieren des Kommando-Topics,
	// die cmd-Subscription ist also garantiert aktiv (sonst ginge das erste
	// Scan-Kommando bei CleanSession verloren).
	if h.onOnline != nil {
		// Startet den vollständigen Scan-Pfad (RefreshAll) - der wertet
		// Ausgaben des fremden Servers aus und gehört daher abgesichert.
		safego.Go("agent-online:"+server.Name, func() { h.onOnline(server) })
	}
}

// agentConnected verbucht einen erfolgreichen Agent-Connect: Registry,
// Health-Felder und (asynchron) der Online-Callback.
func (h *Hub) agentConnected(cl *mqtt.Client) {
	agentID := cl.ID
	h.mu.Lock()
	h.online[agentID] = cl
	h.mu.Unlock()

	server, err := h.servers.FindByAgentID(agentID)
	if err != nil {
		return
	}
	now := time.Now()
	_ = h.servers.UpdateFields(server.ID, map[string]any{
		"reachable":          true,
		"last_error":         "",
		"agent_last_seen_at": now,
		"last_seen_at":       now,
	})
	h.log.Info("agent connected", "server", server.Name, "agent_id", agentID)
	// Der Erst-Scan wird NICHT hier ausgelöst, sondern in handleInventory -
	// erst dann steht fest, dass der Agent das Kommando-Topic abonniert hat.
}

// agentDisconnected verbucht die Trennung: Registry, Health-Felder und
// das Scheitern aller noch wartenden Kommandos dieses Agents. Bei einem
// Session-Takeover (derselbe Agent hat sich neu verbunden) ist bereits der
// NEUE Client eingetragen - dann passiert hier nichts.
func (h *Hub) agentDisconnected(cl *mqtt.Client) {
	agentID := cl.ID
	h.mu.Lock()
	if h.online[agentID] != cl {
		h.mu.Unlock()
		return
	}
	delete(h.online, agentID)
	var waiters []*pendingReq
	for id, req := range h.pending {
		if req.agentID == agentID {
			waiters = append(waiters, req)
			delete(h.pending, id)
		}
	}
	// Alle offenen Conns dieses Agenten einsammeln - sie werden unten
	// geschlossen, damit auch FOLGE-Kommandos (z.B. der nächste Scan-
	// Schritt) sofort scheitern statt aufs Timeout zu warten.
	var deadConns []*agentConn
	for c := range h.conns[agentID] {
		deadConns = append(deadConns, c)
	}
	delete(h.conns, agentID)
	h.mu.Unlock()
	for _, req := range waiters {
		req.ch <- wire.Result{ExitCode: -1, Error: "agent während der ausführung getrennt"}
	}
	for _, c := range deadConns {
		c.markDead()
	}

	server, err := h.servers.FindByAgentID(agentID)
	if err != nil {
		return
	}
	now := time.Now()
	_ = h.servers.UpdateFields(server.ID, map[string]any{
		"reachable":          false,
		"last_error":         "Agent offline (Verbindung getrennt)",
		"agent_last_seen_at": now,
	})
	h.log.Info("agent disconnected", "server", server.Name, "agent_id", agentID)
}

// commandIdleTimeout liefert die erlaubte Stille je Kommando.
func (h *Hub) commandIdleTimeout() time.Duration {
	if h.idleTimeout != nil {
		if d := h.idleTimeout(); d > 0 {
			return d
		}
	}
	return defaultIdleTimeout
}

// publishCommand schickt ein Command-Payload auf das cmd-Topic des Agents.
func (h *Hub) publishCommand(agentID string, cmd wire.Command) error {
	payload, err := json.Marshal(cmd)
	if err != nil {
		return err
	}
	return h.broker.Publish(wire.TopicCmd(agentID), payload, false, 1)
}

// ---- sshx.Conn über MQTT --------------------------------------------------------

// agentConn führt Kommandos als MQTT-Request/Response aus. Close() entsperrt
// wartende Aufrufe und bricht das laufende Kommando im Agent ab - darüber
// greifen Job-Abort und der Laufzeit-Watchdog exakt wie beim SSH-Transport.
type agentConn struct {
	hub     *Hub
	agentID string

	mu         sync.Mutex
	closed     bool
	dead       bool   // Agent getrennt - Folge-Kommandos scheitern sofort
	onActivity func() // Lebenszeichen-Rückruf, siehe sshx.Conn.OnActivity
	done       chan struct{}
}

// OnActivity hinterlegt den Lebenszeichen-Rückruf dieser Verbindung.
func (c *agentConn) OnActivity(fn func()) {
	c.mu.Lock()
	c.onActivity = fn
	c.mu.Unlock()
}

func (c *agentConn) activity() {
	c.mu.Lock()
	fn := c.onActivity
	c.mu.Unlock()
	if fn != nil {
		fn()
	}
}

func (c *agentConn) Run(cmd string) (string, int, error) {
	return c.RunStdin(cmd, "")
}

// markDead entsperrt wartende und lässt künftige Kommandos sofort scheitern
// (Agent hat sich getrennt). Anders als Close() sendet es kein Cancel - der
// Agent ist ohnehin weg.
func (c *agentConn) markDead() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		c.closed = true
		c.dead = true
		close(c.done)
	}
}

func (c *agentConn) RunStdin(cmd, stdin string) (string, int, error) {
	c.mu.Lock()
	dead := c.dead
	if c.closed {
		c.mu.Unlock()
		if dead {
			return "", -1, ErrAgentOffline
		}
		return "", -1, errors.New("verbindung geschlossen")
	}
	c.mu.Unlock()

	c.activity()
	defer c.activity()

	reqID := uuid.NewString()
	req := c.hub.register(reqID, c.agentID)
	defer c.hub.unregister(reqID)

	idle := c.hub.commandIdleTimeout()
	// Der Agent bekommt dieselbe Frist mitgegeben: Bricht die Verbindung zum
	// Server weg, beendet er das hängende Kommando von sich aus.
	command := wire.Command{ID: reqID, Cmd: cmd, Stdin: stdin, IdleTimeoutSec: int(idle.Seconds())}
	if err := c.hub.publishCommand(c.agentID, command); err != nil {
		return "", -1, fmt.Errorf("kommando senden: %w", err)
	}

	// Gewartet wird auf STILLE, nicht auf Gesamtdauer: Jedes Lebenszeichen
	// des Agents stellt die Uhr zurück, ein arbeitendes Kommando darf
	// deshalb beliebig lange laufen.
	timeout := time.NewTimer(idle)
	defer timeout.Stop()
	for {
		select {
		case <-req.progress:
			c.activity()
			timeout.Reset(idle)
			continue
		case res := <-req.ch:
			out := res.Output
			if res.Truncated {
				out += "\n[Ausgabe vom Agent gekürzt - Limit überschritten]"
			}
			if res.Error != "" {
				return out, res.ExitCode, errors.New(res.Error)
			}
			return out, res.ExitCode, nil
		case <-c.done:
			c.mu.Lock()
			dead := c.dead
			c.mu.Unlock()
			if dead {
				// Agent getrennt - kein Cancel nötig, er ist weg.
				return "", -1, ErrAgentOffline
			}
			// Job-Abort/Watchdog hat die Verbindung geschlossen - Kommando im
			// Agent abbrechen (Kill der Prozessgruppe).
			_ = c.hub.publishCommand(c.agentID, wire.Command{ID: reqID, Cancel: true})
			return "", -1, errors.New("kommando abgebrochen (verbindung geschlossen)")
		case <-timeout.C:
			_ = c.hub.publishCommand(c.agentID, wire.Command{ID: reqID, Cancel: true})
			return "", -1, fmt.Errorf("ohne lebenszeichen - kommando nach %s stille abgebrochen", idle)
		}
	}
}

func (c *agentConn) Close() error {
	c.mu.Lock()
	if !c.closed {
		c.closed = true
		close(c.done)
	}
	c.mu.Unlock()
	c.hub.forgetConn(c)
	return nil
}

// ---- Broker-Hook (Auth, ACL, Session-Events) ------------------------------------

// agentHook bindet Authentifizierung, ACL und Session-Events des Brokers
// an den Hub. Agents authentifizieren sich mit Username=AgentID (UUID) und
// Passwort=Token-Secret; die ACL sperrt jeden Agent strikt auf sein
// eigenes Topic-Subtree.
type agentHook struct {
	mqtt.HookBase
	hub *Hub
}

func (a *agentHook) ID() string { return "lcm-agents" }

func (a *agentHook) Provides(b byte) bool {
	switch b {
	case mqtt.OnConnectAuthenticate, mqtt.OnACLCheck,
		mqtt.OnSessionEstablished, mqtt.OnDisconnect:
		return true
	}
	return false
}

// Panic-Schutz der Hooks: Diese Callbacks laufen in den Goroutinen des
// MQTT-Brokers - also außerhalb JEDER Recover-Middleware. Der Weg dorthin
// führt über einen fasthttp-Hijack (eigene Goroutine, kein recover) in den
// mochi-Broker (ebenfalls kein recover). Ein Panic hier würde daher den
// gesamten Dienst beenden, und OnConnectAuthenticate ist noch dazu durch
// UNAUTHENTIFIZIERTE Pakete auf dem Agent-Port erreichbar.
//
// Deshalb ist jeder Hook einzeln abgesichert. Sicherheits-Regel dabei: Ein
// abgefangener Panic gilt als Ablehnung, niemals als Zustimmung.

// OnConnectAuthenticate prüft AgentID + Token-Secret gegen die DB.
func (a *agentHook) OnConnectAuthenticate(cl *mqtt.Client, pk packets.Packet) (ok bool) {
	// Fail-closed: Bei einem Panic bleibt der benannte Rückgabewert false,
	// die Verbindung wird also abgelehnt.
	defer safego.Recover("mqtt-hook:authenticate", nil)
	agentID := string(pk.Connect.Username)
	// Client-ID MUSS der AgentID entsprechen - sonst könnte ein gültiger
	// Agent mit fremder Client-ID die Session eines anderen übernehmen.
	if agentID == "" || cl.ID != agentID {
		return false
	}
	server, err := a.hub.servers.FindByAgentID(agentID)
	if err != nil || !server.IsAgent() || server.AgentTokenHash == "" {
		return false
	}
	presented := wire.HashSecret(string(pk.Connect.Password))
	return subtle.ConstantTimeCompare([]byte(presented), []byte(server.AgentTokenHash)) == 1
}

// OnACLCheck sperrt Agents auf ihr eigenes Subtree: subscribe nur das
// cmd-Topic, publish nur res/inv/status. Der Inline-Client (Serverseite)
// durchläuft diesen Hook nicht.
func (a *agentHook) OnACLCheck(cl *mqtt.Client, topic string, write bool) (ok bool) {
	// Fail-closed wie bei der Authentifizierung: Panic => Zugriff verweigert.
	defer safego.Recover("mqtt-hook:acl", nil)
	if cl.Net.Inline {
		return true
	}
	id := cl.ID
	if write {
		return topic == wire.TopicRes(id) || topic == wire.TopicInv(id) || topic == wire.TopicStatus(id)
	}
	return topic == wire.TopicCmd(id)
}

func (a *agentHook) OnSessionEstablished(cl *mqtt.Client, pk packets.Packet) {
	defer safego.Recover("mqtt-hook:session-established", nil)
	if cl.Net.Inline {
		return
	}
	a.hub.agentConnected(cl)
}

func (a *agentHook) OnDisconnect(cl *mqtt.Client, err error, expire bool) {
	defer safego.Recover("mqtt-hook:disconnect", nil)
	if cl.Net.Inline {
		return
	}
	a.hub.agentDisconnected(cl)
}
