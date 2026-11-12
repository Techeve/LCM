import { LitElement, css, html, nothing, type TemplateResult } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-card';
import '../../components/lcm-data-table';
import '../../components/lcm-empty-state';
import '../../components/lcm-notice';
import type { Column } from '../../components/lcm-data-table';
import { baseStyles } from '../../components/shared';
import {
  familyLabel,
  hintKey,
  normalisePortScan,
  portScanApi,
  portScanUrl,
  reasonKey,
  serviceName,
  severityKey,
  silentTargets,
  type OpenPort,
  type PortScan,
  type PortSeverity,
} from '../../api/servers-portscan';
import { I18nController } from '../../lib/controllers';
import { formatRelative } from '../../lib/format';
import { t } from '../../lib/i18n';
import { LiveData } from '../../lib/live-data';
import './srv-action';
import { secStyles } from './sec-styles';
import { plainLabels, whenLoaded } from './srv-tab';

type PortRow = OpenPort & { id: number };

const TONES: Record<PortSeverity, 'crit' | 'warn' | 'ok'> = { critical: 'crit', warning: 'warn', ok: 'ok' };
const RANK: Record<PortSeverity, number> = { critical: 0, warning: 1, ok: 2 };

/** Die Zeilen der Tabelle: kritisch zuerst, dann nach Port. */
export function portRows(scan: PortScan): PortRow[] {
  return scan.ports.map((p) => ({ ...p, id: p.port })).sort((a, b) => RANK[a.severity] - RANK[b.severity] || a.port - b.port);
}

/**
 * `<lcm-srv-portscan server-id server-name can-write>`: Portscan von LCM aus. Zeigt die offenen Ports mit Dienst,
 * Bewertung gegen die LCM-Firewall und einem kurzen Hinweis; „Ports prüfen“ startet den Scan als Job.
 */
export class SrvPortScan extends LitElement {
  static override properties = {
    serverId: { type: Number, attribute: 'server-id' },
    serverName: { type: String, attribute: 'server-name' },
    canWrite: { type: Boolean, attribute: 'can-write' },
  };
  static override styles = [
    baseStyles,
    secStyles,
    css`
      :host {
        display: block;
        min-inline-size: 0;
      }
      .summary {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: var(--space-2) var(--space-3);
        margin-block-end: var(--space-3);
      }
      lcm-notice {
        margin-block-end: var(--space-3);
      }
      .targets {
        margin: var(--space-3) 0 0;
      }
    `,
  ];

  declare serverId: number;
  declare serverName: string;
  declare canWrite: boolean;

  readonly #scan = new LiveData<PortScan>(this, { url: () => portScanUrl(this.serverId), topics: () => [`server:${this.serverId}`], transform: normalisePortScan });

  constructor() {
    super();
    this.canWrite = false;
    new I18nController(this);
  }

  get #columns(): Column<PortRow>[] {
    return [
      { key: 'severity', label: t('serverSecurity.portScan.column.rating'), sortable: true, sortValue: (r) => RANK[r.severity], render: (r) => this.#ratingCell(r) },
      { key: 'port', label: t('serverSecurity.portScan.column.port'), kind: 'mono', sortable: true },
      { key: 'service', label: t('serverSecurity.portScan.column.service'), sortable: true, value: (r) => serviceName(r.service), render: (r) => serviceName(r.service) || '–' },
      { key: 'families', label: t('serverSecurity.portScan.column.family'), value: (r) => familyLabel(r.families) },
      { key: 'hint', label: t('serverSecurity.portScan.column.hint'), render: (r) => this.#hintCell(r) },
    ];
  }

  #ratingCell(row: PortRow): TemplateResult {
    return html`<lcm-badge tone=${TONES[row.severity]}>${t(severityKey(row.severity))}</lcm-badge>
      ${row.reasons.map((reason) => html`<span class="muted"> ${t(reasonKey(reason))}</span>`)}`;
  }

  #hintCell(row: PortRow): string {
    const key = hintKey(row.service);
    return key ? t(key) : '';
  }

  #renderAction(): TemplateResult | typeof nothing {
    if (!this.canWrite) return nothing;
    return html`<lcm-srv-action
      slot="actions"
      icon="shield"
      server-id=${this.serverId}
      server-name=${this.serverName}
      label=${t('serverSecurity.portScan.run.label')}
      help=${t('serverSecurity.portScan.run.help')}
      meaning=${t('serverSecurity.portScan.run.meaning')}
      confirm-heading=${t('serverSecurity.portScan.run.heading', { name: this.serverName })}
      done=${t('serverSecurity.portScan.run.done', { name: this.serverName })}
      .run=${() => portScanApi.start(this.serverId)}
    ></lcm-srv-action>`;
  }

  #renderNotices(scan: PortScan): TemplateResult[] {
    const notices = silentTargets(scan.targets).map((address) => html`<lcm-notice tone="warn">${t('serverSecurity.portScan.silent', { address })}</lcm-notice>`);
    if (!scan.firewallCompared) notices.push(html`<lcm-notice>${t('serverSecurity.portScan.noCompare')}</lcm-notice>`);
    else if (!scan.firewallActive && scan.ports.length > 0) notices.push(html`<lcm-notice tone="warn">${t('serverSecurity.portScan.firewallOff')}</lcm-notice>`);
    return notices;
  }

  #renderSummary(scan: PortScan): TemplateResult | typeof nothing {
    if (scan.critical === 0 && scan.warnings === 0) return nothing;
    return html`<div class="summary">
      ${scan.critical > 0 ? html`<lcm-badge tone="crit">${t('serverSecurity.portScan.count.critical', { count: scan.critical })}</lcm-badge>` : nothing}
      ${scan.warnings > 0 ? html`<lcm-badge tone="warn">${t('serverSecurity.portScan.count.warning', { count: scan.warnings })}</lcm-badge>` : nothing}
    </div>`;
  }

  #renderBody(scan: PortScan): TemplateResult {
    if (!scan.scannedAt) return html`<lcm-empty-state level="3" icon="shield" heading=${t('serverSecurity.portScan.never.title')} text=${t('serverSecurity.portScan.never.text')}></lcm-empty-state>`;
    const addresses = scan.targets.map((target) => target.address).join(', ');
    return html`${this.#renderSummary(scan)} ${this.#renderNotices(scan)}
      ${scan.ports.length === 0
        ? html`<lcm-empty-state level="3" icon="check" heading=${t('serverSecurity.portScan.none.title')} text=${t('serverSecurity.portScan.none.text')}></lcm-empty-state>`
        : html`<lcm-data-table no-toolbar density="compact" .columns=${this.#columns} .rows=${portRows(scan)} .labels=${plainLabels(t('serverSecurity.portScan.title'), t('serverSecurity.portScan.none.title'))}></lcm-data-table>`}
      <p class="muted targets">${t('serverSecurity.portScan.targets', { addresses })}</p>`;
  }

  #subtitle(scan: PortScan | undefined): string {
    return scan?.scannedAt ? t('serverSecurity.portScan.lastScan', { when: formatRelative(scan.scannedAt) }) : t('serverSecurity.portScan.subtitle');
  }

  override render() {
    return html`<lcm-card collapsible heading=${t('serverSecurity.portScan.title')} subtitle=${this.#subtitle(this.#scan.data)}>
      ${this.#renderAction()} ${whenLoaded(this.#scan, (scan) => this.#renderBody(scan), 3)}
    </lcm-card>`;
  }
}
customElements.define('lcm-srv-portscan', SrvPortScan);
