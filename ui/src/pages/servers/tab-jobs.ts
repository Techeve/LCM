import { css, html, nothing, type TemplateResult } from 'lit';
import '../../components/lcm-button';
import '../../components/lcm-data-table';
import '../../components/lcm-empty-state';
import '../../components/lcm-list-pager';
import '../../components/lcm-select';
import '../../components/lcm-skeleton';
import '../../components/lcm-status';
import '../../components/lcm-switch';
import type { Column, TableLabels } from '../../components/lcm-data-table';
import { baseStyles } from '../../components/shared';
import { ACTIVE_STATUSES, PROBLEM_STATUSES, jobsUrl, normaliseJobs, type JobItem, type JobsPage } from '../../api/jobs';
import { SubscribeController } from '../../lib/controllers';
import { formatDateTime, formatDuration, formatRelative } from '../../lib/format';
import { t } from '../../lib/i18n';
import { LiveData } from '../../lib/live-data';
import { router } from '../../lib/router';
import { kindLabel, statusLabel, statusLevel } from '../jobs/job-labels';
import { tabStyles } from './srv-styles';
import { SrvTab } from './srv-tab';

const PAGE = 25;
const CHOICES = ['', 'problems', 'active'] as const;

/** Jobs dieses Servers: Verlauf mit Statusfilter und Blättern; ein Klick öffnet den Job auf der Jobs-Seite. Live über `server:<id>`. */
export class SrvJobs extends SrvTab {
  static override styles = [
    baseStyles,
    tabStyles,
    css`
      lcm-select {
        flex: 0 1 12rem;
      }
    `,
  ];

  readonly #jobs = new LiveData<JobsPage>(this, {
    url: () => jobsUrl({ serverId: this.server.id, status: this.#statuses(), kind: [], q: '', since: '', until: '', hideHealth: !this.#health, sort: '-started_at', limit: PAGE, offset: (this.#page - 1) * PAGE }),
    topics: () => [`server:${this.server.id}`, 'jobs'],
    transform: normaliseJobs,
  });

  constructor() {
    super();
    new SubscribeController(this, (listener) => router.onChange(listener));
  }

  get #choice(): string {
    const value = router.state.query.get('jstatus') ?? '';
    return (CHOICES as readonly string[]).includes(value) ? value : '';
  }

  get #health(): boolean {
    return router.state.query.get('health') === '1';
  }

  get #page(): number {
    const page = Number.parseInt(router.state.query.get('jpage') ?? '1', 10);
    return Number.isFinite(page) && page > 1 ? page : 1;
  }

  #statuses(): string[] {
    if (this.#choice === 'problems') return [...PROBLEM_STATUSES];
    if (this.#choice === 'active') return [...ACTIVE_STATUSES];
    return [];
  }

  #set(patch: Record<string, string | number | boolean | null>): void {
    router.setQuery({ jpage: null, ...patch });
  }

  get #labels(): TableLabels {
    return {
      caption: t('servers.jobsTab.caption'),
      filter: '',
      all: '',
      columns: '',
      densityComfortable: '',
      densityCompact: '',
      selectAll: '',
      selectRow: '',
      selected: '',
      clear: '',
      empty: t('servers.jobsTab.empty'),
      sortedAsc: '',
      sortedDesc: '',
    };
  }

  get #columns(): Column<JobItem>[] {
    return [
      { key: 'status', label: t('jobs.column.status'), render: (r) => html`<lcm-status level=${statusLevel(r.status)} label=${statusLabel(r.status)}></lcm-status>` },
      { key: 'name', label: t('jobs.column.job'), render: (r) => html`<strong>${kindLabel(r.kind) || r.name}</strong>` },
      { key: 'triggered_by', label: t('jobs.column.triggeredBy') },
      { key: 'started', label: t('jobs.column.started'), render: (r) => this.#started(r) },
      { key: 'duration', label: t('jobs.column.duration'), align: 'end', render: (r) => (r.duration_seconds === null ? '' : formatDuration(r.duration_seconds)) },
    ];
  }

  #started(job: JobItem): TemplateResult | string {
    const when = job.started_at ?? job.created_at;
    return when ? html`<span title=${formatDateTime(when)}>${formatRelative(when)}</span>` : '';
  }

  #renderPager(total: number): TemplateResult | typeof nothing {
    if (total <= PAGE) return nothing;
    const page = this.#page;
    const from = (page - 1) * PAGE + 1;
    const to = Math.min(total, page * PAGE);
    return html`<lcm-list-pager
      page=${page}
      page-size=${PAGE}
      total=${total}
      range-label=${t('jobs.pager.range', { from, to, total })}
      previous-label=${t('jobs.pager.previous')}
      next-label=${t('jobs.pager.next')}
      @lcm-page=${(e: CustomEvent<{ page: number }>) => router.setQuery({ jpage: e.detail.page > 1 ? e.detail.page : null })}
    ></lcm-list-pager>`;
  }

  override render() {
    const { data, error, loading } = this.#jobs;
    if (!data) {
      return error
        ? html`<lcm-empty-state level="3" icon="warning" heading=${t('servers.error')} text=${error}></lcm-empty-state>`
        : html`<lcm-skeleton shape="row" lines="6" label=${t('servers.loading')}></lcm-skeleton>`;
    }
    const options = [
      { value: '', label: t('servers.jobsTab.statusAll') },
      { value: 'problems', label: t('servers.jobsTab.statusProblems') },
      { value: 'active', label: t('servers.jobsTab.statusActive') },
    ];
    return html`<div class="toolbar">
        <lcm-select label=${t('servers.filter.status')} .options=${options} .value=${this.#choice} @lcm-change=${(e: CustomEvent<{ value: string }>) => this.#set({ jstatus: e.detail.value || null })}></lcm-select>
        <lcm-switch label=${t('servers.jobsTab.health')} .checked=${this.#health} @lcm-change=${(e: CustomEvent<{ checked: boolean }>) => this.#set({ health: e.detail.checked ? 1 : null })}></lcm-switch>
      </div>
      <div class=${loading ? 'stale' : ''}>
        <lcm-data-table
          external
          no-toolbar
          density="compact"
          .columns=${this.#columns}
          .rows=${data.items}
          .labels=${this.#labels}
          @lcm-row-activate=${(e: CustomEvent<{ id: string }>) => void router.navigate(`/jobs/${encodeURIComponent(e.detail.id)}`)}
        ></lcm-data-table>
        ${this.#renderPager(data.total)}
      </div>`;
  }
}
customElements.define('lcm-srv-jobs', SrvJobs);
