package controllers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/gofiber/fiber/v3"

	"LCM/internal/api/middlewares"
	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/storage/repositories"
)

// UserController verwaltet User und Rollen (Admin-Funktionen).
type UserController struct {
	users *services.UserService
	totp  *services.TOTPService // für die 2FA-Prüfung beim Self-Passwortwechsel
	guard *loginGuard           // geteilt mit dem AuthController (TOTP-Brute-Force)
}

// NewUserController: guard ist der GETEILTE Brute-Force-Zähler (siehe
// NewLoginGuard). TOTP-Codes werden an mehreren Endpunkten geprüft - sie
// müssen sich denselben Zähler teilen, sonst wechselt ein Angreifer bei
// Sperre einfach den Endpunkt.
func NewUserController(users *services.UserService, totp *services.TOTPService, guard *loginGuard) *UserController {
	if guard == nil {
		guard = newLoginGuard()
	}
	return &UserController{users: users, totp: totp, guard: guard}
}

// List - GET /api/v1/users (users:read)
func (ctrl *UserController) List(c fiber.Ctx) error {
	users, err := ctrl.users.ListUsers()
	if err != nil {
		return err
	}
	return c.JSON(users)
}

// Get - GET /api/v1/users/:id (users:read)
func (ctrl *UserController) Get(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	user, err := ctrl.users.GetUser(id)
	if err != nil {
		return mapServiceError(err)
	}
	return c.JSON(user)
}

type createUserRequest struct {
	Username  string   `json:"username"`
	Email     string   `json:"email"`
	Password  string   `json:"password"`
	FirstName string   `json:"first_name"`
	LastName  string   `json:"last_name"`
	Roles     []string `json:"roles"`
}

// Create - POST /api/v1/users (users:write)
func (ctrl *UserController) Create(c fiber.Ctx) error {
	var req createUserRequest
	// STRIKT: unbekannte Felder abweisen. Vorher wurde z.B. role_ids still
	// verworfen und der Benutzer OHNE jede Rolle angelegt (R2-037).
	if err := strictJSON(c.Body(), &req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	user, err := ctrl.users.CreateUser(req.Username, req.Email, req.Password, req.FirstName, req.LastName, req.Roles, actor(c))
	if err != nil {
		return mapServiceError(err)
	}
	return c.Status(fiber.StatusCreated).JSON(user)
}

type updateRolesRequest struct {
	Roles []string `json:"roles"`
}

type updateProfileRequest struct {
	Email     string `json:"email"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

// UpdateProfile - PATCH /api/v1/users/:id/profile
// Admins (users:write) dürfen jeden bearbeiten; jeder eingeloggte User
// darf sein EIGENES Profil bearbeiten.
func (ctrl *UserController) UpdateProfile(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	if !canEditUser(c, id) {
		return fiber.NewError(fiber.StatusForbidden, "keine Berechtigung, dieses Profil zu bearbeiten")
	}
	var req updateProfileRequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	user, err := ctrl.users.UpdateProfile(id, req.Email, req.FirstName, req.LastName, actor(c))
	if err != nil {
		return mapServiceError(err)
	}
	return c.JSON(user)
}

type resetPasswordRequest struct {
	Password        string `json:"password"`
	CurrentPassword string `json:"current_password"` // nur beim Self-Service nötig
	Code            string `json:"code"`             // 2FA-Code, wenn TOTP aktiv
}

// ResetPassword - POST /api/v1/users/:id/reset-password
// Admins dürfen jedes Passwort zurücksetzen; jeder User sein eigenes. Beim
// eigenen Passwort (Self-Service) ist eine Re-Authentifizierung nötig: das
// AKTUELLE Passwort (und bei aktivem TOTP ein gültiger Code) - so kann ein
// gekapertes Token allein den Account nicht übernehmen.
func (ctrl *UserController) ResetPassword(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	if !canEditUser(c, id) {
		return fiber.NewError(fiber.StatusForbidden, "keine Berechtigung, dieses Passwort zu ändern")
	}
	var req resetPasswordRequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}

	caller := middlewares.CurrentUser(c)
	// Setzt der Nutzer sein EIGENES Passwort? Dann ist der Wechselzwang
	// erledigt; setzt ein Admin ein fremdes, bleibt er bestehen (ein Dritter
	// kennt das Passwort - siehe UserService.ResetPassword).
	selfService := caller != nil && caller.ID == id
	if selfService {
		ok, err := ctrl.users.CheckPassword(id, req.CurrentPassword)
		if err != nil || !ok {
			return fiber.NewError(fiber.StatusUnauthorized, "das aktuelle Passwort ist nicht korrekt")
		}
		if caller.TOTPEnabled {
			// Auch hier gegen Brute-Force gesperrt - derselbe kontobezogene
			// Schlüssel wie Login und 2FA-Deaktivierung, damit ein Angreifer
			// die Sperre nicht durch Wechsel des Endpunkts umgehen kann.
			if locked, retry := ctrl.guard.beginAttempt(totpGuardKey(id), maxLoginFails); locked {
				c.Set("Retry-After", retryAfterSeconds(retry))
				return fiber.NewError(fiber.StatusTooManyRequests,
					"zu viele fehlgeschlagene 2FA-Versuche - bitte später erneut versuchen")
			}
			if err := ctrl.totp.Verify(id, req.Code); err != nil {
				return fiber.NewError(fiber.StatusUnauthorized, "ungültiger 2FA-Code")
			}
			ctrl.guard.reset(totpGuardKey(id))
		}
	}

	if err := ctrl.users.ResetPassword(id, req.Password, selfService, actor(c)); err != nil {
		return mapServiceError(err)
	}
	return c.JSON(fiber.Map{"status": "password_reset"})
}

// canEditUser erlaubt die Bearbeitung, wenn der Aufrufer users:write hat
// ODER seinen eigenen Datensatz bearbeitet.
func canEditUser(c fiber.Ctx, targetID uint) bool {
	user := middlewares.CurrentUser(c)
	if user == nil {
		return false
	}
	if user.ID == targetID {
		return true
	}
	return user.HasPermission(domain.PermUsersWrite)
}

// UpdateRoles - PUT /api/v1/users/:id/roles (roles:write)
func (ctrl *UserController) UpdateRoles(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	var req updateRolesRequest
	if err := strictJSON(c.Body(), &req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	user, err := ctrl.users.UpdateUserRoles(id, req.Roles, actor(c))
	if err != nil {
		return mapServiceError(err)
	}
	return c.JSON(user)
}

// SetActive - PATCH /api/v1/users/:id/active (users:write)
// Sperrt/entsperrt ein Konto, ohne es zu löschen (R2-036).
func (ctrl *UserController) SetActive(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	var req struct {
		Active *bool `json:"active"`
	}
	if err := c.Bind().Body(&req); err != nil || req.Active == nil {
		return fiber.NewError(fiber.StatusBadRequest, "Feld 'active' (true/false) ist erforderlich")
	}
	user, err := ctrl.users.SetActive(id, *req.Active, actor(c))
	if err != nil {
		return mapServiceError(err)
	}
	return c.JSON(user)
}

// Delete - DELETE /api/v1/users/:id (users:write)
func (ctrl *UserController) Delete(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return err
	}
	if err := ctrl.users.DeleteUser(id, actor(c)); err != nil {
		return mapServiceError(err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ListRoles - GET /api/v1/roles (roles:read)
func (ctrl *UserController) ListRoles(c fiber.Ctx) error {
	roles, err := ctrl.users.ListRoles()
	if err != nil {
		return err
	}
	return c.JSON(roles)
}

// paramID parst den :id-Pfadparameter.
func paramID(c fiber.Ctx) (uint, error) {
	return paramNamedID(c, "id")
}

// bindOptionalBody liest einen Request-Body, dessen Felder alle optional sind.
//
// Ein LEERER Rumpf ist dabei kein Fehler, sondern gleichbedeutend mit "{}" -
// sonst scheitert schon das Parsen, und die eigentliche, oft sehr hilfreiche
// Fachmeldung des Services kommt nie zum Zug. Beim Reconnect etwa unterschied
// nur ein fehlendes "{}" zwischen dem nichtssagenden "ungültiger Request-Body"
// und dem Hinweis, dass der Server keine Passwort-Anmeldung anbietet und man
// den System-SSH-Key nutzen soll (BUG-029).
// strictJSON dekodiert einen JSON-Body und weist unbekannte Felder ab -
// gegen das stille Verwerfen falsch benannter Schlüssel (R2-037/R2-050).
func strictJSON(body []byte, out any) error {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("ungültiger Request-Body: %w", err)
	}
	return nil
}

func bindOptionalBody(c fiber.Ctx, out any) error {
	if len(c.Body()) == 0 {
		return nil
	}
	if err := c.Bind().Body(out); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	return nil
}

// paramStrID liest den Pfadparameter "id" als String - für Entitäten mit
// UUID-Primärschlüssel (Jobs, SSH-Sessions, …). Nur auf Nicht-Leere geprüft.
func paramStrID(c fiber.Ctx) (string, error) {
	id := c.Params("id")
	if id == "" {
		return "", fiber.NewError(fiber.StatusBadRequest, "ungültige ID")
	}
	return id, nil
}

// paramNamedID parst einen benannten Pfadparameter als uint.
func paramNamedID(c fiber.Ctx, name string) (uint, error) {
	id, err := strconv.ParseUint(c.Params(name), 10, 32)
	if err != nil {
		return 0, fiber.NewError(fiber.StatusBadRequest, "ungültige ID")
	}
	return uint(id), nil
}

// mapServiceError übersetzt Service-/Repository-Fehler in HTTP-Statuscodes.
func mapServiceError(err error) error {
	switch {
	case errors.Is(err, repositories.ErrNotFound):
		return fiber.NewError(fiber.StatusNotFound, "nicht gefunden")
	case errors.Is(err, services.ErrUsernameTaken),
		errors.Is(err, services.ErrEmailTaken),
		errors.Is(err, services.ErrWeakPassword),
		errors.Is(err, services.ErrInvalidUsername),
		errors.Is(err, services.ErrReservedUsername),
		errors.Is(err, services.ErrUnknownRole),
		errors.Is(err, services.ErrInvalidScope):
		return fiber.NewError(fiber.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, services.ErrProtectedUser), errors.Is(err, services.ErrLastAdmin):
		return fiber.NewError(fiber.StatusForbidden, err.Error())
	default:
		return err
	}
}
