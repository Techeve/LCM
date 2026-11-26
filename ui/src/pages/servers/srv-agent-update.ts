import { LitElement, css, html, nothing } from 'lit';
import '../../components/lcm-button';
import '../../components/lcm-dialog';
import '../../components/lcm-notice';
import type { ButtonVariant } from '../../components/button-styles';
import type { Impact } from '../../components/lcm-dialog';
import { toast } from '../../components/lcm-toaster';
import { baseStyles } from '../../components/shared';
import { agentPlanNoteKey, fetchAgentUpdatePlan, startAgentUpdate, type AgentUpdatePlan } from '../../api/agent-update';
import { refreshServer } from '../../api/servers';
import { I18nController } from '../../lib/controllers';
import { errorText, t } from '../../lib/i18n';
import { announceJob } from '../../lib/job-toast';

/**
 * `<lcm-srv-agent-update server-id server-name>`: der Knopf „Agent aktualisieren“. Ein Klick liest zuerst vom Agent, was
 * ein Update täte, und zeigt „von → nach“ in der Bestätigung: Die Zielversion bestimmt LCM (die höchste, die die
 * Paketquelle des Servers führt und die nicht über LCM liegt), nicht der Anwender. Gibt es nichts zu tun oder geht es
 * nicht, sagt der Dialog es und bietet nichts zum Bestätigen an.
 */
export class SrvAgentUpdate extends LitElement {
  static override properties = {
    serverId: { type: Number, attribute: 'server-id' },
    serverName: { type: String, attribute: 'server-name' },
    variant: { type: String },
    size: { type: String },
    label: { type: String },
    disabled: { type: Boolean },
    _plan: { state: true },
    _loading: { state: true },
    _open: { state: true },
    _busy: { state: true },
    _error: { state: true },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: inline-flex;
      }
      ul {
        margin: var(--space-3) 0 0;
        padding-inline-start: 1.1rem;
        color: var(--text-2);
        font-size: var(--fs-s);
      }
      .warn {
        margin-block-start: var(--space-3);
      }
    `,
  ];

  declare serverId: number;
  declare serverName: string;
  declare variant: ButtonVariant;
  declare size: 'm' | 'small';
  declare label: string;
  declare disabled: boolean;
  declare _plan: AgentUpdatePlan | null;
  declare _loading: boolean;
  declare _open: boolean;
  declare _busy: boolean;
  declare _error: string;

  constructor() {
    super();
    this.serverId = 0;
    this.serverName = '';
    this.variant = 'outline';
    this.size = 'small';
    this.label = '';
    this.disabled = false;
    this._plan = null;
    this._loading = false;
    this._open = false;
    this._busy = false;
    this._error = '';
    new I18nController(this);
  }

  async #begin(): Promise<void> {
    this._loading = true;
    try {
      this._plan = await fetchAgentUpdatePlan(this.serverId);
      this._error = '';
      this._open = true;
    } catch (error) {
      toast({ tone: 'crit', title: t('agentUpdate.planFailed', { name: this.serverName }), message: errorText(error) });
    } finally {
      this._loading = false;
    }
  }

  async #confirm(): Promise<void> {
    // Bestätigen gibt es nur, wenn ein Update ansteht - auch dann, wenn jemand das Ereignis von außen schickt.
    if (this._plan?.action !== 'update') return;
    this._busy = true;
    this._error = '';
    try {
      const job = await startAgentUpdate(this.serverId);
      this._open = false;
      announceJob(t('agentUpdate.label'), this.serverName, job.job_id);
      refreshServer(this.serverId);
      this.dispatchEvent(new CustomEvent('lcm-started', { detail: job, bubbles: true, composed: true }));
    } catch (error) {
      this._error = errorText(error);
    } finally {
      this._busy = false;
    }
  }

  #impacts(plan: AgentUpdatePlan): Impact[] {
    const impacts: Impact[] = [{ label: t('servers.confirm.server'), value: this.serverName }];
    if (plan.action === 'update') {
      impacts.push(
        { label: t('agentUpdate.dialog.agent'), value: t('agentUpdate.dialog.fromTo', { from: plan.installed || '–', to: plan.target }) },
        { label: t('agentUpdate.dialog.lcm'), value: plan.server_version },
        { label: t('agentUpdate.dialog.effect'), value: t('agentUpdate.dialog.effectValue'), tone: 'warn' },
      );
    } else if (plan.installed) {
      impacts.push({ label: t('agentUpdate.dialog.agent'), value: plan.installed });
    }
    return impacts;
  }

  #render(plan: AgentUpdatePlan) {
    const update = plan.action === 'update';
    return html`<lcm-dialog
      ?open=${this._open}
      heading=${t('agentUpdate.dialog.heading', { name: this.serverName })}
      message=${t(agentPlanNoteKey(plan), { from: plan.installed, to: plan.target, server: plan.server_version })}
      .impacts=${this.#impacts(plan)}
      confirm-label=${t('agentUpdate.dialog.confirm')}
      cancel-label=${update ? t('servers.confirm.cancel') : t('agentUpdate.dialog.close')}
      ?confirm-disabled=${!update}
      error=${this._error}
      ?busy=${this._busy}
      @lcm-cancel=${() => (this._open = false)}
      @lcm-confirm=${() => void this.#confirm()}
    >
      ${plan.warnings.includes('enterprise_pin') ? html`<lcm-notice class="warn" tone="warn">${t('agentUpdate.warn.enterprisePin')}</lcm-notice>` : nothing}
    </lcm-dialog>`;
  }

  override render() {
    return html`<lcm-button size=${this.size} variant=${this.variant} icon="download" ?disabled=${this.disabled} ?busy=${this._loading} @click=${() => void this.#begin()}
        >${this.label || t('agentUpdate.label')}</lcm-button
      >
      ${this._plan ? this.#render(this._plan) : nothing}`;
  }
}
customElements.define('lcm-srv-agent-update', SrvAgentUpdate);
