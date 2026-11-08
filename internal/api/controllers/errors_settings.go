package controllers

import (
	"LCM/internal/api/apierr"
	"LCM/internal/core/services"
)

// Einstellungen und ihre Kataloge. Die *.invalid_input-Fehler tragen den
// Prüftext des Dienstes als detail - er ist noch nicht in Codes zerlegt.
var (
	errSettingInvalid            = apierr.Define(400, "settings.invalid_input", "invalid setting value")
	errBrowserSessionModeInvalid = apierr.Define(422, "settings.browser_session_mode_invalid", "browser_session_mode must be memory or cookie")
	errSessionCookieNeedsHTTPS   = apierr.Define(422, "settings.session_cookie_needs_https", "the session cookie option is only available over HTTPS")
	errMailerIncomplete          = apierr.Define(400, "settings.mailer_incomplete", "SMTP configuration is incomplete, host and sender are required")
	errMailerDisabled            = apierr.Define(409, "settings.mailer_disabled", "the standard email sending is not enabled")
	errMailerNoRecipients        = apierr.Define(409, "settings.mailer_no_recipients", "no admin recipients are set for the standard email sending")
	errMailTestFailed            = apierr.Define(422, "settings.mail_test_failed", "the test email could not be sent")
	errBackupNeedsSecret         = apierr.Define(400, "settings.backup_needs_passphrase", "automatic backups need recipient keys or a passphrase, set them first")
	errKnownRepoNotFound         = apierr.Define(404, "known_repo.not_found", "catalog entry not found")
	errKnownRepoInvalid          = apierr.Define(400, "known_repo.invalid_input", "invalid catalog entry")
	errAllowlistNotFound         = apierr.Define(404, "allowlist.not_found", "allowlist not found")
	errAllowlistInvalid          = apierr.Define(400, "allowlist.invalid_input", "invalid allowlist")
	errAllowlistInUse            = apierr.Define(409, "allowlist.in_use", "the IP allowlist is still in use")
	errLapiNotFound              = apierr.Define(404, "crowdsec_lapi.not_found", "CrowdSec LAPI not found")
	errLapiInvalid               = apierr.Define(400, "crowdsec_lapi.invalid_input", "invalid CrowdSec LAPI")
	errAptCacheNotFound          = apierr.Define(404, "apt_cache.not_found", "APT cache not found")
	errAptCacheInvalid           = apierr.Define(400, "apt_cache.invalid_input", "invalid APT cache")
	errScheduleTriggerFails      = apierr.Define(400, "schedule.trigger_failed", "the system schedule could not be triggered")
	errScannerUnavailable        = apierr.Define(409, "cve.scanner_unavailable", "the CVE scanner (Trivy) is not available on the LCM host")
	errSelfUpdateOff             = apierr.Define(503, "selfupdate.not_configured", "self-update is not set up on this installation")
	errSelfUpdateRejected        = apierr.Define(409, "selfupdate.rejected", "the self-update cannot be started")
)

// mailerErrors ordnet die Konfigurationsfehler des Standard-E-Mail-Versands zu.
var mailerErrors = []sentinelCode{
	{services.ErrMailerDisabled, errMailerDisabled},
	{services.ErrMailerNoRecipients, errMailerNoRecipients},
	{services.ErrMailerIncomplete, errMailerIncomplete},
}

// Sicherungen und Wiederherstellung.
var (
	errBackupNotFound      = apierr.Define(404, "backup.not_found", "backup not found")
	errBackupNoPassphrase  = apierr.Define(400, "backup.passphrase_required", "a passphrase or recipient keys are required")
	errBackupWeakSecret    = apierr.Define(422, "backup.weak_passphrase", "the backup passphrase does not meet the security requirements")
	errBackupRecipient     = apierr.Define(422, "backup.invalid_recipient", "invalid backup recipient key")
	errBackupFailed        = apierr.Define(500, "backup.failed", "backup failed")
	errBackupNoIdentity    = apierr.Define(400, "backup.recipient_key_required", "the archive is encrypted to recipient keys, a recipient's private key is required")
	errBackupWrongSecret   = apierr.Define(400, "backup.wrong_passphrase", "wrong passphrase or key, or the file is damaged")
	errBackupFormat        = apierr.Define(400, "backup.invalid_archive", "not a valid LCM backup archive")
	errBackupIncomplete    = apierr.Define(400, "backup.incomplete", "the archive contains no database")
	errRestoreFailed       = apierr.Define(500, "backup.restore_failed", "restore failed")
	errBackupUploadMissing = apierr.Define(400, "backup.upload_missing", "no backup file in the field archive")
	errBackupUploadSize    = apierr.Define(413, "backup.upload_too_large", "the backup file is too large")
	errBackupUnreadable    = apierr.Define(400, "backup.upload_unreadable", "the backup file cannot be read")
)

// Abo und Frühwarnung.
var (
	errNoSubscription     = apierr.Define(400, "subscription.none", "no subscription stored")
	errSubscriptionURL    = apierr.Define(400, "subscription.invalid_service_url", "invalid service address")
	errActivationFailed   = apierr.Define(400, "subscription.activation_failed", "the subscription could not be activated")
	errUnknownAptChannel  = apierr.Define(400, "subscription.unknown_channel", "unknown package channel, allowed: community, beta, enterprise")
	errAptSwitchOff       = apierr.Define(400, "subscription.apt_switch_unavailable", "the apt switch is not available, the LCM host is not registered as an apt server")
	errChannelSwitchFails = apierr.Define(400, "subscription.channel_switch_failed", "the package channel could not be switched")
	errChannelBehind      = apierr.Define(409, "subscription.channel_behind", "the package channel is behind the installed version")
	errChannelUnknown     = apierr.Define(409, "subscription.channel_unknown", "the state of the package channel cannot be retrieved")
	errAdvisoryOff        = apierr.Define(409, "advisory.disabled", "the early warning is not enabled")
	errAdvisoryNoMirror   = apierr.Define(409, "advisory.no_local_copy", "no local copy is configured")
	errAdvisoryNotFound   = apierr.Define(404, "advisory.not_found", "finding not found")
)

// restoreErrors ordnet die Fehler der Wiederherstellung zu.
var restoreErrors = []sentinelCode{
	{services.ErrBackupNotFound, errBackupNotFound},
	{services.ErrBackupNoPassphrase, errBackupNoPassphrase},
	{services.ErrBackupNoIdentity, errBackupNoIdentity},
	{services.ErrBackupPassphrase, errBackupWrongSecret},
	{services.ErrBackupFormat, errBackupFormat},
	{services.ErrBackupIncomplete, errBackupIncomplete},
}

// subscriptionChannelErrors ordnet die Fehler des Kanalwechsels zu.
var subscriptionChannelErrors = []sentinelCode{
	{services.ErrNoSubscription, errNoSubscription},
	{services.ErrUnknownAptChannel, errUnknownAptChannel},
	{services.ErrAptSwitchUnavailable, errAptSwitchOff},
	{services.ErrChannelBehind, errChannelBehind},
	{services.ErrChannelUnknown, errChannelUnknown},
	{services.ErrServerBusy, errServerBusy},
}
