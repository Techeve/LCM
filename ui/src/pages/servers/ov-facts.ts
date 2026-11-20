import { LitElement, css, html, nothing, type TemplateResult } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-button';
import '../../components/lcm-card';
import '../../components/lcm-notice';
import { toast } from '../../components/lcm-toaster';
import { baseStyles, utilStyles, type StatusLevel } from '../../components/shared';
import { isApiDevice, isVirtualDsm, type ServerItem } from '../../api/servers';
import {
  clockState,
  detailApi,
  dnsLevel,
  isPostQuantum,
  packageManagers,
  platformOf,
  splitList,
  summariseExposures,
  supportState,
  type ExposureSummary,
  type ServerStatusDetail,
} from '../../api/server-detail';
import { copyWithToast } from '../../lib/clipboard';
import { I18nController } from '../../lib/controllers';
import { formatBytes, formatDateTime, formatRelative } from '../../lib/format';
import { errorText, t } from '../../lib/i18n';
import { announceJob } from '../../lib/job-toast';
import { insightText } from '../../lib/insight-text';

const MB = 1024 * 1024;
const PROXMOX: Record<string, string> = { pve: 'Proxmox VE', pbs: 'Proxmox Backup Server', pmg: 'Proxmox Mail Gateway', pdm: 'Proxmox Datacenter Manager' };

interface Row {
  label: string;
  value: TemplateResult | string;
}

const DNS_LABEL: Record<'ok' | 'warn' | 'crit', string> = { ok: 'serverDetail.facts.dns.full', warn: 'serverDetail.facts.dns.partial', crit: 'serverDetail.facts.dns.none' };
const CLOCK_LABEL: Record<'ok' | 'ahead' | 'behind', string> = { ok: 'serverDetail.facts.clock.ok', ahead: 'serverDetail.facts.clock.ahead', behind: 'serverDetail.facts.clock.behind' };

const TONE: Record<StatusLevel, 'ok' | 'warn' | 'crit' | 'info' | 'neutral'> = { great: 'ok', ok: 'ok', warn: 'warn', crit: 'crit', running: 'info', unknown: 'neutral' };

/** Kleines Kennzeichen für einen Zustand – so groß wie der Text daneben, keine Pille mit Symbol. */
function pill(level: StatusLevel, label: string, title = ''): TemplateResult {
  return html`<lcm-badge tone=${TONE[level]} title=${title}>${label}</lcm-badge>`;
}

/**
 * `<lcm-srv-facts .server .status can-write>`: die beiden Faktenblöcke der Übersicht, Hardware/Netz und System.
 * Zeilen, die LCM für einen Gerätetyp nicht erhebt (RouterOS, DSM), fallen weg, statt „unbekannt“ zu behaupten.
 */
export class SrvFacts extends LitElement {
  static override properties = {
    server: { attribute: false },
    status: { attribute: false },
    canWrite: { type: Boolean, attribute: 'can-write' },
  };
  static override styles = [
    baseStyles,
    utilStyles,
    css`
      :host {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(min(100%, 24rem), 1fr));
        gap: var(--gap);
        align-items: start;
        min-inline-size: 0;
      }
      dl {
        display: grid;
        grid-template-columns: minmax(9rem, max-content) minmax(0, 1fr);
        gap: var(--space-2) var(--space-5);
        margin: 0;
      }
      dt {
        color: var(--text-2);
      }
      dd {
        display: flex;
        flex-wrap: wrap;
        align-items: baseline;
        gap: var(--space-1) var(--space-2);
        margin: 0;
        min-inline-size: 0;
      }
      .mono {
        overflow-wrap: anywhere;
      }
      .muted {
        font-size: var(--fs-s);
      }
      .num {
        font-variant-numeric: tabular-nums;
      }
      .lines {
        display: flex;
        flex-direction: column;
        gap: var(--space-1);
      }
      .note {
        flex-basis: 100%;
        margin: 0;
        color: var(--text-2);
        font-size: var(--fs-s);
      }
      .warnbox {
        flex-basis: 100%;
        margin: var(--space-1) 0 0;
      }
      .warnbox details {
        margin-block-start: var(--space-1);
      }
      .warnbox summary {
        inline-size: fit-content;
        color: var(--accent-text);
        font-weight: var(--fw-medium);
        cursor: pointer;
      }
      .warnbox ul {
        margin: var(--space-1) 0 0;
        padding-inline-start: var(--space-4);
      }
      .fp {
        flex: 1 1 0;
        min-inline-size: 0;
      }
    `,
  ];

  declare server: ServerItem;
  declare status: ServerStatusDetail | undefined;
  declare canWrite: boolean;

  constructor() {
    super();
    this.canWrite = false;
    this.status = undefined;
    new I18nController(this);
  }

  get #device(): boolean {
    return isApiDevice(this.server);
  }

  // ---- Hardware und Netz ----

  #platform(): string {
    const platform = platformOf(this.server.virtualization);
    if (platform.kind === 'unknown') return t('serverDetail.facts.unknown');
    return platform.kind === 'bare' ? t('serverDetail.facts.platform.bare') : t(platform.kind === 'vm' ? 'serverDetail.facts.platform.vm' : 'serverDetail.facts.platform.container', { name: platform.name });
  }

  #disk(): TemplateResult | string {
    const { disk_total_mb: total, disk_used_mb: used } = this.server;
    if (total <= 0) return t('serverDetail.facts.unknown');
    const percent = Math.round((used / total) * 100);
    return html`<span class="num">${formatBytes(used * MB)} / ${formatBytes(total * MB)}</span><span class="muted">(${percent} %)</span>`;
  }

  #ram(): TemplateResult | string {
    const { mem_total_mb: total, mem_used_mb: used } = this.server;
    if (total <= 0) return t('serverDetail.facts.unknown');
    return html`<span>${formatBytes(total * MB)}</span>${used > 0 ? html`<span class="muted">${t('serverDetail.facts.ramUsed', { used: formatBytes(used * MB) })}</span>` : nothing}`;
  }

  #monoList(value: string): TemplateResult | string {
    const items = splitList(value);
    return items.length ? html`<span class="mono">${items.join(', ')}</span>` : t('serverDetail.facts.unknown');
  }

  #dnsTest(): TemplateResult {
    const s = this.server;
    const level = dnsLevel(s.dns_test_status);
    const result = level === 'unknown'
      ? html`<span class="muted">${t('serverDetail.facts.dnsNever')}</span>`
      : html`${pill(level, t(DNS_LABEL[level]), s.dns_test_detail)}${s.dns_test_at ? html`<span class="muted" title=${formatDateTime(s.dns_test_at)}>${formatRelative(s.dns_test_at)}</span>` : nothing}`;
    return result;
  }

  #clock(): TemplateResult {
    const s = this.server;
    const state = clockState(s);
    const container = platformOf(s.virtualization).kind === 'container';
    const seconds = Math.abs(s.clock_offset_seconds);
    const clock = state === 'unknown'
      ? html`<span class="muted">${t('serverDetail.facts.clock.never')}</span>`
      : pill(state === 'ok' ? 'ok' : 'warn', t(CLOCK_LABEL[state], { seconds }));
    const service = s.ntp_service
      ? html`<lcm-badge title=${s.ntp_servers}>${s.ntp_service} · ${t(s.ntp_synchronized ? 'serverDetail.facts.clock.synced' : 'serverDetail.facts.clock.notSynced')}</lcm-badge>`
      : state !== 'unknown' && !container
        ? html`<lcm-badge tone="warn">${t('serverDetail.facts.clock.noNtp')}</lcm-badge>`
        : nothing;
    return html`${clock}${service}${container ? html`<p class="note">${t('serverDetail.facts.clock.container', { type: s.virtualization })}</p>` : nothing}`;
  }

  /** CPU-Modell mit Kernzahl. VirtualDSM nennt kein Modell, nur seine vCPUs - das ist „virtuell“, nicht „unbekannt“. */
  #cpu(): TemplateResult {
    const s = this.server;
    if (isVirtualDsm(s) && !s.cpu_model) {
      return html`${t('serverDetail.facts.cpuVirtual')}${s.cpu_cores > 0 ? html`<span class="muted">(${t('serverDetail.facts.vcpuCount', { count: s.cpu_cores })})</span>` : nothing}`;
    }
    return html`${s.cpu_model || t('serverDetail.facts.unknown')}${s.cpu_cores > 0 ? html`<span class="muted">(${t('serverDetail.facts.coresCount', { count: s.cpu_cores })})</span>` : nothing}`;
  }

  #hardwareRows(): Row[] {
    const s = this.server;
    const device = this.#device;
    const model = s.hardware_model || s.routerboard_model || s.dsm_model;
    const rows: Array<Row | null> = [
      device ? null : { label: t('serverDetail.facts.platform.label'), value: this.#platform() },
      model ? { label: t('serverDetail.facts.model'), value: model } : null,
      s.cpu_model || s.cpu_cores > 0 || !device || isVirtualDsm(s) ? { label: t('serverDetail.facts.cpu'), value: this.#cpu() } : null,
      { label: t('serverDetail.facts.ram'), value: this.#ram() },
      { label: t('serverDetail.facts.disk'), value: this.#disk() },
      device ? null : { label: t('serverDetail.facts.packageManagers'), value: html`${packageManagers(s).map((name) => html`<lcm-badge>${name}</lcm-badge>`)}` },
      device ? null : { label: t('serverDetail.facts.ips'), value: this.#monoList(s.ip_addresses) },
      device ? null : { label: t('serverDetail.facts.dnsServers'), value: this.#monoList(s.dns_current) },
      device ? null : { label: t('serverDetail.facts.dnsTest'), value: this.#dnsTest() },
      s.timezone || !device ? { label: t('serverDetail.facts.timezone'), value: s.timezone ? html`<span class="mono">${s.timezone}</span>` : t('serverDetail.facts.unknown') } : null,
      s.time_checked_at || s.ntp_service || !device ? { label: t('serverDetail.facts.clock.label'), value: this.#clock() } : null,
    ];
    return rows.filter((row): row is Row => row !== null);
  }

  // ---- System ----

  #os(): TemplateResult {
    const s = this.server;
    const name = [s.os_name, s.os_version].filter(Boolean).join(' ') || t('serverDetail.facts.unknown');
    const channel = s.routeros_channel ? html`<span class="muted">${t('serverDetail.facts.channel', { channel: s.routeros_channel })}</span>` : nothing;
    return html`<span>${name}</span>${channel}`;
  }

  #support(): TemplateResult {
    const support = this.server.os_support;
    const state = supportState(support);
    if (state === 'unknown') return html`<span class="muted">${t('serverDetail.facts.unknown')}</span>`;
    const level: StatusLevel = state === 'supported' ? 'ok' : 'crit';
    const label = state === 'eol' ? t('serverDetail.facts.support.eol') : state === 'eol-soon' ? t('serverDetail.facts.support.eolSoon', { eol: support.eol }) : support.eol ? t('serverDetail.facts.support.until', { eol: support.eol }) : t('serverDetail.facts.support.supported');
    const summary = insightText({ message: support.summary, key: support.summary_key || undefined, params: support.summary_params });
    return html`${pill(level, label)}${support.is_lts ? html`<lcm-badge tone="accent">${t('serverDetail.facts.support.lts')}</lcm-badge>` : nothing}
      ${summary ? html`<p class="note">${summary}</p>` : nothing}`;
  }

  #kernel(): TemplateResult {
    const s = this.server;
    const kernel = this.status?.kernel;
    return html`<span class="mono">${s.kernel_version || t('serverDetail.facts.unknown')}</span>
      ${kernel?.reboot_pending ? html`<lcm-badge tone="warn" title=${t('serverDetail.facts.kernelNewerHint')}>${t('serverDetail.facts.kernelNewer')}</lcm-badge>` : nothing}
      ${kernel && !kernel.managed && kernel.container ? html`<p class="note">${t('serverDetail.facts.kernelFromHost', { type: kernel.container })}</p>` : nothing}`;
  }

  #reboot(): TemplateResult {
    const s = this.server;
    if (!s.reboot_required) return html`${t('serverDetail.facts.no')}`;
    return pill('warn', t('serverDetail.facts.rebootYes'));
  }

  #firewall(): TemplateResult {
    const s = this.server;
    const exposures = s.firewall_active ? (this.status?.exposures ?? []) : [];
    const ports = [String(s.ssh_port), ...splitList(s.firewall_allowed_ports)].join(', ');
    return html`<span>${t(s.firewall_active ? 'serverDetail.facts.firewallOn' : 'serverDetail.facts.firewallOff')}</span>
      ${s.firewall_tool ? html`<lcm-badge>${s.firewall_tool}</lcm-badge>` : nothing}
      ${s.firewall_active ? html`<span class="muted">(${t('serverDetail.facts.ports')}: <span class="mono">${ports}</span>)</span>` : nothing}
      ${exposures.length > 0 ? this.#dockerBypass(summariseExposures(exposures)) : nothing}`;
  }

  /** Eine Zeile mit der Zahl; welche Ports es sind, klappt man bei Bedarf auf – es können Dutzende sein. */
  #dockerBypass(summary: ExposureSummary): TemplateResult {
    const binding =
      summary.unbound === summary.count
        ? t('serverDetail.facts.dockerBypassAll')
        : t('serverDetail.facts.dockerBypassSome', { count: summary.unbound });
    return html`<lcm-notice class="warnbox" tone="warn" heading=${t('serverDetail.facts.dockerBypass', { count: summary.count })}>
      ${summary.unbound > 0 ? binding : nothing}
      <details>
        <summary>${t('serverDetail.facts.dockerBypassShow')}</summary>
        <ul>
          ${summary.groups.map(
            (g) =>
              html`<li>
                ${g.container} ·
                ${g.ports.map(
                  (p, i) =>
                    html`${i ? ', ' : ''}<span class="mono">${p.port}</span>${p.addresses.length
                        ? html` <span class="muted">(${t('serverDetail.facts.dockerBypassOnly')} <span class="mono">${p.addresses.join(', ')}</span>)</span>`
                        : nothing}`,
                )}
              </li>`,
          )}
        </ul>
      </details>
    </lcm-notice>`;
  }

  #securityTools(): TemplateResult {
    const s = this.server;
    const tools = [
      s.crowdsec_installed ? { name: 'CrowdSec', active: s.crowdsec_active } : null,
      s.fail2ban_installed ? { name: 'fail2ban', active: s.fail2ban_active } : null,
    ].filter((tool): tool is { name: string; active: boolean } => tool !== null);
    if (tools.length === 0) return html`<span class="muted">${t('serverDetail.facts.toolsNone')}</span>`;
    return html`${tools.map((tool) => html`<span>${tool.name}</span>${pill(tool.active ? 'ok' : 'warn', t(tool.active ? 'serverDetail.facts.toolActive' : 'serverDetail.facts.toolInactive'))}`)}`;
  }

  #fingerprint(value: string): TemplateResult {
    return html`<span class="mono fp">${value}</span>
      <lcm-button size="small" variant="ghost" icon="copy" icon-only label=${t('serverDetail.facts.copy')} @click=${() => void this.#copy(value)}></lcm-button>`;
  }

  async #copy(value: string): Promise<void> {
    await copyWithToast(value, { copied: t('serverDetail.facts.copied'), failed: t('serverDetail.facts.copyFailed') });
  }

  #deviceUpdate(): TemplateResult | null {
    const s = this.server;
    if (s.os_id === 'routeros') {
      return s.routeros_update_available ? pill('warn', t('serverDetail.facts.updateAvailable', { version: s.routeros_latest_version })) : pill('ok', t('serverDetail.facts.updateCurrent'));
    }
    if (s.os_id === 'dsm') {
      return s.dsm_update_available ? pill('warn', t('serverDetail.facts.updateAvailable', { version: s.dsm_latest_version || '' })) : pill('ok', t('serverDetail.facts.updateCurrent'));
    }
    return null;
  }

  #systemRows(): Row[] {
    const s = this.server;
    const device = this.#device;
    const update = this.#deviceUpdate();
    const proxmox = s.proxmox_type ? `${PROXMOX[s.proxmox_type] ?? 'Proxmox'} ${s.proxmox_version}`.trim() : '';
    const fingerprint = s.os_id === 'dsm' ? s.dsm_cert_fingerprint : s.host_key_fingerprint;
    const rows: Array<Row | null> = [
      { label: t('serverDetail.facts.os'), value: this.#os() },
      proxmox ? { label: t('serverDetail.facts.product'), value: proxmox } : null,
      update ? { label: t('serverDetail.facts.updateState'), value: update } : null,
      device ? null : { label: t('serverDetail.facts.support.label'), value: this.#support() },
      device ? null : { label: t('serverDetail.facts.kernel'), value: this.#kernel() },
      device ? null : { label: t('serverDetail.facts.reboot'), value: this.#reboot() },
      device || s.transport === 'agent' ? null : { label: t('serverDetail.facts.sshHardened'), value: t(s.ssh_hardened ? 'serverDetail.facts.yes' : 'serverDetail.facts.no') },
      s.kex_algorithm
        ? { label: t('serverDetail.facts.kex'), value: html`<span class="mono">${s.kex_algorithm}</span>${isPostQuantum(s.kex_algorithm) ? pill('ok', t('serverDetail.facts.kexPq'), t('serverDetail.facts.kexPqHint')) : pill('unknown', t('serverDetail.facts.kexClassic'), t('serverDetail.facts.kexClassicHint'))}` }
        : null,
      device ? null : { label: t('serverDetail.facts.firewall'), value: this.#firewall() },
      device ? null : { label: t('serverDetail.facts.tools'), value: this.#securityTools() },
      s.os_id === 'dsm' ? { label: t('serverDetail.facts.advisor'), value: s.dsm_security_risks > 0 ? html`${pill('warn', t('serverDetail.facts.advisorRisks', { count: s.dsm_security_risks }))}<span class="muted">${s.dsm_security_summary}</span>` : pill('ok', t('serverDetail.facts.advisorClean')) } : null,
      fingerprint ? { label: t(s.os_id === 'dsm' ? 'serverDetail.facts.certFingerprint' : 'serverDetail.facts.fingerprint'), value: this.#fingerprint(fingerprint) } : null,
    ];
    return rows.filter((row): row is Row => row !== null);
  }

  #list(rows: Row[]): TemplateResult {
    return html`<dl>${rows.map((row) => html`<dt>${row.label}</dt><dd>${row.value}</dd>`)}</dl>`;
  }

  /** Kleiner Kringel oben rechts im Hardware-Block: liest Hardware, Netz und Zeit neu (Job). */
  #refreshHardware(): TemplateResult | typeof nothing {
    if (!this.canWrite) return nothing;
    const label = t('serverDetail.bar.refreshHardware');
    return html`<lcm-button slot="actions" variant="ghost" size="small" icon="refresh" icon-only label=${label} title=${label} @click=${() => void this.#runRefresh()}></lcm-button>`;
  }

  async #runRefresh(): Promise<void> {
    const label = t('serverDetail.bar.refreshHardware');
    try {
      const job = await detailApi.refreshHardware(this.server.id);
      announceJob(label, this.server.name, job.job_id);
    } catch (error) {
      toast({ tone: 'crit', title: t('servers.action.failed', { label }), message: errorText(error) });
    }
  }

  override render() {
    return html`<lcm-card heading=${t('serverDetail.facts.hardwareTitle')}>${this.#refreshHardware()}${this.#list(this.#hardwareRows())}</lcm-card>
      <lcm-card heading=${t('serverDetail.facts.systemTitle')}>${this.#list(this.#systemRows())}</lcm-card>`;
  }
}
customElements.define('lcm-srv-facts', SrvFacts);
