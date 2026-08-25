package controllers

import (
	"errors"

	"github.com/gofiber/fiber/v3"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

// AdvisoryController bedient die Frühwarnung: die Befunde der Online-Quellen
// zum installierten Paketbestand.
type AdvisoryController struct {
	advisories *services.AdvisoryService
	// poll und mirror stoßen die Läufe an und liefern den Job zurück. Die
	// Oberfläche wartet darauf: Ohne das Ergebnis blieb es bei „gestartet",
	// und ob etwas gefunden wurde, nichts zu tun war oder der Lauf scheiterte,
	// erfuhr niemand.
	poll   func(actor string) (*domain.Job, error)
	mirror func(actor string) (*domain.Job, error)
}

func NewAdvisoryController(advisories *services.AdvisoryService, poll, mirror func(actor string) (*domain.Job, error)) *AdvisoryController {
	return &AdvisoryController{advisories: advisories, poll: poll, mirror: mirror}
}

// advisoryPageSize ist die Seitengröße der Befundliste - dieselbe Obergrenze
// wie bei den CVE-Funden.
const advisoryPageSizeMax = 200

// List - GET /api/v1/security/advisories (packages:read)
// Befunde über alle sichtbaren Server, kritischste zuerst. Parameter:
// page, page_size, min_severity und resolved=1 (Verlauf mit anzeigen).
func (ctrl *AdvisoryController) List(c fiber.Ctx) error {
	page := fiber.Query[int](c, "page")
	if page < 1 {
		page = 1
	}
	pageSize := fiber.Query[int](c, "page_size")
	if pageSize <= 0 || pageSize > advisoryPageSizeMax {
		pageSize = 100
	}
	minSeverity := c.Query("min_severity")
	switch minSeverity {
	case domain.SeverityCritical, domain.SeverityHigh, domain.SeverityMedium, domain.SeverityLow:
	default:
		minSeverity = "" // alles andere heißt: nicht filtern
	}

	res, err := ctrl.advisories.Global(scopeFor(c), repositories.AdvisoryFilter{
		IncludeResolved: c.Query("resolved") == "1",
		MinSeverity:     minSeverity,
		Page:            page,
		PageSize:        pageSize,
	})
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{
		"items":     res.Items,
		"total":     res.Total,
		"page":      res.Page,
		"page_size": res.PageSize,
		"summary":   res.Summary,
		"status":    ctrl.status(),
	})
}

// Status - GET /api/v1/security/advisories/status (packages:read)
// Betriebszustand der Frühwarnung, ohne die Fundliste.
func (ctrl *AdvisoryController) Status(c fiber.Ctx) error {
	return c.JSON(ctrl.status())
}

// status bündelt, was die Oberfläche über den Zustand wissen muss.
//
// Die Zeitstempel sind kein Beiwerk: Eine leere Fundliste ist ohne sie nicht
// von „noch nie nachgesehen" zu unterscheiden - und im lokalen Betrieb
// bestimmt der Stand des Spiegels, wie alt die Aussage überhaupt ist.
func (ctrl *AdvisoryController) status() fiber.Map {
	out := fiber.Map{
		"enabled":    ctrl.advisories.Enabled(),
		"local_copy": ctrl.advisories.UsesLocalCopy(),
	}
	if t := ctrl.advisories.LastPollAt(); !t.IsZero() {
		out["last_poll_at"] = t
	}
	if t := ctrl.advisories.MirroredAt(); !t.IsZero() {
		out["mirrored_at"] = t
	}
	// Ohne einen einzigen Server mit erfasstem Paketbestand gibt es nichts
	// zu spiegeln - die Oberfläche kann den scheinbar wirkungslosen Knopf
	// damit erklären, statt den Betreiber raten zu lassen.
	out["mirrorable"] = ctrl.advisories.MirrorableEcosystems()
	return out
}

// Caches - GET /api/v1/security/caches (packages:read)
// Trefferquote und Belegung beider Zwischenspeicher: des Scan-Cache im
// Arbeitsspeicher und des Advisory-Cache in der Datenbank.
func (ctrl *AdvisoryController) Caches(c fiber.Ctx) error {
	report, err := ctrl.advisories.CacheStats()
	if err != nil {
		return err
	}
	return c.JSON(report)
}

// Poll - POST /api/v1/security/advisories/poll (servers:write)
// Stößt einen Durchgang sofort an, statt bis zum nächsten Takt zu warten.
//
// Ohne diesen Knopf passierte nach dem Einschalten der Frühwarnung bis zu
// 15 Minuten lang sichtbar nichts - nicht zu unterscheiden von „es
// funktioniert nicht".
func (ctrl *AdvisoryController) Poll(c fiber.Ctx) error {
	if !ctrl.advisories.Enabled() {
		return fiber.NewError(fiber.StatusConflict, "die frühwarnung ist nicht eingeschaltet")
	}
	job, err := ctrl.poll(actor(c))
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{
		"status": "durchgang gestartet", "job_id": job.ID, "job_name": job.Name,
	})
}

// Mirror - POST /api/v1/security/advisories/mirror (settings:manage)
// Spiegelt die lokale OSV-Kopie sofort. Braucht das Einstellungs-Recht: Der
// Lauf lädt zig Megabyte auf den LCM-Host.
func (ctrl *AdvisoryController) Mirror(c fiber.Ctx) error {
	if !ctrl.advisories.UsesLocalCopy() {
		return fiber.NewError(fiber.StatusConflict, "die lokale kopie ist nicht eingestellt")
	}
	job, err := ctrl.mirror(actor(c))
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{
		"status": "spiegellauf gestartet", "job_id": job.ID, "job_name": job.Name,
	})
}

// Acknowledge - POST /api/v1/security/advisories/:id/acknowledge (servers:write)
// Nimmt einen Befund zur Kenntnis: Er bleibt sichtbar, löst aber keinen Alarm
// mehr aus. Ohne dieses Ventil bliebe bei einem dauerhaft nicht behebbaren
// Befund nur, die ganze Alarmregel abzuschalten.
func (ctrl *AdvisoryController) Acknowledge(c fiber.Ctx) error {
	if err := ctrl.advisories.Acknowledge(scopeFor(c), c.Params("id"), actor(c)); err != nil {
		if errors.Is(err, repositories.ErrNotFound) {
			return fiber.NewError(fiber.StatusNotFound, "befund nicht gefunden")
		}
		return err
	}
	return c.JSON(fiber.Map{"status": "zur kenntnis genommen"})
}
