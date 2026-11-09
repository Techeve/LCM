package controllers

import (
	"LCM/internal/api/apierr"
	"LCM/internal/core/services"
)

// Jobs, Protokolle, Dokumentation und Agent-Download.
var (
	errJobNotRunning    = apierr.Define(409, "job.not_running", "this job is not running (any more)")
	errLogsNotConfigure = apierr.Define(404, "logs.not_configured", "no file log is configured")
	errLogsUnreadable   = apierr.Define(500, "logs.unreadable", "the log file cannot be read")
	errSSHLogNotFound   = apierr.Define(404, "ssh_log.not_found", "log not found")
	errSSHLogForbidden  = apierr.Define(403, "ssh_log.forbidden", "no access to this log")
	errDocsNotFound     = apierr.Define(404, "docs.not_found", "page not found")
	errAgentArch        = apierr.Define(400, "agent.invalid_arch", "arch must be amd64 or arm64")
	errAgentBinary      = apierr.Define(404, "agent.binary_missing", "the agent binary is not available, it is installed with the lcm Debian package")
)

// sshLogErrors ordnet die Fehler der SSH-Protokolle zu.
var sshLogErrors = []sentinelCode{
	{services.ErrLogForbidden, errSSHLogForbidden},
}
