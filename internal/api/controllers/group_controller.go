package controllers

import (
	"errors"

	"github.com/gofiber/fiber/v3"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

// GroupController bedient Servergruppen, deren Rules und die Schedules.
type GroupController struct {
	groups    *services.GroupService
	scheduler *services.Scheduler
}

func NewGroupController(groups *services.GroupService, scheduler *services.Scheduler) *GroupController {
	return &GroupController{groups: groups, scheduler: scheduler}
}

func mapGroupError(err error) error {
	switch {
	case errors.Is(err, repositories.ErrNotFound):
		return fiber.NewError(fiber.StatusNotFound, "nicht gefunden")
	case errors.Is(err, services.ErrProtectedGroup), errors.Is(err, services.ErrProtectedRule),
		errors.Is(err, services.ErrProtectedSchedule):
		return fiber.NewError(fiber.StatusForbidden, err.Error())
	case errors.Is(err, services.ErrInvalidCron), errors.Is(err, services.ErrInvalidRuleType),
		errors.Is(err, services.ErrRuleCommandUnused),
		errors.Is(err, services.ErrEnforceRuleType), errors.Is(err, services.ErrRuleNeedsTarget),
		errors.Is(err, services.ErrEnforceOnlyRuleType),
		errors.Is(err, services.ErrEnforceFirewallSpec),
		errors.Is(err, services.ErrInvalidGroupPriority),
		errors.Is(err, services.ErrNoPackages), errors.Is(err, services.ErrInvalidPackage),
		errors.Is(err, services.ErrCustomActionNotSelected):
		return fiber.NewError(fiber.StatusUnprocessableEntity, err.Error())
	default:
		return err
	}
}

// List - GET /api/v1/server-groups (groups:read)
func (ctrl *GroupController) List(c fiber.Ctx) error {
	groups, err := ctrl.groups.List(scopeFor(c))
	if err != nil {
		return err
	}
	return c.JSON(groups)
}

// Get - GET /api/v1/server-groups/:id (groups:read)
func (ctrl *GroupController) Get(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	group, err := ctrl.groups.Get(scopeFor(c), id)
	if err != nil {
		return mapGroupError(err)
	}
	return c.JSON(group)
}

type groupRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// Priority ist optional: fehlt das Feld, bleibt der Vorrang unverändert
	// (beim Anlegen: Standardwert).
	Priority *int `json:"priority"`
}

// Create - POST /api/v1/server-groups/create (groups:write)
func (ctrl *GroupController) Create(c fiber.Ctx) error {
	var req groupRequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	if req.Name == "" {
		return fiber.NewError(fiber.StatusBadRequest, "name ist erforderlich")
	}
	group, err := ctrl.groups.Create(req.Name, req.Description, req.Priority, actor(c))
	if err != nil {
		return mapGroupError(err)
	}
	return c.Status(fiber.StatusCreated).JSON(group)
}

// UpdateSettings - PATCH /api/v1/server-groups/:id/settings (groups:write)
func (ctrl *GroupController) UpdateSettings(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	var req groupRequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	group, err := ctrl.groups.UpdateSettings(scopeFor(c), id, req.Name, req.Description, req.Priority, actor(c))
	if err != nil {
		return mapGroupError(err)
	}
	return c.JSON(group)
}

// Disband - POST /api/v1/server-groups/:id/disband (groups:write)
func (ctrl *GroupController) Disband(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	if err := ctrl.groups.Disband(scopeFor(c), id, actor(c)); err != nil {
		return mapGroupError(err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

type serverRef struct {
	ServerID uint `json:"server_id"`
}

// AssignServer - POST /api/v1/server-groups/:id/assign-server (groups:write)
func (ctrl *GroupController) AssignServer(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	var req serverRef
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	if err := ctrl.groups.AssignServer(scopeFor(c), id, req.ServerID, actor(c)); err != nil {
		return mapGroupError(err)
	}
	return c.JSON(fiber.Map{"status": "assigned"})
}

// RemoveServer - POST /api/v1/server-groups/:id/remove-server (groups:write)
func (ctrl *GroupController) RemoveServer(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	var req serverRef
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	if err := ctrl.groups.RemoveServer(scopeFor(c), id, req.ServerID, actor(c)); err != nil {
		return mapGroupError(err)
	}
	return c.JSON(fiber.Map{"status": "removed"})
}

type userRef struct {
	UserID uint `json:"user_id"`
}

// AssignManager - POST /api/v1/server-groups/:id/assign-manager (groups:write)
//
// Trägt einen Benutzer als Betreuer der Gruppe ein. Erst dadurch sieht ein
// Benutzer der Manager-Rolle überhaupt Server: ScopeManager filtert jede
// Abfrage auf die Gruppen, in denen er eingetragen ist.
func (ctrl *GroupController) AssignManager(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	var req userRef
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	if err := ctrl.groups.AssignManager(scopeFor(c), id, req.UserID, actor(c)); err != nil {
		return mapGroupError(err)
	}
	return c.JSON(fiber.Map{"status": "assigned"})
}

// RemoveManager - POST /api/v1/server-groups/:id/remove-manager (groups:write)
func (ctrl *GroupController) RemoveManager(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	var req userRef
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	if err := ctrl.groups.RemoveManager(scopeFor(c), id, req.UserID, actor(c)); err != nil {
		return mapGroupError(err)
	}
	return c.JSON(fiber.Map{"status": "removed"})
}

// ---- Schedules ---------------------------------------------------------------

// ListSchedules - GET /api/v1/server-groups/:id/schedules (rules:manage)
func (ctrl *GroupController) ListSchedules(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	schedules, err := ctrl.groups.ListSchedules(scopeFor(c), id)
	if err != nil {
		return mapGroupError(err)
	}
	return c.JSON(schedules)
}

type scheduleRequest struct {
	Name     string `json:"name"`
	CronExpr string `json:"cron_expr"`
}

// DefineSchedule - POST /api/v1/server-groups/:id/schedules/define (rules:manage)
func (ctrl *GroupController) DefineSchedule(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	var req scheduleRequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	if req.Name == "" {
		return fiber.NewError(fiber.StatusBadRequest, "name ist erforderlich")
	}
	sched, err := ctrl.groups.DefineSchedule(scopeFor(c), id, req.Name, req.CronExpr, actor(c))
	if err != nil {
		return mapGroupError(err)
	}
	return c.Status(fiber.StatusCreated).JSON(sched)
}

// UpdateSchedule - PATCH /api/v1/schedules/:id/update (rules:manage)
func (ctrl *GroupController) UpdateSchedule(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	var req scheduleRequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	sched, err := ctrl.groups.UpdateSchedule(scopeFor(c), id, req.Name, req.CronExpr, actor(c))
	if err != nil {
		return mapGroupError(err)
	}
	return c.JSON(sched)
}

// RemoveSchedule - DELETE /api/v1/schedules/:id/remove (rules:manage)
func (ctrl *GroupController) RemoveSchedule(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	if err := ctrl.groups.RemoveSchedule(scopeFor(c), id, actor(c)); err != nil {
		return mapGroupError(err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// EnableSchedule - POST /api/v1/schedules/:id/enable (rules:manage)
func (ctrl *GroupController) EnableSchedule(c fiber.Ctx) error {
	return ctrl.setScheduleEnabled(c, true)
}

// DisableSchedule - POST /api/v1/schedules/:id/disable (rules:manage)
func (ctrl *GroupController) DisableSchedule(c fiber.Ctx) error {
	return ctrl.setScheduleEnabled(c, false)
}

func (ctrl *GroupController) setScheduleEnabled(c fiber.Ctx, enabled bool) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	if err := ctrl.groups.SetScheduleEnabled(scopeFor(c), id, enabled, actor(c)); err != nil {
		return mapGroupError(err)
	}
	return c.JSON(fiber.Map{"enabled": enabled})
}

// TriggerSchedule - POST /api/v1/schedules/:id/trigger-now (rules:manage)
// Führt alle Rules des Schedules sofort aus.
func (ctrl *GroupController) TriggerSchedule(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	sched, err := ctrl.groups.FindSchedule(scopeFor(c), id)
	if err != nil {
		return mapGroupError(err)
	}
	ctrl.scheduler.TriggerScheduleNow(sched, actor(c))
	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{"status": "triggered", "schedule": sched.Name})
}

// ---- Rules -------------------------------------------------------------------

// ListRules - GET /api/v1/server-groups/:id/rules (rules:manage)
func (ctrl *GroupController) ListRules(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	rules, err := ctrl.groups.ListRules(scopeFor(c), id)
	if err != nil {
		return mapGroupError(err)
	}
	return c.JSON(rules)
}

type defineRuleRequest struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Command    string `json:"command"`
	ScheduleID *uint  `json:"schedule_id"`
	Enforce    bool   `json:"enforce"`
}

// DefineRule - POST /api/v1/server-groups/:id/rules/define (rules:manage)
func (ctrl *GroupController) DefineRule(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	var req defineRuleRequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	rule, err := ctrl.groups.DefineRule(scopeFor(c), id, req.Name, req.Type, req.Command, req.ScheduleID, req.Enforce, actor(c))
	if err != nil {
		return mapGroupError(err)
	}
	return c.Status(fiber.StatusCreated).JSON(rule)
}

// ListAllRules - GET /api/v1/rules (rules:manage)
// Globale Regel-Sicht über alle sichtbaren Gruppen (R2-085) - vorher gab es
// Regeln nur je Gruppe, eine Flotten-Übersicht erforderte das Abklappern
// jeder einzelnen Gruppe. Manager sehen nur die Regeln ihrer Gruppen.
func (ctrl *GroupController) ListAllRules(c fiber.Ctx) error {
	rules, err := ctrl.groups.ListAllRules(scopeFor(c))
	if err != nil {
		return err
	}
	// Der Gruppen-Kontext ist der Sinn dieser Sicht - domain.Rule serialisiert
	// die Gruppe selbst nicht (json:"-"), deshalb hier als group_name daneben.
	type ruleWithGroup struct {
		domain.Rule
		GroupName string `json:"group_name"`
	}
	out := make([]ruleWithGroup, 0, len(rules))
	for _, r := range rules {
		name := ""
		if r.Group != nil {
			name = r.Group.Name
		}
		out = append(out, ruleWithGroup{Rule: r, GroupName: name})
	}
	return c.JSON(out)
}

// UpdateRule - PATCH /api/v1/rules/:id/update (rules:manage)
func (ctrl *GroupController) UpdateRule(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	var req defineRuleRequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	rule, err := ctrl.groups.UpdateRule(scopeFor(c), id, req.Name, req.Command, actor(c))
	if err != nil {
		return mapGroupError(err)
	}
	return c.JSON(rule)
}

// RemoveRule - DELETE /api/v1/rules/:id/remove (rules:manage)
func (ctrl *GroupController) RemoveRule(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	if err := ctrl.groups.RemoveRule(scopeFor(c), id, actor(c)); err != nil {
		return mapGroupError(err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// EnableRule - POST /api/v1/rules/:id/enable (rules:manage)
func (ctrl *GroupController) EnableRule(c fiber.Ctx) error {
	return ctrl.setRuleEnabled(c, true)
}

// DisableRule - POST /api/v1/rules/:id/disable (rules:manage)
func (ctrl *GroupController) DisableRule(c fiber.Ctx) error {
	return ctrl.setRuleEnabled(c, false)
}

func (ctrl *GroupController) setRuleEnabled(c fiber.Ctx, enabled bool) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	if err := ctrl.groups.SetRuleEnabled(scopeFor(c), id, enabled, actor(c)); err != nil {
		return mapGroupError(err)
	}
	return c.JSON(fiber.Map{"enabled": enabled})
}

// TriggerRule - POST /api/v1/rules/:id/trigger-now (rules:manage)
func (ctrl *GroupController) TriggerRule(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	rule, err := ctrl.groups.FindRule(scopeFor(c), id)
	if err != nil {
		return mapGroupError(err)
	}
	ctrl.scheduler.TriggerNow(rule, actor(c))
	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{"status": "triggered", "rule": rule.Name})
}
