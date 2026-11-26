import { LitElement, css, html, nothing } from 'lit';
import '../../components/lcm-button';
import '../../components/lcm-card';
import '../../components/lcm-cron';
import '../../components/lcm-input';
import '../../components/lcm-notice';
import '../../components/lcm-status';
import '../../components/lcm-switch';
import { baseStyles } from '../../components/shared';
import { toast } from '../../components/lcm-toaster';
import {
  DEFAULT_AGENT_UPDATE_CRON,
  agentUpdateApi,
  agentUpdateFormOf,
  agentUpdateUrl,
  normaliseAgentUpdatePolicy,
  validateAgentUpdateForm,
  type AgentUpdateForm,
  type AgentUpdatePolicy,
} from '../../api/agent-update';
import { auth } from '../../lib/auth';
import { I18nController } from '../../lib/controllers';
import { formatDateTime } from '../../lib/format';
import { errorText, i18n, t } from '../../lib/i18n';
import { store } from '../../lib/store';
import { cronText } from './cron-describe';
import { cronLabels, cronPresets } from './schedule-modal';

/**
 * <lcm-groups-agent-updates group-id="3"> – die Agent-Update-Regel einer Gruppe: aus (Vorgabe) oder automatisch, mit
 * Zeitfenster (Beginn und Länge, wie bei den Zeitplänen) und der Staffelung (erst ein Server, dann der Rest). Ändern
 * darf, wer Regeln verwalten darf; alle anderen sehen den Stand.
 */
export class LcmGroupsAgentUpdates extends LitElement {
  static override properties = {
    groupId: { type: Number, attribute: 'group-id' },
    _policy: { state: true },
    _form: { state: true },
    _busy: { state: true },
    _error: { state: true },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: block;
        grid-column: 1 / -1;
      }
      .fields {
        display: flex;
        flex-direction: column;
        gap: var(--space-4);
      }
      .sub {
        margin: 0;
        color: var(--text-2);
        font-size: var(--fs-s);
      }
      .actions {
        display: flex;
        flex-wrap: wrap;
        gap: var(--space-2);
      }
      dl {
        display: grid;
        grid-template-columns: max-content 1fr;
        gap: var(--space-2) var(--space-5);
        margin: 0;
        font-size: var(--fs-s);
      }
      dt {
        color: var(--text-2);
      }
      dd {
        margin: 0;
      }
    `,
  ];

  declare groupId: number;
  declare _policy: AgentUpdatePolicy | null;
  declare _form: AgentUpdateForm | null;
  declare _busy: boolean;
  declare _error: string;
  #initial: AgentUpdateForm | null = null;
  #stop: (() => void) | undefined;
  #subscribed = 0;

  constructor() {
    super();
    this.groupId = 0;
    this._policy = null;
    this._form = null;
    this._busy = false;
    this._error = '';
    new I18nController(this);
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    this.#stop?.();
    this.#stop = undefined;
    this.#subscribed = 0;
  }

  override willUpdate(): void {
    if (this.groupId > 0 && this.groupId !== this.#subscribed) this.#subscribe();
  }

  #subscribe(): void {
    this.#stop?.();
    this.#subscribed = this.groupId;
    this._policy = null;
    this._form = null;
    const url = agentUpdateUrl(this.groupId);
    const apply = (data: unknown) => this.#apply(normaliseAgentUpdatePolicy(data));
    this.#stop = store.subscribe(url, apply);
    void store.get(url).then(apply, () => {});
  }

  /** Neuer Stand vom Backend: das Formular folgt nur, solange der Anwender nichts geändert hat. */
  #apply(policy: AgentUpdatePolicy): void {
    const dirty = this._form !== null && this.#initial !== null && JSON.stringify(this._form) !== JSON.stringify(this.#initial);
    this._policy = policy;
    this.#initial = agentUpdateFormOf(policy);
    if (!dirty) this._form = this.#initial;
  }

  get #dirty(): boolean {
    return this._form !== null && this.#initial !== null && JSON.stringify(this._form) !== JSON.stringify(this.#initial);
  }

  #set(patch: Partial<AgentUpdateForm>): void {
    if (this._form) this._form = { ...this._form, ...patch };
  }

  async #act(work: () => Promise<unknown>, done: string): Promise<void> {
    this._busy = true;
    this._error = '';
    try {
      await work();
      toast({ tone: 'ok', title: done });
    } catch (error) {
      this._error = errorText(error);
    } finally {
      this._busy = false;
    }
  }

  #save(): void {
    const form = this._form;
    if (!form || Object.keys(validateAgentUpdateForm(form)).length > 0) return;
    void this.#act(async () => {
      this.#apply(await agentUpdateApi.save(this.groupId, form));
      this._form = this.#initial;
    }, t('agentUpdate.policy.saved'));
  }

  #status(policy: AgentUpdatePolicy) {
    if (policy.agent_servers === 0) return html`<p class="sub">${t('agentUpdate.policy.noAgents')}</p>`;
    const level = policy.outdated > 0 ? 'warn' : 'ok';
    const label = policy.outdated > 0 ? t('agentUpdate.policy.outdated', { count: policy.outdated, total: policy.agent_servers, version: policy.server_version }) : t('agentUpdate.policy.upToDate', { count: policy.agent_servers });
    return html`<lcm-status level=${level} label=${label}></lcm-status>`;
  }

  #lastRun(policy: AgentUpdatePolicy) {
    return html`<dl>
      ${policy.mode === 'auto' && policy.next_run ? html`<dt>${t('agentUpdate.policy.nextRun')}</dt><dd>${formatDateTime(policy.next_run)}</dd>` : nothing}
      <dt>${t('agentUpdate.policy.lastRun')}</dt>
      <dd>${policy.last_run_at ? html`${formatDateTime(policy.last_run_at)}<br /><span class="sub">${policy.last_result}</span>` : t('agentUpdate.policy.never')}</dd>
      ${policy.proven_version ? html`<dt>${t('agentUpdate.policy.proven')}</dt><dd>${policy.proven_version}</dd>` : nothing}
    </dl>`;
  }

  /** Wer nur lesen darf, sieht die Einstellung als Text. */
  #summary(policy: AgentUpdatePolicy) {
    if (policy.mode !== 'auto') return html`<p class="sub">${t('agentUpdate.policy.offText')}</p>`;
    return html`<dl>
      <dt>${t('agentUpdate.policy.staged')}</dt>
      <dd>${policy.staged ? t('agentUpdate.policy.yes') : t('agentUpdate.policy.no')}</dd>
      <dt>${t('agentUpdate.policy.windowRead')}</dt>
      <dd>${cronText(policy.cron_expr)}${policy.window_minutes > 0 ? html`, ${t('agentUpdate.policy.windowMinutes', { minutes: policy.window_minutes })}` : nothing}</dd>
    </dl>`;
  }

  override render() {
    const policy = this._policy;
    const form = this._form;
    if (!policy || !form) return nothing;
    const write = auth.can('rules:manage');
    const errors = validateAgentUpdateForm(form);
    const invalid = Object.keys(errors).length > 0;
    return html`<lcm-card heading=${t('agentUpdate.policy.title')} subtitle=${t('agentUpdate.policy.subtitle')}>
      <div class="fields">
        ${this.#status(policy)}
        ${write
          ? html`<lcm-switch
              label=${t('agentUpdate.policy.auto')}
              description=${t('agentUpdate.policy.autoHint')}
              .checked=${form.auto}
              ?disabled=${this._busy}
              @lcm-change=${(e: CustomEvent<{ checked: boolean }>) => this.#set({ auto: e.detail.checked })}
            ></lcm-switch>`
          : this.#summary(policy)}
        ${write && form.auto
          ? html`<lcm-switch
                label=${t('agentUpdate.policy.staged')}
                description=${t('agentUpdate.policy.stagedHint')}
                .checked=${form.staged}
                ?disabled=${this._busy}
                @lcm-change=${(e: CustomEvent<{ checked: boolean }>) => this.#set({ staged: e.detail.checked })}
              ></lcm-switch>
              <lcm-cron
                .value=${form.cron_expr || DEFAULT_AGENT_UPDATE_CRON}
                .presets=${cronPresets()}
                .labels=${cronLabels()}
                .description=${cronText(form.cron_expr)}
                locale=${i18n.language}
                @lcm-change=${(e: CustomEvent<{ value: string }>) => this.#set({ cron_expr: e.detail.value })}
              ></lcm-cron>
              <lcm-input
                label=${t('agentUpdate.policy.window')}
                width="s"
                digits
                maxlength="4"
                mono
                ?disabled=${this._busy}
                .value=${form.window_minutes}
                .error=${errors.window ? t(errors.window) : ''}
                hint=${t('agentUpdate.policy.windowHint')}
                @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#set({ window_minutes: e.detail.value })}
              ></lcm-input>`
          : nothing}
        ${policy.blocked
          ? html`<lcm-notice tone="warn" heading=${t('agentUpdate.policy.blockedTitle')}>
              ${t('agentUpdate.policy.blocked', { version: policy.server_version })}
              ${write
                ? html`<lcm-button slot="action" size="small" variant="outline" ?busy=${this._busy} @click=${() => void this.#act(async () => this.#apply(await agentUpdateApi.resume(this.groupId)), t('agentUpdate.policy.resumed'))}>${t('agentUpdate.policy.resume')}</lcm-button>`
                : nothing}
            </lcm-notice>`
          : nothing}
        ${this._error ? html`<lcm-notice tone="crit">${this._error}</lcm-notice>` : nothing}
        ${write
          ? html`<div class="actions">
              <lcm-button variant="primary" ?busy=${this._busy} ?disabled=${!this.#dirty || invalid} @click=${() => this.#save()}>${t('groups.action.save')}</lcm-button>
              <lcm-button variant="outline" icon="download" ?disabled=${this._busy || policy.running || this.#dirty} @click=${() => void this.#act(() => agentUpdateApi.run(this.groupId), t('agentUpdate.policy.started'))}
                >${t('agentUpdate.policy.runNow')}</lcm-button
              >
            </div>`
          : nothing}
        ${this.#lastRun(policy)}
      </div>
    </lcm-card>`;
  }
}
customElements.define('lcm-groups-agent-updates', LcmGroupsAgentUpdates);
