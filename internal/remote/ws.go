package remote

import (
	"errors"
	"io"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/fasthttp/websocket"
	"github.com/gofiber/fiber/v3"

	"LCM/internal/api/apierr"
	"LCM/internal/netfilter"
	"LCM/internal/remote/wire"
	"LCM/internal/safego"
)

// errUpgradeFailed meldet einen gescheiterten WebSocket-Upgrade eines Agents.
var errUpgradeFailed = apierr.Define(400, "agent.upgrade_failed", "websocket upgrade failed")

// WSHandler liefert den Fiber-Handler für GET /mqtt: Upgrade auf WebSocket
// und Übergabe der Verbindung an den eingebetteten Broker. Die eigentliche
// Authentifizierung (AgentID + Token) macht der Broker im CONNECT-Paket -
// hier gibt es nur ein kleines per-IP-Ratelimit gegen Upgrade-Fluten. trust
// sagt, wessen X-Forwarded-For dabei zählt (siehe netfilter.ProxyTrust; der
// Halter wird bei jedem Upgrade neu gelesen).
//
// fasthttp/websocket hijackt die Verbindung: der Fiber-Handler kehrt sofort
// zurück, der Callback läuft anschließend auf der gekaperten Verbindung und
// blockiert dort bis zum Disconnect des Agents.
func WSHandler(hub *Hub, trust *netfilter.ProxyTrustHolder) fiber.Handler {
	upgrader := websocket.FastHTTPUpgrader{
		Subprotocols: []string{"mqtt"}, // [MQTT-6.0.0-3]
		// Kein CheckOrigin-Override: Agents senden keinen Origin-Header
		// (kein Browser), der sichere Default lässt sie durch und blockt
		// Cross-Origin-Versuche aus Browsern.
	}
	limiter := newUpgradeLimiter(hub.knownIPs)

	return func(c fiber.Ctx) error {
		// Hinter einem TLS-terminierenden Proxy wäre die Peer-Adresse für
		// ALLE Agents dieselbe - nach einem LCM-Neustart kämen dann nur zehn
		// pro Minute wieder herein, der Rest liefe in die Sperre. Deshalb
		// dieselbe Adressermittlung wie auf dem UI-Port (trusted_proxies).
		ip := c.IP()
		xff := netfilter.JoinForwardedFor(c.Request().Header.PeekAll(fiber.HeaderXForwardedFor))
		if addr, ok := netfilter.ClientIP(c.IP(), xff, trust.Load()); ok {
			ip = addr.String()
		}
		if !limiter.allow(ip) {
			return apierr.TooManyRequests
		}
		// Ausdrücklich abweisen statt den Socket nach dem Upgrade zu kappen: Der
		// Agent erkennt an der HTTP-Antwort, dass LCM ihn abweist (und nicht das
		// Netz oder sein eigenes Binary versagt).
		if !hub.accepting() {
			return c.Status(fiber.StatusServiceUnavailable).SendString("agent access is switched off")
		}
		if hub.PreAuthFull() {
			return c.Status(fiber.StatusServiceUnavailable).SendString("too many connections waiting for login")
		}
		remote := clientAddr(c.RequestCtx().RemoteAddr(), ip)
		err := upgrader.Upgrade(c.RequestCtx(), func(ws *websocket.Conn) {
			// Ab hier ist die Verbindung „hijacked": fasthttp führt diesen
			// Rumpf in einer EIGENEN Goroutine aus, außerhalb der
			// recover-Middleware des Routers. Ohne diesen Schutz würde ein
			// Panic im MQTT-Lesepfad den gesamten Dienst beenden.
			defer safego.Recover("agent-ws-conn", nil)
			defer ws.Close()
			ws.SetReadLimit(wire.MaxPacketSize)
			// Blockiert bis zum Disconnect; Fehler loggt der Broker selbst.
			_ = hub.HandleConn(&wsNetConn{Conn: ws.NetConn(), ws: ws, remote: remote})
		})
		if err != nil {
			return errUpgradeFailed
		}
		return nil
	}
}

// Limits des Upgrade-Ratelimits (Upgrades je Minute und Client-IP).
//
// Unbekannte Quellen bleiben knapp gehalten: Das Upgrade läuft VOR der
// Authentifizierung (AgentID + Token im MQTT-CONNECT), also schützt nur dieses
// Limit vor Upgrade-Fluten. Wer sich schon einmal erfolgreich angemeldet hat
// (bekannte Agent-IP), darf deutlich öfter kommen: ein flatternder Agent oder
// viele Agents hinter einem NAT bzw. Proxy sperren sich sonst selbst aus. Direkt
// nach dem Start ist die Menge der bekannten IPs leer (nur im Speicher), und
// alle Agents melden sich gleichzeitig zurück - deshalb gilt in der Karenzzeit
// auch für unbekannte IPs ein höheres Limit.
const (
	upgradeWindow        = time.Minute
	upgradeLimitUnknown  = 10
	upgradeLimitGrace    = 120 // unbekannte IPs in den ersten upgradeGracePeriod nach dem Start
	upgradeLimitKnown    = 600
	upgradeGracePeriod   = 10 * time.Minute
	knownAgentIPLifetime = 24 * time.Hour
	knownAgentIPMax      = 50_000
)

// knownAgentIPs merkt sich die Client-IPs, von denen sich ein Agent erfolgreich
// authentifiziert hat (Hook OnConnectAuthenticate), für knownAgentIPLifetime.
// Nur im Speicher und in der Größe begrenzt.
type knownAgentIPs struct {
	now func() time.Time

	mu      sync.Mutex
	expires map[netip.Addr]time.Time
}

func newKnownAgentIPs() *knownAgentIPs {
	return &knownAgentIPs{now: time.Now, expires: map[netip.Addr]time.Time{}}
}

// markRemote trägt die IP einer Gegenadresse ("ip:port") ein. Nicht lesbare
// Adressen werden ignoriert.
func (k *knownAgentIPs) markRemote(remote string) {
	ap, err := netip.ParseAddrPort(remote)
	if err != nil {
		return
	}
	k.mark(ap.Addr())
}

func (k *knownAgentIPs) mark(addr netip.Addr) {
	addr = addr.Unmap()
	now := k.now()
	k.mu.Lock()
	defer k.mu.Unlock()
	if _, known := k.expires[addr]; !known && len(k.expires) >= knownAgentIPMax {
		k.pruneLocked(now)
		if len(k.expires) >= knownAgentIPMax {
			return // voll: diese IP bleibt beim normalen Limit
		}
	}
	k.expires[addr] = now.Add(knownAgentIPLifetime)
}

func (k *knownAgentIPs) has(addr netip.Addr) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.now().Before(k.expires[addr.Unmap()])
}

func (k *knownAgentIPs) pruneLocked(now time.Time) {
	for addr, exp := range k.expires {
		if !now.Before(exp) {
			delete(k.expires, addr)
		}
	}
}

// upgradeLimiter ist ein kleines Fixed-Window-Ratelimit pro Client-IP
// (Muster middlewares.APIKeyRateLimit, hier lokal, da paketübergreifend
// kein generischer Limiter existiert). Das Limit hängt davon ab, ob die IP
// als Agent-IP bekannt ist und ob die Karenzzeit nach dem Start noch läuft.
type upgradeLimiter struct {
	known      *knownAgentIPs
	graceUntil time.Time

	mu      sync.Mutex
	buckets map[string]*limiterWindow
}

type limiterWindow struct {
	start time.Time
	count int
}

// newUpgradeLimiter startet die Karenzzeit mit der Uhr von known.
func newUpgradeLimiter(known *knownAgentIPs) *upgradeLimiter {
	return &upgradeLimiter{
		known:      known,
		graceUntil: known.now().Add(upgradeGracePeriod),
		buckets:    map[string]*limiterWindow{},
	}
}

// limitFor liefert die erlaubten Upgrades je Fenster für diese IP.
func (l *upgradeLimiter) limitFor(ip string, now time.Time) int {
	if addr, err := netip.ParseAddr(ip); err == nil && l.known.has(addr) {
		return upgradeLimitKnown
	}
	if now.Before(l.graceUntil) {
		return upgradeLimitGrace
	}
	return upgradeLimitUnknown
}

func (l *upgradeLimiter) allow(ip string) bool {
	now := l.known.now()
	max := l.limitFor(ip, now)
	l.mu.Lock()
	defer l.mu.Unlock()
	w := l.buckets[ip]
	if w == nil || now.Sub(w.start) >= upgradeWindow {
		// Abgelaufene Fenster gelegentlich aufräumen (Map-Wachstum).
		if len(l.buckets) > 10_000 {
			for k, v := range l.buckets {
				if now.Sub(v.start) >= upgradeWindow {
					delete(l.buckets, k)
				}
			}
		}
		l.buckets[ip] = &limiterWindow{start: now, count: 1}
		return true
	}
	w.count++
	return w.count <= max
}

// wsNetConn adaptiert die WebSocket-Verbindung auf net.Conn für den Broker
// (MQTT-Pakete als Binary-Messages) - 1:1 nach dem Muster von mochis
// eigenem websocket-Listener. Deadlines/Adressen laufen über die
// eingebettete rohe Verbindung (ws.NetConn()).
type wsNetConn struct {
	net.Conn
	ws *websocket.Conn
	// remote ist die Adresse, die der Hub als Gegenstelle kennt.
	remote net.Addr

	// r ist der Reader der aktuell angebrochenen Message (nil = keine).
	r io.Reader
}

// clientAddr ist die Gegenadresse, die der Hub für die Verbindung festhält:
// die ermittelte Client-Adresse (hinter einem vertrauten Proxy die
// weitergeleitete) mit dem Port der Verbindung. Lässt sie sich nicht lesen,
// bleibt es bei der Adresse der Verbindung.
func clientAddr(peer net.Addr, clientIP string) net.Addr {
	addr, err := netip.ParseAddr(clientIP)
	if err != nil {
		return peer
	}
	port := 0
	if tcp, ok := peer.(*net.TCPAddr); ok {
		port = tcp.Port
	}
	return &net.TCPAddr{IP: addr.Unmap().AsSlice(), Port: port}
}

func (c *wsNetConn) RemoteAddr() net.Addr { return c.remote }

// errNotBinary: MQTT über WebSocket MUSS Binary-Messages verwenden.
var errNotBinary = errors.New("websocket: message ist nicht binär")

func (c *wsNetConn) Read(p []byte) (int, error) {
	if c.r == nil {
		op, r, err := c.ws.NextReader()
		if err != nil {
			return 0, err
		}
		if op != websocket.BinaryMessage {
			return 0, errNotBinary
		}
		c.r = r
	}
	var n int
	for {
		if n == len(p) {
			return n, nil // Puffer voll - Rest der Message beim nächsten Read
		}
		br, err := c.r.Read(p[n:])
		n += br
		if err != nil {
			// Jedes Fehlerende beendet die aktuelle Message (io.EOF regulär).
			c.r = nil
			if errors.Is(err, io.EOF) {
				err = nil
			}
			return n, err
		}
	}
}

func (c *wsNetConn) Write(p []byte) (int, error) {
	if err := c.ws.WriteMessage(websocket.BinaryMessage, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *wsNetConn) Close() error {
	// fasthttp liefert für gekaperte Verbindungen einen hijackConn, dessen
	// Close() ein No-op ist (aufgeräumt wird erst, wenn der Hijack-Handler
	// zurückkehrt). Ein blockierter Broker-Read bliebe damit bis zum
	// nächsten Client-Paket hängen - DisconnectClient (Token-Revocation,
	// Shutdown) würde erst beim Keepalive greifen. Deshalb: Read-Deadline
	// in die Vergangenheit setzen (entblockt NextReader sofort) und die
	// rohe TCP-Verbindung schließen, wo fasthttp sie freigibt.
	_ = c.Conn.SetReadDeadline(time.Now())
	if u, ok := c.Conn.(interface{ UnsafeConn() net.Conn }); ok {
		_ = u.UnsafeConn().Close()
	}
	return c.Conn.Close()
}
