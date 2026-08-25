package controllers

import (
	"errors"

	"github.com/gofiber/fiber/v3"

	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

// CustomActionController bedient die benutzerdefinierten Aktionen
// (wiederverwendbare Command-Listen für Gruppen-Rules).
type CustomActionController struct {
	actions *services.CustomActionService
}

func NewCustomActionController(actions *services.CustomActionService) *CustomActionController {
	return &CustomActionController{actions: actions}
}

func mapCustomActionError(err error) error {
	switch {
	case errors.Is(err, repositories.ErrNotFound):
		return fiber.NewError(fiber.StatusNotFound, "custom-aktion nicht gefunden")
	case errors.Is(err, services.ErrCustomActionNameRequired), errors.Is(err, services.ErrCustomActionEmpty):
		return fiber.NewError(fiber.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, services.ErrCustomActionInUse):
		return fiber.NewError(fiber.StatusConflict, err.Error())
	default:
		return err
	}
}

// List - GET /api/v1/custom-actions (rules:manage)
func (ctrl *CustomActionController) List(c fiber.Ctx) error {
	actions, err := ctrl.actions.List()
	if err != nil {
		return err
	}
	return c.JSON(actions)
}

type customActionRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Commands    string `json:"commands"`
}

// Create - POST /api/v1/custom-actions (rules:manage)
func (ctrl *CustomActionController) Create(c fiber.Ctx) error {
	var req customActionRequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	action, err := ctrl.actions.Create(req.Name, req.Description, req.Commands, actor(c))
	if err != nil {
		return mapCustomActionError(err)
	}
	return c.Status(fiber.StatusCreated).JSON(action)
}

// Update - PATCH /api/v1/custom-actions/:id (rules:manage)
func (ctrl *CustomActionController) Update(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	var req customActionRequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	action, err := ctrl.actions.Update(id, req.Name, req.Description, req.Commands, actor(c))
	if err != nil {
		return mapCustomActionError(err)
	}
	return c.JSON(action)
}

// Delete - DELETE /api/v1/custom-actions/:id (rules:manage)
func (ctrl *CustomActionController) Delete(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	if err := ctrl.actions.Delete(id, actor(c)); err != nil {
		return mapCustomActionError(err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}
