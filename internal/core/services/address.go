package services

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"LCM/internal/core/domain"
	"LCM/internal/inputcheck"
)

// Adressmodus, Ports und die daraus abgeleiteten Adressen. Die Rechnung steht
// an EINER Stelle (Addresses): Enroll-Befehl, MCP-Adresse, Join-Assistent und
// Mail-Links lesen ihre Adressen von dort und bauen sie nirgends nach.

// FixedPort ist ein Port, den eine Umgebungsvariable festlegt.
type FixedPort struct {
	// Set: Der Wert gilt und sperrt das Feld in der Oberfläche.
	Set bool
	// Value ist der festgelegte Port; 0 bei Set heißt: der Dienst ist
	// abgeschaltet (LCM_AGENT_PORT=0).
	Value int
}

// AddressConfig sagt, was die Umgebung für Ports und Adressen festlegt. Der
// Rest kommt aus den Einstellungen in der Datenbank; die config.json spiegelt
// sie nur (siehe cmd/app/config_sync.go) und sperrt kein Feld.
type AddressConfig struct {
	// UI und Agent sind gesetzt, wenn LCM_PORT bzw. LCM_AGENT_PORT den Port
	// festlegen: LCM kann eine Umgebungsvariable nicht ändern.
	UI, Agent FixedPort
	// ACMEHTTPPort ist der Port des HTTP-01-Listeners (acme_http_listen); 0 = Vorgabe.
	ACMEHTTPPort int
	// Scheme ist das Schema der Adressen, die LCM selbst bildet: https, im
	// Entwicklungsmodus http.
	Scheme string
	// FallbackHost ist der Name, den die Adressen tragen, solange keine
	// öffentliche Adresse gesetzt ist: der Bind-Host der Oberfläche, bei einer
	// Wildcard-Adresse localhost (siehe LinkBaseURL).
	FallbackHost string
}

// AddressLocks sagt je Feld, ob es eine Umgebungsvariable festlegt. MCP und der
// gemeinsame Port haben keine; ihre Schlüssel bleiben der Form halber, immer falsch.
type AddressLocks struct {
	UIPort        bool `json:"ui_port"`
	AgentPort     bool `json:"agent_port"`
	MCPPort       bool `json:"mcp_port"`
	SharedTLSPort bool `json:"shared_tls_port"`
	// AgentEnabled: LCM_AGENT_PORT=0 schaltet den Agenten ab. Im Modus C gilt
	// das nicht - dort bedient der gemeinsame Port den Agenten, und 0 heißt nur:
	// kein eigener Port daneben.
	AgentEnabled bool `json:"agent_enabled"`
}

// EffectiveURLs sind die Adressen, die LCM tatsächlich in Befehle und Links
// schreibt. Leer: keine bekannt.
type EffectiveURLs struct {
	UI    string `json:"ui"`
	Agent string `json:"agent"`
	MCP   string `json:"mcp"`
}

// Addresses ist der wirksame Stand: Modus, Ports (Wert der Umgebung, sonst der
// Datenbank), welche davon gesperrt sind, und die abgeleiteten Adressen.
type Addresses struct {
	Mode          string
	UIPort        int
	AgentPort     int // 0: der Agent-Listener ist abgeschaltet
	MCPPort       int
	SharedTLSPort int
	// AgentEnabled und MCPEnabled sind die Schalter der Dienste, wie sie gelten:
	// Der Agent ist aus, wo die Umgebung ihn abschaltet (LCM_AGENT_PORT=0).
	AgentEnabled bool
	MCPEnabled   bool
	Locks        AddressLocks
	URLs         EffectiveURLs
}

// SharedPortActive meldet, ob der gemeinsame TLS-Port läuft.
func (a Addresses) SharedPortActive() bool { return a.Mode == domain.AddressModeSharedPort }

// AgentListenPort ist der Port des eigenen Agent-Listeners; 0, wenn es keinen
// gibt (Agent aus, oder LCM_AGENT_PORT=0).
func (a Addresses) AgentListenPort() int {
	if !a.AgentEnabled {
		return 0
	}
	return a.AgentPort
}

// WithAddressConfig hinterlegt, was die Umgebung festlegt.
func (s *SettingsService) WithAddressConfig(cfg AddressConfig) *SettingsService {
	s.addr = cfg
	return s
}

// Addresses liefert den wirksamen Stand aus den gespeicherten Einstellungen.
func (s *SettingsService) Addresses() (Addresses, error) {
	st, err := s.settings.Get()
	if err != nil {
		return Addresses{}, err
	}
	return s.AddressesOf(st), nil
}

// AddressesOf rechnet den wirksamen Stand für beliebige Einstellungen aus; so
// prüft das Speichern den Zustand NACH der Änderung.
func (s *SettingsService) AddressesOf(st *domain.GlobalSettings) Addresses {
	a := Addresses{
		Mode:          st.Mode(),
		UIPort:        pickPort(s.addr.UI, st.UIPort, domain.DefaultUIPort),
		AgentPort:     pickPort(s.addr.Agent, st.AgentPort, domain.DefaultAgentPort),
		MCPPort:       pickPort(FixedPort{}, st.MCPPort, domain.DefaultMCPPort),
		SharedTLSPort: pickPort(FixedPort{}, st.SharedTLSPort, domain.DefaultSharedTLSPort),
		Locks:         AddressLocks{UIPort: s.addr.UI.Set, AgentPort: s.addr.Agent.Set},
	}
	a.Locks.AgentEnabled = s.addr.Agent.Set && s.addr.Agent.Value == 0 && !a.SharedPortActive()
	a.AgentEnabled = st.AgentEnabled && !a.Locks.AgentEnabled
	a.MCPEnabled = st.MCPEnabled
	a.URLs = s.deriveURLs(st, a)
	return a
}

// pickPort wählt den Wert der Umgebung, sonst den der Datenbank; ein
// ungesetzter Wert der Datenbank (0) ist die Vorgabe.
func pickPort(fixed FixedPort, stored, def int) int {
	switch {
	case fixed.Set:
		return fixed.Value
	case stored == 0:
		return def
	}
	return stored
}

// deriveURLs rechnet die drei Adressen aus. B: https://<public_host>:<port>.
// C: https://<name>, mit :port nur, wenn der gemeinsame Port nicht 443 ist.
// A: die eingestellten öffentlichen Adressen; fehlt eine, gilt der Host der
// Oberfläche mit dem Port des Dienstes.
func (s *SettingsService) deriveURLs(st *domain.GlobalSettings, a Addresses) EffectiveURLs {
	switch a.Mode {
	case domain.AddressModeOneName:
		return s.oneNameURLs(st, a)
	case domain.AddressModeSharedPort:
		return sharedPortURLs(st, a)
	}
	return s.separateURLs(st, a)
}

// oneNameURLs: ein Name für alle drei Dienste, jeder mit seinem Port. Schema
// und ein abgeschalteter Agent-Listener gelten wie im Modus A.
func (s *SettingsService) oneNameURLs(st *domain.GlobalSettings, a Addresses) EffectiveURLs {
	host := st.PublicHost
	if host == "" {
		return EffectiveURLs{}
	}
	scheme := s.scheme()
	urls := EffectiveURLs{
		UI:  urlFor(scheme, host, a.UIPort),
		MCP: urlFor(scheme, host, a.MCPPort),
	}
	if a.AgentPort > 0 {
		urls.Agent = urlFor(scheme, host, a.AgentPort)
	}
	return urls
}

func sharedPortURLs(st *domain.GlobalSettings, a Addresses) EffectiveURLs {
	port := a.SharedTLSPort
	if port == domain.DefaultSharedTLSPort {
		port = 0 // 443 steht nicht in der Adresse
	}
	return EffectiveURLs{
		UI:    urlForName("https", st.PublicBaseURL, port),
		Agent: urlForName("https", st.PublicAgentURL, port),
		MCP:   urlForName("https", st.PublicMCPURL, port),
	}
}

func (s *SettingsService) separateURLs(st *domain.GlobalSettings, a Addresses) EffectiveURLs {
	scheme := s.scheme()
	ui := st.PublicBaseURL
	if ui == "" {
		ui = urlFor(scheme, s.addr.FallbackHost, a.UIPort)
	}
	// Agent und MCP tragen ohne eigene Adresse den Host der Oberfläche.
	host := sniHost(ui)
	agent := st.PublicAgentURL
	if agent == "" && a.AgentPort > 0 {
		agent = urlFor(scheme, host, a.AgentPort)
	}
	mcp := st.PublicMCPURL
	if mcp == "" {
		mcp = urlFor(scheme, host, a.MCPPort)
	}
	return EffectiveURLs{UI: ui, Agent: agent, MCP: mcp}
}

func (s *SettingsService) scheme() string {
	if s.addr.Scheme == "" {
		return "https"
	}
	return s.addr.Scheme
}

// urlFor setzt Schema, Host und Port zusammen; Port 0 lässt ihn weg, ein leerer Host ergibt "".
func urlFor(scheme, host string, port int) string {
	if host == "" {
		return ""
	}
	hostPort := host
	if port > 0 {
		hostPort = net.JoinHostPort(host, strconv.Itoa(port))
	} else if strings.Contains(host, ":") {
		hostPort = "[" + host + "]"
	}
	return scheme + "://" + hostPort
}

// urlForName bildet die Adresse eines Dienstes im Modus C aus dem Namen seiner
// eingestellten Adresse; der Port der eingestellten Adresse zählt dort nicht.
func urlForName(scheme, configured string, port int) string {
	return urlFor(scheme, sniHost(configured), port)
}

// sniHost ist der Name einer Adresse wie sie der Browser vergleicht: ohne
// Port, ohne Klammern. Leer bei leerer oder unlesbarer Adresse.
func sniHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// normalizePublicHost prüft den einen Namen des Modus B: ein Hostname oder
// eine IPv4-Adresse, ohne Schema, Port, Pfad und Zugangsdaten. Leer ist
// zulässig (nicht gesetzt).
func normalizePublicHost(raw string) (string, error) {
	host := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(raw), "."))
	if host == "" {
		return "", nil
	}
	if strings.ContainsAny(host, "/:@?#\\ \t") {
		return "", fmt.Errorf("%w: public_host ist nur der Name, ohne Schema, Port, Pfad und Zugangsdaten (war %q)", ErrSettingInvalid, raw)
	}
	if problem := inputcheck.HostProblem(host); problem != "" {
		return "", fmt.Errorf("%w: public_host %q: %s", ErrSettingInvalid, raw, problem)
	}
	return host, nil
}
