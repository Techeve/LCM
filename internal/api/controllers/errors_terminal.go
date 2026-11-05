package controllers

import (
	"LCM/internal/api/apierr"
	"LCM/internal/core/services"
)

// Web-Konsole.
var (
	errTerminalDisabled      = apierr.Define(403, "terminal.disabled", "the web console is disabled")
	errTerminalUnsupported   = apierr.Define(400, "terminal.unsupported", "a console is not possible on this server")
	errTooManyConsoles       = apierr.Define(409, "terminal.too_many_consoles", "too many consoles are already open on this server")
	errTerminalTicketFailed  = apierr.Define(500, "terminal.ticket_failed", "console ticket could not be created")
	errTerminalTicketInvalid = apierr.Define(401, "terminal.ticket_invalid", "invalid or expired console ticket")
	errTerminalUpgradeFailed = apierr.Define(400, "terminal.upgrade_failed", "websocket upgrade failed")
)

// terminalErrors ordnet die Absagen der Konsole ihren API-Fehlern zu.
var terminalErrors = []sentinelCode{
	{services.ErrTerminalDisabled, errTerminalDisabled},
	{services.ErrTerminalNotPossible, errTerminalUnsupported},
	{services.ErrTerminalUnsupported, errTerminalUnsupported},
	{services.ErrTooManyConsoles, errTooManyConsoles},
	{services.ErrTooManyConnections, errConnectionLimit},
}
