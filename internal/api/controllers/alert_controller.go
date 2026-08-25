package controllers

import (
	"LCM/internal/core/domain"
	"errors"
	"strconv"

	"github.com/gofiber/fiber/v3"

	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

// AlertController bedient die Alarm-Regeln und die Alarm-Historie sowie das
// manuelle Auslösen einer Auswertung.
type AlertController struct {
	alerts *services.AlertService
}

func NewAlertController(alerts *services.AlertService) *AlertController {
	return &AlertController{alerts: alerts}
}

func mapAlertError(err error) error {
	switch {
	case errors.Is(err, repositories.ErrNotFound):
		return fiber.NewError(fiber.StatusNotFound, "alarmregel nicht gefunden")
	case errors.Is(err, services.ErrAlertRuleNameRequired),
		errors.Is(err, services.ErrAlertRuleTypeInvalid),
		errors.Is(err, services.ErrAlertSeverityInvalid),
		errors.Is(err, services.ErrAlertGroupUnknown):
		return fiber.NewError(fiber.StatusUnprocessableEntity, err.Error())
	default:
		return err
	}
}

// alertRuleRequest ist der Request einer Alarm-Regel.
type alertRuleRequest struct {
	Name             string `json:"name"`
	Type             string `json:"type"`
	Enabled          bool   `json:"enabled"`
	GroupIDs         []uint `json:"group_ids"`
	ChannelID        *uint  `json:"channel_id"`
	Severity         string `json:"severity"`
	ThresholdPercent int    `json:"threshold_percent"`
	ForecastDays     int    `json:"forecast_days"`
	MaxOutdated      int    `json:"max_outdated"`
	MinSeverity      string `json:"min_severity"`
	HeartbeatHours   int    `json:"heartbeat_hours"`
	CooldownMinutes  int    `json:"cooldown_minutes"`
}

func (r *alertRuleRequest) toInput() services.AlertRuleInput {
	return services.AlertRuleInput{
		Name: r.Name, Type: r.Type, Enabled: r.Enabled,
		GroupIDs: r.GroupIDs, ChannelID: r.ChannelID, Severity: r.Severity,
		ThresholdPercent: r.ThresholdPercent, ForecastDays: r.ForecastDays,
		MaxOutdated: r.MaxOutdated, MinSeverity: r.MinSeverity,
		HeartbeatHours: r.HeartbeatHours, CooldownMinutes: r.CooldownMinutes,
	}
}

// List - GET /api/v1/alert-rules (alerts:manage)
func (ctrl *AlertController) List(c fiber.Ctx) error {
	rules, err := ctrl.alerts.List()
	if err != nil {
		return err
	}
	return c.JSON(rules)
}

// Create - POST /api/v1/alert-rules (alerts:manage)
func (ctrl *AlertController) Create(c fiber.Ctx) error {
	var req alertRuleRequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	rule, err := ctrl.alerts.Create(req.toInput(), actor(c))
	if err != nil {
		return mapAlertError(err)
	}
	return c.Status(fiber.StatusCreated).JSON(rule)
}

// Update - PATCH /api/v1/alert-rules/:id (alerts:manage)
func (ctrl *AlertController) Update(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	var req alertRuleRequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	rule, err := ctrl.alerts.Update(id, req.toInput(), actor(c))
	if err != nil {
		return mapAlertError(err)
	}
	return c.JSON(rule)
}

// Delete - DELETE /api/v1/alert-rules/:id (alerts:manage)
func (ctrl *AlertController) Delete(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	if err := ctrl.alerts.Delete(id, actor(c)); err != nil {
		return mapAlertError(err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// Events - GET /api/v1/alert-events (alerts:manage)
// Events - GET /api/v1/alert-events (alerts:manage)
// Seitenweise mit Filtern: type, severity, server_id, page, page_size.
// Antwort {items, total, page, page_size}. Vorher: fest die neuesten 200,
// Paging und Filter wirkungslos - ältere Ereignisse unerreichbar (R2-023).
func (ctrl *AlertController) Events(c fiber.Ctx) error {
	if sev := c.Query("severity"); sev != "" && sev != domain.AlertSeverityInfo &&
		sev != domain.AlertSeverityWarning && sev != domain.AlertSeverityCritical {
		return fiber.NewError(fiber.StatusBadRequest,
			"unbekannter severity-Filter (erlaubt: info, warning, critical)")
	}
	if raw := c.Query("server_id"); raw != "" {
		if _, err := strconv.ParseUint(raw, 10, 64); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "server_id muss eine Zahl sein")
		}
	}
	page := fiber.Query[int](c, "page")
	if page < 1 {
		page = 1
	}
	pageSize := fiber.Query[int](c, "page_size")
	if pageSize <= 0 || pageSize > 500 {
		pageSize = 100
	}
	events, total, err := ctrl.alerts.ListEventsFiltered(repositories.AlertEventFilter{
		Type:     c.Query("type"),
		Severity: c.Query("severity"),
		ServerID: uint(fiber.Query[int](c, "server_id")),
		Limit:    pageSize,
		Offset:   (page - 1) * pageSize,
	})
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"items": events, "total": total, "page": page, "page_size": pageSize})
}

// Evaluate - POST /api/v1/alerts/evaluate (alerts:manage)
// Löst eine sofortige Auswertung aller Alarm-Regeln aus.
func (ctrl *AlertController) Evaluate(c fiber.Ctx) error {
	summary, err := ctrl.alerts.Evaluate(actor(c))
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"status": "evaluated", "summary": summary})
}
