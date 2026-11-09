import { LitElement, css, html, nothing } from 'lit';
import { live } from 'lit/directives/live.js';
import '../../../components/lcm-button';
import '../../../components/lcm-input';
import '../../../components/lcm-notice';
import { baseStyles } from '../../../components/shared';
import { createDsm, probeDsm } from '../../../api/provisioning';
import { I18nController } from '../../../lib/controllers';
import { errorText, t } from '../../../lib/i18n';
import { LIMITS } from '../../../lib/validate';
import { router } from '../../../lib/router';
import { unsaved } from '../../../lib/unsaved';
import { formStyles } from '../../account/form-styles';
import { EXAMPLES, dsmBody, dsmProblems, emptyDsmForm, hasProblems, parsePort, type DsmForm, type FormProblems } from './join-model';

type DsmProblems = FormProblems<DsmForm>;
import { problemText } from './join-parts';
import './join-parts';
import { joinStyles } from './join-styles';

/**
 * <lcm-join-dsm> – Synology DSM über die DSM-Web-API aufnehmen: Verbindungsdaten → Zertifikat bestätigen (erst danach
 * gehen die Zugangsdaten über die Leitung) → anlegen und zum Gerät.
 */
export class LcmJoinDsm extends LitElement {
  static override properties = {
    _form: { state: true },
    _tried: { state: true },
    _fingerprint: { state: true },
    _busy: { state: true },
    _error: { state: true },
  };
  static override styles = [
    baseStyles,
    formStyles,
    joinStyles,
    css`
      :host {
        display: block;
      }
    `,
  ];

  declare _form: DsmForm;
  declare _tried: boolean;
  declare _fingerprint: string;
  declare _busy: boolean;
  declare _error: string;

  constructor() {
    super();
    this._form = emptyDsmForm();
    this._tried = false;
    this._fingerprint = '';
    this._busy = false;
    this._error = '';
    new I18nController(this);
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    unsaved.set(this, false);
    this._form = { ...this._form, password: '' };
  }

  override willUpdate(): void {
    unsaved.set(this, this._form.name !== '' || this._form.host !== '' || this._form.password !== '');
  }

  #set(patch: Partial<DsmForm>): void {
    this._form = { ...this._form, ...patch };
  }

  async #busy(action: () => Promise<void>): Promise<void> {
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

  async #probe(): Promise<void> {
    this._tried = true;
    if (hasProblems(dsmProblems(this._form))) return;
    await this.#busy(async () => {
      this._fingerprint = await probeDsm(this._form.host.trim(), parsePort(this._form.port) ?? 5001);
    });
  }

  async #create(): Promise<void> {
    await this.#busy(async () => {
      const server = await createDsm(dsmBody(this._form, this._fingerprint));
      this._form = emptyDsmForm();
      unsaved.set(this, false);
      await router.navigate(`/servers/${server.id}`);
    });
  }

  #addressFields(problems: DsmProblems) {
    const form = this._form;
    return html`<lcm-input
        label=${t('joinWizard.field.name')}
        maxlength=${LIMITS.name}
        placeholder=${EXAMPLES.nasName}
        .value=${live(form.name)}
        error=${problemText(problems.name, this._tried)}
        @lcm-input=${(event: CustomEvent<{ value: string }>) => this.#set({ name: event.detail.value })}
      ></lcm-input>
      <div class="row">
        <lcm-input
          label=${t('joinWizard.field.host')}
          maxlength=${LIMITS.host}
          placeholder=${EXAMPLES.nasHost}
          mono
          .value=${live(form.host)}
          error=${problemText(problems.host, this._tried)}
          @lcm-input=${(event: CustomEvent<{ value: string }>) => this.#set({ host: event.detail.value })}
        ></lcm-input>
        <lcm-input
          label=${t('joinWizard.dsm.port')}
          digits
          maxlength=5
          width="xs"
          mono
          .value=${live(form.port)}
          error=${problemText(problems.port, this._tried)}
          @lcm-input=${(event: CustomEvent<{ value: string }>) => this.#set({ port: event.detail.value })}
        ></lcm-input>
      </div>`;
  }

  #accountFields(problems: DsmProblems) {
    const form = this._form;
    return html`<lcm-input
        label=${t('joinWizard.dsm.account')}
        maxlength=${LIMITS.loginUser}
        hint=${t('joinWizard.dsm.accountHint')}
        autocomplete="off"
        .value=${live(form.account)}
        error=${problemText(problems.account, this._tried)}
        @lcm-input=${(event: CustomEvent<{ value: string }>) => this.#set({ account: event.detail.value })}
      ></lcm-input>
      <lcm-input
        label=${t('joinWizard.field.password')}
        maxlength=${LIMITS.password}
        type="password"
        autocomplete="off"
        .value=${live(form.password)}
        error=${problemText(problems.password, this._tried)}
        @lcm-input=${(event: CustomEvent<{ value: string }>) => this.#set({ password: event.detail.value })}
      ></lcm-input>`;
  }

  #formStep() {
    const problems = dsmProblems(this._form);
    return html`<div
      class="stack"
      @keydown=${(event: KeyboardEvent) => {
        if (event.key === 'Enter' && event.composedPath()[0] instanceof HTMLInputElement) void this.#probe();
      }}
    >
      ${this.#addressFields(problems)} ${this.#accountFields(problems)}
      ${this._error ? html`<lcm-notice tone="crit">${this._error}</lcm-notice>` : nothing}
      <div class="actions">
        <lcm-button variant="primary" icon="arrow-right" ?busy=${this._busy} @click=${() => void this.#probe()}>${t('joinWizard.dsm.probe')}</lcm-button>
      </div>
    </div>`;
  }

  override render() {
    const steps = [t('joinWizard.step.connection'), t('joinWizard.step.certificate')];
    const confirming = this._fingerprint !== '';
    return html`<div class="panel">
      <lcm-join-steps .steps=${steps} current=${confirming ? 1 : 0}></lcm-join-steps>
      ${confirming
        ? html`${this._error ? html`<lcm-notice tone="crit">${this._error}</lcm-notice>` : nothing}
            <lcm-join-fingerprint
              heading=${t('joinWizard.dsm.fingerprintTitle', { host: this._form.host.trim() })}
              key-type="SHA-256"
              fingerprint=${this._fingerprint}
              hint=${t('joinWizard.dsm.fingerprintHint')}
              confirm-label=${t('joinWizard.dsm.create')}
              ?busy=${this._busy}
              @lcm-back=${() => (this._fingerprint = '')}
              @lcm-confirm=${() => void this.#create()}
            ></lcm-join-fingerprint>`
        : this.#formStep()}
    </div>`;
  }
}
customElements.define('lcm-join-dsm', LcmJoinDsm);
