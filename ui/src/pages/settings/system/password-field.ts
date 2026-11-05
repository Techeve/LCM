import { html, type TemplateResult } from 'lit';
import '../../../components/lcm-password';
import { t } from '../../../lib/i18n';
import { LIMITS } from '../../../lib/validate';
import { passwordFeedback, type PasswordIdentity } from '../../account/password-policy';

/**
 * Passwortfeld mit Stärkeanzeige; `onInput` bekommt den getippten Wert. Mit `identity` gilt die Regel des Servers für
 * neue Konten (Stärke und Gründe), ohne schätzt das Feld nur (Passphrasen für Sicherungen).
 */
export function passwordField(label: string, value: string, onInput: (value: string) => void, hint = '', maxlength: number = LIMITS.password, identity?: PasswordIdentity): TemplateResult {
  const feedback = identity ? passwordFeedback(value, identity) : undefined;
  return html`<lcm-password
    label=${label}
    hint=${hint}
    maxlength=${maxlength}
    .value=${value}
    .score=${feedback?.score}
    .problems=${feedback?.problems ?? []}
    strength-title=${t('settings.password.strength')}
    .strengthLabels=${[t('settings.password.level.0'), t('settings.password.level.1'), t('settings.password.level.2'), t('settings.password.level.3'), t('settings.password.level.4')]}
    show-label=${t('settings.password.show')}
    hide-label=${t('settings.password.hide')}
    @lcm-input=${(e: CustomEvent<{ value: string }>) => onInput(e.detail.value)}
  ></lcm-password>`;
}
