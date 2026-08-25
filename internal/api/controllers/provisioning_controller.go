package controllers

import (
	"errors"
	"time"

	"github.com/gofiber/fiber/v3"

	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

// ProvisioningController bedient die Server↔Linux-User-Zuordnung (inkl.
// Zertifikats-Verteilung) und die Aktivierungslinks für LCM-User.
type ProvisioningController struct {
	prov       *services.ProvisioningService
	activation *services.ActivationService
	servers    *services.ServerService
}

func NewProvisioningController(prov *services.ProvisioningService, activation *services.ActivationService, servers *services.ServerService) *ProvisioningController {
	return &ProvisioningController{prov: prov, activation: activation, servers: servers}
}

func mapProvError(err error) error {
	switch {
	case errors.Is(err, repositories.ErrNotFound):
		return fiber.NewError(fiber.StatusNotFound, "nicht gefunden")
	case errors.Is(err, services.ErrWeakPassword):
		return fiber.NewError(fiber.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, services.ErrActivationInvalid), errors.Is(err, services.ErrActivationExpired):
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	case errors.Is(err, services.ErrProxmoxRestricted), errors.Is(err, services.ErrRestrictedSudo),
		errors.Is(err, services.ErrUserSyncDisabled),
		errors.Is(err, services.ErrServerUserProtected), errors.Is(err, services.ErrServerUserManaged):
		return fiber.NewError(fiber.StatusConflict, err.Error())
	case errors.Is(err, services.ErrInvalidServerUsername):
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	default:
		return err
	}
}

type generateLinkRequest struct {
	UserID   uint `json:"user_id"`
	TTLHours int  `json:"ttl_hours"`
	// SendEmail verschickt den Link zusätzlich als Einladung an die
	// E-Mail-Adresse des Users (setzt den Standard-E-Mail-Versand voraus).
	SendEmail bool `json:"send_email"`
}

// GenerateActivationLink - POST /api/v1/users/activation-links/generate (users:write)
func (ctrl *ProvisioningController) GenerateActivationLink(c fiber.Ctx) error {
	var req generateLinkRequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	ttl := time.Duration(req.TTLHours) * time.Hour
	token, link, err := ctrl.activation.Generate(req.UserID, ttl, actor(c))
	if err != nil {
		return mapProvError(err)
	}
	// Optionaler Mail-Versand: Der Link wurde bereits erzeugt - schlägt nur
	// der Versand fehl (Mailer aus, keine Adresse), kommt das Token trotzdem
	// zurück, mit der Ursache in mail_error.
	mailed, mailError := false, ""
	if req.SendEmail {
		if err := ctrl.activation.MailInvitation(req.UserID, token, link.ExpiresAt); err != nil {
			mailError = err.Error()
		} else {
			mailed = true
		}
	}
	// Das Klartext-Token ist nur hier sichtbar - der Aufrufer baut daraus
	// den Aktivierungslink für den neuen User.
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"token":      token,
		"expires_at": link.ExpiresAt,
		"mailed":     mailed,
		"mail_error": mailError,
	})
}

type consumeLinkRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

// ConsumeActivationLink - POST /api/v1/users/activation-links/consume (public)
func (ctrl *ProvisioningController) ConsumeActivationLink(c fiber.Ctx) error {
	var req consumeLinkRequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	user, err := ctrl.activation.Consume(req.Token, req.Password)
	if err != nil {
		return mapProvError(err)
	}
	return c.JSON(fiber.Map{"status": "activated", "username": user.Username})
}

// ---- Server ↔ User -----------------------------------------------------------

// AssignUser - POST /api/v1/servers/:id/assign-user (servers:write)
// Ordnet einen Linux-Benutzer direkt einem Server zu.
func (ctrl *ProvisioningController) AssignUser(c fiber.Ctx) error {
	return ctrl.serverUserAction(c, true)
}

// RemoveUser - POST /api/v1/servers/:id/remove-user (servers:write)
func (ctrl *ProvisioningController) RemoveUser(c fiber.Ctx) error {
	return ctrl.serverUserAction(c, false)
}

func (ctrl *ProvisioningController) serverUserAction(c fiber.Ctx, assign bool) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	var req struct {
		LinuxUserID uint `json:"linux_user_id"`
	}
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	server, err := ctrl.servers.Get(scopeFor(c), id)
	if err != nil {
		return mapServerError(err)
	}
	if assign {
		err = ctrl.prov.AssignLinuxUserToServer(server, req.LinuxUserID, actor(c))
	} else {
		err = ctrl.prov.RemoveLinuxUserFromServer(server, req.LinuxUserID, actor(c))
	}
	if err != nil {
		return mapProvError(err)
	}
	return c.JSON(fiber.Map{"status": "ok"})
}

// ---- Benutzer-Übersicht (gescannte Konten des Zielsystems) -------------------

// ServerUsers - GET /api/v1/servers/:id/users (servers:read)
// Die beim letzten Scan erfassten anmeldefähigen Linux-Konten.
func (ctrl *ProvisioningController) ServerUsers(c fiber.Ctx) error {
	server, err := ctrl.serverFromPath(c)
	if err != nil {
		return err
	}
	users, err := ctrl.prov.ListServerUsers(server)
	if err != nil {
		return mapProvError(err)
	}
	return c.JSON(users)
}

// PendingUserSyncs - GET /api/v1/servers/:id/users/pending (servers:read)
// Offene Benutzer-Abgleiche: Was liegengeblieben ist, weil der Server im
// Moment der Änderung nicht erreichbar war.
func (ctrl *ProvisioningController) PendingUserSyncs(c fiber.Ctx) error {
	server, err := ctrl.serverFromPath(c)
	if err != nil {
		return err
	}
	pending, err := ctrl.prov.PendingUserSyncs(server.ID)
	if err != nil {
		return mapProvError(err)
	}
	if pending == nil {
		pending = []domain.PendingUserSync{}
	}
	return c.JSON(pending)
}

// RefreshServerUsers - POST /api/v1/servers/:id/users/refresh (servers:write)
// Erhebt die Konten jetzt neu über SSH.
func (ctrl *ProvisioningController) RefreshServerUsers(c fiber.Ctx) error {
	server, err := ctrl.serverFromPath(c)
	if err != nil {
		return err
	}
	users, err := ctrl.prov.RefreshServerUsers(server, actor(c))
	if err != nil {
		if mapped := mapProvError(err); mapped != err { //nolint:errorlint - Default liefert err unverändert
			return mapped
		}
		return fiber.NewError(fiber.StatusBadGateway, err.Error())
	}
	return c.JSON(users)
}

// ServerUserLogins - GET /api/v1/servers/:id/users/:username/logins (servers:read)
// Die Anmelde-Historie eines Kontos (aus wtmp, siehe users_scan.go).
func (ctrl *ProvisioningController) ServerUserLogins(c fiber.Ctx) error {
	server, err := ctrl.serverFromPath(c)
	if err != nil {
		return err
	}
	logins, err := ctrl.prov.ServerUserLogins(server, c.Params("username"), 100)
	if err != nil {
		return mapProvError(err)
	}
	return c.JSON(logins)
}

type serverUserActionRequest struct {
	Username string `json:"username"`
}

// DisableServerUser - POST /api/v1/servers/:id/users/disable (servers:write)
func (ctrl *ProvisioningController) DisableServerUser(c fiber.Ctx) error {
	return ctrl.serverUserOSAction(c, "disable")
}

// EnableServerUser - POST /api/v1/servers/:id/users/enable (servers:write)
func (ctrl *ProvisioningController) EnableServerUser(c fiber.Ctx) error {
	return ctrl.serverUserOSAction(c, "enable")
}

// RemoveServerUser - POST /api/v1/servers/:id/users/remove (servers:write)
// Entfernt das Konto ENDGÜLTIG vom Zielsystem (samt Home-Verzeichnis).
func (ctrl *ProvisioningController) RemoveServerUser(c fiber.Ctx) error {
	return ctrl.serverUserOSAction(c, "remove")
}

func (ctrl *ProvisioningController) serverUserOSAction(c fiber.Ctx, action string) error {
	server, err := ctrl.serverFromPath(c)
	if err != nil {
		return err
	}
	var req serverUserActionRequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	var out string
	switch action {
	case "disable":
		out, err = ctrl.prov.SetServerUserDisabled(server, req.Username, true, actor(c))
	case "enable":
		out, err = ctrl.prov.SetServerUserDisabled(server, req.Username, false, actor(c))
	default:
		out, err = ctrl.prov.RemoveServerUser(server, req.Username, actor(c))
	}
	if err != nil {
		if mapped := mapProvError(err); mapped != err { //nolint:errorlint - Default liefert err unverändert
			return mapped
		}
		return fiber.NewError(fiber.StatusBadGateway, err.Error())
	}
	return c.JSON(fiber.Map{"status": "ok", "output": out})
}

func (ctrl *ProvisioningController) serverFromPath(c fiber.Ctx) (*domain.Server, error) {
	id, err := paramID(c)
	if err != nil {
		return nil, err
	}
	server, err := ctrl.servers.Get(scopeFor(c), id)
	if err != nil {
		return nil, mapServerError(err)
	}
	return server, nil
}

// SyncUsers - POST /api/v1/servers/:id/sync-users (servers:write)
func (ctrl *ProvisioningController) SyncUsers(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	server, err := ctrl.servers.Get(scopeFor(c), id)
	if err != nil {
		return mapServerError(err)
	}
	output, err := ctrl.prov.SyncUsers(server, actor(c))
	if err != nil {
		// Bekannte Service-Fehler (z.B. Proxmox-Sperre → 409) gemappt
		// durchreichen; nur echte Verbindungs-/Zielsystem-Fehler sind 502.
		if mapped := mapProvError(err); mapped != err { //nolint:errorlint - Default liefert err unverändert
			return mapped
		}
		return fiber.NewError(fiber.StatusBadGateway, err.Error())
	}
	return c.JSON(fiber.Map{"status": "synced", "output": output})
}
