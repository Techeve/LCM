import { css, html, nothing, type TemplateResult } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-card';
import '../../components/lcm-data-table';
import '../../components/lcm-empty-state';
import '../../components/lcm-meter';
import '../../components/lcm-notice';
import type { Column } from '../../components/lcm-data-table';
import { baseStyles } from '../../components/shared';
import { serversApi } from '../../api/servers';
import { normaliseOpsDeepScan, opsApi, opsDeepScanUrl, type KernelVuln, type OpsDeepScan, type OpsFinding } from '../../api/servers-ops';
import { formatRelative } from '../../lib/format';
import { t } from '../../lib/i18n';
import { LiveData } from '../../lib/live-data';
import '../security/sec-cell';
import { category } from './ops-scan-reports';
import './srv-action';
import './set-action';
import { opsStyles } from './ops-styles';
import { tabStyles } from './srv-styles';
import { SrvTab, plainLabels, severityLevel, whenLoaded } from './srv-tab';

const tone = (severity: string) => (severity === 'critical' ? 'crit' : severity === 'warning' ? 'warn' : 'neutral');
const label = (severity: string) => t(`servers.deepScan.severity.${severity === 'critical' || severity === 'warning' ? severity : 'info'}`);
const CVE_SEVERITIES = ['critical', 'high', 'medium', 'low'];
const cveSeverity = (value: string) => (CVE_SEVERITIES.includes(value) ? value : 'low');

/**
 * Deep-Scan: Härtungsindex, aktuelle Befunde, kernel-bezogene CVEs und der Verlauf der Läufe mit neu/behoben.
 * Werkzeuge (needrestart, lynis) installieren und den Scan starten laufen als Job.
 */
export class SrvDeepScan extends SrvTab {
  static override styles = [
    baseStyles,
    tabStyles,
    opsStyles,
    css`
      lcm-notice {
        margin-block-end: var(--space-3);
      }
      .index {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: var(--space-3) var(--space-5);
        margin-block-end: var(--space-4);
      }
    `,
  ];

  readonly #scan = new LiveData<OpsDeepScan>(this, { url: () => opsDeepScanUrl(this.server.id), topics: () => [`server:${this.server.id}`], transform: normaliseOpsDeepScan });

  get #columns(): Column<OpsFinding>[] {
    return [
      { key: 'severity', label: t('servers.deepScan.column.severity'), sortable: true, render: (r) => html`<lcm-badge tone=${tone(r.severity)}>${label(r.severity)}</lcm-badge>${r.isNew ? html` <lcm-badge tone="info">${t('servers.deepScan.new')}</lcm-badge>` : nothing}` },
      { key: 'category', label: t('servers.deepScan.column.category'), sortable: true, render: (r) => category(r.category) },
      { key: 'title', label: t('servers.deepScan.column.finding'), render: (r) => html`<strong>${r.title}</strong>` },
      { key: 'detail', label: t('servers.deepScan.column.detail') },
    ];
  }

  get #kernelColumns(): Column<KernelVuln>[] {
    return [
      { key: 'severity', label: t('servers.deepScan.column.severity'), render: (r) => html`<lcm-badge tone=${severityLevel(r.severity) === 'crit' ? 'crit' : 'warn'}>${t(`servers.security.severity.${cveSeverity(r.severity)}`)}</lcm-badge>` },
      { key: 'cve', label: t('serverOps.deepScan.column.cve'), kind: 'mono', render: (r) => (r.url ? html`<lcm-sec-cell href=${r.url} external>${r.cve}</lcm-sec-cell>` : r.cve) },
      { key: 'pkg', label: t('serverOps.deepScan.column.package'), kind: 'mono' },
      { key: 'fixed', label: t('serverOps.deepScan.column.fixed'), kind: 'mono', render: (r) => r.fixed || '–' },
    ];
  }

  #renderActions(): TemplateResult | typeof nothing {
    if (!this.canWrite) return nothing;
    const server = this.server;
    return html`<span class="actions">
      <lcm-set-action
        variant="outline"
        icon="package"
        server-id=${server.id}
        action="deep-scan-tools"
        server-name=${server.name}
        label=${t('serverOps.deepScan.install.label')}
        help=${t('serverOps.deepScan.install.help')}
        meaning=${t('serverOps.deepScan.install.meaning')}
        heading=${t('serverOps.deepScan.install.heading', { name: server.name })}
        .run=${() => opsApi.installDeepScanTools(server.id)}
      ></lcm-set-action>
      <lcm-srv-action
        icon="shield"
        server-id=${server.id}
        server-name=${server.name}
        label=${t('servers.deepScan.run.label')}
        help=${t('servers.deepScan.run.help')}
        meaning=${t('servers.deepScan.run.meaning')}
        confirm-heading=${t('servers.deepScan.run.heading', { name: server.name })}
        done=${t('servers.deepScan.run.done', { name: server.name })}
        .run=${() => serversApi.deepScan(server.id)}
      ></lcm-srv-action>
    </span>`;
  }

  #renderFindings(data: OpsDeepScan): TemplateResult {
    if (!data.scannedAt && !data.scannerAvailable) return html`<lcm-empty-state level="3" icon="shield" heading=${t('servers.deepScan.noScanner.title')} text=${t('servers.deepScan.noScanner.text')}></lcm-empty-state>`;
    if (!data.scannedAt) return html`<lcm-empty-state level="3" icon="shield" heading=${t('servers.deepScan.never.title')} text=${t('servers.deepScan.never.text')}></lcm-empty-state>`;
    const index = data.hardeningIndex;
    return html`${index !== null
        ? html`<div class="index"><span>${t('servers.deepScan.index')}</span><lcm-meter value=${index} label=${t('servers.deepScan.indexLabel', { value: index })} level=${index >= 70 ? 'ok' : index >= 50 ? 'warn' : 'crit'}></lcm-meter></div>`
        : nothing}
      ${data.kernelRebootPending ? html`<lcm-notice tone="warn">${t('serverOps.deepScan.kernelReboot')}</lcm-notice>` : nothing}
      ${data.error ? html`<lcm-notice tone="crit">${data.error}</lcm-notice>` : nothing}
      ${data.findings.length === 0
        ? html`<lcm-empty-state level="3" icon="check" heading=${t('servers.deepScan.clean.title')} text=${t('servers.deepScan.clean.text')}></lcm-empty-state>`
        : html`<lcm-data-table no-toolbar density="compact" .columns=${this.#columns} .rows=${data.findings} .labels=${plainLabels(t('servers.deepScan.caption'), t('servers.deepScan.clean.title'))}></lcm-data-table>`}`;
  }

  #renderKernel(data: OpsDeepScan): TemplateResult | typeof nothing {
    if (!data.scannedAt) return nothing;
    return html`<lcm-card heading=${t('serverOps.deepScan.kernelCves')}>
      ${data.kernelVulns.length === 0
        ? html`<p class="muted">${t('serverOps.deepScan.noKernelCves')}</p>`
        : html`<lcm-data-table no-toolbar density="compact" .columns=${this.#kernelColumns} .rows=${data.kernelVulns} .labels=${plainLabels(t('serverOps.deepScan.kernelCves'), t('serverOps.deepScan.noKernelCves'))}></lcm-data-table>`}
    </lcm-card>`;
  }

  override render() {
    return whenLoaded(
      this.#scan,
      (data) => html`<div class="stack">
        <lcm-card heading=${t('servers.deepScan.title')} subtitle=${data.scannedAt ? t('servers.deepScan.lastScan', { when: formatRelative(data.scannedAt) }) : ''}>
          <div slot="actions">${this.#renderActions()}</div>
          ${this.#renderFindings(data)}
        </lcm-card>
        ${this.#renderKernel(data)}
        <lcm-ops-scan-reports server-id=${this.server.id} .reports=${data.reports}></lcm-ops-scan-reports>
      </div>`,
      5,
    );
  }
}
customElements.define('lcm-srv-deep-scan', SrvDeepScan);
