package controllers

import (
	"errors"

	"LCM/internal/api/apierr"
	"LCM/internal/core/domain"
	"LCM/internal/core/services"
)

// Pfad- und Kommando-Prüfungen, die Berechtigungsprofile, Regelbausteine und
// die Verzeichnis-Härtung gemeinsam nutzen.
var (
	errPathRelative       = apierr.Define(422, "path.relative", "the path must be absolute")
	errPathMeta           = apierr.Define(422, "path.meta_characters", "shell metacharacters and wildcards are not allowed in the path")
	errPathProtected      = apierr.Define(422, "path.protected", "this path is protected, the system or LCM access depends on it")
	errPathModeInvalid    = apierr.Define(422, "path.invalid_mode", "invalid mode, allowed: read, readwrite, deny")
	errCommandEmpty       = apierr.Define(422, "command.empty", "the rule needs a command")
	errCommandRelative    = apierr.Define(422, "command.relative", "the command must start with an absolute path")
	errCommandWildcard    = apierr.Define(422, "command.wildcard", "wildcards are not allowed in the command")
	errCommandMeta        = apierr.Define(422, "command.meta_characters", "shell metacharacters are not allowed in the command")
	errCommandNoArguments = apierr.Define(422, "command.needs_arguments", "this command needs fixed arguments")
)

// Berechtigungsprofile.
var (
	errProfileNotFound    = apierr.Define(404, "profile.not_found", "privilege profile not found")
	errProfileBuiltin     = apierr.Define(403, "profile.builtin", "built-in profiles cannot be changed or deleted")
	errProfileSlugTaken   = apierr.Define(409, "profile.slug_taken", "this slug is already taken")
	errProfileNameTaken   = apierr.Define(409, "profile.name_taken", "this name is already taken")
	errProfileSlugInvalid = apierr.Define(422, "profile.invalid_slug", "invalid slug")
	errProfileNameEmpty   = apierr.Define(422, "profile.name_required", "the profile needs a name")
	errRootEquivalent     = apierr.Define(422, "profile.root_equivalent", "this command effectively grants full root rights and needs explicit confirmation")
)

// Regelbausteine.
var (
	errBlockNotFound     = apierr.Define(404, "block.not_found", "rule block not found")
	errBlockBuiltin      = apierr.Define(403, "block.builtin", "built-in blocks cannot be changed or deleted, clone them to adapt")
	errBlockInUse        = apierr.Define(409, "block.in_use", "the block is used by at least one profile and cannot be deleted")
	errBlockSlugTaken    = apierr.Define(409, "block.slug_taken", "this slug is already taken")
	errBlockNameTaken    = apierr.Define(409, "block.name_taken", "this name is already taken")
	errBlockSlugInvalid  = apierr.Define(422, "block.invalid_slug", "invalid slug")
	errBlockNameEmpty    = apierr.Define(422, "block.name_required", "the block needs a name")
	errBlockNoVariants   = apierr.Define(422, "block.no_variants", "the block needs at least one variant")
	errBlockNoRules      = apierr.Define(422, "block.no_rules", "every variant needs at least one command, file or directory right")
	errBlockFamily       = apierr.Define(422, "block.invalid_family", "invalid distribution family")
	errBlockParamName    = apierr.Define(422, "block.invalid_param_name", "invalid parameter name")
	errBlockParamValue   = apierr.Define(422, "block.invalid_param_value", "invalid parameter value")
	errBlockRenderFailed = apierr.Define(422, "block.preview_failed", "the block could not be rendered")
	errBlockPathLine     = apierr.Define(422, "block.invalid_path_line", `invalid directory right line, expected "mode path"`)
	errBlockRunAs        = apierr.Define(422, "block.invalid_run_as", "invalid target user, expected a Linux user name (empty means root)")
)

// pathErrors sind die Pfad-Prüfungen der Domäne.
var pathErrors = []sentinelCode{
	{domain.ErrPathRelative, errPathRelative},
	{domain.ErrPathMeta, errPathMeta},
	{domain.ErrPathProtected, errPathProtected},
}

// commandErrors sind die Kommando-Prüfungen der Domäne.
var commandErrors = []sentinelCode{
	{domain.ErrSudoCommandEmpty, errCommandEmpty},
	{domain.ErrSudoCommandRelative, errCommandRelative},
	{domain.ErrSudoCommandWildcard, errCommandWildcard},
	{domain.ErrSudoCommandMeta, errCommandMeta},
	{domain.ErrSudoCommandNoArgs, errCommandNoArguments},
}

// profileErrors ordnet die Fehler des Profil-Dienstes ihren API-Fehlern zu.
var profileErrors = concat(
	[]sentinelCode{
		{services.ErrProfileBuiltin, errProfileBuiltin},
		{services.ErrProfileSlugTaken, errProfileSlugTaken},
		{services.ErrProfileNameTaken, errProfileNameTaken},
		{domain.ErrProfileSlugInvalid, errProfileSlugInvalid.With("limit", domain.MaxProfileSlugLen)},
		{domain.ErrProfileNameEmpty, errProfileNameEmpty},
		{domain.ErrPathModeInvalid, errPathModeInvalid},
	},
	commandErrors, pathErrors,
)

// blockErrors ordnet die Fehler des Baustein-Dienstes ihren API-Fehlern zu.
var blockErrors = concat(
	[]sentinelCode{
		{services.ErrBlockBuiltin, errBlockBuiltin},
		{services.ErrBlockInUse, errBlockInUse},
		{services.ErrBlockSlugTake, errBlockSlugTaken},
		{services.ErrBlockNameTake, errBlockNameTaken},
		{domain.ErrBlockSlugInvalid, errBlockSlugInvalid.With("limit", domain.MaxBlockSlugLen)},
		{domain.ErrBlockNameEmpty, errBlockNameEmpty},
		{domain.ErrBlockNoVariants, errBlockNoVariants},
		{domain.ErrBlockNoRules, errBlockNoRules},
		{domain.ErrBlockFamily, errBlockFamily},
		{domain.ErrBlockParamName, errBlockParamName},
		{domain.ErrBlockParamValue, errBlockParamValue},
		{domain.ErrBlockRunAs, errBlockRunAs},
	},
)

// variantErrors sind die Prüf-Fehler einer Variante. Sie kommen mit einer
// Fundstelle umschlossen an ("variante apt, …"); die Fundstelle geht als
// detail mit.
var variantErrors = concat(
	[]sentinelCode{{domain.ErrBlockPathLine, errBlockPathLine}},
	commandErrors, pathErrors,
)

// rootEquivalentError übersetzt die Bestätigungspflicht für Root-Äquivalente;
// ok ist false, wenn err keine ist.
func rootEquivalentError(err error) (*apierr.Error, bool) {
	var rootEquivalent *domain.ErrRootEquivalent
	if !errors.As(err, &rootEquivalent) {
		return nil, false
	}
	return errRootEquivalent.With("binary", rootEquivalent.Binary), true
}
