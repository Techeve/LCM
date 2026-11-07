import { LitElement, css, html, nothing } from 'lit';
import { live } from 'lit/directives/live.js';
import '../components/lcm-button';
import '../components/lcm-card';
import '../components/lcm-input';
import '../components/lcm-logo';
import '../components/lcm-menu';
import '../components/lcm-notice';
import type { LcmInput } from '../components/lcm-input';
import { baseStyles } from '../components/shared';
import { api } from '../lib/api';
import { auth } from '../lib/auth';
import { I18nController, SubscribeController } from '../lib/controllers';
import { errorText, t } from '../lib/i18n';
import { LIMITS, isEmail } from '../lib/validate';
import { router } from '../lib/router';
import { onThemeChange } from '../lib/theme';
import { publicMenus } from './public/public-frame';

const CODE_LENGTH = 6;

type Step = 'credentials' | 'two-factor' | 'forgot' | 'forgot-sent';

/** Nur Ziffern, höchstens sechs: auch ein eingefügtes „123 456“ ergibt einen gültigen Code. */
export function cleanCode(raw: string): string {
  return raw.replace(/\D/g, '').slice(0, CODE_LENGTH);
}

/** Anmeldung mit optionalem zweiten Faktor und dem Zurücksetzen des Passworts. */
export class LoginPage extends LitElement {
  static override properties = {
    _step: { state: true },
    _username: { state: true },
    _password: { state: true },
    _code: { state: true },
    _email: { state: true },
    _error: { state: true },
    _busy: { state: true },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: grid;
        grid-template-rows: 1fr auto;
        justify-items: center;
        min-block-size: 100dvh;
        padding: var(--space-6) var(--page-pad) var(--space-4);
      }
      .stage {
        display: grid;
        align-content: center;
        gap: var(--space-5);
        inline-size: min(26rem, 100%);
      }
      header {
        display: grid;
        justify-items: center;
        gap: var(--space-2);
        text-align: center;
      }
      lcm-logo {
        inline-size: 4rem;
        block-size: 4rem;
        filter: drop-shadow(0 6px 16px var(--shadow-far));
      }
      h1 {
        margin: var(--space-2) 0 0;
        font-size: var(--fs-2xl);
        font-weight: var(--head-weight);
        letter-spacing: var(--head-tracking);
      }
      .subtitle {
        margin: 0;
        color: var(--text-2);
        font-size: var(--fs-m);
      }
      form {
        display: grid;
        gap: var(--space-4);
      }
      h2 {
        margin: 0;
        font-size: var(--fs-l);
        font-weight: var(--head-weight);
        letter-spacing: var(--head-tracking);
      }
      p {
        margin: 0;
        color: var(--text-2);
      }
      .links {
        display: flex;
        justify-content: center;
      }
      .first-start {
        font-size: var(--fs-s);
        text-align: center;
      }
      footer {
        display: flex;
        gap: var(--space-1);
        justify-content: center;
        padding-block-start: var(--space-5);
      }
    `,
  ];

  declare _step: Step;
  declare _username: string;
  declare _password: string;
  declare _code: string;
  declare _email: string;
  declare _error: string;
  declare _busy: boolean;
  #challenge = '';

  constructor() {
    super();
    this._step = 'credentials';
    this._username = '';
    this._password = '';
    this._code = '';
    this._email = '';
    this._error = '';
    this._busy = false;
    new I18nController(this);
    new SubscribeController(this, onThemeChange);
    new SubscribeController(this, (listener) => router.onChange(listener));
  }

  override connectedCallback(): void {
    super.connectedCallback();
    if (auth.isAuthenticated) void router.afterLogin();
  }

  override firstUpdated(): void {
    void this.#focusFirst();
  }

  // ---- Schritte ----

  async #submit(): Promise<void> {
    if (this._busy) return;
    if (this._step === 'credentials') await this.#run(() => this.#signIn());
    else if (this._step === 'two-factor') await this.#run(() => this.#verifyCode());
    else if (this._step === 'forgot') await this.#run(() => this.#requestReset());
  }

  async #run(action: () => Promise<void>): Promise<void> {
    this._busy = true;
    this._error = '';
    try {
      await action();
    } catch (error) {
      this._error = errorText(error);
    } finally {
      this._busy = false;
    }
  }

  async #signIn(): Promise<void> {
    if (!this._username || !this._password) return this.#focusFirst(this._username ? 'password' : 'username');
    const result = await auth.login(this._username.trim(), this._password);
    if (result.kind === 'done') {
      await router.afterLogin();
      return;
    }
    this.#challenge = result.challenge;
    this._password = '';
    this.#show('two-factor');
  }

  async #verifyCode(): Promise<void> {
    if (this._code.length !== CODE_LENGTH) return this.#focusFirst();
    await auth.loginTwoFactor(this.#challenge, this._code);
    await router.afterLogin();
  }

  async #requestReset(): Promise<void> {
    if (!this._email) return this.#focusFirst();
    if (!isEmail(this._email.trim())) {
      this._error = t('check.email');
      return this.#focusFirst();
    }
    await api.post('/api/v1/auth/password-reset', { email: this._email.trim() }, { anonymous: true });
    this._step = 'forgot-sent';
  }

  #show(step: Step): void {
    this._step = step;
    this._error = '';
    this._code = '';
    void this.updateComplete.then(() => this.#focusFirst());
  }

  /** Setzt den Cursor in das Feld mit dem Namen, sonst in das erste Feld des Schritts. */
  async #focusFirst(name?: string): Promise<void> {
    const fields = [...this.renderRoot.querySelectorAll<LcmInput>('lcm-input')];
    const field = fields.find((candidate) => candidate.name === name) ?? fields[0];
    await field?.updateComplete; // das Eingabefeld im Innern ist erst nach dem ersten Rendern da
    field?.focus();
  }

  #onKeyDown(event: KeyboardEvent): void {
    const target = event.composedPath()[0];
    if (event.key !== 'Enter' || target instanceof HTMLButtonElement) return;
    event.preventDefault();
    void this.#submit();
  }

  #onCode(event: CustomEvent<{ value: string }>): void {
    this._code = cleanCode(event.detail.value);
    if (this._code.length === CODE_LENGTH) void this.#submit();
  }

  // ---- Darstellung ----

  /** Warum man hier ist: der Betreiber hat den Weg der Anmeldung gewechselt, oder die Sitzung ist abgelaufen. */
  #hint() {
    if (router.state.query.get('reason') === 'session-mode') return html`<lcm-notice>${t('login.sessionModeChanged')}</lcm-notice>`;
    return router.state.query.get('next') ? html`<lcm-notice>${t('login.sessionHint')}</lcm-notice>` : nothing;
  }

  #credentials() {
    return html`
      ${this.#hint()}
      <lcm-input width="full"
        label=${t('login.username')}
        maxlength=${LIMITS.name}
        name="username"
        autocomplete="username"
        required
        .value=${live(this._username)}
        @lcm-input=${(event: CustomEvent<{ value: string }>) => (this._username = event.detail.value)}
      ></lcm-input>
      <lcm-input width="full"
        label=${t('login.password')}
        maxlength=${LIMITS.password}
        name="password"
        type="password"
        autocomplete="current-password"
        required
        .value=${live(this._password)}
        @lcm-input=${(event: CustomEvent<{ value: string }>) => (this._password = event.detail.value)}
      ></lcm-input>
      ${this.#error()}
      <lcm-button variant="primary" block ?busy=${this._busy} @click=${this.#submit}>
        ${t('login.submit')}
      </lcm-button>
      <div class="links"><lcm-button variant="ghost" size="small" @click=${() => this.#show('forgot')}>${t('login.forgot')}</lcm-button></div>
    `;
  }

  #twoFactor() {
    return html`
      <h2>${t('login.totpTitle')}</h2>
      <p>${t('login.totpHint')}</p>
      <lcm-input width="full"
        label=${t('login.totpCode')}
        name="code"
        mono
        code
        digits
        maxlength=${CODE_LENGTH + 1}
        autocomplete="one-time-code"
        placeholder="123456"
        required
        .value=${live(this._code)}
        @lcm-input=${this.#onCode}
      ></lcm-input>
      ${this.#error()}
      <lcm-button variant="primary" block ?busy=${this._busy} @click=${this.#submit}>
        ${t('login.confirm')}
      </lcm-button>
      <div class="links"><lcm-button variant="ghost" size="small" @click=${() => this.#show('credentials')}>${t('login.backToLogin')}</lcm-button></div>
    `;
  }

  #forgot() {
    return html`
      <h2>${t('login.forgot')}</h2>
      <p>${t('login.resetIntro')}</p>
      <lcm-input width="full"
        label=${t('login.email')}
        maxlength=${LIMITS.email}
        name="email"
        type="email"
        autocomplete="email"
        required
        .value=${live(this._email)}
        @lcm-input=${(event: CustomEvent<{ value: string }>) => (this._email = event.detail.value)}
      ></lcm-input>
      ${this.#error()}
      <lcm-button variant="primary" block ?busy=${this._busy} @click=${this.#submit}>${t('login.requestLink')}</lcm-button>
      <div class="links"><lcm-button variant="ghost" size="small" @click=${() => this.#show('credentials')}>${t('login.backToLogin')}</lcm-button></div>
    `;
  }

  #forgotSent() {
    return html`
      <lcm-notice tone="ok">${t('login.resetDone')}</lcm-notice>
      <lcm-button variant="outline" block @click=${() => this.#show('credentials')}>${t('login.backToLogin')}</lcm-button>
    `;
  }

  #error() {
    return this._error ? html`<lcm-notice tone="crit">${this._error}</lcm-notice>` : nothing;
  }

  #body() {
    switch (this._step) {
      case 'two-factor':
        return this.#twoFactor();
      case 'forgot':
        return this.#forgot();
      case 'forgot-sent':
        return this.#forgotSent();
      default:
        return this.#credentials();
    }
  }

  override render() {
    return html`
      <div class="stage">
        <header>
          <lcm-logo label=${t('nav.brand')}></lcm-logo>
          <h1>LCM</h1>
          <p class="subtitle">${t('login.subtitle')}</p>
        </header>
        <lcm-card>
          <form @keydown=${this.#onKeyDown} @submit=${(event: Event) => event.preventDefault()} aria-label=${t('login.title')}>${this.#body()}</form>
        </lcm-card>
        ${this._step === 'credentials' ? html`<p class="first-start">${t('login.firstStartHint')}</p>` : nothing}
      </div>
      <footer>${publicMenus()}</footer>
    `;
  }
}
customElements.define('lcm-page-login', LoginPage);
