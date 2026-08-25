package services

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/robfig/cron/v3"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

var (
	ErrProtectedGroup    = errors.New("die system-gruppe kann nicht verändert oder aufgelöst werden")
	ErrProtectedRule     = errors.New("system-rules können nicht gelöscht werden")
	ErrProtectedSchedule = errors.New("system-schedules können nicht gelöscht werden")
	ErrInvalidCron       = errors.New("ungültiger cron-ausdruck")
	// ErrEnforceOnlyRuleType: „ACL einrichten" und „Rechte-Soll" beschreiben
	// einen Zustand, der gelten SOLL - nicht eine Aktion zu einer Uhrzeit. An
	// einem Zeitplan gäbe es für sie keinen Ausführungspfad; die Regel liefe
	// ins Leere und meldete „kein Kommando für rule-typ".
	ErrEnforceOnlyRuleType = errors.New("dieser typ ist nur als grundsatz-regel möglich - er beschreibt einen Soll-Zustand, der bei jeder Verbindung geprüft wird, keine Aktion zu einer Uhrzeit")
	// ErrInvalidGroupPriority: Der Vorrang entscheidet, welche Gruppe sich
	// durchsetzt, wenn mehrere dasselbe regeln - kleinere Zahl gewinnt.
	ErrInvalidGroupPriority = fmt.Errorf("ungültiger vorrang - erlaubt ist 1 bis %d (kleinere Zahl = stärkerer Vorrang; Standard %d, System-Gruppe %d)",
		domain.MaxGroupPriority, domain.DefaultGroupPriority, domain.SystemGroupPriority)
	ErrInvalidRuleType = errors.New("ungültiger rule-typ für eine Gruppen-Regel - erlaubt: update, packages, security, package-scan, autoremove, script, custom, sync, health, firewall, docker-prune, docker-update-unused, apt-proxy, reboot, dns-test, deep-scan; flottenweite Läufe (cve-scan, docker-check, alert-check) laufen als System-Zeitplan, nicht pro Gruppe")
	ErrEnforceRuleType = errors.New("grundsatz-regeln sind nur für die typen firewall, apt-proxy, acl-setup und perm-sync möglich - sie tragen einen Soll-Zustand, gegen den geprüft werden kann. Ein Shell-Kommando hat keinen; dafür einen Zeitplan mit einer script-Regel anlegen (dann erscheint jede Ausführung als eigener Job)")
	// ErrEnforceFirewallSpec: eine Firewall-Grundsatz-Regel OHNE erkennbaren
	// Soll-Zustand setzte still „alles außer SSH schließen" durch - auf allen
	// Servern der Gruppe, alle 15 Minuten, ohne Rückmeldung (R2-082). Der
	// Soll-Zustand ist deshalb Pflicht; wer wirklich nur SSH offen haben will,
	// gibt das mit einer leeren Liste ausdrücklich an.
	ErrEnforceFirewallSpec = errors.New("eine Firewall-Grundsatz-Regel braucht einen ausdrücklichen Soll-Zustand: die freizugebenden Regeln als JSON (z.B. [{\"port\":443,\"proto\":\"tcp\"}]) oder eine Portliste (z.B. \"443,8080\"). Sollen AUSSER SSH keine Ports offen sein, ist das mit der leeren Liste [] ausdrücklich anzugeben")
	// ErrRuleCommandUnused: der Regeltyp wertet kein Kommando aus - ein
	// trotzdem mitgeschicktes würde gespeichert und still ignoriert; das
	// ist eine Falle (R2-088: apt-proxy nahm eine abweichende Cache-URL an
	// und verwendete stets die globale).
	ErrRuleCommandUnused       = errors.New("dieser regeltyp verwendet kein kommando - feld weglassen")
	ErrCustomActionNotSelected = errors.New("für eine custom-rule muss eine gültige custom-aktion gewählt werden")
	ErrRuleNeedsTarget         = errors.New("eine rule braucht entweder einen schedule oder das grundsatz-flag (enforce) - genau eines von beiden")
)

// cronParser validiert Cron-Ausdrücke (gleiche Semantik wie der Scheduler).
var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// GroupService verwaltet Servergruppen und deren Rules. Nach jeder
// Änderung an Rules/Schedules wird der Scheduler neu geladen.
type GroupService struct {
	groups        *repositories.GroupRepository
	servers       *repositories.ServerRepository
	audit         *AuditService
	customActions *repositories.CustomActionRepository
	// users wird für die Manager-Zuweisung gebraucht (AssignManager).
	users *repositories.UserRepository
	// reload wird nach Rule-Änderungen aufgerufen (Scheduler.Reload).
	reload func() error
	// prov gleicht die Linux-Benutzer ab, wenn ein Server eine Gruppe
	// betritt oder verlässt. Optional - ohne ihn passiert das erst beim
	// nächsten geplanten Sync.
	prov *ProvisioningService
}

// WithProvisioning verdrahtet den Benutzer-Abgleich: Kommt ein Server in eine
// Gruppe, erbt er deren Linux-Benutzer; verlässt er sie, verliert er die
// Konten, zu denen ihn nur diese Gruppe berechtigt hat.
func (s *GroupService) WithProvisioning(prov *ProvisioningService) *GroupService {
	s.prov = prov
	return s
}

// WithCustomActions verdrahtet die Custom-Aktionen (Validierung des
// Rule-Typs "custom"). Optional, damit schlanke Tests ohne sie auskommen.
func (s *GroupService) WithCustomActions(repo *repositories.CustomActionRepository) *GroupService {
	s.customActions = repo
	return s
}

// WithUsers verdrahtet das Benutzer-Repository für die Manager-Zuweisung.
// Optional wie WithCustomActions, damit schlanke Tests ohne es auskommen.
func (s *GroupService) WithUsers(repo *repositories.UserRepository) *GroupService {
	s.users = repo
	return s
}

func NewGroupService(groups *repositories.GroupRepository, servers *repositories.ServerRepository, audit *AuditService, reload func() error) *GroupService {
	return &GroupService{groups: groups, servers: servers, audit: audit, reload: reload}
}

func (s *GroupService) List(scope repositories.AccessScope) ([]domain.ServerGroup, error) {
	return s.groups.FindAll(scope)
}

func (s *GroupService) Get(scope repositories.AccessScope, id uint) (*domain.ServerGroup, error) {
	return s.groups.FindByID(scope, id)
}

// Create legt eine Gruppe an. priority ist optional (nil = Standardwert).
func (s *GroupService) Create(name, description string, priority *int, actor string) (*domain.ServerGroup, error) {
	group := &domain.ServerGroup{
		Name: name, Description: description, Priority: domain.DefaultGroupPriority,
	}
	if err := applyGroupPriority(group, priority); err != nil {
		return nil, err
	}
	if err := s.groups.Create(group); err != nil {
		return nil, err
	}
	s.audit.Log(actor, "group.create", "group", group.ID, name)
	return group, nil
}

// UpdateSettings ändert Name, Beschreibung und Vorrang. priority ist optional
// (nil = unverändert).
func (s *GroupService) UpdateSettings(scope repositories.AccessScope, id uint, name, description string, priority *int, actor string) (*domain.ServerGroup, error) {
	group, err := s.groups.FindByID(scope, id)
	if err != nil {
		return nil, err
	}
	if group.IsSystem {
		return nil, ErrProtectedGroup
	}
	if name != "" {
		group.Name = name
	}
	group.Description = description
	prev := group.Priority
	if err := applyGroupPriority(group, priority); err != nil {
		return nil, err
	}
	if err := s.groups.Update(group); err != nil {
		return nil, err
	}
	s.audit.Log(actor, "group.update", "group", id, group.Name)
	// Der Vorrang entscheidet, welche Grundsatz-Regel sich auf gemeinsam
	// bespielten Servern durchsetzt - eine Änderung daran kann Ports öffnen
	// oder schließen und gehört deshalb als eigener Eintrag ins Protokoll.
	if group.Priority != prev {
		s.audit.Log(actor, "group.priority", "group", id,
			fmt.Sprintf("%s: Vorrang %d → %d", group.Name, prev, group.Priority))
	}
	return group, nil
}

// applyGroupPriority übernimmt einen gewünschten Vorrang nach Prüfung.
func applyGroupPriority(group *domain.ServerGroup, priority *int) error {
	if priority == nil {
		if group.Priority == 0 {
			group.Priority = domain.DefaultGroupPriority
		}
		return nil
	}
	if !domain.ValidGroupPriority(*priority) {
		return ErrInvalidGroupPriority
	}
	group.Priority = *priority
	return nil
}

// Disband löst eine Gruppe auf: Server bleiben bestehen (nur die Zuordnung
// entfällt), Schedules und Rules der Gruppe werden mit gelöscht. Die
// System-Gruppe ist geschützt.
func (s *GroupService) Disband(scope repositories.AccessScope, id uint, actor string) error {
	group, err := s.groups.FindByID(scope, id)
	if err != nil {
		return err
	}
	if group.IsSystem {
		return ErrProtectedGroup
	}
	if err := s.groups.Disband(id); err != nil {
		return err
	}
	s.audit.Log(actor, "group.disband", "group", id, group.Name)
	_ = s.reload()
	return nil
}

// AssignServer / RemoveServer verwalten die Mitgliedschaft von Servern.
func (s *GroupService) AssignServer(scope repositories.AccessScope, groupID, serverID uint, actor string) error {
	group, err := s.groups.FindByID(scope, groupID)
	if err != nil {
		return err
	}
	server, err := s.servers.FindByID(scope, serverID)
	if err != nil {
		return err
	}
	if err := s.groups.AddServer(group, server); err != nil {
		return err
	}
	s.audit.Log(actor, "group.assign-server", "group", groupID, server.Name)
	// Der Server erbt jetzt die Linux-Benutzer der Gruppe.
	if s.prov != nil {
		s.prov.ReconcileServer(server, nil, actor)
	}
	return nil
}

func (s *GroupService) RemoveServer(scope repositories.AccessScope, groupID, serverID uint, actor string) error {
	group, err := s.groups.FindByID(scope, groupID)
	if err != nil {
		return err
	}
	server, err := s.servers.FindByID(scope, serverID)
	if err != nil {
		return err
	}
	// Vor dem Entfernen festhalten, wer hier berechtigt war - danach ist die
	// Zuordnung weg und der Unterschied nicht mehr zu ermitteln.
	var before []string
	if s.prov != nil {
		before = s.prov.EntitledUsernames(server.ID)
	}
	if err := s.groups.RemoveServer(group, server); err != nil {
		return err
	}
	s.audit.Log(actor, "group.remove-server", "group", groupID, server.Name)
	// Konten, zu denen nur diese Gruppe berechtigt hat, werden entfernt.
	if s.prov != nil {
		s.prov.ReconcileServer(server, before, actor)
	}
	return nil
}

// AssignManager / RemoveManager verwalten, welche Benutzer eine Gruppe
// betreuen - das Gegenstück zu AssignServer/RemoveServer.
//
// Erst darüber wird die Manager-Rolle nutzbar: ScopeManager filtert jede
// Abfrage auf die Gruppen, in denen der Benutzer als Manager eingetragen ist.
// Ohne Schreibweg sah jeder Manager dauerhaft eine leere Server-Liste, und
// das im README als Kernfunktion beworbene RBAC-Feature war funktionslos
// (BUG-018).
func (s *GroupService) AssignManager(scope repositories.AccessScope, groupID, userID uint, actor string) error {
	group, user, err := s.managerTargets(scope, groupID, userID)
	if err != nil {
		return err
	}
	if err := s.groups.AddManager(group, user); err != nil {
		return err
	}
	s.audit.Log(actor, "group.assign-manager", "group", groupID, user.Username)
	return nil
}

func (s *GroupService) RemoveManager(scope repositories.AccessScope, groupID, userID uint, actor string) error {
	group, user, err := s.managerTargets(scope, groupID, userID)
	if err != nil {
		return err
	}
	if err := s.groups.RemoveManager(group, user); err != nil {
		return err
	}
	s.audit.Log(actor, "group.remove-manager", "group", groupID, user.Username)
	return nil
}

// managerTargets löst Gruppe und Benutzer auf und prüft dabei beides -
// unbekannte IDs führen zu einem sauberen Nicht-gefunden-Fehler statt zu
// einem durchschlagenden Datenbankfehler.
func (s *GroupService) managerTargets(scope repositories.AccessScope, groupID, userID uint) (*domain.ServerGroup, *domain.User, error) {
	if s.users == nil {
		return nil, nil, errors.New("benutzerverwaltung nicht verfügbar")
	}
	group, err := s.groups.FindByID(scope, groupID)
	if err != nil {
		return nil, nil, err
	}
	user, err := s.users.FindByID(userID)
	if err != nil {
		return nil, nil, err
	}
	return group, user, nil
}

// ---- Schedules ---------------------------------------------------------------

func (s *GroupService) ListSchedules(scope repositories.AccessScope, groupID uint) ([]domain.Schedule, error) {
	if _, err := s.groups.FindByID(scope, groupID); err != nil {
		return nil, err
	}
	return s.groups.FindSchedulesForGroup(groupID)
}

// DefineSchedule erstellt einen neuen Zeitplan für eine Gruppe. Rules
// werden anschließend am Schedule angelegt (DefineRule mit scheduleID).
func (s *GroupService) DefineSchedule(scope repositories.AccessScope, groupID uint, name, cronExpr, actor string) (*domain.Schedule, error) {
	if _, err := s.groups.FindByID(scope, groupID); err != nil {
		return nil, err
	}
	if _, err := cronParser.Parse(cronExpr); err != nil {
		return nil, ErrInvalidCron
	}
	sched := &domain.Schedule{GroupID: groupID, Name: name, CronExpr: cronExpr, Enabled: true}
	if err := s.groups.CreateSchedule(sched); err != nil {
		return nil, err
	}
	s.audit.Log(actor, "schedule.define", "schedule", sched.ID, name)
	_ = s.reload()
	return sched, nil
}

// UpdateSchedule ändert Name und/oder Cron-Ausdruck eines Schedules.
func (s *GroupService) UpdateSchedule(scope repositories.AccessScope, id uint, name, cronExpr, actor string) (*domain.Schedule, error) {
	sched, err := s.groups.FindSchedule(scope, id)
	if err != nil {
		return nil, err
	}
	if cronExpr != "" {
		if _, err := cronParser.Parse(cronExpr); err != nil {
			return nil, ErrInvalidCron
		}
		sched.CronExpr = cronExpr
	}
	if name != "" {
		sched.Name = name
	}
	if err := s.groups.UpdateSchedule(sched); err != nil {
		return nil, err
	}
	s.audit.Log(actor, "schedule.update", "schedule", id, sched.Name)
	_ = s.reload()
	return sched, nil
}

// RemoveSchedule löscht einen Schedule samt seiner Rules (System-Schedules
// sind geschützt).
func (s *GroupService) RemoveSchedule(scope repositories.AccessScope, id uint, actor string) error {
	sched, err := s.groups.FindSchedule(scope, id)
	if err != nil {
		return err
	}
	if sched.IsSystem {
		return ErrProtectedSchedule
	}
	if err := s.groups.DeleteSchedule(id); err != nil {
		return err
	}
	s.audit.Log(actor, "schedule.remove", "schedule", id, sched.Name)
	_ = s.reload()
	return nil
}

// SetScheduleEnabled aktiviert/deaktiviert einen Zeitplan.
func (s *GroupService) SetScheduleEnabled(scope repositories.AccessScope, id uint, enabled bool, actor string) error {
	sched, err := s.groups.FindSchedule(scope, id)
	if err != nil {
		return err
	}
	sched.Enabled = enabled
	if err := s.groups.UpdateSchedule(sched); err != nil {
		return err
	}
	action := "schedule.disable"
	if enabled {
		action = "schedule.enable"
	}
	s.audit.Log(actor, action, "schedule", id, sched.Name)
	_ = s.reload()
	return nil
}

// FindSchedule lädt einen Schedule scope-geprüft (für Trigger-Now).
func (s *GroupService) FindSchedule(scope repositories.AccessScope, id uint) (*domain.Schedule, error) {
	return s.groups.FindSchedule(scope, id)
}

// ---- Rules -------------------------------------------------------------------

func (s *GroupService) ListRules(scope repositories.AccessScope, groupID uint) ([]domain.Rule, error) {
	if _, err := s.groups.FindByID(scope, groupID); err != nil {
		return nil, err
	}
	return s.groups.FindRulesForGroup(groupID)
}

// DefineRule erstellt eine neue Rule für eine Gruppe. Sie hängt ENTWEDER
// an einem Schedule der Gruppe (zeitgesteuert) ODER ist eine Grundsatz-
// Regel (enforce): die wird bei jeder Verbindung geprüft und durchgesetzt
// und ist nur für zustandserzwingende Typen (Firewall, Script) erlaubt.
func (s *GroupService) DefineRule(scope repositories.AccessScope, groupID uint, name, ruleType, command string, scheduleID *uint, enforce bool, actor string) (*domain.Rule, error) {
	if _, err := s.groups.FindByID(scope, groupID); err != nil {
		return nil, err
	}
	if !validRuleType(ruleType) {
		return nil, ErrInvalidRuleType
	}
	if enforce == (scheduleID != nil) {
		return nil, ErrRuleNeedsTarget
	}
	if enforce && !enforceableRuleType(ruleType) {
		return nil, ErrEnforceRuleType
	}
	if !enforce && enforceOnlyRuleType(ruleType) {
		return nil, ErrEnforceOnlyRuleType
	}
	if err := validateEnforceFirewallSpec(ruleType, enforce, command); err != nil {
		return nil, err
	}
	// Ein Kommando für einen Typ, der keines auswertet, wird abgewiesen
	// statt gespeichert-und-ignoriert (R2-088).
	if !ruleTypeUsesCommand(ruleType) && strings.TrimSpace(command) != "" {
		return nil, fmt.Errorf("%w (typ %q)", ErrRuleCommandUnused, ruleType)
	}
	// Benannte Paket-Updates brauchen eine gültige Paketliste im Command.
	if ruleType == domain.RuleTypePackages {
		if _, err := parsePackageNames(command); err != nil {
			return nil, err
		}
	}
	// Custom-Aktionen referenzieren im Command eine existierende CustomAction-ID.
	if ruleType == domain.RuleTypeCustom {
		if s.customActions == nil {
			return nil, ErrInvalidRuleType
		}
		aid, err := strconv.ParseUint(strings.TrimSpace(command), 10, 64)
		if err != nil {
			return nil, ErrCustomActionNotSelected
		}
		if _, err := s.customActions.FindByID(uint(aid)); err != nil {
			return nil, ErrCustomActionNotSelected
		}
	}
	if scheduleID != nil {
		sched, err := s.groups.FindSchedule(scope, *scheduleID)
		if err != nil {
			return nil, err
		}
		if sched.GroupID != groupID {
			return nil, repositories.ErrNotFound
		}
	}
	rule := &domain.Rule{
		GroupID: groupID, ScheduleID: scheduleID, Enforce: enforce,
		Name: name, Type: ruleType, Command: command, Enabled: true,
	}
	if err := s.groups.CreateRule(rule); err != nil {
		return nil, err
	}
	s.audit.Log(actor, "rule.define", "rule", rule.ID, name)
	_ = s.reload()
	return rule, nil
}

// UpdateRule ändert eine bestehende Rule (Name/Kommando bzw. Portliste).
func (s *GroupService) UpdateRule(scope repositories.AccessScope, id uint, name, command string, actor string) (*domain.Rule, error) {
	rule, err := s.groups.FindRule(scope, id)
	if err != nil {
		return nil, err
	}
	if name != "" {
		rule.Name = name
	}
	if !rule.IsSystem {
		if !ruleTypeUsesCommand(rule.Type) && strings.TrimSpace(command) != "" {
			return nil, fmt.Errorf("%w (typ %q)", ErrRuleCommandUnused, rule.Type)
		}
		// Dieselbe Prüfung wie beim Anlegen: über PATCH ließ sich der
		// Soll-Zustand sonst nachträglich auf „unverstanden" setzen und die
		// Regel schloss ab dem nächsten Health-Check alle Dienstports
		// (R2-082, TS-18-08: command="echo x" → 200, danach nur noch 22/tcp).
		if err := validateEnforceFirewallSpec(rule.Type, rule.Enforce, command); err != nil {
			return nil, err
		}
		rule.Command = command
	}
	if err := s.groups.UpdateRule(rule); err != nil {
		return nil, err
	}
	s.audit.Log(actor, "rule.update", "rule", id, rule.Name)
	_ = s.reload()
	return rule, nil
}

// validateEnforceFirewallSpec stellt sicher, dass eine Firewall-Grundsatz-
// Regel einen VERSTANDENEN Soll-Zustand trägt.
//
// Hintergrund: parseFirewallRuleSpec liefert für einen leeren wie für einen
// nicht als Portliste lesbaren Text („echo x") eine leere Regelmenge OHNE
// Fehler. Die Durchsetzung las das als „keine Ports freigeben" und schloss
// bestandsweit alle Dienstports - selbstheilend alle 15 Minuten und ohne
// jede Rückmeldung (R2-082). Die leere Liste `[]` bleibt zulässig: sie ist
// die ausdrückliche Ansage „nur SSH".
func validateEnforceFirewallSpec(ruleType string, enforce bool, command string) error {
	if !enforce || ruleType != domain.RuleTypeFirewall {
		return nil
	}
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return ErrEnforceFirewallSpec
	}
	rules, err := parseFirewallRuleSpec(trimmed)
	if err != nil {
		return fmt.Errorf("%w - %v", ErrEnforceFirewallSpec, err)
	}
	// Leeres Ergebnis ist nur dann gewollt, wenn ausdrücklich `[]` dasteht.
	if len(rules) == 0 && trimmed != "[]" {
		return ErrEnforceFirewallSpec
	}
	return nil
}

// RemoveRule löscht eine Rule (System-Rules sind geschützt).
func (s *GroupService) RemoveRule(scope repositories.AccessScope, id uint, actor string) error {
	rule, err := s.groups.FindRule(scope, id)
	if err != nil {
		return err
	}
	if rule.IsSystem {
		return ErrProtectedRule
	}
	if err := s.groups.DeleteRule(id); err != nil {
		return err
	}
	s.audit.Log(actor, "rule.remove", "rule", id, rule.Name)
	_ = s.reload()
	return nil
}

// SetRuleEnabled aktiviert/deaktiviert eine einzelne Rule (innerhalb
// ihres Schedules bzw. als Grundsatz-Regel).
func (s *GroupService) SetRuleEnabled(scope repositories.AccessScope, id uint, enabled bool, actor string) error {
	rule, err := s.groups.FindRule(scope, id)
	if err != nil {
		return err
	}
	rule.Enabled = enabled
	if err := s.groups.UpdateRule(rule); err != nil {
		return err
	}
	action := "rule.disable"
	if enabled {
		action = "rule.enable"
	}
	s.audit.Log(actor, action, "rule", id, rule.Name)
	_ = s.reload()
	return nil
}

// FindRule lädt eine Rule scope-geprüft (für Trigger-Now).
func (s *GroupService) FindRule(scope repositories.AccessScope, id uint) (*domain.Rule, error) {
	return s.groups.FindRule(scope, id)
}

// ListAllRules liefert die Regeln aller sichtbaren Gruppen - die globale
// Regel-Sicht (R2-085). Manager sehen nur die Regeln ihrer Gruppen.
func (s *GroupService) ListAllRules(scope repositories.AccessScope) ([]domain.Rule, error) {
	return s.groups.FindAllRules(scope)
}

// ruleTypeUsesCommand: nur diese Typen werten das Command-Feld aus
// (Paketliste, Skript, Custom-Aktions-ID, Firewall-Regel-JSON).
func ruleTypeUsesCommand(t string) bool {
	switch t {
	case domain.RuleTypePackages, domain.RuleTypeScript, domain.RuleTypeCustom, domain.RuleTypeFirewall:
		return true
	}
	return false
}

func validRuleType(t string) bool {
	switch t {
	case domain.RuleTypeUpdate, domain.RuleTypePackages, domain.RuleTypeSecurity,
		domain.RuleTypePackageScan, domain.RuleTypeAutoremove, domain.RuleTypeScript, domain.RuleTypeCustom,
		domain.RuleTypeSync, domain.RuleTypeHealth, domain.RuleTypeFirewall,
		domain.RuleTypeBackup, domain.RuleTypeCleanup, domain.RuleTypeDockerPrune,
		domain.RuleTypeDockerUpdateUnused, domain.RuleTypeAptProxy,
		domain.RuleTypeReboot, domain.RuleTypeRebootIfNeeded,
		domain.RuleTypeDNSTest, domain.RuleTypeDeepScan,
		domain.RuleTypeACLSetup, domain.RuleTypePermSync:
		return true
	}
	return false
}
