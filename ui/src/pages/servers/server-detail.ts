import { LitElement, css, html, nothing, type TemplateResult } from 'lit';
import { html as staticHtml, unsafeStatic } from 'lit/static-html.js';
import '../../components/lcm-badge';
import '../../components/lcm-button';
import '../../components/lcm-dialog';
import '../../components/lcm-empty-state';
import '../../components/lcm-job-progress';
import '../../components/lcm-notice';
import '../../components/lcm-os';
import '../../components/lcm-skeleton';
import '../../components/lcm-status';
import '../../components/lcm-switch';
import '../../components/lcm-tabs';
import type { Impact } from '../../components/lcm-dialog';
import type { TabItem } from '../../components/lcm-tabs';
import { toast } from '../../components/lcm-toaster';
import { groupStyles } from '../../components/button-styles';
import { baseStyles } from '../../components/shared';
import {
  activeJobUrl,
  hasConsole,
  isApiDevice,
  normaliseActiveJob,
  normaliseServers,
  packagesUrl,
  normalisePackages,
  pendingOf,
  parseTab,
  refreshServer,
  serverPath,
  serverSummaryUrl,
  serversApi,
  visibleTabs,
  type ActiveJob,
  type ServerItem,
  type ServerPackage,
  type ServerTab,
} from '../../api/servers';
import { CONSOLES_URL, detailApi, distinctHostname, normaliseConsoles, type OpenConsole } from '../../api/server-detail';
import { abortJob } from '../../api/jobs';
import { actionFor } from '../dashboard/attention-actions';
import { kindLabel } from '../jobs/job-labels';
import { auth } from '../../lib/auth';
import { I18nController, SubscribeController } from '../../lib/controllers';
import { formatDateTime, formatRelative } from '../../lib/format';
import { errorText, t } from '../../lib/i18n';
import { insightText } from '../../lib/insight-text';
import { LiveData } from '../../lib/live-data';
import { FadeOnChange } from '../../lib/motion';
import { palette } from '../../lib/palette';
import { router } from '../../lib/router';
import { STATUS_LEVEL, statusLabel } from './servers-list';
import './set-action';
import './srv-action';
import './srv-more';
import './srv-settings';

const PROXMOX: Record<string, string> = { pve: 'Proxmox VE', pbs: 'Proxmox Backup Server', pmg: 'Proxmox Mail Gateway', pdm: 'Proxmox Datacenter Manager' };

const TAB_MODULES: Record<ServerTab, () => Promise<unknown>> = {
  overview: () => import('./tab-overview'),
  repositories: () => import('./tab-repositories'),
  packages: () => import('./tab-packages'),
  snaps: () => import('./tab-snaps'),
  containers: () => import('./tab-containers'),
  apps: () => import('./tab-apps'),
  users: () => import('./tab-users'),
  security: () => import('./tab-security'),
  'deep-scan': () => import('./tab-deep-scan'),
  storage: () => import('./tab-storage'),
  jobs: () => import('./tab-jobs'),
  ssh: () => import('./tab-ssh'),
};

const TAB_TAGS: Record<ServerTab, string> = {
  overview: 'lcm-srv-overview',
  repositories: 'lcm-srv-repositories',
  packages: 'lcm-srv-packages',
  snaps: 'lcm-srv-snaps',
  containers: 'lcm-srv-containers',
  apps: 'lcm-srv-apps',
  users: 'lcm-srv-users',
  security: 'lcm-srv-security',
  'deep-scan': 'lcm-srv-deep-scan',
  storage: 'lcm-srv-storage',
  jobs: 'lcm-srv-jobs',
  ssh: 'lcm-srv-ssh',
};

/**
 * Serverdetail: Kopf (Name, Ampel mit Befunden, Fakten), Aktionsleiste und Reiter. Jeder Reiter ist ein eigenes, erst bei
 * Bedarf geladenes Modul. Die Daten kommen live über die Themen `server:<id>`; solange ein Job läuft, steht „belegt“ im Kopf.
 */
export class ServerDetail extends LitElement {
  static override properties = {
    _dialogError: { state: true },
    serverId: { type: Number, attribute: 'server-id' },
    tab: { type: String },
    _loaded: { state: true },
    _failed: { state: true },
    _remove: { state: true },
    _purge: { state: true },
    _busy: { state: true },
    _abort: { state: true },
  };
  static override styles = [
    baseStyles,
    groupStyles,
    css`
      :host {
        display: flex;
        flex-direction: column;
        gap: var(--gap);
        min-inline-size: 0;
      }
      /* Serverwechsel: der bisherige Inhalt tritt zurück, bis der neue da ist, und der neue blendet kurz ein (lib/motion).
         Bei reduzierter Bewegung dimmt es ohne Übergang (baseStyles). */
      :host([switching]) {
        opacity: 0.5;
        transition: opacity var(--dur-1) var(--ease);
      }
      .head {
        display: grid;
        grid-template-columns: auto 1fr;
        gap: 0 var(--space-3);
        align-items: center;
      }
      .head lcm-os {
        grid-row: span 3;
        align-self: start;
        margin-block-start: 0.2rem;
      }
      .title-row {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: var(--space-2) var(--space-3);
      }
      h2 {
        margin: 0;
        font-size: var(--fs-2xl);
        font-weight: var(--head-weight);
        line-height: 1.2;
      }
      .facts {
        margin: 0;
        color: var(--text-2);
      }
      .facts .sep {
        margin-inline: 0.4em;
        color: var(--text-3);
      }
      .seen {
        margin: 0;
        color: var(--text-3);
        font-size: var(--fs-s);
      }
      /* Kompakte Knöpfe wie die kleine Knopfleiste der bisherigen Oberfläche: alles passt auf eine Zeile. */
      .bar {
        --btn-h: 2rem;
        --btn-px: 0.65rem;
        --btn-fs: var(--fs-s);
        margin-block-start: var(--space-3);
        max-inline-size: 100%;
        flex-wrap: wrap;
      }
      .states {
        display: flex;
        flex-direction: column;
        gap: var(--space-2);
      }
      .busy-progress {
        display: block;
        max-inline-size: 18rem;
        margin-block-start: var(--space-1);
      }
      .insights {
        display: flex;
        flex-direction: column;
        gap: var(--space-3);
        margin: 0;
        padding: 0;
        list-style: none;
      }
      .insights li {
        display: flex;
        flex-direction: column;
        gap: var(--space-1);
      }
      .insights p {
        margin: 0;
      }
      .insights .sev {
        font-weight: var(--fw-semibold);
      }
      .insights .sev.crit {
        color: var(--crit-text);
      }
      .insights .sev.warn {
        color: var(--warn-text);
      }
      .insights .sev.info {
        color: var(--info-text);
      }
      .insights lcm-button {
        align-self: flex-start;
      }
      .tab-body {
        min-block-size: 8rem;
      }
      lcm-switch {
        margin-block-start: var(--space-3);
      }
    `,
  ];

  declare serverId: number;
  declare tab: string;
  /** Fehler der bestätigten Aktion; steht im Dialog über dem Fuß. */
  declare _dialogError: string;
  declare _loaded: ReadonlySet<ServerTab>;
  declare _failed: boolean;
  declare _remove: boolean;
  declare _purge: boolean;
  declare _busy: boolean;
  declare _abort: boolean;

  #loading = new Set<ServerTab>();

  readonly #summary = new LiveData<ServerItem | null>(this, {
    url: () => (this.serverId ? serverSummaryUrl(this.serverId) : null),
    topics: () => (this.serverId ? [`server:${this.serverId}`] : []),
    transform: (raw) => normaliseServers(raw).items[0] ?? null,
  });
  readonly #active = new LiveData<ActiveJob | null>(this, {
    url: () => (this.serverId ? activeJobUrl(this.serverId) : null),
    transform: normaliseActiveJob,
  });
  readonly #consoles = new LiveData<OpenConsole[]>(this, {
    url: () => (this.serverId && auth.can('servers:read') ? CONSOLES_URL : null),
    topics: () => (this.serverId ? [`server:${this.serverId}`] : []),
    transform: (raw) => normaliseConsoles(raw, this.serverId),
  });
  readonly #packages = new LiveData<ServerPackage[]>(this, {
    url: () => (this.serverId ? packagesUrl(this.serverId) : null),
    transform: normalisePackages,
  });

  constructor() {
    super();
    this._dialogError = '';
    this.serverId = 0;
    this.tab = '';
    this._loaded = new Set();
    this._failed = false;
    this._remove = false;
    this._purge = false;
    this._busy = false;
    this._abort = false;
    new I18nController(this);
    new SubscribeController(this, (listener) => router.onChange(listener));
    new SubscribeController(this, (listener) => auth.onChange(listener));
    new FadeOnChange(this, () => this.#server?.id, () => this);
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    palette.unregister(this);
  }

  override willUpdate(changed: Map<string, unknown>): void {
    if (changed.has('serverId')) this._loaded = new Set();
    this.#ensureTab();
  }

  override updated(): void {
    this.#registerPalette();
    const shown = this.#server;
    this.toggleAttribute('switching', !!shown && shown.id !== this.serverId);
  }

  get #server(): ServerItem | null {
    return this.#summary.data ?? null;
  }

  get #currentTab(): ServerTab {
    const server = this.#server;
    const tab = parseTab(this.tab);
    return server && !visibleTabs(server, auth.can.bind(auth)).includes(tab) ? 'overview' : tab;
  }

  #ensureTab(): void {
    const tab = this.#currentTab;
    if (this._loaded.has(tab) || this.#loading.has(tab)) return;
    this.#loading.add(tab);
    this._failed = false;
    TAB_MODULES[tab]().then(
      () => (this._loaded = new Set([...this._loaded, tab])),
      () => (this._failed = true),
    ).finally(() => this.#loading.delete(tab));
  }

  #registerPalette(): void {
    const server = this.#server;
    if (!server || !auth.can('servers:write')) return palette.unregister(this);
    const go = (action: string) => () => void router.navigate(`${serverPath(server.id, 'packages')}?do=${action}`);
    palette.register(this, [
      { id: 'server.refresh', label: t('servers.palette.refresh', { name: server.name }), icon: 'refresh', keywords: ['apt update', 'paketliste'], run: go('refresh') },
      { id: 'server.upgrade', label: t('servers.palette.upgrade', { name: server.name }), icon: 'download', keywords: ['upgrade', 'updates', 'einspielen'], run: go('upgrade-all') },
      ...(auth.can('groups:write')
        ? [{ id: 'server.groups', label: t('serverDetail.groups.palette', { name: server.name }), icon: 'users' as const, keywords: ['gruppe', 'gruppen', 'zuordnen'], run: () => void router.navigate(`${serverPath(server.id)}?groups=1`) }]
        : []),
      ...(auth.can('servers:console') && hasConsole(server)
        ? [{ id: 'server.terminal', label: t('servers.palette.terminal', { name: server.name }), icon: 'terminal' as const, keywords: ['shell', 'konsole'], run: () => router.setQuery({ terminal: 1 }) }]
        : []),
    ]);
  }

  #gotoTab(tab: string): void {
    void router.navigate(serverPath(this.serverId, parseTab(tab)));
  }

  // ---- Kopf ----

  #renderInsights(server: ServerItem): TemplateResult {
    if (server.insights.length === 0) return html`<p>${t('servers.insights.none')}</p>`;
    return html`<ul class="insights">
      ${server.insights.map((insight) => {
        const action = actionFor(insight.key);
        const tone = insight.severity === 'critical' ? 'crit' : insight.severity === 'warning' ? 'warn' : 'info';
        return html`<li>
          <span class="sev ${tone}">${t(`servers.insights.severity.${tone}`)}</span>
          <p>${insightText(insight)}</p>
          ${action.tab ? html`<lcm-button size="small" variant="outline" href=${serverPath(server.id, action.tab as ServerTab)}>${t(action.labelKey)}</lcm-button>` : nothing}
        </li>`;
      })}
    </ul>`;
  }

  #renderHeader(server: ServerItem): TemplateResult {
    const os = [server.os_name, server.os_version].filter(Boolean).join(' ');
    const port = server.transport === 'agent' ? '' : `:${server.ssh_port}`;
    return html`<div>
      <div class="head">
      <lcm-os os=${server.os_id || 'linux'} size="l"></lcm-os>
      <div class="title-row">
        <h2>${server.name}</h2>
        <lcm-status level=${STATUS_LEVEL[server.status]} label=${statusLabel(server.status)} variant="pill" insight-label=${t('servers.insights.show')}>
          <div slot="insight">${this.#renderInsights(server)}</div>
        </lcm-status>
        ${server.restricted_sudo ? html`<lcm-badge>${t('servers.restrictedSudo')}</lcm-badge>` : nothing}
        ${server.is_demo ? html`<lcm-badge>${t('servers.demo')}</lcm-badge>` : nothing}
        ${server.is_lcm_host ? html`<lcm-badge tone="accent">${t('serverDetail.lcmHost.title')}</lcm-badge>` : nothing}
        ${server.proxmox_type ? html`<lcm-badge>${t('serverDetail.header.proxmox', { product: PROXMOX[server.proxmox_type] ?? 'Proxmox', version: server.proxmox_version })}</lcm-badge>` : nothing}
        ${this.#renderConsoles()}
      </div>
      <p class="facts">
        ${this.#factLine(server, os, port)}
      </p>
      <p class="seen">
        <span title=${server.last_seen_at ?? ''}>${t('servers.facts.lastSeen', { when: server.last_seen_at ? formatRelative(server.last_seen_at) : t('servers.never') })}</span>
        ${server.transport === 'agent' ? html` · ${t(server.agent_connected ? 'servers.facts.agentOnline' : 'servers.facts.agentOffline')}` : nothing}
      </p>
    </div>
      ${this.#renderBar(server)}
    </div>`;
  }

  /** Eine Zeile: System · Adresse · Dienstbenutzer · Hostname · Kernel (· Agent-Version). */
  #factLine(server: ServerItem, os: string, port: string): TemplateResult[] {
    const parts: string[] = [os, `${server.host}${port}`];
    if (server.transport === 'ssh' && server.service_user) parts.push(server.service_user);
    const hostname = distinctHostname(server);
    if (hostname) parts.push(hostname);
    if (server.kernel_version) parts.push(t('servers.facts.kernel', { version: server.kernel_version }));
    if (server.agent_version) parts.push(t('serverDetail.header.agentVersion', { version: server.agent_version }));
    return parts.filter(Boolean).map((part, i) => html`${i > 0 ? html`<span class="sep">·</span>` : nothing}${part}`);
  }

  #renderConsoles(): TemplateResult | typeof nothing {
    const open = this.#consoles.data ?? [];
    if (open.length === 0) return nothing;
    const who = open.map((c) => t('serverDetail.header.consoleSince', { actor: c.actor, time: formatDateTime(c.since) })).join(', ');
    return html`<lcm-badge tone="info" title=${t('serverDetail.header.consoleOpenTitle', { who })}>${t('serverDetail.header.consoleOpen')}</lcm-badge>`;
  }

  /** Eine Knopfleiste: Terminal, Alles aktualisieren, SSH-Härtung, Firewall, Einstellungen und das Menü „Aktionen“. */
  #renderBar(server: ServerItem): TemplateResult {
    const write = auth.can('servers:write');
    const device = isApiDevice(server);
    const ssh = server.transport !== 'agent' && !device;
    const busy = this.#active.data !== null && this.#active.data !== undefined;
    return html`<div class="bar btn-group" role="group" aria-label=${t('serverDetail.more.label')}>
      ${auth.can('servers:console') && hasConsole(server)
        ? html`<lcm-button icon="terminal" @click=${() => router.setQuery({ terminal: 1 })}>${t('servers.bar.terminal.label')}</lcm-button>`
        : nothing}
      ${write
        ? html`<lcm-srv-action
            no-confirm
            icon="refresh"
            ?disabled=${busy}
            server-id=${server.id}
            server-name=${server.name}
            label=${t('serverDetail.bar.refreshAll')}
            help=${t('serverDetail.bar.refreshAllHelp')}
            .run=${() => serversApi.refreshAll(server.id)}
          ></lcm-srv-action>`
        : nothing}
      ${write && ssh
        ? html`<lcm-set-action
            quiet
            icon=${server.ssh_hardened ? 'undo' : 'shield'}
            ?disabled=${busy}
            server-id=${server.id}
            server-name=${server.name}
            action=${server.ssh_hardened ? 'unharden-ssh' : 'harden-ssh'}
            label=${server.ssh_hardened ? t('serverDetail.bar.unharden') : t('serverDetail.more.harden.label')}
            help=${server.ssh_hardened ? t('serverDetail.more.unharden.hint') : t('serverDetail.more.harden.hint')}
            heading=${t(server.ssh_hardened ? 'serverDetail.bar.unhardenHeading' : 'serverDetail.bar.hardenHeading', { name: server.name })}
            done=${t(server.ssh_hardened ? 'serverDetail.more.unharden.done' : 'serverDetail.more.harden.done', { name: server.name })}
            .run=${() => (server.ssh_hardened ? detailApi.unhardenSsh(server.id) : detailApi.hardenSsh(server.id))}
          ></lcm-set-action>`
        : nothing}
      ${write && !device
        ? html`<lcm-button
            icon="firewall"
            @click=${() => (server.proxmox_type ? void router.navigate(serverPath(server.id, 'security')) : router.setQuery({ modal: 'firewall' }))}
            >${t('servers.bar.firewall.label')}${server.firewall_active ? ' ✓' : ''}</lcm-button
          >`
        : nothing}
      ${write
        ? html`<lcm-button icon="settings" icon-only label=${t('servers.bar.settings.label')} title=${t('servers.bar.settings.label')} @click=${() => router.setQuery({ modal: 'settings' })}></lcm-button>
            <lcm-srv-more .server=${server} .onRemove=${() => (this._remove = true)}></lcm-srv-more>`
        : nothing}
    </div>`;
  }

  /**
   * Zustände am Server als Band unter dem Kopf: offline, handlungsunfähig, Wartung, belegt. Sie bleiben, solange
   * der Zustand dauert, und lassen sich nicht wegklicken. Fällt die Verbindung aus, sagt „offline“ schon alles.
   */
  #renderStates(server: ServerItem): TemplateResult | typeof nothing {
    const write = auth.can('servers:write');
    const states = [
      !server.reachable
        ? html`<lcm-notice variant="band" tone="crit" heading=${t('servers.state.offline')}>${server.last_error || t('servers.state.offlineText')}</lcm-notice>`
        : server.incapable
          ? html`<lcm-notice variant="band" tone="crit" heading=${t('servers.incapable.label')}>${t('servers.state.incapableText')}</lcm-notice>`
          : null,
      server.maintenance
        ? html`<lcm-notice
            variant="band"
            tone="info"
            heading=${t('servers.state.maintenance')}
            action-label=${write ? t('servers.bar.settings.label') : ''}
            @lcm-action=${() => router.setQuery({ modal: 'settings' })}
            >${t('servers.state.maintenanceText')}</lcm-notice
          >`
        : null,
      this.#renderBusy(server),
    ].filter((state) => state !== null && state !== nothing);
    return states.length > 0 ? html`<div class="states">${states}</div>` : nothing;
  }

  #renderBusy(server: ServerItem): TemplateResult | typeof nothing {
    const job = this.#active.data;
    if (!job) return nothing;
    const what = kindLabel(job.type) || job.name;
    return html`<lcm-notice variant="band" tone="warn" heading=${t('servers.busy', { name: server.name, job: what })}>
      ${job.id && auth.can('jobs:read') ? html`<a href=${`/jobs/${job.id}`}>${t('servers.action.showJob')}</a>` : nothing}
      <lcm-job-progress class="busy-progress" state="running" indeterminate label=${what} state-label=${t('servers.busyState')}></lcm-job-progress>
      ${job.id && auth.can('servers:write') ? html`<lcm-button slot="action" size="small" variant="outline" icon="x" @click=${() => (this._abort = true)}>${t('serverDetail.abort.label')}</lcm-button>` : nothing}
      </lcm-notice>
      <lcm-dialog
        ?open=${this._abort}
        tone="danger"
        heading=${t('serverDetail.abort.heading', { job: what })}
        message=${t('serverDetail.abort.message')}
        confirm-label=${t('serverDetail.abort.label')}
        cancel-label=${t('servers.confirm.cancel')}
        .impacts=${[
          { label: t('servers.confirm.server'), value: server.name },
          { label: t('serverDetail.abort.effect'), value: t('serverDetail.abort.effectValue'), tone: 'warn' },
        ]}
        error=${this._dialogError}
        ?busy=${this._busy}
        @lcm-cancel=${() => (this._abort = false)}
        @lcm-confirm=${() => void this.#confirmAbort(server, job.id)}
      ></lcm-dialog>`;
  }

  async #confirmAbort(server: ServerItem, jobId: string): Promise<void> {
    this._busy = true;
    this._dialogError = '';
    try {
      await abortJob(jobId);
      toast({ tone: 'ok', title: t('serverDetail.abort.done') });
      this._abort = false;
      this.#active.reload();
      refreshServer(server.id);
    } catch (error) {
      this._dialogError = errorText(error);
    } finally {
      this._busy = false;
    }
  }

  // ---- Reiter ----

  #tabItems(server: ServerItem): TabItem[] {
    const pending = pendingOf(this.#packages.data ?? []).length || server.updates;
    return visibleTabs(server, auth.can.bind(auth)).map((id) => {
      const item: TabItem = { id, label: t(`servers.tabs.${id}`) };
      if (id === 'packages' && pending > 0) return { ...item, badge: String(pending), badgeTone: 'warn' };
      if (id === 'deep-scan' && server.deep_scan_warnings > 0) return { ...item, badge: String(server.deep_scan_warnings), badgeTone: 'warn' };
      return item;
    });
  }

  #renderTabBody(server: ServerItem, tab: ServerTab): TemplateResult {
    if (this._failed) {
      return html`<lcm-empty-state level="3" icon="warning" heading=${t('app.loadFailed.title')} text=${t('app.loadFailed.text')}>
        <lcm-button slot="action" icon="refresh" @click=${() => this.#ensureTab()}>${t('app.retry')}</lcm-button>
      </lcm-empty-state>`;
    }
    if (!this._loaded.has(tab)) return html`<lcm-skeleton shape="block" lines="4" label=${t('servers.loading')}></lcm-skeleton>`;
    const tag = unsafeStatic(TAB_TAGS[tab]);
    return staticHtml`<${tag} .server=${server} ?can-write=${auth.can('servers:write')}></${tag}>`;
  }

  // ---- Entfernen ----

  #removeImpacts(server: ServerItem): Impact[] {
    return [
      { label: t('servers.confirm.server'), value: server.name },
      { label: t('servers.remove.history'), value: t('servers.remove.historyValue'), tone: 'warn' },
      ...(server.groups.length > 0 ? [{ label: t('servers.remove.groups'), value: server.groups.map((g) => g.name).join(', ') }] : []),
      { label: t('servers.confirm.reversible'), value: t('servers.confirm.no'), tone: 'crit' as const },
    ];
  }

  async #confirmRemove(server: ServerItem): Promise<void> {
    this._busy = true;
    this._dialogError = '';
    try {
      await serversApi.decommission(server.id, this._purge);
      toast({ tone: 'ok', title: t('servers.remove.done', { name: server.name }) });
      this._remove = false;
      refreshServer(server.id);
      void router.navigate('/servers');
    } catch (error) {
      this._dialogError = errorText(error);
    } finally {
      this._busy = false;
    }
  }

  #renderRemoveDialog(server: ServerItem): TemplateResult {
    return html`<lcm-dialog
      ?open=${this._remove}
      tone="danger"
      heading=${t('servers.remove.heading', { name: server.name })}
      message=${t('servers.remove.message')}
      confirm-label=${t('servers.remove.confirmLabel')}
      cancel-label=${t('servers.confirm.cancel')}
      expect=${server.name}
      expect-label=${t('servers.remove.expect', { name: server.name })}
      .impacts=${this.#removeImpacts(server)}
      error=${this._dialogError}
      ?busy=${this._busy}
      @lcm-cancel=${() => (this._remove = false)}
      @lcm-confirm=${() => void this.#confirmRemove(server)}
    >
      <lcm-switch label=${t('servers.remove.purge.label')} on-text=${t('servers.remove.purge.on')} off-text=${t('servers.remove.purge.off')} .checked=${this._purge} @lcm-change=${(e: CustomEvent<{ checked: boolean }>) => (this._purge = e.detail.checked)}></lcm-switch>
    </lcm-dialog>`;
  }

  // ---- Terminal, Firewall und Einstellungen: offen, solange `?terminal=1` bzw. `?modal=firewall|settings` in der Adresse steht ----

  #renderOverlays(server: ServerItem): TemplateResult {
    const query = router.state.query;
    const terminal = query.get('terminal') === '1' && auth.can('servers:console') && hasConsole(server);
    if (terminal) void import('./srv-terminal');
    const firewall = query.get('modal') === 'firewall' && auth.can('servers:write') && !isApiDevice(server);
    if (firewall) void import('./sec-firewall');
    return html`${terminal ? html`<lcm-srv-terminal open server-id=${server.id} server-name=${server.name} @lcm-close=${() => router.setQuery({ terminal: null }, { replace: true })}></lcm-srv-terminal>` : nothing}
      ${firewall ? html`<lcm-srv-firewall open server-id=${server.id} server-name=${server.name} @lcm-close=${() => router.setQuery({ modal: null }, { replace: true })}></lcm-srv-firewall>` : nothing}
      ${auth.can('servers:write') ? html`<lcm-srv-settings ?open=${query.get('modal') === 'settings'} server-id=${server.id} @lcm-close=${() => router.setQuery({ modal: null }, { replace: true })}></lcm-srv-settings>` : nothing}`;
  }

  override render() {
    const { error, loading } = this.#summary;
    const server = this.#server;
    if (!server) {
      if (error) return html`<lcm-empty-state level="3" icon="warning" heading=${t('servers.error')} text=${error}><lcm-button slot="action" icon="refresh" @click=${() => this.#summary.reload()}>${t('servers.retry')}</lcm-button></lcm-empty-state>`;
      if (loading) return html`<lcm-skeleton shape="block" lines="6" label=${t('servers.loading')}></lcm-skeleton>`;
      return html`<lcm-empty-state level="2" icon="server" heading=${t('servers.notFound.title')} text=${t('servers.notFound.text')}><lcm-button slot="action" href="/servers">${t('servers.back')}</lcm-button></lcm-empty-state>`;
    }
    const tab = this.#currentTab;
    return html`${this.#renderHeader(server)} ${this.#renderStates(server)}
      <lcm-tabs label=${t('servers.tabs.label')} .tabs=${this.#tabItems(server)} value=${tab} @lcm-tab-change=${(e: CustomEvent<{ id: string }>) => this.#gotoTab(e.detail.id)}>
        <div class="tab-body">${this.#renderTabBody(server, tab)}</div>
      </lcm-tabs>
      ${auth.can('servers:write') ? this.#renderRemoveDialog(server) : nothing} ${this.#renderOverlays(server)}`;
  }
}
customElements.define('lcm-server-detail', ServerDetail);
