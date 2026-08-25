package controllers

import (
	"encoding/json"
	"errors"

	"github.com/gofiber/fiber/v3"

	"LCM/internal/core/services"
	"LCM/internal/infrastructure/notify"
	"LCM/internal/storage/repositories"
)

// NotificationController bedient die Benachrichtigungskanäle
// (Provider-Instanzen des generischen Notification-Service).
type NotificationController struct {
	channels *services.NotificationService
}

func NewNotificationController(channels *services.NotificationService) *NotificationController {
	return &NotificationController{channels: channels}
}

func mapNotificationError(err error) error {
	switch {
	case errors.Is(err, repositories.ErrNotFound):
		return fiber.NewError(fiber.StatusNotFound, "benachrichtigungskanal nicht gefunden")
	case errors.Is(err, services.ErrChannelNameRequired),
		errors.Is(err, services.ErrChannelTypeInvalid),
		errors.Is(err, services.ErrChannelConfigInvalid),
		errors.Is(err, notify.ErrUnknownChannelType):
		return fiber.NewError(fiber.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, services.ErrChannelInUse),
		errors.Is(err, services.ErrChannelManaged):
		return fiber.NewError(fiber.StatusConflict, err.Error())
	default:
		return err
	}
}

// GetSystemChannel - GET /api/v1/settings/mail-channel (settings:manage)
// Zustand der Checkbox „Standard-E-Mail-Versand als Benachrichtigungskanal".
func (ctrl *NotificationController) GetSystemChannel(c fiber.Ctx) error {
	enabled, err := ctrl.channels.SystemChannelEnabled()
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"enabled": enabled})
}

// SetSystemChannel - PUT /api/v1/settings/mail-channel (settings:manage)
// Schaltet den verwalteten System-E-Mail-Kanal (Anlegen beim ersten
// Aktivieren; Abschalten deaktiviert nur, Alarmregeln bleiben intakt).
func (ctrl *NotificationController) SetSystemChannel(c fiber.Ctx) error {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	if err := ctrl.channels.EnsureSystemChannel(req.Enabled, actor(c)); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"enabled": req.Enabled})
}

// channelRequest ist der generische Request eines Kanals: Config ist die
// provider-spezifische, nicht-sensible Konfiguration als JSON-Objekt.
type channelRequest struct {
	Name    string          `json:"name"`
	Type    string          `json:"type"`
	Enabled bool            `json:"enabled"`
	Config  json.RawMessage `json:"config"`
	Secret  string          `json:"secret"`
}

func (r *channelRequest) toInput() services.ChannelInput {
	config := ""
	if len(r.Config) > 0 {
		config = string(r.Config)
	}
	return services.ChannelInput{
		Name: r.Name, Type: r.Type, Enabled: r.Enabled,
		Config: config, Secret: r.Secret,
	}
}

// List - GET /api/v1/notification-channels (notifications:manage)
func (ctrl *NotificationController) List(c fiber.Ctx) error {
	channels, err := ctrl.channels.List()
	if err != nil {
		return err
	}
	return c.JSON(channels)
}

// Create - POST /api/v1/notification-channels (notifications:manage)
func (ctrl *NotificationController) Create(c fiber.Ctx) error {
	var req channelRequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	channel, err := ctrl.channels.Create(req.toInput(), actor(c))
	if err != nil {
		return mapNotificationError(err)
	}
	return c.Status(fiber.StatusCreated).JSON(channel)
}

// Update - PATCH /api/v1/notification-channels/:id (notifications:manage)
func (ctrl *NotificationController) Update(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	var req channelRequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	channel, err := ctrl.channels.Update(id, req.toInput(), actor(c))
	if err != nil {
		return mapNotificationError(err)
	}
	return c.JSON(channel)
}

// Delete - DELETE /api/v1/notification-channels/:id (notifications:manage)
func (ctrl *NotificationController) Delete(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	if err := ctrl.channels.Delete(id, actor(c)); err != nil {
		return mapNotificationError(err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// Test - POST /api/v1/notification-channels/:id/test (notifications:manage)
func (ctrl *NotificationController) Test(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	if err := ctrl.channels.Test(id, actor(c)); err != nil {
		if mapped := mapNotificationError(err); mapped != err {
			return mapped
		}
		return fiber.NewError(fiber.StatusUnprocessableEntity, err.Error())
	}
	return c.JSON(fiber.Map{"status": "sent"})
}
