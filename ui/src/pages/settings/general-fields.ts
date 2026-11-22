// Felder von Einstellungen → Allgemein, die auch der Einrichtungsassistent zeigt: E-Mail-Versand und Aufbewahrung.
// Gleiche Felder, Texte und Grenzen an beiden Stellen; wer sie zeichnet, reicht nur seine Eingabezeile herein.

import { html, type TemplateResult } from 'lit';
import { readMailSecurity, type MailSecurity } from '../../lib/mail-security';
import { t } from '../../lib/i18n';
import { LIMITS } from '../../lib/validate';
import { mailSecurityField } from './mail-security-field';
import { choice, flag, int, secret, text, type FieldSpec } from './system/form-state';
import type { InputOptions } from './system/settings-form';

/** Zeichnet ein Eingabefeld zu einem Schlüssel der Felderliste (Entwurf, Fehler und Änderung gehören dem Aufrufer). */
export type InputRenderer = (key: string, label: string, options?: InputOptions) => TemplateResult;

export const MAIL_FIELDS: FieldSpec[] = [
  flag('mail_enabled'),
  text('mail_host', LIMITS.host, 'host'),
  int('mail_port', 1, 65535),
  choice('mail_security', (settings) => readMailSecurity(undefined, settings.mail_use_tls)),
  text('mail_username', LIMITS.loginUser),
  secret('mail_password', LIMITS.password),
  text('mail_from', LIMITS.sender, 'sender'),
  text('mail_admin_recipients', LIMITS.list, 'emails'),
];

export const RETENTION_FIELDS: FieldSpec[] = [int('log_retention_days', 0, 3650), int('routine_log_retention_days', 1, 3650), int('storage_history_retention_days', 90, 365)];

/** Server, Port, Verschlüsselung, Zugang, Absender und Empfänger der Admin-Mails. */
export function mailFields(input: InputRenderer, security: string, onSecurity: (security: MailSecurity) => void, disabled: boolean): TemplateResult {
  return html`<div class="field-row">
      ${input('mail_host', t('settings.general.mail.host'), { placeholder: 'smtp.example.com', mono: true, disabled })}
      ${input('mail_port', t('settings.general.mail.port'), { width: 'xs', disabled })}
    </div>
    ${mailSecurityField(security, onSecurity, disabled)} ${input('mail_username', t('settings.general.mail.user'), { disabled })}
    ${input('mail_password', t('settings.general.mail.password'), { type: 'password', autocomplete: 'new-password', disabled, placeholder: t('settings.form.unchangedSecret') })}
    ${input('mail_from', t('settings.general.mail.from'), { placeholder: 'lcm@example.com', width: 'l', disabled, hint: t('settings.general.mail.fromHint') })}
    ${input('mail_admin_recipients', t('settings.general.mail.admins'), { placeholder: 'admin@example.com', width: 'l', disabled, hint: t('settings.general.mail.adminsHint') })}`;
}

/** Wie lange Job-Verläufe, Routine-Protokolle und der Speicherverlauf aufgehoben werden. */
export function retentionFields(input: InputRenderer): TemplateResult {
  return html`${input('log_retention_days', t('settings.general.keep.logs'), { hint: t('settings.general.keep.logsHint') })}
    ${input('routine_log_retention_days', t('settings.general.keep.routine'), { hint: t('settings.general.keep.routineHint') })}
    ${input('storage_history_retention_days', t('settings.general.keep.storage'), { hint: t('settings.general.keep.storageHint') })}`;
}
