package controllers

import (
	"errors"

	"github.com/gofiber/fiber/v3"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

// AppController bedient den Anwendungskatalog und die Funde je Server:
// Software, die nicht aus der Paketverwaltung stammt.
type AppController struct {
	apps    *services.AppService
	servers *services.ServerService
	// run stößt Sicherung/Update einer erkannten Anwendung an. Als Funktion
	// statt als Executor-Verweis: Der Controller braucht genau diesen einen
	// Einstieg, nicht den ganzen Ausführungsapparat.
	run func(server *domain.Server, slug, kind string, withBackup bool, actor string) (*domain.Job, error)
}

func NewAppController(apps *services.AppService) *AppController {
	return &AppController{apps: apps}
}

// WithActions verdrahtet das Auslösen von Sicherung und Update.
func (ctrl *AppController) WithActions(servers *services.ServerService,
	run func(server *domain.Server, slug, kind string, withBackup bool, actor string) (*domain.Job, error),
) *AppController {
	ctrl.servers, ctrl.run = servers, run
	return ctrl
}

func mapAppError(err error) error {
	switch {
	case errors.Is(err, repositories.ErrNotFound):
		return fiber.NewError(fiber.StatusNotFound, "anwendung nicht gefunden")
	case errors.Is(err, services.ErrAppNotDetected), errors.Is(err, services.ErrAppNoAction):
		return fiber.NewError(fiber.StatusConflict, err.Error())
	case errors.Is(err, services.ErrAppBuiltinDelete):
		return fiber.NewError(fiber.StatusForbidden, err.Error())
	case errors.Is(err, services.ErrAppSlugTaken):
		return fiber.NewError(fiber.StatusConflict, err.Error())
	case errors.Is(err, domain.ErrAppSlug), errors.Is(err, domain.ErrAppName),
		errors.Is(err, domain.ErrAppMarker), errors.Is(err, domain.ErrAppNoMarker),
		errors.Is(err, domain.ErrAppCompare), errors.Is(err, domain.ErrAppPattern),
		errors.Is(err, domain.ErrAppSource), errors.Is(err, services.ErrAppActionMissing):
		return fiber.NewError(fiber.StatusUnprocessableEntity, err.Error())
	default:
		return err
	}
}

type appRequest struct {
	Slug           string `json:"slug"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	NameEN         string `json:"name_en"`
	DescriptionEN  string `json:"description_en"`
	Enabled        bool   `json:"enabled"`
	Markers        string `json:"markers"`
	VersionCommand string `json:"version_command"`
	VersionPattern string `json:"version_pattern"`
	Compare        string `json:"compare"`
	LatestSource   string `json:"latest_source"`
	LatestPattern  string `json:"latest_pattern"`
	BackupActionID *uint  `json:"backup_action_id"`
	UpdateActionID *uint  `json:"update_action_id"`
}

func (r appRequest) toEntry() *domain.AppCatalogEntry {
	return &domain.AppCatalogEntry{
		Slug: r.Slug, Name: r.Name, Description: r.Description, Enabled: r.Enabled,
		NameEN: r.NameEN, DescriptionEN: r.DescriptionEN,
		Markers: r.Markers, VersionCommand: r.VersionCommand, VersionPattern: r.VersionPattern,
		Compare: r.Compare, LatestSource: r.LatestSource, LatestPattern: r.LatestPattern,
		BackupActionID: r.BackupActionID, UpdateActionID: r.UpdateActionID,
	}
}

// ListApps - GET /api/v1/apps (settings:manage)
func (ctrl *AppController) ListApps(c fiber.Ctx) error {
	entries, err := ctrl.apps.List()
	if err != nil {
		return err
	}
	return c.JSON(entries)
}

// CreateApp - POST /api/v1/apps (settings:manage)
func (ctrl *AppController) CreateApp(c fiber.Ctx) error {
	var req appRequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	entry := req.toEntry()
	if err := ctrl.apps.Create(entry, actor(c)); err != nil {
		return mapAppError(err)
	}
	return c.Status(fiber.StatusCreated).JSON(entry)
}

// UpdateApp - PUT /api/v1/apps/:id (settings:manage)
func (ctrl *AppController) UpdateApp(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	var req appRequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	entry := req.toEntry()
	if err := ctrl.apps.Update(id, entry, actor(c)); err != nil {
		return mapAppError(err)
	}
	return c.JSON(entry)
}

// DeleteApp - DELETE /api/v1/apps/:id (settings:manage)
func (ctrl *AppController) DeleteApp(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	if err := ctrl.apps.Delete(id, actor(c)); err != nil {
		return mapAppError(err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ServerApps - GET /api/v1/servers/:id/apps (servers:read)
// Die auf diesem Server gefundenen Anwendungen samt der Dienste, die zu
// keinem Paket gehören.
func (ctrl *AppController) ServerApps(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	apps, err := ctrl.apps.ForServer(id)
	if err != nil {
		return mapAppError(err)
	}
	return c.JSON(apps)
}

// RunAppAction - POST /api/v1/servers/:id/apps/:slug/:action (servers:write)
// action ist "backup" oder "update"; ohne "with_backup": false im Body läuft
// vor dem Update die hinterlegte Sicherung.
func (ctrl *AppController) RunAppAction(c fiber.Ctx) error {
	if ctrl.run == nil || ctrl.servers == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "aktionen sind nicht verfügbar")
	}
	id, err := paramID(c)
	if err != nil {
		return err
	}
	kind := c.Params("action")
	if kind != services.AppActionBackup && kind != services.AppActionUpdate {
		return fiber.NewError(fiber.StatusBadRequest, "aktion muss backup oder update sein")
	}
	req := struct {
		WithBackup *bool `json:"with_backup"`
	}{}
	if len(c.Body()) > 0 {
		if err := c.Bind().Body(&req); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
		}
	}
	// Ohne Angabe wird gesichert - die vorsichtige Vorgabe ist hier die
	// richtige.
	withBackup := req.WithBackup == nil || *req.WithBackup

	server, err := ctrl.servers.Get(scopeFor(c), id)
	if err != nil {
		return mapServerError(err)
	}
	job, err := ctrl.run(server, c.Params("slug"), kind, withBackup, actor(c))
	if err != nil {
		return mapAppError(err)
	}
	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{"status": "started", "job_id": job.ID})
}
