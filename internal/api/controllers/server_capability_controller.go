package controllers

import (
	"github.com/gofiber/fiber/v3"

	"LCM/internal/core/domain"
)

// capabilityResponse ist eine Kategorie der Betriebsfähigkeit, wie die
// Oberfläche sie braucht: die Zeile selbst plus das, was sich erst aus Zustand,
// Server und Einstellungen ergibt. Die abgeleiteten Felder stehen hier und nicht
// am Domänentyp, der nur gespeichert wird.
type capabilityResponse struct {
	domain.ServerCapability
	Effective string `json:"effective"` // "critical", "warning" oder "" (in Ordnung)
	Stale     string `json:"stale"`     // nur packages: Stufe des Paketstand-Stillstands, sonst ""
	Repair    string `json:"repair"`    // "dpkg_configure", "fix_deps" oder ""
	Retryable bool   `json:"retryable"`
}

// Capabilities - GET /api/v1/servers/:id/capabilities (servers:read)
// Immer alle vier Kategorien in Anzeigereihenfolge, auch ohne Verlauf.
func (ctrl *ServerController) Capabilities(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	caps, err := ctrl.servers.Capabilities(scopeFor(c), id)
	if err != nil {
		return mapServerError(err)
	}
	out := make([]capabilityResponse, 0, len(caps.Rows))
	for i := range caps.Rows {
		row := &caps.Rows[i]
		actions := ctrl.servers.CapabilityActions(caps.Server, row)
		r := capabilityResponse{
			ServerCapability: *row,
			Effective:        row.Effective(),
			Repair:           actions.Repair,
			Retryable:        actions.Retryable,
		}
		if row.Category == domain.CapabilityPackages {
			r.Stale = caps.Stale
		}
		out = append(out, r)
	}
	return c.JSON(out)
}

// RepairPackages - POST /api/v1/servers/:id/packages/repair (servers:write)
// Body {"action": "dpkg_configure" | "fix_deps"}. Startet die Reparatur als Job.
func (ctrl *ServerController) RepairPackages(c fiber.Ctx) error {
	var req struct {
		Action string `json:"action"`
	}
	if err := bindBody(c, &req); err != nil {
		return err
	}
	return ctrl.startPackageJob(c, func(id uint) (*domain.Job, error) {
		return ctrl.servers.RepairPackages(scopeFor(c), id, req.Action, actor(c))
	})
}

// RetryCapability - POST /api/v1/servers/:id/capabilities/:category/retry (servers:write)
// Löst die fehlgeschlagene Aktion der Kategorie erneut aus.
func (ctrl *ServerController) RetryCapability(c fiber.Ctx) error {
	category := c.Params("category")
	return ctrl.startPackageJob(c, func(id uint) (*domain.Job, error) {
		return ctrl.servers.RetryCapability(scopeFor(c), id, category, actor(c))
	})
}
