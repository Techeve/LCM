package controllers

import (
	"errors"

	"github.com/gofiber/fiber/v3"

	"LCM/internal/api/apierr"
	"LCM/internal/core/services"
)

var errPortScanNoTarget = apierr.Define(409, "server.portscan_no_target", "this server has no address LCM can scan")

// PortScanController: Portscan eines Servers von LCM aus.
type PortScanController struct {
	scans *services.PortScanService
}

func NewPortScanController(scans *services.PortScanService) *PortScanController {
	return &PortScanController{scans: scans}
}

// Get - GET /api/v1/servers/:id/port-scan (servers:read)
// Letzter Lauf samt Bewertung; noch nie gescannt = scanned_at null.
func (ctrl *PortScanController) Get(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	view, err := ctrl.scans.Latest(scopeFor(c), id)
	if err != nil {
		return mapServerError(err)
	}
	return c.JSON(view)
}

// Start - POST /api/v1/servers/:id/port-scan (servers:write)
// Startet den Scan als Job. Der Rumpf wird nicht gelesen: Das Ziel ist
// immer die gespeicherte Adresse des Servers, nie eine aus der Anfrage.
func (ctrl *PortScanController) Start(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	job, err := ctrl.scans.Start(scopeFor(c), id, actor(c))
	if errors.Is(err, services.ErrPortScanNoTarget) {
		return errPortScanNoTarget
	}
	if err != nil {
		return mapServerError(err)
	}
	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{
		"status": "started", "job_id": job.ID, "job_name": job.Name,
	})
}
