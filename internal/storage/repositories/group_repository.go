package repositories

import (
	"gorm.io/gorm"

	"LCM/internal/core/domain"
)

// GroupRepository verwaltet Servergruppen samt Zuordnungen und Rules.
type GroupRepository struct {
	db *gorm.DB
}

func NewGroupRepository(db *gorm.DB) *GroupRepository {
	return &GroupRepository{db: db}
}

func (r *GroupRepository) Create(group *domain.ServerGroup) error {
	return r.db.Create(group).Error
}

func (r *GroupRepository) Update(group *domain.ServerGroup) error {
	return r.db.Save(group).Error
}

func (r *GroupRepository) FindByID(scope AccessScope, id uint) (*domain.ServerGroup, error) {
	var group domain.ServerGroup
	q := scope.scopeGroups(r.db.Preload("Servers").Preload("Managers").Preload("LinuxUsers").Preload("Rules"))
	if err := q.First(&group, id).Error; err != nil {
		return nil, translate(err)
	}
	countACLCapable(&group)
	return &group, nil
}

func (r *GroupRepository) FindByName(name string) (*domain.ServerGroup, error) {
	var group domain.ServerGroup
	if err := r.db.Preload("Servers").Preload("Rules").Where("name = ?", name).First(&group).Error; err != nil {
		return nil, translate(err)
	}
	return &group, nil
}

func (r *GroupRepository) FindAll(scope AccessScope) ([]domain.ServerGroup, error) {
	var groups []domain.ServerGroup
	q := scope.scopeGroups(r.db.Preload("Servers")).Order("server_groups.name")
	if err := q.Find(&groups).Error; err != nil {
		return nil, err
	}
	for i := range groups {
		countACLCapable(&groups[i])
	}
	return groups, nil
}

// countACLCapable zählt, wie viele Mitglieds-Server ACLs tragen - die
// Grundlage der Kennzeichnung „voll ACL-fähig" bei der Zuweisung.
func countACLCapable(group *domain.ServerGroup) {
	group.ACLServers = len(group.Servers)
	group.ACLCapable = 0
	for i := range group.Servers {
		if group.Servers[i].ACLUsable {
			group.ACLCapable++
		}
	}
}

// Disband löst eine Gruppe auf (Zuordnungen + Rules mit).
func (r *GroupRepository) Disband(id uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		group := domain.ServerGroup{ID: id}
		for _, assoc := range []string{"Servers", "Managers", "LinuxUsers"} {
			if err := tx.Model(&group).Association(assoc).Clear(); err != nil {
				return err
			}
		}
		if err := tx.Where("group_id = ?", id).Delete(&domain.Rule{}).Error; err != nil {
			return err
		}
		if err := tx.Where("group_id = ?", id).Delete(&domain.Schedule{}).Error; err != nil {
			return err
		}
		res := tx.Delete(&domain.ServerGroup{}, id)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// ---- Zuordnungen -------------------------------------------------------------

// AddServer fügt einen Server zur Gruppe hinzu. Der zusammengesetzte
// Primärschlüssel der Join-Tabelle (server_group_id, server_id) stellt
// sicher, dass ein Server pro Gruppe nur EINMAL vorkommt; ein bereits
// vorhandener Server wird hier daher übersprungen (kein Fehler, idempotent).
func (r *GroupRepository) AddServer(group *domain.ServerGroup, server *domain.Server) error {
	count := r.db.Model(group).Where("id = ?", server.ID).Association("Servers").Count()
	if count > 0 {
		return nil // bereits Mitglied
	}
	return r.db.Model(group).Association("Servers").Append(server)
}

func (r *GroupRepository) RemoveServer(group *domain.ServerGroup, server *domain.Server) error {
	return r.db.Model(group).Association("Servers").Delete(server)
}

// AddManager / RemoveManager verwalten, welche Benutzer eine Gruppe betreuen.
//
// Die Beziehung ServerGroup.Managers steuert die Mandantentrennung: für einen
// Benutzer der Manager-Rolle filtert ScopeManager jede Abfrage auf die
// Gruppen, in denen er als Manager eingetragen ist. Bislang wurde sie
// ausschließlich GELESEN - es gab keinen Weg, sie zu beschreiben, weshalb
// jeder Manager dauerhaft eine leere Server-Liste sah und die im README
// beworbene Rolle unbenutzbar war (BUG-018).
func (r *GroupRepository) AddManager(group *domain.ServerGroup, user *domain.User) error {
	count := r.db.Model(group).Where("id = ?", user.ID).Association("Managers").Count()
	if count > 0 {
		return nil // bereits Manager dieser Gruppe
	}
	return r.db.Model(group).Association("Managers").Append(user)
}

func (r *GroupRepository) RemoveManager(group *domain.ServerGroup, user *domain.User) error {
	return r.db.Model(group).Association("Managers").Delete(user)
}

// ---- Rules -------------------------------------------------------------------

func (r *GroupRepository) CreateRule(rule *domain.Rule) error {
	return r.db.Create(rule).Error
}

func (r *GroupRepository) UpdateRule(rule *domain.Rule) error {
	return r.db.Save(rule).Error
}

func (r *GroupRepository) DeleteRule(id uint) error {
	res := r.db.Delete(&domain.Rule{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// FindRule lädt eine Rule; der Scope wird über die Gruppe geprüft.
func (r *GroupRepository) FindRule(scope AccessScope, id uint) (*domain.Rule, error) {
	var rule domain.Rule
	q := r.db.Preload("Group")
	if !scope.All {
		q = q.Where("rules.group_id IN (SELECT server_group_id FROM server_group_managers WHERE user_id = ?)",
			scope.UserID)
	}
	if err := q.First(&rule, id).Error; err != nil {
		return nil, translate(err)
	}
	return &rule, nil
}

// FindAllRules liefert die Regeln ALLER sichtbaren Gruppen (inkl. Gruppe) -
// die globale Regel-Sicht. Vorher gab es Regeln nur je Gruppe; wer wissen
// wollte, was flottenweit definiert ist, musste jede Gruppe einzeln
// abklappern (R2-085). Manager sehen nur die Regeln ihrer Gruppen.
func (r *GroupRepository) FindAllRules(scope AccessScope) ([]domain.Rule, error) {
	var rules []domain.Rule
	q := r.db.Preload("Group")
	if !scope.All {
		q = q.Where("rules.group_id IN (SELECT server_group_id FROM server_group_managers WHERE user_id = ?)",
			scope.UserID)
	}
	if err := q.Order("group_id, id").Find(&rules).Error; err != nil {
		return nil, err
	}
	return rules, nil
}

func (r *GroupRepository) FindRulesForGroup(groupID uint) ([]domain.Rule, error) {
	var rules []domain.Rule
	if err := r.db.Where("group_id = ?", groupID).Order("id").Find(&rules).Error; err != nil {
		return nil, err
	}
	return rules, nil
}

// FindEnforceRulesForServer liefert die aktiven Grundsatz-Regeln, die für
// einen Server gelten: die seiner Gruppen plus die der System-Gruppe
// (deren Regeln gelten für ALLE Server).
//
// Die Gruppe wird mitgeladen: Über ihren Vorrang entscheidet sich, welche
// Regel sich durchsetzt, wenn zwei Gruppen dasselbe regeln (siehe
// resolveEnforceRules).
func (r *GroupRepository) FindEnforceRulesForServer(serverID uint) ([]domain.Rule, error) {
	var rules []domain.Rule
	err := r.db.Preload("Group").Where(`enforce = ? AND enabled = ? AND (
		group_id IN (SELECT server_group_id FROM server_group_servers WHERE server_id = ?)
		OR group_id IN (SELECT id FROM server_groups WHERE is_system = ?))`,
		true, true, serverID, true).
		Order("id").Find(&rules).Error
	if err != nil {
		return nil, err
	}
	return rules, nil
}

// ServersOfGroup liefert die Mitglieds-Server einer Gruppe.
func (r *GroupRepository) ServersOfGroup(groupID uint) ([]domain.Server, error) {
	var group domain.ServerGroup
	if err := r.db.Preload("Servers").First(&group, groupID).Error; err != nil {
		return nil, translate(err)
	}
	return group.Servers, nil
}

// GroupByID lädt eine Gruppe ohne Scope-Prüfung und ohne Preloads
// (interne Nutzung, z.B. Executor).
func (r *GroupRepository) GroupByID(id uint) (*domain.ServerGroup, error) {
	var group domain.ServerGroup
	if err := r.db.First(&group, id).Error; err != nil {
		return nil, translate(err)
	}
	return &group, nil
}

// ---- Schedules ---------------------------------------------------------------

func (r *GroupRepository) CreateSchedule(s *domain.Schedule) error {
	return r.db.Create(s).Error
}

func (r *GroupRepository) UpdateSchedule(s *domain.Schedule) error {
	return r.db.Omit("Rules").Save(s).Error
}

// DeleteSchedule löscht einen Schedule samt seiner Rules.
func (r *GroupRepository) DeleteSchedule(id uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("schedule_id = ?", id).Delete(&domain.Rule{}).Error; err != nil {
			return err
		}
		res := tx.Delete(&domain.Schedule{}, id)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// FindSchedule lädt einen Schedule samt Rules; der Scope wird über die
// Gruppe geprüft (Manager sehen nur Schedules ihrer Gruppen).
func (r *GroupRepository) FindSchedule(scope AccessScope, id uint) (*domain.Schedule, error) {
	var sched domain.Schedule
	q := r.db.Preload("Rules", func(db *gorm.DB) *gorm.DB { return db.Order("rules.id") })
	if !scope.All {
		q = q.Where("schedules.group_id IN (SELECT server_group_id FROM server_group_managers WHERE user_id = ?)",
			scope.UserID)
	}
	if err := q.First(&sched, id).Error; err != nil {
		return nil, translate(err)
	}
	return &sched, nil
}

func (r *GroupRepository) FindSchedulesForGroup(groupID uint) ([]domain.Schedule, error) {
	var schedules []domain.Schedule
	err := r.db.Preload("Rules", func(db *gorm.DB) *gorm.DB { return db.Order("rules.id") }).
		Where("group_id = ?", groupID).Order("id").Find(&schedules).Error
	if err != nil {
		return nil, err
	}
	return schedules, nil
}

// FindAllSchedules liefert alle Schedules samt Rules (Scheduler + Übersicht).
func (r *GroupRepository) FindAllSchedules() ([]domain.Schedule, error) {
	var schedules []domain.Schedule
	err := r.db.Preload("Rules", func(db *gorm.DB) *gorm.DB { return db.Order("rules.id") }).
		Order("id").Find(&schedules).Error
	if err != nil {
		return nil, err
	}
	return schedules, nil
}
