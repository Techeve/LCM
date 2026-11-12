import { LitElement, css, html, nothing } from 'lit';
import { live } from 'lit/directives/live.js';
import '../../../components/lcm-button';
import '../../../components/lcm-command';
import '../../../components/lcm-input';
import '../../../components/lcm-notice';
import { baseStyles, emit } from '../../../components/shared';
import { agentListenerPort, createAgentServer, type AgentResult } from '../../../api/provisioning';
import { I18nController } from '../../../lib/controllers';
import { errorText, t } from '../../../lib/i18n';
import { LIMITS } from '../../../lib/validate';
import { unsaved } from '../../../lib/unsaved';
import { formStyles } from '../../account/form-styles';
import { agentCommands, EXAMPLES, lineProblem } from './join-model';
import { problemText } from './join-parts';
import './join-parts';
import { joinStyles } from './join-styles';

/**
 * <lcm-join-agent> – Agent-Server anlegen: Name vergeben, LCM erzeugt das Enrollment-Token (genau einmal sichtbar)
 * und die fertigen Befehle. Solange das Token auf der Seite steht, lädt keine neue Version die Seite neu.
 */
export class LcmJoinAgent extends LitElement {
  static override properties = {
    _name: { state: true },
    _tried: { state: true },
    _busy: { state: true },
    _error: { state: true },
    _result: { state: true },
    _port: { state: true },
  };
  static override styles = [
    baseStyles,
    formStyles,
    joinStyles,
    css`
      :host {
        display: block;
      }
      .commands {
        display: grid;
        gap: var(--space-4);
      }
      .alt {
        display: grid;
        gap: var(--space-2);
        padding-block-start: var(--space-4);
        border-block-start: 1px solid var(--line);
      }
    `,
  ];

  declare _name: string;
  declare _tried: boolean;
  declare _busy: boolean;
  declare _error: string;
  declare _result: AgentResult | null;
  declare _port: number;

  constructor() {
    super();
    this._name = '';
    this._tried = false;
    this._busy = false;
    this._error = '';
    this._result = null;
    this._port = 0;
    new I18nController(this);
  }

  override connectedCallback(): void {
    super.connectedCallback();
    void agentListenerPort().then((port) => (this._port = port));
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    unsaved.set(this, false);
    emit(this, 'lcm-join-lock', { locked: false });
    this._result = null; // das Token bleibt nicht im Speicher liegen
  }

  override willUpdate(changed: Map<string, unknown>): void {
    unsaved.set(this, this._result !== null);
    // Mit dem Token auf der Seite sperrt die Seite den Wechsel der Verbindungsart (das Token käme nicht wieder).
    if (changed.has('_result')) emit(this, 'lcm-join-lock', { locked: this._result !== null });
  }

  async #create(): Promise<void> {
    this._tried = true;
    if (lineProblem(this._name, LIMITS.name) || this._busy) return;
    this._busy = true;
    this._error = '';
    try {
      this._result = await createAgentServer(this._name.trim());
    } catch (error) {
      this._error = errorText(error);
    } finally {
      this._busy = false;
    }
  }

  #command(label: string, value: string, hint = '') {
    return html`<div class="stack tight">
      <lcm-command label=${label} .value=${value} copy-label=${t('joinWizard.copy')} copied-label=${t('joinWizard.copied')}></lcm-command>
      ${hint ? html`<span class="lead small">${hint}</span>` : nothing}
    </div>`;
  }

  #form() {
    return html`<div
      class="stack"
      @keydown=${(event: KeyboardEvent) => {
        if (event.key === 'Enter' && event.composedPath()[0] instanceof HTMLInputElement) void this.#create();
      }}
    >
      <lcm-input
        label=${t('joinWizard.field.name')}
        maxlength=${LIMITS.name}
        hint=${t('joinWizard.agent.nameHint')}
        placeholder=${EXAMPLES.agentName}
        .value=${live(this._name)}
        error=${problemText(lineProblem(this._name, LIMITS.name), this._tried)}
        @lcm-input=${(event: CustomEvent<{ value: string }>) => (this._name = event.detail.value)}
      ></lcm-input>
      ${this._error ? html`<lcm-notice tone="crit">${this._error}</lcm-notice>` : nothing}
      <div class="actions">
        <lcm-button variant="primary" icon="key" ?busy=${this._busy} @click=${() => void this.#create()}>${t('joinWizard.agent.create')}</lcm-button>
      </div>
    </div>`;
  }

  #ready(result: AgentResult) {
    const commands = agentCommands(location, this._port, result.token, result.download_pin);
    return html`<div class="stack">
      <h2>${t('joinWizard.agent.readyTitle', { name: result.server.name })}</h2>
      <lcm-notice tone="warn">${t('joinWizard.agent.once')}</lcm-notice>
      <div class="commands">
        ${this.#command(t('joinWizard.agent.repo'), commands.repo, t('joinWizard.agent.repoHint'))}
        ${this.#command(t('joinWizard.agent.install'), commands.install)}
        ${this.#command(
          t('joinWizard.agent.enroll'),
          commands.enroll,
          this._port > 0 ? t('joinWizard.agent.portHint', { port: this._port }) : t('joinWizard.agent.enrollHint'),
        )}
      </div>
      <div class="alt">
        <h3>${t('joinWizard.agent.manual')}</h3>
        ${this.#command(t('joinWizard.agent.manualLabel'), commands.manual, t('joinWizard.agent.manualHint'))}
      </div>
      <p class="lead">${t('joinWizard.agent.after')}</p>
      <div class="actions">
        <lcm-button variant="primary" icon="server" href=${`/servers/${result.server.id}`}>${t('joinWizard.result.toServer')}</lcm-button>
      </div>
    </div>`;
  }

  override render() {
    const steps = [t('joinWizard.step.name'), t('joinWizard.step.install')];
    return html`<div class="panel">
      <lcm-join-steps .steps=${steps} current=${this._result ? 1 : 0}></lcm-join-steps>
      ${this._result ? this.#ready(this._result) : this.#form()}
    </div>`;
  }
}
customElements.define('lcm-join-agent', LcmJoinAgent);
