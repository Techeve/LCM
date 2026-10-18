package controllers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/fasthttp/websocket"
	"github.com/gofiber/fiber/v3"

	"LCM/internal/api/middlewares"
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

// SetEnabled - PUT /api/v1/servers/:id/console (servers:console)
//
// Schaltet die Web-Konsole für einen Server ab oder wieder frei. Die Route
// hängt am Konsolen-Recht, nicht an servers:write: Wer Server konfigurieren
// darf, soll sich die Shell nicht selbst freischalten können.
func (ctrl *TerminalController) SetEnabled(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	var req struct {
		Disabled bool `json:"disabled"`
	}
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültige Anfrage")
	}
	if err := ctrl.servers.SetConsoleDisabled(scopeFor(c), id, req.Disabled, actor(c)); err != nil {
		return mapServerError(err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// Ticket - POST /api/v1/servers/:id/terminal/ticket (servers:console)
//
// Die Sichtbarkeit wird HIER geprüft, nicht erst beim Verbinden: Connect
// kennt den Benutzer nur noch über die Fahrkarte und öffnet mit ScopeAll.
// Ohne die Prüfung reichte das Konsolen-Recht, um eine Fahrkarte für JEDEN
// Server zu ziehen - auch außerhalb der eigenen Gruppen.
func (ctrl *TerminalController) Ticket(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	server, err := ctrl.servers.Get(scopeFor(c), id)
	if err != nil {
		return mapServerError(err)
	}
	token, err := ctrl.tickets.Issue(actor(c), id, middlewares.ClientIP(c))
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "fahrkarte konnte nicht erzeugt werden")
	}
	middlewares.SecurityEvent(c, "terminal.ticket.ok", "user", actor(c), "server", server.Name)
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
// Sekunden gültig, an genau diesen Server und an die Client-Adresse gebunden,
// die sie geholt hat.
func (ctrl *TerminalController) Connect(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	who, ok := ctrl.tickets.Redeem(c.Query("ticket"), id, middlewares.ClientIP(c))
	if !ok {
		middlewares.SecurityEvent(c, "terminal.ticket.rejected")
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

// wsConn ist der Teil des WebSockets, den pump braucht - eigene
// Schnittstelle, damit sich das Schließverhalten ohne Netz testen lässt.
type wsConn interface {
	ReadMessage() (int, []byte, error)
	WriteMessage(messageType int, data []byte) error
	SetReadDeadline(t time.Time) error
	Close() error
}

// closeSocket beendet die Verbindung zum Browser, nachdem die Shell zu Ende
// ist: erst eine ordentliche Close-Nachricht, dann die Lesefrist auf „jetzt"
// - das weckt die Eingabe-Schleife, die sonst auf den nächsten Tastendruck
// wartete - und zuletzt die Verbindung selbst.
func closeSocket(ws wsConn) {
	_ = ws.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "sitzung beendet"))
	_ = ws.SetReadDeadline(time.Now())
	_ = ws.Close()
}

// pump führt die beiden Richtungen zusammen: Was der Server ausgibt, geht an
// den Browser; was der Browser schickt, geht in die Shell.
//
// Entscheidend ist das Ende, und es kann von beiden Seiten kommen:
//   - Browser geht (Beenden, Tab zu) oder die Leerlauf-Frist läuft ab: Die
//     Eingabe endet, die Sitzung wird SOFORT geschlossen. Erst das beendet
//     das blockierende term.Read der Ausgabe - vorher wartete pump auf eine
//     Ausgabe, die bei einer ruhigen Shell nie kam, und die Sitzung samt
//     SSH-Verbindung blieb für immer offen.
//   - Shell endet (exit): Die Ausgabe endet und schließt den WebSocket; das
//     beendet die wartende Eingabe.
func pump(ws wsConn, term sshx.Terminal) {
	done := make(chan struct{})
	safego.Go("terminal-out", func() {
		defer close(done)
		copyOutput(ws, term)
		closeSocket(ws)
	})
	copyInput(ws, term)
	term.Close()
	<-done
}

// copyOutput reicht die Ausgabe der Shell an den Browser, bis eine Seite weg ist.
func copyOutput(ws wsConn, term sshx.Terminal) {
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
}

// copyInput reicht Tastatureingaben und Größenänderungen an die Shell, bis
// der Browser geht oder die Leerlauf-Frist abläuft: Eine Sitzung, in der
// nichts mehr getippt wird, endet von selbst - sonst hielte ein vergessener
// Browser-Tab die Shell auf dem Server dauerhaft offen.
func copyInput(ws wsConn, term sshx.Terminal) {
	for {
		if err := ws.SetReadDeadline(time.Now().Add(services.TerminalIdleTimeout)); err != nil {
			return
		}
		kind, data, err := ws.ReadMessage()
		if err != nil {
			return
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
			return
		}
	}
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
	case errors.Is(err, services.ErrTooManyConsoles):
		return fiber.NewError(fiber.StatusConflict, err.Error())
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
