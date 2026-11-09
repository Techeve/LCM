import { LitElement, html, nothing } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-card';
import '../../components/lcm-data-table';
import '../../components/lcm-dialog';
import '../../components/lcm-empty-state';
import '../../components/lcm-explainer';
import '../../components/lcm-server-action';
import '../../components/lcm-skeleton';
import type { Column, Row } from '../../components/lcm-data-table';
import { toast } from '../../components/lcm-toaster';
import { baseStyles } from '../../components/shared';
import { SCHEDULES_OVERVIEW_URL, SERVER_GROUPS_URL, normaliseGroupOptions, normaliseSchedules, settingsMoreApi, type GroupOption, type ScheduleEntry } from '../../api/settings-more';
import { auth } from '../../lib/auth';
import { I18nController } from '../../lib/controllers';
import { formatDateTime, formatRelative } from '../../lib/format';
import { errorText, t } from '../../lib/i18n';
import { LiveData } from '../../lib/live-data';
import { jobsLink } from '../../lib/job-link';
import { isGroupSchedule, scheduleName, triggerHelp, triggerImpacts, triggerPreview, whenText } from './automation/schedule-model';
import './automation/schedule-rules';
import './packages/cells';
import { actionLabels, explainerToggle, sectionStyles, tableLabels } from './packages/kit';

type ScheduleRow = ScheduleEntry & Row & { group: string };

/** Einstellungen → Zeitpläne: alle Zeitpläne der Gruppen und des Systems, von Hand auslösbar. */
export class SettingsSchedules extends LitElement {
  static override properties = { _dialogError: { state: true }, _confirm: { state: true }, _busy: { state: true } };
  static override styles = [baseStyles, sectionStyles];

  /** Fehler der bestätigten Aktion; steht im Dialog über dem Fuß. */
  declare _dialogError: string;
  declare _confirm: ScheduleRow | null;
  declare _busy: boolean;
  // Die Übersicht ist systemweit und damit Administratoren vorbehalten (settings:manage).
  readonly #schedules = new LiveData<ScheduleEntry[]>(this, {
    url: () => (auth.can('settings:manage') ? SCHEDULES_OVERVIEW_URL : null),
    transform: normaliseSchedules,
  });
  readonly #groups = new LiveData<GroupOption[]>(this, {
    url: () => (auth.can('groups:read') ? SERVER_GROUPS_URL : null),
    transform: normaliseGroupOptions,
  });

  constructor() {
    super();
    this._dialogError = '';
    this._confirm = null;
    this._busy = false;
    new I18nController(this);
  }

  #groupName(id: number | null): string {
    if (!id) return '';
    return this.#groups.data?.find((group) => group.id === id)?.name ?? `#${id}`;
  }

  async #trigger(): Promise<void> {
    const entry = this._confirm;
    if (!entry) return;
    this._busy = true;
    this._dialogError = '';
    try {
      await settingsMoreApi.triggerSchedule(entry);
      // Bewusst „ausgelöst“: Der Lauf beginnt erst, sein Ergebnis steht in den Jobs.
      toast({
        tone: 'ok',
        title: t('settings.schedules.notice.triggered', { name: scheduleName(entry) }),
        ...jobsLink(t('settings.schedules.notice.jobs')),
      });
      this._confirm = null;
    } catch (error) {
      this._dialogError = errorText(error);
    } finally {
      this._busy = false;
    }
  }

  get #columns(): Column[] {
    const row = (r: Row) => r as ScheduleRow;
    return [
      {
        key: 'name',
        label: t('settings.schedules.column.name'),
        sortable: true,
        value: (r) => scheduleName(row(r)),
        render: (r) =>
          html`<lcm-settings-cell sub=${isGroupSchedule(row(r)) ? t('settings.schedules.groupOf', { group: row(r).group }) : t('settings.schedules.origin.system')}
            ><strong>${scheduleName(row(r))}</strong>${row(r).enabled ? nothing : html`<lcm-badge tone="warn">${t('settings.schedules.off')}</lcm-badge>`}</lcm-settings-cell
          >`,
      },
      { key: 'rules', label: t('schedules.rules'), render: (r) => html`<lcm-schedule-rules .entry=${row(r)}></lcm-schedule-rules>` },
      {
        // Rhythmus und nächster Lauf in einer Spalte, damit „Regeln“ neben der Erklärspalte Platz hat.
        key: 'next',
        label: t('settings.schedules.column.when'),
        sortable: true,
        value: (r) => row(r).next_run || '9999',
        render: (r) => html`<lcm-settings-cell
          title=${[row(r).cron_expr, row(r).next_run ? formatDateTime(row(r).next_run) : ''].filter(Boolean).join(' · ')}
          sub=${row(r).next_run ? formatRelative(row(r).next_run) : ''}
          >${whenText(row(r).cron_expr)}</lcm-settings-cell
        >`,
      },
      {
        key: 'actions',
        label: t('app.actions'),
        labelHidden: true,
        align: 'end',
        render: (r) => html`<lcm-server-action
          label=${t('settings.schedules.trigger.label')}
          help=${triggerHelp(row(r))}
          variant="ghost"
          icon="play"
          .labels=${actionLabels()}
          .preview=${triggerPreview(row(r))}
          @click=${() => (this._confirm = row(r))}
        ></lcm-server-action>`,
      },
    ];
  }

  #table() {
    if (!auth.can('settings:manage')) {
      return html`<lcm-empty-state level="3" icon="lock" heading=${t('settings.schedules.adminOnly.title')} text=${t('settings.schedules.adminOnly.text')}></lcm-empty-state>`;
    }
    const schedules = this.#schedules.data;
    if (!schedules) return this.#schedules.error ? html`<p class="callout warn">${this.#schedules.error}</p>` : html`<lcm-skeleton lines="6"></lcm-skeleton>`;
    if (schedules.length === 0) {
      return html`<lcm-empty-state level="3" icon="calendar" heading=${t('settings.schedules.empty.title')} text=${t('settings.schedules.empty.text')}></lcm-empty-state>`;
    }
    const rows = schedules.map((s) => ({ ...s, id: `${s.kind}:${s.schedule_id ?? 0}`, group: this.#groupName(s.group_id) })) as ScheduleRow[];
    return html`<lcm-data-table
      no-toolbar
      density="compact"
      .sort=${{ key: 'next', dir: 'asc' }}
      .columns=${this.#columns}
      .rows=${rows}
      .labels=${tableLabels(t('settings.schedules.caption'), t('settings.schedules.empty.title'))}
    ></lcm-data-table>`;
  }

  #dialog() {
    const entry = this._confirm;
    return html`<lcm-dialog
      ?open=${entry !== null}
      heading=${entry ? t('settings.schedules.confirm.title', { name: scheduleName(entry) }) : ''}
      message=${entry ? triggerHelp(entry) : ''}
      .impacts=${entry ? triggerImpacts(entry, entry.group) : []}
      confirm-label=${t('settings.schedules.trigger.label')}
      cancel-label=${t('settings.more.modal.cancel')}
      error=${this._dialogError}
      ?busy=${this._busy}
      @lcm-confirm=${() => void this.#trigger()}
      @lcm-cancel=${() => (this._confirm = null)}
    ></lcm-dialog>`;
  }

  #explainer() {
    const toggle = explainerToggle();
    return html`<lcm-explainer topic="settings-schedules" heading=${t('settings.schedules.explainer.title')} collapse-label=${toggle.collapse} expand-label=${toggle.expand}>
      <p>${t('settings.schedules.explainer.intro')}</p>
      <ul>
        <li><strong>${t('settings.schedules.origin.group')}:</strong> ${t('settings.schedules.explainer.group')}</li>
        <li><strong>${t('settings.schedules.origin.system')}:</strong> ${t('settings.schedules.explainer.system')}</li>
      </ul>
      <p>${t('settings.schedules.explainer.manual')}</p>
    </lcm-explainer>`;
  }

  override render() {
    return html`<div class="layout explain">
        <div class="main">
          <lcm-card level="2" heading=${t('settings.schedules.title')}>${this.#table()}</lcm-card>
        </div>
        ${this.#explainer()}
      </div>
      ${this.#dialog()}`;
  }
}
customElements.define('lcm-settings-schedules', SettingsSchedules);
