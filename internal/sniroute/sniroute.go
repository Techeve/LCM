// Package sniroute verteilt die TLS-Verbindungen eines gemeinsamen Ports nach
// dem angefragten Namen (SNI) auf mehrere Dienste.
//
// Der Router nimmt die TCP-Verbindung an, führt den TLS-Handshake selbst und
// legt die fertige *tls.Conn dem Dienst in einen speicherinternen
// net.Listener. Nichts wird kopiert oder weitergereicht: Der Dienst liest und
// schreibt auf derselben Verbindung, und seine HTTP-Bibliothek erkennt an der
// *tls.Conn, dass sie verschlüsselt ist.
package sniroute

import (
	"context"
	"crypto/tls"
	"log/slog"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"
)

// DefaultHandshakeTimeout begrenzt Handshake und Übergabe einer Verbindung.
// Danach gilt sie als Scanner oder hängende Gegenstelle und wird geschlossen.
const DefaultHandshakeTimeout = 10 * time.Second

// DefaultMaxPendingHandshakes begrenzt, wie viele Verbindungen gleichzeitig im
// Handshake stecken dürfen. Ohne Grenze könnte ein Strom halboffener
// Verbindungen beliebig viele Goroutinen binden; darüber wird neu Ankommendes
// sofort geschlossen.
const DefaultMaxPendingHandshakes = 256

// Refuse ist das Ergebnis von Config.Resolve für einen Namen, dessen Dienst
// ausgeschaltet ist: Die Verbindung wird nach dem Handshake geschlossen und
// geht nicht an den Fallback.
const Refuse = "\x00refuse"

// Name bringt einen angefragten Namen (SNI) in die Form zum Vergleichen:
// klein geschrieben, ohne den Punkt am Ende.
func Name(serverName string) string {
	return strings.TrimSuffix(strings.ToLower(serverName), ".")
}

// HostOf ist der Name einer öffentlichen Adresse (https://lcm.example.org:9310)
// in der Form von Name: ohne Port, klein geschrieben. Leer, wenn die Adresse
// leer oder nicht lesbar ist.
func HostOf(publicURL string) string {
	u, err := url.Parse(publicURL)
	if err != nil {
		return ""
	}
	return Name(u.Hostname())
}

// Config beschreibt einen Router.
type Config struct {
	// TLS ist die Konfiguration des gemeinsamen Ports (Zertifikatspool).
	TLS *tls.Config
	// Resolve nennt zu dem angefragten Namen das Ziel. Der Name ist leer, wenn
	// der Client keinen gesendet hat. Wird es nicht angeschlossen (Listen),
	// geht die Verbindung an Fallback; Refuse schließt sie stattdessen.
	Resolve func(serverName string) string
	// Fallback ist das Ziel für alles, was Resolve nicht oder ins Leere
	// zuordnet.
	Fallback string
	// HandshakeTimeout: 0 = DefaultHandshakeTimeout.
	HandshakeTimeout time.Duration
	// MaxPendingHandshakes: 0 oder kleiner = DefaultMaxPendingHandshakes.
	MaxPendingHandshakes int
	// Logger: nil = slog.Default().
	Logger *slog.Logger
}

// Router nimmt Verbindungen auf einem Listener an und verteilt sie.
type Router struct {
	tlsConfig  *tls.Config
	resolve    func(string) string
	fallback   string
	timeout    time.Duration
	maxPending int
	logger     *slog.Logger

	mu      sync.Mutex
	targets map[string]*targetListener
	raw     net.Listener
	pending map[net.Conn]struct{} // Verbindungen vor der Übergabe
	closed  bool
	wg      sync.WaitGroup
}

// New legt einen Router an. Er tut nichts, bis Serve läuft.
func New(cfg Config) *Router {
	if cfg.HandshakeTimeout <= 0 {
		cfg.HandshakeTimeout = DefaultHandshakeTimeout
	}
	if cfg.MaxPendingHandshakes <= 0 {
		cfg.MaxPendingHandshakes = DefaultMaxPendingHandshakes
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Router{
		tlsConfig:  cfg.TLS,
		resolve:    cfg.Resolve,
		fallback:   cfg.Fallback,
		timeout:    cfg.HandshakeTimeout,
		maxPending: cfg.MaxPendingHandshakes,
		logger:     cfg.Logger,
		targets:    map[string]*targetListener{},
		pending:    map[net.Conn]struct{}{},
	}
}

// Listen schließt ein Ziel an und liefert den Listener, den sein Dienst
// bedient. Ein Ziel, das schon angeschlossen war, wird ersetzt und sein
// Listener geschlossen. Schließt der Dienst den Listener (Shutdown), ist das
// Ziel wieder frei, und seine Namen gehen an den Fallback.
func (r *Router) Listen(target string) net.Listener {
	l := &targetListener{router: r, name: target, conns: make(chan net.Conn), done: make(chan struct{})}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		_ = l.Close()
		return l
	}
	old := r.targets[target]
	r.targets[target] = l
	r.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	return l
}

// Serve nimmt Verbindungen von ln an, bis ln oder der Router geschlossen
// wird. Ein Schließen über Close ist kein Fehler. Serve gehört ln: Es wird
// mit dem Router geschlossen.
func (r *Router) Serve(ln net.Listener) error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		_ = ln.Close()
		return nil
	}
	r.raw = ln
	r.mu.Unlock()

	var backoff time.Duration
	for {
		conn, err := ln.Accept()
		if err != nil {
			if r.isClosed() {
				return nil
			}
			if !isTemporary(err) {
				return err
			}
			backoff = nextBackoff(backoff)
			r.logger.Warn("shared port: accept failed, retrying", "error", err, "retry_in", backoff)
			time.Sleep(backoff)
			continue
		}
		backoff = 0
		if !r.start(conn) {
			return nil
		}
	}
}

// Close schließt den Listener, alle Ziele und die Verbindungen, die noch im
// Handshake stecken, und wartet, bis deren Goroutinen fertig sind. Bereits
// übergebene Verbindungen gehören den Diensten.
func (r *Router) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	raw := r.raw
	targets := make([]*targetListener, 0, len(r.targets))
	for _, l := range r.targets {
		targets = append(targets, l)
	}
	pending := make([]net.Conn, 0, len(r.pending))
	for c := range r.pending {
		pending = append(pending, c)
	}
	r.mu.Unlock()

	var err error
	if raw != nil {
		err = raw.Close()
	}
	for _, l := range targets {
		_ = l.Close()
	}
	for _, c := range pending {
		_ = c.Close()
	}
	r.wg.Wait()
	return err
}

func (r *Router) isClosed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closed
}

// start übernimmt eine Verbindung in eine eigene Goroutine. false heißt: Der
// Router ist geschlossen, die Verbindung ist zu. Ist die Grenze für
// Handshakes erreicht, wird nur diese Verbindung verworfen.
func (r *Router) start(raw net.Conn) bool {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		_ = raw.Close()
		return false
	}
	if len(r.pending) >= r.maxPending {
		r.mu.Unlock()
		r.logger.Debug("shared port: too many pending handshakes, connection dropped", "remote", raw.RemoteAddr().String())
		_ = raw.Close()
		return true
	}
	r.pending[raw] = struct{}{}
	r.wg.Add(1)
	r.mu.Unlock()
	go r.handle(raw)
	return true
}

// handle führt den Handshake und legt die Verbindung ins Ziel ihres Namens.
func (r *Router) handle(raw net.Conn) {
	defer r.wg.Done()
	defer r.release(raw)

	conn := tls.Server(raw, r.tlsConfig)
	ctx, cancel := context.WithTimeout(context.Background(), r.timeout)
	defer cancel()
	if err := conn.HandshakeContext(ctx); err != nil {
		// Im Internet probiert alles Mögliche an offenen Ports: nur Debug.
		r.logger.Debug("shared port: tls handshake failed", "remote", raw.RemoteAddr().String(), "error", err)
		_ = raw.Close()
		return
	}
	serverName := conn.ConnectionState().ServerName
	target := r.targetFor(serverName)
	if target == nil {
		r.logger.Debug("shared port: no service for connection", "server_name", serverName)
		_ = conn.Close()
		return
	}
	if !target.deliver(conn, r.timeout) {
		r.logger.Debug("shared port: service did not take the connection", "server_name", serverName, "service", target.name)
		_ = conn.Close()
	}
}

func (r *Router) release(raw net.Conn) {
	r.mu.Lock()
	delete(r.pending, raw)
	r.mu.Unlock()
}

// targetFor wählt das angeschlossene Ziel für einen Namen, sonst den Fallback.
func (r *Router) targetFor(serverName string) *targetListener {
	name := r.fallback
	if r.resolve != nil {
		name = r.resolve(serverName)
	}
	if name == Refuse {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if l := r.targets[name]; l != nil {
		return l
	}
	return r.targets[r.fallback]
}

// forget entfernt l aus den Zielen, wenn es noch das aktuelle ist.
func (r *Router) forget(l *targetListener) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.targets[l.name] == l {
		delete(r.targets, l.name)
	}
}

func (r *Router) addr() net.Addr {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.raw != nil {
		return r.raw.Addr()
	}
	return &net.TCPAddr{}
}

// targetListener ist der Listener eines Dienstes: Accept liefert die Verbindungen,
// die der Router ihm zuordnet.
type targetListener struct {
	router *Router
	name   string
	conns  chan net.Conn
	done   chan struct{}
	once   sync.Once
}

func (l *targetListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.conns:
		return conn, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *targetListener) Close() error {
	l.once.Do(func() {
		close(l.done)
		l.router.forget(l)
	})
	return nil
}

func (l *targetListener) Addr() net.Addr { return l.router.addr() }

// deliver übergibt conn an Accept, solange der Listener offen ist und
// höchstens eine Frist lang. false: nicht übergeben.
func (l *targetListener) deliver(conn net.Conn, wait time.Duration) bool {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case l.conns <- conn:
		return true
	case <-l.done:
	case <-timer.C:
	}
	return false
}

// isTemporary erkennt Fehler wie zu viele offene Dateien, nach denen Accept
// es später wieder versuchen darf (wie net/http).
func isTemporary(err error) bool {
	t, ok := err.(interface{ Temporary() bool })
	return ok && t.Temporary()
}

func nextBackoff(current time.Duration) time.Duration {
	const first, limit = 5 * time.Millisecond, time.Second
	if current == 0 {
		return first
	}
	return min(current*2, limit)
}
