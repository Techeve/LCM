package controllers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/fasthttp/websocket"
	"github.com/gofiber/fiber/v3"

	"LCM/internal/core/services"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/safego"
	"LCM/internal/storage/repositories"
)

// TerminalController verbindet den Browser mit einer Shell auf einem Server.
//
// Der Weg ist zweistufig, und das hat einen Grund: Die WebSocket-Schnittstelle
// des Browsers kann keine eigenen Kopfzeilen setzen, der Anmelde-Token käme
// also nicht mit. Statt ihn an die URL zu hängen - wo er in Zugriffs- und
// Proxy-Protokollen landete, und dieser Token öffnet eine Root-Shell - holt
// der Browser zuerst eine Einmal-Fahrkarte über den normal authentifizierten
// Weg und baut damit die Verbindung auf (siehe services.TerminalTickets).
type TerminalController struct {
	servers *services.ServerService
	tickets *services.TerminalTickets
}

func NewTerminalController(servers *services.ServerService, tickets *services.TerminalTickets) *TerminalController {
	return &TerminalController{servers: servers, tickets: tickets}
}

// Ticket - POST /api/v1/servers/:id/terminal/ticket (servers:console)
func (ctrl *TerminalController) Ticket(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	token, err := ctrl.tickets.Issue(actor(c), id)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "fahrkarte konnte nicht erzeugt werden")
	}
	return c.JSON(fiber.Map{"ticket": token, "expires_in_seconds": 30})
}

// wsMessage ist das Protokoll zwischen Browser und Server. Tastenanschläge
// gehen als Text; alles andere trägt einen Typ, damit eine Größenänderung
// nicht als Eingabe im Terminal landet.
type wsMessage struct {
	Type string `json:"type"`           // "resize"
	Cols int    `json:"cols,omitempty"` // bei "resize"
	Rows int    `json:"rows,omitempty"`
}

// Connect - GET /api/v1/servers/:id/terminal?ticket=… (Fahrkarte statt Anmeldung)
//
// Der Endpunkt läuft OHNE die übliche Anmelde-Middleware: Die Fahrkarte ist
// hier der Nachweis, und sie ist strenger als der Token - einmalig, dreißig
// Sekunden gültig, an genau diesen Server gebunden.
func (ctrl *TerminalController) Connect(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	who, ok := ctrl.tickets.Redeem(c.Query("ticket"), id)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "ungültige oder abgelaufene fahrkarte")
	}

	term, err := ctrl.servers.OpenTerminal(repositories.ScopeAll(), id, who,
		c.Query("term"), queryInt(c, "cols"), queryInt(c, "rows"))
	if err != nil {
		return mapTerminalError(err)
	}

	upgrader := websocket.FastHTTPUpgrader{}
	upErr := upgrader.Upgrade(c.RequestCtx(), func(ws *websocket.Conn) {
		// Ab hier ist die Verbindung gekapert und läuft in einer eigenen
		// Goroutine - außerhalb der recover-Middleware des Routers. Ohne
		// diesen Schutz beendete ein Panic hier den ganzen Dienst.
		defer safego.Recover("terminal-ws", nil)
		defer ws.Close()
		defer term.Close()
		pump(ws, term)
	})
	if upErr != nil {
		term.Close()
		return fiber.NewError(fiber.StatusBadRequest, "websocket-upgrade fehlgeschlagen")
	}
	return nil
}

// pump führt die beiden Richtungen zusammen: Was der Server ausgibt, geht an
// den Browser; was der Browser schickt, geht in die Shell.
//
// Die Ausgaberichtung läuft in einer eigenen Goroutine, die Eingaberichtung
// hier - so beendet das Ende der einen auch die andere, egal welche Seite
// zuerst geht.
func pump(ws *websocket.Conn, term sshx.Terminal) {
	done := make(chan struct{})
	safego.Go("terminal-out", func() {
		defer close(done)
		buf := make([]byte, 4096)
		for {
			n, err := term.Read(buf)
			if n > 0 {
				if werr := ws.WriteMessage(websocket.BinaryMessage, buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				return // Shell beendet oder Verbindung weg
			}
		}
	})

	for {
		// Die Leerlauf-Frist: Eine Sitzung, in der nichts mehr getippt wird,
		// endet von selbst. Ohne sie hielte ein vergessener Browser-Tab den
		// Exec-Slot des Servers dauerhaft besetzt - dort liefen dann keine
		// Zeitpläne mehr, ohne dass es jemandem auffiele.
		if err := ws.SetReadDeadline(time.Now().Add(services.TerminalIdleTimeout)); err != nil {
			break
		}
		kind, data, err := ws.ReadMessage()
		if err != nil {
			break
		}
		select {
		case <-done:
			return // die Shell ist weg
		default:
		}
		if kind == websocket.TextMessage && len(data) > 0 && data[0] == '{' {
			var msg wsMessage
			if json.Unmarshal(data, &msg) == nil && msg.Type == "resize" {
				if rerr := term.Resize(msg.Cols, msg.Rows); rerr != nil {
					slog.Debug("terminal resize failed", "error", rerr)
				}
				continue
			}
		}
		if _, err := term.Write(data); err != nil {
			break
		}
	}
	<-done
}

// mapTerminalError übersetzt die Absagen der Konsole in HTTP-Antworten, die
// den Grund benennen - „geht nicht" allein hilft niemandem weiter.
func mapTerminalError(err error) error {
	switch {
	case errors.Is(err, services.ErrTerminalDisabled):
		return fiber.NewError(fiber.StatusForbidden, err.Error())
	case errors.Is(err, services.ErrTerminalNotPossible),
		errors.Is(err, services.ErrTerminalUnsupported):
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	case errors.Is(err, services.ErrTooManyConnections):
		return fiber.NewError(fiber.StatusConflict,
			"auf diesem Server läuft gerade etwas anderes - bitte kurz warten")
	}
	return mapServerError(err)
}

// queryInt liest eine Zahl aus der Abfrage; 0 bei Unsinn (das Terminal setzt
// dann seine Vorgabe, siehe sshx.clampWindow).
func queryInt(c fiber.Ctx, key string) int {
	return fiber.Query[int](c, key)
}
