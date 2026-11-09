package controllers

import (
	"LCM/internal/api/apierr"
	"LCM/internal/core/services"
)

// Anmeldung, Zwei-Faktor und Aktivierungslinks.
var (
	errInvalidCredentials = apierr.Define(401, "auth.invalid_credentials", "invalid credentials")
	errLoginLocked        = apierr.Define(429, "auth.login_locked", "too many failed login attempts, try again later")
	errTOTPLocked         = apierr.Define(429, "auth.totp_locked", "too many failed 2FA attempts, try again later")
	errChallengeInvalid   = apierr.Define(401, "auth.challenge_invalid", "invalid or expired login")
	errTOTPInvalid        = apierr.Define(401, "auth.totp_invalid", "invalid 2FA code")
	errTOTPReused         = apierr.Define(401, "auth.totp_reused", "this 2FA code was already used, wait for the next one")
	errTOTPNotSetup       = apierr.Define(409, "auth.totp_not_setup", "2FA has not been set up yet")
	errTOTPAlreadyOn      = apierr.Define(409, "auth.totp_already_enabled", "2FA is already enabled")
	errPasswordIncorrect  = apierr.Define(401, "auth.password_incorrect", "the current password is incorrect")
	errPasswordForEmail   = apierr.Define(401, "auth.password_required_for_email", "the current password is required to change the email address")
	errActivationInvalid  = apierr.Define(400, "auth.activation_invalid", "the activation link is invalid")
	errActivationExpired  = apierr.Define(400, "auth.activation_expired", "the activation link has expired or was already used")
)

// Benutzerverwaltung.
var (
	errUserNotFound      = apierr.Define(404, "user.not_found", "user not found")
	errProfileForbidden  = apierr.Define(403, "user.profile_forbidden", "not allowed to edit this profile")
	errPasswordForbidden = apierr.Define(403, "user.password_forbidden", "not allowed to change this password")
	errUsernameTaken     = apierr.Define(422, "user.username_taken", "this user name is already taken")
	errEmailTaken        = apierr.Define(422, "user.email_taken", "this email address belongs to another user")
	errInvalidUsername   = apierr.Define(422, "user.invalid_username", "user name must be 3-32 characters (a-z, 0-9, _, -)")
	errReservedUsername  = apierr.Define(422, "user.reserved_username", "this user name is reserved")
	errUnknownRole       = apierr.Define(422, "user.unknown_role", "unknown role")
	errProtectedUser     = apierr.Define(403, "user.protected", "system users cannot be changed or deleted")
	errLastAdmin         = apierr.Define(403, "user.last_admin", "the last active administrator cannot be locked or demoted")
	errWeakPassword      = apierr.Define(422, "password.too_weak", "the password does not meet the security requirements")
)

// API-Schlüssel.
var (
	errInvalidScope  = apierr.Define(422, "apikey.invalid_scope", "invalid scope, allowed: read, readwrite, mcp")
	errInvalidExpiry = apierr.Define(400, "apikey.invalid_expiry", "expires_in_days must be positive, omit the field for a key without expiry")
)

// userErrors ordnet die Fehler des Benutzer-Dienstes ihren API-Fehlern zu.
var userErrors = []sentinelCode{
	{services.ErrUsernameTaken, errUsernameTaken},
	{services.ErrEmailTaken, errEmailTaken},
	{services.ErrWeakPassword, errWeakPassword},
	{services.ErrInvalidUsername, errInvalidUsername},
	{services.ErrReservedUsername, errReservedUsername},
	{services.ErrUnknownRole, errUnknownRole},
	{services.ErrInvalidScope, errInvalidScope},
	{services.ErrProtectedUser, errProtectedUser},
	{services.ErrLastAdmin, errLastAdmin},
}

// twoFactorErrors ordnet die Fehler der Zwei-Faktor-Verwaltung zu.
var twoFactorErrors = []sentinelCode{
	{services.ErrTOTPReused, errTOTPReused},
	{services.ErrTOTPInvalid, errTOTPInvalid},
	{services.ErrTOTPNotSetup, errTOTPNotSetup},
	{services.ErrTOTPAlreadyOn, errTOTPAlreadyOn},
}

// activationErrors ordnet die Fehler der Aktivierungslinks zu.
var activationErrors = []sentinelCode{
	{services.ErrActivationInvalid, errActivationInvalid},
	{services.ErrActivationExpired, errActivationExpired},
}
