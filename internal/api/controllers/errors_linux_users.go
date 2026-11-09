package controllers

import (
	"LCM/internal/api/apierr"
	"LCM/internal/core/services"
)

// Linux-Benutzer und ihre Konten auf den Servern.
var (
	errLinuxUserNotFound    = apierr.Define(404, "linux_user.not_found", "linux user not found")
	errLinuxUsernameInvalid = apierr.Define(422, "linux_user.invalid_username", "invalid linux user name")
	errLinuxUsernameReserve = apierr.Define(422, "linux_user.reserved_username", "this user name is reserved")
	errLinuxUsernameTaken   = apierr.Define(422, "linux_user.username_taken", "this linux user name is already taken")
	errLinuxShellInvalid    = apierr.Define(422, "linux_user.invalid_shell", "invalid shell, expected an absolute path")
	errLinuxSSHKeyInvalid   = apierr.Define(422, "linux_user.invalid_ssh_key", "invalid SSH public key, expected OpenSSH format")
	errLinuxEmptyActivation = apierr.Define(422, "linux_user.activation_empty", "a password or an SSH key must be set")
	errLinuxUserAssigned    = apierr.Define(409, "linux_user.still_assigned", "the user is still assigned to servers, remove it from all servers first")

	errServerUserSyncOff     = apierr.Define(409, "server_user.sync_disabled", "linux user sync is disabled for this server")
	errServerUserProtected   = apierr.Define(409, "server_user.protected", "this account is protected and cannot be changed in the user overview")
	errServerUserManaged     = apierr.Define(409, "server_user.managed", "this account is distributed by LCM, remove it through the linux user management")
	errServerUsernameInvalid = apierr.Define(400, "server_user.invalid_username", "invalid user name")
)

// linuxUserErrors ordnet die Fehler der Linux-Benutzer-Verwaltung zu.
var linuxUserErrors = concat(
	[]sentinelCode{
		{services.ErrInvalidLinuxUsername, errLinuxUsernameInvalid},
		{services.ErrReservedLinuxUsername, errLinuxUsernameReserve},
		{services.ErrInvalidShell, errLinuxShellInvalid},
		{services.ErrLinuxUsernameTaken, errLinuxUsernameTaken},
		{services.ErrInvalidSSHKey, errLinuxSSHKeyInvalid},
		{services.ErrWeakPassword, errWeakPassword},
		{services.ErrEmptyActivation, errLinuxEmptyActivation},
		{services.ErrLinuxUserStillOnServers, errLinuxUserAssigned},
	},
	activationErrors,
)

// provisioningErrors ordnet die Fehler der Konten-Verteilung zu.
var provisioningErrors = concat(
	[]sentinelCode{
		{services.ErrWeakPassword, errWeakPassword},
		{services.ErrProxmoxRestricted, errProxmoxRestricted},
		{services.ErrRestrictedSudo, errRestrictedSudo},
		{services.ErrUserSyncDisabled, errServerUserSyncOff},
		{services.ErrServerUserProtected, errServerUserProtected},
		{services.ErrServerUserManaged, errServerUserManaged},
		{services.ErrInvalidServerUsername, errServerUsernameInvalid},
	},
	activationErrors,
)
