package controllers

import (
	"github.com/gofiber/fiber/v3"

	"LCM/internal/core/services"
)

// AgentUpdateController bedient die Agent-Update-Regel einer Servergruppe.
type AgentUpdateController struct {
	groups  *services.GroupService
	updates *services.AgentUpdateService
}

func NewAgentUpdateController(groups *services.GroupService, updates *services.AgentUpdateService) *AgentUpdateController {
	return &AgentUpdateController{groups: groups, updates: updates}
}

// visibleGroup liest die Gruppen-ID und prüft, dass der Aufrufer die Gruppe sieht.
func (ctrl *AgentUpdateController) visibleGroup(c fiber.Ctx) (uint, error) {
	id, err := paramID(c)
	if err != nil {
		return 0, err
	}
	if _, err := ctrl.groups.Get(scopeFor(c), id); err != nil {
		return 0, mapGroupError(err)
	}
	return id, nil
}

// Get - GET /api/v1/server-groups/:id/agent-updates (groups:read)
// Die Agent-Update-Regel der Gruppe samt nächstem Fenster, Zahl der Agent-Server
// und Stand des letzten Laufs.
func (ctrl *AgentUpdateController) Get(c fiber.Ctx) error {
	id, err := ctrl.visibleGroup(c)
	if err != nil {
		return err
	}
	view, err := ctrl.updates.Get(id)
	if err != nil {
		return err
	}
	return c.JSON(view)
}

// Save - PUT /api/v1/server-groups/:id/agent-updates (rules:manage)
// Body: {mode: "off"|"auto", staged, cron_expr, window_minutes}. Eine neue
// Einstellung hebt eine Sperre nach einem gescheiterten Lauf auf.
func (ctrl *AgentUpdateController) Save(c fiber.Ctx) error {
	id, err := ctrl.visibleGroup(c)
	if err != nil {
		return err
	}
	var req services.AgentUpdateInput
	if err := bindBody(c, &req); err != nil {
		return err
	}
	if err := checkLine("cron_expr", req.CronExpr, maxNameLen); err != nil {
		return err
	}
	view, err := ctrl.updates.Save(id, req, actor(c))
	if err != nil {
		return mapGroupError(err)
	}
	return c.JSON(view)
}

// Run - POST /api/v1/server-groups/:id/agent-updates/run (rules:manage)
// Startet einen Lauf jetzt, unabhängig vom Fenster und davon, ob die Regel an ist.
func (ctrl *AgentUpdateController) Run(c fiber.Ctx) error {
	id, err := ctrl.visibleGroup(c)
	if err != nil {
		return err
	}
	if err := ctrl.updates.RunNow(id, actor(c)); err != nil {
		return mapGroupError(err)
	}
	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{"status": "started"})
}

// Resume - POST /api/v1/server-groups/:id/agent-updates/resume (rules:manage)
// Hebt die Sperre auf, die ein gescheiterter erster Server oder ein Rollback
// gesetzt hat; der nächste Lauf versucht es wieder.
func (ctrl *AgentUpdateController) Resume(c fiber.Ctx) error {
	id, err := ctrl.visibleGroup(c)
	if err != nil {
		return err
	}
	view, err := ctrl.updates.Resume(id, actor(c))
	if err != nil {
		return mapGroupError(err)
	}
	return c.JSON(view)
}
