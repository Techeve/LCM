package services

import (
	"sync"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// Die Ampel eines Servers ist teuer: rund zehn Abfragen über Pakete, CVEs,
// Container, Speicher und den letzten Job. Das Dashboard braucht sie für
// JEDEN Server, und man landet dort oft. Früher stellte der Browser dafür eine
// Anfrage je Server - bei tausend Servern tausend Anfragen, jede mit eigener
// Token-Prüfung und eigenem Benutzer-Lookup.
//
// Deshalb zwei Dinge:
//
//   - StatusAll bewertet alle sichtbaren Server in EINER Anfrage.
//   - Ein Cache im Arbeitsspeicher hält die fertigen Ergebnisse. Er gilt
//     höchstens statusCacheTTL lang und wird früher geleert, wenn sich etwas
//     ändert: Endet ein Job, vergisst er diesen Server (ein serverloser Job
//     wie der CVE-Scan betrifft alle); jede ändernde Anfrage an die API leert
//     ihn ganz. Was dann noch durchrutscht - etwa eine geänderte
//     Gewichtungsliste - ist nach spätestens einer Minute aktuell.
//
// Die Detailseite rechnet weiterhin frisch (Status) und legt ihr Ergebnis im
// Cache ab.

// statusCacheTTL begrenzt, wie alt eine Ampel aus dem Cache sein darf.
const statusCacheTTL = time.Minute

// statusWorkers ist die Zahl der Server, die StatusAll gleichzeitig bewertet.
// SQLite liest im WAL-Modus parallel; mehr als eine Handvoll bringt nichts,
// weil die Abfragen um dieselbe Platte konkurrieren.
const statusWorkers = 4

// ServerStatus ist die Ampel-Bewertung eines Servers.
type ServerStatus struct {
	ID        uint                   `json:"id"`
	Status    string                 `json:"status"`
	Insights  []domain.StatusInsight `json:"insights"`
	OSSupport domain.OSSupportInfo   `json:"os_support"`
	// Incapable: LCM kann den Server nicht mehr pflegen - Paketverwaltung
	// oder Verbindung sind ausgefallen, oder der Paketstand ist kritisch
	// veraltet. Speist den Dashboard-Filter „LCM handlungsunfähig".
	Incapable bool `json:"incapable"`
}

type cachedStatus struct {
	value ServerStatus
	at    time.Time
}

// statusCache hält berechnete Ampeln je Server.
type statusCache struct {
	ttl     time.Duration
	mu      sync.Mutex
	entries map[uint]cachedStatus
}

func newStatusCache(ttl time.Duration) *statusCache {
	return &statusCache{ttl: ttl, entries: map[uint]cachedStatus{}}
}

func (c *statusCache) get(id uint) (ServerStatus, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[id]
	if !ok || time.Since(e.at) > c.ttl {
		return ServerStatus{}, false
	}
	return e.value, true
}

func (c *statusCache) put(v ServerStatus) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[v.ID] = cachedStatus{value: v, at: time.Now()}
}

// forget verwirft die Ampel eines Servers - nil verwirft alle.
func (c *statusCache) forget(id *uint) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if id == nil {
		clear(c.entries)
		return
	}
	delete(c.entries, *id)
}

// ForgetStatus verwirft die zwischengespeicherte Ampel eines Servers (nil:
// aller Server). Aufgerufen, wenn ein Job endet oder die API etwas ändert.
func (s *ServerService) ForgetStatus(serverID *uint) {
	s.statuses.forget(serverID)
}

// WithCapabilities verdrahtet den Speicher der Betriebsfähigkeit. Ohne ihn
// (schlanke Tests) gibt es keine Aktionsfehler und keinen Stillstand.
func (s *ServerService) WithCapabilities(repo *repositories.CapabilityRepository) *ServerService {
	s.capabilities = repo
	return s
}

// staleLimits sind die Schwellen für „Paketstand veraltet" aus den globalen
// Einstellungen.
type staleLimits struct {
	warn, critical time.Duration
}

// loadStaleLimits liest die Einstellungen EINMAL je Anfrage. Ohne
// Einstellungen (schlanke Tests) gelten die Vorgaben.
func (s *ServerService) loadStaleLimits() (staleLimits, error) {
	settings := &domain.GlobalSettings{}
	if s.settings != nil {
		loaded, err := s.settings.Get()
		if err != nil {
			return staleLimits{}, err
		}
		settings = loaded
	}
	return staleLimitsOf(settings), nil
}

func staleLimitsOf(settings *domain.GlobalSettings) staleLimits {
	return staleLimits{warn: settings.PackagesStaleWarnAfter(), critical: settings.PackagesStaleCriticalAfter()}
}

// statusFacts sind die für alle Server gemeinsamen Eingaben der Bewertung, die
// StatusAll einmal lädt statt je Server.
type statusFacts struct {
	capabilities map[uint][]domain.ServerCapability
	limits       staleLimits
}

// loadStatusFacts lädt die Betriebsfähigkeit der Server mit EINER Abfrage und
// die Schwellen einmal. Das Dashboard bewertet hunderte Server auf einmal;
// eine Abfrage je Server ließe die Liste an der Datenbank hängen.
func (s *ServerService) loadStatusFacts(ids []uint) (statusFacts, error) {
	limits, err := s.loadStaleLimits()
	if err != nil {
		return statusFacts{}, err
	}
	facts := statusFacts{limits: limits}
	if s.capabilities == nil {
		return facts, nil
	}
	facts.capabilities, err = s.capabilities.FindForServers(ids)
	return facts, err
}

// Status bewertet einen Server frisch und legt das Ergebnis im Cache ab.
func (s *ServerService) Status(scope repositories.AccessScope, id uint) (string, []domain.StatusInsight, domain.OSSupportInfo, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return "", nil, domain.OSSupportInfo{}, err
	}
	facts, err := s.loadStatusFacts([]uint{id})
	if err != nil {
		return "", nil, domain.OSSupportInfo{}, err
	}
	st, err := s.computeStatus(server, facts.capabilities[id], facts.limits)
	if err != nil {
		return "", nil, domain.OSSupportInfo{}, err
	}
	s.statuses.put(st)
	return st.Status, st.Insights, st.OSSupport, nil
}

// ServerCapabilities ist die Betriebsfähigkeit eines Servers samt dem dabei
// geladenen Server: Wer die Aktionen je Kategorie braucht (CapabilityActions),
// muss ihn nicht ein zweites Mal laden.
type ServerCapabilities struct {
	Server *domain.Server
	Rows   []domain.ServerCapability // alle Kategorien in Anzeigereihenfolge
	Stale  string                    // Stufe des Paketstand-Stillstands
}

// Capabilities liefert die vier Kategorien der Betriebsfähigkeit eines Servers
// in der Anzeigereihenfolge und die Stufe des Paketstand-Stillstands. Kategorien
// ohne Zeile (noch nie ein Job dieser Art) erscheinen als in Ordnung.
func (s *ServerService) Capabilities(scope repositories.AccessScope, id uint) (*ServerCapabilities, error) {
	// Der Zugriffsprüfung wegen zuerst den Server laden (Mandantentrennung).
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	facts, err := s.loadStatusFacts([]uint{id})
	if err != nil {
		return nil, err
	}
	rows := facts.capabilities[id]
	level, _ := packagesStaleness(server, rows, facts.limits, time.Now())
	return &ServerCapabilities{Server: server, Rows: allCategories(id, rows), Stale: level}, nil
}

// allCategories füllt fehlende Kategorien als in Ordnung auf.
func allCategories(serverID uint, rows []domain.ServerCapability) []domain.ServerCapability {
	out := make([]domain.ServerCapability, 0, len(domain.CapabilityCategories))
	for _, category := range domain.CapabilityCategories {
		c := capabilityOf(rows, category)
		if c == nil {
			c = &domain.ServerCapability{ServerID: serverID, Category: category, State: domain.CapabilityOK}
		}
		out = append(out, *c)
	}
	return out
}

func capabilityOf(rows []domain.ServerCapability, category string) *domain.ServerCapability {
	for i := range rows {
		if rows[i].Category == category {
			return &rows[i]
		}
	}
	return nil
}

// StatusAll liefert die Ampeln aller für den Aufrufer sichtbaren Server - aus
// dem Cache, wo er frisch ist, sonst neu berechnet. Ein Server, dessen
// Bewertung scheitert, fehlt im Ergebnis; das Dashboard zeigt ihn dann rot.
func (s *ServerService) StatusAll(scope repositories.AccessScope) ([]ServerStatus, error) {
	servers, err := s.servers.FindAll(scope)
	if err != nil {
		return nil, err
	}
	results := make([]ServerStatus, len(servers))
	found := make([]bool, len(servers))
	missing := s.fillFromCache(servers, results, found)
	if len(missing) > 0 {
		if err := s.computeMissing(servers, missing, results, found); err != nil {
			return nil, err
		}
	}

	out := results[:0]
	for i, ok := range found {
		if ok {
			out = append(out, results[i])
		}
	}
	return out, nil
}

// fillFromCache übernimmt frische Ampeln aus dem Cache und nennt die
// Positionen der Server, die neu zu berechnen sind.
func (s *ServerService) fillFromCache(servers []domain.Server, results []ServerStatus, found []bool) []int {
	var missing []int
	for i := range servers {
		if st, ok := s.statuses.get(servers[i].ID); ok {
			results[i], found[i] = st, true
			continue
		}
		missing = append(missing, i)
	}
	return missing
}

// computeMissing berechnet die Ampeln der übrigen Server parallel. Die
// gemeinsamen Eingaben werden vorher einmal geladen - nur wenn überhaupt etwas
// zu berechnen ist.
func (s *ServerService) computeMissing(servers []domain.Server, missing []int, results []ServerStatus, found []bool) error {
	ids := make([]uint, len(missing))
	for n, i := range missing {
		ids[n] = servers[i].ID
	}
	facts, err := s.loadStatusFacts(ids)
	if err != nil {
		return err
	}
	work := make(chan int)
	var wg sync.WaitGroup
	for range statusWorkers {
		wg.Go(func() {
			for i := range work {
				results[i], found[i] = s.computeAndCache(&servers[i], facts)
			}
		})
	}
	for _, i := range missing {
		work <- i
	}
	close(work)
	wg.Wait()
	return nil
}

// computeAndCache berechnet die Ampel eines Servers und legt sie im Cache ab.
func (s *ServerService) computeAndCache(server *domain.Server, facts statusFacts) (ServerStatus, bool) {
	st, err := s.computeStatus(server, facts.capabilities[server.ID], facts.limits)
	if err != nil {
		return ServerStatus{}, false
	}
	s.statuses.put(st)
	return st, true
}

// computeStatus bewertet einen geladenen Server: Ampel, Befunde und
// OS-Support (aktuelle LTS/EOL) zum aktuellen Zeitpunkt. Betriebsfähigkeit und
// Schwellen kommen vom Aufrufer, damit die Liste sie nicht je Server lädt.
func (s *ServerService) computeStatus(server *domain.Server, caps []domain.ServerCapability, limits staleLimits) (ServerStatus, error) {
	id := server.ID
	outdated, err := s.servers.CountOutdatedPackages(id)
	if err != nil {
		return ServerStatus{}, err
	}
	// CVE-Zählung GEWICHTET: Docker-CVEs zählen nur für ausdrücklich als
	// relevant markierte Container; Hochgewichtungs-Liste und lauschende
	// Dienste heben eine Stufe an.
	facts, err := s.servers.VulnerabilityFacts(id)
	if err != nil {
		return ServerStatus{}, err
	}
	relevantRefs := dockerRelevantRefs(s.servers, server)
	weighted := weightedVulnSummary(facts, s.cveWeightList(), splitCSVList(server.ListeningPackages), relevantRefs)
	outdatedImages, err := s.servers.CountOutdatedDockerImages(id)
	if err != nil {
		return ServerStatus{}, err
	}
	// Größe des Paketbestands: 0 heißt "nie erfasst" und macht den Server
	// unbewertbar statt makellos (BUG-020).
	inventoried, err := s.servers.CountPackages(id)
	if err != nil {
		return ServerStatus{}, err
	}
	// Speicher: die erfassten Volumes, die angeordnete Überwachung einzelner
	// davon und der Zustand der Verbünde. Fehler hier dürfen die Bewertung
	// nicht kippen - dann fehlt eben dieser Teil der Befunde.
	volumes, _ := s.servers.FindDiskVolumes(id)
	monitore, _ := s.servers.FindVolumeMonitors(id)
	storage, _ := s.servers.FindStorageHealth(id)
	in := domain.TrafficLightInput{
		OutdatedPackages: int(outdated), Now: time.Now(),
		Volumes: volumes, VolumeMonitors: monitore, StorageHealth: storage,
		CriticalVulns: weighted[domain.SeverityCritical], HighVulns: weighted[domain.SeverityHigh],
		RaisedVulnPackages:      raisedVulnPackages(facts, s.cveWeightList(), splitCSVList(server.ListeningPackages), relevantRefs),
		OutdatedContainerImages: int(outdatedImages),
		TotalVulns:              countedVulns(facts, relevantRefs),
		// Ernste, aber unbehebbare Lücken: reiner Info-Hinweis (R2-056).
		UnfixableVulns: unfixableCritHigh(facts, s.cveWeightList(), splitCSVList(server.ListeningPackages), relevantRefs),
		// RouterOS hat konstruktionsbedingt keinen Paketbestand - das darf hier
		// nicht als „nicht bewertbar" (Rot) durchschlagen; die Bewertung läuft
		// dort über die Versions-Aktualität (siehe TrafficLight).
		InventoryMissing: inventoried == 0 && !server.IsRouterOS(),
		CVEScanError:     server.CVEScanError,
		DeepScanWarnings: server.DeepScanWarnings,
		// Stand der zentralen Schwachstellen-Datenbank - reiner Hinweis, er
		// faerbt die Ampel nicht (siehe TrafficLightInput.CVEDB).
		CVEDB: s.CVEDBStatus(),
	}
	in.Capabilities = caps
	in.PackagesStale, in.PackagesStaleSince = packagesStaleness(server, caps, limits, in.Now)
	status, insights := server.TrafficLight(in)
	// Die Docker-Qualifizierung hängt BEWUSST hinter der Farbentscheidung:
	// sie beschreibt die Erreichbarkeit genauer, ist aber kein Mangel, der
	// den Server gelb färben dürfte (siehe dockerFirewallInsight).
	if qual := dockerFirewallInsight(dockerPortExposures(s.servers, server), server.FirewallActive); qual != nil {
		insights = append(insights, *qual)
	}
	osSupport := domain.OSSupportStatus(server.OSID, server.OSVersionID, server.OSName, in.Now)
	return ServerStatus{
		ID: id, Status: status, Insights: insights, OSSupport: osSupport,
		Incapable: isIncapable(caps, in.PackagesStale),
	}, nil
}

// packagesStaleness stuft den Stillstand der Paketaktualisierung ein. Bezug ist
// der letzte ERFOLG, nicht der letzte Versuch - ein Server, dessen Läufe alle
// scheitern oder hängen, ist genau der Fall. Ohne bisherigen Erfolg zählt das
// Anlagedatum, sonst bliebe ein nie gelungener Server ewig unauffällig.
//
// Ausgenommen sind Geräte, deren Pakete LCM gar nicht verwaltet (RouterOS,
// DSM), Demo-Server (erfundene Daten) und nicht erreichbare Server: Deren
// Stillstand erklärt die Verbindung - und ein bewusst geduldeter Ausfall
// (Nichterreichbarkeit unkritisch) soll nicht über diesen Umweg rot werden.
func packagesStaleness(server *domain.Server, caps []domain.ServerCapability, limits staleLimits, now time.Time) (string, time.Time) {
	if server.IsAPIDevice() || server.IsDemo || !server.Reachable {
		return "", time.Time{}
	}
	ref := server.CreatedAt
	if c := capabilityOf(caps, domain.CapabilityPackages); c != nil && c.LastSuccessAt != nil {
		ref = *c.LastSuccessAt
	}
	if ref.IsZero() {
		return "", time.Time{}
	}
	switch age := now.Sub(ref); {
	case age > limits.critical:
		return domain.PackagesStaleCritical, ref
	case age > limits.warn:
		return domain.PackagesStaleWarning, ref
	}
	return "", time.Time{}
}

// isIncapable: LCM kann den Server nicht mehr pflegen. Ein fehlgeschlagener
// Regel- oder CVE-Lauf zählt nicht dazu - der Server bleibt dann pflegbar.
func isIncapable(caps []domain.ServerCapability, staleLevel string) bool {
	if staleLevel == domain.PackagesStaleCritical {
		return true
	}
	for _, category := range []string{domain.CapabilityPackages, domain.CapabilityConnection} {
		if c := capabilityOf(caps, category); c != nil && c.State == domain.CapabilityFailed {
			return true
		}
	}
	return false
}
