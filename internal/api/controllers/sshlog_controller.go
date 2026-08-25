package controllers

import (
	"errors"

	"github.com/gofiber/fiber/v3"

	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

// SSHLogController liefert die SSH-Protokolle: pro Server, pro Job und die
// Einzelsession mit allen Kommandos.
type SSHLogController struct {
	logs *services.SSHLogService
}

func NewSSHLogController(logs *services.SSHLogService) *SSHLogController {
	return &SSHLogController{logs: logs}
}

func mapLogError(err error) error {
	switch {
	case errors.Is(err, repositories.ErrNotFound):
		return fiber.NewError(fiber.StatusNotFound, "nicht gefunden")
	case errors.Is(err, services.ErrLogForbidden):
		return fiber.NewError(fiber.StatusForbidden, err.Error())
	default:
		return err
	}
}

// ServerSessions - GET /api/v1/servers/:id/ssh-sessions (jobs:read)
func (ctrl *SSHLogController) ServerSessions(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	limit := fiber.Query[int](c, "limit")
	sessions, err := ctrl.logs.ServerSessions(scopeFor(c), id, limit)
	if err != nil {
		return mapLogError(err)
	}
	return c.JSON(sessions)
}

// JobSessions - GET /api/v1/jobs/:id/ssh-sessions (jobs:read)
// Alle Verbindungen (inkl. Kommandos) einer Job-Ausführung.
func (ctrl *SSHLogController) JobSessions(c fiber.Ctx) error {
	id, err := paramStrID(c)
	if err != nil {
		return err
	}
	sessions, err := ctrl.logs.JobSessions(scopeFor(c), id)
	if err != nil {
		return mapLogError(err)
	}
	return c.JSON(sessions)
}

// Session - GET /api/v1/ssh-sessions/:id (jobs:read)
// Eine einzelne Session mit allen Kommandos.
func (ctrl *SSHLogController) Session(c fiber.Ctx) error {
	id, err := paramStrID(c)
	if err != nil {
		return err
	}
	session, err := ctrl.logs.Session(scopeFor(c), id)
	if err != nil {
		return mapLogError(err)
	}
	return c.JSON(session)
}
