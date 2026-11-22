package netfilter

import (
	"fmt"
	"net/netip"
	"strings"
	"sync/atomic"
)

// Endpoint ist einer der drei Dienste, für die ein Admin erlaubte Netzbereiche
// festlegen kann.
type Endpoint string

const (
	EndpointUI    Endpoint = "ui"    // Weboberfläche und REST-API
	EndpointAgent Endpoint = "agent" // Agent-Endpunkt (/mqtt)
	EndpointMCP   Endpoint = "mcp"   // MCP-Schnittstelle
)

// Endpoints nennt alle Endpunkte in fester Reihenfolge.
var Endpoints = []Endpoint{EndpointUI, EndpointAgent, EndpointMCP}

// Label ist der Name des Endpunkts für Menschen.
func (e Endpoint) Label() string {
	switch e {
	case EndpointUI:
		return "Weboberfläche"
	case EndpointAgent:
		return "Agent"
	}
	return "MCP"
}

// ConfigKey ist der Schlüssel in der config.json, der die Liste des Endpunkts
// festlegt.
func (e Endpoint) ConfigKey() string {
	switch e {
	case EndpointUI:
		return "allowed_ips"
	case EndpointAgent:
		return "agent_allowed_ips"
	}
	return "mcp_allowed_ips"
}

// ParseEndpoint macht aus dem Namen in einer URL oder auf der Konsole einen
// Endpunkt.
func ParseEndpoint(name string) (Endpoint, bool) {
	for _, e := range Endpoints {
		if string(e) == name {
			return e, true
		}
	}
	return "", false
}

// MaxNetworkEntries begrenzt die Liste eines Endpunkts, die über die API
// kommt. Mehr braucht kein Betreiber, und jede Anfrage durchläuft die Liste.
const MaxNetworkEntries = 64

// Normalize prüft die Einträge einer Netzliste und liefert sie in einheitlicher
// Schreibweise: Schlüsselwörter klein, Adressen und Netze als CIDR (eine
// einzelne Adresse wird /32 bzw. /128, IPv4-in-IPv6 wird entpackt, Host-Bits
// fallen weg). Leere Einträge und Duplikate entfallen. Der Fehler nennt den
// fehlerhaften Eintrag.
func Normalize(entries []string) ([]string, error) {
	seen := map[string]bool{}
	out := make([]string, 0, len(entries))
	for _, raw := range entries {
		entry, err := normalizeEntry(raw)
		if err != nil {
			return nil, err
		}
		if entry == "" || seen[entry] {
			continue
		}
		seen[entry] = true
		out = append(out, entry)
	}
	if len(out) > MaxNetworkEntries {
		return nil, fmt.Errorf("höchstens %d Einträge erlaubt, es sind %d", MaxNetworkEntries, len(out))
	}
	return out, nil
}

func normalizeEntry(raw string) (string, error) {
	entry := strings.TrimSpace(raw)
	if entry == "" {
		return "", nil
	}
	if isKeyword(entry) {
		return strings.ToLower(entry), nil
	}
	prefix, err := parsePrefix(entry)
	if err != nil {
		return "", fmt.Errorf("ungültiger Eintrag %q: erwartet eine IP-Adresse, ein Netz in CIDR-Schreibweise "+
			"(z. B. 10.0.0.0/8) oder eines der Wörter localhost und private", entry)
	}
	return prefix.String(), nil
}

func isKeyword(entry string) bool {
	switch strings.ToLower(entry) {
	case keywordLocalhost, keywordLoopback, keywordPrivate:
		return true
	}
	return false
}

// Holder hält die Allowlist eines Endpunkts austauschbar zur Laufzeit. Die
// Middleware liest sie bei jeder Anfrage; ein Austausch wirkt sofort und ohne
// Neustart. Der Nullwert und nil lassen alles zu (nil für Apps ohne Liste,
// etwa in Tests).
type Holder struct {
	list atomic.Pointer[Allowlist]
}

// Load liefert die aktuelle Allowlist.
func (h *Holder) Load() Allowlist {
	if h == nil {
		return Allowlist{}
	}
	if list := h.list.Load(); list != nil {
		return *list
	}
	return Allowlist{}
}

// Store ersetzt die Allowlist.
func (h *Holder) Store(list Allowlist) { h.list.Store(&list) }

// Allows prüft addr gegen die aktuelle Allowlist.
func (h *Holder) Allows(addr netip.Addr) bool { return h.Load().Allows(addr) }
