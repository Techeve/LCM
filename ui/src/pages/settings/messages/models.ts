// Meldungen: Formulare für Benachrichtigungskanal und Alarmregel, Prüfung, Körper für die API und die
// Texte je Alarmtyp. Die Typen und ihre Vorgaben folgen `domain/alert.go`.

import type { AlertRule, AlertRuleBody, Channel, ChannelBody } from '../../../api/settings-more';
import { t } from '../../../lib/i18n';
import { DEFAULT_MAIL_SECURITY, MAIL_SECURITY_PORT, type MailSecurity } from '../../../lib/mail-security';
import { LIMITS, charCount, hasControlChars, isEmail, isHost, isHttpUrl, isPort, isWholeNumber, senderProblem } from '../../../lib/validate';

// ---- Benachrichtigungskanal ----------------------------------------------------------------------

export const CHANNEL_TYPE_KEYS: Record<string, string> = {
  email: 'settings.notifications.type.email',
  webhook: 'settings.notifications.type.webhook',
  system_email: 'settings.notifications.type.systemEmail',
};

export const FORMAT_KEYS: Record<string, string> = {
  teams: 'settings.notifications.format.teams',
  generic: 'settings.notifications.format.generic',
};

export interface ChannelForm {
  name: string;
  type: string;
  enabled: boolean;
  /** Geheimnis: SMTP-Passwort bzw. Webhook-URL. Leer heißt beim Bearbeiten „unverändert“. */
  secret: string;
  host: string;
  port: string;
  username: string;
  from: string;
  recipients: string;
  security: MailSecurity;
  format: string;
}

export function emptyChannelForm(): ChannelForm {
  return { name: '', type: 'email', enabled: true, secret: '', host: '', port: String(MAIL_SECURITY_PORT[DEFAULT_MAIL_SECURITY]), username: '', from: '', recipients: '', security: DEFAULT_MAIL_SECURITY, format: 'teams' };
}

export function channelFormOf(channel: Channel): ChannelForm {
  return {
    name: channel.name,
    type: channel.type,
    enabled: channel.enabled,
    secret: '',
    host: channel.email.host,
    port: String(channel.email.port),
    username: channel.email.username,
    from: channel.email.from,
    recipients: channel.email.recipients.join('\n'),
    security: channel.email.security,
    format: channel.format,
  };
}

export function recipientList(text: string): string[] {
  return text
    .split(/[\n,;]+/)
    .map((r) => r.trim())
    .filter(Boolean);
}

/** Empfänger je Kanal, wie im Backend (`maxRecipients`). */
export const MAX_RECIPIENTS = 50;

export type ChannelErrors = Partial<Record<'name' | 'secret' | 'host' | 'port' | 'username' | 'from' | 'recipients', string>>;

/** Die Empfänger, die keine Adresse sind. */
export function badRecipients(text: string): string[] {
  return recipientList(text).filter((entry) => !isEmail(entry));
}

/** Die ersten ungültigen Empfänger für die Fehlermeldung. */
export function badRecipientsText(text: string): string {
  const bad = badRecipients(text);
  return bad.length > 3 ? `${bad.slice(0, 3).join(', ')} …` : bad.join(', ');
}

export function validateChannelForm(form: ChannelForm, isNew: boolean): ChannelErrors {
  const errors: ChannelErrors = {};
  const name = form.name.trim();
  if (!name) errors.name = 'settings.notifications.form.nameRequired';
  else if (charCount(name) > LIMITS.name) errors.name = 'check.tooLong';
  else if (hasControlChars(name)) errors.name = 'check.oneLine';
  if (form.type === 'webhook') {
    const url = form.secret.trim();
    if (isNew && !url) errors.secret = 'settings.notifications.form.urlRequired';
    else if (url && !url.startsWith('https://')) errors.secret = 'settings.notifications.form.urlHttps';
    else if (url && !isHttpUrl(url)) errors.secret = 'check.url';
    return errors;
  }
  const host = form.host.trim();
  if (!host) errors.host = 'settings.notifications.form.hostRequired';
  else if (!isHost(host)) errors.host = 'check.host';
  if (!isPort(form.port)) errors.port = 'settings.notifications.form.portRange';
  if (charCount(form.username.trim()) > LIMITS.loginUser) errors.username = 'check.tooLong';
  else if (hasControlChars(form.username)) errors.username = 'check.oneLine';
  if (charCount(form.secret) > LIMITS.password) errors.secret = 'check.tooLong';
  const fromProblem = senderProblem(form.from.trim());
  if (fromProblem === 'name') errors.from = 'check.senderName';
  else if (fromProblem) errors.from = 'settings.notifications.form.fromInvalid';
  const recipients = recipientList(form.recipients);
  if (recipients.length === 0) errors.recipients = 'settings.notifications.form.recipientsInvalid';
  else if (recipients.length > MAX_RECIPIENTS) errors.recipients = 'check.tooMany';
  else if (badRecipients(form.recipients).length > 0) errors.recipients = 'check.emails';
  return errors;
}

export function channelBody(form: ChannelForm): ChannelBody {
  const body: ChannelBody = {
    name: form.name.trim(),
    type: form.type,
    enabled: form.enabled,
    config:
      form.type === 'webhook'
        ? { format: form.format }
        : {
            host: form.host.trim(),
            port: Number(form.port) || 0,
            username: form.username.trim(),
            from: form.from.trim(),
            recipients: recipientList(form.recipients),
            security: form.security,
          },
  };
  if (form.secret.trim()) body.secret = form.secret.trim();
  return body;
}

/** Kurzfassung der Konfiguration für die Tabelle. */
export function channelSummary(channel: Channel): string {
  if (channel.type === 'webhook') return t(FORMAT_KEYS[channel.format] ?? 'settings.notifications.format.generic');
  if (channel.type === 'system_email') return t('settings.notifications.systemSummary');
  return t('settings.notifications.emailSummary', { host: channel.email.host || '–', port: channel.email.port, count: channel.email.recipients.length });
}

// ---- Alarmregel ----------------------------------------------------------------------------------

export type Threshold = 'percent' | 'days' | 'severity' | 'outdated' | 'hours' | 'none';

interface AlertType {
  label: string;
  hint: string;
  threshold: Threshold;
  /** Bewertet LCM selbst statt der Server; Gruppen sind dann gegenstandslos. */
  self?: boolean;
}

export const ALERT_TYPES: Record<string, AlertType> = {
  disk_capacity: { label: 'settings.alerts.types.diskCapacity', hint: 'settings.alerts.hints.diskCapacity', threshold: 'percent' },
  storage_forecast: { label: 'settings.alerts.types.storageForecast', hint: 'settings.alerts.hints.storageForecast', threshold: 'days' },
  security_cve: { label: 'settings.alerts.types.securityCve', hint: 'settings.alerts.hints.securityCve', threshold: 'severity' },
  failed_updates: { label: 'settings.alerts.types.failedUpdates', hint: 'settings.alerts.hints.failedUpdates', threshold: 'outdated' },
  action_failed: { label: 'settings.alerts.types.actionFailed', hint: 'settings.alerts.hints.actionFailed', threshold: 'none' },
  heartbeat: { label: 'settings.alerts.types.heartbeat', hint: 'settings.alerts.hints.heartbeat', threshold: 'hours' },
  reboot_required: { label: 'settings.alerts.types.rebootRequired', hint: 'settings.alerts.hints.rebootRequired', threshold: 'none' },
  apt_cacher_down: { label: 'settings.alerts.types.aptCacherDown', hint: 'settings.alerts.hints.aptCacherDown', threshold: 'none', self: true },
  crowdsec_lapi_down: { label: 'settings.alerts.types.crowdsecLapiDown', hint: 'settings.alerts.hints.crowdsecLapiDown', threshold: 'none', self: true },
  deep_scan: { label: 'settings.alerts.types.deepScan', hint: 'settings.alerts.hints.deepScan', threshold: 'none' },
  cve_db_stale: { label: 'settings.alerts.types.cveDbStale', hint: 'settings.alerts.hints.cveDbStale', threshold: 'none', self: true },
  backup_stale: { label: 'settings.alerts.types.backupStale', hint: 'settings.alerts.hints.backupStale', threshold: 'none', self: true },
  tls_cert_expiring: { label: 'settings.alerts.types.tlsCertExpiring', hint: 'settings.alerts.hints.tlsCertExpiring', threshold: 'none', self: true },
  advisory: { label: 'settings.alerts.types.advisory', hint: 'settings.alerts.hints.advisory', threshold: 'severity' },
};

export const SEVERITY_KEYS: Record<string, string> = {
  info: 'settings.alerts.severity.info',
  warning: 'settings.alerts.severity.warning',
  critical: 'settings.alerts.severity.critical',
};

export const SEVERITY_TONES: Record<string, 'info' | 'warn' | 'crit'> = { info: 'info', warning: 'warn', critical: 'crit' };

export const CVE_SEVERITY_KEYS: Record<string, string> = {
  low: 'settings.alerts.cveSeverity.low',
  medium: 'settings.alerts.cveSeverity.medium',
  high: 'settings.alerts.cveSeverity.high',
  critical: 'settings.alerts.cveSeverity.critical',
};

/** Vorgaben des Backends, falls die Regel 0 trägt. */
export const DEFAULTS = { percent: 90, days: 10, hours: 24, cooldown: 360, minSeverity: 'high' };

export function alertTypeLabel(type: string): string {
  const info = ALERT_TYPES[type];
  return info ? t(info.label) : type;
}

export function isSelfType(type: string): boolean {
  return ALERT_TYPES[type]?.self === true;
}

export interface RuleForm {
  name: string;
  type: string;
  enabled: boolean;
  group_ids: number[];
  channel_id: string;
  severity: string;
  threshold_percent: string;
  forecast_days: string;
  max_outdated: string;
  min_severity: string;
  heartbeat_hours: string;
  cooldown_minutes: string;
}

export function emptyRuleForm(): RuleForm {
  return {
    name: '',
    type: 'disk_capacity',
    enabled: true,
    group_ids: [],
    channel_id: '',
    severity: 'warning',
    threshold_percent: String(DEFAULTS.percent),
    forecast_days: String(DEFAULTS.days),
    max_outdated: '0',
    min_severity: DEFAULTS.minSeverity,
    heartbeat_hours: String(DEFAULTS.hours),
    // Beim Anlegen sichtbar vorbelegt; 0 heißt im Backend „keine Sperre“.
    cooldown_minutes: String(DEFAULTS.cooldown),
  };
}

export function ruleFormOf(rule: AlertRule): RuleForm {
  return {
    name: rule.name,
    type: rule.type,
    enabled: rule.enabled,
    group_ids: [...rule.group_ids],
    channel_id: rule.channel_id ? String(rule.channel_id) : '',
    severity: rule.severity,
    threshold_percent: String(rule.threshold_percent || DEFAULTS.percent),
    forecast_days: String(rule.forecast_days || DEFAULTS.days),
    max_outdated: String(rule.max_outdated),
    min_severity: rule.min_severity || DEFAULTS.minSeverity,
    heartbeat_hours: String(rule.heartbeat_hours || DEFAULTS.hours),
    cooldown_minutes: String(rule.cooldown_minutes),
  };
}

/** Erlaubte Bereiche der Zahlenfelder, wie im Backend (`checkAlertRequest`). */
export const ALERT_RANGES = {
  threshold_percent: { min: 1, max: 100 },
  forecast_days: { min: 1, max: 3650 },
  max_outdated: { min: 0, max: 100000 },
  heartbeat_hours: { min: 1, max: 8760 },
  cooldown_minutes: { min: 0, max: 525600 },
} as const;

type RangeField = keyof typeof ALERT_RANGES;

function inRange(form: RuleForm, field: RangeField): boolean {
  return isWholeNumber(form[field], ALERT_RANGES[field].min, ALERT_RANGES[field].max);
}

export type RuleErrors = Partial<Record<'name' | RangeField, string>>;

export function validateRuleForm(form: RuleForm): RuleErrors {
  const errors: RuleErrors = {};
  const name = form.name.trim();
  if (!name) errors.name = 'settings.alerts.form.nameRequired';
  else if (charCount(name) > LIMITS.name) errors.name = 'check.tooLong';
  else if (hasControlChars(name)) errors.name = 'check.oneLine';
  const threshold = ALERT_TYPES[form.type]?.threshold;
  if (threshold === 'percent' && !inRange(form, 'threshold_percent')) errors.threshold_percent = 'settings.alerts.form.percentRange';
  if (threshold === 'days' && !inRange(form, 'forecast_days')) errors.forecast_days = 'settings.alerts.form.positive';
  if (threshold === 'outdated' && !inRange(form, 'max_outdated')) errors.max_outdated = 'settings.alerts.form.wholeNumber';
  if (threshold === 'hours' && !inRange(form, 'heartbeat_hours')) errors.heartbeat_hours = 'settings.alerts.form.positive';
  if (!inRange(form, 'cooldown_minutes')) errors.cooldown_minutes = 'settings.alerts.form.wholeNumber';
  return errors;
}

export function ruleBody(form: RuleForm): AlertRuleBody {
  return {
    name: form.name.trim(),
    type: form.type,
    enabled: form.enabled,
    group_ids: isSelfType(form.type) ? [] : form.group_ids,
    channel_id: form.channel_id ? Number(form.channel_id) : null,
    severity: form.severity,
    threshold_percent: Number(form.threshold_percent) || 0,
    forecast_days: Number(form.forecast_days) || 0,
    max_outdated: Number(form.max_outdated) || 0,
    min_severity: form.min_severity,
    heartbeat_hours: Number(form.heartbeat_hours) || 0,
    cooldown_minutes: Number(form.cooldown_minutes) || 0,
  };
}

/** Kurzfassung der Schwelle je Typ für die Tabelle. */
export function thresholdText(rule: AlertRule): string {
  switch (ALERT_TYPES[rule.type]?.threshold) {
    case 'percent':
      return t('settings.alerts.threshold.percent', { value: rule.threshold_percent || DEFAULTS.percent });
    case 'days':
      return t('settings.alerts.threshold.days', { value: rule.forecast_days || DEFAULTS.days });
    case 'severity':
      return t('settings.alerts.threshold.severity', { value: t(CVE_SEVERITY_KEYS[rule.min_severity || DEFAULTS.minSeverity] ?? 'settings.alerts.cveSeverity.high') });
    case 'outdated':
      return t('settings.alerts.threshold.outdated', { value: rule.max_outdated });
    case 'hours':
      return t('settings.alerts.threshold.hours', { value: rule.heartbeat_hours || DEFAULTS.hours });
    default:
      return t('settings.alerts.threshold.none');
  }
}

/** Für wen die Regel gilt: LCM selbst, alle Server oder die genannten Gruppen. */
export function scopeText(rule: AlertRule): string {
  if (isSelfType(rule.type)) return t('settings.alerts.scope.self');
  return rule.group_names.length ? rule.group_names.join(', ') : t('settings.alerts.scope.all');
}

/** Aktive Regeln ohne Kanal: Sie protokollieren nur, niemand erfährt davon. */
export function silentRules(rules: AlertRule[]): number {
  return rules.filter((rule) => rule.enabled && !rule.channel_id).length;
}
