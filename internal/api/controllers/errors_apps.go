package controllers

import (
	"LCM/internal/api/apierr"
	"LCM/internal/core/domain"
	"LCM/internal/core/services"
)

// Anwendungskatalog und Anwendungsaktionen.
var (
	errAppNotFound       = apierr.Define(404, "app.not_found", "application not found")
	errAppNotDetected    = apierr.Define(409, "app.not_detected", "this application was not detected on the server")
	errAppNoAction       = apierr.Define(409, "app.no_action", "no matching action is defined for this application")
	errAppBuiltinDelete  = apierr.Define(403, "app.builtin_delete", "built-in applications can be disabled but not deleted")
	errAppSlugTaken      = apierr.Define(409, "app.slug_taken", "an application with this technical name already exists")
	errAppSlug           = apierr.Define(422, "app.invalid_slug", "invalid technical name (lowercase, digits, hyphen)")
	errAppName           = apierr.Define(422, "app.name_required", "the name is required")
	errAppMarker         = apierr.Define(422, "app.invalid_marker", "invalid marker")
	errAppNoMarker       = apierr.Define(422, "app.no_marker", "at least one marker is required")
	errAppCompare        = apierr.Define(422, "app.invalid_compare", "unknown comparison type")
	errAppPattern        = apierr.Define(422, "app.invalid_pattern", "invalid version pattern")
	errAppSource         = apierr.Define(422, "app.invalid_source", "invalid source for the latest version")
	errAppActionMissing  = apierr.Define(422, "app.action_missing", "the selected action does not exist")
	errAppActionsOff     = apierr.Define(503, "app.actions_unavailable", "actions are not available")
	errAppInvalidAction  = apierr.Define(400, "app.invalid_action", "action must be backup or update")
	errCustomNotFound    = apierr.Define(404, "custom_action.not_found", "custom action not found")
	errCustomNameMissing = apierr.Define(422, "custom_action.name_required", "the custom action needs a name")
	errCustomEmpty       = apierr.Define(422, "custom_action.empty", "the custom action contains no command")
	errCustomNameTaken   = apierr.Define(409, "custom_action.name_taken", "a custom action with this name already exists")
	errCustomInUse       = apierr.Define(409, "custom_action.in_use", "the custom action is used by at least one rule and cannot be deleted")
)

// appErrors ordnet die Fehler des Anwendungskatalogs zu.
var appErrors = []sentinelCode{
	{services.ErrAppNotDetected, errAppNotDetected},
	{services.ErrAppNoAction, errAppNoAction},
	{services.ErrAppBuiltinDelete, errAppBuiltinDelete},
	{services.ErrAppSlugTaken, errAppSlugTaken},
	{domain.ErrAppSlug, errAppSlug},
	{domain.ErrAppName, errAppName},
	{domain.ErrAppMarker, errAppMarker},
	{domain.ErrAppNoMarker, errAppNoMarker},
	{domain.ErrAppCompare, errAppCompare},
	{domain.ErrAppPattern, errAppPattern},
	{domain.ErrAppSource, errAppSource},
	{services.ErrAppActionMissing, errAppActionMissing},
}

// customActionErrors ordnet die Fehler der Custom-Aktionen zu.
var customActionErrors = []sentinelCode{
	{services.ErrCustomActionNameRequired, errCustomNameMissing},
	{services.ErrCustomActionEmpty, errCustomEmpty},
	{services.ErrCustomActionNameTaken, errCustomNameTaken},
	{services.ErrCustomActionInUse, errCustomInUse},
}
