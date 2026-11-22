package services

import (
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"strings"
	"sync"

	"LCM/internal/core/domain"
	"LCM/internal/netfilter"
	"LCM/internal/storage/repositories"
)

// ErrNetworkLockout: Die neue Liste der Weboberfläche schlösse den Admin aus,
// der sie gerade setzt.
var ErrNetworkLockout = errors.New("die eigene Adresse liegt nicht in der neuen Liste - die Weboberfläche wäre für diesen Admin gesperrt")

// NetworkInvalidError: Die Liste enthält einen ungültigen Eintrag oder ist zu
// lang. Detail nennt den Grund.
type NetworkInvalidError struct{ Detail string }

func (e *NetworkInvalidError) Error() string { return e.Detail }

// AgentsOutsideError: Die neue Agent-Liste schlösse verbundene Agenten aus.
// Sie werden erst getrennt, wenn der Admin das bestätigt.
type AgentsOutsideError struct{ Agents []domain.AgentPeer }

func (e *AgentsOutsideError) Error() string {
	return fmt.Sprintf("%d verbundene Agenten liegen außerhalb der neuen Liste", len(e.Agents))
}

// AgentPeers liefert die verbundenen Agenten und trennt sie (der Hub).
type AgentPeers interface {
	ConnectedPeers() []domain.AgentPeer
	Disconnect(agentID string)
}

// NetworkView ist der Stand der Netzbereiche eines Endpunkts.
type NetworkView struct {
	Endpoint string   `json:"endpoint"`
	Allowed  []string `json:"allowed"` // leer = alle Netze
}

// NetworkUpdate trägt, was zur Prüfung einer Änderung gehört.
type NetworkUpdate struct {
	// ClientAddr ist die Adresse des Admins, der die Änderung auslöst.
	ClientAddr netip.Addr
	// Confirm bestätigt, dass verbundene Agenten außerhalb der neuen Liste
	// getrennt werden.
	Confirm bool
}

// NetworkUpdateResult ist der neue Stand samt den getrennten Agenten.
type NetworkUpdateResult struct {
	NetworkView
	Disconnected []domain.AgentPeer `json:"disconnected"`
	// ConfigSyncError: Die Liste ist gespeichert, die config.json ließ sich aber
	// nicht schreiben. Nicht fatal.
	ConfigSyncError string `json:"config_sync_error,omitempty"`
}

// NetworkAccessService verwaltet die erlaubten Netzbereiche je Endpunkt (UI,
// Agent, MCP). Die Listen liegen je Endpunkt in einem Holder, den die
// Middleware bei jeder Anfrage liest - eine Änderung wirkt sofort. Quelle ist
// die Datenbank; die config.json spiegelt sie (siehe WithConfigSync).
type NetworkAccessService struct {
	settings   *repositories.SettingsRepository
	audit      *AuditService
	holders    map[netfilter.Endpoint]*netfilter.Holder
	peers      AgentPeers
	configSync func() error // schreibt die config.json nach einer Änderung; optional

	mu sync.Mutex // serialisiert Änderungen
}

// NewNetworkAccessService lädt die gespeicherten Listen.
func NewNetworkAccessService(settings *repositories.SettingsRepository, audit *AuditService) (*NetworkAccessService, error) {
	s := &NetworkAccessService{
		settings: settings,
		audit:    audit,
		holders:  map[netfilter.Endpoint]*netfilter.Holder{},
	}
	for _, ep := range netfilter.Endpoints {
		s.holders[ep] = &netfilter.Holder{}
		if err := s.load(ep); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// WithConfigSync verdrahtet das Schreiben der Listen in die config.json nach
// jeder Änderung.
func (s *NetworkAccessService) WithConfigSync(fn func() error) *NetworkAccessService {
	s.configSync = fn
	return s
}

// syncConfig ruft das Schreiben auf; ein Fehler ist nicht fatal und kommt als
// Text zurück.
func (s *NetworkAccessService) syncConfig() string {
	if s.configSync == nil {
		return ""
	}
	if err := s.configSync(); err != nil {
		slog.Warn("config.json konnte nicht mit den Netzbereichen abgeglichen werden", "error", err)
		return err.Error()
	}
	return ""
}

// WithAgentPeers verdrahtet den Hub, damit eine engere Agent-Liste die
// betroffenen Verbindungen kennt und trennen kann.
func (s *NetworkAccessService) WithAgentPeers(peers AgentPeers) *NetworkAccessService {
	s.peers = peers
	return s
}

// Holder liefert die Liste des Endpunkts für die Middleware.
func (s *NetworkAccessService) Holder(ep netfilter.Endpoint) *netfilter.Holder {
	return s.holders[ep]
}

// Allows meldet, ob addr für den Endpunkt zugelassen ist. Der Listener auf
// Port 80 fragt damit, wer auf die Weboberfläche weitergeleitet wird.
func (s *NetworkAccessService) Allows(ep netfilter.Endpoint, addr netip.Addr) bool {
	return s.holders[ep].Allows(addr)
}

// View liefert den Stand aller Endpunkte.
func (s *NetworkAccessService) View() ([]NetworkView, error) {
	st, err := s.settings.Get()
	if err != nil {
		return nil, err
	}
	views := make([]NetworkView, 0, len(netfilter.Endpoints))
	for _, ep := range netfilter.Endpoints {
		views = append(views, s.view(ep, st))
	}
	return views, nil
}

// Update setzt die Liste eines Endpunkts. Die Prüfungen laufen vor dem
// Speichern: gültig, kein Aussperren des Admins (Weboberfläche),
// keine unbestätigte Trennung von Agenten (Agent).
func (s *NetworkAccessService) Update(ep netfilter.Endpoint, entries []string, req NetworkUpdate, actor string) (NetworkUpdateResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	normalized, err := netfilter.Normalize(entries)
	if err != nil {
		return NetworkUpdateResult{}, &NetworkInvalidError{Detail: err.Error()}
	}
	list, err := netfilter.Parse(normalized)
	if err != nil {
		return NetworkUpdateResult{}, &NetworkInvalidError{Detail: err.Error()}
	}
	if ep == netfilter.EndpointUI && !list.Allows(req.ClientAddr) {
		return NetworkUpdateResult{}, ErrNetworkLockout
	}
	if outside := s.agentsOutside(ep, list); len(outside) > 0 && !req.Confirm {
		return NetworkUpdateResult{}, &AgentsOutsideError{Agents: outside}
	}
	before, err := s.store(ep, normalized)
	if err != nil {
		return NetworkUpdateResult{}, err
	}
	s.holders[ep].Store(list)
	// Erst nach dem Austausch trennen: Wer sich zwischendurch verbunden hat,
	// ist schon an der neuen Liste gescheitert oder steht hier mit auf der Liste.
	disconnected := s.agentsOutside(ep, list)
	for _, peer := range disconnected {
		s.peers.Disconnect(peer.AgentID)
	}
	s.audit.Log(actor, "settings.network.update", "settings", 1,
		fmt.Sprintf("%s: %s -> %s, %d Agenten getrennt", ep, describeList(before), describeList(normalized), len(disconnected)))
	st, err := s.settings.Get()
	if err != nil {
		return NetworkUpdateResult{}, err
	}
	return NetworkUpdateResult{NetworkView: s.view(ep, st), Disconnected: disconnected, ConfigSyncError: s.syncConfig()}, nil
}

// Reset leert die gespeicherte Liste eines Endpunkts: alle Netze sind wieder
// erlaubt. Der Rettungsweg, wenn jemand sich ausgesperrt hat (`lcm network
// reset`); die config.json bekommt die leere Liste mit.
func (s *NetworkAccessService) Reset(ep netfilter.Endpoint, actor string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	before, err := s.store(ep, nil)
	if err != nil {
		return err
	}
	s.holders[ep].Store(netfilter.Allowlist{})
	s.audit.Log(actor, "settings.network.reset", "settings", 1,
		fmt.Sprintf("%s: %s -> alle Netze", ep, describeList(before)))
	s.syncConfig()
	return nil
}

// agentsOutside liefert die verbundenen Agenten, die list ausschließt. Nur der
// Agent-Endpunkt hat welche.
func (s *NetworkAccessService) agentsOutside(ep netfilter.Endpoint, list netfilter.Allowlist) []domain.AgentPeer {
	if ep != netfilter.EndpointAgent || list.IsEmpty() || s.peers == nil {
		return nil
	}
	var outside []domain.AgentPeer
	for _, peer := range s.peers.ConnectedPeers() {
		if !list.Allows(peer.Addr) {
			outside = append(outside, peer)
		}
	}
	return outside
}

// load setzt die Liste des Endpunkts aus den gespeicherten Einstellungen.
func (s *NetworkAccessService) load(ep netfilter.Endpoint) error {
	st, err := s.settings.Get()
	if err != nil {
		return fmt.Errorf("netzbereiche lesen: %w", err)
	}
	list, err := netfilter.Parse(splitNets(storedNets(st, ep)))
	if err != nil {
		return fmt.Errorf("netzbereiche für %s: %w", ep.Label(), err)
	}
	s.holders[ep].Store(list)
	return nil
}

// store speichert die Liste des Endpunkts und liefert die bisherige.
func (s *NetworkAccessService) store(ep netfilter.Endpoint, entries []string) ([]string, error) {
	st, err := s.settings.Get()
	if err != nil {
		return nil, err
	}
	before := splitNets(storedNets(st, ep))
	if err := s.settings.UpdateFields(map[string]any{NetworkColumn(ep): strings.Join(entries, ",")}); err != nil {
		return nil, err
	}
	return before, nil
}

func (s *NetworkAccessService) view(ep netfilter.Endpoint, st *domain.GlobalSettings) NetworkView {
	return NetworkView{Endpoint: string(ep), Allowed: splitNets(storedNets(st, ep))}
}

// NetworkColumn ist die Spalte der Einstellungen, in der die Liste des
// Endpunkts steht.
func NetworkColumn(ep netfilter.Endpoint) string {
	switch ep {
	case netfilter.EndpointUI:
		return "allowed_nets_ui"
	case netfilter.EndpointAgent:
		return "allowed_nets_agent"
	}
	return "allowed_nets_mcp"
}

// StoredNets liefert die gespeicherte Liste eines Endpunkts; leer heißt: alle Netze.
func StoredNets(st *domain.GlobalSettings, ep netfilter.Endpoint) []string {
	return splitNets(storedNets(st, ep))
}

func storedNets(st *domain.GlobalSettings, ep netfilter.Endpoint) string {
	switch ep {
	case netfilter.EndpointUI:
		return st.AllowedNetsUI
	case netfilter.EndpointAgent:
		return st.AllowedNetsAgent
	}
	return st.AllowedNetsMCP
}

// splitNets zerlegt die gespeicherte kommagetrennte Liste; leer ergibt eine
// leere Liste, nie nil (so steht [] in der JSON-Antwort).
func splitNets(stored string) []string {
	return cleanEntries(strings.Split(stored, ","))
}

func cleanEntries(entries []string) []string {
	out := []string{}
	for _, e := range entries {
		if e = strings.TrimSpace(e); e != "" {
			out = append(out, e)
		}
	}
	return out
}

func describeList(entries []string) string {
	if len(entries) == 0 {
		return "alle Netze"
	}
	return strings.Join(entries, ", ")
}
