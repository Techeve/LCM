package controllers

import (
	"errors"

	"github.com/gofiber/fiber/v3"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
)

// SubscriptionController bedient die Enterprise-Subscription dieser
// Installation: Status, Aktivierung, manuelles Lebenszeichen und die
// apt-Kanal-Umstellung des LCM-Hosts. Alles unter settings:manage - die
// Subscription ist Betreiber-Sache.
type SubscriptionController struct {
	subs *services.SubscriptionService
}

func NewSubscriptionController(subs *services.SubscriptionService) *SubscriptionController {
	return &SubscriptionController{subs: subs}
}

// Status - GET /api/v1/subscription (settings:manage)
func (ctrl *SubscriptionController) Status(c fiber.Ctx) error {
	view, err := ctrl.subs.Status()
	if err != nil {
		return err
	}
	return c.JSON(view)
}

// Activate - POST /api/v1/subscription/activate (settings:manage)
// Tauscht den Subscription-Key gegen den instanzgebundenen Zugangsschlüssel.
// Fehlermeldungen des Dienstes (an andere Instanz gebunden, abgelaufen,
// widerrufen) gehen als 400 mit Klartext an die Oberfläche.
func (ctrl *SubscriptionController) Activate(c fiber.Ctx) error {
	var in struct {
		SubscriptionKey string `json:"subscription_key"`
		ServiceURL      string `json:"service_url"`
	}
	if err := c.Bind().Body(&in); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	// Optional zuerst die Dienst-Adresse übernehmen (ein Formular, ein Klick).
	if in.ServiceURL != "" {
		if err := ctrl.subs.SetServiceURL(in.ServiceURL, actor(c)); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
	}
	if _, err := ctrl.subs.Activate(in.SubscriptionKey, actor(c)); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	view, err := ctrl.subs.Status()
	if err != nil {
		return err
	}
	return c.JSON(view)
}

// Verify - POST /api/v1/subscription/verify (settings:manage)
// Manuelles „Jetzt prüfen": Lebenszeichen an den Dienst, Status zurück.
func (ctrl *SubscriptionController) Verify(c fiber.Ctx) error {
	if _, err := ctrl.subs.Verify(actor(c)); err != nil {
		if errors.Is(err, services.ErrNoSubscription) {
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
		return err
	}
	view, err := ctrl.subs.Status()
	if err != nil {
		return err
	}
	return c.JSON(view)
}

// Remove - DELETE /api/v1/subscription (settings:manage)
// Entfernt die Subscription lokal; die Bindung beim Anbieter bleibt, bis
// er sie freigibt.
func (ctrl *SubscriptionController) Remove(c fiber.Ctx) error {
	if err := ctrl.subs.Remove(actor(c)); err != nil {
		return err
	}
	view, err := ctrl.subs.Status()
	if err != nil {
		return err
	}
	return c.JSON(view)
}

// SetAptChannel - POST /api/v1/subscription/apt (settings:manage)
// Stellt den LCM-Host per Job auf einen Paketkanal um (channel=community,
// beta oder enterprise). Antwortet mit dem Job - die Oberfläche verfolgt ihn
// und meldet Erfolg erst nach Job-Ende.
//
// enable ist der Vorgänger dieses Aufrufs (true = Enterprise) und bleibt für
// ältere Clients gültig, solange kein channel mitkommt.
func (ctrl *SubscriptionController) SetAptChannel(c fiber.Ctx) error {
	var in struct {
		Channel string `json:"channel"`
		Enable  bool   `json:"enable"`
	}
	if err := c.Bind().Body(&in); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	channel := in.Channel
	if channel == "" {
		channel = domain.AptChannelCommunity
		if in.Enable {
			channel = domain.AptChannelEnterprise
		}
	}
	job, err := ctrl.subs.SetAptChannel(channel, actor(c))
	if err != nil {
		switch {
		case errors.Is(err, services.ErrNoSubscription),
			errors.Is(err, services.ErrUnknownAptChannel),
			errors.Is(err, services.ErrAptSwitchUnavailable):
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		case errors.Is(err, services.ErrServerBusy):
			return fiber.NewError(fiber.StatusConflict, err.Error())
		}
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return c.Status(fiber.StatusAccepted).JSON(job)
}
