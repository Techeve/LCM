import { LitElement, css, html, nothing } from 'lit';
import { live } from 'lit/directives/live.js';
import '../../components/lcm-button';
import '../../components/lcm-card';
import '../../components/lcm-dialog';
import '../../components/lcm-input';
import '../../components/lcm-notice';
import '../../components/lcm-status';
import type { Impact } from '../../components/lcm-dialog';
import type { LcmInput } from '../../components/lcm-input';
import { toast } from '../../components/lcm-toaster';
import { baseStyles } from '../../components/shared';
import { TOTP_INPUT_MAX, cleanTotpCode, disableTotp, enableTotp, groupSecret, startTotpSetup, type TotpSetup } from '../../api/account';
import { auth } from '../../lib/auth';
import { I18nController, SubscribeController } from '../../lib/controllers';
import { errorText, t } from '../../lib/i18n';
import { router } from '../../lib/router';
import { safeQrSource } from './account-model';
import { formStyles } from './form-styles';

/**
 * <lcm-account-two-factor ?required return-to="/servers"> – Zwei-Faktor-Anmeldung (TOTP) einrichten, bestätigen
 * und abschalten. Das Geheimnis steht nur in diesem Element und verschwindet beim Verlassen. Ist die Einrichtung
 * Pflicht (`required`), geht es nach dem Bestätigen zu `return-to`.
 */
export class LcmAccountTwoFactor extends LitElement {
  static override properties = {
    required: { type: Boolean },
    returnTo: { type: String, attribute: 'return-to' },
    _setup: { state: true },
    _code: { state: true },
    _disabling: { state: true },
    _disableCode: { state: true },
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
      .setup {
        display: grid;
        grid-template-columns: auto minmax(0, 1fr);
        gap: var(--space-5);
        align-items: start;
      }
      .qr {
        display: grid;
        place-items: center;
        padding: var(--space-3);
        border-radius: var(--radius-l);
        /* Der QR-Code braucht hellen Grund, auch im dunklen Theme. */
        background: #fff;
        box-shadow: var(--elev-1);
      }
      img {
        display: block;
        inline-size: 12rem;
        block-size: 12rem;
        image-rendering: pixelated;
      }
      ol {
        display: grid;
        gap: var(--space-3);
        margin: 0;
        padding-inline-start: 1.2rem;
        line-height: 1.5;
      }
      details summary {
        color: var(--accent-text);
        font-size: var(--fs-s);
        cursor: pointer;
      }
      .secret {
        margin-block-start: var(--space-2);
        font-family: var(--font-mono);
        font-size: var(--fs-m);
        letter-spacing: 0.08em;
        user-select: all;
      }
      @media (max-width: 40rem) {
        .setup {
          grid-template-columns: minmax(0, 1fr);
        }
      }
    `,
  ];

  declare required: boolean;
  declare returnTo: string;
  declare _setup: TotpSetup | null;
  declare _code: string;
  declare _disabling: boolean;
  declare _disableCode: string;
  declare _busy: boolean;
  declare _error: string;

  constructor() {
    super();
    this.required = false;
    this.returnTo = '/';
    this._setup = null;
    this._code = '';
    this._disabling = false;
    this._disableCode = '';
    this._busy = false;
    this._error = '';
    new I18nController(this);
    new SubscribeController(this, (listener) => auth.onChange(listener));
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    this._setup = null; // das Geheimnis bleibt nicht im Speicher liegen
    this._code = '';
  }

  async #run(action: () => Promise<void>): Promise<void> {
    if (this._busy) return;
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

  #start(): Promise<void> {
    return this.#run(async () => {
      this._setup = await startTotpSetup();
      this._code = '';
      await this.updateComplete;
      this.renderRoot.querySelector<LcmInput>('lcm-input.code')?.focus();
    });
  }

  #enable(): Promise<void> {
    return this.#run(async () => {
      await enableTotp(this._code);
      this._setup = null;
      this._code = '';
      await auth.refresh();
      toast({ tone: 'ok', title: t('account.twoFactor.enabled') });
      if (this.required && !auth.remediation) await router.navigate(this.returnTo, { replace: true });
    });
  }

  #disable(): Promise<void> {
    if (this._disableCode.length !== 6) {
      this._error = t('account.twoFactor.codeMissing');
      return Promise.resolve();
    }
    return this.#run(async () => {
      try {
        await disableTotp(this._disableCode);
      } finally {
        this._disableCode = '';
      }
      this._disabling = false;
      await auth.refresh();
      toast({ tone: 'warn', title: t('account.twoFactor.disabled') });
      // Verlangt eine Rolle 2FA, ist das Konto jetzt im Pflichtschritt: der Router sperrt den Rest.
      await router.reevaluate();
    });
  }

  #onCode(event: CustomEvent<{ value: string }>): void {
    this._code = cleanTotpCode(event.detail.value);
    if (this._code.length === 6) void this.#enable();
  }

  #disableImpacts(): Impact[] {
    return [
      { label: t('account.twoFactor.impact.login'), value: t('account.twoFactor.impact.loginValue'), tone: 'warn' },
      { label: t('account.twoFactor.impact.policy'), value: t('account.twoFactor.impact.policyValue') },
      { label: t('account.twoFactor.impact.reversible'), value: t('account.twoFactor.impact.reversibleValue'), tone: 'ok' },
    ];
  }

  #active() {
    return html`<div class="stack">
      <lcm-status level="great" label=${t('account.twoFactor.active')} variant="pill"></lcm-status>
      <p class="lead">${t('account.twoFactor.activeText')}</p>
      <div class="actions">
        <lcm-button variant="danger" icon="x" @click=${() => (this._disabling = true)}>${t('account.twoFactor.disable')}</lcm-button>
      </div>
      <lcm-dialog
        ?open=${this._disabling}
        tone="danger"
        heading=${t('account.twoFactor.confirm.heading')}
        message=${t('account.twoFactor.confirm.message')}
        confirm-label=${t('account.twoFactor.confirm.label')}
        cancel-label=${t('account.cancel')}
        .impacts=${this.#disableImpacts()}
        ?busy=${this._busy}
        error=${this._disabling ? this._error : ''}
        @lcm-confirm=${() => void this.#disable()}
        @lcm-cancel=${() => {
          this._disabling = false;
          this._disableCode = '';
          this._error = '';
        }}
      >
        <lcm-input
          label=${t('account.twoFactor.code')}
          width="s"
          mono
          digits
          maxlength=${TOTP_INPUT_MAX}
          autocomplete="one-time-code"
          placeholder="000000"
          .value=${live(this._disableCode)}
          @lcm-input=${(event: CustomEvent<{ value: string }>) => (this._disableCode = cleanTotpCode(event.detail.value))}
        ></lcm-input>
      </lcm-dialog>
    </div>`;
  }

  #inactive() {
    return html`<div class="stack">
      <p class="lead">${t('account.twoFactor.intro')}</p>
      <div class="actions">
        <lcm-button variant="primary" icon="shield" ?busy=${this._busy} @click=${() => void this.#start()}>${t('account.twoFactor.setup')}</lcm-button>
      </div>
    </div>`;
  }

  #setupView(setup: TotpSetup) {
    const qr = safeQrSource(setup.qr_code);
    return html`<div class="setup">
      ${qr ? html`<div class="qr"><img src=${qr} alt=${t('account.twoFactor.qrAlt')} width="192" height="192" /></div>` : nothing}
      <div class="stack">
        <ol>
          <li>${t('account.twoFactor.step.scan')}</li>
          <li>${t('account.twoFactor.step.code')}</li>
        </ol>
        <details>
          <summary>${t('account.twoFactor.noScanner')}</summary>
          <div class="secret">${groupSecret(setup.secret)}</div>
        </details>
        <lcm-input
          class="code"
          label=${t('account.twoFactor.code')}
          width="s"
          mono
          digits
          maxlength=${TOTP_INPUT_MAX}
          autocomplete="one-time-code"
          placeholder="000000"
          .value=${live(this._code)}
          @lcm-input=${this.#onCode}
        ></lcm-input>
        <div class="actions">
          <lcm-button variant="primary" ?disabled=${this._code.length !== 6} ?busy=${this._busy} @click=${() => void this.#enable()}
            >${t('account.twoFactor.enable')}</lcm-button
          >
          ${this.required ? nothing : html`<lcm-button variant="ghost" @click=${() => (this._setup = null)}>${t('account.cancel')}</lcm-button>`}
        </div>
      </div>
    </div>`;
  }

  #body() {
    if (auth.user?.totp_enabled) return this.#active();
    return this._setup ? this.#setupView(this._setup) : this.#inactive();
  }

  override render() {
    return html`<lcm-card level="2" heading=${t('account.twoFactor.title')} subtitle=${t('account.twoFactor.subtitle')}>
      <div class="stack">
        ${this.#body()} ${this._error && !this._disabling ? html`<lcm-notice tone="crit">${this._error}</lcm-notice>` : nothing}
      </div>
    </lcm-card>`;
  }
}
customElements.define('lcm-account-two-factor', LcmAccountTwoFactor);
