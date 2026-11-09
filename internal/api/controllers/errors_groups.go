package controllers

import (
	"LCM/internal/api/apierr"
	"LCM/internal/core/domain"
	"LCM/internal/core/services"
)

// Servergruppen, Zeitpläne und Regeln.
var (
	errGroupNotFound         = apierr.Define(404, "group.not_found", "group not found")
	errGroupNameTaken        = apierr.Define(409, "group.name_taken", "a group with this name already exists")
	errGroupProtected        = apierr.Define(403, "group.protected", "the system group cannot be changed or dissolved")
	errRuleProtected         = apierr.Define(403, "group.rule_protected", "system rules cannot be deleted")
	errScheduleProtected     = apierr.Define(403, "group.schedule_protected", "system schedules cannot be deleted")
	errInvalidCron           = apierr.Define(422, "group.invalid_cron", "invalid cron expression")
	errInvalidSpread         = apierr.Define(422, "group.invalid_spread", "the time window must be between 0 minutes and the interval of two schedule runs")
	errInvalidPriority       = apierr.Define(422, "group.invalid_priority", "invalid priority")
	errInvalidRuleType       = apierr.Define(422, "group.invalid_rule_type", "invalid rule type for a group rule")
	errEnforceRuleType       = apierr.Define(422, "group.enforce_rule_type", "baseline rules are only possible for the types firewall, apt-proxy, acl-setup and perm-sync")
	errEnforceFirewallSpec   = apierr.Define(422, "group.enforce_firewall_spec", "a firewall baseline rule needs an explicit target state")
	errRuleCommandUnused     = apierr.Define(422, "group.rule_command_unused", "this rule type uses no command, omit the field")
	errCustomActionMissing   = apierr.Define(422, "group.custom_action_not_selected", "a custom rule needs a valid custom action")
	errRuleNeedsTarget       = apierr.Define(422, "group.rule_needs_target", "a rule needs either a schedule or the baseline flag, exactly one of both")
	errInvalidDNSTestDomain  = apierr.Define(422, "group.invalid_dns_test_domain", "invalid test domain")
	errInvalidAptCacheChoice = apierr.Define(422, "group.invalid_apt_cache", "invalid APT cache selection")
	errInvalidDockerScope    = apierr.Define(422, "group.invalid_docker_scope", "invalid selection, expected a compose project or container name or empty")
	errRuleOrderInvalid      = apierr.Define(422, "group.invalid_rule_order", "invalid order, all rules of exactly one trigger of this group are expected, each once")
)

// groupErrors ordnet die Fehler des Gruppen-Dienstes ihren API-Fehlern zu.
var groupErrors = []sentinelCode{
	{services.ErrGroupNameTaken, errGroupNameTaken},
	{services.ErrProtectedGroup, errGroupProtected},
	{services.ErrProtectedRule, errRuleProtected},
	{services.ErrProtectedSchedule, errScheduleProtected},
	{services.ErrInvalidCron, errInvalidCron},
	{services.ErrInvalidSpread, errInvalidSpread},
	{services.ErrInvalidGroupPriority, errInvalidPriority.With("limit", domain.MaxGroupPriority)},
	{services.ErrInvalidRuleType, errInvalidRuleType},
	{services.ErrEnforceRuleType, errEnforceRuleType},
	{services.ErrEnforceFirewallSpec, errEnforceFirewallSpec},
	{services.ErrRuleCommandUnused, errRuleCommandUnused},
	{services.ErrCustomActionNotSelected, errCustomActionMissing},
	{services.ErrRuleNeedsTarget, errRuleNeedsTarget},
	{services.ErrInvalidDNSTestDomain, errInvalidDNSTestDomain},
	{services.ErrInvalidAptCacheChoice, errInvalidAptCacheChoice},
	{services.ErrInvalidDockerScope, errInvalidDockerScope},
	{services.ErrRuleOrderInvalid, errRuleOrderInvalid},
	{services.ErrNoPackages, errNoPackages},
	{services.ErrInvalidPackage, errInvalidPackage},
}
