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

// Status bewertet einen Server frisch und legt das Ergebnis im Cache ab.
func (s *ServerService) Status(scope repositories.AccessScope, id uint) (string, []domain.StatusInsight, domain.OSSupportInfo, error) {
	server, err := s.servers.FindByID(scope, id)
	if err != nil {
		return "", nil, domain.OSSupportInfo{}, err
	}
	st, err := s.computeStatus(server)
	if err != nil {
		return "", nil, domain.OSSupportInfo{}, err
	}
	s.statuses.put(st)
	return st.Status, st.Insights, st.OSSupport, nil
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
	work := make(chan int)
	var wg sync.WaitGroup
	for range statusWorkers {
		wg.Go(func() {
			for i := range work {
				results[i], found[i] = s.cachedStatus(&servers[i])
			}
		})
	}
	for i := range servers {
		work <- i
	}
	close(work)
	wg.Wait()

	out := results[:0]
	for i, ok := range found {
		if ok {
			out = append(out, results[i])
		}
	}
	return out, nil
}

// cachedStatus liefert die Ampel aus dem Cache oder berechnet sie.
func (s *ServerService) cachedStatus(server *domain.Server) (ServerStatus, bool) {
	if st, ok := s.statuses.get(server.ID); ok {
		return st, true
	}
	st, err := s.computeStatus(server)
	if err != nil {
		return ServerStatus{}, false
	}
	s.statuses.put(st)
	return st, true
}

// computeStatus bewertet einen geladenen Server: Ampel, Befunde und
// OS-Support (aktuelle LTS/EOL) zum aktuellen Zeitpunkt.
func (s *ServerService) computeStatus(server *domain.Server) (ServerStatus, error) {
	id := server.ID
	outdated, err := s.servers.CountOutdatedPackages(id)
	if err != nil {
		return ServerStatus{}, err
	}
	last, err := s.jobs.jobs.LastFinishedForServer(id)
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
	if last != nil {
		in.LastJobFailed = last.Status == domain.JobStatusFailed
		in.LastJobName = last.Name
	}
	status, insights := server.TrafficLight(in)
	// Die Docker-Qualifizierung hängt BEWUSST hinter der Farbentscheidung:
	// sie beschreibt die Erreichbarkeit genauer, ist aber kein Mangel, der
	// den Server gelb färben dürfte (siehe dockerFirewallInsight).
	if qual := dockerFirewallInsight(dockerPortExposures(s.servers, server), server.FirewallActive); qual != nil {
		insights = append(insights, *qual)
	}
	osSupport := domain.OSSupportStatus(server.OSID, server.OSVersionID, server.OSName, in.Now)
	return ServerStatus{ID: id, Status: status, Insights: insights, OSSupport: osSupport}, nil
}
