package services

import (
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"LCM/internal/core/agentcaps"
	"LCM/internal/core/domain"
	"LCM/internal/cron"
	"LCM/internal/safego"
	"LCM/internal/storage/repositories"
	"LCM/internal/version"
)

// Agent-Update-Regel einer Gruppe.
//
// Ist die Regel eingeschaltet, startet der Zeitplaner zu Beginn des Fensters
// einen Lauf (Rollout): Er nimmt die Agent-Server der Gruppe, deren Agent in
// der Feature-Reihe von LCM oder darunter liegt, und aktualisiert sie über
// denselben Job wie der Knopf. Auch Agenten auf dem Stand von LCM zählen mit:
// Ein neuer Agent-Bugfix kommt so heran, ohne dass LCM selbst ein Update braucht.
//
// Der erste Server (nach Namen, damit es von Lauf zu Lauf derselbe bleibt)
// geht immer allein voraus. Sein Job liest, was die Quelle gerade bietet; die
// übrigen kommen nur noch dran, wenn ihr Agent unter seinem Stand liegt. Ist
// nichts Neues da, bleibt es bei diesem einen Job.
//
// Gestaffelt (Vorgabe) heißt zusätzlich: Der Rest wird auf höchstens die
// Version gezogen, die der erste erreicht hat, und erst nachdem er sich damit
// zurückgemeldet hat und auf Kommandos antwortet (das prüft der Job selbst).
// Den Stand hält die Regel fest (ProvenFor/ProvenVersion).
//
// Scheitert der erste Server gestaffelt - oder meldet irgendein Server einen
// Rollback -, hält die Regel für diese LCM-Version an (BlockedFor). Ohne
// Eingriff versucht sie es nicht noch einmal; eine neue LCM-Version oder
// „Fortsetzen“ hebt die Sperre auf. Server, die zurückgerollt sind, lässt der
// Lauf aus: Sie brauchen erst einen Blick.

// ErrRolloutRunning: Für diese Gruppe läuft schon ein Lauf.
var ErrRolloutRunning = errors.New("für diese gruppe läuft bereits ein agent-update")

// Fehler der Regel-Eingabe.
var (
	ErrAgentUpdateMode   = errors.New("ungültiger modus - erlaubt: off, auto")
	ErrAgentUpdateCron   = errors.New("ungültiger zeitpunkt für das fenster (cron-ausdruck)")
	ErrAgentUpdateWindow = errors.New("ungültige fensterlänge (0 bis 1440 minuten)")
)

// rolloutWindowGrace ist, wie lange nach dem Fensterbeginn ein Lauf noch
// „im Fenster“ gilt, wenn die Regel kein Fenster (0 Minuten) hat - bei einem
// LCM-Update kurz nach dem Beginn soll er noch starten dürfen.
const rolloutWindowGrace = 30 * time.Minute

// Verzögerung des Laufs nach einem LCM-Update: Die Agenten verbinden sich nach
// dem Neustart von LCM mit wachsendem Abstand (bis zu zwei Minuten) neu.
// Variable für Tests.
var AgentUpdateAfterLCMUpdate = 5 * time.Minute

// AgentUpdateService führt die Agent-Update-Regeln der Gruppen aus.
type AgentUpdateService struct {
	policies *repositories.AgentUpdateRepository
	audit    *AuditService
	// members liefert die Server einer Gruppe; reload liest einen Server frisch.
	members func(groupID uint) ([]domain.Server, error)
	reload  func(id uint) (*domain.Server, error)
	// connected meldet, ob der Agent des Servers gerade verbunden ist.
	connected func(*domain.Server) bool
	// start startet den Update-Job (limit begrenzt die Zielversion), wait
	// wartet auf sein Ende und meldet den Erfolg.
	start func(serverID uint, actor, limit string) (jobID string, err error)
	wait  func(jobID string) bool
	// reschedule lässt den Zeitplaner die Fenster neu anlegen.
	reschedule func() error
	// serverVersion ist die Version dieses LCM; sleep und now sind Test-Nähte.
	serverVersion func() string
	sleep         func(time.Duration)
	now           func() time.Time

	mu      sync.Mutex
	running map[uint]bool
}

// NewAgentUpdateService verdrahtet den Dienst mit Server- und Gruppen-Dienst.
func NewAgentUpdateService(policies *repositories.AgentUpdateRepository, groups *repositories.GroupRepository,
	servers *repositories.ServerRepository, serverSvc *ServerService, audit *AuditService, reschedule func() error) *AgentUpdateService {
	return &AgentUpdateService{
		policies: policies, audit: audit,
		members: groups.ServersOfGroup,
		reload: func(id uint) (*domain.Server, error) {
			return servers.FindByID(repositories.ScopeAll(), id)
		},
		connected: serverSvc.AgentConnected,
		start: func(serverID uint, actor, limit string) (string, error) {
			job, err := serverSvc.StartAgentUpdateTo(repositories.ScopeAll(), serverID, actor, limit)
			if err != nil {
				return "", err
			}
			return job.ID, nil
		},
		wait:          serverSvc.waitJobTerminal,
		reschedule:    reschedule,
		serverVersion: func() string { return version.Version },
		sleep:         time.Sleep,
		now:           time.Now,
		running:       map[uint]bool{},
	}
}

// AgentConnected meldet, ob der Agent des Servers verbunden ist.
func (s *ServerService) AgentConnected(server *domain.Server) bool {
	return server.IsAgent() && s.agents != nil && s.agents.Online(server.AgentID)
}

// ---- Einstellungen -------------------------------------------------------------

// AgentUpdateInput sind die Felder, die der Betreiber setzt.
type AgentUpdateInput struct {
	Mode          string `json:"mode"`
	Staged        bool   `json:"staged"`
	CronExpr      string `json:"cron_expr"`
	WindowMinutes int    `json:"window_minutes"`
}

// AgentUpdateView ist die Regel samt dem, was die Oberfläche daneben zeigt.
type AgentUpdateView struct {
	domain.AgentUpdatePolicy
	// NextRun ist der nächste Fensterbeginn (RFC 3339; leer, wenn die Regel aus ist).
	NextRun string `json:"next_run"`
	// ServerVersion ist die Version dieses LCM - bis dahin zieht die Regel nach.
	ServerVersion string `json:"server_version"`
	// AgentServers zählt die Agent-Server der Gruppe, Outdated die davon hinter LCM.
	AgentServers int `json:"agent_servers"`
	Outdated     int `json:"outdated"`
	// Running meldet, ob gerade ein Lauf arbeitet.
	Running bool `json:"running"`
	// Blocked meldet, dass die Regel für die aktuelle LCM-Version angehalten hat.
	Blocked bool `json:"blocked"`
}

// Get liefert die Regel einer Gruppe.
func (s *AgentUpdateService) Get(groupID uint) (*AgentUpdateView, error) {
	p, err := s.policies.Get(groupID)
	if err != nil {
		return nil, err
	}
	view := &AgentUpdateView{AgentUpdatePolicy: *p, ServerVersion: s.serverVersion()}
	if p.Enabled() {
		view.NextRun = nextRunOf(p.CronExpr, s.now())
	}
	view.Blocked = p.BlockedFor != "" && p.BlockedFor == view.ServerVersion
	s.mu.Lock()
	view.Running = s.running[groupID]
	s.mu.Unlock()
	best := bestKnown(p, view.ServerVersion)
	if servers, err := s.members(groupID); err == nil {
		for i := range servers {
			if !servers[i].IsAgent() {
				continue
			}
			view.AgentServers++
			if behind(&servers[i], best) {
				view.Outdated++
			}
		}
	}
	return view, nil
}

// Save speichert die Einstellungen der Regel.
func (s *AgentUpdateService) Save(groupID uint, in AgentUpdateInput, actor string) (*AgentUpdateView, error) {
	p, err := validateAgentUpdateInput(groupID, in)
	if err != nil {
		return nil, err
	}
	if err := s.policies.SaveSettings(p); err != nil {
		return nil, err
	}
	if s.reschedule != nil {
		if err := s.reschedule(); err != nil {
			return nil, err
		}
	}
	if s.audit != nil {
		s.audit.Log(actor, "group.agent-update", "group", groupID,
			fmt.Sprintf("%s, gestaffelt: %t, Fenster %s + %d min", p.Mode, p.Staged, p.CronExpr, p.WindowMinutes))
	}
	return s.Get(groupID)
}

// validateAgentUpdateInput prüft die Eingabe und füllt die Vorgaben.
func validateAgentUpdateInput(groupID uint, in AgentUpdateInput) (*domain.AgentUpdatePolicy, error) {
	if in.Mode != domain.AgentUpdateOff && in.Mode != domain.AgentUpdateAuto {
		return nil, ErrAgentUpdateMode
	}
	if in.WindowMinutes < 0 || in.WindowMinutes > domain.MaxAgentUpdateWindowMinutes {
		return nil, ErrAgentUpdateWindow
	}
	expr := strings.TrimSpace(in.CronExpr)
	if expr == "" {
		expr = domain.DefaultAgentUpdateCron
	}
	if _, err := cron.Parse(expr); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAgentUpdateCron, err)
	}
	return &domain.AgentUpdatePolicy{
		GroupID: groupID, Mode: in.Mode, Staged: in.Staged, CronExpr: expr, WindowMinutes: in.WindowMinutes,
	}, nil
}

// nextRunOf liefert den nächsten Fensterbeginn als RFC 3339.
func nextRunOf(expr string, now time.Time) string {
	sched, err := cron.Parse(expr)
	if err != nil {
		return ""
	}
	if next := sched.Next(now); !next.IsZero() {
		return next.Format(time.RFC3339)
	}
	return ""
}

// Resume hebt die Sperre nach einem gescheiterten Lauf auf.
func (s *AgentUpdateService) Resume(groupID uint, actor string) (*AgentUpdateView, error) {
	p, err := s.policies.Get(groupID)
	if err != nil {
		return nil, err
	}
	p.BlockedFor = ""
	if err := s.policies.RecordRun(p); err != nil {
		return nil, err
	}
	if s.audit != nil {
		s.audit.Log(actor, "group.agent-update-resume", "group", groupID, "Sperre aufgehoben")
	}
	return s.Get(groupID)
}

// ---- Lauf ----------------------------------------------------------------------

// RolloutResult fasst einen Lauf zusammen.
type RolloutResult struct {
	Candidates int
	Updated    int
	Current    int // schon auf dem Stand, nichts geändert
	Failed     int
	Skipped    int
	Canary     string
	Blocked    bool
	Note       string
}

// Summary ist der Satz, der in der Regel als letztes Ergebnis steht.
func (r RolloutResult) Summary() string {
	parts := []string{}
	switch {
	case r.Note != "" && r.Candidates == 0:
		return r.Note
	case r.Canary != "":
		parts = append(parts, "erster Server: "+r.Canary)
	}
	parts = append(parts, fmt.Sprintf("%d aktualisiert, %d schon aktuell, %d fehlgeschlagen, %d übersprungen", r.Updated, r.Current, r.Failed, r.Skipped))
	if r.Blocked {
		parts = append(parts, "angehalten - die Regel läuft für diese LCM-Version nicht weiter, bis du sie fortsetzt")
	}
	if r.Note != "" {
		parts = append(parts, r.Note)
	}
	return strings.Join(parts, "; ")
}

// RunNow stößt einen Lauf von Hand an (läuft im Hintergrund). Er ignoriert die
// Fenstergrenzen: wer ihn auslöst, will ihn jetzt.
func (s *AgentUpdateService) RunNow(groupID uint, actor string) error {
	if !s.claim(groupID) {
		return ErrRolloutRunning
	}
	safego.Go(fmt.Sprintf("agent-update:group-%d", groupID), func() {
		defer s.release(groupID)
		s.rollout(groupID, actor, false)
	})
	return nil
}

// RunWindow ist der Lauf des Zeitplaners zu Beginn des Fensters.
func (s *AgentUpdateService) RunWindow(groupID uint, actor string) {
	if !s.claim(groupID) {
		slog.Info("agent update: previous run still active - skipped", "group_id", groupID)
		return
	}
	defer s.release(groupID)
	s.rollout(groupID, actor, true)
}

// OnLCMUpdated wird nach dem Start einer neuen LCM-Version aufgerufen: Mit ihr
// dürfen die Agenten nachziehen. Läuft gerade ein Fenster einer Regel, beginnt
// der Lauf nach kurzer Wartezeit (die Agenten verbinden sich neu); sonst
// übernimmt der Beginn des nächsten Fensters.
func (s *AgentUpdateService) OnLCMUpdated() {
	policies, err := s.policies.Enabled()
	if err != nil {
		slog.Warn("agent update: rules not loadable after the lcm update", "error", err)
		return
	}
	for i := range policies {
		p := policies[i]
		if !inWindow(p.CronExpr, p.WindowMinutes, s.now()) {
			continue
		}
		safego.Go(fmt.Sprintf("agent-update:after-lcm-update-%d", p.GroupID), func() {
			s.sleep(AgentUpdateAfterLCMUpdate)
			s.RunWindow(p.GroupID, ActorScheduler)
		})
	}
}

// inWindow meldet, ob gerade ein Fenster der Regel läuft: Es begann höchstens
// minutes (mindestens rolloutWindowGrace) vor jetzt.
func inWindow(expr string, minutes int, now time.Time) bool {
	sched, err := cron.Parse(expr)
	if err != nil {
		return false
	}
	length := max(time.Duration(minutes)*time.Minute, rolloutWindowGrace)
	next := sched.Next(now.Add(-length).Add(-time.Second))
	return !next.IsZero() && !next.After(now)
}

func (s *AgentUpdateService) claim(groupID uint) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running[groupID] {
		return false
	}
	s.running[groupID] = true
	return true
}

func (s *AgentUpdateService) release(groupID uint) {
	s.mu.Lock()
	delete(s.running, groupID)
	s.mu.Unlock()
}

// rollout ist ein Lauf. requireEnabled: nur wenn die Regel eingeschaltet ist
// (der Zeitplaner); ein Lauf von Hand geht auch bei ausgeschalteter Regel.
func (s *AgentUpdateService) rollout(groupID uint, actor string, requireEnabled bool) RolloutResult {
	p, err := s.policies.Get(groupID)
	if err != nil {
		slog.Error("agent update: rule not loadable", "group_id", groupID, "error", err)
		return RolloutResult{Note: "Regel nicht lesbar: " + err.Error()}
	}
	if requireEnabled && !p.Enabled() {
		return RolloutResult{Note: "Regel ist aus"}
	}
	ver := s.serverVersion()
	result := s.runPolicy(p, ver, actor)
	now := s.now()
	p.LastRunAt, p.LastResult = &now, result.Summary()
	if err := s.policies.RecordRun(p); err != nil {
		slog.Error("agent update: result not saved", "group_id", groupID, "error", err)
	}
	slog.Info("agent update run finished", "group_id", groupID, "result", p.LastResult)
	return result
}

// runPolicy führt den Lauf aus und pflegt dabei den Stand in p.
func (s *AgentUpdateService) runPolicy(p *domain.AgentUpdatePolicy, ver, actor string) RolloutResult {
	if !versionIsKnown(ver) {
		return RolloutResult{Note: "Diese LCM-Version ist ein Entwicklungsstand - es gibt keine Zielversion."}
	}
	if p.BlockedFor != "" && p.BlockedFor == ver {
		return RolloutResult{Blocked: true, Note: "angehalten: Der erste Server dieser LCM-Version ist gescheitert - erst nach „Fortsetzen“ geht es weiter."}
	}
	servers, err := s.members(p.GroupID)
	if err != nil {
		return RolloutResult{Note: "Server der Gruppe nicht lesbar: " + err.Error()}
	}
	cands, skipped := rolloutCandidates(servers, ver, s.connected)
	res := RolloutResult{Candidates: len(cands), Skipped: skipped}
	if len(cands) == 0 {
		res.Note = "Nichts zu tun: Kein erreichbarer Agent der Gruppe in der Reihe von LCM " + ver + "."
		return res
	}

	// Der erste Server geht allein voraus: Sein Job findet heraus, welche
	// Version die Quelle gerade bietet (auch einen neuen Bugfix, während LCM
	// auf derselben Version bleibt). Steht er danach auf einer Version, muss
	// nur noch, wer darunter liegt.
	first := cands[0]
	if p.Staged {
		res.Canary = first.Name
	}
	outcome := s.updateOne(&first, actor, "")
	switch {
	case outcome.ok:
		res.count(outcome)
		p.ProvenFor, p.ProvenVersion = ver, version.Core(outcome.version)
	case outcome.notReady:
		res.Skipped++
	default:
		res.Failed++
	}
	if p.Staged && outcome.notReady {
		res.Note = "Der erste Server (" + first.Name + ") war nicht bereit: " + outcome.note + " - der nächste Lauf versucht es wieder."
		return res
	}
	if !outcome.ok && !outcome.notReady && (p.Staged || outcome.rolled) {
		res.Blocked = true
		res.Note = "Der erste Server (" + first.Name + ") ist gescheitert: " + outcome.note
		p.BlockedFor = ver
		return res
	}

	rest, limit := cands[1:], ""
	if outcome.ok && versionIsKnown(outcome.version) {
		reached := version.Core(outcome.version)
		var current int
		rest, current = below(rest, reached)
		res.Current += current
		if p.Staged {
			limit = reached
		}
	}
	s.updateRest(rest, p, limit, actor, ver, &res)
	return res
}

// below lässt nur die Server übrig, deren Agent unter v liegt; current zählt
// die übrigen (sie stehen schon so hoch wie der erste Server).
func below(servers []domain.Server, v string) (rest []domain.Server, current int) {
	for _, srv := range servers {
		if version.Compare(srv.AgentVersion, v) < 0 {
			rest = append(rest, srv)
		} else {
			current++
		}
	}
	return rest, current
}

// count bucht ein gelungenes Update: geändert oder schon aktuell.
func (r *RolloutResult) count(out oneOutcome) {
	if out.changed {
		r.Updated++
	} else {
		r.Current++
	}
}

// rolloutCandidates wählt die Server, die ein Lauf aktualisiert: verbundene
// Agent-Server, die hinter ver zurückliegen, in fester Reihenfolge (nach Namen,
// damit der erste Server sich nicht von Lauf zu Lauf ändert). Zurückgerollte
// Server und Server in Wartung bleiben außen vor; skipped zählt sie.
func rolloutCandidates(servers []domain.Server, ver string, connected func(*domain.Server) bool) (cands []domain.Server, skipped int) {
	for i := range servers {
		srv := &servers[i]
		if !srv.IsAgent() || srv.IsDemo || !inLine(srv, ver) {
			continue
		}
		if srv.AgentRollback != "" || srv.InMaintenance() || !connected(srv) {
			skipped++
			continue
		}
		cands = append(cands, *srv)
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].Name < cands[j].Name })
	return cands, skipped
}

// inLine meldet, ob LCM den Agent des Servers aktualisieren darf: Seine Version
// ist bekannt und liegt nicht in einer höheren Feature-Reihe als LCM. Ein
// Agent auf dem Stand von LCM zählt mit, denn es kann einen Bugfix geben.
// Ohne bekannte Version (Selbstbau, nie gemeldet) lässt LCM ihn in Ruhe.
func inLine(srv *domain.Server, ver string) bool {
	return versionIsKnown(srv.AgentVersion) && !agentcaps.Ahead(srv.AgentVersion, ver)
}

// bestKnown ist die höchste Agent-Version, die LCM für die Gruppe kennt: die
// eigene Version oder, wenn höher, der Bugfix, den der erste Server zuletzt
// erreicht hat.
func bestKnown(p *domain.AgentUpdatePolicy, ver string) string {
	if p.ProvenFor == ver && versionIsKnown(p.ProvenVersion) && version.Compare(p.ProvenVersion, ver) > 0 {
		return p.ProvenVersion
	}
	return ver
}

// behind meldet, ob der Agent des Servers unter v liegt.
func behind(srv *domain.Server, v string) bool {
	return versionIsKnown(srv.AgentVersion) && versionIsKnown(v) && version.Compare(srv.AgentVersion, v) < 0
}

// oneOutcome ist das Ergebnis des Updates eines Servers.
type oneOutcome struct {
	ok       bool
	changed  bool   // die Version hat sich geändert
	version  string // die Agent-Version nach dem Update
	rolled   bool
	note     string
	notReady bool // Server belegt oder nicht erreichbar: kein Fehler des Updates
}

// updateOne aktualisiert einen Server und wartet auf das Ende des Jobs.
func (s *AgentUpdateService) updateOne(srv *domain.Server, actor, limit string) oneOutcome {
	jobID, err := s.start(srv.ID, actor, limit)
	if err != nil {
		// Belegt oder nicht erreichbar: Der nächste Lauf versucht es wieder.
		return oneOutcome{note: err.Error(), notReady: heldBack(err) || errors.Is(err, ErrAgentOffline)}
	}
	ok := s.wait(jobID)
	fresh, err := s.reload(srv.ID)
	out := oneOutcome{ok: ok}
	if err == nil {
		out.version = fresh.AgentVersion
		out.changed = !agentcaps.SameVersion(srv.AgentVersion, fresh.AgentVersion)
		out.rolled = fresh.AgentRollback != ""
	}
	if !ok {
		out.note = "der Update-Job ist gescheitert (siehe Job-Verlauf)"
		if out.rolled {
			out.note = "der Agent ist auf die vorige Version zurückgerollt"
		}
	}
	return out
}

// updateRest aktualisiert die übrigen Server: gleichmäßig über das Fenster
// verteilt, mit begrenzter Parallelität. Ein Rollback hält die noch nicht
// begonnenen an.
func (s *AgentUpdateService) updateRest(rest []domain.Server, p *domain.AgentUpdatePolicy, limit, actor, ver string, res *RolloutResult) {
	if len(rest) == 0 {
		return
	}
	window := time.Duration(p.WindowMinutes) * time.Minute
	sem := make(chan struct{}, ruleParallelism)
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		stopped bool
	)
	var prev time.Duration
	for i := range rest {
		srv := rest[i]
		// Relativ zum vorigen Start: Die Zeit, die das Warten auf einen freien
		// Platz kostet, zählt dabei zum Fenster.
		offset := spreadOffset(i, len(rest), window)
		if offset > prev {
			s.sleep(offset - prev)
			prev = offset
		}
		mu.Lock()
		halt := stopped
		mu.Unlock()
		if halt {
			mu.Lock()
			res.Skipped++
			mu.Unlock()
			continue
		}
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			defer safego.Recover("agent-update:"+srv.Name, nil)
			out := s.updateOne(&srv, actor, limit)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case out.ok:
				res.count(out)
			case out.notReady:
				res.Skipped++
			default:
				res.Failed++
				if out.rolled {
					stopped = true
					res.Blocked = true
					p.BlockedFor = ver
					res.Note = "Rollback auf " + srv.Name + ": Die Regel hält an."
				}
			}
		}()
	}
	wg.Wait()
}
