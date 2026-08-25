package controllers

import (
	"errors"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

// JobController bedient die Job-Historie und den SSH-Konsolen-Output.
type JobController struct {
	jobs  *services.JobService
	audit *services.AuditService
}

func NewJobController(jobs *services.JobService, audit *services.AuditService) *JobController {
	return &JobController{jobs: jobs, audit: audit}
}

// History - GET /api/v1/jobs/history (jobs:read)
// Gefiltert und seitenweise. Query-Parameter: server_id, status, type,
// q (Namenssuche), triggered_by, hide_health, from, to (Datum YYYY-MM-DD),
// page, page_size. Antwort: {items, total, page, page_size}.
func (ctrl *JobController) History(c fiber.Ctx) error {
	page := fiber.Query[int](c, "page")
	if page < 1 {
		page = 1
	}
	pageSize := fiber.Query[int](c, "page_size")
	if pageSize <= 0 || pageSize > 500 {
		pageSize = 100
	}
	// Unbekannte Filterwerte werden ABGEWIESEN statt still zu einer leeren
	// Treffermenge zu führen (R2-069): "status=quatsch" sah aus wie „keine
	// solchen Jobs", und ein nicht parsbares server_id lieferte ALLE Jobs.
	if s := c.Query("status"); s != "" && !validJobStatusFilter(s) {
		return fiber.NewError(fiber.StatusBadRequest,
			"unbekannter status-Filter "+strconv.Quote(s)+" (erlaubt: pending, running, success, failed, blocked)")
	}
	if raw := c.Query("server_id"); raw != "" {
		if _, err := strconv.ParseUint(raw, 10, 64); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "server_id muss eine Zahl sein")
		}
	}
	if jt := c.Query("type"); jt != "" && !knownJobType(jt) {
		return fiber.NewError(fiber.StatusBadRequest, "unbekannter type-Filter - siehe Job-Typen in der Historie")
	}
	f := repositories.JobFilter{
		ServerID:    uint(fiber.Query[int](c, "server_id")),
		Status:      c.Query("status"),
		Type:        c.Query("type"),
		NameQuery:   c.Query("q"),
		TriggeredBy: c.Query("triggered_by"),
		HideHealth:  fiber.Query[bool](c, "hide_health"),
		Limit:       pageSize,
		Offset:      (page - 1) * pageSize,
	}
	if from := parseDateParam(c.Query("from")); from != nil {
		f.From = from
	}
	if to := parseDateParam(c.Query("to")); to != nil {
		// "bis"-Tag inklusive: exklusive obere Grenze = Folgetag 00:00.
		next := to.Add(24 * time.Hour)
		f.To = &next
	}

	jobs, total, err := ctrl.jobs.HistoryFiltered(scopeFor(c), f)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"items": jobs, "total": total, "page": page, "page_size": pageSize})
}

// FilterOptions - GET /api/v1/jobs/filter-options (jobs:read)
// Distinkte Typen und Auslöser für die Filter-Dropdowns.
func (ctrl *JobController) FilterOptions(c fiber.Ctx) error {
	types, triggeredBy, err := ctrl.jobs.FilterOptions(scopeFor(c), uint(fiber.Query[int](c, "server_id")))
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"types": types, "triggered_by": triggeredBy})
}

// parseDateParam liest ein Datum im Format YYYY-MM-DD (leer => nil).
func parseDateParam(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil
	}
	return &t
}

// ConsoleOutput - GET /api/v1/jobs/:id/ssh-output (jobs:read)
func (ctrl *JobController) ConsoleOutput(c fiber.Ctx) error {
	id, err := paramStrID(c)
	if err != nil {
		return err
	}
	job, output, err := ctrl.jobs.ConsoleOutput(scopeFor(c), id)
	if err != nil {
		return mapServerError(err)
	}
	return c.JSON(fiber.Map{
		"job_id":    job.ID,
		"name":      job.Name,
		"status":    job.Status,
		"exit_code": job.ExitCode,
		"output":    output,
	})
}

// Abort - POST /api/v1/jobs/:id/abort (servers:write)
// Bricht einen laufenden Job manuell ab und hebt damit die Server-Sperre auf
// (Sicherheitsventil für hängende Läufe).
func (ctrl *JobController) Abort(c fiber.Ctx) error {
	id, err := paramStrID(c)
	if err != nil {
		return err
	}
	job, err := ctrl.jobs.Abort(scopeFor(c), id, actor(c))
	if err != nil {
		if errors.Is(err, services.ErrJobNotRunning) {
			return fiber.NewError(fiber.StatusConflict, err.Error())
		}
		return mapServerError(err)
	}
	ctrl.audit.Log(actor(c), "job.abort", "job", 0, job.Name)
	return c.JSON(job)
}

// AuditLog - GET /api/v1/audit (audit:read)
func (ctrl *JobController) AuditLog(c fiber.Ctx) error {
	limit := fiber.Query[int](c, "limit")
	entries, err := ctrl.audit.Recent(limit)
	if err != nil {
		return err
	}
	return c.JSON(entries)
}

// validJobStatusFilter prüft den status-Query gegen die echten Job-Zustände.
func validJobStatusFilter(s string) bool {
	switch s {
	case domain.JobStatusPending, domain.JobStatusRunning, domain.JobStatusSuccess,
		domain.JobStatusFailed, domain.JobStatusBlocked, domain.JobStatusAborted:
		return true
	}
	return false
}

// knownJobType prüft den type-Query gegen alle Job-Typen, die LCM erzeugt:
// die Regel-Typen plus die Refresh-Jobs.
func knownJobType(t string) bool {
	switch t {
	case domain.RuleTypeUpdate, domain.RuleTypePackages, domain.RuleTypeSecurity,
		domain.RuleTypePackageScan, domain.RuleTypeAutoremove, domain.RuleTypeScript,
		domain.RuleTypeCustom, domain.RuleTypeSync, domain.RuleTypeHealth,
		domain.RuleTypeFirewall, domain.RuleTypeBackup, domain.RuleTypeCleanup,
		domain.RuleTypeCVEScan, domain.RuleTypeDockerCheck, domain.RuleTypeAppCheck, domain.RuleTypeDockerUpdate,
		domain.RuleTypeDockerPrune, domain.RuleTypeDockerUpdateUnused,
		domain.RuleTypeAptProxy, domain.RuleTypeAlertCheck,
		domain.RuleTypeReboot, domain.RuleTypeRebootIfNeeded,
		domain.RuleTypeDNSTest, domain.RuleTypeDeepScan,
		services.JobTypeHardwareRefresh, services.JobTypeFullRefresh:
		return true
	}
	return false
}
