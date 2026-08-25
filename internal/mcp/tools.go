package mcp

import (
	"encoding/json"
	"fmt"
)

// Provider liefert die read-only Serverdaten für die MCP-Tools. Die
// Implementierung (services.MCPProvider) baut die ServerViews aus dem
// ServerService und wendet dabei die Whitelist an.
type Provider interface {
	// Servers liefert die Sichten aller (für den Key sichtbaren) Server.
	Servers() ([]ServerView, error)
	// Server liefert eine Server-Sicht per numerischer ID oder exaktem Namen;
	// found=false, wenn keiner passt.
	Server(idOrName string) (view *ServerView, found bool, err error)
}

// toolDef beschreibt ein MCP-Tool (Name, Beschreibung, JSON-Schema der
// Argumente) für tools/list.
type toolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// toolDefs ist der statische Katalog der angebotenen Tools - allesamt
// read-only. Es gibt bewusst KEINE schreibenden/konfigurierenden Tools.
var toolDefs = []toolDef{
	{
		Name:        "list_servers",
		Description: "Listet alle von LCM verwalteten Server mit ihren read-only Eigenschaften (Betriebssystem, Version, Status, Erreichbarkeit, Hardware, Update-Stand, Sicherheitslage). Enthält keine Zugangsdaten.",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
	},
	{
		Name:        "get_server",
		Description: "Liefert die Eigenschaften eines einzelnen Servers, adressiert per numerischer ID oder exaktem Namen.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"server": map[string]any{"type": "string", "description": "Server-ID oder exakter Name"},
			},
			"required": []string{"server"},
		},
	},
	{
		Name:        "fleet_summary",
		Description: "Aggregierte Flottensicht: Anzahl Server gesamt, erreichbar/nicht erreichbar, Verteilung nach Ampel-Status, Server mit verfügbaren Updates und mit kritischen CVEs.",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
	},
}

// callTool führt ein Tool aus und liefert das MCP-Tool-Result (content[]).
// Ein unbekanntes Tool bzw. ungültige Argumente ergeben ein isError-Result
// (kein Protokoll-Fehler - so sieht der Agent die Ursache).
func (h *Handler) callTool(name string, args json.RawMessage) toolResult {
	switch name {
	case "list_servers":
		views, err := h.provider.Servers()
		if err != nil {
			return errorResult("Server konnten nicht gelesen werden: " + err.Error())
		}
		return jsonResult(map[string]any{"servers": views, "count": len(views)})

	case "get_server":
		var in struct {
			Server string `json:"server"`
		}
		if len(args) > 0 {
			_ = json.Unmarshal(args, &in)
		}
		if in.Server == "" {
			return errorResult("Argument 'server' (ID oder Name) ist erforderlich")
		}
		view, found, err := h.provider.Server(in.Server)
		if err != nil {
			return errorResult("Server konnte nicht gelesen werden: " + err.Error())
		}
		if !found {
			return errorResult(fmt.Sprintf("Kein Server mit ID/Name %q gefunden", in.Server))
		}
		return jsonResult(view)

	case "fleet_summary":
		views, err := h.provider.Servers()
		if err != nil {
			return errorResult("Server konnten nicht gelesen werden: " + err.Error())
		}
		return jsonResult(summarize(views))

	default:
		return errorResult("Unbekanntes Tool: " + name)
	}
}

// toolResult ist das MCP-Ergebnis eines tools/call.
type toolResult struct {
	Content []toolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type toolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// jsonResult verpackt ein beliebiges Objekt als einzelnen Text-Content
// (kompaktes JSON) - das gängige Ausgabeformat für MCP-Tools.
func jsonResult(v any) toolResult {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return errorResult("Ergebnis konnte nicht serialisiert werden")
	}
	return toolResult{Content: []toolContent{{Type: "text", Text: string(b)}}}
}

// errorResult liefert eine Fehlermeldung als Tool-Result (isError=true).
func errorResult(msg string) toolResult {
	return toolResult{Content: []toolContent{{Type: "text", Text: msg}}, IsError: true}
}
