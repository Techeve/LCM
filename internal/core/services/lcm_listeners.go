package services

import (
	"slices"
	"sort"
	"strconv"
	"strings"

	"LCM/internal/core/domain"
)

// lcmProcess ist der Prozessname, unter dem die eigenen Ports von LCM in den
// Firewall-Vorschlägen des LCM-Hosts stehen.
const lcmProcess = "lcm"

// LcmListeners liefert die Ports, auf denen LCM selbst lauscht, aus dem
// Adressmodell (Addresses): Oberfläche, Agent nur bei eingeschaltetem Agenten,
// MCP nur bei eingeschaltetem MCP, der gemeinsame Port im Modus C und Port 80,
// solange Let's Encrypt per HTTP-01 eingerichtet ist (LCM öffnet ihn nur
// während einer Prüfung, die CA braucht ihn aber jederzeit erreichbar).
// Sie speisen die Firewall-Vorschläge des LCM-Hosts, unabhängig davon, wann
// dessen Sockets zuletzt eingelesen wurden.
func (s *SettingsService) LcmListeners() ([]domain.ListeningPort, error) {
	st, err := s.settings.Get()
	if err != nil {
		return nil, err
	}
	a := s.AddressesOf(st)
	roles := lcmPortRoles{}
	roles.add(a.UIPort, domain.LcmRoleUI)
	roles.add(a.AgentListenPort(), domain.LcmRoleAgent)
	if a.MCPEnabled {
		roles.add(a.MCPPort, domain.LcmRoleMCP)
	}
	if a.SharedPortActive() {
		roles.addShared(a)
	}
	if st.ACMEEnabled && st.ACMEChallenge != ACMEChallengeDNS01 {
		roles.add(s.acmeHTTPPort(), domain.LcmRoleACME)
	}
	return roles.listeners(), nil
}

// lcmPortRoles sammelt je Port, welche Schnittstellen von LCM dort lauschen.
// Die Reihenfolge der Ports ist die des ersten Eintrags, die der Rollen fest:
// ui, agent, mcp, acme (so wird hinzugefügt).
type lcmPortRoles struct {
	ports []int
	roles map[int][]string
}

// add trägt eine Schnittstelle auf einem Port ein; Port 0 (kein Listener) und
// doppelte Einträge fallen weg.
func (r *lcmPortRoles) add(port int, role string) {
	if port <= 0 {
		return
	}
	if r.roles == nil {
		r.roles = map[int][]string{}
	}
	if _, seen := r.roles[port]; !seen {
		r.ports = append(r.ports, port)
	}
	if !slices.Contains(r.roles[port], role) {
		r.roles[port] = append(r.roles[port], role)
	}
}

// addShared trägt den gemeinsamen Port des Modus C ein: Er bedient immer die
// Oberfläche, den Agenten und MCP nur, solange sie eingeschaltet sind (ohne
// eigenen Namen sind sie dort aus). Ihre eigenen Ports laufen daneben weiter.
func (r *lcmPortRoles) addShared(a Addresses) {
	r.add(a.SharedTLSPort, domain.LcmRoleUI)
	if a.AgentEnabled {
		r.add(a.SharedTLSPort, domain.LcmRoleAgent)
	}
	if a.MCPEnabled {
		r.add(a.SharedTLSPort, domain.LcmRoleMCP)
	}
}

// sortedRoles bringt die Rollen in die feste Reihenfolge ui, agent, mcp, acme.
func sortedRoles(roles []string) []string {
	order := []string{domain.LcmRoleUI, domain.LcmRoleAgent, domain.LcmRoleMCP, domain.LcmRoleACME}
	out := make([]string, 0, len(roles))
	for _, role := range order {
		if slices.Contains(roles, role) {
			out = append(out, role)
		}
	}
	return out
}

func (r *lcmPortRoles) listeners() []domain.ListeningPort {
	out := make([]domain.ListeningPort, 0, len(r.ports))
	for _, port := range r.ports {
		out = append(out, domain.ListeningPort{
			Port: port, Proto: "tcp", Bind: "0.0.0.0", IPVersion: "v4",
			Process: lcmProcess, LcmRoles: sortedRoles(r.roles[port]),
		})
	}
	return out
}

// withLcmListeners ergänzt das Port-Inventar des LCM-Hosts um die eigenen
// Ports von LCM. Ein Port, den der Socket-Scan schon kennt (gleiche Nummer und
// Protokoll), behält Bind-Adresse, IP-Version und Prozess aus dem Scan - der
// weiß sie genauer -, bekommt aber die Schnittstellen von LCM (LcmRoles) für
// jeden gescannten Socket dieses Ports, IPv4 wie IPv6.
func withLcmListeners(scanned, own []domain.ListeningPort) []domain.ListeningPort {
	ownRoles := map[string][]string{}
	for _, p := range own {
		ownRoles[listeningKey(p)] = p.LcmRoles
	}
	known := map[string]bool{}
	merged := make([]domain.ListeningPort, 0, len(scanned)+len(own))
	for _, p := range scanned {
		known[listeningKey(p)] = true
		p.LcmRoles = ownRoles[listeningKey(p)]
		merged = append(merged, p)
	}
	for _, p := range own {
		if !known[listeningKey(p)] {
			known[listeningKey(p)] = true
			merged = append(merged, p)
		}
	}
	sort.SliceStable(merged, func(i, j int) bool {
		if merged[i].Port != merged[j].Port {
			return merged[i].Port < merged[j].Port
		}
		return merged[i].Proto < merged[j].Proto
	})
	return merged
}

func listeningKey(p domain.ListeningPort) string {
	return strings.ToLower(p.Proto) + "/" + strconv.Itoa(p.Port)
}

// WithLcmListeners hinterlegt, woher die eigenen Ports von LCM kommen (siehe
// SettingsService.LcmListeners). Sie stehen in den Firewall-Vorschlägen des
// LCM-Hosts. Optional; ohne sie zeigt der Host nur, was der Scan gesehen hat.
func (s *ServerService) WithLcmListeners(fn func() ([]domain.ListeningPort, error)) *ServerService {
	s.lcmListeners = fn
	return s
}

// fillLcmListeners legt die eigenen Ports von LCM in das Inventar des
// LCM-Hosts (nur in der Antwort, nicht in der Datenbank): So fehlt dort auch
// dann nichts, wenn der Scan älter ist als die letzte Änderung der Adressen.
func (s *ServerService) fillLcmListeners(server *domain.Server) {
	if s.lcmListeners == nil || !server.IsLcmHost() || server.IsDemo {
		return
	}
	own, err := s.lcmListeners()
	if err != nil {
		return
	}
	server.ListeningPorts = listeningPortsJSON(withLcmListeners(parseListeningPortsJSON(server.ListeningPorts), own))
}
