package mcp

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/recover"

	"LCM/internal/safego"
)

// Server verwaltet den MCP-HTTP-Listener zur LAUFZEIT: Er lässt sich über die
// LCM-Einstellungen jederzeit starten, stoppen oder auf eine andere
// Bind-Adresse/Port umschalten (Apply). Der eigentliche Listener läuft in
// einer eigenen Goroutine; Umschalten fährt den alten sauber herunter.
//
// Der Endpunkt spricht einfaches HTTP (kein TLS) - passend zur lokalen
// Standard-Bindung (127.0.0.1) und zu MCP-Clients. Für Fernzugriff gehört ein
// TLS-terminierender Reverse-Proxy oder ein Tunnel davor (siehe Doku).
type Server struct {
	handler *Handler
	logger  *slog.Logger

	mu   sync.Mutex
	app  *fiber.App
	addr string // aktuelle Listen-Adresse, "" = aus
}

// NewServer erstellt den (zunächst gestoppten) MCP-Listener-Manager.
func NewServer(handler *Handler, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{handler: handler, logger: logger}
}

// Apply bringt den Listener in den gewünschten Zustand. enabled=false stoppt
// ihn; enabled=true (neu)startet ihn auf host:port. Läuft er bereits auf genau
// dieser Adresse, passiert nichts.
func (s *Server) Apply(enabled bool, host string, port int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !enabled || port <= 0 {
		s.stopLocked()
		return
	}
	if host == "" {
		host = "127.0.0.1"
	}
	newAddr := fmt.Sprintf("%s:%d", host, port)
	if s.app != nil && s.addr == newAddr {
		return // schon aktiv auf dieser Adresse
	}
	s.stopLocked()

	app := s.buildApp()
	s.app = app
	s.addr = newAddr
	s.logger.Info("MCP listener started", "address", "http://"+newAddr+"/mcp")
	a, addr := app, newAddr
	safego.Go("mcp-listener", func() {
		if err := a.Listen(addr, fiber.ListenConfig{DisableStartupMessage: true}); err != nil {
			s.logger.Error("MCP listener terminated", "address", addr, "error", err)
		}
	})
}

// Shutdown stoppt den Listener (für den Prozess-Shutdown).
func (s *Server) Shutdown() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopLocked()
}

// stopLocked fährt den laufenden Listener herunter (Aufrufer hält s.mu).
func (s *Server) stopLocked() {
	if s.app == nil {
		return
	}
	app := s.app
	s.app = nil
	s.addr = ""
	// ShutdownWithTimeout schließt den Listener zeitnah; die Listen-Goroutine
	// kehrt danach zurück.
	safego.Go("mcp-shutdown", func() {
		_ = app.ShutdownWithTimeout(3 * time.Second)
	})
	s.logger.Info("MCP listener stopped")
}

// buildApp baut die schlanke Fiber-App: NUR der /mcp-JSON-RPC-Endpunkt, keine
// UI, keine REST-API.
func (s *Server) buildApp() *fiber.App {
	app := fiber.New(fiber.Config{
		AppName:      "LCM-MCP",
		ReadTimeout:  30 * time.Second,
		IdleTimeout:  120 * time.Second,
		ErrorHandler: func(c fiber.Ctx, err error) error { return c.SendStatus(fiber.StatusInternalServerError) },
	})
	app.Use(recover.New())
	app.Use(func(c fiber.Ctx) error {
		c.Set("X-Content-Type-Options", "nosniff")
		return c.Next()
	})
	app.Post("/mcp", s.handler.Fiber())
	// GET /mcp wird für den stateless Request/Response-Betrieb nicht genutzt
	// (kein server-initiiertes SSE) - klar mit 405 beantworten.
	app.Get("/mcp", func(c fiber.Ctx) error {
		return c.Status(fiber.StatusMethodNotAllowed).JSON(fiber.Map{"error": "nutze POST /mcp (JSON-RPC 2.0)"})
	})
	return app
}
