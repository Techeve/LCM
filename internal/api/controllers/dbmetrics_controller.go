package controllers

import (
	"bytes"

	"github.com/gofiber/fiber/v3"

	"LCM/internal/dbmetrics"
)

// DBMetricsController liefert die Messung der Datenbank: als JSON für das
// Werkzeug lcm-probe und im Prometheus-Textformat für alle, die Prometheus
// schon haben. Beide lesen dieselbe Momentaufnahme.
type DBMetricsController struct {
	reporter *dbmetrics.Reporter
}

// NewDBMetricsController verbindet den Controller mit der Messung.
func NewDBMetricsController(reporter *dbmetrics.Reporter) *DBMetricsController {
	return &DBMetricsController{reporter: reporter}
}

// Snapshot - GET /api/v1/system/db (settings:manage)
func (ctrl *DBMetricsController) Snapshot(c fiber.Ctx) error {
	return c.JSON(ctrl.reporter.Snapshot())
}

// Metrics - GET /metrics (settings:manage)
func (ctrl *DBMetricsController) Metrics(c fiber.Ctx) error {
	var buf bytes.Buffer
	if err := dbmetrics.WritePrometheus(&buf, ctrl.reporter.Snapshot()); err != nil {
		return err
	}
	c.Set(fiber.HeaderContentType, "text/plain; version=0.0.4; charset=utf-8")
	return c.Send(buf.Bytes())
}
