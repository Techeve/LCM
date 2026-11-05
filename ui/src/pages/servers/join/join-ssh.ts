import { LitElement, css, html, nothing } from 'lit';
import { live } from 'lit/directives/live.js';
import '../../../components/lcm-button';
import '../../../components/lcm-choice';
import '../../../components/lcm-command';
import '../../../components/lcm-input';
import '../../../components/lcm-notice';
import '../../../components/lcm-server-action';
import '../../../components/lcm-switch';
import type { ChoiceOption } from '../../../components/lcm-choice';
import { baseStyles } from '../../../components/shared';
import { normaliseServers, serverSummaryUrl, type ServerItem } from '../../../api/servers';
import {
  joinServer,
  onboardingPublicKey,
  probeHostKey,
  reconnectServer,
  type HostKeyProbe,
  type JoinAuthMethod,
  type JoinedServer,
} from '../../../api/provisioning';
import { api } from '../../../lib/api';
import { I18nController } from '../../../lib/controllers';
import { errorText, t } from '../../../lib/i18n';
import { LIMITS } from '../../../lib/validate';
import { unsaved } from '../../../lib/unsaved';
import { formStyles } from '../../account/form-styles';
import { actionLabels } from '../preview';
import {
  EXAMPLES,
  duplicateOf,
  emptySshForm,
  hasProblems,
  hostKeyFile,
  joinBody,
  parsePort,
  reconnectBody,
  sshProblems,
  type FormProblems,
  type KnownServer,
  type SshForm,
} from './join-model';
import { problemText } from './join-parts';
import './join-parts';
import './root-guide';
import { joinStyles } from './join-styles';

type SshProblems = FormProblems<SshForm>;

type Step = 'form' | 'fingerprint' | 'confirm' | 'running' | 'done';
const STEP_INDEX: Record<Step, number> = { form: 0, fingerprint: 1, confirm: 2, running: 2, done: 3 };

function known(server: ServerItem): KnownServer {
  return { id: server.id, name: server.name, host: server.host, ssh_port: server.ssh_port };
}

/**
 * <lcm-join-ssh reconnect-id="42"> – Server per SSH aufnehmen oder (mit `reconnect-id`) neu verbinden:
 * Verbindungsdaten → Host-Schlüssel bestätigen → LCM richtet Dienstbenutzer und Schlüssel ein → Ergebnis.
 * Das Login-Passwort steht nur in diesem Element und verschwindet nach der Aufnahme.
 */
export class LcmJoinSsh extends LitElement {
  static override properties = {
    reconnectId: { type: Number, attribute: 'reconnect-id' },
    _step: { state: true },
    _form: { state: true },
    _tried: { state: true },
    _probe: { state: true },
    _busy: { state: true },
    _error: { state: true },
    _known: { state: true },
    _onboardingKey: { state: true },
    _target: { state: true },
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

  declare reconnectId: number | null;
  declare _step: Step;
  declare _form: SshForm;
  declare _tried: boolean;
  declare _probe: HostKeyProbe | null;
  declare _busy: boolean;
  declare _error: string;
  declare _known: KnownServer[];
  declare _onboardingKey: string;
  declare _target: ServerItem | null;
  declare _result: JoinedServer | null;
  #lookup: ReturnType<typeof setTimeout> | undefined;
  #loadedTarget: number | null = null;

  constructor() {
    super();
    this.reconnectId = null;
    this._step = 'form';
    this._form = emptySshForm();
    this._tried = false;
    this._probe = null;
    this._busy = false;
    this._error = '';
    this._known = [];
    this._onboardingKey = '';
    this._target = null;
    this._result = null;
    new I18nController(this);
  }

  override connectedCallback(): void {
    super.connectedCallback();
    void onboardingPublicKey().then((key) => (this._onboardingKey = key));
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    clearTimeout(this.#lookup);
    unsaved.set(this, false);
    this._form = { ...this._form, password: '' };
  }

  override willUpdate(): void {
    if (this.reconnectId !== this.#loadedTarget) {
      this.#loadedTarget = this.reconnectId;
      if (this.reconnectId !== null) void this.#loadTarget(this.reconnectId);
    }
    const typed = this._form.name !== '' || this._form.host !== '' || this._form.password !== '';
    unsaved.set(this, this._step !== 'done' && typed);
  }

  get #reconnecting(): boolean {
    return this.reconnectId !== null;
  }

  async #loadTarget(id: number): Promise<void> {
    try {
      const server = normaliseServers(await api.get(serverSummaryUrl(id))).items[0];
      if (!server) throw new Error(t('joinWizard.reconnect.notFound'));
      this._target = server;
      this._form = { ...emptySshForm(), host: server.host, port: String(server.ssh_port || 22), restricted: server.restricted_sudo };
    } catch (error) {
      this._error = errorText(error);
    }
  }

  #set(patch: Partial<SshForm>): void {
    this._form = { ...this._form, ...patch };
    if ('host' in patch) this.#scheduleLookup();
  }

  /** Schon angelegte Server mit dieser Adresse suchen (sofortige Warnung; verbindlich prüft das Backend). */
  #scheduleLookup(): void {
    clearTimeout(this.#lookup);
    const host = this._form.host.trim();
    if (!host) {
      this._known = [];
      return;
    }
    this.#lookup = setTimeout(async () => {
      try {
        const page = normaliseServers(await api.get(`/api/v1/servers?q=${encodeURIComponent(host)}&limit=50`));
        if (host === this._form.host.trim()) this._known = page.items.map(known);
      } catch {
        this._known = []; // ohne Liste bleibt die Prüfung im Backend
      }
    }, 300);
  }

  get #duplicate() {
    return duplicateOf(this._known, this._form.host, parsePort(this._form.port), this.reconnectId);
  }

  async #probe(): Promise<void> {
    this._tried = true;
    if (hasProblems(sshProblems(this._form, !this.#reconnecting)) || this.#duplicate.taken || this._busy) return;
    this._busy = true;
    this._error = '';
    try {
      this._probe = await probeHostKey(this._form.host.trim(), parsePort(this._form.port) ?? 22);
      this._step = 'fingerprint';
    } catch (error) {
      this._error = errorText(error);
    } finally {
      this._busy = false;
    }
  }

  async #run(): Promise<void> {
    const probe = this._probe;
    if (!probe || this._busy) return;
    this._busy = true;
    this._error = '';
    this._step = 'running';
    try {
      this._result =
        this.reconnectId !== null
          ? await reconnectServer(this.reconnectId, reconnectBody(this._form, probe.fingerprint))
          : await joinServer(joinBody(this._form, probe.fingerprint));
      this._form = { ...this._form, password: '' };
      this._step = 'done';
    } catch (error) {
      this._error = errorText(error);
      this._step = 'confirm';
    } finally {
      this._busy = false;
    }
  }

  #restart(): void {
    this._form = emptySshForm();
    this._probe = null;
    this._result = null;
    this._tried = false;
    this._error = '';
    this._step = 'form';
  }

  #authOptions(): ChoiceOption[] {
    return [
      { id: 'password', label: t('joinWizard.auth.password'), icon: 'lock' },
      { id: 'key', label: t('joinWizard.auth.systemKey'), icon: 'key' },
    ];
  }

  /** Was LCM auf dem Ziel tut – in der Bestätigung und während es läuft. */
  #actions(): string[] {
    const items = [
      t('joinWizard.ssh.does.login'),
      this.#reconnecting ? t('joinWizard.ssh.does.reuser') : t('joinWizard.ssh.does.user'),
      this._form.restricted ? t('joinWizard.ssh.does.sudoRestricted') : t('joinWizard.ssh.does.sudoFull'),
      t('joinWizard.ssh.does.verify'),
    ];
    items.push(this.#reconnecting ? t('joinWizard.ssh.does.keep') : t('joinWizard.ssh.does.group'));
    return items;
  }

  #keyHint() {
    return html`<lcm-notice>
      <div class="stack tight">
        <span>${t('joinWizard.ssh.keyHint')}</span>
        ${this._onboardingKey
          ? html`<lcm-command
              label=${t('joinWizard.ssh.keyPublic')}
              .value=${this._onboardingKey}
              copy-label=${t('joinWizard.copy')}
              copied-label=${t('joinWizard.copied')}
            ></lcm-command>`
          : html`<span>${t('joinWizard.ssh.keyWhere')}</span>`}
      </div>
    </lcm-notice>`;
  }

  #duplicateNote() {
    const { taken, sameHost } = this.#duplicate;
    if (taken) {
      return html`<lcm-notice tone="crit">${t('joinWizard.ssh.duplicate')} <a href=${`/servers/${taken.id}`}>${taken.name}</a></lcm-notice>`;
    }
    if (sameHost) {
      return html`<lcm-notice>${t('joinWizard.ssh.sameHost', { port: sameHost.ssh_port })} <a href=${`/servers/${sameHost.id}`}>${sameHost.name}</a></lcm-notice>`;
    }
    return nothing;
  }

  #nameField(problems: SshProblems) {
    if (this.#reconnecting) return html`<p class="lead">${t('joinWizard.reconnect.intro', { name: this._target?.name ?? '' })}</p>`;
    return html`<lcm-input
      label=${t('joinWizard.field.name')}
      maxlength=${LIMITS.name}
      placeholder=${EXAMPLES.sshName}
      .value=${live(this._form.name)}
      error=${problemText(problems.name, this._tried)}
      @lcm-input=${(event: CustomEvent<{ value: string }>) => this.#set({ name: event.detail.value })}
    ></lcm-input>`;
  }

  #addressRow(problems: SshProblems) {
    const form = this._form;
    return html`<div class="row">
      <lcm-input
        label=${t('joinWizard.field.host')}
        maxlength=${LIMITS.host}
        placeholder=${EXAMPLES.sshHost}
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
    </div>`;
  }

  #loginUserField(problems: SshProblems) {
    return html`<lcm-input
      label=${t('joinWizard.field.loginUser')}
      maxlength=${LIMITS.loginUser}
      hint=${t('joinWizard.ssh.loginUserHint')}
      mono
      autocomplete="off"
      .value=${live(this._form.loginUser)}
      error=${problemText(problems.loginUser, this._tried)}
      @lcm-input=${(event: CustomEvent<{ value: string }>) => this.#set({ loginUser: event.detail.value })}
    ></lcm-input>`;
  }

  #authFields(problems: SshProblems) {
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
            hint=${t('joinWizard.ssh.passwordHint')}
            .value=${live(form.password)}
            error=${problemText(problems.password, this._tried)}
            @lcm-input=${(event: CustomEvent<{ value: string }>) => this.#set({ password: event.detail.value })}
          ></lcm-input>`
        : this.#keyHint()}`;
  }

  #formStep() {
    const problems = sshProblems(this._form, !this.#reconnecting);
    return html`<div class="stack" @keydown=${(event: KeyboardEvent) => this.#onEnter(event, () => void this.#probe())}>
      ${this.#nameField(problems)} ${this.#addressRow(problems)} ${this.#duplicateNote()} ${this.#loginUserField(problems)}
      <lcm-join-root-guide></lcm-join-root-guide>
      ${this.#authFields(problems)}
      <lcm-switch
        label=${t('joinWizard.ssh.restricted.label')}
        description=${t('joinWizard.ssh.restricted.description')}
        on-text=${t('joinWizard.ssh.restricted.on')}
        off-text=${t('joinWizard.ssh.restricted.off')}
        .checked=${this._form.restricted}
        @lcm-change=${(event: CustomEvent<{ checked: boolean }>) => this.#set({ restricted: event.detail.checked })}
      ></lcm-switch>
      ${this._error ? html`<lcm-notice tone="crit">${this._error}</lcm-notice>` : nothing}
      <div class="actions">
        <lcm-button variant="primary" icon="arrow-right" ?busy=${this._busy} ?disabled=${Boolean(this.#duplicate.taken)} @click=${() => void this.#probe()}
          >${t('joinWizard.probe')}</lcm-button
        >
      </div>
    </div>`;
  }

  #fingerprintStep(probe: HostKeyProbe) {
    return html`<lcm-join-fingerprint
      heading=${t('joinWizard.ssh.fingerprintTitle', { host: this._form.host.trim() })}
      key-type=${probe.key_type}
      fingerprint=${probe.fingerprint}
      verify-label=${t('joinWizard.ssh.fingerprintVerify')}
      verify-command=${`ssh-keygen -lf ${hostKeyFile(probe.key_type)}`}
      hint=${this.#reconnecting ? t('joinWizard.reconnect.fingerprintHint') : t('joinWizard.ssh.fingerprintHint')}
      confirm-label=${t('joinWizard.fingerprintOk')}
      @lcm-back=${() => (this._step = 'form')}
      @lcm-confirm=${() => (this._step = 'confirm')}
    ></lcm-join-fingerprint>`;
  }

  #confirmStep() {
    const reconnecting = this.#reconnecting;
    const host = this._form.host.trim();
    return html`<div class="stack">
      <h2>${t('joinWizard.ssh.confirmTitle', { host })}</h2>
      <ol class="does">
        ${this.#actions().map((item) => html`<li>${item}</li>`)}
      </ol>
      <lcm-notice>${t('joinWizard.ssh.passwordNotStored')}</lcm-notice>
      <p class="lead">${t('joinWizard.ssh.noHardening')}</p>
      ${this._error ? html`<lcm-notice tone="crit">${this._error}</lcm-notice>` : nothing}
      <div class="actions">
        <lcm-button variant="ghost" icon="chevron-left" @click=${() => (this._step = 'fingerprint')}>${t('joinWizard.back')}</lcm-button>
        <lcm-server-action
          variant="primary"
          icon=${reconnecting ? 'refresh' : 'plus'}
          label=${reconnecting ? t('joinWizard.reconnect.run.label') : t('joinWizard.ssh.run.label')}
          help=${reconnecting ? t('joinWizard.reconnect.run.help') : t('joinWizard.ssh.run.help')}
          meaning=${reconnecting ? t('joinWizard.reconnect.run.meaning') : t('joinWizard.ssh.run.meaning')}
          .preview=${{ docsHref: '/docs/serveraenderungen' }}
          .labels=${actionLabels()}
          @click=${() => void this.#run()}
        ></lcm-server-action>
      </div>
    </div>`;
  }

  #doneStep(server: JoinedServer) {
    const system = [server.os_name, server.os_version].filter(Boolean).join(' ');
    return html`<div class="stack">
      <lcm-notice tone="ok">${this.#reconnecting ? t('joinWizard.reconnect.done', { name: server.name }) : t('joinWizard.ssh.done', { name: server.name })}</lcm-notice>
      <dl class="facts">
        <dt>${t('joinWizard.result.host')}</dt>
        <dd class="mono">${server.host}${server.ssh_port ? `:${server.ssh_port}` : ''}</dd>
        ${server.service_user ? html`<dt>${t('joinWizard.result.serviceUser')}</dt><dd class="mono">${server.service_user}</dd>` : nothing}
        <dt>${t('joinWizard.result.rights')}</dt>
        <dd>${server.restricted_sudo ? t('joinWizard.result.restricted') : t('joinWizard.result.full')}</dd>
        ${system ? html`<dt>${t('joinWizard.result.system')}</dt><dd>${system}</dd>` : nothing}
      </dl>
      <p class="lead">${t('joinWizard.ssh.doneJob')} <a href=${`/servers/${server.id}/jobs`}>${t('joinWizard.result.jobs')}</a></p>
      <div class="actions">
        <lcm-button variant="primary" icon="server" href=${`/servers/${server.id}`}>${t('joinWizard.result.toServer')}</lcm-button>
        ${this.#reconnecting ? nothing : html`<lcm-button variant="ghost" icon="plus" @click=${this.#restart}>${t('joinWizard.result.another')}</lcm-button>`}
      </div>
    </div>`;
  }

  #onEnter(event: KeyboardEvent, action: () => void): void {
    const target = event.composedPath()[0];
    if (event.key !== 'Enter' || !(target instanceof HTMLInputElement)) return;
    event.preventDefault();
    action();
  }

  #body() {
    switch (this._step) {
      case 'fingerprint':
        return this._probe ? this.#fingerprintStep(this._probe) : nothing;
      case 'confirm':
        return this.#confirmStep();
      case 'running':
        return html`<lcm-join-progress
          heading=${this.#reconnecting ? t('joinWizard.reconnect.running') : t('joinWizard.ssh.running')}
          .items=${this.#actions()}
        ></lcm-join-progress>`;
      case 'done':
        return this._result ? this.#doneStep(this._result) : nothing;
      default:
        return this.#formStep();
    }
  }

  override render() {
    const steps = [
      t('joinWizard.step.connection'),
      t('joinWizard.step.fingerprint'),
      this.#reconnecting ? t('joinWizard.step.reconnect') : t('joinWizard.step.join'),
      t('joinWizard.step.done'),
    ];
    if (this.#reconnecting && this._target && this._target.transport !== 'ssh') {
      return html`<lcm-notice tone="crit">${t('joinWizard.reconnect.onlySsh')}</lcm-notice>`;
    }
    return html`<div class="panel">
      <lcm-join-steps .steps=${steps} current=${STEP_INDEX[this._step]}></lcm-join-steps>
      ${this.#body()}
    </div>`;
  }
}
customElements.define('lcm-join-ssh', LcmJoinSsh);
