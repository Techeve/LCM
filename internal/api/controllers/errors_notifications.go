package controllers

import (
	"LCM/internal/api/apierr"
	"LCM/internal/core/services"
	"LCM/internal/infrastructure/notify"
)

// Benachrichtigungskanäle.
var (
	errChannelNotFound  = apierr.Define(404, "notification.not_found", "notification channel not found")
	errChannelName      = apierr.Define(422, "notification.name_required", "the channel needs a name")
	errChannelType      = apierr.Define(422, "notification.invalid_type", "unknown notification channel type")
	errChannelConfig    = apierr.Define(422, "notification.invalid_config", "invalid channel configuration")
	errChannelNameTaken = apierr.Define(409, "notification.name_taken", "a notification channel with this name already exists")
	errChannelInUse     = apierr.Define(409, "notification.in_use", "the channel is used by at least one alert rule and cannot be deleted")
	errChannelManaged   = apierr.Define(409, "notification.managed", "the system email channel is managed in the general settings")
	errChannelTest      = apierr.Define(422, "notification.test_failed", "the test message could not be sent")
)

// Alarmregeln.
var (
	errAlertNotFound = apierr.Define(404, "alert.not_found", "alert rule not found")
	errAlertName     = apierr.Define(422, "alert.name_required", "the alert rule needs a name")
	errAlertType     = apierr.Define(422, "alert.invalid_type", "unknown alert type")
	errAlertSeverity = apierr.Define(422, "alert.invalid_severity", "invalid severity level")
	errAlertGroup    = apierr.Define(422, "alert.unknown_group", "unknown server group")
)

// notificationErrors ordnet die Fehler der Kanal-Verwaltung zu.
var notificationErrors = concat(
	[]sentinelCode{
		{services.ErrChannelNameRequired, errChannelName},
		{services.ErrChannelTypeInvalid, errChannelType},
		{notify.ErrUnknownChannelType, errChannelType},
		{services.ErrChannelConfigInvalid, errChannelConfig},
		{services.ErrChannelNameTaken, errChannelNameTaken},
		{services.ErrChannelInUse, errChannelInUse},
		{services.ErrChannelManaged, errChannelManaged},
	},
	mailerErrors,
)

// alertErrors ordnet die Fehler der Alarmregel-Verwaltung zu.
var alertErrors = []sentinelCode{
	{services.ErrAlertRuleNameRequired, errAlertName},
	{services.ErrAlertRuleTypeInvalid, errAlertType},
	{services.ErrAlertSeverityInvalid, errAlertSeverity},
	{services.ErrAlertGroupUnknown, errAlertGroup},
}
