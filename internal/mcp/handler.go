package mcp

import (
	"encoding/json"
	"strings"

	"github.com/gofiber/fiber/v3"
)

// mcpProtocolVersion ist die von diesem Server angebotene MCP-Protokollversion.
// Bei initialize wird die vom Client angeforderte Version gespiegelt, wenn
// vorhanden - ansonsten diese.
const mcpProtocolVersion = "2025-06-18"

// AuthFunc prüft einen Bearer-Token und liefert true, wenn es ein gültiger,
// aktiver API-Key mit MCP-Scope ist.
type AuthFunc func(token string) bool

// Handler bündelt die Abhängigkeiten des MCP-JSON-RPC-Endpunkts.
type Handler struct {
	provider     Provider
	auth         AuthFunc
	serverName   string
	serverVers   string
	instructions string
}

// NewHandler erstellt den MCP-Handler.
func NewHandler(provider Provider, auth AuthFunc, serverName, serverVersion string) *Handler {
	return &Handler{
		provider:   provider,
		auth:       auth,
		serverName: serverName,
		serverVers: serverVersion,
		instructions: "Read-only-Zugriff auf die von LCM verwalteten Server. " +
			"Nutze list_servers/get_server/fleet_summary, um Eigenschaften abzurufen. " +
			"Es gibt keine schreibenden Aktionen und keine Zugangsdaten.",
	}
}

// --- JSON-RPC 2.0 Nachrichten ---

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"` // fehlt/null bei Notifications
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

const (
	codeParse          = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternal       = -32603
)

// Fiber gibt den POST-/mcp-Handler zurück. Authentifizierung per
// `Authorization: Bearer <api-key>` (MCP-Scope-Pflicht).
func (h *Handler) Fiber() fiber.Handler {
	return func(c fiber.Ctx) error {
		// Auth zuerst - ohne gültigen MCP-Key kein Zugriff.
		token := bearerToken(c.Get("Authorization"))
		if token == "" || h.auth == nil || !h.auth(token) {
			c.Set("WWW-Authenticate", `Bearer realm="LCM MCP"`)
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "ungültiger oder fehlender MCP-API-Key (Authorization: Bearer …)",
			})
		}

		var req rpcRequest
		if err := json.Unmarshal(c.Body(), &req); err != nil {
			return c.JSON(errResponse(nil, codeParse, "JSON konnte nicht geparst werden"))
		}
		if req.JSONRPC != "2.0" || req.Method == "" {
			return c.JSON(errResponse(req.ID, codeInvalidRequest, "ungültige JSON-RPC-2.0-Anfrage"))
		}

		// Notifications (ohne ID, z.B. notifications/initialized) werden
		// quittiert, ohne dass ein Ergebnis zurückkommt (HTTP 202).
		isNotification := len(req.ID) == 0 || string(req.ID) == "null"

		switch req.Method {
		case "initialize":
			return c.JSON(okResponse(req.ID, h.initializeResult(req.Params)))
		case "notifications/initialized", "notifications/cancelled":
			return c.SendStatus(fiber.StatusAccepted)
		case "ping":
			return c.JSON(okResponse(req.ID, map[string]any{}))
		case "tools/list":
			return c.JSON(okResponse(req.ID, map[string]any{"tools": toolDefs}))
		case "tools/call":
			var p struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			if len(req.Params) > 0 {
				if err := json.Unmarshal(req.Params, &p); err != nil {
					return c.JSON(errResponse(req.ID, codeInvalidParams, "ungültige tools/call-Parameter"))
				}
			}
			if p.Name == "" {
				return c.JSON(errResponse(req.ID, codeInvalidParams, "tool-name fehlt"))
			}
			return c.JSON(okResponse(req.ID, h.callTool(p.Name, p.Arguments)))
		default:
			if isNotification {
				return c.SendStatus(fiber.StatusAccepted)
			}
			return c.JSON(errResponse(req.ID, codeMethodNotFound, "unbekannte Methode: "+req.Method))
		}
	}
}

// initializeResult beantwortet den MCP-Handshake.
func (h *Handler) initializeResult(params json.RawMessage) map[string]any {
	version := mcpProtocolVersion
	if len(params) > 0 {
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if json.Unmarshal(params, &p) == nil && p.ProtocolVersion != "" {
			version = p.ProtocolVersion // die Version des Clients spiegeln
		}
	}
	return map[string]any{
		"protocolVersion": version,
		"capabilities":    map[string]any{"tools": map[string]any{}},
		"serverInfo":      map[string]any{"name": h.serverName, "version": h.serverVers},
		"instructions":    h.instructions,
	}
}

func okResponse(id json.RawMessage, result any) rpcResponse {
	return rpcResponse{JSONRPC: "2.0", ID: id, Result: result}
}

func errResponse(id json.RawMessage, code int, msg string) rpcResponse {
	return rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}}
}

// bearerToken extrahiert den Token aus einem "Bearer <token>"-Header.
func bearerToken(header string) string {
	if t, ok := strings.CutPrefix(header, "Bearer "); ok {
		return strings.TrimSpace(t)
	}
	return ""
}
