import { html, nothing, type TemplateResult } from 'lit';
import '../../components/lcm-notice';
import '../../components/lcm-select';
import { t } from '../../lib/i18n';
import { MAIL_SECURITY, mailSecurityOptions, readMailSecurity, type MailSecurity } from '../../lib/mail-security';

/**
 * Auswahl der Verschlüsselung des Mailversands (TLS, STARTTLS, Keine) für System-Mailer und E-Mail-Kanal.
 * Bei „Keine“ steht direkt darunter die Warnung vor dem Klartext.
 */
export function mailSecurityField(value: string, onChange: (security: MailSecurity) => void, disabled = false): TemplateResult {
  const security = readMailSecurity(value, undefined);
  return html`<lcm-select
    label=${t('settings.mailSecurity.label')}
    width="s"
    ?disabled=${disabled}
    .options=${mailSecurityOptions()}
    .value=${security}
    @lcm-change=${(e: CustomEvent<{ value: string }>) => onChange(readMailSecurity(e.detail.value, undefined))}
  ></lcm-select>
  ${security === MAIL_SECURITY.none ? html`<lcm-notice tone="warn" variant="line">${t('settings.mailSecurity.noneWarning')}</lcm-notice>` : nothing}`;
}
