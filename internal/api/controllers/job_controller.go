package controllers

import (
	"errors"
	"strings"

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

// List - GET /api/v1/jobs (jobs:read)
// Filtern, Sortieren und Blättern im Backend. Query: server_id, status
// (Kommaliste), kind oder type (Kommaliste), q (Teil des Namens), triggered_by,
// hide_health, since und until (RFC 3339; until ist exklusiv), sort
// (-started_at, started_at oder -duration; Vorgabe -started_at), limit
// (Vorgabe 50, höchstens 500), offset.
// Antwort: {items, total}; ein Eintrag ist der Job ohne Ausgabe plus
// server_name und duration_seconds.
func (ctrl *JobController) List(c fiber.Ctx) error {
	f, err := jobFilter(c)
	if err != nil {
		return err
	}
	items, total, err := ctrl.jobs.List(scopeFor(c), f)
	if err != nil {
		return err
	}
	return c.JSON(newListResponse(items, int(total)))
}

// jobFilter liest und prüft die Query-Parameter der Job-Liste. Unbekannte
// Filterwerte werden ABGEWIESEN statt still zu einer leeren Treffermenge zu
// führen (R2-069).
func jobFilter(c fiber.Ctx) (repositories.JobFilter, error) {
	var f repositories.JobFilter
	if err := checkLines(lineField{"q", c.Query("q"), maxQueryLen}, lineField{"triggered_by", c.Query("triggered_by"), maxQueryLen}); err != nil {
		return f, err
	}
	var err error
	if f.ServerID, err = idQuery(c, "server_id"); err != nil {
		return f, err
	}
	statuses, err := enumListQuery(c, "status", jobStatuses)
	if err != nil {
		return f, err
	}
	// kind ist der Name, den die Oberfläche kennt; type der alte.
	typeKey := "type"
	if c.Query("kind") != "" {
		typeKey = "kind"
	}
	types, err := enumListQuery(c, typeKey, jobTypes)
	if err != nil {
		return f, err
	}
	f.Status, f.Type = strings.Join(statuses, ","), strings.Join(types, ",")
	f.NameQuery, f.TriggeredBy, f.HideHealth = c.Query("q"), c.Query("triggered_by"), fiber.Query[bool](c, "hide_health")
	if f.From, err = timeQuery(c, "since"); err != nil {
		return f, err
	}
	if f.To, err = timeQuery(c, "until"); err != nil {
		return f, err
	}
	if f.Sort, err = sortQuery(c, []string{repositories.JobSortStartedDesc, repositories.JobSortStartedAsc, repositories.JobSortDurationDesc}, repositories.JobSortStartedDesc); err != nil {
		return f, err
	}
	f.Limit, f.Offset = pagingOf(c, 50, 500)
	return f, nil
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
			return errJobNotRunning
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

// jobStatuses sind die echten Job-Zustände.
var jobStatuses = []string{
	domain.JobStatusPending, domain.JobStatusRunning, domain.JobStatusSuccess,
	domain.JobStatusFailed, domain.JobStatusBlocked, domain.JobStatusAborted,
}

// jobTypes sind alle Job-Typen, die LCM erzeugt: die Regel-Typen plus die
// Refresh-, Reparatur- und Onboarding-Jobs. Ein Typ, der hier fehlt, lässt sich
// im Verlauf nicht filtern (400), obwohl die Oberfläche ihn anbietet; ein Test
// gleicht die Liste der Oberfläche (job-labels.ts) mit dieser ab.
var jobTypes = []string{
	domain.RuleTypeUpdate, domain.RuleTypePackages, domain.RuleTypeSecurity,
	domain.RuleTypePackageScan, domain.RuleTypeAutoremove, domain.RuleTypeScript,
	domain.RuleTypeCustom, domain.RuleTypeSync, domain.RuleTypeHealth,
	domain.RuleTypeFirewall, domain.RuleTypeBackup, domain.RuleTypeCleanup,
	domain.RuleTypeCVEScan, domain.RuleTypeDockerCheck, domain.RuleTypeAppCheck, domain.RuleTypeDockerUpdate,
	domain.RuleTypeDockerPrune, domain.RuleTypeDockerUpdateUnused,
	domain.RuleTypeDockerComposeUpdate, domain.RuleTypeDockerContainerUpdate,
	domain.RuleTypeAptProxy, domain.RuleTypeAlertCheck,
	domain.RuleTypeReboot, domain.RuleTypeRebootIfNeeded,
	domain.RuleTypeDNSTest, domain.RuleTypeDeepScan,
	domain.RuleTypeACLSetup, domain.RuleTypePermSync,
	domain.JobTypeJoin, domain.JobTypeReconnect, domain.JobTypeHardenSSH,
	services.JobTypeHardwareRefresh, services.JobTypeFullRefresh,
	services.JobTypeRepairDpkg, services.JobTypeRepairDeps,
}
