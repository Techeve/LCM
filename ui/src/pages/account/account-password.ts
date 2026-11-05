import { LitElement, css, html, nothing } from 'lit';
import { live } from 'lit/directives/live.js';
import '../../components/lcm-button';
import '../../components/lcm-card';
import '../../components/lcm-dialog';
import '../../components/lcm-input';
import '../../components/lcm-save-bar';
import type { Impact } from '../../components/lcm-dialog';
import { toast } from '../../components/lcm-toaster';
import { baseStyles } from '../../components/shared';
import { TOTP_INPUT_MAX, changeOwnPassword, cleanTotpCode } from '../../api/account';
import { auth } from '../../lib/auth';
import { I18nController, SubscribeController } from '../../lib/controllers';
import { errorText, t } from '../../lib/i18n';
import { router } from '../../lib/router';
import { saveBarLabels } from '../../lib/save-bar';
import { loginHref } from '../../lib/safe-path';
import { LIMITS } from '../../lib/validate';
import { identityOf } from './account-model';
import { formStyles } from './form-styles';
import './new-password';
import type { LcmAccountNewPassword, NewPasswordDetail } from './new-password';

/**
 * <lcm-account-password return-to="/servers/42"> – eigenes Passwort ändern: aktuelles Passwort, bei aktiver 2FA ein
 * Code, das neue Passwort mit Regeln. Das Backend beendet danach alle Sitzungen und entwertet ältere API-Schlüssel;
 * die Seite meldet deshalb ab und führt zur Anmeldung, die danach zu `return-to` zurückführt.
 */
export class LcmAccountPassword extends LitElement {
  static override properties = {
    returnTo: { type: String, attribute: 'return-to' },
    _current: { state: true },
    _code: { state: true },
    _password: { state: true },
    _valid: { state: true },
    _confirming: { state: true },
    _busy: { state: true },
    _error: { state: true },
  };
  static override styles = [
    baseStyles,
    formStyles,
    css`
      :host {
        display: block;
      }
      .stack {
        max-inline-size: 30rem;
      }
    `,
  ];

  declare returnTo: string;
  declare _current: string;
  declare _code: string;
  declare _password: string;
  declare _valid: boolean;
  declare _confirming: boolean;
  declare _busy: boolean;
  declare _error: string;

  constructor() {
    super();
    this.returnTo = '/account/security';
    this._current = '';
    this._code = '';
    this._password = '';
    this._valid = false;
    this._confirming = false;
    this._busy = false;
    this._error = '';
    new I18nController(this);
    new SubscribeController(this, (listener) => auth.onChange(listener));
  }

  get #needsCode(): boolean {
    return Boolean(auth.user?.totp_enabled);
  }

  get #ready(): boolean {
    return this._valid && this._current.length > 0 && (!this.#needsCode || this._code.length === 6);
  }

  #impacts(): Impact[] {
    return [
      { label: t('account.password.impact.sessions'), value: t('account.password.impact.sessionsValue'), tone: 'warn' },
      { label: t('account.password.impact.apiKeys'), value: t('account.password.impact.apiKeysValue'), tone: 'warn' },
      { label: t('account.password.impact.reversible'), value: t('account.password.impact.reversibleValue') },
    ];
  }

  #ask(): void {
    if (!this.#ready) return;
    this._error = '';
    this._confirming = true;
  }

  async #change(): Promise<void> {
    const user = auth.user;
    if (!user || this._busy) return;
    this._busy = true;
    try {
      await changeOwnPassword(user.id, { password: this._password, current_password: this._current, code: this._code });
    } catch (error) {
      this._error = errorText(error);
      this._busy = false;
      this._confirming = false;
      return;
    }
    // Die Sitzung gilt ab jetzt als beendet: sauber abmelden (auch die anderen Tabs) und zur Anmeldung.
    toast({ tone: 'ok', title: t('account.password.done'), message: t('account.password.doneText') });
    await auth.logout();
    await router.navigate(loginHref(this.returnTo), { replace: true });
  }

  #onKeyDown(event: KeyboardEvent): void {
    const target = event.composedPath()[0];
    if (event.key !== 'Enter' || !(target instanceof HTMLInputElement)) return;
    event.preventDefault();
    this.#ask();
  }

  override render() {
    const user = auth.user;
    return html`<lcm-card level="2" heading=${t('account.password.title')} subtitle=${t('account.password.subtitle')}>
      <div class="stack" @keydown=${this.#onKeyDown}>
        <lcm-input
          label=${t('account.password.current')}
          type="password"
          autocomplete="current-password"
          maxlength=${LIMITS.password}
          .value=${live(this._current)}
          @lcm-input=${(event: CustomEvent<{ value: string }>) => (this._current = event.detail.value)}
        ></lcm-input>
        ${this.#needsCode
          ? html`<lcm-input
              label=${t('account.password.code')}
              width="s"
              hint=${t('account.password.codeHint')}
              mono
              digits
              maxlength=${TOTP_INPUT_MAX}
              autocomplete="one-time-code"
              placeholder="000000"
              .value=${live(this._code)}
              @lcm-input=${(event: CustomEvent<{ value: string }>) => (this._code = cleanTotpCode(event.detail.value))}
            ></lcm-input>`
          : nothing}
        <lcm-account-new-password
          .identity=${identityOf(user)}
          @lcm-input=${(event: CustomEvent<NewPasswordDetail>) => {
            this._password = event.detail.value;
            this._valid = event.detail.valid;
          }}
        ></lcm-account-new-password>
        <lcm-save-bar
          always
          ?dirty=${this.#ready}
          error=${this._error}
          .labels=${{ ...saveBarLabels(t('account.password.submit'), ''), unsaved: '' }}
          @lcm-save=${this.#ask}
        ></lcm-save-bar>
      </div>
      <lcm-dialog
        ?open=${this._confirming}
        heading=${t('account.password.confirm.heading')}
        message=${t('account.password.confirm.message')}
        confirm-label=${t('account.password.confirm.label')}
        cancel-label=${t('account.cancel')}
        .impacts=${this.#impacts()}
        ?busy=${this._busy}
        @lcm-confirm=${() => void this.#change()}
        @lcm-cancel=${() => (this._confirming = false)}
      ></lcm-dialog>
    </lcm-card>`;
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    this._current = '';
    this._password = '';
    this.renderRoot.querySelector<LcmAccountNewPassword>('lcm-account-new-password')?.clear();
  }
}
customElements.define('lcm-account-password', LcmAccountPassword);
