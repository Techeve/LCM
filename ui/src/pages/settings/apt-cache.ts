import { LitElement, css, html, nothing } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-button';
import '../../components/lcm-card';
import '../../components/lcm-data-table';
import '../../components/lcm-dialog';
import '../../components/lcm-empty-state';
import '../../components/lcm-input';
import '../../components/lcm-select';
import '../../components/lcm-server-action';
import '../../components/lcm-skeleton';
import '../../components/lcm-switch';
import type { Column } from '../../components/lcm-data-table';
import type { Impact } from '../../components/lcm-dialog';
import { toast } from '../../components/lcm-toaster';
import { baseStyles } from '../../components/shared';
import {
  ALL_SERVERS_URL,
  APT_CACHES_URL,
  aptCacheOverviewUrl,
  normaliseAptCaches,
  normaliseOverview,
  normaliseServerLinks,
  settingsMoreApi,
  type AptCache,
  type AptCacheOverview,
  type ProbeStatus,
  type ServerLink,
} from '../../api/settings-more';
import { auth } from '../../lib/auth';
import { I18nController, SubscribeController } from '../../lib/controllers';
import { events } from '../../lib/events';
import { errorText, t } from '../../lib/i18n';
import { LIMITS } from '../../lib/validate';
import { LiveData } from '../../lib/live-data';
import { palette } from '../../lib/palette';
import { router } from '../../lib/router';
import { store } from '../../lib/store';
import { cacheBody, cacheNameFor, connectedCount, connectedServers, selectedCache, validateCacheForm, type CacheForm } from './packages/apt-cache-model';
import { ModalForm, actionLabels, generalError, sectionStyles, tableLabels } from './packages/kit';
import './packages/cells';
import './packages/monitor-card';
import './packages/settings-modal';

const FIELDS: Record<string, string> = { 'field:name': 'name', 'field:url': 'url' };

type Probe = ProbeStatus | 'checking';
type Confirm = { kind: 'delete'; cache: AptCache } | { kind: 'disconnect'; server: ServerLink } | { kind: 'restart' } | { kind: 'permanent'; enable: boolean };

/** Einstellungen → APT-Cache: zentrale Paket-Caches (apt-cacher-ng), Statistik, angebundene Server, Dienst auf dem LCM-Host. */
export class SettingsAptCache extends LitElement {
  static override properties = {
    _dialogError: { state: true },
    _probes: { state: true },
    _confirm: { state: true },
    _busy: { state: true },
    _output: { state: true },
  };
  static override styles = [
    baseStyles,
    sectionStyles,
    css`
      .stats {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(14rem, 1fr));
        gap: var(--space-3);
      }
      .tile {
        padding: var(--space-4);
        border-radius: var(--radius-m);
        background: var(--surface-2);
      }
      .tile .label {
        color: var(--text-2);
        font-size: var(--fs-s);
      }
      .tile .value {
        margin-block: var(--space-1);
        font-size: var(--fs-2xl);
        font-weight: var(--fw-semibold);
        font-variant-numeric: tabular-nums;
      }
      pre {
        max-block-size: 14rem;
        margin: var(--space-3) 0 0;
        padding: var(--space-3);
        overflow: auto;
        border-radius: var(--radius-m);
        background: var(--surface-2);
        font-family: var(--font-mono);
        font-size: var(--fs-xs);
        white-space: pre-wrap;
      }
      .card-text {
        margin: 0 0 var(--space-3);
        color: var(--text-2);
        font-size: var(--fs-s);
      }
    `,
  ];

  /** Fehler der bestätigten Aktion; steht im Dialog über dem Fuß. */
  declare _dialogError: string;
  declare _probes: Record<number, Probe>;
  declare _confirm: Confirm | null;
  declare _busy: boolean;
  declare _output: string;
  #stops: (() => void)[] = [];
  #pendingJobs = 0;

  readonly #caches = new LiveData<AptCache[]>(this, { url: () => APT_CACHES_URL, transform: normaliseAptCaches });
  readonly #overview = new LiveData<AptCacheOverview>(this, {
    url: () => (this.#caches.data ? aptCacheOverviewUrl(this.#selected?.id ?? 0) : null),
    transform: normaliseOverview,
  });
  readonly #servers = new LiveData<ServerLink[]>(this, {
    url: () => (auth.can('servers:read') ? ALL_SERVERS_URL : null),
    topics: () => (auth.can('servers:read') ? ['servers'] : []),
    transform: normaliseServerLinks,
  });
  readonly #modal = new ModalForm<CacheForm>(this, (modal) => {
    if (modal.kind === 'new') return { name: '', url: '' };
    if (modal.kind !== 'edit' || !this.#caches.data) return this.#caches.loading ? undefined : null;
    const cache = this.#caches.data.find((c) => c.id === modal.id);
    return cache ? { name: cache.name, url: cache.url } : null;
  });

  constructor() {
    super();
    this._dialogError = '';
    this._probes = {};
    this._confirm = null;
    this._busy = false;
    this._output = '';
    new I18nController(this);
    new SubscribeController(this, (listener) => router.onChange(listener));
  }

  override connectedCallback(): void {
    super.connectedCallback();
    palette.register(this, [{ id: 'apt-cache-new', key: 'n', label: t('settings.aptCache.add'), icon: 'plus', run: () => this.#modal.show({ kind: 'new' }) }]);
    // Neustart und permanentes Caching laufen als Job auf dem LCM-Host; danach den Stand neu holen.
    this.#stops = [
      events.subscribe(['jobs']),
      events.onEvent((message) => {
        if (message.event !== 'job.finished' || this.#pendingJobs === 0) return;
        this.#pendingJobs -= 1;
        store.invalidate('/api/v1/settings/apt-cache/overview');
      }),
    ];
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    palette.unregister(this);
    this.#stops.forEach((stop) => stop());
    this.#stops = [];
  }

  override willUpdate(): void {
    this.#modal.sync();
  }

  get #selected(): AptCache | undefined {
    return selectedCache(this.#caches.data ?? [], router.state.query.get('cache'));
  }

  get #canWriteServers(): boolean {
    return auth.can('servers:write');
  }

  async #probe(cache: AptCache): Promise<void> {
    this._probes = { ...this._probes, [cache.id]: 'checking' };
    try {
      this._probes = { ...this._probes, [cache.id]: await settingsMoreApi.checkAptCache(cache.id) };
    } catch (error) {
      this._probes = { ...this._probes, [cache.id]: { reachable: false, running: false, message: errorText(error) } };
    }
  }

  async #save(): Promise<void> {
    const form = this.#modal.form;
    if (!form) return;
    const id = this.#modal.editId;
    const valid = Object.keys(validateCacheForm(form)).length === 0;
    if (await this.#modal.submit(valid, () => settingsMoreApi.saveAptCache(id, cacheBody(form)), FIELDS)) {
      toast({ tone: 'ok', title: t('settings.aptCache.notice.saved', { name: form.name.trim() }) });
    }
  }

  async #confirmAction(): Promise<void> {
    const confirm = this._confirm;
    if (!confirm) return;
    this._busy = true;
    this._dialogError = '';
    try {
      await this.#run(confirm);
      this._confirm = null;
    } catch (error) {
      this._dialogError = errorText(error);
    } finally {
      this._busy = false;
    }
  }

  async #run(confirm: Confirm): Promise<void> {
    const serverId = this.#overview.data?.server_id;
    switch (confirm.kind) {
      case 'delete':
        await settingsMoreApi.deleteAptCache(confirm.cache.id);
        toast({ tone: 'ok', title: t('settings.aptCache.notice.deleted', { name: confirm.cache.name }) });
        return;
      case 'disconnect':
        this._output = await settingsMoreApi.disconnectAptProxy(confirm.server.id);
        toast({ tone: 'ok', title: t('settings.aptCache.notice.disconnected', { name: confirm.server.name }) });
        return;
      case 'restart':
        if (!serverId) return;
        await settingsMoreApi.restartAptCacher(serverId);
        this.#pendingJobs += 1;
        toast({ tone: 'info', title: t('settings.aptCache.notice.restartStarted') });
        return;
      case 'permanent':
        if (!serverId) return;
        await settingsMoreApi.setPermanentCache(serverId, confirm.enable);
        this.#pendingJobs += 1;
        toast({ tone: 'info', title: t('settings.aptCache.notice.permanentStarted') });
    }
  }

  #status(cache: AptCache) {
    const probe = this._probes[cache.id];
    if (!probe) return html`<span class="muted">–</span>`;
    if (probe === 'checking') return html`<span class="muted">${t('settings.aptCache.status.checking')}</span>`;
    const [tone, key] = probe.running
      ? ['ok', 'settings.aptCache.status.running']
      : probe.reachable
        ? ['warn', 'settings.aptCache.status.foreign']
        : ['crit', 'settings.aptCache.status.down'];
    return html`<lcm-badge tone=${tone} title=${probe.message}>${t(key)}</lcm-badge>`;
  }

  get #columns(): Column<AptCache>[] {
    const servers = this.#servers.data;
    return [
      { key: 'name', label: t('settings.aptCache.column.name'), sortable: true, render: (r) => html`<strong>${r.name}</strong>` },
      { key: 'url', label: t('settings.aptCache.column.url'), kind: 'mono' },
      ...(servers ? [{ key: 'servers', label: t('settings.aptCache.column.servers'), align: 'end' as const, render: (r: AptCache) => String(connectedCount(servers, r)) }] : []),
      { key: 'status', label: t('settings.aptCache.column.status'), render: (r) => this.#status(r) },
      {
        key: 'actions',
        label: t('app.actions'),
        labelHidden: true,
        align: 'end',
        render: (r) => html`<lcm-settings-actions>
          <lcm-button
            variant="ghost"
            size="small"
            icon="refresh"
            ?busy=${this._probes[r.id] === 'checking'}
            @click=${(e: Event) => {
              e.stopPropagation();
              void this.#probe(r);
            }}
            >${t('settings.aptCache.check')}</lcm-button
          >
          <lcm-button
            variant="ghost"
            size="small"
            icon="trash"
            icon-only
            label=${t('settings.aptCache.delete.label')}
            @click=${(e: Event) => {
              e.stopPropagation();
              this._confirm = { kind: 'delete', cache: r };
            }}
          ></lcm-button>
        </lcm-settings-actions>`,
      },
    ];
  }

  #cachesCard() {
    const caches = this.#caches.data;
    let body;
    if (!caches) body = this.#caches.error ? html`<p class="callout warn">${this.#caches.error}</p>` : html`<lcm-skeleton lines="3"></lcm-skeleton>`;
    else if (caches.length === 0) {
      body = html`<lcm-empty-state level="3" icon="package" heading=${t('settings.aptCache.empty.title')} text=${t('settings.aptCache.empty.text')}></lcm-empty-state>`;
    } else {
      body = html`<lcm-data-table
        no-toolbar
        density="compact"
        active-id=${String(this.#modal.editId ?? '')}
        .columns=${this.#columns}
        .rows=${caches.map((c) => ({ ...c, id: String(c.id) }))}
        .labels=${tableLabels(t('settings.aptCache.caption'), t('settings.aptCache.empty.title'))}
        @lcm-row-activate=${(e: CustomEvent<{ id: string }>) => this.#modal.show({ kind: 'edit', id: Number(e.detail.id) })}
      ></lcm-data-table>`;
    }
    return html`<lcm-card level="2" heading=${t('settings.aptCache.caches')}>
      <lcm-button slot="actions" variant="primary" size="small" icon="plus" @click=${() => this.#modal.show({ kind: 'new' })}>${t('settings.aptCache.add')}</lcm-button>
      ${body}
    </lcm-card>`;
  }

  #statsCard() {
    const caches = this.#caches.data ?? [];
    if (caches.length === 0) return nothing;
    const stats = this.#overview.data?.stats;
    const tile = (label: string, value: string, recent: string) => html`<div class="tile">
      <div class="label">${label}</div>
      <div class="value">${value || '–'}</div>
      <div class="label">${t('settings.aptCache.stats.recent', { value: recent || '–' })}</div>
    </div>`;
    return html`<lcm-card level="2" heading=${t('settings.aptCache.stats.title')}>
      ${caches.length > 1
        ? html`<lcm-select
            slot="actions"
            label=${t('settings.aptCache.stats.cache')}
            .options=${caches.map((c) => ({ value: String(c.id), label: c.name }))}
            .value=${String(this.#selected?.id ?? '')}
            @lcm-change=${(e: CustomEvent<{ value: string }>) => router.setQuery({ cache: e.detail.value }, { replace: true })}
          ></lcm-select>`
        : nothing}
      ${!this.#overview.data
        ? html`<lcm-skeleton lines="2"></lcm-skeleton>`
        : stats
          ? html`<div class="stats">
                ${tile(t('settings.aptCache.stats.fetched'), stats.data_fetched, stats.data_fetched_recent)}
                ${tile(t('settings.aptCache.stats.served'), stats.data_served, stats.data_served_recent)}
              </div>
              <p class="card-text">${t('settings.aptCache.stats.hint')}</p>`
          : html`<p class="card-text">${t('settings.aptCache.stats.unavailable')}</p>`}
    </lcm-card>`;
  }

  #connectedCard() {
    if (!auth.can('servers:read')) return nothing;
    const servers = this.#servers.data;
    const caches = this.#caches.data ?? [];
    const connected = servers ? connectedServers(servers) : [];
    const columns: Column<ServerLink>[] = [
      { key: 'name', label: t('settings.aptCache.connected.server'), sortable: true, render: (r) => html`<lcm-settings-cell><a href=${`/servers/${r.id}`}>${r.name}</a></lcm-settings-cell>` },
      { key: 'host', label: t('settings.aptCache.connected.host'), kind: 'mono' },
      { key: 'cache', label: t('settings.aptCache.connected.cache'), render: (r) => cacheNameFor(caches, r.apt_proxy_url) },
    ];
    if (this.#canWriteServers) {
      columns.push({
        key: 'actions',
        label: t('app.actions'),
        labelHidden: true,
        align: 'end',
        render: (r) => html`<lcm-server-action
          label=${t('settings.aptCache.disconnect.label')}
          help=${t('settings.aptCache.disconnect.help')}
          variant="ghost"
          icon="x"
          .labels=${actionLabels()}
          .preview=${{ reversible: true, reboot: false }}
          @click=${() => (this._confirm = { kind: 'disconnect', server: r })}
        ></lcm-server-action>`,
      });
    }
    return html`<lcm-card level="2" heading=${t('settings.aptCache.connected.title')}>
      <p class="card-text">${t('settings.aptCache.connected.intro')}</p>
      ${!servers
        ? html`<lcm-skeleton lines="2"></lcm-skeleton>`
        : connected.length === 0
          ? html`<p class="card-text">${t('settings.aptCache.connected.none')}</p>`
          : html`<lcm-data-table
              no-toolbar
              density="compact"
              .columns=${columns}
              .rows=${connected.map((s) => ({ ...s, id: String(s.id) }))}
              .labels=${tableLabels(t('settings.aptCache.connected.title'), t('settings.aptCache.connected.none'))}
            ></lcm-data-table>`}
      ${this._output ? html`<pre aria-label=${t('settings.aptCache.connected.output')}>${this._output}</pre>` : nothing}
    </lcm-card>`;
  }

  #hostCard() {
    const overview = this.#overview.data;
    if (!overview) return nothing;
    if (!overview.managed) {
      return html`<lcm-card level="2" heading=${t('settings.aptCache.host.title')}><p class="card-text">${t('settings.aptCache.host.notManaged')}</p></lcm-card>`;
    }
    const canWrite = this.#canWriteServers;
    return html`<lcm-card level="2" heading=${t('settings.aptCache.host.title')}>
      ${canWrite
        ? html`<lcm-server-action
            slot="actions"
            label=${t('settings.aptCache.host.restart')}
            help=${t('settings.aptCache.host.restartHelp')}
            icon="refresh"
            .labels=${actionLabels()}
            .preview=${{ reversible: true, reboot: false }}
            @click=${() => (this._confirm = { kind: 'restart' })}
          ></lcm-server-action>`
        : nothing}
      <lcm-switch
        label=${t('settings.aptCache.host.permanent')}
        on-label=${t('settings.more.switch.on')}
        off-label=${t('settings.more.switch.off')}
        on-text=${t('settings.aptCache.host.permanentOn')}
        off-text=${t('settings.aptCache.host.permanentOff')}
        .checked=${overview.permanent_cache}
        ?disabled=${!canWrite || this.#pendingJobs > 0}
        @lcm-change=${(e: CustomEvent<{ checked: boolean }>) => {
          (e.target as HTMLElement & { checked: boolean }).checked = overview.permanent_cache;
          this._confirm = { kind: 'permanent', enable: e.detail.checked };
        }}
      ></lcm-switch>
      ${overview.disk_usage ? html`<dl class="facts"><dt>${t('settings.aptCache.host.disk')}</dt><dd>${overview.disk_usage}</dd></dl>` : nothing}
    </lcm-card>`;
  }

  #dialog() {
    const confirm = this._confirm;
    let heading = '';
    let message = '';
    let label = '';
    let impacts: Impact[] = [];
    let danger = false;
    if (confirm?.kind === 'delete') {
      heading = t('settings.aptCache.delete.title', { name: confirm.cache.name });
      message = t('settings.aptCache.delete.message');
      label = t('settings.aptCache.delete.confirm');
      danger = true;
      impacts = [{ label: t('settings.aptCache.delete.servers'), value: t('settings.aptCache.delete.serversValue'), tone: 'ok' }];
    } else if (confirm?.kind === 'disconnect') {
      heading = t('settings.aptCache.disconnect.title', { name: confirm.server.name });
      message = t('settings.aptCache.disconnect.message');
      label = t('settings.aptCache.disconnect.confirm');
      impacts = [
        { label: t('settings.more.impact.servers'), value: confirm.server.name },
        { label: t('settings.aptCache.disconnect.policy'), value: t('settings.aptCache.disconnect.policyValue'), tone: 'warn' },
        { label: t('settings.more.impact.reversible'), value: t('settings.more.impact.yes'), tone: 'ok' },
      ];
    } else if (confirm?.kind === 'restart') {
      heading = t('settings.aptCache.host.restartTitle');
      message = t('settings.aptCache.host.restartMessage');
      label = t('settings.aptCache.host.restart');
      impacts = [
        { label: t('settings.aptCache.host.downtime'), value: t('settings.aptCache.host.downtimeValue'), tone: 'warn' },
        { label: t('settings.more.impact.reversible'), value: t('settings.more.impact.yes'), tone: 'ok' },
      ];
    } else if (confirm?.kind === 'permanent') {
      heading = t(confirm.enable ? 'settings.aptCache.host.permanentEnableTitle' : 'settings.aptCache.host.permanentDisableTitle');
      message = t(confirm.enable ? 'settings.aptCache.host.permanentOn' : 'settings.aptCache.host.permanentOff');
      label = t('settings.aptCache.host.permanentConfirm');
      impacts = [
        { label: t('settings.aptCache.host.runsOn'), value: t('settings.aptCache.host.runsOnValue') },
        { label: t('settings.more.impact.reversible'), value: t('settings.more.impact.yes'), tone: 'ok' },
      ];
    }
    return html`<lcm-dialog
      ?open=${confirm !== null}
      heading=${heading}
      message=${message}
      .impacts=${impacts}
      confirm-label=${label}
      cancel-label=${t('settings.more.modal.cancel')}
      tone=${danger ? 'danger' : 'default'}
      error=${this._dialogError}
      ?busy=${this._busy}
      @lcm-confirm=${() => void this.#confirmAction()}
      @lcm-cancel=${() => (this._confirm = null)}
    ></lcm-dialog>`;
  }

  #modalView() {
    const form = this.#modal.form;
    const errors = form ? validateCacheForm(form) : {};
    const editing = this.#modal.editId !== null;
    return html`<lcm-settings-modal
      ?open=${form !== null}
      heading=${t(editing ? 'settings.aptCache.form.editTitle' : 'settings.aptCache.form.newTitle')}
      save-label=${t(editing ? 'settings.more.modal.save' : 'settings.more.modal.create')}
      ?dirty=${this.#modal.dirty}
      ?busy=${this.#modal.busy}
      error=${generalError(this.#modal.error)}
      @lcm-submit=${() => void this.#save()}
      @lcm-close=${() => this.#modal.close()}
    >
      ${form
        ? html`<lcm-input
              label=${t('settings.aptCache.form.name')}
              placeholder=${t('settings.aptCache.form.namePlaceholder')}
              maxlength=${LIMITS.name}
              error=${this.#modal.fieldText(errors, 'name', LIMITS.name)}
              .value=${form.name}
              @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#modal.set({ name: e.detail.value })}
            ></lcm-input>
            <lcm-input
              label=${t('settings.aptCache.form.url')}
              width="l"
              mono
              maxlength=${LIMITS.path}
              placeholder=${'http://192.168.1.10:3142'}
              hint=${t('settings.aptCache.form.urlHint')}
              error=${this.#modal.fieldText(errors, 'url')}
              .value=${form.url}
              @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#modal.set({ url: e.detail.value })}
            ></lcm-input>`
        : nothing}
    </lcm-settings-modal>`;
  }

  override render() {
    return html`<div class="main">
        <p class="lead">${t('settings.aptCache.lead')}</p>
        ${this.#cachesCard()} ${this.#statsCard()} ${this.#connectedCard()} ${this.#hostCard()}
        <lcm-settings-monitor type="apt_cacher_down" intro=${t('settings.aptCache.monitorIntro')} rule-name=${t('settings.aptCache.monitorRule')}></lcm-settings-monitor>
        <p class="callout">${t('settings.aptCache.otherCaches')} <a href="/security/caches">${t('settings.aptCache.otherCachesLink')}</a></p>
      </div>
      ${this.#modalView()} ${this.#dialog()}`;
  }
}
customElements.define('lcm-settings-apt-cache', SettingsAptCache);
