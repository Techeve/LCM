package controllers

import (
	"errors"

	"github.com/gofiber/fiber/v3"

	"LCM/internal/core/domain"
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
	if errors.Is(err, repositories.ErrNotFound) {
		return errAlertNotFound
	}
	return mapSentinel(err, alertErrors)
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
	if err := bindBody(c, &req); err != nil {
		return err
	}
	if err := checkAlertRequest(req); err != nil {
		return err
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
	if err := bindBody(c, &req); err != nil {
		return err
	}
	if err := checkAlertRequest(req); err != nil {
		return err
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
// Seitenweise mit Filtern, neueste zuerst. Query: server_id, type, severity,
// since (RFC 3339), limit (Vorgabe 100, höchstens 500), offset. Der Verlauf
// enthält nur Ereignisse von Servern im Sichtbereich des Aufrufers; Ereignisse
// ohne Server sehen nur Admins. Antwort: {items, total} (R2-023).
func (ctrl *AlertController) Events(c fiber.Ctx) error {
	f, err := alertEventFilter(c)
	if err != nil {
		return err
	}
	events, total, err := ctrl.alerts.ListEventsFiltered(scopeFor(c), f)
	if err != nil {
		return err
	}
	return c.JSON(newListResponse(events, int(total)))
}

// alertEventFilter liest und prüft die Query-Parameter der Alarm-Historie.
func alertEventFilter(c fiber.Ctx) (repositories.AlertEventFilter, error) {
	var f repositories.AlertEventFilter
	if err := checkLines(lineField{"type", c.Query("type"), maxQueryLen}); err != nil {
		return f, err
	}
	if sev := c.Query("severity"); sev != "" && sev != domain.AlertSeverityInfo &&
		sev != domain.AlertSeverityWarning && sev != domain.AlertSeverityCritical {
		return f, errInvalidFilter.With("filter", "severity").With("allowed", "info, warning, critical")
	}
	var err error
	if f.ServerID, err = idQuery(c, "server_id"); err != nil {
		return f, err
	}
	if f.Since, err = timeQuery(c, "since"); err != nil {
		return f, err
	}
	f.Type, f.Severity = c.Query("type"), c.Query("severity")
	f.Limit, f.Offset = pagingOf(c, 100, 500)
	return f, nil
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

// checkAlertRequest prüft Namen, Texte und die Zahlenfelder einer Alarm-Regel.
// 0 heißt bei den Schwellen „Vorgabe“ bzw. beim Cooldown „keine Sperre“.
func checkAlertRequest(req alertRuleRequest) error {
	if err := checkLines(lineField{"name", req.Name, maxNameLen}, lineField{"type", req.Type, maxNameLen}, lineField{"severity", req.Severity, maxNameLen}, lineField{"min_severity", req.MinSeverity, maxNameLen}); err != nil {
		return err
	}
	if err := checkCount("group_ids", len(req.GroupIDs), maxItems); err != nil {
		return err
	}
	for _, r := range []struct {
		field    string
		value    int
		min, max int
	}{
		{"threshold_percent", req.ThresholdPercent, 0, 100},
		{"forecast_days", req.ForecastDays, 0, 3650},
		{"max_outdated", req.MaxOutdated, 0, 100000},
		{"heartbeat_hours", req.HeartbeatHours, 0, 8760},
		{"cooldown_minutes", req.CooldownMinutes, 0, 525600},
	} {
		if err := checkRange(r.field, r.value, r.min, r.max); err != nil {
			return err
		}
	}
	return nil
}
