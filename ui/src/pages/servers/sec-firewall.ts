import { LitElement, css, html, nothing, type TemplateResult } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-button';
import '../../components/lcm-dialog';
import '../../components/lcm-input';
import '../../components/lcm-modal';
import '../../components/lcm-notice';
import '../../components/lcm-skeleton';
import type { Impact } from '../../components/lcm-dialog';
import { toast } from '../../components/lcm-toaster';
import { baseStyles } from '../../components/shared';
import { IP_ALLOWLISTS_URL, normalizeAllowlists, type IpAllowlist } from '../../api/groups';
import { dockerUrl, normaliseDocker } from '../../api/server-data';
import { normaliseServerSecurity, publishedPorts, securityApi, serverDetailUrl, splitList, type ListeningPort, type ServerSecurity } from '../../api/servers-security';
import { I18nController } from '../../lib/controllers';
import { errorText, t } from '../../lib/i18n';
import { LiveData } from '../../lib/live-data';
import { unsaved } from '../../lib/unsaved';
import '../groups/firewall-editor';
import type { SshSources } from '../groups/firewall-editor';
import type { FirewallRow } from '../groups/rule-model';
import {
  hasEditorErrors,
  initialRows,
  initialSshSources,
  listeningSuggestions,
  openPortsSummary,
  ruleFromListening,
  rulesPayload,
  sshLocksOut,
  sshPayload,
  withLcmAddress,
  type SshSourcesForm,
} from './sec-firewall-model';
import type { ActionPreviewData } from '../../api/servers';
import type { ConfigPreviewParams } from '../../api/servers-previews';
import { effectImpacts } from './preview';
import './preview-panel';
import { startJob } from './sec-run';
import { secStyles } from './sec-styles';

type Confirm = '' | 'enable' | 'disable';

/**
 * `<lcm-srv-firewall open server-id server-name>`: der Firewall-Editor eines Servers im großen Fenster (`?modal=firewall`).
 * Die SSH-Freigabe ist fest und lässt sich nur auf Quellen einschränken; dazu die Freigaben, Vorschläge aus den
 * lauschenden Diensten und der Hinweis auf Docker-Ports, an denen die Firewall nichts ändert. Aktivieren, neu anwenden
 * und deaktivieren laufen über eine Bestätigung mit Wirkung und dann als Job.
 */
export class SrvFirewall extends LitElement {
  static override properties = {
    _modalError: { state: true },
    open: { type: Boolean, reflect: true },
    serverId: { type: Number, attribute: 'server-id' },
    serverName: { type: String, attribute: 'server-name' },
    _rows: { state: true },
    _ssh: { state: true },
    _listening: { state: true },
    _showErrors: { state: true },
    _confirm: { state: true },
    _preview: { state: true },
    _busy: { state: true },
    _scanning: { state: true },
  };
  static override styles = [
    baseStyles,
    secStyles,
    css`
      .status {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: var(--space-2);
        margin-block-end: var(--space-4);
      }
      .sources-title {
        margin: 0 0 var(--space-2);
        color: var(--text-1);
        font-size: var(--fs-s);
        font-weight: var(--fw-medium);
      }
      .ssh-port {
        font-family: var(--font-mono);
        font-weight: var(--fw-semibold);
      }
      .footer-start {
        margin-inline-end: auto;
      }
    `,
  ];

  declare open: boolean;
  declare serverId: number;
  declare serverName: string;
  /** Fehler der letzten Aktion im Fenster; steht über dem Fuß. */
  declare _modalError: string;
  declare _rows: FirewallRow[] | undefined;
  declare _ssh: SshSourcesForm;
  declare _listening: ListeningPort[] | undefined;
  declare _showErrors: boolean;
  declare _confirm: Confirm;
  declare _preview: ActionPreviewData | undefined;
  declare _busy: boolean;
  declare _scanning: boolean;

  #base = '';
  readonly #security = new LiveData<ServerSecurity>(this, { url: () => (this.open && this.serverId ? serverDetailUrl(this.serverId) : null), transform: normaliseServerSecurity });
  readonly #allowlists = new LiveData<IpAllowlist[]>(this, { url: () => (this.open ? IP_ALLOWLISTS_URL : null), transform: normalizeAllowlists });
  readonly #docker = new LiveData<number[]>(this, { url: () => (this.open && this.serverId ? dockerUrl(this.serverId) : null), transform: (raw) => publishedPorts(normaliseDocker(raw).containers) });

  constructor() {
    super();
    this._modalError = '';
    this.open = false;
    this.serverId = 0;
    this.serverName = '';
    this._rows = undefined;
    this._ssh = { allowlist_ids: [], source_ips: '' };
    this._listening = undefined;
    this._showErrors = false;
    this._confirm = '';
    this._preview = undefined;
    this._busy = false;
    this._scanning = false;
    new I18nController(this);
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    unsaved.set(this, false);
  }

  override willUpdate(): void {
    if (!this.open) {
      this._rows = undefined;
      this._listening = undefined;
      return;
    }
    const security = this.#security.data;
    if (security && !this._rows) {
      this._rows = initialRows(security);
      this._ssh = initialSshSources(security.sshSources);
      this.#base = this.#snapshot();
    }
  }

  #snapshot(): string {
    return JSON.stringify([this._rows, this._ssh]);
  }

  #edited(): void {
    unsaved.set(this, this.#snapshot() !== this.#base);
  }

  #setRows(rows: FirewallRow[]): void {
    this._rows = rows;
    this.#edited();
  }

  #setSsh(ssh: SshSourcesForm): void {
    this._ssh = ssh;
    this.#edited();
  }

  #close(): void {
    this._modalError = '';
    unsaved.set(this, false);
    this.dispatchEvent(new CustomEvent('lcm-close', { bubbles: true, composed: true }));
  }

  async #rescan(): Promise<void> {
    this._scanning = true;
    this._modalError = '';
    try {
      this._listening = await securityApi.scanListeningPorts(this.serverId);
      toast({ tone: 'ok', title: t('serverSecurity.firewall.rescan.done', { count: this._listening.length }) });
    } catch (error) {
      this._modalError = errorText(error);
    } finally {
      this._scanning = false;
    }
  }

  #askEnable(): void {
    if (hasEditorErrors(this._rows ?? [], this._ssh)) {
      this._showErrors = true;
      return;
    }
    this._preview = undefined;
    this._confirm = 'enable';
  }

  async #apply(security: ServerSecurity, enable: boolean): Promise<void> {
    this._busy = true;
    const label = t(enable ? (security.firewallActive ? 'serverSecurity.firewall.reapply' : 'serverSecurity.firewall.enable') : 'serverSecurity.firewall.disable.label');
    const rows = this._rows ?? [];
    const job = await startJob(this.serverId, this.serverName, label, () =>
      securityApi.configureFirewall(this.serverId, enable, enable ? rulesPayload(rows) : [], enable ? sshPayload(this._ssh) : null),
    );
    this._busy = false;
    this._confirm = '';
    if (job) this.#close();
  }

  // ---- Bestätigung ----

  #sourcesValue(allowlists: IpAllowlist[]): string {
    const names = allowlists.filter((a) => this._ssh.allowlist_ids.includes(a.id)).map((a) => a.name);
    const parts = [...names, ...splitList(this._ssh.source_ips)];
    return parts.length > 0 ? parts.join(', ') : t('serverSecurity.firewall.impact.everywhere');
  }

  #enableImpacts(security: ServerSecurity): Impact[] {
    const docker = this.#dockerPorts(security);
    return [
      { label: t('serverSecurity.firewall.impact.open'), value: openPortsSummary(this._rows ?? [], security.sshPort) },
      { label: t('serverSecurity.firewall.impact.ssh'), value: this.#sourcesValue(this.#allowlists.data ?? []) },
      ...(docker.length > 0 ? [{ label: t('serverSecurity.firewall.impact.docker'), value: t('serverSecurity.firewall.impact.dockerValue', { ports: docker.join(', ') }), tone: 'warn' as const }] : []),
      ...(sshLocksOut(this._ssh, security.lcmSourceIp) ? [{ label: t('serverSecurity.firewall.impact.lcm'), value: t('serverSecurity.firewall.impact.lcmValue', { ip: security.lcmSourceIp }), tone: 'crit' as const }] : []),
    ];
  }

  #previewParams(): ConfigPreviewParams {
    return { rules: rulesPayload(this._rows ?? []), sshSources: sshPayload(this._ssh) };
  }

  #renderConfirm(security: ServerSecurity): TemplateResult {
    const disable = this._confirm === 'disable';
    const heading = disable
      ? t('serverSecurity.firewall.disable.heading', { name: this.serverName })
      : t(security.firewallActive ? 'serverSecurity.firewall.confirm.reapply' : 'serverSecurity.firewall.confirm.enable', { name: this.serverName });
    const impacts: Impact[] = [{ label: t('servers.confirm.server'), value: this.serverName }, ...(disable ? [] : this.#enableImpacts(security)), ...effectImpacts(this._preview)];
    return html`<lcm-dialog
      ?open=${this._confirm !== ''}
      tone=${disable ? 'danger' : 'default'}
      heading=${heading}
      message=${disable ? t('serverSecurity.firewall.disable.message') : t('serverSecurity.firewall.confirm.message')}
      confirm-label=${disable ? t('serverSecurity.firewall.disable.label') : t(security.firewallActive ? 'serverSecurity.firewall.reapply' : 'serverSecurity.firewall.enable')}
      cancel-label=${t('servers.confirm.cancel')}
      .impacts=${impacts}
      ?busy=${this._busy}
      @lcm-cancel=${() => (this._confirm = '')}
      @lcm-confirm=${() => void this.#apply(security, !disable)}
    >
      ${this._confirm !== ''
        ? html`<lcm-preview-panel
            server-id=${this.serverId}
            action=${disable ? 'firewall-disable' : 'firewall-apply'}
            .params=${disable ? {} : this.#previewParams()}
            @lcm-preview=${(e: CustomEvent<ActionPreviewData>) => (this._preview = e.detail)}
          ></lcm-preview-panel>`
        : nothing}
    </lcm-dialog>`;
  }

  // ---- Inhalt ----

  #dockerPorts(security: ServerSecurity): number[] {
    return (this.#docker.data ?? []).filter((port) => port !== security.sshPort);
  }

  /** Unter der Tabelle: LCM-Adresse übernehmen und die Warnung, wenn LCM sich selbst aussperren würde. */
  #renderSshHints(security: ServerSecurity): TemplateResult | typeof nothing {
    const lockout = sshLocksOut(this._ssh, security.lcmSourceIp);
    const restricted = this._ssh.allowlist_ids.length > 0 || this._ssh.source_ips.trim() !== '';
    if (!restricted || !security.lcmSourceIp) return nothing;
    return html`<div class="row">
      ${lockout ? html`<lcm-notice tone="crit">${t('serverSecurity.firewall.ssh.lockout', { ip: security.lcmSourceIp })}</lcm-notice>` : nothing}
      ${lockout
        ? html`<lcm-button size="small" icon="plus" @click=${() => this.#setSsh(withLcmAddress(this._ssh, security.lcmSourceIp))}>${t('serverSecurity.firewall.ssh.addLcm', { ip: security.lcmSourceIp })}</lcm-button>`
        : nothing}
    </div>`;
  }

  #renderListening(security: ServerSecurity, rows: FirewallRow[]): TemplateResult {
    const listening = this._listening ?? security.listening;
    const suggestions = listeningSuggestions(listening, rows, security.sshPort, this.#docker.data ?? []);
    return html`<section class="section">
      <div class="row">
        <h3>${t('serverSecurity.firewall.listening.title')}</h3>
        <lcm-button variant="ghost" size="small" icon="refresh" ?busy=${this._scanning} @click=${() => void this.#rescan()}>${t('serverSecurity.firewall.rescan.label')}</lcm-button>
      </div>
      ${listening.length === 0
        ? html`<p class="muted">${t('serverSecurity.firewall.listening.none')}</p>`
        : suggestions.length === 0
          ? html`<p class="muted">${t('serverSecurity.firewall.listening.covered')}</p>`
          : html`<div class="chips">
              ${suggestions.map(
                (l) => html`<button
                  class="chip"
                  type="button"
                  aria-label=${t('serverSecurity.firewall.listening.add', { port: l.port, proto: l.proto })}
                  @click=${() => this.#setRows([...rows, ruleFromListening(l)])}
                >
                  + <span class="mono">${l.port}/${l.proto}</span>${l.process ? html` · ${l.process}` : nothing}${l.bind && l.bind !== '0.0.0.0' && l.bind !== '::' ? html` <span class="muted mono">(${l.bind})</span>` : nothing}
                </button>`,
              )}
            </div>`}
    </section>`;
  }

  #renderDocker(security: ServerSecurity): TemplateResult | typeof nothing {
    const ports = this.#dockerPorts(security);
    if (ports.length === 0) return nothing;
    return html`<section class="section">
      <lcm-notice tone="warn" heading=${t('serverSecurity.firewall.docker.title', { ports: ports.join(', ') })}>${t('serverSecurity.firewall.docker.hint')}</lcm-notice>
    </section>`;
  }

  #renderBody(security: ServerSecurity): TemplateResult {
    if (security.proxmox) return html`<lcm-notice>${t('serverSecurity.firewall.proxmox')}</lcm-notice>`;
    const rows = this._rows ?? [];
    const allowlists = this.#allowlists.data ?? [];
    return html`<p class="lead">${t('serverSecurity.firewall.lead', { port: security.sshPort })}</p>
      <div class="status">
        <lcm-badge tone=${security.firewallActive ? 'ok' : 'neutral'}>${t(security.firewallActive ? 'serverSecurity.firewall.active' : 'serverSecurity.firewall.inactive')}</lcm-badge>
        <lcm-badge>${t('serverSecurity.firewall.tool', { tool: security.firewallBackend })}</lcm-badge>
        ${security.firewallTool ? nothing : html`<span class="muted">${t('serverSecurity.firewall.willInstall')}</span>`}
      </div>
      <section class="section">
        <lcm-groups-firewall-editor
          .rows=${rows}
          .allowlists=${allowlists}
          .ssh=${{ port: security.sshPort, ...this._ssh }}
          ?show-errors=${this._showErrors}
          @lcm-change=${(e: CustomEvent<{ rows: FirewallRow[] }>) => this.#setRows(e.detail.rows)}
          @lcm-ssh-change=${(e: CustomEvent<{ ssh: SshSources }>) => this.#setSsh({ allowlist_ids: e.detail.ssh.allowlist_ids, source_ips: e.detail.ssh.source_ips })}
        ></lcm-groups-firewall-editor>
        ${this.#renderSshHints(security)}
      </section>
      ${this.#renderListening(security, rows)} ${this.#renderDocker(security)}
      ${this._showErrors && hasEditorErrors(rows, this._ssh) ? html`<lcm-notice slot="note" tone="crit">${t('serverSecurity.firewall.invalid')}</lcm-notice>` : nothing}
      ${this._modalError ? html`<lcm-notice slot="note" tone="crit">${this._modalError}</lcm-notice>` : nothing} ${this.#renderConfirm(security)}`;
  }

  #renderFooter(security: ServerSecurity | undefined): TemplateResult {
    const editable = !!security && !security.proxmox && !!this._rows;
    return html`${editable && security.firewallActive
        ? html`<lcm-button slot="footer" class="footer-start" variant="outline" icon="power" @click=${() => {
              this._preview = undefined;
              this._confirm = 'disable';
            }}>${t('serverSecurity.firewall.disable.label')}</lcm-button>`
        : nothing}
      <lcm-button slot="footer" variant="ghost" @click=${() => this.#close()}>${t('servers.confirm.cancel')}</lcm-button>
      ${editable
        ? html`<lcm-button slot="footer" variant="primary" icon="firewall" @click=${() => this.#askEnable()}>
            ${t(security.firewallActive ? 'serverSecurity.firewall.reapply' : 'serverSecurity.firewall.enable')}
          </lcm-button>`
        : nothing}`;
  }

  override render() {
    const { data, error } = this.#security;
    const body = data
      ? this.#renderBody(data)
      : error
        ? html`<lcm-notice tone="warn">${error}</lcm-notice>`
        : html`<lcm-skeleton shape="block" lines="5" label=${t('servers.loading')}></lcm-skeleton>`;
    return html`<lcm-modal ?open=${this.open} size="xl" heading=${t('serverSecurity.firewall.title', { name: this.serverName })} close-label=${t('servers.confirm.cancel')} @lcm-close=${() => this.#close()}>
      ${body} ${this.#renderFooter(data)}
    </lcm-modal>`;
  }
}
customElements.define('lcm-srv-firewall', SrvFirewall);
