package controllers

import (
	"github.com/gofiber/fiber/v3"

	"LCM/internal/appdocs"
)

// DocsController liefert die mitgelieferte Anwender-Doku aus (siehe Paket
// appdocs). Bewusst OHNE Anmeldung erreichbar: Die Anleitung zum Einrichten
// des SSH-Schlüssels wird genau dann gebraucht, wenn man noch keinen Zugang
// hat - etwa aus der Aktivierungs-Mail heraus. Die Seiten sind ins Binary
// eingebettet und enthalten nichts Vertrauliches.
type DocsController struct{}

func NewDocsController() *DocsController { return &DocsController{} }

// List - GET /api/v1/docs?lang=de (öffentlich)
func (ctrl *DocsController) List(c fiber.Ctx) error {
	pages, err := appdocs.List(c.Query("lang"))
	if err != nil {
		return err
	}
	return c.JSON(pages)
}

// Get - GET /api/v1/docs/:slug?lang=de (öffentlich)
func (ctrl *DocsController) Get(c fiber.Ctx) error {
	page, err := appdocs.Get(c.Query("lang"), c.Params("slug"))
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "seite nicht gefunden")
	}
	return c.JSON(page)
}
