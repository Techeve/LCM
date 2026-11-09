import { css, html, nothing, type TemplateResult } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-button';
import '../../components/lcm-data-table';
import '../../components/lcm-empty-state';
import '../../components/lcm-input';
import '../../components/lcm-skeleton';
import '../../components/lcm-switch';
import type { Column, TableLabels } from '../../components/lcm-data-table';
import { baseStyles } from '../../components/shared';
import { toast } from '../../components/lcm-toaster';
import { normalisePackages, packagesUrl, pendingOf, serversApi, type PreviewAction, type ServerPackage } from '../../api/servers';
import { normalisePins, packagesApi, pinFor, pinsUrl, type PinsView } from '../../api/servers-packages';
import { SubscribeController } from '../../lib/controllers';
import { errorText, t } from '../../lib/i18n';
import { LIMITS } from '../../lib/validate';
import { LiveData } from '../../lib/live-data';
import { router } from '../../lib/router';
import { store } from '../../lib/store';
import './pkg-pins';
import './pkg-version';
import { SrvAction } from './srv-action';
import './srv-action';
import { tabStyles } from './srv-styles';
import { SrvTab } from './srv-tab';

const SHOWN_STEP = 200;
const DEEP_LINK_ACTIONS: readonly string[] = ['refresh', 'upgrade-all', 'upgrade-security'];

interface PackageRow extends ServerPackage {
  id: string;
}

/**
 * Pakete: ausstehende Updates (Standard) oder der ganze Bestand. Alle Aktionen sind Server-Aktionen mit Erklärblase,
 * Befehlsvorschau und Bestätigung. Ausgewählte Pakete lassen sich gemeinsam aktualisieren oder entfernen.
 * Je Paket lässt sich eine bestimmte Version installieren (`?modal=version&vp=<paket>`) und ein Pin setzen; die Pins
 * verwaltet ein eigenes Fenster (`?modal=pins`).
 * Suche und Umfang stehen in der Adresse (`?pq=`, `?all=1`); `?do=` öffnet die Bestätigung einer Aktion (Befehlspalette).
 */
export class SrvPackages extends SrvTab {
  static override properties = {
    ...SrvTab.properties,
    _selected: { state: true },
    _shown: { state: true },
  };
  static override styles = [
    baseStyles,
    tabStyles,
    css`
      lcm-input {
        flex: 1 1 12rem;
        max-inline-size: 22rem;
      }
      lcm-switch {
        flex: none;
      }
      .more {
        display: flex;
        justify-content: center;
        margin-block-start: var(--space-3);
      }
      .bulk-actions {
        display: contents;
      }
      .row-actions {
        display: inline-flex;
        gap: var(--space-1);
      }
    `,
  ];

  declare _selected: string[];
  declare _shown: number;

  readonly #packages = new LiveData<ServerPackage[]>(this, {
    url: () => packagesUrl(this.server.id),
    topics: () => [`server:${this.server.id}`],
    transform: normalisePackages,
  });
  readonly #pins = new LiveData<PinsView>(this, {
    url: () => pinsUrl(this.server.id),
    topics: () => [`server:${this.server.id}`],
    transform: normalisePins,
  });

  constructor() {
    super();
    this._selected = [];
    this._shown = SHOWN_STEP;
    new SubscribeController(this, (listener) => router.onChange(listener));
  }

  override updated(): void {
    this.#openDeepLink();
  }

  get #all(): boolean {
    return router.state.query.get('all') === '1';
  }

  get #search(): string {
    return router.state.query.get('pq') ?? '';
  }

  /** `?do=upgrade-all` öffnet nach dem Laden die Bestätigung und räumt die Adresse auf. */
  #openDeepLink(): void {
    const wanted = router.state.query.get('do');
    if (!wanted) return;
    if (!DEEP_LINK_ACTIONS.includes(wanted)) return queueMicrotask(() => router.setQuery({ do: null }, { replace: true }));
    const action = this.renderRoot.querySelector<SrvAction>(`lcm-srv-action[data-do="${wanted}"]`);
    if (!action) return;
    // Außerhalb des Aktualisierungszyklus: die Adresse zu ändern stößt ein neues Rendern an.
    queueMicrotask(() => {
      router.setQuery({ do: null }, { replace: true });
      void action.openConfirm();
    });
  }

  // ---- Aktionen ----

  #action(options: { key: string; action: PreviewAction; run: () => ReturnType<typeof serversApi.refreshPackages>; variant?: 'primary' | 'outline' | 'secondary' | 'danger'; icon: 'refresh' | 'download' | 'shield' | 'trash' | 'package'; deep?: boolean; params?: { package?: string[] } }): TemplateResult {
    const { key } = options;
    return html`<lcm-srv-action
      data-do=${options.deep ? options.action : nothing}
      variant=${options.variant ?? 'outline'}
      icon=${options.icon}
      server-id=${this.server.id}
      server-name=${this.server.name}
      action=${options.action}
      .params=${options.params ?? {}}
      label=${t(`servers.packages.${key}.label`)}
      help=${t(`servers.packages.${key}.help`)}
      meaning=${t(`servers.packages.${key}.meaning`)}
      ?danger=${options.variant === 'danger'}
      .run=${options.run}
    ></lcm-srv-action>`;
  }

  get #modal(): string {
    return router.state.query.get('modal') ?? '';
  }

  #openModal(modal: 'pins' | 'version', pkg = ''): void {
    router.setQuery({ modal, vp: pkg || null });
  }

  #closeModal(): void {
    router.setQuery({ modal: null, vp: null }, { replace: true });
  }

  async #pin(name: string): Promise<void> {
    try {
      await packagesApi.createPin(this.server.id, { name, no_remove: true, hold: false, note: '', global: false });
      toast({ tone: 'ok', title: t('serverPackages.pins.added', { name }) });
      store.invalidate(pinsUrl(this.server.id));
    } catch (error) {
      toast({ tone: 'crit', title: t('serverPackages.pins.failed'), message: errorText(error) });
    }
  }

  #renderPinsButton(): TemplateResult {
    const count = this.#pins.data?.pins.length ?? 0;
    return html`<lcm-srv-action
      no-confirm
      variant="outline"
      icon="lock"
      server-id=${this.server.id}
      server-name=${this.server.name}
      label=${t('serverPackages.pins.open', { count })}
      help=${t('serverPackages.pins.openHelp')}
      .run=${async () => this.#openModal('pins')}
    ></lcm-srv-action>`;
  }

  #renderActions(): TemplateResult | typeof nothing {
    if (!this.canWrite) return nothing;
    const id = this.server.id;
    return html`<div class="toolbar">
      ${this.#action({ key: 'refresh', action: 'refresh', run: () => serversApi.refreshPackages(id), icon: 'refresh', deep: true })}
      ${this.#action({ key: 'upgradeAll', action: 'upgrade-all', run: () => serversApi.upgradeAll(id), icon: 'download', variant: 'primary', deep: true })}
      ${this.#action({ key: 'upgradeSecurity', action: 'upgrade-security', run: () => serversApi.upgradeSecurity(id), icon: 'shield', deep: true })}
      ${this.#action({ key: 'autoremove', action: 'autoremove', run: () => serversApi.autoremove(id), icon: 'trash' })}
      ${this.server.package_manager === 'apt' ? this.#action({ key: 'kernels', action: 'kernels-cleanup', run: () => serversApi.cleanupKernels(id), icon: 'trash' }) : nothing}
      <span class="grow"></span>
      ${this.#renderPinsButton()}
    </div>`;
  }

  // ---- Tabelle ----

  get #labels(): TableLabels {
    return {
      caption: t('servers.packages.caption'),
      filter: '',
      all: '',
      columns: '',
      densityComfortable: '',
      densityCompact: '',
      selectAll: t('servers.packages.selectAll'),
      selectRow: t('servers.packages.selectRow'),
      selected: t('servers.packages.selected'),
      clear: t('servers.packages.clear'),
      empty: t('servers.packages.noMatch'),
      sortedAsc: t('servers.table.sortedAsc'),
      sortedDesc: t('servers.table.sortedDesc'),
    };
  }

  get #columns(): Column<PackageRow>[] {
    return [
      { key: 'name', label: t('servers.packages.column.name'), sortable: true, locked: true, render: (r) => this.#nameCell(r) },
      { key: 'version', label: t('servers.packages.column.installed'), kind: 'mono', sortable: true },
      { key: 'candidate', label: t('servers.packages.column.available'), kind: 'mono', sortable: true, render: (r) => r.candidate || '–' },
      { key: 'security', label: t('servers.packages.column.type'), sortable: true, sortValue: (r) => (r.security ? 0 : 1), render: (r) => this.#typeCell(r) },
      ...(this.canWrite ? [{ key: 'actions', label: t('serverPackages.packages.actions'), align: 'end' as const, render: (r: PackageRow) => this.#rowActions(r) }] : []),
    ];
  }

  #nameCell(pkg: PackageRow): TemplateResult {
    const pin = pinFor(this.#pins.data?.pins ?? [], pkg.name);
    return html`<strong>${pkg.name}</strong>${pkg.held_reason ? html` <lcm-badge>${t('servers.packages.held')}</lcm-badge>` : nothing}${pin
        ? html` <lcm-badge tone="ok" title=${t('serverPackages.pins.pinnedBy', { name: pin.name })}>${t('serverPackages.pins.pinned')}</lcm-badge>`
        : nothing}`;
  }

  #rowActions(pkg: PackageRow): TemplateResult {
    const pins = this.#pins.data;
    const pinned = !!pins && !!pinFor(pins.pins, pkg.name);
    return html`<span class="row-actions">
      <lcm-button size="small" variant="ghost" @click=${() => this.#openModal('version', pkg.name)}>${t('serverPackages.packages.version')}</lcm-button>
      ${pins?.available && !pinned
        ? html`<lcm-button size="small" variant="ghost" icon="lock" icon-only label=${t('serverPackages.pins.pinPackage', { name: pkg.name })} title=${t('serverPackages.pins.pinPackage', { name: pkg.name })} @click=${() => void this.#pin(pkg.name)}></lcm-button>`
        : nothing}
    </span>`;
  }

  #renderModals(packages: ServerPackage[]): TemplateResult | typeof nothing {
    if (!this.canWrite) return nothing;
    const modal = this.#modal;
    const wanted = router.state.query.get('vp') ?? '';
    const pkg = modal === 'version' ? packages.find((p) => p.name === wanted) : undefined;
    return html`<lcm-pkg-pins
        ?open=${modal === 'pins'}
        server-id=${this.server.id}
        server-name=${this.server.name}
        ?proxmox=${!!this.server.proxmox_type}
        .view=${this.#pins.data}
        @lcm-close=${() => this.#closeModal()}
      ></lcm-pkg-pins>
      <lcm-pkg-version ?open=${!!pkg} server-id=${this.server.id} server-name=${this.server.name} .pkg=${pkg} @lcm-close=${() => this.#closeModal()}></lcm-pkg-version>`;
  }

  #typeCell(pkg: PackageRow): TemplateResult | string {
    if (pkg.security) return html`<lcm-badge tone="crit">${t('servers.packages.security')}</lcm-badge>`;
    return pkg.candidate ? html`<lcm-badge tone="warn">${t('servers.packages.update')}</lcm-badge>` : '';
  }

  #onSearch(value: string): void {
    router.setQuery({ pq: value.trim() || null }, { replace: true });
  }

  #onSelection(event: CustomEvent<{ selected: string[] }>): void {
    this._selected = event.detail.selected;
  }

  #renderBulk(): TemplateResult | typeof nothing {
    if (!this.canWrite || this._selected.length === 0) return nothing;
    const id = this.server.id;
    const names = this._selected;
    return html`<span slot="bulk" class="bulk-actions">
      ${this.#action({ key: 'updateSelected', action: 'update', run: () => serversApi.updatePackages(id, names), icon: 'download', variant: 'primary', params: { package: names } })}
      ${this.#action({ key: 'removeSelected', action: 'remove', run: () => serversApi.removePackages(id, names), icon: 'trash', variant: 'danger', params: { package: names } })}
    </span>`;
  }

  #renderToolbarSlot(): TemplateResult {
    return html`<lcm-input maxlength=${LIMITS.query} slot="toolbar" width="full" type="search" icon="search" label=${t('servers.packages.search')} .value=${this.#search} @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#onSearch(e.detail.value)}></lcm-input>
      <lcm-switch slot="toolbar" label=${t('servers.packages.showAll')} .checked=${this.#all} @lcm-change=${(e: CustomEvent<{ checked: boolean }>) => router.setQuery({ all: e.detail.checked ? 1 : null }, { replace: true })}></lcm-switch>`;
  }

  #renderTable(rows: PackageRow[]): TemplateResult {
    const shown = rows.slice(0, this._shown);
    return html`<lcm-data-table
        density="compact"
        no-chooser
        ?selectable=${this.canWrite}
        .columns=${this.#columns}
        .rows=${shown}
        .labels=${this.#labels}
        .selected=${this._selected}
        @lcm-selection-change=${this.#onSelection}
      >
        ${this.#renderToolbarSlot()} ${this.#renderBulk()}
      </lcm-data-table>
      ${rows.length > shown.length
        ? html`<div class="more"><lcm-button variant="outline" @click=${() => (this._shown += SHOWN_STEP)}>${t('servers.packages.more', { count: rows.length - shown.length })}</lcm-button></div>`
        : nothing}`;
  }

  #filtered(packages: ServerPackage[]): PackageRow[] {
    const needle = this.#search.toLowerCase();
    const base = this.#all ? packages : pendingOf(packages);
    return base.filter((p) => !needle || p.name.toLowerCase().includes(needle)).map((p) => ({ ...p, id: p.name }));
  }

  #renderEmpty(packages: ServerPackage[]): TemplateResult {
    if (packages.length === 0) {
      return html`<lcm-empty-state level="3" icon="package" heading=${t('servers.packages.empty.title')} text=${t('servers.packages.empty.text')}></lcm-empty-state>`;
    }
    const failed = this.server.update_check_error;
    const state = failed ? 'unchecked' : this.server.incapable ? 'unsure' : 'uptodate';
    return html`<lcm-empty-state level="3" icon=${state === 'uptodate' ? 'check' : 'warning'} heading=${t(`servers.packages.${state}.title`)} text=${t(`servers.packages.${state}.text`, { count: packages.length, error: failed })}>
      <lcm-button slot="action" variant="outline" @click=${() => router.setQuery({ all: 1 }, { replace: true })}>${t('servers.packages.showAll')}</lcm-button>
    </lcm-empty-state>`;
  }

  override render() {
    const { data, error } = this.#packages;
    if (!data) {
      return error
        ? html`<lcm-empty-state level="3" icon="warning" heading=${t('servers.error')} text=${error}></lcm-empty-state>`
        : html`<lcm-skeleton shape="row" lines="6" label=${t('servers.loading')}></lcm-skeleton>`;
    }
    const rows = this.#filtered(data);
    const showEmpty = rows.length === 0 && this.#search === '' && (!this.#all || data.length === 0);
    return html`<div class="stack">
        ${this.#renderActions()} ${showEmpty ? this.#renderEmpty(data) : this.#renderTable(rows)}
      </div>
      ${this.#renderModals(data)}`;
  }
}
customElements.define('lcm-srv-packages', SrvPackages);
