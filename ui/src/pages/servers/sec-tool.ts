import { LitElement, css, html, nothing, type TemplateResult } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-button';
import '../../components/lcm-card';
import '../../components/lcm-data-table';
import '../../components/lcm-dialog';
import '../../components/lcm-input';
import '../../components/lcm-notice';
import '../../components/lcm-skeleton';
import type { Column } from '../../components/lcm-data-table';
import { baseStyles } from '../../components/shared';
import type { IpAllowlist } from '../../api/groups';
import { bansUrl, normaliseBans, securityApi, splitList, TOOL_NAMES, type ManageAction, type ManageInput, type SecurityBan, type SecurityTool } from '../../api/servers-security';
import { I18nController } from '../../lib/controllers';
import { t } from '../../lib/i18n';
import { LIMITS, invalidAddresses } from '../../lib/validate';
import { LiveData } from '../../lib/live-data';
import { watchJob } from '../../lib/job-watch';
import type { ActionPreviewData } from '../../api/servers';
import { effectImpacts } from './preview';
import './preview-panel';
import './set-action';
import { plainLabels } from './srv-tab';
import { startJob } from './sec-run';
import { secStyles } from './sec-styles';

type ServiceAction = Extract<ManageAction, 'start' | 'stop' | 'restart' | 'enable' | 'disable'>;

/** Die Dienst-Aktionen in Anzeigereihenfolge; Stoppen und Autostart aus schwächen den Schutz und fragen deshalb nach. */
const SERVICE_ACTIONS: readonly { id: ServiceAction; label: string; help: string; confirm: boolean }[] = [
  { id: 'start', label: 'serverSecurity.tool.start.label', help: 'serverSecurity.tool.start.help', confirm: false },
  { id: 'restart', label: 'serverSecurity.tool.restart.label', help: 'serverSecurity.tool.restart.help', confirm: false },
  { id: 'stop', label: 'serverSecurity.tool.stop.label', help: 'serverSecurity.tool.stop.help', confirm: true },
  { id: 'enable', label: 'serverSecurity.tool.enable.label', help: 'serverSecurity.tool.enable.help', confirm: false },
  { id: 'disable', label: 'serverSecurity.tool.disable.label', help: 'serverSecurity.tool.disable.help', confirm: true },
];

/**
 * `<lcm-srv-tool tool="crowdsec" server-id server-name lcm-ip active can-write .allowlists>`: ein installiertes
 * Sicherheits-Tool bedienen. Dienst steuern, Allowlist neu schreiben, Sperren ansehen und aufheben, deinstallieren.
 * Alles außer der Sperrliste läuft als Job; solange einer aus dieser Karte läuft, sind ihre Knöpfe gesperrt, danach
 * wird die Sperrliste neu gelesen.
 */
export class SrvTool extends LitElement {
  static override properties = {
    tool: { type: String },
    serverId: { type: Number, attribute: 'server-id' },
    serverName: { type: String, attribute: 'server-name' },
    lcmIp: { type: String, attribute: 'lcm-ip' },
    active: { type: Boolean },
    canWrite: { type: Boolean, attribute: 'can-write' },
    restricted: { type: Boolean },
    allowlists: { attribute: false },
    _ips: { state: true },
    _ids: { state: true },
    _running: { state: true },
    _uninstall: { state: true },
    _preview: { state: true },
    _busy: { state: true },
  };
  static override styles = [
    baseStyles,
    secStyles,
    css`
      :host {
        display: block;
        min-inline-size: 0;
      }
      .head {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: var(--space-2);
      }
      .uninstall {
        margin-inline-start: auto;
      }
    `,
  ];

  declare tool: SecurityTool;
  declare serverId: number;
  declare serverName: string;
  declare lcmIp: string;
  declare active: boolean;
  declare canWrite: boolean;
  declare restricted: boolean;
  declare allowlists: IpAllowlist[];
  declare _ips: string | undefined;
  declare _ids: number[];
  declare _running: boolean;
  declare _uninstall: boolean;
  declare _preview: ActionPreviewData | undefined;
  declare _busy: boolean;

  #stopWatch: () => void = () => {};
  readonly #bans = new LiveData<SecurityBan[]>(this, { url: () => (this.serverId ? bansUrl(this.serverId, this.tool) : null), transform: normaliseBans });

  constructor() {
    super();
    this.tool = 'fail2ban';
    this.serverId = 0;
    this.serverName = '';
    this.lcmIp = '';
    this.active = false;
    this.canWrite = false;
    this.restricted = false;
    this.allowlists = [];
    this._ips = undefined;
    this._ids = [];
    this._running = false;
    this._uninstall = false;
    this._preview = undefined;
    this._busy = false;
    new I18nController(this);
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    this.#stopWatch();
  }

  get #name(): string {
    return TOOL_NAMES[this.tool];
  }

  /** Merkt sich den gestarteten Job: Knöpfe gesperrt, bis er fertig ist, danach die Sperrliste neu. */
  #watch(job: { job_id?: string } | null | undefined): void {
    if (!job?.job_id) return;
    this.#stopWatch();
    this._running = true;
    this.#stopWatch = watchJob(job.job_id, () => {
      this._running = false;
      this.#bans.reload();
    });
  }

  async #manage(action: ManageAction, extra: Partial<ManageInput> = {}, label = ''): Promise<{ job_id?: string } | null> {
    const input: ManageInput = { tool: this.tool, action, ...extra };
    const job = await startJob(this.serverId, this.serverName, label || t('serverSecurity.tool.jobLabel', { tool: this.#name }), () => securityApi.manageTool(this.serverId, input));
    this.#watch(job);
    return job;
  }

  async #uninstall(): Promise<void> {
    this._busy = true;
    const job = await this.#manage('uninstall', {}, t('serverSecurity.tool.uninstall.label'));
    this._busy = false;
    if (job) this._uninstall = false;
  }

  #renderService(): TemplateResult {
    const tool = this.#name;
    return html`<section class="section">
      <h3>${t('serverSecurity.tool.service')}</h3>
      <div class="row">
        ${SERVICE_ACTIONS.map(
          (a) => html`<lcm-set-action
            variant="outline"
            server-id=${this.serverId}
            server-name=${this.serverName}
            action="security-tool-manage"
            .params=${{ tool: { tool: this.tool, action: a.id } }}
            label=${t(a.label)}
            help=${t(a.help, { tool })}
            ?no-confirm=${!a.confirm}
            heading=${t('serverSecurity.tool.confirmHeading', { action: t(a.label), tool, name: this.serverName })}
            ?danger=${a.confirm}
            ?disabled=${this._running}
            .run=${() => securityApi.manageTool(this.serverId, { tool: this.tool, action: a.id })}
            @lcm-done=${(e: CustomEvent<{ job_id: string }>) => this.#watch(e.detail)}
          ></lcm-set-action>`,
        )}
        <lcm-button
          class="uninstall"
          variant="outline"
          icon="trash"
          ?disabled=${this._running}
          @click=${() => {
            this._preview = undefined;
            this._uninstall = true;
          }}
          >${t('serverSecurity.tool.uninstall.label')}</lcm-button>
      </div>
    </section>`;
  }

  #toggleList(id: number): void {
    this._ids = this._ids.includes(id) ? this._ids.filter((x) => x !== id) : [...this._ids, id];
  }

  #renderAllowlist(): TemplateResult {
    const ips = this._ips ?? this.lcmIp;
    const bad = invalidAddresses(ips);
    return html`<section class="section">
      <h3>${t('serverSecurity.tool.allowlist.title')}</h3>
      ${this.allowlists.length > 0
        ? html`<div class="chips" role="group" aria-label=${t('groups.firewall.allowlists')}>
            ${this.allowlists.map((list) => html`<button class="chip" type="button" aria-pressed=${String(this._ids.includes(list.id))} @click=${() => this.#toggleList(list.id)}>${list.name}</button>`)}
          </div>`
        : nothing}
      <div class="row end">
        <lcm-input
          label=${t('serverSecurity.tool.allowlist.ips')}
          width="l"
          mono
          maxlength=${LIMITS.list}
          placeholder="203.0.113.7 198.51.100.0/24"
          .value=${ips}
          error=${bad.length > 0 ? t('check.addresses', { list: bad.slice(0, 3).join(', ') }) : ''}
          @lcm-input=${(e: CustomEvent<{ value: string }>) => (this._ips = e.detail.value)}
        ></lcm-input>
        <lcm-set-action
          no-confirm
          variant="outline"
          icon="check"
          server-id=${this.serverId}
          server-name=${this.serverName}
          action="security-tool-manage"
          .params=${{ tool: { tool: this.tool, action: 'allowlist', allowlist_ips: splitList(ips), allowlist_ids: [...this._ids] } }}
          label=${t('serverSecurity.tool.allowlist.apply')}
          help=${t('serverSecurity.tool.allowlist.help')}
          ?disabled=${this._running || bad.length > 0}
          .run=${() => securityApi.manageTool(this.serverId, { tool: this.tool, action: 'allowlist', allowlist_ips: splitList(ips), allowlist_ids: [...this._ids] })}
          @lcm-done=${(e: CustomEvent<{ job_id: string }>) => this.#watch(e.detail)}
        ></lcm-set-action>
      </div>
    </section>`;
  }

  get #banColumns(): Column<SecurityBan>[] {
    return [
      { key: 'ip', label: t('serverSecurity.tool.bans.ip'), kind: 'mono', sortable: true },
      { key: 'scope', label: this.tool === 'crowdsec' ? t('serverSecurity.tool.bans.scenario') : t('serverSecurity.tool.bans.jail'), sortable: true, render: (r) => html`${r.scope || '–'}${r.cause ? html` <span class="muted">· ${r.cause}</span>` : nothing}` },
      ...(this.tool === 'crowdsec' ? [{ key: 'since', label: t('serverSecurity.tool.bans.remaining'), render: (r: SecurityBan) => r.since || '–' }] : []),
      ...(this.canWrite && !this.restricted
        ? [
            {
              key: 'unban',
              label: t('serverSecurity.tool.bans.actions'),
              align: 'end' as const,
              render: (r: SecurityBan) =>
                html`<lcm-button size="small" variant="ghost" icon="undo" ?disabled=${this._running} label=${t('serverSecurity.tool.bans.unbanAria', { ip: r.ip })} @click=${() => void this.#manage('unban', { unban_ip: r.ip }, t('serverSecurity.tool.bans.unbanJob', { ip: r.ip }))}>
                  ${t('serverSecurity.tool.bans.unban')}
                </lcm-button>`,
            },
          ]
        : []),
    ];
  }

  #renderBans(): TemplateResult {
    const { data, error, loading } = this.#bans;
    const body = error
      ? html`<lcm-notice tone="warn">${t('serverSecurity.tool.bans.error', { error })}</lcm-notice>`
      : data === undefined
        ? html`<lcm-skeleton shape="row" lines="2" label=${t('servers.loading')}></lcm-skeleton>`
        : data.length === 0
          ? html`<p class="muted">${t('serverSecurity.tool.bans.none')}</p>`
          : html`<lcm-data-table no-toolbar density="compact" .columns=${this.#banColumns} .rows=${data} .labels=${plainLabels(t('serverSecurity.tool.bans.title'), t('serverSecurity.tool.bans.none'))}></lcm-data-table>`;
    return html`<section class="section">
      <div class="row">
        <h3>${t('serverSecurity.tool.bans.title')}</h3>
        <lcm-button variant="ghost" size="small" icon="refresh" ?busy=${loading} @click=${() => this.#bans.reload()}>${t('serverSecurity.tool.bans.refresh')}</lcm-button>
      </div>
      ${body}
    </section>`;
  }

  #renderUninstall(): TemplateResult {
    return html`<lcm-dialog
      ?open=${this._uninstall}
      tone="danger"
      heading=${t('serverSecurity.tool.uninstall.heading', { tool: this.#name, name: this.serverName })}
      message=${t('serverSecurity.tool.uninstall.message')}
      confirm-label=${t('serverSecurity.tool.uninstall.label')}
      cancel-label=${t('servers.confirm.cancel')}
      expect=${this.serverName}
      expect-label=${t('serverSecurity.tool.uninstall.expect', { name: this.serverName })}
      .impacts=${[{ label: t('servers.confirm.server'), value: this.serverName }, ...effectImpacts(this._preview)]}
      ?busy=${this._busy}
      @lcm-cancel=${() => (this._uninstall = false)}
      @lcm-confirm=${() => void this.#uninstall()}
    >
      ${this._uninstall
        ? html`<lcm-preview-panel
            server-id=${this.serverId}
            action="security-tool-manage"
            .params=${{ tool: { tool: this.tool, action: 'uninstall' } }}
            @lcm-preview=${(e: CustomEvent<ActionPreviewData>) => (this._preview = e.detail)}
          ></lcm-preview-panel>`
        : nothing}
    </lcm-dialog>`;
  }

  #renderBody(): TemplateResult {
    if (!this.canWrite) return this.#renderBans();
    if (this.restricted) return html`<lcm-notice tone="warn">${t('serverSecurity.restricted')}</lcm-notice>${this.#renderBans()}`;
    return html`${this.#renderService()} ${this.#renderAllowlist()} ${this.#renderBans()} ${this.#renderUninstall()}`;
  }

  override render() {
    return html`<lcm-card heading=${t('serverSecurity.tool.title', { tool: this.#name })}>
      <div slot="actions" class="head">
        ${this._running ? html`<lcm-badge tone="info">${t('serverSecurity.tool.running')}</lcm-badge>` : nothing}
        <lcm-badge tone=${this.active ? 'ok' : 'warn'}>${t(this.active ? 'serverSecurity.tool.active' : 'serverSecurity.tool.inactive')}</lcm-badge>
      </div>
      ${this.#renderBody()}
    </lcm-card>`;
  }
}
customElements.define('lcm-srv-tool', SrvTool);
