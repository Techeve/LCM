import { LitElement, css, html, nothing, type TemplateResult } from 'lit';
import '../../components/lcm-button';
import '../../components/lcm-card';
import '../../components/lcm-data-table';
import '../../components/lcm-empty-state';
import '../../components/lcm-list-pager';
import '../../components/lcm-skeleton';
import '../../components/lcm-status';
import './jobs-abort';
import './jobs-active';
import './jobs-detail';
import './jobs-filters';
import {
  JOB_FILTER_OPTIONS_URL,
  PAGE_SIZE,
  SERVERS_URL,
  jobsUrl,
  normaliseFilterOptions,
  normaliseJobs,
  normaliseServers,
  type JobFilterOptions,
  type JobItem,
  type JobsPage,
  type ServerRef,
} from '../../api/jobs';
import type { Column, SortState, TableLabels } from '../../components/lcm-data-table';
import { baseStyles, utilStyles } from '../../components/shared';
import { auth } from '../../lib/auth';
import { I18nController, SubscribeController } from '../../lib/controllers';
import { formatDateTime, formatDuration, formatRelative } from '../../lib/format';
import { t } from '../../lib/i18n';
import { LiveData } from '../../lib/live-data';
import { FadeOnChange } from '../../lib/motion';
import { palette } from '../../lib/palette';
import { router } from '../../lib/router';
import { kindLabel, statusLabel, statusLevel } from './job-labels';
import {
  filtersFromQuery,
  hasFilters,
  queryPatch,
  resetPatch,
  sortFromTable,
  sortToTable,
  toActiveQuery,
  toJobsQuery,
  type JobFilters,
} from './jobs-state';

/** Jobs: laufende und wartende oben, darunter der Verlauf mit Filtern; ein Job öffnet sich als Fenster (`/jobs/:id`). */
export class JobsPageElement extends LitElement {
  static override properties = { _abort: { state: true } };
  static override styles = [
    baseStyles,
    utilStyles,
    css`
      :host {
        display: block;
      }
      h1 {
        margin: 0 0 var(--space-5);
        font-size: var(--fs-2xl);
        font-weight: var(--head-weight);
        letter-spacing: var(--head-tracking);
        line-height: 1.15;
      }
      .stack {
        display: flex;
        flex-direction: column;
        gap: var(--gap);
      }
      .name {
        font-weight: var(--fw-semibold);
      }
      .num {
        font-variant-numeric: tabular-nums;
      }
    `,
  ];

  /** Fehler der bestätigten Aktion; steht im Dialog über dem Fuß. */
  declare _abort: JobItem | undefined;

  #queryKey = '';
  #filters: JobFilters = filtersFromQuery(new URLSearchParams());

  readonly #history = new LiveData<JobsPage>(this, { url: () => jobsUrl(toJobsQuery(this.#current())), topics: () => ['jobs'], transform: normaliseJobs });
  readonly #active = new LiveData<JobsPage>(this, { url: () => jobsUrl(toActiveQuery(this.#current())), transform: normaliseJobs });
  readonly #servers = new LiveData<ServerRef[]>(this, { url: () => SERVERS_URL, transform: normaliseServers });
  readonly #options = new LiveData<JobFilterOptions>(this, { url: () => JOB_FILTER_OPTIONS_URL, transform: normaliseFilterOptions });

  constructor() {
    super();
    this._abort = undefined;
    new I18nController(this);
    new SubscribeController(this, (listener) => router.onChange(listener));
    new SubscribeController(this, (listener) => auth.onChange(listener));
    new FadeOnChange(this, () => this.#history.data !== undefined, () => this.renderRoot.querySelector('.history'));
  }

  override connectedCallback(): void {
    super.connectedCallback();
    palette.register(this, [
      { id: 'jobs.running', label: t('jobs.palette.running'), icon: 'play', keywords: ['running', 'laufend'], run: () => this.#setFilters({ status: 'running,pending' }) },
      { id: 'jobs.problems', label: t('jobs.palette.problems'), icon: 'warning', keywords: ['failed', 'fehler'], run: () => this.#setFilters({ status: 'problems' }) },
    ]);
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    palette.unregister(this);
  }

  /** Die Filter aus der Adresse; dasselbe Objekt, solange die Query gleich bleibt (die Eingabefelder behalten sonst ihren Text nicht). */
  #current(): JobFilters {
    const key = router.state.query.toString();
    if (key !== this.#queryKey) {
      this.#queryKey = key;
      this.#filters = filtersFromQuery(router.state.query);
    }
    return this.#filters;
  }

  #setFilters(change: Partial<JobFilters>): void {
    router.setQuery(queryPatch(change));
  }

  get #selectedId(): string {
    return router.state.match?.params.id ?? '';
  }

  #open(id: string): void {
    void router.navigate(`/jobs/${encodeURIComponent(id)}${location.search}`);
  }

  #close(): void {
    if (this.#selectedId) void router.navigate(`/jobs${location.search}`, { replace: true });
  }

  get #labels(): TableLabels {
    return {
      caption: t('jobs.table.caption'),
      filter: '',
      all: '',
      columns: '',
      densityComfortable: '',
      densityCompact: '',
      selectAll: '',
      selectRow: '',
      selected: '',
      clear: '',
      empty: t('jobs.table.empty'),
      sortedAsc: t('jobs.table.sortedAsc'),
      sortedDesc: t('jobs.table.sortedDesc'),
    };
  }

  get #columns(): Column<JobItem>[] {
    return [
      { key: 'status', label: t('jobs.column.status'), render: (r) => this.#statusCell(r) },
      { key: 'name', label: t('jobs.column.job'), render: (r) => html`<span class="name">${r.name || kindLabel(r.kind)}</span>` },
      { key: 'server', label: t('jobs.column.server'), render: (r) => r.server_name },
      { key: 'kind', label: t('jobs.column.kind'), render: (r) => kindLabel(r.kind) },
      { key: 'triggered_by', label: t('jobs.column.triggeredBy') },
      { key: 'started', label: t('jobs.column.started'), sortable: true, render: (r) => this.#startedCell(r) },
      { key: 'duration', label: t('jobs.column.duration'), sortable: true, align: 'end', render: (r) => this.#durationCell(r) },
    ];
  }

  #statusCell(job: JobItem): TemplateResult {
    return html`<lcm-status level=${statusLevel(job.status)} label=${statusLabel(job.status)}></lcm-status>`;
  }

  #startedCell(job: JobItem): TemplateResult | string {
    const when = job.started_at ?? job.created_at;
    return when ? html`<span title=${formatDateTime(when)}>${formatRelative(when)}</span>` : '';
  }

  #durationCell(job: JobItem): TemplateResult | string {
    return job.duration_seconds === null ? '' : html`<span class="num">${formatDuration(job.duration_seconds)}</span>`;
  }

  #onSort(event: CustomEvent<SortState | null>): void {
    this.#setFilters({ sort: sortFromTable(event.detail) });
  }

  #renderPager(total: number, filters: JobFilters): TemplateResult | typeof nothing {
    const from = (filters.page - 1) * PAGE_SIZE + 1;
    const to = Math.min(total, filters.page * PAGE_SIZE);
    return html`<lcm-list-pager
      page=${filters.page}
      page-size=${PAGE_SIZE}
      total=${total}
      range-label=${t('jobs.pager.range', { from, to, total })}
      previous-label=${t('jobs.pager.previous')}
      next-label=${t('jobs.pager.next')}
      @lcm-page=${(e: CustomEvent<{ page: number }>) => this.#setFilters({ page: e.detail.page })}
    ></lcm-list-pager>`;
  }

  #renderEmptyOrTable(page: JobsPage, filters: JobFilters): TemplateResult {
    if (page.items.length === 0 && !hasFilters(filters)) {
      return html`<lcm-empty-state level="3" icon="jobs" heading=${t('jobs.empty.title')} text=${t('jobs.empty.text')}></lcm-empty-state>`;
    }
    return html`<lcm-data-table
        external
        no-toolbar
        .columns=${this.#columns}
        .rows=${page.items}
        .labels=${this.#labels}
        .sort=${sortToTable(filters.sort)}
        active-id=${this.#selectedId}
        @lcm-sort=${(e: CustomEvent<SortState | null>) => this.#onSort(e)}
        @lcm-row-activate=${(e: CustomEvent<{ id: string }>) => this.#open(e.detail.id)}
      ></lcm-data-table>
      ${this.#renderPager(page.total, filters)}`;
  }

  #renderHistory(filters: JobFilters): TemplateResult {
    const { data, error, loading } = this.#history;
    if (!data) {
      if (error) return html`<lcm-empty-state level="3" icon="warning" heading=${t('jobs.error')} text=${error}></lcm-empty-state>`;
      return html`<lcm-skeleton shape="row" lines="8" label=${t('jobs.loading')}></lcm-skeleton>`;
    }
    return html`<div class="history ${loading ? 'stale' : ''}">${this.#renderEmptyOrTable(data, filters)}</div>`;
  }

  override render() {
    const filters = this.#current();
    const canAbort = auth.can('servers:write');
    const id = this.#selectedId;
    const listed = [...(this.#active.data?.items ?? []), ...(this.#history.data?.items ?? [])].find((job) => job.id === id);
    return html`<h1>${t('jobs.title')}</h1>
      <div class="stack">
        <lcm-jobs-active .jobs=${this.#active.data?.items ?? []} ?can-abort=${canAbort} @lcm-job-open=${(e: CustomEvent<{ id: string }>) => this.#open(e.detail.id)} @lcm-job-abort=${(e: CustomEvent<{ job: JobItem }>) => (this._abort = e.detail.job)}></lcm-jobs-active>
        <lcm-card level="2">
          <lcm-jobs-filters
            .filters=${filters}
            .servers=${this.#servers.data ?? []}
            .kinds=${this.#options.data?.types ?? []}
            .triggers=${this.#options.data?.triggered_by ?? []}
            @lcm-filters-change=${(e: CustomEvent<Partial<JobFilters>>) => this.#setFilters(e.detail)}
            @lcm-filters-reset=${() => router.setQuery(resetPatch(filters))}
          ></lcm-jobs-filters>
        </lcm-card>
        ${this.#renderHistory(filters)}
      </div>
      <lcm-jobs-detail job-id=${id} .job=${listed} ?can-abort=${canAbort} @lcm-close=${() => this.#close()} @lcm-job-abort=${(e: CustomEvent<{ job: JobItem }>) => (this._abort = e.detail.job)}></lcm-jobs-detail>
      <lcm-job-abort-dialog .job=${this._abort} @lcm-close=${() => (this._abort = undefined)}></lcm-job-abort-dialog>`;
  }
}
customElements.define('lcm-page-jobs', JobsPageElement);
