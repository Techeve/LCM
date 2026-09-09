// Package controllers enthält die Fiber-Handler (HTTP-Transport-Schicht).
// Controller validieren Input, delegieren an Services und formen die
// JSON-Response - Business-Logik gehört NICHT hierher.
package controllers

import (
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"LCM/internal/api/middlewares"
	"LCM/internal/core/domain"
	"LCM/internal/core/services"
)

// AuthController bedient Login (inkl. 2FA), Self-Service-Passwort-Reset
// und "Wer bin ich?"-Abfragen.
type AuthController struct {
	auth       *services.AuthService
	totp       *services.TOTPService
	settings   *services.SettingsService
	activation *services.ActivationService
	// guard sperrt Konten nach zu vielen Fehlversuchen (Brute-Force-Schutz,
	// wirkt auch bei über viele IPs verteilten Angriffen). App-weiter
	// Singleton, da der Controller einmal pro App gebaut wird.
	guard *loginGuard
}

func NewAuthController(auth *services.AuthService, totp *services.TOTPService, settings *services.SettingsService, activation *services.ActivationService, guard *loginGuard) *AuthController {
	if guard == nil {
		guard = newLoginGuard()
	}
	return &AuthController{auth: auth, totp: totp, settings: settings, activation: activation, guard: guard}
}

// NewLoginGuard erzeugt den app-weiten Brute-Force-Zähler. Der Router baut ihn
// EINMAL und gibt ihn an alle Controller, die Anmelde- oder TOTP-Versuche
// prüfen - nur so greift eine Sperre endpunktübergreifend.
func NewLoginGuard() *loginGuard { return newLoginGuard() }

// clientIP liefert die maßgebliche Client-Adresse für Brute-Force-Sperren -
// dieselbe wie für IP-Allowlist und Protokolle (middlewares.ResolveClientIP).
func (ctrl *AuthController) clientIP(c fiber.Ctx) string {
	return middlewares.ClientIP(c)
}

// RequestPasswordReset - POST /api/v1/auth/password-reset (öffentlich)
// Stößt den Self-Service-Passwort-Reset per E-Mail an. Die Antwort ist
// IMMER dieselbe - ob die Adresse existiert, ist nach außen nicht erkennbar
// (keine User-Enumeration). Pro Client-IP gedrosselt: hier zählt JEDER
// Aufruf als Versuch, nicht nur Fehlversuche - der Endpunkt ist anonym und
// löst Mail-Versand aus.
func (ctrl *AuthController) RequestPasswordReset(c fiber.Ctx) error {
	var req struct {
		Email string `json:"email"`
	}
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	// Prüfen und Zählen in einem Schritt (kein Race zwischen beidem).
	key := "pwreset:" + ctrl.clientIP(c)
	if locked, retry := ctrl.guard.beginAttempt(key, maxLoginFails); locked {
		c.Set("Retry-After", retryAfterSeconds(retry))
		return fiber.NewError(fiber.StatusTooManyRequests,
			"zu viele Anfragen - bitte später erneut versuchen")
	}
	if err := ctrl.activation.RequestPasswordReset(req.Email); err != nil {
		return err
	}
	// Bewusst ohne die Adresse: sie gehört einem Dritten, und ob sie
	// existiert, verrät auch das Protokoll nicht.
	middlewares.SecurityEvent(c, "password.reset.requested")
	return c.JSON(fiber.Map{"status": "ok"})
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token string       `json:"token"`
	User  *userProfile `json:"user"`
}

// userProfile ist die API-Sicht auf einen User (ohne interne Felder).
type userProfile struct {
	ID                 uint     `json:"id"`
	Username           string   `json:"username"`
	Email              string   `json:"email"`
	FirstName          string   `json:"first_name"`
	LastName           string   `json:"last_name"`
	DisplayName        string   `json:"display_name"`
	Roles              []string `json:"roles"`
	Permissions        []string `json:"permissions"`
	TOTPEnabled        bool     `json:"totp_enabled"`
	MustChangePassword bool     `json:"must_change_password"`
}

func toProfile(u *domain.User) *userProfile {
	return &userProfile{
		ID:                 u.ID,
		Username:           u.Username,
		Email:              u.Email,
		FirstName:          u.FirstName,
		LastName:           u.LastName,
		DisplayName:        u.FullName(),
		Roles:              u.RoleNames(),
		Permissions:        u.PermissionCodes(),
		TOTPEnabled:        u.TOTPEnabled,
		MustChangePassword: u.MustChangePassword,
	}
}

// Login - POST /api/v1/auth/login
// Erster Schritt: Prüft Credentials. Hat der User 2FA aktiviert, wird
// KEIN reguläres Token ausgegeben, sondern eine Challenge - der zweite
// Schritt (/auth/login/2fa) verifiziert den TOTP-Code.
func (ctrl *AuthController) Login(c fiber.Ctx) error {
	var req loginRequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	if req.Username == "" || req.Password == "" {
		return fiber.NewError(fiber.StatusBadRequest, "username und password sind erforderlich")
	}

	// Brute-Force-Schutz auf ZWEI Schlüsseln gleichzeitig:
	//   - pro Client-IP: stoppt schnelles Durchprobieren von einer Quelle.
	//   - pro KONTO: stoppt Password-Spraying, das über viele IPs verteilt
	//     wird und die IP-Sperre damit gezielt unterläuft.
	// Die Kontoschwelle liegt höher (maxAccountFails) und die Sperre ist
	// gedeckelt, damit ein Angreifer ein fremdes Konto nicht billig aussperren
	// kann (Account-Lockout-DoS).
	//
	// Die Client-IP kommt aus middlewares.ClientIP - hinter einem Reverse-Proxy
	// wäre die rohe Peer-Adresse für ALLE Clients dieselbe, fünf Fehlversuche
	// hätten dann die Anmeldung der gesamten Installation gesperrt.
	ipKey := "ip:" + ctrl.clientIP(c)
	userKey := "user:" + req.Username
	if locked, retry := ctrl.guard.beginAttempt(ipKey, maxLoginFails); locked {
		return ctrl.loginLocked(c, retry, req.Username)
	}
	if locked, retry := ctrl.guard.beginAttempt(userKey, maxAccountFails); locked {
		return ctrl.loginLocked(c, retry, req.Username)
	}

	user, err := ctrl.auth.VerifyPassword(req.Username, req.Password)
	if err != nil {
		// Der Versuch ist durch beginAttempt bereits verbucht.
		if errors.Is(err, services.ErrInvalidCredentials) || errors.Is(err, services.ErrUserInactive) {
			middlewares.SecurityEvent(c, "login.failed", "user", req.Username)
			return fiber.NewError(fiber.StatusUnauthorized, "ungültige Anmeldedaten")
		}
		return err
	}
	ctrl.guard.reset(ipKey)
	ctrl.guard.reset(userKey)

	if user.TOTPEnabled {
		challenge, err := ctrl.auth.IssueChallenge(user)
		if err != nil {
			return err
		}
		middlewares.SecurityEvent(c, "login.password.ok", "user", user.Username)
		return c.JSON(fiber.Map{"twofa_required": true, "challenge": challenge})
	}

	// 2FA-Enforcement: Wird für die Rollen des Users 2FA erzwungen, hat er
	// es aber noch nicht eingerichtet, bekommt er trotzdem ein Token, muss
	// aber sofort die Einrichtung durchlaufen (must_setup_2fa signalisiert
	// das dem Frontend).
	mustSetup := false
	if settings, err := ctrl.settings.Get(); err == nil {
		mustSetup = settings.Requires2FA(user.RoleNames())
	}

	token, err := ctrl.auth.CompleteLogin(user)
	if err != nil {
		return err
	}
	middlewares.SecurityEvent(c, "login.ok", "user", user.Username)
	return c.JSON(fiber.Map{"token": token, "user": toProfile(user), "must_setup_2fa": mustSetup})
}

// loginLocked beantwortet einen Anmeldeversuch in laufender Sperre.
func (ctrl *AuthController) loginLocked(c fiber.Ctx, retry time.Duration, username string) error {
	c.Set("Retry-After", retryAfterSeconds(retry))
	middlewares.SecurityEvent(c, "login.locked", "user", username)
	return fiber.NewError(fiber.StatusTooManyRequests,
		"zu viele fehlgeschlagene Anmeldeversuche - bitte später erneut versuchen")
}

type verify2FARequest struct {
	Challenge string `json:"challenge"`
	Code      string `json:"code"`
}

// LoginTOTP - POST /api/v1/auth/login/2fa
// Zweiter Schritt: Verifiziert den TOTP-Code gegen die Challenge und
// liefert das finale JWT.
func (ctrl *AuthController) LoginTOTP(c fiber.Ctx) error {
	var req verify2FARequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	user, err := ctrl.auth.ValidateChallenge(req.Challenge)
	if err != nil {
		return fiber.NewError(fiber.StatusUnauthorized, "ungültige oder abgelaufene Anmeldung")
	}
	if locked, retry := ctrl.guard.beginAttempt(totpGuardKey(user.ID), maxLoginFails); locked {
		c.Set("Retry-After", retryAfterSeconds(retry))
		middlewares.SecurityEvent(c, "login.2fa.locked", "user", user.Username)
		return fiber.NewError(fiber.StatusTooManyRequests,
			"zu viele fehlgeschlagene 2FA-Versuche - bitte später erneut versuchen")
	}
	if err := ctrl.totp.Verify(user.ID, req.Code); err != nil {
		middlewares.SecurityEvent(c, "login.2fa.failed", "user", user.Username)
		return fiber.NewError(fiber.StatusUnauthorized, "ungültiger 2FA-Code")
	}
	ctrl.guard.reset(totpGuardKey(user.ID))
	// Die Challenge ist mit diesem Schritt verbraucht. Ohne Widerruf bliebe
	// sie bis zu ihrem Ablauf gültig - wer sie mitgelesen hat, könnte mit dem
	// nächsten Code eine zweite Sitzung eröffnen.
	ctrl.auth.RevokeToken(req.Challenge)
	token, err := ctrl.auth.CompleteLogin(user)
	if err != nil {
		return err
	}
	middlewares.SecurityEvent(c, "login.ok", "user", user.Username, "second_factor", "totp")
	return c.JSON(loginResponse{Token: token, User: toProfile(user)})
}

// SetupTOTP - POST /api/v1/auth/2fa/setup (RequireAuth)
func (ctrl *AuthController) SetupTOTP(c fiber.Ctx) error {
	user := middlewares.CurrentUser(c)
	res, err := ctrl.totp.Setup(user.ID)
	if err != nil {
		if errors.Is(err, services.ErrTOTPAlreadyOn) {
			return fiber.NewError(fiber.StatusConflict, err.Error())
		}
		return err
	}
	return c.JSON(res)
}

type totpCodeRequest struct {
	Code string `json:"code"`
}

// EnableTOTP - POST /api/v1/auth/2fa/enable (RequireAuth)
func (ctrl *AuthController) EnableTOTP(c fiber.Ctx) error {
	user := middlewares.CurrentUser(c)
	var req totpCodeRequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	if err := ctrl.totp.Enable(user.ID, req.Code); err != nil {
		return map2FAError(err)
	}
	middlewares.SecurityEvent(c, "2fa.enabled.ok", "user", user.Username)
	return c.JSON(fiber.Map{"status": "enabled"})
}

// DisableTOTP - POST /api/v1/auth/2fa/disable (RequireAuth)
//
// Ebenfalls gegen Brute-Force gesperrt: Wer ein Token besitzt (aber nicht den
// zweiten Faktor), könnte den 6-stelligen Code sonst ungebremst durchprobieren
// und 2FA damit abschalten - der Endpunkt wäre die schwächste Stelle der
// gesamten 2FA-Kette gewesen.
func (ctrl *AuthController) DisableTOTP(c fiber.Ctx) error {
	user := middlewares.CurrentUser(c)
	var req totpCodeRequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "ungültiger Request-Body")
	}
	if locked, retry := ctrl.guard.beginAttempt(totpGuardKey(user.ID), maxLoginFails); locked {
		c.Set("Retry-After", retryAfterSeconds(retry))
		return fiber.NewError(fiber.StatusTooManyRequests,
			"zu viele fehlgeschlagene 2FA-Versuche - bitte später erneut versuchen")
	}
	if err := ctrl.totp.Disable(user.ID, req.Code); err != nil {
		if errors.Is(err, services.ErrTOTPInvalid) {
			middlewares.SecurityEvent(c, "2fa.disable.failed", "user", user.Username)
		}
		return map2FAError(err)
	}
	ctrl.guard.reset(totpGuardKey(user.ID))
	middlewares.SecurityEvent(c, "2fa.disabled", "user", user.Username)
	return c.JSON(fiber.Map{"status": "disabled"})
}

// Logout - POST /api/v1/auth/logout (RequireAuth)
// Entwertet das verwendete Token SERVERSEITIG bis zu seinem regulären
// Ablauf. Vorher war der Endpunkt ein No-Op - der Client verwarf sein
// Token, aber wer es abgefangen hatte, konnte es weiterbenutzen (R2-059).
func (ctrl *AuthController) Logout(c fiber.Ctx) error {
	if token, ok := strings.CutPrefix(c.Get("Authorization"), "Bearer "); ok {
		ctrl.auth.RevokeToken(strings.TrimSpace(token))
	}
	return c.JSON(fiber.Map{"status": "logged_out"})
}

// Me - GET /api/v1/auth/me (RequireAuth)
func (ctrl *AuthController) Me(c fiber.Ctx) error {
	return c.JSON(toProfile(middlewares.CurrentUser(c)))
}

func map2FAError(err error) error {
	switch {
	case errors.Is(err, services.ErrTOTPInvalid):
		return fiber.NewError(fiber.StatusUnauthorized, err.Error())
	case errors.Is(err, services.ErrTOTPNotSetup), errors.Is(err, services.ErrTOTPAlreadyOn):
		return fiber.NewError(fiber.StatusConflict, err.Error())
	default:
		return err
	}
}
