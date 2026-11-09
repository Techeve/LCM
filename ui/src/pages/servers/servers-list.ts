import { LitElement, css, html, nothing, type TemplateResult } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-button';
import '../../components/lcm-data-table';
import '../../components/lcm-empty-state';
import '../../components/lcm-input';
import '../../components/lcm-list-pager';
import '../../components/lcm-meter';
import '../../components/lcm-select';
import '../../components/lcm-skeleton';
import type { Column, SortState, TableLabels } from '../../components/lcm-data-table';
import type { Option } from '../../components/lcm-select';
import { baseStyles, safeStorage, utilStyles, type StatusLevel } from '../../components/shared';
import { GROUPS_URL, PAGE_SIZE, serversUrl, type ServerItem, type ServerStatus } from '../../api/servers';
import { CONSOLES_URL } from '../../api/server-detail';
import { consolesByServer, normaliseServerList, type ServerListPage } from '../../api/servers-list';
import { auth } from '../../lib/auth';
import { I18nController, SubscribeController } from '../../lib/controllers';
import { formatDateTime, formatRelative } from '../../lib/format';
import { t } from '../../lib/i18n';
import { LIMITS } from '../../lib/validate';
import { LiveData } from '../../lib/live-data';
import { router } from '../../lib/router';
import './list-cells';
import './server-os';
import { filtersFromQuery, hasFilters, queryPatch, resetPatch, sortFromTable, sortToTable, toServersQuery, type ServerFilters } from './servers-state';

const COLUMNS_KEY = 'lcm.servers.columns';
const COMPACT_COLUMNS = new Set(['status', 'name', 'os', 'updates']);
const SEARCH_DELAY_MS = 250;

export const STATUS_LEVEL: Record<ServerStatus, StatusLevel> = { excellent: 'great', green: 'ok', yellow: 'warn', red: 'crit' };

export function statusLabel(status: ServerStatus): string {
  return t(`servers.status.${status}`);
}

interface GroupOption {
  id: number;
  name: string;
}

function normaliseGroups(raw: unknown): GroupOption[] {
  const rows = Array.isArray(raw) ? raw : raw && typeof raw === 'object' && Array.isArray((raw as { items?: unknown }).items) ? (raw as { items: unknown[] }).items : [];
  return rows
    .filter((r): r is { id: number; name: string } => !!r && typeof r === 'object' && typeof (r as { id?: unknown }).id === 'number')
    .map((r) => ({ id: r.id, name: String(r.name ?? '') }));
}

function readHiddenColumns(): string[] | null {
  try {
    const parsed: unknown = JSON.parse(safeStorage.get(COLUMNS_KEY) ?? 'null');
    return Array.isArray(parsed) ? parsed.filter((c): c is string => typeof c === 'string') : null;
  } catch {
    return null;
  }
}

/**
 * Serverliste: Suche, Filter, Sortierung, Spaltenwahl und Blättern, alles in der Adresse (außer der Spaltenwahl, die
 * der Browser merkt). Mit `compact` (Detail steht daneben) bleiben nur Ampel, Name, OS-Logo und Updates.
 */
export class ServersList extends LitElement {
  static override properties = {
    activeId: { type: String, attribute: 'active-id' },
    compact: { type: Boolean, reflect: true },
    _query: { state: true },
    _more: { state: true },
    _hidden: { state: true },
  };
  static override styles = [
    baseStyles,
    utilStyles,
    css`
      :host {
        display: block;
        min-inline-size: 0;
      }
      lcm-input,
      lcm-select {
        flex: 1 1 9rem;
        max-inline-size: 16rem;
      }
      lcm-input.search {
        flex-basis: 12rem;
      }
      :host([compact]) lcm-input.search {
        flex-basis: 7rem;
      }
      :host([compact]) lcm-input,
      :host([compact]) lcm-select {
        max-inline-size: none;
      }
      :host([compact]) lcm-select {
        flex: 0 1 8.5rem;
      }
      .more-toggle {
        align-self: end;
      }
      .count {
        margin-block-start: var(--space-3);
        color: var(--text-2);
        font-size: var(--fs-s);
      }
      .empty-filter {
        display: grid;
        justify-items: center;
        gap: var(--space-3);
        padding: var(--space-6) var(--space-4);
        color: var(--text-2);
        text-align: center;
      }
    `,
  ];

  declare activeId: string;
  declare compact: boolean;
  declare _query: string;
  declare _more: boolean;
  declare _hidden: string[] | null;

  #queryKey = '';
  #filters: ServerFilters = filtersFromQuery(new URLSearchParams());
  #timer: ReturnType<typeof setTimeout> | undefined;

  readonly #servers = new LiveData<ServerListPage>(this, {
    url: () => serversUrl(toServersQuery(this.#current())),
    topics: () => ['servers'],
    transform: normaliseServerList,
  });
  readonly #consoles = new LiveData<Map<number, string[]>>(this, { url: () => CONSOLES_URL, topics: () => ['servers'], transform: consolesByServer });
  readonly #groups = new LiveData<GroupOption[]>(this, { url: () => GROUPS_URL, transform: normaliseGroups });

  constructor() {
    super();
    this.activeId = '';
    this.compact = false;
    this._query = '';
    this._more = false;
    this._hidden = readHiddenColumns();
    new I18nController(this);
    new SubscribeController(this, (listener) => router.onChange(listener));
    new SubscribeController(this, (listener) => auth.onChange(listener));
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    clearTimeout(this.#timer);
  }

  override willUpdate(): void {
    this.#current(); // hält das Suchfeld bei Änderungen der Adresse von außen aktuell
  }

  /** Die Filter aus der Adresse; dasselbe Objekt, solange die Query gleich bleibt. */
  #current(): ServerFilters {
    const key = router.state.query.toString();
    if (key !== this.#queryKey) {
      this.#queryKey = key;
      this.#filters = filtersFromQuery(router.state.query);
      this._query = this.#filters.q;
    }
    return this.#filters;
  }

  #set(change: Partial<ServerFilters>): void {
    router.setQuery(queryPatch(change));
  }

  #onSearch(value: string): void {
    this._query = value;
    clearTimeout(this.#timer);
    this.#timer = setTimeout(() => router.setQuery(queryPatch({ q: value.trim() }), { replace: true }), SEARCH_DELAY_MS);
  }

  #open(id: string): void {
    void router.navigate(`/servers/${encodeURIComponent(id)}${location.search}`);
  }

  get #labels(): TableLabels {
    return {
      caption: t('servers.table.caption'),
      filter: '',
      all: '',
      columns: t('servers.table.columns'),
      densityComfortable: '',
      densityCompact: '',
      selectAll: '',
      selectRow: '',
      selected: '',
      clear: '',
      empty: '',
      sortedAsc: t('servers.table.sortedAsc'),
      sortedDesc: t('servers.table.sortedDesc'),
    };
  }

  get #columns(): Column<ServerItem>[] {
    // Schmal neben dem Detail erklären Ampel und OS-Logo sich selbst: ohne Überschrift, das Logo vor dem Namen.
    const compact = this.compact;
    const status: Column<ServerItem> = { key: 'status', label: t('servers.column.status'), labelHidden: compact, sortable: true, locked: true, render: (r) => html`<lcm-srv-status .server=${r} ?compact=${compact}></lcm-srv-status>` };
    const os: Column<ServerItem> = { key: 'os', label: t('servers.column.os'), labelHidden: compact, sortable: true, render: (r) => this.#osCell(r) };
    const name: Column<ServerItem> = {
      key: 'name',
      label: t('servers.column.name'),
      sortable: true,
      locked: true,
      render: (r) => html`<lcm-srv-name .server=${r} .consoles=${this.#consoles.data?.get(r.id) ?? []}></lcm-srv-name>`,
    };
    const rest: Column<ServerItem>[] = [
      { key: 'host', label: t('servers.column.host'), kind: 'mono', render: (r) => html`<lcm-srv-host .server=${r}></lcm-srv-host>` },
      { key: 'kernel', label: t('servers.column.kernel'), kind: 'mono', sortable: true, render: (r) => r.kernel_version || '–' },
      { key: 'last_seen', label: t('servers.column.lastSeen'), sortable: true, render: (r) => this.#seenCell(r) },
      { key: 'updates', label: t('servers.column.updates'), sortable: true, align: 'end', render: (r) => this.#updatesCell(r) },
      { key: 'storage', label: t('servers.column.storage'), render: (r) => this.#storageCell(r) },
    ];
    if (compact) return [status, os, name, ...rest];
    const [host, ...more] = rest;
    return [status, name, host, os, ...more];
  }

  #osCell(server: ServerItem): TemplateResult {
    return html`<lcm-server-os .server=${server} ?compact=${this.compact}></lcm-server-os>`;
  }

  #seenCell(server: ServerItem): TemplateResult | string {
    return server.last_seen_at ? html`<span title=${formatDateTime(server.last_seen_at)}>${formatRelative(server.last_seen_at)}</span>` : t('servers.never');
  }

  /** Auch 0 als (graue) Marke, damit alle Zahlen der Spalte auf einer Linie stehen. */
  #updatesCell(server: ServerItem): TemplateResult {
    return html`<lcm-badge tone=${server.updates > 0 ? 'warn' : 'neutral'}>${server.updates}</lcm-badge>`;
  }

  #storageCell(server: ServerItem): TemplateResult | string {
    if (server.disk_total_mb <= 0) return '';
    const percent = Math.round((server.disk_used_mb / server.disk_total_mb) * 100);
    return html`<lcm-meter value=${percent} label=${t('servers.storage.used', { percent })}></lcm-meter>`;
  }

  get #hiddenColumns(): string[] | null {
    if (this.compact) return this.#columns.filter((c) => !COMPACT_COLUMNS.has(c.key)).map((c) => c.key);
    return this._hidden;
  }

  #statusOptions(): Option[] {
    return [
      { value: '', label: t('servers.filter.statusAll') },
      { value: 'excellent', label: statusLabel('excellent') },
      { value: 'green', label: statusLabel('green') },
      { value: 'yellow', label: statusLabel('yellow') },
      { value: 'red', label: statusLabel('red') },
      { value: 'yellow,red', label: t('servers.filter.attention') },
      { value: 'incapable', label: t('servers.incapable.label') },
    ];
  }

  #statusValue(filters: ServerFilters): string {
    if (filters.incapable && !filters.status) return 'incapable';
    return this.#statusOptions().some((o) => o.value === filters.status) ? filters.status : '';
  }

  #onStatus(value: string): void {
    this.#set(value === 'incapable' ? { status: '', incapable: true } : { status: value, incapable: false });
  }

  /** Die vorkommenden Betriebssysteme; ein Wert aus einem alten Link bleibt wählbar. */
  #osOptions(filters: ServerFilters): Option[] {
    const names = this.#servers.data?.osNames ?? [];
    const values = filters.os && !names.includes(filters.os) ? [filters.os, ...names] : names;
    return [{ value: '', label: t('servers.osAll') }, ...values.map((name) => ({ value: name, label: name }))];
  }

  #renderFilters(filters: ServerFilters): TemplateResult {
    const groups: Option[] = [{ value: '', label: t('servers.filter.groupAll') }, ...(this.#groups.data ?? []).map((g) => ({ value: String(g.id), label: g.name }))];
    const extended = !this.compact || this._more || filters.os !== '' || filters.kernel !== '' || filters.group !== '';
    return html`<lcm-input maxlength=${LIMITS.query} class="search" width="full" slot="toolbar" type="search" icon="search" label=${t('servers.filter.search')} .value=${this._query} @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#onSearch(e.detail.value)}></lcm-input>
      <lcm-select slot="toolbar" label=${t('servers.filter.status')} .options=${this.#statusOptions()} .value=${this.#statusValue(filters)} @lcm-change=${(e: CustomEvent<{ value: string }>) => this.#onStatus(e.detail.value)}></lcm-select>
      ${extended
        ? html`<lcm-select slot="toolbar" label=${t('servers.filter.group')} .options=${groups} .value=${filters.group} @lcm-change=${(e: CustomEvent<{ value: string }>) => this.#set({ group: e.detail.value })}></lcm-select>
            <lcm-select slot="toolbar" label=${t('servers.filter.os')} .options=${this.#osOptions(filters)} .value=${filters.os} @lcm-change=${(e: CustomEvent<{ value: string }>) => this.#set({ os: e.detail.value })}></lcm-select>
            <lcm-input slot="toolbar" label=${t('servers.filter.kernel')} placeholder=${t('servers.kernelPlaceholder')} mono .value=${filters.kernel} @lcm-change=${(e: CustomEvent<{ value: string }>) => this.#set({ kernel: e.detail.value.trim() })}></lcm-input>`
        : html`<lcm-button slot="toolbar" class="more-toggle" variant="ghost" icon="filter" icon-only label=${t('servers.filter.more')} @click=${() => (this._more = true)}></lcm-button>`}`;
  }

  #renderPager(total: number, filters: ServerFilters): TemplateResult | typeof nothing {
    if (total === 0) return nothing;
    if (total <= PAGE_SIZE) return html`<div class="count">${t('servers.count', { count: total })}</div>`;
    const from = (filters.page - 1) * PAGE_SIZE + 1;
    const to = Math.min(total, filters.page * PAGE_SIZE);
    return html`<lcm-list-pager
      page=${filters.page}
      page-size=${PAGE_SIZE}
      total=${total}
      range-label=${t('servers.pager.range', { from, to, total })}
      previous-label=${t('servers.pager.previous')}
      next-label=${t('servers.pager.next')}
      @lcm-page=${(e: CustomEvent<{ page: number }>) => this.#set({ page: e.detail.page })}
    ></lcm-list-pager>`;
  }

  #renderTable(page: ServerListPage, filters: ServerFilters): TemplateResult {
    return html`<lcm-data-table
        external
        ?no-chooser=${this.compact}
        density="compact"
        .columns=${this.#columns}
        .rows=${page.items}
        .labels=${this.#labels}
        .sort=${sortToTable(filters.sort)}
        .hiddenColumns=${this.#hiddenColumns}
        active-id=${this.activeId}
        @lcm-sort=${(e: CustomEvent<SortState | null>) => this.#set({ sort: sortFromTable(e.detail) })}
        @lcm-columns-change=${(e: CustomEvent<{ hidden: string[] }>) => this.#saveColumns(e.detail.hidden)}
        @lcm-row-activate=${(e: CustomEvent<{ id: string }>) => this.#open(e.detail.id)}
      >
        ${this.#renderFilters(filters)}
      </lcm-data-table>
      ${page.items.length === 0 ? this.#renderNoMatch(filters) : nothing} ${this.#renderPager(page.total, filters)}`;
  }

  #saveColumns(hidden: string[]): void {
    this._hidden = hidden;
    safeStorage.set(COLUMNS_KEY, JSON.stringify(hidden));
  }

  #renderNoMatch(filters: ServerFilters): TemplateResult {
    return html`<div class="empty-filter">
      <span>${t('servers.table.empty')}</span>
      ${hasFilters(filters) ? html`<lcm-button size="small" variant="outline" icon="x" @click=${() => router.setQuery(resetPatch(filters))}>${t('servers.filter.reset')}</lcm-button>` : nothing}
    </div>`;
  }

  #renderBody(filters: ServerFilters): TemplateResult {
    const { data, error, loading } = this.#servers;
    if (!data) {
      if (error) return html`<lcm-empty-state level="3" icon="warning" heading=${t('servers.error')} text=${error}><lcm-button slot="action" icon="refresh" @click=${() => this.#servers.reload()}>${t('servers.retry')}</lcm-button></lcm-empty-state>`;
      return html`<lcm-skeleton shape="row" lines="8" label=${t('servers.loading')}></lcm-skeleton>`;
    }
    if (data.total === 0 && !hasFilters(filters)) {
      return html`<lcm-empty-state level="2" icon="server" heading=${t('servers.empty.title')} text=${t('servers.empty.text')}>
        ${auth.can('servers:write') ? html`<lcm-button slot="action" variant="primary" icon="plus" href="/servers/join">${t('servers.add')}</lcm-button>` : nothing}
      </lcm-empty-state>`;
    }
    return html`<div class=${loading ? 'stale' : ''}>${this.#renderTable(data, filters)}</div>`;
  }

  override render() {
    return this.#renderBody(this.#current());
  }
}
customElements.define('lcm-servers-list', ServersList);
