import { LitElement, css, html, nothing } from 'lit';
import '../../components/lcm-button';
import '../../components/lcm-notice';
import { baseStyles } from '../../components/shared';
import { consumeAccountActivation } from '../../api/provisioning';
import { I18nController, SubscribeController } from '../../lib/controllers';
import { errorText, t } from '../../lib/i18n';
import { router } from '../../lib/router';
import { formStyles } from '../account/form-styles';
import '../account/new-password';
import type { NewPasswordDetail } from '../account/new-password';
import './public-frame';

/** Der Link aus der Mail führt zu `/activate?token=…` (Einladung und Zurücksetzen) oder `/password-reset?token=…`. */
export function activationMode(path: string): 'activate' | 'reset' {
  return path.startsWith('/password-reset') ? 'reset' : 'activate';
}

/**
 * Öffentliche Seite für Links aus Einladungs- und Zurücksetzen-Mails (`/activate`, `/password-reset`): setzt das
 * Passwort des LCM-Kontos. Der Link gilt einmal und nur kurz; nach Erfolg verschwindet das Token aus der Adresse.
 */
export class ActivatePage extends LitElement {
  static override properties = {
    _password: { state: true },
    _valid: { state: true },
    _busy: { state: true },
    _error: { state: true },
    _username: { state: true },
    _done: { state: true },
  };
  static override styles = [
    baseStyles,
    formStyles,
    css`
      :host {
        display: block;
      }
    `,
  ];

  declare _password: string;
  declare _valid: boolean;
  declare _busy: boolean;
  declare _error: string;
  declare _username: string;
  declare _done: boolean;
  #token = '';

  constructor() {
    super();
    this._password = '';
    this._valid = false;
    this._busy = false;
    this._error = '';
    this._username = '';
    this._done = false;
    new I18nController(this);
    new SubscribeController(this, (listener) => router.onChange(listener));
  }

  override willUpdate(): void {
    const token = router.state.query.get('token');
    if (token) this.#token = token;
  }

  get #mode() {
    return activationMode(router.state.path);
  }

  async #submit(): Promise<void> {
    if (!this._valid || this._busy) return;
    this._busy = true;
    this._error = '';
    try {
      const result = await consumeAccountActivation(this.#token, this._password);
      this._username = result.username ?? '';
      this._done = true;
      this._password = '';
      this.#token = '';
      router.setQuery({ token: null }, { replace: true });
      await this.updateComplete;
      this.renderRoot.querySelector<HTMLElement>('.success')?.focus();
    } catch (error) {
      this._error = errorText(error);
    } finally {
      this._busy = false;
    }
  }

  #onKeyDown(event: KeyboardEvent): void {
    const target = event.composedPath()[0];
    if (event.key !== 'Enter' || !(target instanceof HTMLInputElement)) return;
    event.preventDefault();
    void this.#submit();
  }

  #body() {
    if (this._done) {
      return html`<div class="stack">
        <lcm-notice tone="ok" class="success" tabindex="-1">${this._username ? t('activate.doneAs', { username: this._username }) : t('activate.done')}</lcm-notice>
        <lcm-button variant="primary" block href="/login">${t('activate.toLogin')}</lcm-button>
      </div>`;
    }
    if (!this.#token) {
      return html`<div class="stack">
        <lcm-notice tone="warn">${t('activate.noToken')}</lcm-notice>
        <lcm-button variant="outline" block href="/login">${t('activate.toLogin')}</lcm-button>
      </div>`;
    }
    return html`<div class="stack" @keydown=${this.#onKeyDown}>
      <lcm-account-new-password
        wide
        @lcm-input=${(event: CustomEvent<NewPasswordDetail>) => {
          this._password = event.detail.value;
          this._valid = event.detail.valid;
        }}
      ></lcm-account-new-password>
      ${this._error ? html`<lcm-notice tone="crit">${this._error}</lcm-notice>` : nothing}
      <lcm-button variant="primary" block ?disabled=${!this._valid} ?busy=${this._busy} @click=${() => void this.#submit()}>${t('activate.submit')}</lcm-button>
    </div>`;
  }

  override render() {
    const reset = this.#mode === 'reset';
    return html`<lcm-public-frame
      heading=${reset ? t('activate.resetTitle') : t('activate.title')}
      subtitle=${this._done ? '' : reset ? t('activate.resetIntro') : t('activate.intro')}
      >${this.#body()}</lcm-public-frame
    >`;
  }
}
customElements.define('lcm-page-activate', ActivatePage);
