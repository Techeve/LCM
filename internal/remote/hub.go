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
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"

	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"

	"github.com/google/uuid"

	"LCM/internal/core/agentcaps"
	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/remote/wire"
	"LCM/internal/safego"
	"LCM/internal/storage/repositories"
)

// listenerID kennzeichnet die über Fiber hereingereichten WebSocket-
// Verbindungen im Broker (rein informativ, es gibt keinen echten Listener).
const listenerID = "ws-fiber"

// defaultIdleTimeout ist die erlaubte Stille eines Kommandos an einen Agent
// OHNE Jobs, falls keine Einstellung verdrahtet ist - bewusst über dem
// Watchdog-Vorgabewert, damit im Regelfall zuerst der Job-Watchdog greift und
// sauber abbricht. Agenten mit Jobs kennen keine Stille-Frist.
const defaultIdleTimeout = 45 * time.Minute

// defaultMaxSilence ist die längste Zeit ohne jedes Lebenszeichen des Agents,
// die ein Aufruf abwartet. Danach gibt der Aufrufer mit ErrResultUnknown auf,
// damit nichts ewig blockiert - das Kommando selbst bleibt unberührt. Mit dem
// Ergebnis ist erst nach Stunden zu rechnen, wenn der Agent während eines langen
// Laufs verschwindet; 24 Stunden sind weit darüber.
const defaultMaxSilence = 24 * time.Hour

// defaultJobSilence: Ein Job meldet sich alle ProgressInterval - auch einer, der
// auf das vorige Kommando wartet oder von einem neuen Agent übernommen wurde. Nach
// dem Fünffachen ohne Lebenszeichen bei verbundenem Agent hat er das Kommando nie
// bekommen. Nur die Wartezeit bei GETRENNTEM Agent reicht bis maxSilence.
const defaultJobSilence = 5 * wire.ProgressInterval

// maxQueuedCancels begrenzt die Abbrüche je Agent, die auf seine Rückkehr
// warten.
const maxQueuedCancels = 256

// ErrCommandUnknown: Der Agent ist verbunden, kennt das Kommando aber offenbar
// nicht: Er meldet es nicht in seinem Inventar oder seit langem kein Lebenszeichen
// dazu. Meist ist es ihm nie zugestellt worden (die Verbindung riss beim Senden
// ab). LCM hält nichts an, was dort doch läuft.
var ErrCommandUnknown = errors.New("der Agent kennt das Kommando nicht - es ist vermutlich nie bei ihm angekommen; bitte erneut versuchen")

// ErrAgentOffline: der Agent des Servers ist nicht mit dem Broker verbunden.
// (Eigene Definition statt services.ErrAgentOffline, um keinen Importzyklus
// zu erzeugen - der Fehlertext trägt die Information.)
var ErrAgentOffline = errors.New("Agent nicht verbunden - der Server ist offline oder der lcm-agent läuft nicht")

// ErrResultUnknown: Der Agent hat sich so lange nicht gemeldet, dass LCM nicht
// mehr auf das Ergebnis wartet. Ob das Kommando noch läuft oder fertig ist,
// weiß LCM nicht; angehalten hat es nichts.
var ErrResultUnknown = errors.New("ergebnis unbekannt - der Agent hat sich lange nicht gemeldet; das Kommando wurde nicht angehalten, ein später eintreffendes Ergebnis wird im Job nachgetragen")

// JobTracker verbindet die Kommandos des Hubs mit den LCM-Jobs, für die sie
// laufen. So findet ein Ergebnis, das erst nach dem Ende des Jobs eintrifft
// (LCM-Neustart, aufgegebenes Warten), seinen Job wieder. Optional.
type JobTracker interface {
	// CommandSent: Der Job schickt das Kommando reqID an einen Agent mit Jobs.
	// Läuft synchron VOR dem Senden.
	CommandSent(jobID, reqID string)
	// CommandDone: Der Ausgang von reqID ist bekannt (Ergebnis erhalten, nie
	// angekommen, abgebrochen) - ein späteres Ergebnis gibt es nicht mehr.
	CommandDone(jobID, reqID string)
	// LateResult trägt ein Ergebnis nach, auf das niemand mehr wartet. Gibt
	// zurück, ob ein Job des Agents es übernommen hat.
	LateResult(agentID string, res wire.Result) bool
}

// Hub ist Broker + Registry + RPC-Vermittler in einem.
type Hub struct {
	servers *repositories.ServerRepository
	log     *slog.Logger
	broker  *mqtt.Server
	// knownIPs sind die IPs, von denen sich ein Agent erfolgreich angemeldet
	// hat; das Upgrade-Ratelimit (ws.go) lässt sie öfter durch.
	knownIPs *knownAgentIPs

	// idleTimeout liefert die erlaubte Stille je Kommando an einen Agent ohne
	// Jobs (aus den globalen Einstellungen + Puffer). Optional.
	idleTimeout func() time.Duration
	// maxSilence ist die längste Zeit ohne Lebenszeichen, die ein Aufruf
	// abwartet (defaultMaxSilence).
	maxSilence time.Duration
	// jobSilence ist die Zeit, nach der ein Aufruf an einen VERBUNDENEN Agent mit
	// Jobs ohne jedes Lebenszeichen scheitert (defaultJobSilence). Ein Job
	// meldet sich alle wire.ProgressInterval, solange der Agent verbunden ist;
	// bleibt das aus, kennt der Agent das Kommando nicht.
	jobSilence time.Duration
	// onOnline wird nach dem Connect eines Agents asynchron aufgerufen,
	// sobald sein Inventar eintrifft (= cmd-Topic sicher abonniert) - z.B.
	// für den Erst-Scan eines frisch enrollten Servers. Optional.
	onOnline func(server *domain.Server)
	// tracker ordnet Kommandos ihren Jobs zu (WithJobTracker). Optional.
	tracker JobTracker

	mu sync.Mutex
	// online hält je AgentID den aktuell verbundenen Broker-Client. Der
	// Client-Zeiger (statt bool) schützt gegen Session-Takeover-Rennen:
	// verbindet sich derselbe Agent neu, feuert der Disconnect des ALTEN
	// Clients evtl. nach dem Connect des neuen - ausgetragen wird nur,
	// wenn noch derselbe Client eingetragen ist.
	online map[string]*mqtt.Client
	// connectedAt ist der Zeitpunkt, zu dem der Agent seine jüngste Verbindung
	// aufgebaut hat.
	connectedAt map[string]time.Time
	pending     map[string]*pendingReq // Request-ID → Warteplatz
	// conns hält je AgentID die offenen agentConns. Trennt sich ein Agent ohne
	// wire.CapabilityJobs, werden ALLE seine Conns geschlossen - sonst würde ein
	// laufender Mehr-Kommando-Ablauf (z.B. Scan) beim nächsten Kommando auf den
	// toten Agenten warten, statt sofort zu scheitern. Ein neuer Aufruf auf der
	// Conn eines getrennten Agents mit Jobs scheitert ebenso sofort (RunStdin).
	conns map[string]map[*agentConn]struct{}
	// sockets hält die Verbindungen in HandleConn, auch solche ohne CONNECT.
	sockets map[net.Conn]struct{}
	// preAuth begrenzt die Verbindungen, die sich noch nicht angemeldet haben (S-07).
	preAuth *preAuthGate
	// off: Der Agent-Zugang ist ausgeschaltet (SetAccepting); neue Verbindungen
	// werden abgewiesen. Der Nullwert heißt: an.
	off bool
	// ttys hält die offenen Web-Konsolen je Sitzungsname (tty.go).
	ttys map[string]*agentTerminal

	// pingInterval ist der Takt der Pings an Agenten mit CapabilityPing.
	pingInterval time.Duration
	// pingable sind die Agenten, die auf ihrer aktuellen Verbindung
	// CapabilityPing gemeldet haben; pongs die Zeit der letzten Antwort je
	// Agent (bleibt nach der Trennung stehen: „zuletzt gehört").
	pingable map[string]struct{}
	pongs    map[string]time.Time
	// jobs sind die Agenten, die in ihrem jüngsten Inventar wire.CapabilityJobs
	// gemeldet haben: Ihre Kommandos überleben Abbrüche, LCM wartet auf sie
	// (siehe Kopf von wire).
	jobs map[string]struct{}
	// cancels sind Abbrüche, die LCM einem getrennten Agent nicht zustellen
	// konnte; sie gehen nach seiner Rückkehr hinaus.
	cancels map[string][]string
	// links ist die Verbindungsbilanz je Agent (link.go).
	links *linkBook
	// done beendet die Hintergrundschleifen (Close).
	done     chan struct{}
	doneOnce sync.Once
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
	// durable: Der Agent führt das Kommando als Job aus, der Abbrüche überlebt.
	// Trennt er sich, wartet der Aufruf auf seine Rückkehr; sonst scheitert er.
	durable bool
	// vitals ist, was der Agent zuletzt am Kommando gemeldet hat, und at, wann.
	vitals *wire.Vitals
	at     time.Time
	// sent ist der Zeitpunkt des Abschickens, sign der des jüngsten
	// Lebenszeichens (zu Anfang sent).
	sent, sign time.Time
	// failure ist der Fehler, mit dem failWaiters den Aufruf beendet hat. Er wird
	// vor dem Senden auf ch gesetzt; await gibt ihn unverändert zurück, damit
	// der Aufrufer die Sentinel-Fehler erkennt (errors.Is).
	failure error
}

// New erstellt den Hub (Broker startet erst mit Start()).
func New(servers *repositories.ServerRepository, log *slog.Logger) *Hub {
	if log == nil {
		log = slog.Default()
	}
	return &Hub{
		servers:     servers,
		log:         log,
		knownIPs:    newKnownAgentIPs(),
		online:      map[string]*mqtt.Client{},
		connectedAt: map[string]time.Time{},
		pending:     map[string]*pendingReq{},
		conns:       map[string]map[*agentConn]struct{}{},
		sockets:     map[net.Conn]struct{}{},
		preAuth:     &preAuthGate{limits: defaultPreAuthLimits()},
		ttys:        map[string]*agentTerminal{},

		pingInterval: wire.PingInterval,
		pingable:     map[string]struct{}{},
		pongs:        map[string]time.Time{},
		jobs:         map[string]struct{}{},
		cancels:      map[string][]string{},
		links:        newLinkBook(),
		maxSilence:   defaultMaxSilence,
		jobSilence:   defaultJobSilence,
		done:         make(chan struct{}),
	}
}

// WithPingInterval verkürzt den Takt der Pings (Tests). Vor Start aufrufen.
func (h *Hub) WithPingInterval(d time.Duration) *Hub {
	h.pingInterval = d
	return h
}

// WithIdleTimeout verdrahtet die erlaubte Stille je Kommando an einen Agent
// ohne Jobs (globale Einstellung + Puffer). Agenten mit Jobs bekommen keine.
// Optional.
func (h *Hub) WithIdleTimeout(fn func() time.Duration) *Hub {
	h.idleTimeout = fn
	return h
}

// commandIdleTimeout liefert die erlaubte Stille je Kommando an einen Agent
// ohne Jobs.
func (h *Hub) commandIdleTimeout() time.Duration {
	if h.idleTimeout != nil {
		if d := h.idleTimeout(); d > 0 {
			return d
		}
	}
	return defaultIdleTimeout
}

// WithMaxSilence legt fest, wie lange ein Aufruf ohne jedes Lebenszeichen des
// Agents wartet, bevor er mit ErrResultUnknown aufgibt (Vorgabe 24 Stunden).
// Vor Start aufrufen.
func (h *Hub) WithMaxSilence(d time.Duration) *Hub {
	h.maxSilence = d
	return h
}

// WithJobSilence legt fest, wie lange ein Aufruf an einen verbundenen Agent mit
// Jobs ohne Lebenszeichen wartet, bevor er scheitert (Vorgabe das Fünffache von
// wire.ProgressInterval). Vor Start aufrufen.
func (h *Hub) WithJobSilence(d time.Duration) *Hub {
	h.jobSilence = d
	return h
}

// WithOnAgentOnline verdrahtet den Connect-Callback (Erst-Scan). Optional.
func (h *Hub) WithOnAgentOnline(fn func(server *domain.Server)) *Hub {
	h.onOnline = fn
	return h
}

// WithJobTracker verdrahtet die Zuordnung der Kommandos zu ihren Jobs, damit
// späte Ergebnisse im Job landen. Optional. Vor Start aufrufen.
func (h *Hub) WithJobTracker(t JobTracker) *Hub {
	h.tracker = t
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
	if err := h.broker.Subscribe("lcm/a/+"+wire.TopicTTOSuf, 3, h.handleTTY); err != nil {
		return err
	}
	safego.Go("agent-pings", h.pingLoop)
	return nil
}

// Close fährt den Broker herunter. Die Verbindungen trennt es selbst: Sie kommen
// über HandleConn, an keinem Listener des Brokers. Dessen Close trennt sie deshalb
// nicht, wartet aber auf ihr Ende und käme nie zurück. Das gilt auch für eine
// Verbindung ohne CONNECT, die der Broker noch gar nicht als Client führt.
func (h *Hub) Close() {
	if h.broker == nil {
		return
	}
	// Ein zweites Close ist folgenlos: Der Broker verträgt es nicht (er schließt
	// einen Kanal doppelt).
	h.doneOnce.Do(h.shutdown)
}

// shutdown ist der Rumpf von Close, der nur einmal läuft.
func (h *Hub) shutdown() {
	close(h.done)
	h.mu.Lock()
	h.expectDropOfOnline(domain.DropLCM)
	for c := range h.sockets {
		_ = c.Close()
	}
	waiters := h.takeAllPending()
	h.mu.Unlock()
	// Auch Aufrufe an Agenten mit Jobs: Nach dem Schließen kann kein Ergebnis
	// mehr bei diesem Hub ankommen.
	failWaiters(waiters, errHubClosed)
	_ = h.broker.Close()
}

var errHubClosed = errors.New("lcm wird beendet")

// errAgentAccessOff: Der Agent-Zugang ist ausgeschaltet.
var errAgentAccessOff = errors.New("agent access is switched off")

// SetAccepting schaltet den Agent-Zugang (Einstellung agent_enabled). Aus
// trennt alle verbundenen Agenten und weist neue Verbindungen ab, auf welchem
// Weg sie auch kommen (eigener Port oder gemeinsamer Port). Für nil ein No-op.
func (h *Hub) SetAccepting(on bool) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.off = !on
	var open []net.Conn
	var waiters []*pendingReq
	if !on {
		h.expectDropOfOnline(domain.DropAccessOff)
		for c := range h.sockets {
			open = append(open, c)
		}
		waiters = h.takeAllPending()
	}
	h.mu.Unlock()
	for _, c := range open {
		_ = c.Close()
	}
	failWaiters(waiters, errAgentAccessOff)
}

// expectDropOfOnline vermerkt für alle verbundenen Agenten, warum LCM sie gleich
// trennt. Der Aufrufer hält h.mu.
func (h *Hub) expectDropOfOnline(cause string) {
	for agentID := range h.online {
		h.links.expect(agentID, cause)
	}
}

// accepting meldet, ob der Agent-Zugang an ist (SetAccepting).
func (h *Hub) accepting() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return !h.off
}

// HandleConn reicht eine (bereits als WebSocket-net.Conn adaptierte)
// Verbindung in den Broker - blockiert bis zum Disconnect des Clients. Bis zur
// Anmeldung gelten die Grenzen aus preauth.go: höchstens preAuthMaxConns solche
// Verbindungen, ein CONNECT binnen preAuthDeadline und nicht über preAuthMaxPacket.
func (h *Hub) HandleConn(c net.Conn) error {
	h.mu.Lock()
	if h.off {
		h.mu.Unlock()
		_ = c.Close()
		return errAgentAccessOff
	}
	h.sockets[c] = struct{}{}
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.sockets, c)
		h.mu.Unlock()
	}()
	guarded, ok := h.preAuth.enter(c)
	if !ok {
		_ = c.Close()
		return errPreAuthFull
	}
	defer guarded.release()
	_ = c.SetReadDeadline(time.Now().Add(h.preAuth.limits.deadline))
	return h.broker.EstablishConnection(listenerID, guarded)
}

// PreAuthFull meldet, ob schon zu viele Verbindungen auf ihre Anmeldung warten.
// Der WebSocket-Handler weist dann vor dem Upgrade mit 503 ab.
func (h *Hub) PreAuthFull() bool { return h.preAuth.full() }

// ---- services.AgentConnector ---------------------------------------------------

// Online meldet, ob der Agent aktuell verbunden ist.
func (h *Hub) Online(agentID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.online[agentID] != nil
}

// ConnectedPeers liefert die verbundenen Agenten mit ihrer Gegenadresse, wie
// der WebSocket-Handler sie ermittelt hat (hinter einem vertrauten Proxy die
// weitergeleitete). Für die Prüfung, wen eine engere Netzliste aussperrt.
func (h *Hub) ConnectedPeers() []domain.AgentPeer {
	h.mu.Lock()
	clients := make(map[string]*mqtt.Client, len(h.online))
	for agentID, cl := range h.online {
		clients[agentID] = cl
	}
	h.mu.Unlock()

	peers := make([]domain.AgentPeer, 0, len(clients))
	for agentID, cl := range clients {
		peer := domain.AgentPeer{AgentID: agentID, Name: agentID}
		if server, err := h.servers.FindByAgentID(agentID); err == nil {
			peer.Name = server.Name
		}
		if remote, err := netip.ParseAddrPort(cl.Net.Remote); err == nil {
			peer.Addr = remote.Addr().Unmap()
		}
		peers = append(peers, peer)
	}
	slices.SortFunc(peers, func(a, b domain.AgentPeer) int { return strings.Compare(a.Name, b.Name) })
	return peers
}

// Disconnect trennt eine aktive Agent-Session (Token-Revocation,
// Server-Löschung). No-op, wenn der Agent nicht verbunden ist.
//
// Das Trennen ist gewollt, der Agent kommt nicht wieder: Wartende Aufrufe
// scheitern, statt auf eine Rückkehr zu warten.
func (h *Hub) Disconnect(agentID string) {
	if h.broker == nil {
		return
	}
	if cl, ok := h.broker.Clients.Get(agentID); ok {
		h.links.expect(agentID, domain.DropRevoked)
		_ = h.broker.DisconnectClient(cl, packets.ErrNotAuthorized)
	}
	h.mu.Lock()
	waiters := h.takePending(agentID)
	h.mu.Unlock()
	failWaiters(waiters, ErrAgentOffline)
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
	now := time.Now()
	req := &pendingReq{
		agentID:  agentID,
		ch:       make(chan wire.Result, 1),
		progress: make(chan struct{}, 1),
		sent:     now,
		sign:     now,
	}
	h.mu.Lock()
	_, req.durable = h.jobs[agentID]
	h.pending[reqID] = req
	h.mu.Unlock()
	return req
}

// takePending nimmt die wartenden Aufrufe eines Agents aus der Registry (h.mu
// gehalten).
func (h *Hub) takePending(agentID string) []*pendingReq {
	var waiters []*pendingReq
	for id, req := range h.pending {
		if req.agentID == agentID {
			waiters = append(waiters, req)
			delete(h.pending, id)
		}
	}
	return waiters
}

// takeAllPending nimmt alle wartenden Aufrufe aus der Registry (h.mu gehalten).
func (h *Hub) takeAllPending() []*pendingReq {
	var waiters []*pendingReq
	for id, req := range h.pending {
		waiters = append(waiters, req)
		delete(h.pending, id)
	}
	return waiters
}

// failWaiters lässt die Aufrufe mit err scheitern.
func failWaiters(waiters []*pendingReq, err error) {
	for _, req := range waiters {
		req.failure = err
		req.ch <- wire.Result{ExitCode: -1, Error: err.Error()}
	}
}

func (h *Hub) unregister(reqID string) {
	h.mu.Lock()
	delete(h.pending, reqID)
	h.mu.Unlock()
}

// handleResult stellt ein Agent-Ergebnis dem wartenden Aufrufer zu. Ein
// Ergebnis ohne Warteplatz (LCM wurde neu gestartet, oder der Aufrufer hat nach
// ErrResultUnknown aufgegeben) lässt sich nicht mehr zustellen; es wird im Job
// nachgetragen, zu dem das Kommando gehörte (JobTracker), und protokolliert.
func (h *Hub) handleResult(cl *mqtt.Client, sub packets.Subscription, pk packets.Packet) {
	// Verarbeitet Nutzdaten eines fremden Agents in einer Broker-Goroutine -
	// ohne diesen Schutz wäre ein fehlerhaftes Paket ein Dienst-Ausfall.
	defer safego.Recover("mqtt:handle-result", nil)
	var res wire.Result
	if err := json.Unmarshal(pk.Payload, &res); err != nil {
		return
	}
	agentID := wire.AgentIDFromTopic(pk.TopicName)
	if res.Pong {
		h.recordPong(agentID)
		return
	}
	if res.ID == "" {
		return
	}
	h.mu.Lock()
	req, ok := h.pending[res.ID]
	// Zustellung nur, wenn das Ergebnis vom richtigen Agent kommt - ein
	// Agent kann dank ACL ohnehin nur auf sein eigenes res-Topic schreiben,
	// aber die Request-ID könnte er raten; der Abgleich schließt das aus.
	if ok && req.agentID == agentID {
		// Ein Lebenszeichen schließt den Auftrag NICHT ab - der Warteplatz
		// bleibt bestehen, nur die Uhr des Aufrufers wird zurückgesetzt.
		if res.Progress {
			req.vitals, req.at = res.Vitals, time.Now()
			req.sign = req.at
		} else {
			delete(h.pending, res.ID)
		}
	} else {
		ok = false
	}
	h.mu.Unlock()
	if !ok {
		h.lateResult(agentID, res)
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

// lateResult behandelt ein Ergebnis, für das niemand mehr wartet: Gehört es zu
// einem Job, trägt der den Ausgang nach; in jedem Fall wird es protokolliert.
// Lebenszeichen zu einem unbekannten Aufruf sind nichts Besonderes (er ist
// längst zu Ende).
func (h *Hub) lateResult(agentID string, res wire.Result) {
	if res.Progress {
		return
	}
	recorded := h.tracker != nil && h.tracker.LateResult(agentID, res)
	h.log.Info("result without a waiting caller (late result)", "agent_id", agentID, "id", res.ID,
		"exit", res.ExitCode, "error", res.Error, "output_bytes", len(res.Output), "recovered", res.Recovered,
		"recorded_on_job", recorded)
}

// handleInventory übernimmt Version/Hostinfo eines frisch verbundenen Agents.
func (h *Hub) handleInventory(cl *mqtt.Client, sub packets.Subscription, pk packets.Packet) {
	defer safego.Recover("mqtt:handle-inventory", nil)
	var inv wire.Inventory
	if err := json.Unmarshal(pk.Payload, &inv); err != nil {
		return
	}
	inv = boundedInventory(inv)
	agentID := wire.AgentIDFromTopic(pk.TopicName)
	agent := agentcaps.FromInventory(inv)
	// Hub-Zustand (Fähigkeiten, offene Aufrufe, Anpingen) braucht die Serverzeile
	// nicht und läuft daher auch ohne sie.
	server, err := h.servers.FindByAgentID(agentID)
	if err != nil {
		h.log.Error("inventory: server lookup failed", "agent_id", agentID, "error", err)
	} else {
		h.storeInventory(server, inv, agent)
	}
	h.rememberCapabilities(agentID, agent)
	h.failUnreachable(cl, agentID, agent, inv.KnownJobs)
	h.startPinging(agentID, agent)
	h.flushCancels(agentID)
	// Erst-Scan hier (nicht schon bei OnSessionEstablished) auslösen: das
	// Inventar sendet der Agent NACH dem Abonnieren des Kommando-Topics,
	// die cmd-Subscription ist also garantiert aktiv (sonst ginge das erste
	// Scan-Kommando bei CleanSession verloren).
	if h.onOnline != nil && server != nil {
		// Startet den vollständigen Scan-Pfad (RefreshAll) - der wertet
		// Ausgaben des fremden Servers aus und gehört daher abgesichert.
		safego.Go("agent-online:"+server.Name, func() { h.onOnline(server) })
	}
}

// Obergrenzen für das, was ein Agent über sich melden darf und LCM in der
// Datenbank ablegt. Echte Werte sind kurz (Version, Hex-Pins, ein paar
// Fähigkeiten); ein übernommener Agent könnte sonst bei jedem Verbindungsaufbau
// ein Paket bis zur Brokergrenze (wire.MaxPacketSize) in die Serverzeile schreiben.
const (
	maxInventoryField        = 256
	maxInventoryCapabilities = 32
)

// boundedInventory kürzt die Felder des Inventars, die gespeichert werden, auf
// vernünftige Längen. Gültige Inventare bleiben unverändert.
func boundedInventory(inv wire.Inventory) wire.Inventory {
	clip := func(s string) string {
		if len(s) <= maxInventoryField {
			return s
		}
		return strings.ToValidUTF8(s[:maxInventoryField], "")
	}
	inv.AgentVersion, inv.KeyPin, inv.CAPin = clip(inv.AgentVersion), clip(inv.KeyPin), clip(inv.CAPin)
	inv.RolledBack, inv.RolledBackTo, inv.RolledBackReason = clip(inv.RolledBack), clip(inv.RolledBackTo), clip(inv.RolledBackReason)
	if len(inv.Capabilities) > maxInventoryCapabilities {
		inv.Capabilities = inv.Capabilities[:maxInventoryCapabilities]
	}
	inv.Capabilities = slices.Clone(inv.Capabilities)
	for i, c := range inv.Capabilities {
		inv.Capabilities[i] = clip(c)
	}
	return inv
}

// storeInventory schreibt das Inventar in die Serverzeile.
func (h *Hub) storeInventory(server *domain.Server, inv wire.Inventory, agent agentcaps.Agent) {
	rolledBack, reason := keptRollback(server, inv)
	err := h.servers.UpdateFields(server.ID, map[string]any{
		"agent_version":  inv.AgentVersion,
		"agent_protocol": agent.ProtocolVersion(),
		// Auch leer übernehmen: Ein auf eine ältere Fassung zurückgesetzter
		// Agent führt die Pins nicht mehr und hielte die Umstellung sonst
		// nicht auf.
		"agent_key_pin":      inv.KeyPin,
		"agent_ca_pin":       inv.CAPin,
		"agent_capabilities": strings.Join(inv.Capabilities, ","),
		// Der Zeitpunkt zeigt einem Update-Job, dass sich der Agent nach dem
		// Start gemeldet hat.
		"agent_rollback":        rolledBack,
		"agent_rollback_reason": reason,
		"agent_reported_at":     time.Now(),
	})
	if err != nil {
		h.log.Error("inventory: updating server row failed", "agent_id", server.AgentID, "error", err)
	}
}

// keptRollback bestimmt den Rollback-Befund nach einem Inventar. Meldet der
// Agent einen, gilt er. Meldet er keinen, bleibt ein bestehender stehen, solange
// der Agent noch auf derselben Version läuft: Nach einem Rollback gleicht LCM
// das Paket an die laufende Version an, der Agent startet dann normal und
// vergisst seine Meldung - der Befund aber gilt weiter, bis ein Update auf eine
// andere Version gelingt (der Update-Job leert ihn vor jedem Versuch).
func keptRollback(server *domain.Server, inv wire.Inventory) (string, string) {
	if inv.RolledBack != "" || server.AgentRollback == "" {
		return inv.RolledBack, inv.RolledBackReason
	}
	if agentcaps.SameVersion(server.AgentVersion, inv.AgentVersion) {
		return server.AgentRollback, server.AgentRollbackReason
	}
	return "", ""
}

// agentConnected verbucht einen erfolgreichen Agent-Connect: Registry,
// Health-Felder und (asynchron) der Online-Callback.
func (h *Hub) agentConnected(cl *mqtt.Client) {
	agentID := cl.ID
	h.mu.Lock()
	previous := h.online[agentID] != nil && h.online[agentID] != cl
	h.online[agentID] = cl
	h.connectedAt[agentID] = time.Now()
	h.links.connected(agentID, previous)
	// Ob der Agent pingen darf, sagt erst sein Inventar auf DIESER Verbindung.
	// (Dass er Jobs führt, bleibt über Verbindungen hinweg stehen: Ein Aufruf,
	// der auf seine Rückkehr wartet, darf nicht scheitern, weil er sich gleich
	// wieder trennt, bevor sein Inventar eintrifft.)
	delete(h.pingable, agentID)
	// Konsolen einer früheren Verbindung desselben Agents sind tot: Der Agent
	// beendet seine Shells, wenn ihm die Verbindung wegbricht, und ein neu
	// gestarteter Agent kennt sie gar nicht. Bei einer Übernahme meldet der
	// alte Client kein Trennen (siehe agentDisconnected) - ohne dies blieben
	// sie bis zur Leerlauf-Frist hängen.
	stale := h.terminalsOf(agentID)
	h.mu.Unlock()
	for _, t := range stale {
		t.end(ErrAgentOffline)
	}

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

// agentDisconnected verbucht die Trennung: Registry, Health-Felder und das
// Scheitern der noch wartenden Kommandos dieses Agents. Ein Agent mit
// wire.CapabilityJobs führt seine Kommandos über die Trennung hinaus weiter:
// Seine Aufrufe, und die Conns, auf denen sie laufen, bleiben stehen, bis er
// zurück ist und das Ergebnis liefert. Bei einem Session-Takeover (derselbe
// Agent hat sich neu verbunden) ist bereits der NEUE Client eingetragen - dann
// passiert hier nichts.
func (h *Hub) agentDisconnected(cl *mqtt.Client, err error) {
	agentID := cl.ID
	h.mu.Lock()
	if h.online[agentID] != cl {
		h.mu.Unlock()
		return
	}
	if err == nil {
		err = cl.StopCause()
	}
	cause := h.links.dropped(agentID, err)
	delete(h.online, agentID)
	delete(h.pingable, agentID)
	_, durable := h.jobs[agentID]
	var waiters []*pendingReq
	var deadConns []*agentConn
	if !durable {
		waiters = h.takePending(agentID)
		// Alle offenen Conns dieses Agenten einsammeln - sie werden unten
		// geschlossen, damit auch FOLGE-Kommandos (z.B. der nächste Scan-
		// Schritt) sofort scheitern statt aufs Timeout zu warten.
		for c := range h.conns[agentID] {
			deadConns = append(deadConns, c)
		}
		delete(h.conns, agentID)
	}
	deadTTYs := h.terminalsOf(agentID)
	h.mu.Unlock()
	for _, t := range deadTTYs {
		t.end(ErrAgentOffline)
	}
	failWaiters(waiters, errors.New("agent während der ausführung getrennt"))
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
	h.log.Info("agent disconnected", "server", server.Name, "agent_id", agentID, "cause", cause, "calls_kept", durable)
}

// rememberCapabilities merkt sich, ob der Agent Jobs führt (sein jüngstes
// Inventar gilt).
func (h *Hub) rememberCapabilities(agentID string, agent agentcaps.Agent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if agent.Supports(agentcaps.FeatureDurableJobs) {
		h.jobs[agentID] = struct{}{}
	} else {
		delete(h.jobs, agentID)
	}
}

// failUnreachable lässt die Aufrufe scheitern, die ein Agent mit Jobs nach seinem
// jüngsten Inventar nicht mehr beantworten kann - statt sie bis zur Lebenszeichen-
// Frist (oder maxSilence) warten zu lassen:
//   - Der Agent führt keine Jobs mehr (zurückgesetzt auf eine ältere Fassung):
//     Seine alten Jobs gibt es nicht mehr, ein Ergebnis kommt nie.
//   - Er meldet seine bekannten Kommandos (wire.Inventory.KnownJobs), und ein
//     Aufruf, den LCM VOR dieser Verbindung abgeschickt hat, steht nicht darin:
//     Er ist nie angekommen (die Verbindung riss beim Senden ab). Ein Aufruf
//     von nach dem Verbindungsaufbau gilt nicht - der Agent kann ihn bekommen
//     haben, bevor er sein Inventar zusammenstellte.
//
// Ein Inventar einer überholten Verbindung (cl ist nicht mehr die aktuelle)
// zählt nicht.
func (h *Hub) failUnreachable(cl *mqtt.Client, agentID string, agent agentcaps.Agent, known []string) {
	h.mu.Lock()
	if cl != nil && h.online[agentID] != cl {
		h.mu.Unlock()
		return
	}
	downgraded := !agent.Supports(agentcaps.FeatureDurableJobs)
	reported := known != nil
	since := h.connectedAt[agentID]
	var failed []*pendingReq
	for id, req := range h.pending {
		if req.agentID != agentID || !req.durable {
			continue
		}
		unknown := reported && req.sent.Before(since) && !slices.Contains(known, id)
		if downgraded || unknown {
			failed = append(failed, req)
			delete(h.pending, id)
		}
	}
	h.mu.Unlock()
	if downgraded {
		failWaiters(failed, errAgentDowngraded)
	} else {
		failWaiters(failed, ErrCommandUnknown)
	}
}

// errAgentDowngraded: Der Agent führt keine Jobs mehr.
var errAgentDowngraded = errors.New("der Agent wurde auf eine ältere Version zurückgesetzt - sein Kommando läuft dort nicht weiter; bitte erneut versuchen")

// cancelCommand hält ein Kommando des Agents an: Ist er verbunden, geht der
// Abbruch sofort hinaus. Sonst wartet er auf seine Rückkehr (flushCancels) -
// ein ausdrücklicher Abbruch darf nicht verloren gehen, nur weil die Leitung
// gerade fehlt, denn das Kommando läuft dort weiter.
func (h *Hub) cancelCommand(agentID, reqID string) {
	if h.Online(agentID) {
		_ = h.publishCommand(agentID, wire.Command{ID: reqID, Cancel: true})
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.cancels[agentID]) < maxQueuedCancels {
		h.cancels[agentID] = append(h.cancels[agentID], reqID)
	}
}

// flushCancels schickt die aufgeschobenen Abbrüche, sobald der Agent zurück ist
// und sein Kommando-Topic abonniert hat (sein Inventar trifft erst danach ein).
func (h *Hub) flushCancels(agentID string) {
	h.mu.Lock()
	ids := h.cancels[agentID]
	delete(h.cancels, agentID)
	h.mu.Unlock()
	for _, id := range ids {
		_ = h.publishCommand(agentID, wire.Command{ID: id, Cancel: true})
	}
}

// CommandStatus beschreibt das laufende Kommando eines Agents, wie der Agent es
// zuletzt gemeldet hat.
type CommandStatus struct {
	// Vitals sind die Beobachtungen des Agents (nil, solange er keine
	// gemeldet hat, etwa ein älterer Agent).
	Vitals *wire.Vitals
	// At ist der Zeitpunkt der letzten Meldung (Null: noch keine).
	At time.Time
	// Online: Der Agent ist verbunden. Sonst wartet LCM auf seine Rückkehr, und
	// Vitals sind die letzte bekannte Beobachtung.
	Online bool
}

// CommandStatus liefert den Stand des Kommandos, auf das LCM bei diesem Agent
// wartet. false, wenn keins wartet. Daraus lässt sich anzeigen „läuft, seit X
// ohne Ausgabe“ (Vitals.SilentSec), „wartet auf vorheriges Kommando“ oder „Agent
// getrennt, Ergebnis folgt“; angehalten wird nichts nach Zeit.
func (h *Hub) CommandStatus(agentID string) (CommandStatus, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, req := range h.pending {
		if req.agentID == agentID {
			_, online := h.online[agentID]
			return CommandStatus{Vitals: req.vitals, At: req.at, Online: online}, true
		}
	}
	return CommandStatus{}, false
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
	jobID      string // Job, für den die Verbindung geöffnet wurde (leer: keiner)
	done       chan struct{}
}

// BindJob verbindet die Conn mit dem LCM-Job, für den sie arbeitet: Ihre
// Kommandos werden dort vermerkt (JobTracker).
func (c *agentConn) BindJob(jobID string) {
	c.mu.Lock()
	c.jobID = jobID
	c.mu.Unlock()
}

// boundJob liefert den Job der Conn (leer: keiner).
func (c *agentConn) boundJob() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.jobID
}

// OnActivity hinterlegt den Lebenszeichen-Rückruf dieser Verbindung.
// KeyExchange: Der Agent-Transport läuft über MQTT, es gibt keinen
// SSH-Handshake und damit kein ausgehandeltes Verfahren. Leer heißt
// „unbekannt" - die Post-Quanten-Bewertung greift hier bewusst nicht.
func (c *agentConn) KeyExchange() string { return "" }

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
	// Ein neuer Aufruf braucht einen verbundenen Agent. (Nur laufende warten über
	// eine Trennung hinweg, siehe wire.CapabilityJobs.)
	if !c.hub.Online(c.agentID) {
		return "", -1, ErrAgentOffline
	}

	c.activity()
	defer c.activity()

	reqID := uuid.NewString()
	req := c.hub.register(reqID, c.agentID)
	defer c.hub.unregister(reqID)

	// Ein älterer Agent bekommt dieselbe Stille-Frist wie LCM selbst: Bricht die
	// Verbindung zum Server weg, beendet er das hängende Kommando von sich aus.
	// Ein Agent mit Jobs kennt keine (idle = 0, das Feld entfällt).
	idle := c.silenceLimit(req)
	command := wire.Command{ID: reqID, Cmd: cmd, Stdin: stdin, IdleTimeoutSec: int(idle.Seconds())}
	jobID := c.trackedJob(req)
	if jobID != "" {
		c.hub.tracker.CommandSent(jobID, reqID)
	}
	if err := c.hub.publishCommand(c.agentID, command); err != nil {
		c.commandDone(jobID, reqID)
		return "", -1, fmt.Errorf("kommando senden: %w", err)
	}
	out, code, err := c.await(reqID, req, idle)
	// Nur wer aufgibt, ohne den Ausgang zu kennen, lässt die Kennung stehen:
	// Dann kann das Ergebnis noch nachkommen.
	if !outcomeUnknown(err) {
		c.commandDone(jobID, reqID)
	}
	return out, code, err
}

// errCommandSilent: await hat aufgegeben, weil der verbundene Agent für das
// Kommando kein Lebenszeichen mehr meldet. Für Aufrufer ist es ErrCommandUnknown;
// anders als die Meldung des Agents ist es nur eine Vermutung, ein Ergebnis kann
// also noch nachkommen.
var errCommandSilent = fmt.Errorf("%w", ErrCommandUnknown)

// outcomeUnknown: Der Ausgang des Kommandos ist nicht bekannt - der Agent hat
// es vielleicht ausgeführt, und ein spätes Ergebnis kann noch eintreffen. Die
// Request-ID bleibt dann im Job stehen. (Ein Neustart von LCM und das
// Abschalten des Agent-Zugangs beenden das Warten, nicht das Kommando.)
func outcomeUnknown(err error) bool {
	return errors.Is(err, ErrResultUnknown) || errors.Is(err, errHubClosed) ||
		errors.Is(err, errAgentAccessOff) || errors.Is(err, errCommandSilent)
}

// trackedJob liefert den Job, dem das Kommando zuzuordnen ist - leer, wenn
// es keinen gibt oder der Agent keine Jobs führt (dann überlebt das Kommando
// keine Trennung, und es kommt kein spätes Ergebnis).
func (c *agentConn) trackedJob(req *pendingReq) string {
	if c.hub.tracker == nil || !req.durable {
		return ""
	}
	return c.boundJob()
}

// commandDone meldet den bekannten Ausgang des Kommandos an den JobTracker.
func (c *agentConn) commandDone(jobID, reqID string) {
	if jobID != "" {
		c.hub.tracker.CommandDone(jobID, reqID)
	}
}

// silenceLimit ist die erlaubte Stille eines Aufrufs: bei einem Agent mit Jobs
// keine (0), sonst die Einstellung - ein Agent ohne Jobs überlebt keine
// Trennung, ein hängendes Kommando hielte den Server sonst für immer fest.
func (c *agentConn) silenceLimit(req *pendingReq) time.Duration {
	if req.durable {
		return 0
	}
	return c.hub.commandIdleTimeout()
}

// await wartet auf das Ergebnis. Bei einem Agent mit Jobs gibt es keine
// Zeitgrenze für das Kommando und keinen Abbruch durch LCM: Nur ein
// ausdrückliches Close hält es an. Damit der Aufrufer nie ewig blockiert, endet
// das Warten mit ErrResultUnknown, wenn der Agent sich länger als maxSilence
// nicht gemeldet hat - ohne das Kommando anzuhalten. Bei einem Agent ohne Jobs
// (idle > 0) wird das Kommando nach idle Stille abgebrochen, jedes Lebenszeichen
// stellt die Uhr zurück.
func (c *agentConn) await(reqID string, req *pendingReq, idle time.Duration) (string, int, error) {
	silence := time.NewTimer(c.hub.maxSilence)
	defer silence.Stop()
	var idleTimer <-chan time.Time
	var resetIdle func()
	if idle > 0 {
		t := time.NewTimer(idle)
		defer t.Stop()
		idleTimer = t.C
		resetIdle = func() { t.Reset(idle) }
	}
	// Was der Agent selbst meldet, beurteilt er: Ein Aufruf, auf den LCM wartet,
	// gilt dem Job-Watchdog als lebendig (auch während einer Trennung), und
	// angehalten wird nur auf Wunsch des Anwenders. Der Watchdog bleibt für SSH
	// und ältere Agenten.
	var keepalive, unknown <-chan time.Time
	if req.durable {
		ticker := time.NewTicker(wire.ProgressInterval)
		defer ticker.Stop()
		keepalive = ticker.C
		check := time.NewTicker(max(c.hub.jobSilence/5, time.Millisecond))
		defer check.Stop()
		unknown = check.C
	}
	for {
		select {
		case <-req.progress:
			c.activity()
			silence.Reset(c.hub.maxSilence)
			if resetIdle != nil {
				resetIdle()
			}
		case <-keepalive:
			c.activity()
		case <-unknown:
			if c.hub.silentWhileOnline(req) {
				return "", -1, errCommandSilent
			}
		case res := <-req.ch:
			out := res.Output
			if res.Truncated {
				out += "\n[Ausgabe vom Agent gekürzt - Limit überschritten]"
			}
			if req.failure != nil {
				return out, res.ExitCode, req.failure
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
			// Agent abbrechen (SIGTERM, nach Frist SIGKILL).
			c.hub.cancelCommand(c.agentID, reqID)
			return "", -1, errors.New("kommando abgebrochen (verbindung geschlossen)")
		case <-idleTimer:
			_ = c.hub.publishCommand(c.agentID, wire.Command{ID: reqID, Cancel: true})
			return "", -1, fmt.Errorf("ohne lebenszeichen - kommando nach %s stille abgebrochen", idle)
		case <-silence.C:
			return "", -1, ErrResultUnknown
		}
	}
}

// silentWhileOnline: Der Agent ist verbunden, und das Kommando hat seit jobSilence
// kein Lebenszeichen mehr gemeldet - gerechnet ab dem Abschicken oder dem
// Verbindungsaufbau des Agents, je nachdem, was jünger ist. Ein Job meldet sich
// alle wire.ProgressInterval, solange der Agent ihn kennt; wer schweigt, ist
// dem Agent unbekannt (nie zugestellt). Bei getrenntem Agent gilt allein
// maxSilence.
func (h *Hub) silentWhileOnline(req *pendingReq) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, online := h.online[req.agentID]; !online {
		return false
	}
	ref := req.sign
	if since := h.connectedAt[req.agentID]; since.After(ref) {
		ref = since
	}
	return time.Since(ref) > h.jobSilence
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
	if subtle.ConstantTimeCompare([]byte(presented), []byte(server.AgentTokenHash)) != 1 {
		return false
	}
	// Nur eine erfolgreiche Anmeldung macht die IP zur bekannten Agent-IP.
	a.hub.knownIPs.markRemote(cl.Net.Remote)
	if guarded, ok := cl.Net.Conn.(*preAuthConn); ok {
		guarded.authenticated()
	}
	return true
}

// OnACLCheck sperrt Agents auf ihr eigenes Subtree: subscribe nur die
// Topics cmd und tty, publish nur res/inv/status/ttyo. Der Inline-Client (Serverseite)
// durchläuft diesen Hook nicht.
func (a *agentHook) OnACLCheck(cl *mqtt.Client, topic string, write bool) (ok bool) {
	// Fail-closed wie bei der Authentifizierung: Panic => Zugriff verweigert.
	defer safego.Recover("mqtt-hook:acl", nil)
	if cl.Net.Inline {
		return true
	}
	id := cl.ID
	if write {
		return topic == wire.TopicRes(id) || topic == wire.TopicInv(id) || topic == wire.TopicStatus(id) || topic == wire.TopicTTYOut(id)
	}
	return topic == wire.TopicCmd(id) || topic == wire.TopicTTY(id)
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
	a.hub.agentDisconnected(cl, err)
}
