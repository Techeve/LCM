package controllers

import (
	"github.com/gofiber/fiber/v3"

	"LCM/internal/api/middlewares"
	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// scopeFor leitet die Daten-Sichtbarkeit aus dem authentifizierten User
// ab (Tenant Isolation): Admins sehen alles, alle anderen nur die Server
// und Gruppen ihrer zugewiesenen Servergruppen.
func scopeFor(c fiber.Ctx) repositories.AccessScope {
	user := middlewares.CurrentUser(c)
	if user == nil {
		return repositories.ScopeManager(0) // sieht nichts
	}
	for _, r := range user.RoleNames() {
		if r == domain.RoleAdmin {
			return repositories.ScopeAll()
		}
	}
	return repositories.ScopeManager(user.ID)
}

// actor liefert den Usernamen für Audit-/Job-Protokolle.
func actor(c fiber.Ctx) string {
	if u := middlewares.CurrentUser(c); u != nil {
		return u.Username
	}
	return "unbekannt"
}
