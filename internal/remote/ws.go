package remote

import (
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"github.com/fasthttp/websocket"
	"github.com/gofiber/fiber/v3"

	"LCM/internal/remote/wire"
	"LCM/internal/safego"
)

// WSHandler liefert den Fiber-Handler für GET /mqtt: Upgrade auf WebSocket
// und Übergabe der Verbindung an den eingebetteten Broker. Die eigentliche
// Authentifizierung (AgentID + Token) macht der Broker im CONNECT-Paket -
// hier gibt es nur ein kleines per-IP-Ratelimit gegen Upgrade-Fluten.
//
// fasthttp/websocket hijackt die Verbindung: der Fiber-Handler kehrt sofort
// zurück, der Callback läuft anschließend auf der gekaperten Verbindung und
// blockiert dort bis zum Disconnect des Agents.
func WSHandler(hub *Hub) fiber.Handler {
	upgrader := websocket.FastHTTPUpgrader{
		Subprotocols: []string{"mqtt"}, // [MQTT-6.0.0-3]
		// Kein CheckOrigin-Override: Agents senden keinen Origin-Header
		// (kein Browser), der sichere Default lässt sie durch und blockt
		// Cross-Origin-Versuche aus Browsern.
	}
	limiter := newUpgradeLimiter(10, time.Minute)

	return func(c fiber.Ctx) error {
		if !limiter.allow(c.IP()) {
			return fiber.NewError(fiber.StatusTooManyRequests,
				"zu viele Verbindungsversuche - bitte später erneut versuchen")
		}
		err := upgrader.Upgrade(c.RequestCtx(), func(ws *websocket.Conn) {
			// Ab hier ist die Verbindung „hijacked": fasthttp führt diesen
			// Rumpf in einer EIGENEN Goroutine aus, außerhalb der
			// recover-Middleware des Routers. Ohne diesen Schutz würde ein
			// Panic im MQTT-Lesepfad den gesamten Dienst beenden.
			defer safego.Recover("agent-ws-conn", nil)
			defer ws.Close()
			ws.SetReadLimit(wire.MaxPacketSize)
			// Blockiert bis zum Disconnect; Fehler loggt der Broker selbst.
			_ = hub.HandleConn(&wsNetConn{Conn: ws.NetConn(), ws: ws})
		})
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "websocket-upgrade fehlgeschlagen")
		}
		return nil
	}
}

// upgradeLimiter ist ein kleines Fixed-Window-Ratelimit pro Client-IP
// (Muster middlewares.APIKeyRateLimit, hier lokal, da paketübergreifend
// kein generischer Limiter existiert).
type upgradeLimiter struct {
	max    int
	window time.Duration

	mu      sync.Mutex
	buckets map[string]*limiterWindow
}

type limiterWindow struct {
	start time.Time
	count int
}

func newUpgradeLimiter(max int, window time.Duration) *upgradeLimiter {
	return &upgradeLimiter{max: max, window: window, buckets: map[string]*limiterWindow{}}
}

func (l *upgradeLimiter) allow(ip string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	w := l.buckets[ip]
	if w == nil || now.Sub(w.start) >= l.window {
		// Abgelaufene Fenster gelegentlich aufräumen (Map-Wachstum).
		if len(l.buckets) > 10_000 {
			for k, v := range l.buckets {
				if now.Sub(v.start) >= l.window {
					delete(l.buckets, k)
				}
			}
		}
		l.buckets[ip] = &limiterWindow{start: now, count: 1}
		return true
	}
	w.count++
	return w.count <= l.max
}

// wsNetConn adaptiert die WebSocket-Verbindung auf net.Conn für den Broker
// (MQTT-Pakete als Binary-Messages) - 1:1 nach dem Muster von mochis
// eigenem websocket-Listener. Deadlines/Adressen laufen über die
// eingebettete rohe Verbindung (ws.NetConn()).
type wsNetConn struct {
	net.Conn
	ws *websocket.Conn

	// r ist der Reader der aktuell angebrochenen Message (nil = keine).
	r io.Reader
}

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
