package services

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// Die Serverliste filtert, sortiert und blättert im Backend. Name, Host, OS und
// Kernel liegen verschlüsselt in der Datenbank und lassen sich nicht per SQL
// durchsuchen; sie werden in Go über den (nach Gruppe und Kennung schon in SQL
// eingegrenzten) Sichtbereich gefiltert. Die Ampel kommt aus dem Statuscache
// (siehe server_status.go) und wird nur für das berechnet, was Filter oder
// Sortierung braucht - sonst allein für die Seite.

// Grenzen der Seitengröße.
const (
	ServerListDefaultLimit = 100
	ServerListMaxLimit     = 1000
)

// ServerSortKeys sind die erlaubten Werte von ServerListQuery.Sort. Ein
// vorangestelltes "-" kehrt die Reihenfolge um. „status" sortiert den
// schlechtesten Zustand zuerst (rot, gelb, grün, sehr gut), „-status" den
// besten.
var ServerSortKeys = []string{
	"name", "-name", "status", "-status", "last_seen", "-last_seen",
	"os", "-os", "kernel", "-kernel", "updates", "-updates",
}

// ServerListQuery sind Filter, Sortierung und Seite der Serverliste.
type ServerListQuery struct {
	Q         string   // Teilstring in Name oder Host, ohne Groß-/Kleinschreibung
	Name      string   // genauer Name (ohne Groß-/Kleinschreibung)
	OS        string   // Teilstring in OS-Name, -Version oder -Kennung
	Kernel    string   // Versionspräfix je Stelle: „6" trifft 6.x, nicht 16.x (kernelMatches)
	Statuses  []string // Ampelstufen (domain.ServerStatus*)
	Incapable bool     // nur Server, die LCM nicht mehr pflegen kann
	GroupID   uint
	IDs       []uint
	Sort      string // einer aus ServerSortKeys, leer = "name"
	Limit     int    // 0 = ServerListDefaultLimit, höchstens ServerListMaxLimit
	Offset    int
}

// ServerListItem ist ein Eintrag der Serverliste: der Server mit allen Feldern
// wie bei GET /servers/:id (samt Gruppen) plus Ampel und Zahl der Updates.
type ServerListItem struct {
	domain.Server
	Status    string                 `json:"status"`
	Insights  []domain.StatusInsight `json:"insights"`
	OSSupport domain.OSSupportInfo   `json:"os_support"`
	Incapable bool                   `json:"incapable"`
	// Updates ist die Zahl der überfälligen Paket-Updates.
	Updates int `json:"updates"`
	// IsLcmHost meldet den Server, auf dem LCM selbst läuft (domain.Server.IsLcmHost).
	// Die Oberfläche zeigt dafür die LCM-Host-Karte, ohne die Regel nachzubauen.
	IsLcmHost bool `json:"is_lcm_host"`
}

// ServerPage ist eine Seite der Serverliste; Total zählt alle Treffer.
// OSNames sind die Betriebssysteme, die im Sichtbereich vorkommen (vor den
// Textfiltern) - die Auswahl des OS-Filters.
type ServerPage struct {
	Items   []ServerListItem `json:"items"`
	Total   int              `json:"total"`
	OSNames []string         `json:"os_names"`
}

// listFacts sammelt die je Server geladenen Zusatzdaten einer Listenabfrage.
type listFacts struct {
	statuses map[uint]ServerStatus
	updates  map[uint]int
}

// ListPage liefert eine gefilterte, sortierte Seite der sichtbaren Server.
func (s *ServerService) ListPage(scope repositories.AccessScope, q ServerListQuery) (*ServerPage, error) {
	loaded, err := s.servers.FindSelected(scope, q.GroupID, q.IDs)
	if err != nil {
		return nil, err
	}
	// Mit Zeigern arbeiten: Ein Server ist ein großer Datensatz, und das
	// Sortieren verschöbe sonst bei jedem Vergleich Kopien davon.
	servers := make([]*domain.Server, len(loaded))
	for i := range loaded {
		servers[i] = &loaded[i]
	}
	osNames := distinctOSNames(servers)
	servers = filterServerTexts(servers, q)
	facts := listFacts{statuses: map[uint]ServerStatus{}, updates: map[uint]int{}}

	sortKey := strings.TrimPrefix(q.Sort, "-")
	if len(q.Statuses) > 0 || q.Incapable || sortKey == "status" {
		if err := s.loadStatuses(&facts, servers); err != nil {
			return nil, err
		}
		servers = filterServerStatuses(servers, q, facts.statuses)
	}
	if sortKey == "updates" {
		if err := s.loadUpdates(&facts, servers); err != nil {
			return nil, err
		}
	}
	sortServers(servers, q.Sort, &facts)

	page := pageOf(servers, q.Limit, q.Offset)
	if err := s.loadStatuses(&facts, page); err != nil {
		return nil, err
	}
	if err := s.loadUpdates(&facts, page); err != nil {
		return nil, err
	}
	return &ServerPage{Items: s.listItems(page, &facts), Total: len(servers), OSNames: osNames}, nil
}

// distinctOSNames sind die vorkommenden OS-Namen, sortiert und ohne leere.
func distinctOSNames(servers []*domain.Server) []string {
	names := []string{}
	for _, srv := range servers {
		if name := strings.TrimSpace(srv.OSName); name != "" && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	slices.SortFunc(names, func(a, b string) int { return cmp.Compare(strings.ToLower(a), strings.ToLower(b)) })
	return names
}

func (s *ServerService) listItems(servers []*domain.Server, facts *listFacts) []ServerListItem {
	items := make([]ServerListItem, len(servers))
	for i := range servers {
		s.fillAgentState(servers[i])
		st := facts.statuses[servers[i].ID]
		items[i] = ServerListItem{
			Server: *servers[i], Status: st.Status, Insights: st.Insights,
			OSSupport: st.OSSupport, Incapable: st.Incapable, Updates: facts.updates[servers[i].ID],
			IsLcmHost: servers[i].IsLcmHost(),
		}
	}
	return items
}

// loadStatuses ergänzt die Ampeln der Server, die noch fehlen - in einem
// Durchgang, der den Cache nutzt. Ein Server, dessen Bewertung scheitert, gilt
// wie im Dashboard als rot.
func (s *ServerService) loadStatuses(facts *listFacts, servers []*domain.Server) error {
	var missing []domain.Server
	for i := range servers {
		if _, ok := facts.statuses[servers[i].ID]; !ok {
			missing = append(missing, *servers[i])
		}
	}
	if len(missing) == 0 {
		return nil
	}
	computed, err := s.statusOf(missing)
	if err != nil {
		return err
	}
	for _, st := range computed {
		facts.statuses[st.ID] = st
	}
	for i := range missing {
		if _, ok := facts.statuses[missing[i].ID]; !ok {
			facts.statuses[missing[i].ID] = ServerStatus{ID: missing[i].ID, Status: domain.ServerStatusRed}
		}
	}
	return nil
}

// loadUpdates ergänzt die Update-Zahlen der Server, die noch fehlen.
func (s *ServerService) loadUpdates(facts *listFacts, servers []*domain.Server) error {
	ids := make([]uint, 0, len(servers))
	for i := range servers {
		if _, ok := facts.updates[servers[i].ID]; !ok {
			ids = append(ids, servers[i].ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	counts, err := s.servers.CountOutdatedPackagesFor(ids)
	if err != nil {
		return err
	}
	for _, id := range ids {
		facts.updates[id] = counts[id] // 0 für Server ohne überfällige Pakete
	}
	return nil
}

// filterServerTexts wendet die Textfilter an (Name, Host, OS, Kernel).
func filterServerTexts(servers []*domain.Server, q ServerListQuery) []*domain.Server {
	name := strings.ToLower(strings.TrimSpace(q.Name))
	text, osText, kernel := strings.ToLower(q.Q), strings.ToLower(q.OS), strings.ToLower(strings.TrimSpace(q.Kernel))
	return slices.DeleteFunc(servers, func(srv *domain.Server) bool {
		switch {
		case name != "" && strings.ToLower(strings.TrimSpace(srv.Name)) != name:
			return true
		case text != "" && !containsFold(srv.Name, text) && !containsFold(srv.Host, text):
			return true
		case osText != "" && !containsFold(srv.OSName+" "+srv.OSVersion+" "+srv.OSID, osText):
			return true
		case kernel != "" && !kernelMatches(srv.KernelVersion, kernel):
			return true
		}
		return false
	})
}

// kernelMatches vergleicht die Kernel-Version Stelle für Stelle mit dem
// Präfix aus der Suche: „6" trifft 6.1.0 und 6.5.3, aber nicht 16.1 oder
// 5.10.6; „6.5" nur 6.5.x. Je Stelle zählt die führende Zahl (Debian hängt
// „-13-amd64" an). Endet die Suche mit einer Stelle, die keine reine Zahl ist
// („6.1.0-13"), gilt für diese letzte Stelle ein Textpräfix.
func kernelMatches(version, query string) bool {
	want := strings.Split(strings.Trim(query, "."), ".")
	have := strings.Split(strings.ToLower(version), ".")
	if len(want) > len(have) {
		return false
	}
	for i, part := range want {
		last := i == len(want)-1
		if last && !isDigits(part) {
			return strings.HasPrefix(have[i], part)
		}
		if leadingNumber(have[i]) != part {
			return false
		}
	}
	return true
}

func isDigits(s string) bool {
	return s != "" && strings.Trim(s, "0123456789") == ""
}

// leadingNumber ist der führende Ziffernteil („0-13-amd64" → „0").
func leadingNumber(s string) string {
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	return s[:end]
}

// containsFold prüft, ob s den kleingeschriebenen Teilstring sub enthält.
func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), sub)
}

// filterServerStatuses wendet Ampel- und Handlungsunfähigkeits-Filter an.
func filterServerStatuses(servers []*domain.Server, q ServerListQuery, statuses map[uint]ServerStatus) []*domain.Server {
	return slices.DeleteFunc(servers, func(srv *domain.Server) bool {
		st := statuses[srv.ID]
		if len(q.Statuses) > 0 && !slices.Contains(q.Statuses, st.Status) {
			return true
		}
		return q.Incapable && !st.Incapable
	})
}

// statusRank ordnet die Ampelstufen vom schlechtesten zum besten Zustand.
func statusRank(status string) int {
	switch status {
	case domain.ServerStatusRed:
		return 0
	case domain.ServerStatusYellow:
		return 1
	case domain.ServerStatusGreen:
		return 2
	default:
		return 3 // excellent
	}
}

// lastSeen ist der letzte Kontakt zum Server; ohne Angabe die Nullzeit, also
// „am längsten her".
func lastSeen(srv *domain.Server) time.Time {
	switch {
	case srv.LastSeenAt != nil:
		return *srv.LastSeenAt
	case srv.AgentLastSeenAt != nil:
		return *srv.AgentLastSeenAt
	}
	return time.Time{}
}

// sortServers sortiert nach dem Schlüssel (Vorgabe "name"); bei Gleichstand
// entscheiden Name und Kennung, damit das Blättern stabil bleibt.
func sortServers(servers []*domain.Server, sortBy string, facts *listFacts) {
	key, desc := strings.TrimPrefix(sortBy, "-"), strings.HasPrefix(sortBy, "-")
	compare := func(a, b *domain.Server) int {
		switch key {
		case "status":
			return cmp.Compare(statusRank(facts.statuses[a.ID].Status), statusRank(facts.statuses[b.ID].Status))
		case "last_seen":
			return lastSeen(a).Compare(lastSeen(b))
		case "os":
			return cmp.Compare(strings.ToLower(a.OSName+" "+a.OSVersion), strings.ToLower(b.OSName+" "+b.OSVersion))
		case "kernel":
			return cmp.Compare(strings.ToLower(a.KernelVersion), strings.ToLower(b.KernelVersion))
		case "updates":
			return cmp.Compare(facts.updates[a.ID], facts.updates[b.ID])
		}
		return compareByName(a, b)
	}
	slices.SortStableFunc(servers, func(a, b *domain.Server) int {
		c := compare(a, b)
		if desc {
			c = -c
		}
		if c != 0 {
			return c
		}
		return compareByName(a, b)
	})
}

// compareByName ordnet nach Name (ohne Groß-/Kleinschreibung), dann Kennung.
func compareByName(a, b *domain.Server) int {
	if c := cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); c != 0 {
		return c
	}
	return cmp.Compare(a.ID, b.ID)
}

// pageOf schneidet die Seite aus der sortierten Liste. limit 0 = Vorgabe.
func pageOf(servers []*domain.Server, limit, offset int) []*domain.Server {
	if limit <= 0 {
		limit = ServerListDefaultLimit
	}
	limit = min(limit, ServerListMaxLimit)
	if offset < 0 || offset >= len(servers) {
		return nil
	}
	return servers[offset:min(offset+limit, len(servers))]
}
