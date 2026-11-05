package controllers

import (
	"github.com/gofiber/fiber/v3"

	"LCM/internal/api/middlewares"
	"LCM/internal/core/domain"
	"LCM/internal/core/services"
)

// DashboardController liefert die Startseite in einem Aufruf.
type DashboardController struct {
	dashboard *services.DashboardService
}

func NewDashboardController(dashboard *services.DashboardService) *DashboardController {
	return &DashboardController{dashboard: dashboard}
}

// Get - GET /api/v1/dashboard (servers:read)
// Kacheln, Server mit Handlungsbedarf, letzte Jobs, nächste Läufe und letzte
// Alarme - alles im Sichtbereich des Aufrufers, kurz zwischengespeichert. Den
// Alarm-Verlauf gibt es nur mit dem Recht alerts:manage, wie unter /alert-events.
func (ctrl *DashboardController) Get(c fiber.Ctx) error {
	withAlerts := middlewares.CurrentUser(c).HasPermission(domain.PermAlertsManage)
	dash, err := ctrl.dashboard.Get(scopeFor(c), withAlerts)
	if err != nil {
		return err
	}
	return c.JSON(dash)
}
