import { css, html, nothing, type TemplateResult } from 'lit';
import '../../components/lcm-ampel';
import '../../components/lcm-badge';
import '../../components/lcm-button';
import '../../components/lcm-card';
import '../../components/lcm-data-table';
import '../../components/lcm-empty-state';
import '../../components/lcm-notice';
import '../../components/lcm-select';
import type { Column } from '../../components/lcm-data-table';
import type { Option } from '../../components/lcm-select';
import { baseStyles } from '../../components/shared';
import { IP_ALLOWLISTS_URL, normalizeAllowlists, type IpAllowlist } from '../../api/groups';
import { serverPath, serversApi } from '../../api/servers';
import { vulnerabilitiesUrl } from '../../api/server-data';
import {
  SECURITY_TOOLS,
  filterVulns,
  normaliseSecurityVulns,
  normaliseServerSecurity,
  parseVulnSource,
  securityApi,
  serverDetailUrl,
  type SecurityVuln,
  type SecurityVulnData,
  type ServerSecurity,
  type VulnSource,
} from '../../api/servers-security';
import { SubscribeController } from '../../lib/controllers';
import { formatRelative } from '../../lib/format';
import { t } from '../../lib/i18n';
import { LiveData } from '../../lib/live-data';
import { router } from '../../lib/router';
import '../security/sec-cell';
import './sec-harden';
import './sec-tool';
import './srv-action';
import './set-action';
import { secStyles } from './sec-styles';
import { tabStyles } from './srv-styles';
import { SrvTab, plainLabels, severityLevel, whenLoaded } from './srv-tab';

const SEVERITIES = ['critical', 'high', 'medium', 'low'] as const;
type Severity = (typeof SEVERITIES)[number];

const known = (value: string): Severity => (SEVERITIES.includes(value as Severity) ? (value as Severity) : 'low');

/**
 * Sicherheit: Schutz des Servers (Firewall, SSH, Sicherheits-Tools) mit dem Firewall-Editor und dem Einrichten von
 * fail2ban, CrowdSec und SSH-2FA, die Verwaltung der installierten Tools, Verzeichnisrechte (ACL, Abschotten) und die
 * Sicherheitslücken (CVE) des letzten Scans, filterbar nach Herkunft (`?cvesrc=os|docker`).
 */
export class SrvSecurity extends SrvTab {
  static override styles = [
    baseStyles,
    tabStyles,
    secStyles,
    css`
      .protection {
        display: grid;
        grid-template-columns: repeat(auto-fill, minmax(15rem, 1fr));
        gap: var(--space-3) var(--space-5);
        margin: 0;
        padding: 0;
        list-style: none;
      }
      .protection li {
        display: flex;
        align-items: center;
        gap: var(--space-2);
      }
      .summary {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: var(--space-2) var(--space-3);
        margin-block-end: var(--space-3);
      }
      .card-actions {
        display: flex;
        flex-wrap: wrap;
        gap: var(--space-2);
      }
      .extra {
        margin: var(--space-4) 0 0;
      }
    `,
  ];

  readonly #vulns = new LiveData<SecurityVulnData>(this, { url: () => vulnerabilitiesUrl(this.server.id), topics: () => [`server:${this.server.id}`], transform: normaliseSecurityVulns });
  readonly #security = new LiveData<ServerSecurity>(this, { url: () => serverDetailUrl(this.server.id), topics: () => [`server:${this.server.id}`], transform: normaliseServerSecurity });
  readonly #allowlists = new LiveData<IpAllowlist[]>(this, { url: () => (this.canWrite ? IP_ALLOWLISTS_URL : null), transform: normalizeAllowlists });

  constructor() {
    super();
    new SubscribeController(this, (listener) => router.onChange(listener));
  }

  get #source(): VulnSource {
    return parseVulnSource(router.state.query.get('cvesrc'));
  }

  get #installOpen(): boolean {
    return router.state.query.get('modal') === 'security-tool';
  }

  // ---- Schutz ----

  #protectionItem(on: boolean, key: string, detail = ''): TemplateResult {
    return html`<li><lcm-ampel level=${on ? 'ok' : 'unknown'} size="m" label=${t(on ? 'servers.security.on' : 'servers.security.off')}></lcm-ampel><span>${t(`servers.security.${key}`)}${detail ? html` <span class="muted mono">${detail}</span>` : nothing}</span></li>`;
  }

  #renderProtectionActions(s: ServerSecurity): TemplateResult | typeof nothing {
    if (!this.canWrite) return nothing;
    return html`<div slot="actions" class="card-actions">
      ${s.proxmox
        ? nothing
        : html`<lcm-button size="small" variant="outline" icon="firewall" @click=${() => router.setQuery({ modal: 'firewall' })}>${t('serverSecurity.protection.firewall')}</lcm-button>`}
      <lcm-button size="small" variant="outline" icon="shield" @click=${() => router.setQuery({ modal: 'security-tool' })}>${t('serverSecurity.protection.install')}</lcm-button>
    </div>`;
  }

  #renderProtection(s: ServerSecurity): TemplateResult {
    return html`<lcm-card heading=${t('servers.security.protection')} subtitle=${t('servers.security.protectionHint')}>
      ${this.#renderProtectionActions(s)}
      <ul class="protection">
        ${this.#protectionItem(s.firewallActive, 'firewall', s.firewallActive ? s.firewallTool : '')}
        ${this.#protectionItem(s.sshHardened, 'sshHardened')} ${this.#protectionItem(s.rootLoginDisabled, 'rootLogin')} ${this.#protectionItem(s.twoFactor, 'twoFactor')}
        ${this.#protectionItem(s.active.fail2ban, 'fail2ban')} ${this.#protectionItem(s.active.crowdsec, 'crowdsec')}
      </ul>
      ${s.firewallActive && s.firewallPorts ? html`<p class="muted extra">${t('servers.security.ports')}: <span class="mono">${s.firewallPorts}</span></p>` : nothing}
      ${s.proxmox ? html`<p class="muted extra">${t('serverSecurity.firewall.proxmox')}</p>` : nothing}
    </lcm-card>`;
  }

  // ---- SSH-2FA und Sicherheits-Tools ----

  #renderTwoFactor(s: ServerSecurity): TemplateResult | typeof nothing {
    if (!s.twoFactor) return nothing;
    const server = this.server;
    const remove = this.canWrite && !s.restrictedSudo;
    return html`<lcm-card heading=${t('serverSecurity.twoFactor.title')}>
      <div slot="actions" class="card-actions"><lcm-badge tone="ok">${t('serverSecurity.tool.active')}</lcm-badge></div>
      <div class="section">
        <p class="muted">${t('serverSecurity.twoFactor.intro')}</p>
        <p>${t('serverSecurity.twoFactor.enroll')} <a href=${serverPath(server.id, 'users')}>${t('servers.tabs.users')}</a></p>
        ${this.canWrite && s.restrictedSudo ? html`<lcm-notice tone="warn">${t('serverSecurity.restricted')}</lcm-notice>` : nothing}
        ${remove
          ? html`<div class="row">
              <lcm-set-action
                variant="outline"
                icon="x"
                danger
                server-id=${server.id}
                action="ssh-2fa-disable"
                server-name=${server.name}
                label=${t('serverSecurity.twoFactor.remove.label')}
                help=${t('serverSecurity.twoFactor.remove.help')}
                meaning=${t('serverSecurity.twoFactor.remove.meaning')}
                heading=${t('serverSecurity.twoFactor.remove.heading', { name: server.name })}
                .run=${() => securityApi.configureSsh2fa(server.id, false)}
              ></lcm-set-action>
            </div>`
          : nothing}
      </div>
    </lcm-card>`;
  }

  #renderTools(s: ServerSecurity): TemplateResult[] {
    const server = this.server;
    return SECURITY_TOOLS.filter((tool) => s.installed[tool]).map(
      (tool) => html`<lcm-srv-tool
        tool=${tool}
        server-id=${server.id}
        server-name=${server.name}
        lcm-ip=${s.lcmSourceIp}
        ?active=${s.active[tool]}
        ?can-write=${this.canWrite}
        .allowlists=${this.#allowlists.data ?? []}
      ></lcm-srv-tool>`,
    );
  }

  #renderInstall(s: ServerSecurity): TemplateResult | typeof nothing {
    if (!this.canWrite) return nothing;
    const open = this.#installOpen;
    if (open) void import('./sec-tool-install');
    return open
      ? html`<lcm-srv-tool-install open server-id=${this.server.id} server-name=${this.server.name} .security=${s} @lcm-close=${() => router.setQuery({ modal: null }, { replace: true })}></lcm-srv-tool-install>`
      : nothing;
  }

  #renderSecurity(s: ServerSecurity): TemplateResult {
    return html`${this.#renderProtection(s)} ${this.#renderTwoFactor(s)} ${this.#renderTools(s)}
      <lcm-srv-harden server-id=${this.server.id} server-name=${this.server.name} ?has-acl=${s.hasAcl} ?acl-usable=${s.aclUsable} ?can-write=${this.canWrite}></lcm-srv-harden>
      ${this.#renderInstall(s)}`;
  }

  // ---- CVE ----

  get #columns(): Column<SecurityVuln>[] {
    return [
      { key: 'severity', label: t('servers.security.column.severity'), sortable: true, sortValue: (r) => SEVERITIES.indexOf(known(r.severity)), render: (r) => html`<lcm-badge tone=${severityLevel(r.severity) === 'crit' ? 'crit' : 'warn'}>${t(`servers.security.severity.${known(r.severity)}`)}</lcm-badge>` },
      { key: 'source', label: t('security.cve.column.source'), sortable: true, render: (r) => this.#sourceCell(r) },
      { key: 'cve', label: 'CVE', sortable: true, render: (r) => this.#cveCell(r) },
      { key: 'pkg', label: t('servers.security.column.package'), sortable: true },
      { key: 'installed', label: t('servers.security.column.installed'), kind: 'mono' },
      { key: 'fixed', label: t('servers.security.column.fixed'), kind: 'mono', render: (r) => r.fixed || t('servers.security.noFix') },
    ];
  }

  #cveCell(item: SecurityVuln): TemplateResult {
    const id = item.url ? html`<lcm-sec-cell href=${item.url} external mono>${item.cve}</lcm-sec-cell>` : html`<lcm-sec-cell mono>${item.cve}</lcm-sec-cell>`;
    return html`${id}${item.title ? html`<lcm-sec-cell block muted title=${item.title}>${item.title}</lcm-sec-cell>` : nothing}`;
  }

  #sourceCell(item: SecurityVuln): TemplateResult {
    if (item.source !== 'docker') return html`<lcm-badge>${t('security.cve.sourceBadge.os')}</lcm-badge>`;
    return html`<lcm-badge tone="info">${t('security.cve.sourceBadge.docker')}</lcm-badge>
      ${item.unused ? html`<lcm-badge title=${t('serverSecurity.cve.unusedHint')}>${t('serverSecurity.cve.unused')}</lcm-badge>` : nothing}
      ${item.imageRef ? html`<lcm-sec-cell block muted mono>${item.imageRef}</lcm-sec-cell>` : nothing}`;
  }

  get #sourceOptions(): Option[] {
    return [
      { value: '', label: t('security.cve.source.all') },
      { value: 'os', label: t('security.cve.source.os') },
      { value: 'docker', label: t('security.cve.source.docker') },
    ];
  }

  #renderScanAction(): TemplateResult | typeof nothing {
    if (!this.canWrite) return nothing;
    const server = this.server;
    return html`<lcm-srv-action
      icon="refresh"
      server-id=${server.id}
      server-name=${server.name}
      label=${t('servers.security.scan.label')}
      help=${t('servers.security.scan.help')}
      meaning=${t('servers.security.scan.meaning')}
      confirm-heading=${t('servers.security.scan.heading', { name: server.name })}
      done=${t('servers.security.scan.done', { name: server.name })}
      .run=${() => serversApi.scanVulnerabilities(server.id)}
    ></lcm-srv-action>`;
  }

  #renderList(data: SecurityVulnData): TemplateResult {
    const source = this.#source;
    if (data.items.length === 0) {
      if (!data.scannerAvailable) return html`<lcm-empty-state level="3" icon="shield" heading=${t('servers.security.noScanner.title')} text=${t('servers.security.noScanner.text')}></lcm-empty-state>`;
      const scanned = data.lastScan !== null;
      return html`<lcm-empty-state level="3" icon="check" heading=${scanned ? t('servers.security.clean.title') : t('servers.security.neverScanned.title')} text=${scanned ? t('servers.security.clean.text') : t('servers.security.neverScanned.text')}></lcm-empty-state>`;
    }
    const rows = filterVulns(data.items, source);
    return html`<div class="toolbar">
        <lcm-select label=${t('security.cve.source.label')} width="m" .options=${this.#sourceOptions} .value=${source} @lcm-change=${(e: CustomEvent<{ value: string }>) => router.setQuery({ cvesrc: e.detail.value || null }, { replace: true })}></lcm-select>
      </div>
      <lcm-data-table no-toolbar density="compact" .columns=${this.#columns} .rows=${rows} .labels=${plainLabels(t('servers.security.caption'), t('security.cve.noMatch'))}></lcm-data-table>`;
  }

  #renderVulns(data: SecurityVulnData, listening: string): TemplateResult {
    const summary = SEVERITIES.filter((s) => (data.summary[s] ?? 0) > 0).map((s) => html`<lcm-badge tone=${severityLevel(s) === 'crit' ? 'crit' : 'warn'}>${t(`servers.security.severityCount.${s}`, { count: data.summary[s] })}</lcm-badge>`);
    const unused = data.unusedCount > 0 ? html`<lcm-badge title=${t('serverSecurity.cve.unusedHint')}>${t('serverSecurity.cve.unusedCount', { count: data.unusedCount })}</lcm-badge>` : nothing;
    return html`<lcm-card heading=${t('servers.security.cves')} subtitle=${data.lastScan ? t('servers.security.lastScan', { when: formatRelative(data.lastScan) }) : ''}>
      <div slot="actions">${this.#renderScanAction()}</div>
      ${summary.length > 0 || data.unusedCount > 0 ? html`<div class="summary">${summary} ${unused}</div>` : nothing}
      ${listening ? html`<p class="muted">${t('serverSecurity.cve.listening')} <span class="mono">${listening}</span></p>` : nothing}
      ${this.server.cve_scan_error ? html`<lcm-notice tone="crit" heading=${t('servers.security.scanFailed')}>${this.server.cve_scan_error}</lcm-notice>` : nothing} ${this.#renderList(data)}
    </lcm-card>`;
  }

  override render() {
    const listening = this.#security.data?.listeningPackages ?? '';
    return html`<div class="stack">
      ${whenLoaded(this.#security, (s) => this.#renderSecurity(s), 2)} ${whenLoaded(this.#vulns, (data) => this.#renderVulns(data, listening), 5)}
    </div>`;
  }
}
customElements.define('lcm-srv-security', SrvSecurity);
