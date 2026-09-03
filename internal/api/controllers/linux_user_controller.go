package controllers

import (
	"errors"
	"time"

	"github.com/gofiber/fiber/v3"

	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

// LinuxUserController verwaltet die Betriebssystem-Benutzer (Katalog,
// SSH-Keys, Zuordnung zu Gruppen). Die Zuordnung zu einzelnen Servern
// läuft über den ProvisioningController (/servers/:id/assign-user).
type LinuxUserController struct {
	linux *services.LinuxUserService
}

func NewLinuxUserController(linux *services.LinuxUserService) *LinuxUserController {
	return &LinuxUserController{linux: linux}
}

func mapLinuxUserError(err error) error {
	switch {
	case errors.Is(err, repositories.ErrNotFound):
		return fiber.NewError(fiber.StatusNotFound, "nicht gefunden")
	case errors.Is(err, services.ErrInvalidLinuxUsername),
		errors.Is(err, services.ErrReservedLinuxUsername),
		errors.Is(err, services.ErrInvalidShell),
		errors.Is(err, services.ErrLinuxUsernameTaken),
		errors.Is(err, services.ErrInvalidSSHKey),
		errors.Is(err, services.ErrWeakPassword),
		errors.Is(err, services.ErrEmptyActivation):
		return fiber.NewError(fiber.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, services.ErrActivationInvalid), errors.Is(err, services.ErrActivationExpired):
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	case errors.Is(err, services.ErrLinuxUserStillOnServers):
		return fiber.NewError(fiber.StatusConflict, err.Error())
	default:
		return err
	}
}

// List - GET /api/v1/linux-users (linuxusers:read)
func (ctrl *LinuxUserController) List(c fiber.Ctx) error {
	users, err := ctrl.linux.List()
	if err != nil {
		return err
	}
	return c.JSON(users)
}

// Get - GET /api/v1/linux-users/:id (linuxusers:read)
func (ctrl *LinuxUserController) Get(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	u, err := ctrl.linux.Get(id)
	if err != nil {
		return mapLinuxUserError(err)
	}
	return c.JSON(u)
}

type linuxUserRequest struct {
	Username string `json:"username"`
	FullName string `json:"full_name"`
	Email    string `json:"email"`
	Shell    string `json:"shell"`
	Sudo     bool   `json:"sudo"`
	Active   bool   `json:"active"`
	// Berechtigungsprofil; hat Vorrang vor dem alten sudo-Häkchen. Das Feld
	// fehlte hier bis 1.26 - mitgeschickt wurde es still verworfen.
	DefaultProfileID *uint `json:"default_profile_id"`
}

// Create - POST /api/v1/linux-users (linuxusers:write)
func (ctrl *LinuxUserController) Create(c fiber.Ctx) error {
	var req linuxUserRequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	u, err := ctrl.linux.Create(services.LinuxUserCreateInput{
		Username: req.Username, FullName: req.FullName, Email: req.Email,
		Shell: req.Shell, Sudo: req.Sudo, DefaultProfileID: req.DefaultProfileID,
	}, actor(c))
	if err != nil {
		return mapLinuxUserError(err)
	}
	return c.Status(fiber.StatusCreated).JSON(u)
}

// Update - PATCH /api/v1/linux-users/:id (linuxusers:write)
func (ctrl *LinuxUserController) Update(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	// PATCH-Rumpf mit Zeigern: nur mitgeschickte Felder ändern (R2-041 -
	// vorher deaktivierte ein {"sudo":true} den Benutzer still).
	var req struct {
		FullName *string `json:"full_name"`
		Email    *string `json:"email"`
		Shell    *string `json:"shell"`
		Sudo     *bool   `json:"sudo"`
		Active   *bool   `json:"active"`
		// Berechtigungsprofil; hat Vorrang vor dem alten sudo-Häkchen.
		DefaultProfileID *uint `json:"default_profile_id"`
	}
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	u, err := ctrl.linux.Update(id, services.LinuxUserUpdateInput{
		FullName: req.FullName, Email: req.Email, Shell: req.Shell,
		Sudo: req.Sudo, Active: req.Active, DefaultProfileID: req.DefaultProfileID,
	}, actor(c))
	if err != nil {
		return mapLinuxUserError(err)
	}
	return c.JSON(u)
}

type genLinuxActivationRequest struct {
	TTLHours int `json:"ttl_hours"`
	// SendEmail schickt den Link zusätzlich an die hinterlegte Adresse des
	// Linux-Benutzers. Ohne das bleibt es wie bisher: Der Administrator
	// bekommt den Link und gibt ihn selbst weiter.
	SendEmail bool `json:"send_email"`
}

// GenerateActivation - POST /api/v1/linux-users/:id/activation-links/generate
// (linuxusers:write). Liefert das einmalige Token für den Self-Service-Link.
func (ctrl *LinuxUserController) GenerateActivation(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	var req genLinuxActivationRequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	token, act, err := ctrl.linux.GenerateActivation(id, time.Duration(req.TTLHours)*time.Hour, actor(c))
	if err != nil {
		return mapLinuxUserError(err)
	}
	out := fiber.Map{"token": token, "expires_at": act.ExpiresAt}

	// Versand ist eine Option, kein Ersatz: Der Link kommt IMMER auch in der
	// Antwort zurück. Scheitert die Mail, hat der Administrator ihn trotzdem
	// vor sich und kann ihn von Hand weitergeben - der Link ist ja bereits
	// gültig. Ein harter Fehlschlag würde hier nur verschleiern, dass er
	// existiert.
	if req.SendEmail {
		out["mailed"] = true
		if err := ctrl.linux.MailActivation(id, token, act.ExpiresAt); err != nil {
			out["mailed"] = false
			out["mail_error"] = err.Error()
		}
	}
	return c.Status(fiber.StatusCreated).JSON(out)
}

type consumeLinuxActivationRequest struct {
	Token       string `json:"token"`
	Password    string `json:"password"`
	KeyName     string `json:"key_name"`
	PublicKey   string `json:"public_key"`
	GenerateKey bool   `json:"generate_key"`
}

// ConsumeActivation - POST /api/v1/linux-users/activation-links/consume (public)
// Der Mitarbeiter setzt Passwort und/oder SSH-Key. Mit generate_key erzeugt
// LCM das Schlüsselpaar und liefert den privaten Schlüssel einmalig zurück.
func (ctrl *LinuxUserController) ConsumeActivation(c fiber.Ctx) error {
	var req consumeLinuxActivationRequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	u, privateKey, err := ctrl.linux.ConsumeActivation(req.Token, services.LinuxActivationInput{
		Password: req.Password, KeyName: req.KeyName, PublicKey: req.PublicKey,
		GenerateKey: req.GenerateKey,
	})
	if err != nil {
		return mapLinuxUserError(err)
	}
	resp := fiber.Map{"status": "activated", "username": u.Username}
	if privateKey != "" {
		resp["private_key"] = privateKey
	}
	return c.JSON(resp)
}

// RemoveFromServers - POST /api/v1/linux-users/:id/remove-from-servers
// (linuxusers:write). Deprovisioniert den Benutzer aktiv von allen Servern
// (userdel) und löst die Zuordnungen - Voraussetzung fürs Löschen.
func (ctrl *LinuxUserController) RemoveFromServers(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	// Optionaler Body {server_ids:[…]}: nur die genannten Server anfassen.
	// Früher wurde das Feld still ignoriert und IMMER überall
	// deprovisioniert (R2-042).
	var req struct {
		ServerIDs []uint `json:"server_ids"`
	}
	if err := bindOptionalBody(c, &req); err != nil {
		return err
	}
	results, err := ctrl.linux.RemoveFromServers(id, req.ServerIDs, actor(c))
	if err != nil {
		return mapLinuxUserError(err)
	}
	return c.JSON(fiber.Map{"status": "deprovisioned", "results": results})
}

// Delete - DELETE /api/v1/linux-users/:id (linuxusers:write)
// Blockiert (409), solange der Benutzer noch Servern zugeordnet ist.
func (ctrl *LinuxUserController) Delete(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	if err := ctrl.linux.Delete(id, actor(c)); err != nil {
		return mapLinuxUserError(err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

type sshKeyRequest struct {
	Name      string `json:"name"`
	PublicKey string `json:"public_key"`
}

// AddKey - POST /api/v1/linux-users/:id/keys (linuxusers:write)
func (ctrl *LinuxUserController) AddKey(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	var req sshKeyRequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	key, err := ctrl.linux.AddSSHKey(id, req.Name, req.PublicKey, actor(c))
	if err != nil {
		return mapLinuxUserError(err)
	}
	return c.Status(fiber.StatusCreated).JSON(key)
}

// GenerateKey - POST /api/v1/linux-users/:id/keys/generate (linuxusers:write)
// Erzeugt serverseitig ein ed25519-Schlüsselpaar: Der Public Key wird beim
// Benutzer gespeichert, der private Schlüssel geht EINMALIG zurück (Download)
// und wird nirgends persistiert.
func (ctrl *LinuxUserController) GenerateKey(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	var req sshKeyRequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	key, privateKey, err := ctrl.linux.GenerateSSHKey(id, req.Name, actor(c))
	if err != nil {
		return mapLinuxUserError(err)
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"key": key, "private_key": privateKey})
}

// RemoveKey - DELETE /api/v1/linux-users/keys/:keyId (linuxusers:write)
func (ctrl *LinuxUserController) RemoveKey(c fiber.Ctx) error {
	id, err := paramNamedID(c, "keyId")
	if err != nil {
		return err
	}
	if err := ctrl.linux.RemoveSSHKey(id, actor(c)); err != nil {
		return mapLinuxUserError(err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// AssignGroup - POST /api/v1/linux-users/:id/assign-group (linuxusers:write)
func (ctrl *LinuxUserController) AssignGroup(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	var req struct {
		GroupID uint `json:"group_id"`
		// ProfileID ist optional: fehlt es, gilt das Standardprofil des
		// Benutzers auch auf den Servern dieser Gruppe.
		ProfileID *uint `json:"profile_id"`
	}
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	if err := ctrl.linux.AssignToGroup(scopeFor(c), id, req.GroupID, req.ProfileID, actor(c)); err != nil {
		return mapLinuxUserError(err)
	}
	return c.JSON(fiber.Map{"status": "assigned"})
}

// SetGroupProfile - POST /api/v1/linux-users/:id/group-profile (linuxusers:write)
func (ctrl *LinuxUserController) SetGroupProfile(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	var req struct {
		GroupID   uint  `json:"group_id"`
		ProfileID *uint `json:"profile_id"`
	}
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	if err := ctrl.linux.SetGroupProfile(scopeFor(c), id, req.GroupID, req.ProfileID, actor(c)); err != nil {
		return mapLinuxUserError(err)
	}
	return c.JSON(fiber.Map{"status": "updated"})
}

// GroupAssignments - GET /api/v1/linux-users/:id/group-assignments (linuxusers:read)
func (ctrl *LinuxUserController) GroupAssignments(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	rows, err := ctrl.linux.GroupAssignments(id)
	if err != nil {
		return mapLinuxUserError(err)
	}
	return c.JSON(rows)
}

// RemoveGroup - POST /api/v1/linux-users/:id/remove-group (linuxusers:write)
func (ctrl *LinuxUserController) RemoveGroup(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	var req struct {
		GroupID uint `json:"group_id"`
	}
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	if err := ctrl.linux.RemoveFromGroup(scopeFor(c), id, req.GroupID, actor(c)); err != nil {
		return mapLinuxUserError(err)
	}
	return c.JSON(fiber.Map{"status": "removed"})
}
