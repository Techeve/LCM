package middlewares

import "LCM/internal/api/apierr"

// Fehler der Middlewares. Die allgemeinen (Anmeldung nötig, Recht fehlt,
// Rate-Limit) kommen aus apierr.
var (
	errMCPKeyOnREST    = apierr.Define(403, "auth.mcp_key_not_allowed", "the MCP API key is only valid for the MCP interface, not for the REST API")
	errKeyReadOnly     = apierr.Define(403, "auth.key_read_only", "the API key has read-only scope")
	errPasswordChange  = apierr.Define(403, "auth.password_change_required", "the password must be changed first, the account is locked until then")
	errTOTPSetupNeeded = apierr.Define(403, "auth.totp_setup_required", "two-factor authentication is mandatory for this role and must be set up first")
	errIPDenied        = apierr.Define(403, "access.ip_denied", "access denied")
	errDemoDisabled    = apierr.Define(403, "demo.disabled", "disabled in this public demo")
	errLengthRequired  = apierr.Define(411, "request.length_required", "Content-Length is required, bodies without length are only allowed for the backup upload")
	errBodyTooLarge    = apierr.Define(413, "request.body_too_large", "request body too large")
)
