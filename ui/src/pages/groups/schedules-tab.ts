import { LitElement, css, html, nothing } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-button';
import '../../components/lcm-card';
import '../../components/lcm-dialog';
import '../../components/lcm-empty-state';
import '../../components/lcm-server-action';
import '../../components/lcm-switch';
import type { Impact } from '../../components/lcm-dialog';
import { baseStyles, emit } from '../../components/shared';
import { toast } from '../../components/lcm-toaster';
import { groupsApi, inOrder, type Group, type Schedule } from '../../api/groups';
import { I18nController } from '../../lib/controllers';
import { errorText, t } from '../../lib/i18n';
import { jobsLink } from '../../lib/job-link';
import { cronText } from './cron-describe';
import { actionLabels, activeRules, runImpacts, runPreview } from './run-support';
import { groupPath, type Modal } from './url-state';

type Confirm = { kind: 'run' | 'delete'; schedule: Schedule };

/**
 * <lcm-groups-schedules .group=${…} .schedules=${…} ?can-manage> – die Zeitpläne einer Gruppe: Ausführen,
 * Ein-/Ausschalten, Bearbeiten, Löschen. Meldet `lcm-modal-request`.
 */
export class LcmGroupsSchedules extends LitElement {
  static override properties = {
    _dialogError: { state: true },
    group: { attribute: false },
    schedules: { attribute: false },
    canManage: { type: Boolean, attribute: 'can-manage' },
    _confirm: { state: true },
    _busy: { state: true },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: flex;
        flex-direction: column;
        gap: var(--gap);
        min-inline-size: 0;
      }
      .tools {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: var(--space-2);
      }
      .when {
        margin: 0;
        font-weight: var(--fw-medium);
      }
      .cron {
        margin-inline-start: var(--space-2);
        color: var(--text-2);
        font-family: var(--font-mono);
        font-size: var(--fs-s);
      }
      .meta {
        margin: var(--space-1) 0 0;
        color: var(--text-2);
        font-size: var(--fs-s);
      }
      ol {
        margin: var(--space-3) 0 0;
        padding-inline-start: 1.4rem;
        color: var(--text-1);
        font-size: var(--fs-s);
      }
      a {
        color: var(--accent-text);
        font-weight: var(--fw-semibold);
      }
      .bar {
        display: flex;
        justify-content: flex-end;
      }
    `,
  ];

  declare group: Group;
  declare schedules: Schedule[];
  declare canManage: boolean;
  /** Fehler der bestätigten Aktion; steht im Dialog über dem Fuß. */
  declare _dialogError: string;
  declare _confirm: Confirm | null;
  declare _busy: boolean;

  constructor() {
    super();
    this._dialogError = '';
    this.group = undefined as unknown as Group;
    this.schedules = [];
    this.canManage = false;
    this._confirm = null;
    this._busy = false;
    new I18nController(this);
  }

  #request(modal: Modal): void {
    emit(this, 'lcm-modal-request', modal);
  }

  async #toggle(schedule: Schedule, enabled: boolean): Promise<void> {
    try {
      await groupsApi.setScheduleEnabled(schedule.id, enabled);
      toast({ tone: 'ok', title: t(enabled ? 'schedules.notice.enabled' : 'schedules.notice.disabled', { name: schedule.name }) });
    } catch (error) {
      toast({ tone: 'crit', title: t('schedules.notice.failed'), message: errorText(error) });
    }
  }

  async #confirmed(): Promise<void> {
    const confirm = this._confirm;
    if (!confirm) return;
    this._busy = true;
    this._dialogError = '';
    try {
      if (confirm.kind === 'delete') {
        await groupsApi.removeSchedule(confirm.schedule.id);
        toast({ tone: 'ok', title: t('schedules.notice.deleted', { name: confirm.schedule.name }) });
      } else {
        await groupsApi.triggerSchedule(confirm.schedule.id);
        toast({ tone: 'info', title: t('schedules.notice.started', { name: confirm.schedule.name }), ...jobsLink(t('groups.run.showJobs')) });
      }
      this._confirm = null;
    } catch (error) {
      this._dialogError = errorText(error);
    } finally {
      this._busy = false;
    }
  }

  #dialog() {
    const confirm = this._confirm;
    const schedule = confirm?.schedule;
    const running = confirm?.kind === 'run';
    let impacts: Impact[] = [];
    if (schedule) {
      impacts = running
        ? runImpacts(schedule.rules, this.group.servers)
        : [
            { label: t('schedules.delete.rules'), value: t('schedules.delete.rulesValue', { count: schedule.rules.length }), tone: schedule.rules.length > 0 ? 'warn' : undefined },
            { label: t('schedules.delete.effect'), value: t('schedules.delete.effectValue') },
            { label: t('groups.run.reversible'), value: t('groups.run.no'), tone: 'crit' },
          ];
    }
    return html`<lcm-dialog
      ?open=${confirm !== null}
      heading=${schedule ? t(running ? 'schedules.run.title' : 'schedules.delete.title', { name: schedule.name }) : ''}
      message=${t(running ? 'schedules.run.message' : 'schedules.delete.message')}
      .impacts=${impacts}
      confirm-label=${running ? t('groups.run.start') : t('groups.action.delete')}
      cancel-label=${t('groups.action.cancel')}
      tone=${running ? 'default' : 'danger'}
      ?busy=${this._busy}
      error=${this._dialogError}
      @lcm-confirm=${this.#confirmed}
      @lcm-cancel=${() => (this._confirm = null)}
    ></lcm-dialog>`;
  }

  #card(schedule: Schedule) {
    const rules = inOrder(this.group.rules.filter((rule) => rule.schedule_id === schedule.id));
    const description = cronText(schedule.cron_expr);
    const runnable = this.group.servers.length > 0 && activeRules(rules).length > 0;
    return html`<lcm-card heading=${schedule.name}>
      ${schedule.is_system ? html`<lcm-badge slot="actions">${t('groups.system')}</lcm-badge>` : nothing}
      ${schedule.enabled ? nothing : html`<lcm-badge slot="actions" tone="warn">${t('schedules.off')}</lcm-badge>`}
      <p class="when">${description || schedule.cron_expr}<span class="cron">${description ? schedule.cron_expr : ''}</span></p>
      ${schedule.spread_minutes > 0 ? html`<p class="meta">${t('schedules.spread', { minutes: schedule.spread_minutes })}</p>` : nothing}
      ${rules.length > 0
        ? html`<ol>${rules.map((rule) => html`<li>${rule.name}</li>`)}</ol>
            <p class="meta"><a href=${groupPath(this.group.id, 'rules')}>${t('schedules.toRules')}</a></p>`
        : html`<p class="meta">${t('schedules.noRules')} <a href=${groupPath(this.group.id, 'rules')}>${t('schedules.toRules')}</a></p>`}
      ${this.canManage
        ? html`<div class="tools" slot="footer">
            <lcm-server-action
              label=${t('schedules.run.label')}
              icon="play"
              variant="primary"
              ?disabled=${!runnable}
              help=${t('schedules.run.help')}
              meaning=${t('schedules.run.meaning')}
              .labels=${actionLabels()}
              .preview=${runPreview(rules, this.group.servers)}
              @click=${() => (this._confirm = { kind: 'run', schedule })}
            ></lcm-server-action>
            <lcm-switch
              label=${t('schedules.active')}
              .checked=${schedule.enabled}
              on-label=${t('schedules.onLabel')}
              off-label=${t('schedules.offLabel')}
              on-text=${t('schedules.onText')}
              off-text=${t('schedules.offText')}
              @lcm-change=${(e: CustomEvent<{ checked: boolean }>) => this.#toggle(schedule, e.detail.checked)}
            ></lcm-switch>
            ${schedule.is_system
              ? nothing
              : html`<lcm-button variant="ghost" icon="edit" @click=${() => this.#request({ kind: 'edit-schedule', id: schedule.id })}>${t('groups.action.edit')}</lcm-button>
                  <lcm-button variant="ghost" icon="trash" @click=${() => (this._confirm = { kind: 'delete', schedule })}>${t('groups.action.delete')}</lcm-button>`}
          </div>`
        : nothing}
    </lcm-card>`;
  }

  override render() {
    if (!this.group) return nothing;
    return html`
      ${this.canManage
        ? html`<div class="bar"><lcm-button variant="primary" icon="plus" @click=${() => this.#request({ kind: 'new-schedule' })}>${t('schedules.add')}</lcm-button></div>`
        : nothing}
      ${this.schedules.length === 0
        ? html`<lcm-empty-state level="3" icon="calendar" heading=${t('schedules.emptyTitle')} text=${t('schedules.emptyText')}></lcm-empty-state>`
        : this.schedules.map((schedule) => this.#card(schedule))}
      ${this.#dialog()}
    `;
  }
}
customElements.define('lcm-groups-schedules', LcmGroupsSchedules);
