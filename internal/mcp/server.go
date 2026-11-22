package mcp

import (
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/recover"

	"LCM/internal/api/apierr"
	"LCM/internal/safego"
)

// Server verwaltet den MCP-HTTP-Listener zur LAUFZEIT: Er lässt sich über die
// LCM-Einstellungen jederzeit starten, stoppen oder auf eine andere
// Bind-Adresse/Port umschalten (Apply). Der eigentliche Listener läuft in
// einer eigenen Goroutine; Umschalten fährt den alten sauber herunter.
//
// Der Endpunkt spricht TLS mit dem Zertifikatspool von LCM und bindet an den Host
// der Oberfläche (siehe runtimeListeners.Prepare). Einfaches HTTP gibt es nur im
// Entwicklungsmodus (ohne Zertifikate) und nur auf Loopback. Läuft LCM mit einem
// gemeinsamen TLS-Port, nimmt der Server dort zusätzlich die Verbindungen an,
// die der angefragte Name (SNI) ihm zuordnet.
type Server struct {
	handler *Handler
	logger  *slog.Logger
	// guards laufen vor jedem Request (Client-Adresse, Netzbereiche). Sie
	// kommen von außen, weil das Paket der Middlewares von diesem hier
	// abhängt.
	guards []fiber.Handler
	// tlsConfig liefert die TLS-Konfiguration des Zertifikatspools. nil im
	// Entwicklungsmodus (--dev): Dort gibt es keine Zertifikate, und MCP mit
	// TLS startet nicht.
	tlsConfig func() *tls.Config
	// shared schließt MCP an den gemeinsamen TLS-Port an und liefert den
	// Listener dafür. nil = kein gemeinsamer Port.
	shared func() net.Listener

	mu       sync.Mutex
	app      *fiber.App
	addr     string       // aktuelle Listen-Adresse, "" = aus
	tls      bool         // spricht der eigene Listener TLS
	sharedLn net.Listener // Listener am gemeinsamen Port, solange der Server läuft
}

// NewServer erstellt den (zunächst gestoppten) MCP-Listener-Manager.
func NewServer(handler *Handler, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{handler: handler, logger: logger}
}

// WithGuards hängt Middlewares vor die Route, etwa den Filter der erlaubten
// Netzbereiche. Nur vor dem ersten Apply aufrufen.
func (s *Server) WithGuards(guards ...fiber.Handler) *Server {
	s.guards = guards
	return s
}

// WithTLS gibt dem Server den Zertifikatspool für MCP mit TLS. Nur vor dem
// ersten Apply aufrufen.
func (s *Server) WithTLS(config func() *tls.Config) *Server {
	s.tlsConfig = config
	return s
}

// WithShared hängt den Server an den gemeinsamen TLS-Port; listen liefert bei
// jedem Start des Servers den Listener dafür. Nur vor dem ersten Apply
// aufrufen.
func (s *Server) WithShared(listen func() net.Listener) *Server {
	s.shared = listen
	return s
}

// Apply bringt den Listener in den gewünschten Zustand. enabled=false stoppt
// ihn; enabled=true (neu)startet ihn auf host:port, mit useTLS verschlüsselt.
// Läuft er bereits genau so, passiert nichts. Lässt sich die neue Adresse nicht
// binden, läuft der bisherige Listener weiter (oder ist, wo das nicht geht,
// wiederhergestellt); der Fehler steht im Log und kommt zurück.
func (s *Server) Apply(enabled bool, host string, port int, useTLS bool) error {
	commit, _, err := s.Prepare(enabled, host, port, useTLS)
	if err != nil {
		s.logger.Error("MCP listener not switched - the previous one keeps running", "error", err)
		return err
	}
	commit()
	return nil
}

// Prepare bereitet Apply vor: Es bindet die neue Adresse, ohne den laufenden
// Listener anzufassen. Scheitert das, bleibt alles beim Alten und der Fehler
// kommt zurück. commit setzt die Änderung um (der alte Listener hält an, der
// neue bedient), abort gibt die gebundene Adresse unbenutzt wieder frei.
//
// Wechselt nur TLS auf derselben Adresse, geht das nicht vorab: Die Adresse wird
// erst frei, wenn der alte Listener heruntergefahren ist. Prepare tauscht dann
// selbst, bindet kurz wiederholt und stellt bei einem Fehler den alten
// Listener wieder her; abort stellt ihn ebenfalls wieder her.
func (s *Server) Prepare(enabled bool, host string, port int, useTLS bool) (commit, abort func(), err error) {
	noop := func() {}
	if !enabled || port <= 0 {
		return func() { s.Shutdown() }, noop, nil
	}
	if host == "" {
		host = "127.0.0.1"
	}
	if useTLS && s.tlsConfig == nil {
		// Entwicklungsmodus (--dev): Es gibt keine Zertifikate. Ein Fehler hier
		// würde jede Adressänderung sperren; MCP spricht stattdessen HTTP, aber
		// nur auf Loopback - sonst läge das Token im Klartext im Netz, und MCP bleibt aus.
		if !isLoopback(host) {
			s.logger.Warn("MCP with TLS is not available in development mode - MCP stays off", "host", host)
			return func() { s.Shutdown() }, noop, nil
		}
		useTLS = false
	}
	newAddr := fmt.Sprintf("%s:%d", host, port)

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.app != nil && s.addr == newAddr && s.tls == useTLS {
		return noop, noop, nil // schon so aktiv
	}
	if s.app != nil && s.addr == newAddr {
		oldTLS := s.tls
		if err := s.retuneLocked(newAddr, useTLS); err != nil {
			return nil, nil, err
		}
		return noop, func() { s.retune(newAddr, oldTLS) }, nil
	}
	ln, err := net.Listen("tcp", newAddr)
	if err != nil {
		return nil, nil, fmt.Errorf("MCP-Adresse %s: %w", newAddr, err)
	}
	return func() { s.switchTo(ln, newAddr, useTLS) }, func() { _ = ln.Close() }, nil
}

// isLoopback meldet, ob host nur lokal erreichbar ist.
func isLoopback(host string) bool {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	ip := net.ParseIP(host)
	return strings.EqualFold(host, "localhost") || (ip != nil && ip.IsLoopback())
}

var (
	// rebindFor ist die Zeit, in der ein eben freigegebener Port erneut
	// gebunden werden darf, bevor das Binden als gescheitert gilt.
	rebindFor   = 2 * time.Second
	rebindEvery = 50 * time.Millisecond
)

// retune wechselt TLS auf der laufenden Adresse (siehe retuneLocked).
func (s *Server) retune(addr string, useTLS bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.retuneLocked(addr, useTLS); err != nil {
		s.logger.Error("MCP listener not restored", "error", err)
	}
}

// retuneLocked fährt den Listener herunter und startet ihn auf derselben
// Adresse mit useTLS neu. Gelingt das Binden nicht, stellt es den alten Stand
// wieder her und meldet den Fehler. Aufrufer hält s.mu.
func (s *Server) retuneLocked(addr string, useTLS bool) error {
	oldTLS := s.tls
	<-s.stopLocked()
	err := s.rebindLocked(addr, useTLS)
	if err == nil {
		return nil
	}
	if restoreErr := s.rebindLocked(addr, oldTLS); restoreErr != nil {
		s.logger.Error("MCP listener could not be restored - MCP is off until the next change", "error", restoreErr)
	}
	return err
}

// rebindLocked bindet addr (kurz wiederholt) und startet den Listener darauf.
func (s *Server) rebindLocked(addr string, useTLS bool) error {
	deadline := time.Now().Add(rebindFor)
	for {
		ln, err := net.Listen("tcp", addr)
		if err == nil {
			s.startLocked(ln, addr, useTLS, nil)
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("MCP-Adresse %s: %w", addr, err)
		}
		time.Sleep(rebindEvery)
	}
}

// switchTo fährt den laufenden Listener herunter und startet den neuen auf ln.
func (s *Server) switchTo(ln net.Listener, addr string, useTLS bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.startLocked(ln, addr, useTLS, s.stopLocked())
}

// startLocked startet den Listener auf ln, sobald after geschlossen ist (nil:
// sofort). Aufrufer hält s.mu und hat den alten Listener schon angehalten.
func (s *Server) startLocked(ln net.Listener, addr string, useTLS bool, after <-chan struct{}) {
	scheme := "http"
	var tlsConf *tls.Config
	if useTLS {
		tlsConf, scheme = s.tlsConfig(), "https"
	}

	app := s.buildApp()
	s.app, s.addr, s.tls = app, addr, useTLS
	s.logger.Info("MCP listener started", "address", scheme+"://"+addr+"/mcp")
	safego.Go("mcp-listener", func() {
		if after != nil {
			<-after
		}
		if err := s.serve(app, ln, tlsConf); err != nil && !errors.Is(err, net.ErrClosed) {
			s.logger.Error("MCP listener terminated", "address", addr, "error", err)
		}
	})
	s.attachSharedLocked(app)
}

// serve bedient app auf ln, mit TLS, wenn tlsConf gesetzt ist.
func (s *Server) serve(app *fiber.App, ln net.Listener, tlsConf *tls.Config) error {
	if tlsConf != nil {
		ln = tls.NewListener(ln, tlsConf)
	}
	return app.Listener(ln, fiber.ListenConfig{DisableStartupMessage: true})
}

// AttachShared hängt den laufenden Server an den gemeinsamen TLS-Port, der
// erst jetzt (neu) öffnet. Ohne laufenden Server oder ohne gemeinsamen Port
// passiert nichts.
func (s *Server) AttachShared() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.app != nil {
		s.attachSharedLocked(s.app)
	}
}

// attachSharedLocked schließt app an den gemeinsamen Port an (Aufrufer hält
// s.mu). Ein bisheriger Anschluss wird ersetzt.
func (s *Server) attachSharedLocked(app *fiber.App) {
	if s.sharedLn != nil {
		_ = s.sharedLn.Close()
		s.sharedLn = nil
	}
	if s.shared == nil {
		return
	}
	if ln := s.shared(); ln != nil {
		s.sharedLn = ln
		s.serveShared(app, ln)
	}
}

// serveShared bedient den gemeinsamen TLS-Port mit derselben App.
func (s *Server) serveShared(app *fiber.App, ln net.Listener) {
	safego.Go("mcp-shared-listener", func() {
		if err := app.Listener(ln, fiber.ListenConfig{DisableStartupMessage: true}); err != nil && !errors.Is(err, net.ErrClosed) {
			s.logger.Error("MCP listener at the shared port terminated", "error", err)
		}
	})
}

// Shutdown stoppt den Listener (für den Prozess-Shutdown).
func (s *Server) Shutdown() {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.stopLocked()
}

// stopLocked fährt den laufenden Listener herunter (Aufrufer hält s.mu). Der
// Kanal schließt, wenn er fertig ist; ohne laufenden Listener ist er schon zu.
func (s *Server) stopLocked() <-chan struct{} {
	stopped := make(chan struct{})
	if s.app == nil {
		close(stopped)
		return stopped
	}
	app := s.app
	s.app = nil
	s.addr = ""
	if s.sharedLn != nil {
		// Auch wenn die App ihn noch nicht bedient: Sonst bliebe er als Ziel
		// angeschlossen, und niemand nähme die Verbindungen an.
		_ = s.sharedLn.Close()
		s.sharedLn = nil
	}
	// ShutdownWithTimeout schließt den Listener zeitnah; die Listen-Goroutine
	// kehrt danach zurück.
	safego.Go("mcp-shutdown", func() {
		defer close(stopped)
		_ = app.ShutdownWithTimeout(3 * time.Second)
	})
	s.logger.Info("MCP listener stopped")
	return stopped
}

// buildApp baut die schlanke Fiber-App: NUR der /mcp-JSON-RPC-Endpunkt, keine
// UI, keine REST-API.
func (s *Server) buildApp() *fiber.App {
	app := fiber.New(fiber.Config{
		AppName:     "LCM-MCP",
		ReadTimeout: 30 * time.Second,
		IdleTimeout: 120 * time.Second,
		// Der Status des Fehlers (403 des Netzbereich-Filters), sonst 500.
		ErrorHandler: func(c fiber.Ctx, err error) error { return c.SendStatus(apierr.StatusOf(err)) },
	})
	app.Use(recover.New())
	for _, guard := range s.guards {
		app.Use(guard)
	}
	app.Use(func(c fiber.Ctx) error {
		c.Set("X-Content-Type-Options", "nosniff")
		return c.Next()
	})
	app.Post("/mcp", s.handler.Fiber())
	// GET /mcp wird für den stateless Request/Response-Betrieb nicht genutzt
	// (kein server-initiiertes SSE) - klar mit 405 beantworten.
	app.Get("/mcp", func(c fiber.Ctx) error {
		return c.Status(fiber.StatusMethodNotAllowed).JSON(fiber.Map{"error": "use POST /mcp (JSON-RPC 2.0)"})
	})
	return app
}
