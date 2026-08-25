package controllers

import (
	"github.com/gofiber/fiber/v3"

	"LCM/internal/api/middlewares"
	"LCM/internal/core/services"
)

// SystemController ist das Referenzbeispiel für Controller ohne
// Datenbank-Anbindung: Er delegiert an einen reinen Logik-Service
// (SystemService) - keine Repository-Schicht beteiligt.
type SystemController struct {
	system *services.SystemService
}

func NewSystemController(system *services.SystemService) *SystemController {
	return &SystemController{system: system}
}

// Info - GET /api/v1/system/info (öffentlich)
// Liefert Version, Build-Nummer und Laufzeitdaten der Anwendung.
func (ctrl *SystemController) Info(c fiber.Ctx) error {
	info := ctrl.system.Info()
	// Anonyme Aufrufer bekommen NUR Version und Build - das braucht der
	// Footer auf der Anmeldeseite. Commit-Hash, Dirty-Flag, Go-Version,
	// Plattform, Laufzeit und Agent-Port bleiben angemeldeten Nutzern
	// vorbehalten: unangemeldet wären sie ein präzises Fingerprinting für
	// die Auswahl passender Exploits, und der Agent-Port verriete zusätzlich
	// die zweite Angriffsfläche.
	if middlewares.CurrentUser(c) == nil {
		return c.JSON(services.SystemInfo{Version: info.Version, Build: info.Build})
	}
	return c.JSON(info)
}
