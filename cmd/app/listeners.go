package main

import (
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"strconv"

	"github.com/gofiber/fiber/v3"

	"LCM/internal/config"
	"LCM/internal/core/services"
	"LCM/internal/mcp"
	"LCM/internal/runtimeport"
)

// runtimeListeners hält die Listener der Dienste und setzt eine Änderung der
// Ports oder des Adressmodus ohne Neustart um (services.AddressApplier).
type runtimeListeners struct {
	ui, agent *portListener
	mcp       *mcp.Server
	shared    *sharedPort // nil im Entwicklungsmodus (kein TLS)

	uiHost, agentHost string
	// agentGate schaltet den Agent-Zugang (Hub.SetAccepting): aus trennt es die
	// verbundenen Agenten und weist neue ab. Optional.
	agentGate func(on bool)
	// sharedHost ist der Bind-Host aus shared_tls_listen der config.json; leer:
	// alle Adressen. Der Port kommt aus den Einstellungen.
	sharedHost string
}

// hostPort setzt Bind-Host und Port zur Listen-Adresse zusammen; ohne Port ("kein
// Listener") ist sie leer.
func hostPort(host string, port int) string {
	if port <= 0 {
		return ""
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

// sharedAddr ist die Listen-Adresse des gemeinsamen Ports; leer ohne Port.
func (r *runtimeListeners) sharedAddr(port int) string {
	return hostPort(r.sharedHost, port)
}

// Prepare bindet alle Ports, die der neue Stand braucht, ohne die laufenden
// Listener anzufassen. Scheitert eines, geben die schon gebundenen ihre Ports
// wieder frei, und alles bleibt beim Alten. Der Start geht nicht darüber: Dort
// ist nur die Oberfläche Pflicht (siehe run).
func (r *runtimeListeners) Prepare(plan services.AddressPlan) (services.AddressChange, error) {
	change := &listenerChange{}
	steps := []func() (commit, abort func(), err error){
		func() (func(), func(), error) { return r.shared.Prepare(r.sharedAddr(plan.SharedTLSPort)) },
		func() (func(), func(), error) { return r.agent.Prepare(hostPort(r.agentHost, plan.AgentPort)) },
		func() (func(), func(), error) {
			return r.mcp.Prepare(plan.MCPEnabled, r.mcpHost(), plan.MCPPort, true)
		},
		// Der Schalter des Agenten wirkt erst mit dem Commit: Scheitert ein Bind,
		// bleiben die verbundenen Agenten verbunden.
		func() (func(), func(), error) {
			return func() { r.setAgentAccess(plan.AgentEnabled) }, func() {}, nil
		},
		// Die Oberfläche zuletzt: Ihr Port ist der, an dem der Aufrufer hängt.
		func() (func(), func(), error) { return r.ui.Prepare(hostPort(r.uiHost, plan.UIPort)) },
	}
	for _, step := range steps {
		commit, abort, err := step()
		if err != nil {
			change.Rollback()
			return nil, err
		}
		change.commits = append(change.commits, commit)
		change.aborts = append(change.aborts, abort)
	}
	return change, nil
}

// mcpHost ist der Bind-Host von MCP: der der Oberfläche.
func (r *runtimeListeners) mcpHost() string { return mcpBindHost(r.uiHost) }

// mcpBindHost: MCP bindet an den Host der Oberfläche; ohne Angabe an alle
// Interfaces, wie die Oberfläche selbst.
func mcpBindHost(uiHost string) string {
	if uiHost == "" {
		return "0.0.0.0"
	}
	return uiHost
}

func (r *runtimeListeners) setAgentAccess(on bool) {
	if r.agentGate != nil {
		r.agentGate(on)
	}
}

// listenerChange ist eine vorbereitete Umschaltung (services.AddressChange).
type listenerChange struct {
	commits, aborts []func()
}

func (c *listenerChange) Commit() {
	for _, commit := range c.commits {
		commit()
	}
}

func (c *listenerChange) Rollback() {
	for _, abort := range c.aborts {
		abort()
	}
}

// listenerSetup sind die Bausteine für den Start der Listener.
type listenerSetup struct {
	cfg      *config.Config
	dataDir  string
	settings *services.SettingsService
	app      *fiber.App // Weboberfläche und REST-API
	agentApp *fiber.App // Agent-Gateway, nil ohne LCM Remote
	mcp      *mcp.Server
	shared   *sharedPort // nil im Entwicklungsmodus
	// tlsConfig liefert die Konfiguration des Zertifikatspools; nil im Entwicklungsmodus (HTTP).
	tlsConfig func() *tls.Config
	// agentGate schaltet den Agent-Zugang (siehe runtimeListeners.agentGate); optional.
	agentGate func(on bool)
}

// startListeners öffnet die Ports der Einstellungen und verdrahtet den Wechsel
// zur Laufzeit. Die Oberfläche ist Pflicht: Lässt sich ihr Port nicht binden,
// scheitert der Start. Agent und gemeinsamer Port melden nur im Log, wenn sie
// nicht aufgehen - LCM läuft dann mit den übrigen weiter.
func startListeners(s listenerSetup) (*runtimeListeners, error) {
	addr, err := s.settings.Addresses()
	if err != nil {
		return nil, fmt.Errorf("adressen lesen: %w", err)
	}
	r := &runtimeListeners{
		ui:         newPortListener("Oberfläche", s.app, s.tlsConfig, uiRetireGrace),
		mcp:        s.mcp,
		shared:     s.shared,
		uiHost:     s.cfg.Host,
		agentHost:  agentBindHost(s.cfg),
		agentGate:  s.agentGate,
		sharedHost: listenHostOf(s.cfg.SharedTLSListen),
	}
	r.setAgentAccess(addr.AgentEnabled)
	r.ui.onOpen = func(addr string) { noteUIPort(s.dataDir, addr) }
	if s.agentApp != nil {
		r.agent = newPortListener("Agent", s.agentApp, s.tlsConfig, 0)
	}
	if err := r.agent.Open(hostPort(r.agentHost, addr.AgentListenPort())); err != nil {
		slog.Error("agent listener not available - LCM keeps running on the other ports", "error", err)
	}
	s.shared.SetApps(s.app, s.agentApp)
	if addr.SharedPortActive() {
		if err := s.shared.Open(r.sharedAddr(addr.SharedTLSPort)); err != nil {
			slog.Error("shared port not available - LCM keeps running on the other ports", "error", err)
		}
	}
	if err := r.ui.Open(hostPort(r.uiHost, addr.UIPort)); err != nil {
		return nil, fmt.Errorf("oberfläche starten: %w", err)
	}
	s.settings.WithAddressApplier(r)
	return r, nil
}

// noteUIPort vermerkt den Port der Oberfläche für `lcm -healthcheck` und
// lcm-probe, die nur die config.json kennen (siehe runtimeport).
func noteUIPort(dataDir, addr string) {
	if err := runtimeport.Write(dataDir, listenPort(addr)); err != nil {
		slog.Warn("ui port could not be noted for healthcheck and probe", "error", err)
	}
}

// agentBindHost ist der Bind-Host des Agent-Listeners: ohne Angabe alle
// Interfaces - der Agent verbindet sich von außen.
func agentBindHost(cfg *config.Config) string {
	if cfg.AgentHost == "" {
		return "0.0.0.0"
	}
	return cfg.AgentHost
}

// Close schließt die Listener (beim Beenden).
func (r *runtimeListeners) Close() {
	r.ui.Close()
	r.agent.Close()
}
