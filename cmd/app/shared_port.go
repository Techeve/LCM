package main

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/gofiber/fiber/v3"

	"LCM/internal/core/services"
	"LCM/internal/infrastructure/tlsx"
	"LCM/internal/mcp"
	"LCM/internal/netfilter"
	"LCM/internal/safego"
	"LCM/internal/sniroute"
)

// Die Dienste am gemeinsamen Port heißen wie die Endpunkte der Netzbereiche.
const (
	targetUI    = string(netfilter.EndpointUI)
	targetAgent = string(netfilter.EndpointAgent)
	targetMCP   = string(netfilter.EndpointMCP)
)

// routeTarget nennt den Dienst, der eine Verbindung mit diesem Namen (SNI)
// bekommt: der, dessen öffentliche Adresse den Namen trägt, bei gleichem Namen
// in der Reihenfolge UI, Agent, MCP. Ohne Namen und bei unbekanntem Namen
// geht sie an die Weboberfläche. Trägt ein ausgeschalteter Dienst den Namen,
// ist es sniroute.Refuse: Die Verbindung wird geschlossen und fällt nicht auf
// die Oberfläche zurück.
func routeTarget(addr publicAddresses, serverName string) string {
	name := sniroute.Name(serverName)
	if name == "" {
		return targetUI
	}
	for _, ep := range []struct {
		target, url string
		enabled     bool
	}{
		{targetUI, addr.UI, true}, {targetAgent, addr.Agent, addr.AgentEnabled}, {targetMCP, addr.MCP, addr.MCPEnabled},
	} {
		if sniroute.HostOf(ep.url) != name {
			continue
		}
		if !ep.enabled {
			return sniroute.Refuse
		}
		return ep.target
	}
	return targetUI
}

// sharedPort ist der gemeinsame TLS-Port: ein Listener mit dem Zertifikatspool,
// hinter dem Weboberfläche, Agent und MCP je nach angefragtem Namen bedient
// werden. Er öffnet und schließt sich zur Laufzeit (Prepare), ohne Neustart.
// Alle Methoden sind für nil (kein gemeinsamer Port möglich, --dev) ausgelegt.
type sharedPort struct {
	pool      *tlsx.Pool
	addresses func() publicAddresses
	mcp       *mcp.Server // optional: wird mit dem Port angeschlossen

	mu        sync.Mutex
	ui, agent *fiber.App
	addr      string // gewünschte Adresse, "" = aus
	router    *sniroute.Router
	listening atomic.Bool
}

// newSharedPort legt den gemeinsamen Port an, geschlossen. Geöffnet wird er mit
// Open oder Prepare, wenn die Dienste bereitstehen (SetApps).
func newSharedPort(pool *tlsx.Pool, addresses func() publicAddresses) *sharedPort {
	return &sharedPort{pool: pool, addresses: addresses}
}

// SetApps nennt die Apps, die der Port bedient: die Weboberfläche und, wenn es
// ihn gibt, das Agent-Gateway.
func (p *sharedPort) SetApps(ui, agent *fiber.App) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ui, p.agent = ui, agent
}

// AttachMCP schließt MCP an den Port an, auch wenn dieser erst später öffnet.
func (p *sharedPort) AttachMCP(server *mcp.Server) {
	if p == nil {
		return
	}
	p.mcp = server
	server.WithShared(func() net.Listener { return p.listen(targetMCP) })
}

// listen schließt ein Ziel am laufenden Router an; nil, wenn der Port zu ist.
func (p *sharedPort) listen(target string) net.Listener {
	p.mu.Lock()
	router := p.router
	p.mu.Unlock()
	if router == nil {
		return nil
	}
	return router.Listen(target)
}

// Open öffnet den Port beim Start. Scheitert das Öffnen, läuft LCM mit den
// übrigen Listenern weiter, wie beim Agent-Listener; der Fehler kommt zurück
// zum Protokollieren.
func (p *sharedPort) Open(addr string) error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	p.addr = addr
	p.mu.Unlock()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("shared port %s: %w", addr, withBindHint(addr, err))
	}
	p.open(ln, addr)
	return nil
}

// Prepare bindet den neuen Port, ohne den laufenden anzufassen (addr leer: Port
// aus). commit setzt um, abort gibt die gebundene Adresse unbenutzt frei.
func (p *sharedPort) Prepare(addr string) (commit, abort func(), err error) {
	noop := func() {}
	if p == nil {
		return noop, noop, nil
	}
	p.mu.Lock()
	same := addr == p.addr && (addr == "" || p.listening.Load())
	p.mu.Unlock()
	if same {
		return noop, noop, nil
	}
	if addr == "" {
		return p.stop, noop, nil
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, nil, fmt.Errorf("gemeinsamer Port %s: %w", addr, withBindHint(addr, err))
	}
	return func() { p.stop(); p.open(ln, addr) }, func() { _ = ln.Close() }, nil
}

// open nimmt ln in Betrieb: ein neuer Router, Weboberfläche und Agent daran,
// dann MCP.
func (p *sharedPort) open(ln net.Listener, addr string) {
	router := sniroute.New(sniroute.Config{
		TLS:      p.pool.TLSConfig(),
		Resolve:  func(serverName string) string { return routeTarget(p.addresses(), serverName) },
		Fallback: targetUI,
	})
	p.mu.Lock()
	p.addr, p.router = addr, router
	ui, agent := p.ui, p.agent
	p.mu.Unlock()

	if ui != nil {
		serveTarget(router, ui, targetUI)
	}
	if agent != nil {
		serveTarget(router, agent, targetAgent)
	}
	if p.mcp != nil {
		p.mcp.AttachShared()
	}
	warnHostConflict(p.addresses())
	slog.Info("LCM shared port (HTTPS): web interface, agent and MCP by requested name", "address", "https://"+addr)
	p.listening.Store(true)
	safego.Go("shared-port", func() {
		if err := router.Serve(ln); err != nil {
			slog.Error("shared port terminated", "error", err)
		}
		p.ended(router)
	})
}

// ended vermerkt, dass der Router am Ende ist - nur, wenn er noch der aktuelle ist.
func (p *sharedPort) ended(router *sniroute.Router) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.router == router {
		p.router = nil
		p.listening.Store(false)
	}
}

// serveTarget lässt app die Verbindungen des Ziels bedienen. Dieselbe App
// bedient daneben weiter ihren eigenen Port.
func serveTarget(router *sniroute.Router, app *fiber.App, target string) {
	ln := router.Listen(target)
	safego.Go("shared-port-"+target, func() {
		if err := app.Listener(ln, fiber.ListenConfig{DisableStartupMessage: true}); err != nil && !isClosedListener(err) {
			slog.Error("shared port: service stopped", "service", target, "error", err)
		}
	})
}

// stop schließt den Port samt Zielen; die Dienste laufen auf ihren Einzelports weiter.
func (p *sharedPort) stop() {
	p.mu.Lock()
	router := p.router
	p.router, p.addr = nil, ""
	p.listening.Store(false)
	p.mu.Unlock()
	if router != nil {
		_ = router.Close()
		slog.Info("shared port closed")
	}
}

// Close schließt den Port (beim Beenden).
func (p *sharedPort) Close() {
	if p != nil {
		p.stop()
	}
}

// bindHint erklärt, was gegen einen verweigerten Port unter 1024 hilft; sonst leer.
func bindHint(addr string, err error) string {
	_, portText, splitErr := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portText)
	if splitErr != nil || port >= 1024 || !errors.Is(err, fs.ErrPermission) {
		return ""
	}
	return "ports below 1024 need the capability CAP_NET_BIND_SERVICE (the packaged lcm.service grants it): " +
		"on an older or customized unit run `systemctl edit lcm` and add under [Service] " +
		"CapabilityBoundingSet=CAP_NET_BIND_SERVICE and AmbientCapabilities=CAP_NET_BIND_SERVICE, " +
		"or use a port above 1023"
}

// warnHostConflict meldet beim Start zwei Dienste mit demselben Namen: Es gilt
// die Reihenfolge UI, Agent, MCP.
func warnHostConflict(addr publicAddresses) {
	c := services.FindPublicHostConflict(addr.UI, addr.Agent, addr.MCP, addr.MCPEnabled)
	if c == nil {
		return
	}
	slog.Warn(fmt.Sprintf("shared port: %s and %s have the same public name %q - connections go to %s, %s is not reachable by name. "+
		"Give each service its own name in the public addresses", c.First, c.Second, c.Host, c.First, c.Second))
}

// sharedStatus beschreibt den gemeinsamen Port für `lcm tls status`.
type sharedStatus struct {
	Address   string        `json:"address"`
	Listening bool          `json:"listening"`
	Routes    []sharedRoute `json:"routes"`
}

// sharedRoute ist ein Dienst und der Name, unter dem er am gemeinsamen Port
// erreichbar ist.
type sharedRoute struct {
	Service    string `json:"service"` // ui, agent oder mcp
	Host       string `json:"host,omitempty"`
	Inactive   bool   `json:"inactive,omitempty"`    // der Dienst ist ausgeschaltet (Agent oder MCP)
	ShadowedBy string `json:"shadowed_by,omitempty"` // ein Dienst davor hat denselben Namen
}

// Status beschreibt den Port und, welcher Name zu welchem Dienst führt. nil,
// wenn es keinen gemeinsamen Port gibt.
func (p *sharedPort) Status() *sharedStatus {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	addr := p.addr
	p.mu.Unlock()
	if addr == "" {
		return nil
	}
	return &sharedStatus{Address: addr, Listening: p.listening.Load(), Routes: sharedRoutes(p.addresses())}
}

func sharedRoutes(addr publicAddresses) []sharedRoute {
	var routes []sharedRoute
	owner := map[string]string{}
	for _, ep := range []struct {
		service, url string
		active       bool
	}{
		{targetUI, addr.UI, true}, {targetAgent, addr.Agent, addr.AgentEnabled}, {targetMCP, addr.MCP, addr.MCPEnabled},
	} {
		route := sharedRoute{Service: ep.service, Host: sniroute.HostOf(ep.url)}
		switch {
		case route.Host == "":
		case !ep.active:
			route.Inactive = true
		case owner[route.Host] != "":
			route.ShadowedBy = owner[route.Host]
		default:
			owner[route.Host] = ep.service
		}
		routes = append(routes, route)
	}
	return routes
}
