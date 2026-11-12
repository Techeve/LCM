package controllers

import (
	"LCM/internal/api/apierr"
	"LCM/internal/core/domain"
	"LCM/internal/core/services"
	"LCM/internal/infrastructure/sshx"
)

// Server verwalten und erreichen.
var (
	errServerNotFound         = apierr.Define(404, "server.not_found", "server not found")
	errServerNameTaken        = apierr.Define(409, "server.name_taken", "a server with this name already exists")
	errServerHostTaken        = apierr.Define(409, "server.host_taken", "this server is already registered")
	errServerBusy             = apierr.Define(409, "server.busy", "a job is already running on this server")
	errFingerprintMismatch    = apierr.Define(400, "server.fingerprint_mismatch", "host key fingerprint differs from the confirmed value")
	errHostKeyMismatch        = apierr.Define(400, "server.host_key_mismatch", "SSH host key does not match the stored fingerprint")
	errLoopbackInContainer    = apierr.Define(400, "server.loopback_in_container", "localhost is the container itself, use the network address of the Docker host")
	errPasswordAuthOff        = apierr.Define(400, "server.password_auth_unavailable", "the server does not offer password login")
	errPasswordRejected       = apierr.Define(400, "server.password_rejected", "login rejected, wrong user name or password")
	errNoOnboardingKey        = apierr.Define(400, "server.no_onboarding_key", "no system onboarding SSH key available")
	errNotRevertible          = apierr.Define(400, "server.not_revertible", "this package source is not marked for reverting")
	errNoRevertCandidates     = apierr.Define(409, "server.no_revert_candidates", "no package source to revert")
	errRepoNotSecurable       = apierr.Define(400, "server.repo_not_securable", "this package source is not an unencrypted source of this server")
	errHTTPSCheckTimeout      = apierr.Define(504, "server.https_check_timeout", "the server did not finish the HTTPS check in time")
	errNotLcmHost             = apierr.Define(400, "server.not_lcm_host", "action is only available for the LCM host")
	errLcmHostNotApt          = apierr.Define(400, "server.lcm_host_not_apt", "automatic setup works only on Debian and Ubuntu (apt)")
	errLcmHostInContainer     = apierr.Define(400, "server.lcm_host_in_container", "LCM runs in a container, this setup is not possible here")
	errProxmoxRestricted      = apierr.Define(409, "server.proxmox_restricted", "not available on Proxmox systems")
	errRouterOSUnsupported    = apierr.Define(409, "server.routeros_unsupported", "not available for MikroTik RouterOS")
	errDSMUnsupported         = apierr.Define(409, "server.dsm_unsupported", "not available for Synology DSM")
	errNotDSM                 = apierr.Define(409, "server.not_dsm", "only available for Synology DSM")
	errDSMNotSettable         = apierr.Define(422, "dsm.not_settable", "LCM does not set this DSM setting")
	errDSMFirewallNotSettable = apierr.Define(422, "dsm.firewall_not_settable", "LCM does not switch the DSM firewall, its rules stay with DSM")
	errFirewallToolMissing    = apierr.Define(409, "server.firewall_tool_missing", "the firewall tool is not installed on this system")
	errRestrictedSudo         = apierr.Define(409, "server.restricted_sudo", "not available in restricted sudo mode")
	errAgentTransport         = apierr.Define(409, "server.agent_unsupported", "not available for agent servers")
	errNotAgentServer         = apierr.Define(409, "server.not_agent", "not an agent server")
	errAgentOffline           = apierr.Define(409, "server.agent_offline", "agent is not connected")
	errNoOldKernels           = apierr.Define(409, "server.no_old_kernels", "no old kernels to remove")
	errKernelCleanupMissing   = apierr.Define(409, "server.kernel_cleanup_unsupported", "removing old kernels is only possible on apt systems")
	errNoSnapd                = apierr.Define(409, "server.no_snapd", "snapd is not present on this server")
	errCapabilityNotFailed    = apierr.Define(409, "server.capability_not_failed", "this category has not failed, nothing to retry")
	errNotRetryable           = apierr.Define(409, "server.not_retryable", "the failed action cannot be retried")
	errRetryUnavailable       = apierr.Define(409, "server.retry_unavailable", "retrying is not available on this instance")
	errUnknownRepairAction    = apierr.Define(400, "server.unknown_repair_action", "unknown repair action")
	errRepairUnsupported      = apierr.Define(400, "server.repair_unsupported", "this repair is not supported for the package manager of this server")
	errUnknownCapability      = apierr.Define(400, "server.unknown_capability", "unknown capability category")
	errPreviewUnknownAction   = apierr.Define(404, "server.preview_unknown_action", "unknown action for the command preview")
	errPreviewUnavailable     = apierr.Define(409, "server.preview_unavailable", "the commands of this action are not known in advance")
	errVolumeNotMonitorable   = apierr.Define(400, "server.volume_not_monitorable", "this volume cannot be monitored")
	errNoPathsSelected        = apierr.Define(400, "server.no_paths_selected", "no directories selected")
	errServerActionFailed     = apierr.Define(502, "server.action_failed", "the action failed on the target system")
	errServerProbeFailed      = apierr.Define(502, "server.probe_failed", "the server could not be reached")
	errConnectionLimit        = apierr.Define(409, "server.connection_limit", "the server is busy with another connection, please wait a moment")
	errNTPNotSynced           = apierr.Define(502, "time.ntp_not_synced", "time servers are set, but synchronization could not be confirmed")
	errClockFromHost          = apierr.Define(409, "time.clock_from_host", "the system clock comes from the host and cannot be set in a container")
	errInvalidTimezone        = apierr.Define(400, "time.invalid_timezone", "invalid time zone")
	errInvalidNTPServer       = apierr.Define(400, "time.invalid_ntp_server", "invalid NTP server")
	errTooManyNTPServers      = apierr.Define(400, "time.too_many_ntp_servers", "too many NTP servers")
	errInvalidDNSServer       = apierr.Define(400, "dns.invalid_server", "invalid DNS server IP")
	errTooManyDNSServers      = apierr.Define(400, "dns.too_many_servers", "too many DNS servers")
	errNoAptCache             = apierr.Define(400, "apt_cache.not_selected", "no APT cache selected or configured")
	errInvalidFirewallRules   = apierr.Define(400, "firewall.invalid_rules", "invalid firewall rules")
	errUnknownSecurityTool    = apierr.Define(400, "security_tool.unknown", "unknown security tool")
	errBanListTimeout         = apierr.Define(504, "security_tool.bans_timeout", "server did not deliver the ban list in time")
	errInvalidAllowlistIP     = apierr.Define(400, "security_tool.invalid_allowlist_ip", "invalid allowlist IP")
	errCrowdSecLapiMissing    = apierr.Define(400, "security_tool.lapi_missing", "no CrowdSec LAPI selected or configured")
	errCrowdSecConsoleMissing = apierr.Define(400, "security_tool.console_key_missing", "no CrowdSec console key configured")
	errInvalidLapiMode        = apierr.Define(400, "security_tool.invalid_lapi_mode", "invalid LAPI mode")
	errSecurityToolAction     = apierr.Define(400, "security_tool.unknown_action", "unknown action for the security tool")
	errSecurityToolMissing    = apierr.Define(409, "security_tool.not_installed", "the security tool is not installed on this server")
	errUnbanIPInvalid         = apierr.Define(400, "security_tool.invalid_unban_ip", "a valid IP address is required to unban")
	errCrowdSecUnsupported    = apierr.Define(400, "security_tool.crowdsec_unsupported", "CrowdSec is not supported on this package manager")
)

// Pakete, Paketquellen und Snaps.
var (
	errNoPackages           = apierr.Define(422, "package.none_given", "no valid package names given")
	errInvalidPackage       = apierr.Define(422, "package.invalid_name", "invalid package name")
	errInvalidVersion       = apierr.Define(422, "package.invalid_version", "invalid package version")
	errVersionOnePackage    = apierr.Define(422, "package.version_needs_one_package", "installing an exact version works for exactly one package")
	errProtectedPackage     = apierr.Define(422, "package.protected", "this package is protected and cannot be removed through LCM")
	errPinnedPackage        = apierr.Define(422, "package.pinned", "this package is protected by a pin, remove the pin first")
	errInvalidPinName       = apierr.Define(422, "package.invalid_pin_name", "invalid package name for the pin")
	errPinNeedsEffect       = apierr.Define(422, "package.pin_needs_effect", "a pin needs at least one effect")
	errPinsUnavailable      = apierr.Define(409, "server.pins_unavailable", "package pins are not available on Proxmox systems")
	errPinsNotWired         = apierr.Define(409, "server.pins_not_available", "package pins are not available on this instance")
	errUnknownRepo          = apierr.Define(422, "repo.unknown", "unknown package source")
	errRepoManagerMismatch  = apierr.Define(422, "repo.package_manager_mismatch", "package source does not match the package manager of the server")
	errNoSnaps              = apierr.Define(422, "snap.none_given", "no valid snap names given")
	errInvalidSnap          = apierr.Define(422, "snap.invalid_name", "invalid snap name")
	errProtectedSnap        = apierr.Define(422, "snap.protected", "this snap is the basis of snap management and cannot be removed through LCM")
	errInvalidComposeName   = apierr.Define(422, "docker.invalid_compose_name", "invalid compose project or service name")
	errInvalidDockerRef     = apierr.Define(422, "docker.invalid_image_ref", "invalid image reference")
	errComposeUnavailable   = apierr.Define(422, "docker.compose_unavailable", "docker compose (v2) is not available on the server")
	errDockerUnavailable    = apierr.Define(422, "docker.unavailable", "docker is not available on the server")
	errComposeUnknown       = apierr.Define(422, "docker.compose_project_unknown", "compose project is not known on this server")
	errDockerImageUnknown   = apierr.Define(422, "docker.image_unknown", "image is not known on this server")
	errDockerImageInUse     = apierr.Define(422, "docker.image_in_use", "the image is used by a container and cannot be deleted")
	errComposePathMissing   = apierr.Define(422, "docker.compose_path_missing", "neither working directory nor config files of the compose project are known")
	errDockerContainerUnk   = apierr.Define(422, "docker.container_unknown", "container is not known on this server")
	errNoImagesToUpdate     = apierr.Define(422, "docker.no_images_to_update", "no used, tagged registry images on this server, nothing to update")
	errDockerUpdatesDisable = apierr.Define(422, "docker.updates_disabled", "docker updates are disabled for this server")
)

// serverErrors ordnet die Fehler des Server-Dienstes ihren API-Fehlern zu.
var serverErrors = []sentinelCode{
	{services.ErrServerNameTaken, errServerNameTaken},
	{services.ErrServerHostTaken, errServerHostTaken},
	{services.ErrFingerprintMismatch, errFingerprintMismatch},
	{sshx.ErrHostKeyMismatch, errHostKeyMismatch},
	{services.ErrLoopbackInContainer, errLoopbackInContainer},
	{services.ErrServerBusy, errServerBusy},
	{services.ErrMaintenance, apierr.Maintenance},
	{services.ErrProxmoxRestricted, errProxmoxRestricted},
	{services.ErrPackagePinsUnavailable, errPinsUnavailable},
	{services.ErrPackagePinsNotWired, errPinsNotWired},
	{services.ErrGlobalPinScope, apierr.Forbidden},
	{services.ErrRouterOSUnsupported, errRouterOSUnsupported},
	{services.ErrDSMUnsupported, errDSMUnsupported},
	{services.ErrNotDSM, errNotDSM},
	{services.ErrDSMNotSettable, errDSMNotSettable},
	{services.ErrDSMNoTargets, errMissingFields.With("fields", "keys")},
	{services.ErrFirewallToolMissing, errFirewallToolMissing},
	{services.ErrRestrictedSudo, errRestrictedSudo},
	{services.ErrAgentTransport, errAgentTransport},
	{services.ErrNotAgentServer, errNotAgentServer},
	{services.ErrAgentOffline, errAgentOffline},
	{services.ErrNoOldKernels, errNoOldKernels},
	{services.ErrNoSnapd, errNoSnapd},
	{services.ErrNoRevertCandidates, errNoRevertCandidates},
	{services.ErrKernelCleanupUnsupported, errKernelCleanupMissing},
	{sshx.ErrPasswordAuthUnavailable, errPasswordAuthOff},
	{sshx.ErrPasswordRejected, errPasswordRejected},
	{services.ErrNotRevertible, errNotRevertible},
	{services.ErrRepoNotSecurable, errRepoNotSecurable},
	{services.ErrHTTPSCheckTimeout, errHTTPSCheckTimeout},
	{services.ErrNoOnboardingKey, errNoOnboardingKey},
	{services.ErrNotLcmHost, errNotLcmHost},
	{services.ErrLcmHostNotApt, errLcmHostNotApt},
	{services.ErrLcmHostInContainer, errLcmHostInContainer},
	{services.ErrUnknownSecurityTool, errUnknownSecurityTool},
	{services.ErrBanListTimeout, errBanListTimeout},
	{services.ErrInvalidAllowlistIP, errInvalidAllowlistIP},
	{services.ErrCrowdSecLapiMissing, errCrowdSecLapiMissing},
	{services.ErrCrowdSecConsoleMissing, errCrowdSecConsoleMissing},
	{services.ErrInvalidLapiMode, errInvalidLapiMode},
	{services.ErrSecurityToolAction, errSecurityToolAction},
	{services.ErrSecurityToolNotInstalled, errSecurityToolMissing},
	{services.ErrUnbanIPMissing, errUnbanIPInvalid},
	{services.ErrCrowdSecUnsupported, errCrowdSecUnsupported},
	{services.ErrInvalidFirewallRules, errInvalidFirewallRules},
	{services.ErrNoAptCacheURL, errNoAptCache},
	{services.ErrTooManyDNSServers, errTooManyDNSServers.With("limit", domain.MaxDNSServers)},
	{services.ErrInvalidDNSServer, errInvalidDNSServer},
	{services.ErrInvalidTimezone, errInvalidTimezone},
	{services.ErrInvalidNTPServer, errInvalidNTPServer},
	{services.ErrTooManyNTPServers, errTooManyNTPServers.With("limit", domain.MaxNTPServers)},
	{services.ErrUnknownRepairAction, errUnknownRepairAction},
	{services.ErrRepairUnsupported, errRepairUnsupported},
	{services.ErrUnknownCapability, errUnknownCapability},
	{services.ErrUnknownPreviewAction, errPreviewUnknownAction},
	{services.ErrPreviewUnavailable, errPreviewUnavailable},
	{services.ErrCustomActionNotFound, errCustomNotFound},
	{services.ErrCapabilityNotFailed, errCapabilityNotFailed},
	{services.ErrNotRetryable, errNotRetryable},
	{services.ErrRetryNotWired, errRetryUnavailable},
	{services.ErrClockFromHost, errClockFromHost},
	{services.ErrNTPNotSynced, errNTPNotSynced},
	{services.ErrNoSnaps, errNoSnaps},
	{services.ErrInvalidSnap, errInvalidSnap},
	{services.ErrProtectedSnap, errProtectedSnap},
	{services.ErrNoPackages, errNoPackages},
	{services.ErrInvalidPackage, errInvalidPackage},
	{services.ErrInvalidVersion, errInvalidVersion},
	{services.ErrVersionOnePkg, errVersionOnePackage},
	{services.ErrProtectedPackage, errProtectedPackage},
	{services.ErrPinnedPackage, errPinnedPackage},
	{services.ErrPackagePinName, errInvalidPinName},
	{services.ErrPackagePinEffect, errPinNeedsEffect},
	{services.ErrUnknownRepo, errUnknownRepo},
	{services.ErrRepoPackageManagerMismatch, errRepoManagerMismatch},
	{services.ErrInvalidComposeName, errInvalidComposeName},
	{services.ErrInvalidDockerRef, errInvalidDockerRef},
	{services.ErrComposeUnavailable, errComposeUnavailable},
	{services.ErrComposeProjectUnknown, errComposeUnknown},
	{services.ErrDockerImageUnknown, errDockerImageUnknown},
	{services.ErrDockerImageInUse, errDockerImageInUse},
	{services.ErrDockerUnavailable, errDockerUnavailable},
	{services.ErrComposePathMissing, errComposePathMissing},
	{services.ErrDockerContainerUnknown, errDockerContainerUnk},
	{services.ErrNoImagesToUpdate, errNoImagesToUpdate},
	{services.ErrDockerUpdatesDisabled, errDockerUpdatesDisable},
}
