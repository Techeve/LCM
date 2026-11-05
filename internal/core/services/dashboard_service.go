package services

import (
	"sort"
	"strconv"
	"sync"
	"time"

	"LCM/internal/core/domain"
	"LCM/internal/core/events"
	"LCM/internal/storage/repositories"
)

// Das Dashboard holt alles, was die Startseite zeigt, mit EINEM Aufruf. Die
// Abfragen sind konstant viele - Server samt Ampeln, Jobs, Zeitpläne, Alarme -,
// unabhängig davon, wie viele Server es gibt.

// Grenzen der Listen auf dem Dashboard.
const (
	dashboardAttentionMax = 20
	dashboardJobsMax      = 15
	dashboardUpcomingMax  = 10
	dashboardAlertsMax    = 10
)

// dashboardTTL begrenzt, wie alt ein zwischengespeichertes Dashboard sein darf.
// Ereignisse entwerten es früher (siehe DashboardService.fresh).
const dashboardTTL = 5 * time.Second

// DashboardTiles zählt die Server je Ampelstufe.
type DashboardTiles struct {
	Excellent int `json:"excellent"`
	Green     int `json:"green"`
	Yellow    int `json:"yellow"`
	Red       int `json:"red"`
	// Incapable: LCM kann den Server nicht mehr pflegen (Paketverwaltung oder
	// Verbindung ausgefallen, siehe ServerStatus.Incapable). Ein Server zählt
	// hier zusätzlich zu seiner Ampelstufe.
	Incapable int `json:"incapable"`
	Total     int `json:"total"`
}

// DashboardAttention ist ein Server, der Aufmerksamkeit braucht, mit seinem
// wichtigsten Befund.
type DashboardAttention struct {
	ServerID uint                  `json:"server_id"`
	Name     string                `json:"name"`
	Level    string                `json:"level"`
	Insight  *domain.StatusInsight `json:"insight"` // nil, wenn die Bewertung keinen Befund liefert
}

// DashboardJob ist ein beendeter Job.
type DashboardJob struct {
	ID         string     `json:"id"`
	ServerID   *uint      `json:"server_id"`
	ServerName string     `json:"server_name"`
	Kind       string     `json:"kind"`
	Name       string     `json:"name"`
	Status     string     `json:"status"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	// DurationSeconds ist leer, wenn der Job nie gestartet wurde.
	DurationSeconds *int64 `json:"duration_seconds"`
}

// DashboardRule ist eine Regel eines anstehenden Zeitplans.
type DashboardRule struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// DashboardRun ist der nächste Lauf eines Zeitplans.
type DashboardRun struct {
	ScheduleID   uint            `json:"schedule_id"`
	ScheduleName string          `json:"schedule_name"`
	GroupID      uint            `json:"group_id"`
	GroupName    string          `json:"group_name"`
	NextRun      time.Time       `json:"next_run"`
	Rules        []DashboardRule `json:"rules"`
}

// Dashboard ist die Antwort von GET /api/v1/dashboard.
type Dashboard struct {
	GeneratedAt  time.Time            `json:"generated_at"`
	Tiles        DashboardTiles       `json:"tiles"`
	Attention    []DashboardAttention `json:"attention"`
	RecentJobs   []DashboardJob       `json:"recent_jobs"`
	Upcoming     []DashboardRun       `json:"upcoming"`
	RecentAlerts []domain.AlertEvent  `json:"recent_alerts"`
}

// DashboardService stellt das Dashboard zusammen und hält es kurz im Speicher.
type DashboardService struct {
	servers *ServerService
	jobs    *repositories.JobRepository
	alerts  *repositories.AlertRepository
	groups  *repositories.GroupRepository
	bus     *events.Bus
	now     func() time.Time

	mu    sync.Mutex
	cache map[string]*dashboardEntry
}

// dashboardEntry ist der Zwischenspeicher für einen Sichtbereich. Sein eigener
// Mutex sorgt dafür, dass gleichzeitige Aufrufe desselben Bereichs EINMAL
// rechnen, während andere Bereiche nicht warten.
type dashboardEntry struct {
	mu         sync.Mutex
	value      *Dashboard
	at         time.Time
	generation uint64
}

func NewDashboardService(servers *ServerService, jobs *repositories.JobRepository,
	alerts *repositories.AlertRepository, groups *repositories.GroupRepository, bus *events.Bus) *DashboardService {
	return &DashboardService{
		servers: servers, jobs: jobs, alerts: alerts, groups: groups, bus: bus,
		now: time.Now, cache: map[string]*dashboardEntry{},
	}
}

// Get liefert das Dashboard für einen Sichtbereich. withAlerts sagt, ob der
// Aufrufer den Alarm-Verlauf sehen darf (Recht alerts:manage).
func (s *DashboardService) Get(scope repositories.AccessScope, withAlerts bool) (*Dashboard, error) {
	entry := s.entryFor(cacheKey(scope, withAlerts))
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if s.fresh(entry) {
		return entry.value, nil
	}
	// Die Generation VOR dem Rechnen merken: Passiert währenddessen etwas,
	// gilt das Ergebnis als schon überholt.
	generation := s.generation()
	value, err := s.build(scope, withAlerts)
	if err != nil {
		return nil, err
	}
	entry.value, entry.at, entry.generation = value, s.now(), generation
	return value, nil
}

func cacheKey(scope repositories.AccessScope, withAlerts bool) string {
	key := "all"
	if !scope.All {
		key = "user:" + strconv.FormatUint(uint64(scope.UserID), 10)
	}
	if withAlerts {
		key += "+alerts"
	}
	return key
}

func (s *DashboardService) entryFor(key string) *dashboardEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.cache[key]
	if !ok {
		e = &dashboardEntry{}
		s.cache[key] = e
	}
	return e
}

// generation ist der Stand der Ereignisse, die das Dashboard verändern.
func (s *DashboardService) generation() uint64 {
	return s.bus.Generation(events.TypeServerStatus, events.TypeJobFinished, events.TypeAlertFired)
}

// fresh meldet, ob der Zwischenspeicher noch gilt: jung genug und seit dem
// Rechnen kein Ereignis, das etwas am Dashboard ändert.
func (s *DashboardService) fresh(e *dashboardEntry) bool {
	return e.value != nil && s.now().Sub(e.at) < dashboardTTL && e.generation == s.generation()
}

func (s *DashboardService) build(scope repositories.AccessScope, withAlerts bool) (*Dashboard, error) {
	servers, err := s.servers.servers.FindAll(scope)
	if err != nil {
		return nil, err
	}
	statuses, err := s.servers.statusOf(servers)
	if err != nil {
		return nil, err
	}
	dash := &Dashboard{GeneratedAt: s.now()}
	dash.Tiles, dash.Attention = summarizeFleet(servers, statuses)

	if dash.RecentJobs, err = s.recentJobs(scope); err != nil {
		return nil, err
	}
	if dash.Upcoming, err = s.upcoming(scope); err != nil {
		return nil, err
	}
	dash.RecentAlerts = []domain.AlertEvent{}
	if withAlerts {
		if dash.RecentAlerts, err = s.alerts.FindRecentEventsScoped(scope, dashboardAlertsMax); err != nil {
			return nil, err
		}
	}
	return dash, nil
}

// summarizeFleet zählt die Ampeln und wählt die Server, die Aufmerksamkeit brauchen
// (rot vor gelb, je Stufe in der Reihenfolge der Serverliste). Ein Server, dessen
// Bewertung scheiterte (fehlt in statuses), zählt als rot - wie auf der
// bisherigen Dashboard-Seite.
func summarizeFleet(servers []domain.Server, statuses []ServerStatus) (DashboardTiles, []DashboardAttention) {
	byID := make(map[uint]ServerStatus, len(statuses))
	for _, st := range statuses {
		byID[st.ID] = st
	}
	var tiles DashboardTiles
	var red, yellow []DashboardAttention
	for i := range servers {
		st, ok := byID[servers[i].ID]
		if !ok {
			st = ServerStatus{ID: servers[i].ID, Status: domain.ServerStatusRed}
		}
		tiles.Total++
		if st.Incapable {
			tiles.Incapable++
		}
		item := DashboardAttention{ServerID: st.ID, Name: servers[i].Name, Level: st.Status, Insight: topInsight(st.Insights)}
		switch st.Status {
		case domain.ServerStatusExcellent:
			tiles.Excellent++
		case domain.ServerStatusGreen:
			tiles.Green++
		case domain.ServerStatusYellow:
			tiles.Yellow++
			yellow = append(yellow, item)
		default:
			tiles.Red++
			red = append(red, item)
		}
	}
	attention := append(red, yellow...)
	if len(attention) > dashboardAttentionMax {
		attention = attention[:dashboardAttentionMax]
	}
	if attention == nil {
		attention = []DashboardAttention{}
	}
	return tiles, attention
}

// topInsight wählt den wichtigsten Befund: die höchste Schwere, bei Gleichstand
// den ersten (die Bewertung liefert sie nach Wichtigkeit geordnet).
func topInsight(insights []domain.StatusInsight) *domain.StatusInsight {
	var best *domain.StatusInsight
	for i := range insights {
		if best == nil || severityRank(insights[i].Severity) > severityRank(best.Severity) {
			best = &insights[i]
		}
	}
	return best
}

func severityRank(severity string) int {
	switch severity {
	case "critical":
		return 2
	case "warning":
		return 1
	}
	return 0
}

func (s *DashboardService) recentJobs(scope repositories.AccessScope) ([]DashboardJob, error) {
	jobs, err := s.jobs.RecentFinished(scope, dashboardJobsMax)
	if err != nil {
		return nil, err
	}
	out := make([]DashboardJob, 0, len(jobs))
	for i := range jobs {
		j := &jobs[i]
		item := DashboardJob{
			ID: j.ID, ServerID: j.ServerID, Kind: j.Type, Name: j.Name, Status: j.Status,
			StartedAt: j.StartedAt, FinishedAt: j.FinishedAt,
		}
		if j.Server != nil {
			item.ServerName = j.Server.Name
		}
		if j.StartedAt != nil && j.FinishedAt != nil {
			secs := int64(j.FinishedAt.Sub(*j.StartedAt).Seconds())
			item.DurationSeconds = &secs
		}
		out = append(out, item)
	}
	return out, nil
}

// upcoming berechnet die nächsten Läufe der Zeitpläne aus ihren Cron-Ausdrücken.
// System-Zeitpläne (Health-Check im Viertelstundentakt) bleiben draußen - sie
// verdrängten alles andere -, ebenso abgeschaltete Zeitpläne und solche ohne
// aktive Regel.
func (s *DashboardService) upcoming(scope repositories.AccessScope) ([]DashboardRun, error) {
	schedules, err := s.groups.FindSchedulesForScope(scope)
	if err != nil {
		return nil, err
	}
	now := s.now()
	runs := make([]DashboardRun, 0, len(schedules))
	for i := range schedules {
		if run, ok := nextRun(&schedules[i], now); ok {
			runs = append(runs, run)
		}
	}
	sort.SliceStable(runs, func(a, b int) bool { return runs[a].NextRun.Before(runs[b].NextRun) })
	if len(runs) > dashboardUpcomingMax {
		runs = runs[:dashboardUpcomingMax]
	}
	return runs, nil
}

// nextRun beschreibt den nächsten Lauf eines Zeitplans; ok=false, wenn er nicht
// ins Dashboard gehört oder sein Ausdruck unbrauchbar ist.
func nextRun(sc *domain.Schedule, now time.Time) (DashboardRun, bool) {
	if sc.IsSystem || !sc.Enabled {
		return DashboardRun{}, false
	}
	spec, err := cronParser.Parse(sc.CronExpr)
	if err != nil {
		return DashboardRun{}, false
	}
	rules := make([]DashboardRule, 0, len(sc.Rules))
	for _, r := range sc.Rules {
		if r.Enabled {
			rules = append(rules, DashboardRule{ID: r.ID, Name: r.Name, Kind: r.Type})
		}
	}
	next := spec.Next(now)
	if len(rules) == 0 || next.IsZero() {
		return DashboardRun{}, false
	}
	run := DashboardRun{ScheduleID: sc.ID, ScheduleName: sc.Name, GroupID: sc.GroupID, NextRun: next, Rules: rules}
	if sc.Group != nil {
		run.GroupName = sc.Group.Name
	}
	return run, true
}
