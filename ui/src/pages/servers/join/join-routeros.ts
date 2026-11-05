import { LitElement, css, html, nothing } from 'lit';
import { live } from 'lit/directives/live.js';
import '../../../components/lcm-button';
import '../../../components/lcm-choice';
import '../../../components/lcm-command';
import '../../../components/lcm-input';
import '../../../components/lcm-notice';
import type { ChoiceOption } from '../../../components/lcm-choice';
import { baseStyles, emit } from '../../../components/shared';
import { createRouterOS, probeHostKey, type HostKeyProbe, type JoinAuthMethod, type RouterOSResult } from '../../../api/provisioning';
import { I18nController } from '../../../lib/controllers';
import { errorText, t } from '../../../lib/i18n';
import { LIMITS } from '../../../lib/validate';
import { router } from '../../../lib/router';
import { unsaved } from '../../../lib/unsaved';
import { formStyles } from '../../account/form-styles';
import { EXAMPLES, emptyRouterOSForm, hasProblems, parsePort, routerOSBody, routerOSImportCommand, routerOSProblems, type FormProblems, type RouterOSForm } from './join-model';

type RouterOSProblems = FormProblems<RouterOSForm>;
import { problemText } from './join-parts';
import './join-parts';
import { joinStyles } from './join-styles';

type Step = 'form' | 'fingerprint' | 'key';

/**
 * <lcm-join-routeros> – MikroTik-RouterOS-Gerät zur Überwachung aufnehmen: Verbindungsdaten → Host-Schlüssel bestätigen
 * → anlegen. Mit Passwort geht es danach direkt zum Gerät; mit Schlüssel zeigt LCM den öffentlichen Schlüssel zum Import.
 */
export class LcmJoinRouterOS extends LitElement {
  static override properties = {
    _step: { state: true },
    _form: { state: true },
    _tried: { state: true },
    _probe: { state: true },
    _busy: { state: true },
    _error: { state: true },
    _result: { state: true },
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

  declare _step: Step;
  declare _form: RouterOSForm;
  declare _tried: boolean;
  declare _probe: HostKeyProbe | null;
  declare _busy: boolean;
  declare _error: string;
  declare _result: RouterOSResult | null;

  constructor() {
    super();
    this._step = 'form';
    this._form = emptyRouterOSForm();
    this._tried = false;
    this._probe = null;
    this._busy = false;
    this._error = '';
    this._result = null;
    new I18nController(this);
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    unsaved.set(this, false);
    this._form = { ...this._form, password: '' };
  }

  override willUpdate(changed: Map<string, unknown>): void {
    unsaved.set(this, this._step !== 'key' && (this._form.name !== '' || this._form.host !== ''));
    if (changed.has('_step')) emit(this, 'lcm-join-lock', { locked: this._step === 'key' });
  }

  #set(patch: Partial<RouterOSForm>): void {
    this._form = { ...this._form, ...patch };
  }

  async #probe(): Promise<void> {
    this._tried = true;
    if (hasProblems(routerOSProblems(this._form)) || this._busy) return;
    await this.#busy(async () => {
      this._probe = await probeHostKey(this._form.host.trim(), parsePort(this._form.port) ?? 22);
      this._step = 'fingerprint';
    });
  }

  async #create(): Promise<void> {
    const probe = this._probe;
    if (!probe) return;
    await this.#busy(async () => {
      const result = await createRouterOS(routerOSBody(this._form, probe.fingerprint));
      this._form = { ...this._form, password: '' };
      if (result.public_key) {
        this._result = result;
        this._step = 'key';
      } else {
        await router.navigate(`/servers/${result.server.id}`);
      }
    });
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

  #authOptions(): ChoiceOption[] {
    return [
      { id: 'password', label: t('joinWizard.auth.password'), icon: 'lock' },
      { id: 'key', label: t('joinWizard.routeros.keyAuth'), icon: 'key' },
    ];
  }

  #identityFields(problems: RouterOSProblems) {
    const form = this._form;
    return html`<lcm-input
        label=${t('joinWizard.field.name')}
        maxlength=${LIMITS.name}
        placeholder=${EXAMPLES.routerName}
        .value=${live(form.name)}
        error=${problemText(problems.name, this._tried)}
        @lcm-input=${(event: CustomEvent<{ value: string }>) => this.#set({ name: event.detail.value })}
      ></lcm-input>
      <div class="row">
        <lcm-input
          label=${t('joinWizard.field.host')}
          maxlength=${LIMITS.host}
          placeholder=${EXAMPLES.routerHost}
          mono
          .value=${live(form.host)}
          error=${problemText(problems.host, this._tried)}
          @lcm-input=${(event: CustomEvent<{ value: string }>) => this.#set({ host: event.detail.value })}
        ></lcm-input>
        <lcm-input
          label=${t('joinWizard.field.sshPort')}
          digits
          maxlength=5
          width="xs"
          mono
          .value=${live(form.port)}
          error=${problemText(problems.port, this._tried)}
          @lcm-input=${(event: CustomEvent<{ value: string }>) => this.#set({ port: event.detail.value })}
        ></lcm-input>
      </div>
      <lcm-input
        label=${t('joinWizard.field.loginUser')}
        maxlength=${LIMITS.loginUser}
        hint=${t('joinWizard.routeros.userHint')}
        mono
        autocomplete="off"
        .value=${live(form.loginUser)}
        error=${problemText(problems.loginUser, this._tried)}
        @lcm-input=${(event: CustomEvent<{ value: string }>) => this.#set({ loginUser: event.detail.value })}
      ></lcm-input>`;
  }

  #authFields(problems: RouterOSProblems) {
    const form = this._form;
    return html`<lcm-choice
        variant="segmented"
        label=${t('joinWizard.auth.label')}
        .options=${this.#authOptions()}
        value=${form.authMethod}
        @lcm-change=${(event: CustomEvent<{ value: JoinAuthMethod }>) => this.#set({ authMethod: event.detail.value })}
      ></lcm-choice>
      ${form.authMethod === 'password'
        ? html`<lcm-input
            label=${t('joinWizard.field.password')}
            maxlength=${LIMITS.password}
            type="password"
            autocomplete="off"
            hint=${t('joinWizard.routeros.passwordHint')}
            .value=${live(form.password)}
            error=${problemText(problems.password, this._tried)}
            @lcm-input=${(event: CustomEvent<{ value: string }>) => this.#set({ password: event.detail.value })}
          ></lcm-input>`
        : html`<lcm-notice>${t('joinWizard.routeros.keyModeHint')}</lcm-notice>`}`;
  }

  #formStep() {
    const problems = routerOSProblems(this._form);
    return html`<div
      class="stack"
      @keydown=${(event: KeyboardEvent) => {
        if (event.key === 'Enter' && event.composedPath()[0] instanceof HTMLInputElement) void this.#probe();
      }}
    >
      ${this.#identityFields(problems)} ${this.#authFields(problems)}
      ${this._error ? html`<lcm-notice tone="crit">${this._error}</lcm-notice>` : nothing}
      <div class="actions">
        <lcm-button variant="primary" icon="arrow-right" ?busy=${this._busy} @click=${() => void this.#probe()}>${t('joinWizard.probe')}</lcm-button>
      </div>
    </div>`;
  }

  #keyStep(result: RouterOSResult) {
    return html`<div class="stack">
      <lcm-notice tone="ok">${t('joinWizard.routeros.keyReady', { name: result.server.name })}</lcm-notice>
      <p class="lead">${t('joinWizard.routeros.keyReadyHint')}</p>
      <lcm-command
        label=${t('joinWizard.routeros.publicKey')}
        .value=${result.public_key ?? ''}
        copy-label=${t('joinWizard.copy')}
        copied-label=${t('joinWizard.copied')}
      ></lcm-command>
      <lcm-command
        label=${t('joinWizard.routeros.importCommand')}
        .value=${routerOSImportCommand(this._form.loginUser)}
        copy-label=${t('joinWizard.copy')}
        copied-label=${t('joinWizard.copied')}
      ></lcm-command>
      <div class="actions">
        <lcm-button variant="primary" icon="server" href=${`/servers/${result.server.id}`}>${t('joinWizard.result.toServer')}</lcm-button>
      </div>
    </div>`;
  }

  #body() {
    if (this._step === 'key' && this._result) return this.#keyStep(this._result);
    if (this._step === 'fingerprint' && this._probe) {
      return html`${this._error ? html`<lcm-notice tone="crit">${this._error}</lcm-notice>` : nothing}
        <lcm-join-fingerprint
          heading=${t('joinWizard.ssh.fingerprintTitle', { host: this._form.host.trim() })}
          key-type=${this._probe.key_type}
          fingerprint=${this._probe.fingerprint}
          hint=${t('joinWizard.ssh.fingerprintHint')}
          confirm-label=${t('joinWizard.routeros.create')}
          ?busy=${this._busy}
          @lcm-back=${() => (this._step = 'form')}
          @lcm-confirm=${() => void this.#create()}
        ></lcm-join-fingerprint>`;
    }
    return this.#formStep();
  }

  override render() {
    const steps = [t('joinWizard.step.connection'), t('joinWizard.step.fingerprint'), t('joinWizard.step.done')];
    const current = this._step === 'form' ? 0 : this._step === 'fingerprint' ? 1 : 2;
    return html`<div class="panel">
      <lcm-join-steps .steps=${steps} current=${current}></lcm-join-steps>
      ${this.#body()}
    </div>`;
  }
}
customElements.define('lcm-join-routeros', LcmJoinRouterOS);
