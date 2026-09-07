package services

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/advisories"
	"LCM/internal/infrastructure/trivy"
	"LCM/internal/storage/repositories"
)

// AdvisoryService ist die Frühwarnung: Er fragt Online-Quellen direkt nach
// Befunden zum installierten Paketbestand - die schnelle Spur neben dem
// täglichen Trivy-Scan.
//
// Der Zuschnitt folgt dem, was die Quelle billig macht: Nicht je Server
// abfragen, sondern den Paketbestand ALLER Server zu einer Menge von purls
// zusammenlegen. Fünfzig fast gleiche Debian-Server ergeben so ein paar
// tausend verschiedene purls statt Zehntausender Einzelabfragen - und der
// lokale Cache drückt das im eingeschwungenen Betrieb weiter herunter.
type AdvisoryService struct {
	servers  *repositories.ServerRepository
	findings *repositories.AdvisoryRepository
	cache    *repositories.AdvisoryCacheRepository
	settings *repositories.SettingsRepository
	source   advisories.Source
	local    *advisories.LocalOSV
	exploits advisories.ExploitSource
	// scanStats liefert den Zustand des Trivy-Ergebnis-Zwischenspeichers.
	// Als Closure, weil der Dienst den Scanner sonst nicht kennt - dasselbe
	// Muster wie bei den anderen Quer-Verdrahtungen.
	scanStats func() trivy.CacheStats
	now       func() time.Time
}

// WithScanCacheStats verdrahtet den Zustand des Scan-Zwischenspeichers.
func (s *AdvisoryService) WithScanCacheStats(fn func() trivy.CacheStats) *AdvisoryService {
	s.scanStats = fn
	return s
}

// WithLocalSource verdrahtet die lokale OSV-Kopie. Optional - ohne sie
// bleibt die Einstellung „lokale Kopie" wirkungslos und die Frühwarnung
// fragt weiter online ab.
func (s *AdvisoryService) WithLocalSource(local *advisories.LocalOSV) *AdvisoryService {
	s.local = local
	return s
}

// WithExploitSource verdrahtet die Ausnutzungs-Anreicherung (EUVD). Optional
// - ohne sie entfällt der tägliche Lauf ersatzlos, alles andere bleibt.
func (s *AdvisoryService) WithExploitSource(src advisories.ExploitSource) *AdvisoryService {
	s.exploits = src
	return s
}

func NewAdvisoryService(
	servers *repositories.ServerRepository,
	findings *repositories.AdvisoryRepository,
	cache *repositories.AdvisoryCacheRepository,
	settings *repositories.SettingsRepository,
	source advisories.Source,
) *AdvisoryService {
	return &AdvisoryService{
		servers: servers, findings: findings, cache: cache,
		settings: settings, source: source, now: time.Now,
	}
}

// cacheEntryTTL ist die Aufbewahrungsfrist verwaister Cache-Einträge. Nach
// einem Distributions-Upgrade veralten alle purls einer Maschine auf einen
// Schlag; ohne Aufräumen bliebe der Bestand für immer liegen.
const cacheEntryTTL = 30 * 24 * time.Hour

// Enabled meldet, ob die Frühwarnung eingeschaltet ist. Standard ist AUS:
// Die Online-Abfrage schickt den (entpersonalisierten) Paketbestand an einen
// fremden Dienst - das entscheidet der Betreiber, nicht die Voreinstellung.
func (s *AdvisoryService) Enabled() bool {
	if s == nil {
		return false
	}
	st, err := s.settings.Get()
	if err != nil {
		return false
	}
	if !st.AdvisoryPollingEnabled {
		return false
	}
	src := s.sourceFor(st)
	return src != nil && src.Available()
}

// UsesLocalCopy meldet, ob im Betrieb die lokale Kopie befragt wird.
func (s *AdvisoryService) UsesLocalCopy() bool {
	st, err := s.settings.Get()
	return err == nil && st.AdvisoryLocalCopy && s.local != nil
}

// MirroredAt liefert den Stand der lokalen Kopie (Nullzeit = keine).
func (s *AdvisoryService) MirroredAt() time.Time {
	if s.local == nil {
		return time.Time{}
	}
	return s.local.MirroredAt()
}

// sourceFor wählt die Quelle: die lokale Kopie, wenn sie eingestellt UND
// verdrahtet ist, sonst die Online-Abfrage.
//
// Die Prüfung auf Available() weiter oben ist dabei der entscheidende Teil:
// Eine lokale Kopie, die noch nie gespiegelt wurde, meldet für jedes Paket
// „nichts gefunden" - ein sauberes Ergebnis für etwas, das nie geprüft
// wurde. Sie gilt deshalb erst als verfügbar, wenn Daten da sind, und die
// Frühwarnung gilt bis dahin als ausgeschaltet.
func (s *AdvisoryService) sourceFor(st *domain.GlobalSettings) advisories.Source {
	if st.AdvisoryLocalCopy && s.local != nil {
		return s.local
	}
	return s.source
}

// activeSource liefert die aktuell gültige Quelle.
func (s *AdvisoryService) activeSource() advisories.Source {
	st, err := s.settings.Get()
	if err != nil {
		return s.source
	}
	return s.sourceFor(st)
}

// MirrorableEcosystems liefert die Distributionen, die gespiegelt werden
// könnten - also die, die im Bestand tatsächlich vorkommen.
//
// Die Oberfläche braucht die Zahl, um den häufigsten Fehlschlag ohne
// Fehlermeldung zu erklären: Ohne einen einzigen Server mit erfasstem
// Paketbestand gibt es nichts zu spiegeln, und der Knopf tut scheinbar
// nichts.
func (s *AdvisoryService) MirrorableEcosystems() []string {
	targets, err := s.collectTargets()
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, t := range targets {
		for purl := range t.purlToPkg {
			eco := advisories.EcosystemForPurl(purl)
			if eco == "" || seen[eco] {
				continue
			}
			seen[eco] = true
			out = append(out, eco)
		}
	}
	sort.Strings(out)
	return out
}

// RefreshLocalCopy spiegelt die OSV-Datenbank für die Distributionen, die im
// Bestand tatsächlich vorkommen. Alles zu spiegeln wäre ein Vielfaches an
// Umfang für Daten, die nie abgefragt werden.
func (s *AdvisoryService) RefreshLocalCopy(ctx context.Context) (string, error) {
	if s.local == nil {
		return "Keine lokale Kopie verdrahtet.", nil
	}
	return s.local.Refresh(ctx, s.MirrorableEcosystems())
}

// cacheTTL liefert die eingestellte Cache-Gültigkeit in Minuten (0 = aus).
func (s *AdvisoryService) cacheTTL() int {
	st, err := s.settings.Get()
	if err != nil {
		return 0
	}
	return st.AdvisoryCacheTTL()
}

// target bündelt einen Server mit seinem Paketbestand.
type advisoryTarget struct {
	server *domain.Server
	// purlToPkg bildet den purl auf das Paket zurück - die Quelle antwortet
	// je purl, die Befunde brauchen aber Paketname und installierte Version.
	purlToPkg map[string]domain.Package
}

// Poll führt einen Durchgang aus: Paketbestand einsammeln, offene purls
// abfragen, Befunde je Server abgleichen. Rückgabe ist eine Zusammenfassung
// für Protokoll und Job-Ausgabe.
func (s *AdvisoryService) Poll(ctx context.Context) (string, error) {
	if !s.Enabled() {
		return "Frühwarnung ist ausgeschaltet.", nil
	}
	targets, err := s.collectTargets()
	if err != nil {
		return "", err
	}
	if len(targets) == 0 {
		return "Keine Server mit Paketbestand - nichts abzufragen.", nil
	}

	src := s.activeSource()
	purls := uniquePurls(targets)
	now := s.now()

	// Der Zwischenspeicher wird nur benutzt, wenn er auch etwas bringt.
	//
	// Er ist kein Geschwindigkeits-, sondern ein Vertraulichkeits-Werkzeug:
	// Er begrenzt, wie viel vom eigenen Bestand einen fremden Dienst
	// erreicht. Antwortet die Quelle aus der lokalen Kopie (siehe
	// Source.Local), gibt es nichts zu begrenzen - dann bliebe nur sein
	// Preis: das Neuschreiben sämtlicher Einträge bei JEDEM Durchgang.
	// Dasselbe gilt, wenn der Betreiber ihn per TTL 0 abgeschaltet hat.
	ttl := s.cacheTTL()
	cacheActive := ttl > 0 && !src.Local()
	if !cacheActive {
		ttl = 0
	}

	cached, err := s.cache.FreshEntries(purls, now, ttl)
	if err != nil {
		return "", err
	}
	missing := make([]string, 0, len(purls))
	for _, p := range purls {
		if _, ok := cached[p]; !ok {
			missing = append(missing, p)
		}
	}

	// Nur die offenen purls gehen nach außen. Der Cache spart hier nicht in
	// erster Linie Anfragen (querybatch bündelt ohnehin), sondern begrenzt,
	// wie viel vom eigenen Bestand überhaupt einen fremden Dienst erreicht.
	byPurl := map[string][]string{} // purl → Advisory-Kennungen
	for p, e := range cached {
		byPurl[p] = splitIDs(e.AdvisoryIDs)
	}
	if len(missing) > 0 {
		fresh, err := src.Query(ctx, missing)
		if err != nil {
			return "", err
		}
		entries := make([]domain.AdvisoryCacheEntry, 0, len(missing))
		for _, p := range missing {
			ids := make([]string, 0, len(fresh[p]))
			for _, a := range fresh[p] {
				ids = append(ids, a.ID)
			}
			sort.Strings(ids)
			byPurl[p] = ids
			entries = append(entries, domain.AdvisoryCacheEntry{
				Purl: p, Source: src.Name(),
				AdvisoryIDs: strings.Join(ids, ","), CheckedAt: now,
			})
		}
		// Nur schreiben, was auch wieder gelesen wird - sonst kostet ein
		// abgeschalteter Zwischenspeicher mehr als ein eingeschalteter.
		if cacheActive {
			if err := s.cache.PutEntries(entries); err != nil {
				return "", err
			}
		}
	}

	details, err := s.resolveDetails(ctx, byPurl)
	if err != nil {
		return "", err
	}

	var totalNew, totalReopened, totalResolved int
	for _, t := range targets {
		found := s.findingsFor(t, byPurl, details)
		res, err := s.findings.Reconcile(t.server.ID, src.Name(), found, now)
		if err != nil {
			slog.Warn("advisory reconcile failed", "server", t.server.Name, "error", err)
			continue
		}
		totalNew += len(res.New)
		totalReopened += len(res.Reopened)
		totalResolved += res.Resolved
	}

	if _, err := s.cache.PurgeEntriesOlderThan(now.Add(-cacheEntryTTL)); err != nil {
		slog.Warn("advisory cache purge failed", "error", err)
	}
	if err := s.settings.UpdateFields(map[string]any{"advisory_last_poll_at": now}); err != nil {
		slog.Warn("advisory poll: zeitstempel nicht gespeichert", "error", err)
	}
	// Die Trefferquote zählt nur mit, wenn der Zwischenspeicher überhaupt
	// befragt wurde - sonst stünde dort auf Dauer eine Null, die nichts über
	// seine Wirksamkeit aussagt, sondern nur darüber, dass er ausgeschaltet ist.
	if cacheActive {
		if err := s.cache.RecordRun(len(purls)-len(missing), len(missing), now); err != nil {
			slog.Warn("advisory poll: trefferquote nicht gespeichert", "error", err)
		}
	}
	return fmt.Sprintf("%d paket(e) geprüft (%s), %d neue(r) befund(e), %d wiedereröffnet, %d behoben",
		len(purls), cacheNote(cacheActive, len(purls)-len(missing), src.Local()),
		totalNew, totalReopened, totalResolved), nil
}

// cacheNote beschreibt im Protokolleintrag, was der Zwischenspeicher beigetragen
// hat - und wenn er nicht befragt wurde, warum nicht. „0 aus dem
// Zwischenspeicher" bei jedem Durchgang war die Meldung, an der jahrelang
// niemand erkennen konnte, ob er wirkungslos oder schlicht aus war.
func cacheNote(active bool, hits int, local bool) string {
	switch {
	case active:
		return fmt.Sprintf("%d aus dem Zwischenspeicher", hits)
	case local:
		return "lokale Kopie, ohne Zwischenspeicher"
	default:
		return "Zwischenspeicher aus"
	}
}

// collectTargets lädt die Server samt Paketbestand und baut je Server die
// purl-Zuordnung.
func (s *AdvisoryService) collectTargets() ([]advisoryTarget, error) {
	servers, err := s.servers.FindAllUnscoped()
	if err != nil {
		return nil, err
	}
	out := make([]advisoryTarget, 0, len(servers))
	for i := range servers {
		srv := &servers[i]
		// Dieselben Ausnahmen wie beim CVE-Scan: Demo-Server sind erfunden,
		// reine API-Geräte (RouterOS, DSM) haben keinen Linux-Paketbestand,
		// über den eine Aussage möglich wäre - und Server in Wartung sind
		// bewusst aus dem Betrieb genommen.
		if srv.IsDemo || srv.IsAPIDevice() || srv.InMaintenance() {
			continue
		}
		pkgs, err := s.servers.FindPackages(srv.ID)
		if err != nil {
			slog.Warn("advisory poll: packages not loadable", "server", srv.Name, "error", err)
			continue
		}
		if len(pkgs) == 0 {
			continue
		}
		target := trivy.Target{
			OSID: srv.OSID, OSVersionID: srv.OSVersionID,
			PackageManager: srv.PackageManager, Packages: pkgs,
		}
		mapping := make(map[string]domain.Package, len(pkgs))
		for _, p := range pkgs {
			if p.Name == "" {
				continue
			}
			mapping[trivy.PurlFor(target, p.Name, p.Version)] = p
		}
		out = append(out, advisoryTarget{server: srv, purlToPkg: mapping})
	}
	return out, nil
}

// uniquePurls sammelt die purls aller Server zu einer sortierten Menge -
// der eigentliche Spar-Hebel: gleiche Pakete werden einmal gefragt.
func uniquePurls(targets []advisoryTarget) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range targets {
		for p := range t.purlToPkg {
			if seen[p] {
				continue
			}
			seen[p] = true
			out = append(out, p)
		}
	}
	sort.Strings(out) // stabile Reihenfolge - Tests und Protokolle bleiben lesbar
	return out
}

// resolveDetails besorgt die Beschreibungen aller aufgetretenen Kennungen:
// erst aus dem lokalen Bestand, nur die unbekannten von der Quelle.
//
// Anders als der purl-Befund verfallen Beschreibungen nicht über eine Uhr -
// ein Advisory ändert seinen Titel nicht. Maßgeblich ist der
// Änderungsstempel der Quelle, den die Abfrage ohnehin mitliefert.
func (s *AdvisoryService) resolveDetails(ctx context.Context, byPurl map[string][]string) (map[string]domain.AdvisoryDetail, error) {
	seen := map[string]bool{}
	var ids []string
	for _, list := range byPurl {
		for _, id := range list {
			if seen[id] {
				continue
			}
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return map[string]domain.AdvisoryDetail{}, nil
	}
	known, err := s.cache.Details(ids)
	if err != nil {
		return nil, err
	}
	var unknown []string
	for _, id := range ids {
		if _, ok := known[id]; !ok {
			unknown = append(unknown, id)
		}
	}
	if len(unknown) == 0 {
		return known, nil
	}
	fetched, err := s.activeSource().Details(ctx, unknown)
	if err != nil {
		return nil, err
	}
	now := s.now()
	rows := make([]domain.AdvisoryDetail, 0, len(fetched))
	for id, d := range fetched {
		row := domain.AdvisoryDetail{
			AdvisoryID: id, Source: s.activeSource().Name(), Kind: domain.AdvisoryKindFor(id),
			Severity: d.Severity, Title: d.Title, URL: d.URL,
			FixedVersions: joinFixedVersions(d.FixedVersions),
			Aliases:       strings.Join(d.Aliases, ","),
			Modified:      d.Modified, FetchedAt: now,
		}
		known[id] = row
		rows = append(rows, row)
	}
	if err := s.cache.PutDetails(rows); err != nil {
		return nil, err
	}
	return known, nil
}

// findingsFor baut die Befunde eines Servers aus seinen purls.
func (s *AdvisoryService) findingsFor(t advisoryTarget, byPurl map[string][]string, details map[string]domain.AdvisoryDetail) []domain.AdvisoryFinding {
	var out []domain.AdvisoryFinding
	for purl, pkg := range t.purlToPkg {
		for _, id := range byPurl[purl] {
			d := details[id]
			out = append(out, domain.AdvisoryFinding{
				AdvisoryID:       id,
				Kind:             domain.AdvisoryKindFor(id),
				PackageName:      pkg.Name,
				InstalledVersion: pkg.Version,
				FixedVersion:     fixedFor(d.FixedVersions, pkg.Name),
				Severity:         d.Severity,
				Title:            d.Title,
				URL:              d.URL,
			})
		}
	}
	return out
}

// joinFixedVersions serialisiert die Fix-Versionen als "paket=version"-Paare.
func joinFixedVersions(in map[string]string) string {
	if len(in) == 0 {
		return ""
	}
	parts := make([]string, 0, len(in))
	for name, version := range in {
		parts = append(parts, name+"="+version)
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

// fixedFor liest die behebende Version eines Pakets aus der serialisierten
// Form zurück.
func fixedFor(joined, pkg string) string {
	for _, part := range strings.Split(joined, ",") {
		name, version, ok := strings.Cut(part, "=")
		if ok && name == pkg {
			return version
		}
	}
	return ""
}

// splitIDs zerlegt die kommagetrennte Kennungsliste eines Cache-Eintrags.
// Der leere Eintrag ist der Normalfall („geprüft, nichts gefunden") und darf
// keine leere Kennung erzeugen.
func splitIDs(joined string) []string {
	if strings.TrimSpace(joined) == "" {
		return nil
	}
	return strings.Split(joined, ",")
}

// EnrichExploited markiert die Befunde, für die eine aktive Ausnutzung belegt
// ist (EUVD). Diese Quelle erzeugt bewusst KEINE eigenen Befunde: Sie ist
// nicht paketgenau und könnte gar nicht sagen, ob eine Lücke den eigenen
// Bestand betrifft. Ihr Wert ist allein die Dringlichkeit - „das wird da
// draußen wirklich ausgenutzt".
//
// Der Abgleich läuft über die Aliase: Ein Befund zu einem Debian-Paket trägt
// oft die Kennung der Distribution (DSA-…), die Ausnutzungsliste dagegen
// CVE-Kennungen. Ohne Aliase liefe der Abgleich für genau die Pakete ins
// Leere, um die es hier geht.
func (s *AdvisoryService) EnrichExploited(ctx context.Context) (string, error) {
	if s.exploits == nil || !s.exploits.Available() {
		return "Keine Quelle für Ausnutzungs-Signale verdrahtet.", nil
	}
	if !s.Enabled() {
		return "Frühwarnung ist ausgeschaltet.", nil
	}
	exploited, err := s.exploits.ExploitedCVEs(ctx)
	if err != nil {
		return "", err
	}
	details, err := s.cache.AllDetails()
	if err != nil {
		return "", err
	}
	var ids []string
	for i := range details {
		if exploitedDetail(&details[i], exploited) {
			ids = append(ids, details[i].AdvisoryID)
		}
	}
	marked, err := s.findings.SetExploited(ids)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d ausgenutzte schwachstelle(n) bekannt, %d davon betreffen den eigenen bestand",
		len(exploited), marked), nil
}

// exploitedDetail meldet, ob eine Beschreibung - über ihre eigene Kennung
// oder einen ihrer Aliase - in der Ausnutzungsliste steht.
func exploitedDetail(d *domain.AdvisoryDetail, exploited map[string]bool) bool {
	if exploited[strings.ToUpper(d.AdvisoryID)] {
		return true
	}
	for _, alias := range strings.Split(d.Aliases, ",") {
		if alias = strings.ToUpper(strings.TrimSpace(alias)); alias != "" && exploited[alias] {
			return true
		}
	}
	return false
}

// CacheReport bündelt den Zustand beider Zwischenspeicher.
//
// Es sind zwei sehr verschiedene Dinge, und die Oberfläche muss das zeigen:
// Der Scan-Zwischenspeicher liegt im Arbeitsspeicher und misst, wie
// gleichförmig die Flotte ist; der Advisory-Zwischenspeicher liegt in der
// Datenbank und misst, wie viel vom eigenen Paketbestand gar nicht erst nach
// außen gehen musste.
type CacheReport struct {
	Scan     trivy.CacheStats            `json:"scan"`
	Advisory *domain.AdvisoryCacheStats  `json:"advisory"`
	Snapshot *repositories.CacheSnapshot `json:"snapshot"`
	// TTLMinutes ist die eingestellte Gültigkeit - ohne sie ist „frisch"
	// in der Momentaufnahme nicht einzuordnen.
	TTLMinutes int `json:"ttl_minutes"`
}

// CacheStats liefert den Bericht über beide Zwischenspeicher.
func (s *AdvisoryService) CacheStats() (*CacheReport, error) {
	ttl := s.cacheTTL()
	stats, snap, err := s.cache.Stats(s.now(), ttl)
	if err != nil {
		return nil, err
	}
	out := &CacheReport{Advisory: stats, Snapshot: snap, TTLMinutes: ttl}
	if s.scanStats != nil {
		out.Scan = s.scanStats()
	}
	return out, nil
}

// --- Lesende Zugriffe für API und Alarm ---------------------------------------

// Global listet die Befunde über die sichtbaren Server, seitenweise.
func (s *AdvisoryService) Global(scope repositories.AccessScope, f repositories.AdvisoryFilter) (*repositories.AdvisoryPage, error) {
	return s.findings.Global(scope, f)
}

// LastPollAt liefert den Zeitpunkt des letzten erfolgreichen Durchgangs
// (Nullzeit = noch nie gelaufen).
//
// Der Wert ist die Antwort auf die naheliegendste Frage vor der Fundliste:
// „Wann wurde zuletzt nachgesehen?" Ohne ihn ist eine leere Liste nicht von
// „noch nie geprüft" zu unterscheiden - genau die Verwechslung, gegen die
// dieses Feature gebaut ist.
func (s *AdvisoryService) LastPollAt() time.Time {
	st, err := s.settings.Get()
	if err != nil || st.AdvisoryLastPollAt == nil {
		return time.Time{}
	}
	return *st.AdvisoryLastPollAt
}

// ActiveForServer liefert die offenen Befunde eines Servers.
func (s *AdvisoryService) ActiveForServer(id uint) ([]domain.AdvisoryFinding, error) {
	return s.findings.ActiveForServer(id)
}

// Acknowledge nimmt einen Befund zur Kenntnis (kein Alarm mehr).
func (s *AdvisoryService) Acknowledge(scope repositories.AccessScope, id, actor string) error {
	return s.findings.Acknowledge(scope, id, actor, s.now())
}
