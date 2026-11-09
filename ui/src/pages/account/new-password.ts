import { LitElement, css, html } from 'lit';
import { live } from 'lit/directives/live.js';
import '../../components/lcm-input';
import '../../components/lcm-password';
import { baseStyles, emit } from '../../components/shared';
import { I18nController } from '../../lib/controllers';
import { t } from '../../lib/i18n';
import { checkPassword, PASSWORD_MAX_LENGTH, passwordFeedback, type PasswordIdentity } from './password-policy';

export interface NewPasswordDetail {
  value: string;
  /** Erfüllt die Regeln und beide Felder stimmen überein. */
  valid: boolean;
}

/** Ergebnis für beide Felder: Regeln erfüllt und Wiederholung gleich. */
export function newPasswordState(value: string, confirm: string, identity: PasswordIdentity) {
  const check = checkPassword(value, identity);
  const mismatch = confirm.length > 0 && confirm !== value;
  return { check, mismatch, valid: check.ok && confirm === value };
}

/**
 * <lcm-account-new-password label="Neues Passwort" .identity=${…}> – neues Passwort mit Stärkeanzeige, Wiederholung
 * und den Regeln des Servers als Liste. Meldet `lcm-input` { value, valid }. Die Regeln sind nur Benutzerführung,
 * verbindlich prüft der Server.
 */
export class LcmAccountNewPassword extends LitElement {
  static override properties = {
    label: { type: String },
    identity: { attribute: false },
    wide: { type: Boolean },
    _value: { state: true },
    _confirm: { state: true },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: grid;
        gap: var(--space-3);
      }
    `,
  ];

  declare label: string;
  declare identity: PasswordIdentity;
  /** In schmalen Karten (Aktivierung): Felder über die volle Breite. */
  declare wide: boolean;
  declare _value: string;
  declare _confirm: string;

  constructor() {
    super();
    this.wide = false;
    this.label = '';
    this.identity = {};
    this._value = '';
    this._confirm = '';
    new I18nController(this);
  }

  /** Leert beide Felder, z. B. nach erfolgreichem Speichern. */
  clear(): void {
    this._value = '';
    this._confirm = '';
  }

  #update(value: string, confirm: string): void {
    this._value = value;
    this._confirm = confirm;
    const { valid } = newPasswordState(value, confirm, this.identity);
    emit<NewPasswordDetail>(this, 'lcm-input', { value, valid });
  }

  #strengthLabels(): string[] {
    return [
      t('password.strength.veryWeak'),
      t('password.strength.weak'),
      t('password.strength.fair'),
      t('password.strength.strong'),
      t('password.strength.veryStrong'),
    ];
  }

  override render() {
    const { mismatch } = newPasswordState(this._value, this._confirm, this.identity);
    const { score, problems } = passwordFeedback(this._value, this.identity);
    return html`
      <lcm-password
        width=${this.wide ? 'full' : 'm'}
        label=${this.label || t('password.new')}
        maxlength=${PASSWORD_MAX_LENGTH}
        .value=${live(this._value)}
        hint=${t('password.requirements')}
        .score=${score}
        .problems=${problems}
        strength-title=${t('password.strengthLabel')}
        .strengthLabels=${this.#strengthLabels()}
        show-label=${t('password.show')}
        hide-label=${t('password.hide')}
        @lcm-input=${(event: CustomEvent<{ value: string }>) => {
          event.stopPropagation();
          this.#update(event.detail.value, this._confirm);
        }}
      ></lcm-password>
      <lcm-input
        width=${this.wide ? 'full' : 'm'}
        label=${t('password.confirm')}
        type="password"
        autocomplete="new-password"
        maxlength=${PASSWORD_MAX_LENGTH}
        .value=${live(this._confirm)}
        error=${mismatch ? t('password.mismatch') : ''}
        @lcm-input=${(event: CustomEvent<{ value: string }>) => {
          event.stopPropagation();
          this.#update(this._value, event.detail.value);
        }}
      ></lcm-input>
    `;
  }
}
customElements.define('lcm-account-new-password', LcmAccountNewPassword);
