import { LitElement, css, html, nothing, type TemplateResult } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-card';
import '../../components/lcm-empty-state';
import '../../components/lcm-icon';
import '../../components/lcm-notice';
import '../../components/lcm-skeleton';
import { baseStyles, utilStyles } from '../../components/shared';
import { deepScanReportUrl, groupByCategory, normaliseReportFindings, type OpsFinding, type ScanReport } from '../../api/servers-ops';
import { I18nController, SubscribeController } from '../../lib/controllers';
import { formatDateTime } from '../../lib/format';
import { t } from '../../lib/i18n';
import { LiveData } from '../../lib/live-data';
import { router } from '../../lib/router';
import { opsStyles } from './ops-styles';

const SEVERITY: Record<string, { key: string; tone: 'crit' | 'warn' | 'neutral' }> = {
  critical: { key: 'servers.deepScan.severity.critical', tone: 'crit' },
  warning: { key: 'servers.deepScan.severity.warning', tone: 'warn' },
  info: { key: 'servers.deepScan.severity.info', tone: 'neutral' },
};

const CATEGORY: Record<string, string> = {
  kernel: 'serverOps.deepScan.category.kernel',
  restart: 'serverOps.deepScan.category.restart',
  misconfig: 'serverOps.deepScan.category.misconfig',
};

const severity = (value: string) => SEVERITY[value] ?? SEVERITY.info!;
/** Bereich eines Befunds in Worten; Unbekanntes bleibt, wie das Backend es nennt. */
export const category = (value: string) => (CATEGORY[value] ? t(CATEGORY[value]) : value || '–');

/**
 * `<lcm-ops-scan-reports server-id .reports>`: der Verlauf der Deep-Scan-Läufe, neuester zuerst, mit neu/behoben
 * gegenüber dem vorherigen Lauf. Ein Lauf klappt auf (`?report=<id>`) und lädt erst dann seine Befunde.
 */
export class OpsScanReports extends LitElement {
  static override properties = {
    serverId: { type: Number, attribute: 'server-id' },
    reports: { attribute: false },
  };
  static override styles = [
    baseStyles,
    utilStyles,
    opsStyles,
    css`
      ol {
        margin: 0;
        padding: 0;
        list-style: none;
      }
      ol > li + li {
        border-block-start: 1px solid var(--line);
      }
      button.head {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: var(--space-2) var(--space-3);
        inline-size: 100%;
        padding: var(--space-3) var(--space-2);
        border: 0;
        border-radius: var(--radius-s);
        background: none;
        color: var(--text-1);
        font: inherit;
        text-align: start;
        cursor: pointer;
      }
      button.head:hover {
        background: var(--surface-1);
      }
      button.head:focus-visible {
        outline: 2px solid var(--focus-ring);
        outline-offset: 1px;
      }
      .chevron {
        color: var(--text-2);
        transition: transform var(--dur-2) var(--ease);
      }
      [aria-expanded='true'] .chevron {
        transform: rotate(90deg);
      }
      .when {
        font-weight: var(--fw-semibold);
        font-variant-numeric: tabular-nums;
      }
      .body {
        display: flex;
        flex-direction: column;
        gap: var(--space-3);
        padding: 0 var(--space-2) var(--space-4) var(--space-6);
      }
      h4 {
        margin: 0 0 var(--space-2);
        color: var(--text-2);
        font-size: var(--fs-xs);
        font-weight: var(--fw-semibold);
        letter-spacing: var(--label-spacing);
        text-transform: var(--label-transform);
      }
      ul.findings {
        display: flex;
        flex-direction: column;
        gap: var(--space-2);
        margin: 0;
        padding: 0;
        list-style: none;
      }
      .muted {
        margin: 0;
         font-size: var(--fs-s);
      }
    `,
  ];

  declare serverId: number;
  declare reports: ScanReport[];

  readonly #detail = new LiveData<OpsFinding[]>(this, { url: () => (this.#open ? deepScanReportUrl(this.serverId, this.#open) : null), transform: normaliseReportFindings });

  constructor() {
    super();
    this.serverId = 0;
    this.reports = [];
    new I18nController(this);
    new SubscribeController(this, (listener) => router.onChange(listener));
  }

  get #open(): string {
    const id = router.state.query.get('report') ?? '';
    return this.reports.some((r) => r.id === id) ? id : '';
  }

  #toggle(id: string): void {
    router.setQuery({ report: this.#open === id ? null : id }, { replace: true });
  }

  #badges(report: ScanReport, latest: boolean): TemplateResult {
    const total = report.critical + report.warnings + report.infos;
    return html`${latest ? html`<lcm-badge tone="accent">${t('serverOps.deepScan.latest')}</lcm-badge>` : nothing}
    ${report.critical > 0 ? html`<lcm-badge tone="crit">${t('serverOps.deepScan.count.critical', { count: report.critical })}</lcm-badge>` : nothing}
    ${report.warnings > 0 ? html`<lcm-badge tone="warn">${t('serverOps.deepScan.count.warning', { count: report.warnings })}</lcm-badge>` : nothing}
    ${report.infos > 0 ? html`<lcm-badge>${t('serverOps.deepScan.count.info', { count: report.infos })}</lcm-badge>` : nothing}
    ${total === 0 ? html`<lcm-badge tone="ok">${t('serverOps.deepScan.noFindings')}</lcm-badge>` : nothing}
    ${report.newFindings > 0 ? html`<lcm-badge tone="crit">${t('serverOps.deepScan.newCount', { count: report.newFindings })}</lcm-badge>` : nothing}
    ${report.resolvedFindings > 0 ? html`<lcm-badge tone="ok">${t('serverOps.deepScan.resolvedCount', { count: report.resolvedFindings })}</lcm-badge>` : nothing}
    ${report.hardeningIndex !== null ? html`<lcm-badge>${t('serverOps.deepScan.indexShort', { value: report.hardeningIndex })}</lcm-badge>` : nothing}`;
  }

  #findings(): TemplateResult {
    const { data, error } = this.#detail;
    if (!data) {
      return error ? html`<p class="muted">${error}</p>` : html`<lcm-skeleton shape="row" lines="3" label=${t('servers.loading')}></lcm-skeleton>`;
    }
    if (data.length === 0) return html`<p class="muted">${t('serverOps.deepScan.noFindings')}</p>`;
    return html`${groupByCategory(data).map(
      (group) => html`<section>
        <h4>${category(group.category)} · ${group.findings.length}</h4>
        <ul class="findings">
          ${group.findings.map(
            (f) => html`<li>
              <span class="cell"><lcm-badge tone=${severity(f.severity).tone}>${t(severity(f.severity).key)}</lcm-badge>${f.isNew ? html`<lcm-badge tone="info">${t('servers.deepScan.new')}</lcm-badge>` : nothing}<span>${f.title}</span></span>
              ${f.detail ? html`<span class="sub">${f.detail}</span>` : nothing}
            </li>`,
          )}
        </ul>
      </section>`,
    )}`;
  }

  #body(report: ScanReport): TemplateResult {
    return html`<div class="body">
      <p class="muted">${t('serverOps.deepScan.tools', { tools: report.tools.split(',').filter(Boolean).join(', ') || '–' })}${report.kernelRebootPending ? html` · ${t('serverOps.deepScan.rebootThen')}` : nothing}</p>
      ${report.resolvedTitles.length > 0
        ? html`<lcm-notice tone="ok" heading=${t('serverOps.deepScan.resolvedSince')}>
            <ul>
              ${report.resolvedTitles.map((title) => html`<li>${title}</li>`)}
            </ul>
          </lcm-notice>`
        : nothing}
      ${this.#findings()}
    </div>`;
  }

  override render() {
    const open = this.#open;
    return html`<lcm-card heading=${t('serverOps.deepScan.reports')}>
      ${this.reports.length === 0
        ? html`<p class="muted">${t('serverOps.deepScan.noReports')}</p>`
        : html`<ol>
            ${this.reports.map(
              (report, index) => html`<li>
                <button class="head" type="button" aria-expanded=${String(open === report.id)} @click=${() => this.#toggle(report.id)}>
                  <lcm-icon class="chevron" name="chevron-right"></lcm-icon>
                  <span class="when">${formatDateTime(report.createdAt)}</span>
                  ${this.#badges(report, index === 0)}
                </button>
                ${open === report.id ? this.#body(report) : nothing}
              </li>`,
            )}
          </ol>`}
    </lcm-card>`;
  }
}
customElements.define('lcm-ops-scan-reports', OpsScanReports);
