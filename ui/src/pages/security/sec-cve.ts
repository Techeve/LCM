import { LitElement, html, nothing, type TemplateResult } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-button';
import '../../components/lcm-data-table';
import '../../components/lcm-empty-state';
import '../../components/lcm-list-pager';
import '../../components/lcm-select';
import '../../components/lcm-skeleton';
import './sec-cell';
import './sec-scanner';
import type { Column } from '../../components/lcm-data-table';
import type { Option } from '../../components/lcm-select';
import { baseStyles } from '../../components/shared';
import { CVE_PAGE_SIZE, SCANNER_URL, SEVERITIES, normaliseCvePage, normaliseScanner, vulnerabilitiesUrl, type CveItem, type CvePage, type ScannerStatus, type Severity } from '../../api/security';
import { auth } from '../../lib/auth';
import { I18nController, SubscribeController } from '../../lib/controllers';
import { formatNumber } from '../../lib/format';
import { t } from '../../lib/i18n';
import { LiveData } from '../../lib/live-data';
import { router } from '../../lib/router';
import { plainLabels, secStyles } from './sec-styles';
import { cvePatch, cveQueryFromUrl, hasCveFilter, pageRange } from './security-state';

const SEVERITY_TEXT: Record<Severity, string> = {
  critical: 'security.severity.critical',
  high: 'security.severity.high',
  medium: 'security.severity.medium',
  low: 'security.severity.low',
  unknown: 'security.severity.unknown',
};

const SUMMARY_TEXT = {
  critical: 'security.cve.summary.critical',
  high: 'security.cve.summary.high',
  medium: 'security.cve.summary.medium',
  low: 'security.cve.summary.low',
} as const;

/** Schwere als Farbton der Marke. */
export function severityTone(severity: Severity): 'crit' | 'warn' | 'neutral' {
  if (severity === 'critical') return 'crit';
  if (severity === 'high' || severity === 'medium') return 'warn';
  return 'neutral';
}

export function severityLabel(severity: Severity): string {
  return t(SEVERITY_TEXT[severity]);
}

/**
 * <lcm-sec-cve> – CVE-Funde (Trivy) über alle sichtbaren Server, kritischste zuerst, mit Stand des Scanners darüber.
 * Quelle und Seite stehen in der Adresse.
 */
export class SecCve extends LitElement {
  static override styles = [
    baseStyles,
    secStyles,
  ];

  readonly #scanner = new LiveData<ScannerStatus>(this, { url: () => SCANNER_URL, transform: normaliseScanner });
  readonly #cves = new LiveData<CvePage>(this, { url: () => vulnerabilitiesUrl(cveQueryFromUrl(router.state.query)), transform: normaliseCvePage });

  constructor() {
    super();
    new I18nController(this);
    new SubscribeController(this, (listener) => router.onChange(listener));
    new SubscribeController(this, (listener) => auth.onChange(listener));
  }

  get #sourceOptions(): Option[] {
    return [
      { value: '', label: t('security.cve.source.all') },
      { value: 'os', label: t('security.cve.source.os') },
      { value: 'docker', label: t('security.cve.source.docker') },
    ];
  }

  get #columns(): Column<CveItem>[] {
    return [
      { key: 'severity', label: t('security.cve.column.severity'), render: (r) => html`<lcm-badge tone=${severityTone(r.severity)}>${severityLabel(r.severity)}</lcm-badge>` },
      { key: 'source', label: t('security.cve.column.source'), render: (r) => this.#sourceCell(r) },
      { key: 'server', label: t('security.cve.column.server'), render: (r) => html`<lcm-sec-cell href=${`/servers/${r.serverId}`} strong>${r.serverName}</lcm-sec-cell>` },
      { key: 'cve', label: t('security.cve.column.cve'), render: (r) => this.#cveCell(r) },
      { key: 'pkg', label: t('security.cve.column.package') },
      { key: 'installed', label: t('security.cve.column.installed'), kind: 'mono' },
      { key: 'fixed', label: t('security.cve.column.fixed'), kind: 'mono', render: (r) => r.fixed || html`<lcm-sec-cell muted>${t('security.cve.noFix')}</lcm-sec-cell>` },
    ];
  }

  #cveCell(item: CveItem): TemplateResult {
    const id = item.url ? html`<lcm-sec-cell href=${item.url} external mono>${item.cve}</lcm-sec-cell>` : html`<lcm-sec-cell mono>${item.cve}</lcm-sec-cell>`;
    return html`${id}${item.title ? html`<lcm-sec-cell block muted title=${item.title}>${item.title}</lcm-sec-cell>` : nothing}`;
  }

  #sourceCell(item: CveItem): TemplateResult {
    if (item.source !== 'docker') return html`<lcm-badge>${t('security.cve.sourceBadge.os')}</lcm-badge>`;
    return html`<lcm-badge tone="info">${t('security.cve.sourceBadge.docker')}</lcm-badge>${item.imageRef ? html`<lcm-sec-cell block muted mono>${item.imageRef}</lcm-sec-cell>` : nothing}`;
  }

  #setQuery(change: Parameters<typeof cvePatch>[0]): void {
    router.setQuery(cvePatch(change));
  }

  #renderSummary(page: CvePage): TemplateResult | typeof nothing {
    const badges = SEVERITIES.filter((severity) => page.summary[severity] > 0).map(
      (severity) => html`<lcm-badge tone=${severityTone(severity)}>${t(SUMMARY_TEXT[severity], { count: page.summary[severity] })}</lcm-badge>`,
    );
    return badges.length > 0 ? html`<div class="summary">${badges}</div>` : nothing;
  }

  #renderToolbar(source: string, filtered: boolean): TemplateResult {
    return html`<div class="toolbar">
      <lcm-select label=${t('security.cve.source.label')} .options=${this.#sourceOptions} .value=${source} @lcm-change=${(e: CustomEvent<{ value: string }>) => this.#setQuery({ source: e.detail.value as 'os' })}></lcm-select>
      ${filtered ? html`<lcm-button size="small" variant="ghost" icon="undo" @click=${() => this.#setQuery({ source: '' })}>${t('security.filter.reset')}</lcm-button>` : nothing}
    </div>`;
  }

  #emptyText(): string {
    const scanner = this.#scanner.data;
    return scanner && (!scanner.available || !scanner.updatedAt) ? t('security.cve.empty.noDb') : t('security.cve.empty.text');
  }

  #renderPager(page: CvePage): TemplateResult | typeof nothing {
    const range = pageRange(page.page, CVE_PAGE_SIZE, page.total);
    if (!range) return nothing;
    return html`<lcm-list-pager
      page=${page.page}
      page-size=${CVE_PAGE_SIZE}
      total=${page.total}
      range-label=${t('security.pager.range', { from: range.from, to: range.to, total: formatNumber(page.total) })}
      previous-label=${t('security.pager.previous')}
      next-label=${t('security.pager.next')}
      @lcm-page=${(e: CustomEvent<{ page: number }>) => this.#setQuery({ page: e.detail.page })}
    ></lcm-list-pager>`;
  }

  #renderList(page: CvePage, filtered: boolean): TemplateResult {
    if (page.items.length === 0 && !filtered) {
      return html`<lcm-empty-state level="2" icon="shield" heading=${t('security.cve.empty.title')} text=${this.#emptyText()}></lcm-empty-state>`;
    }
    return html`<lcm-data-table
        external
        no-toolbar
        .columns=${this.#columns}
        .rows=${page.items}
        .labels=${plainLabels(t('security.cve.caption'), t('security.cve.noMatch'))}
      ></lcm-data-table>
      ${this.#renderPager(page)}`;
  }

  #renderBody(): TemplateResult {
    const { data, error, loading } = this.#cves;
    if (!data) {
      return error
        ? html`<lcm-empty-state level="2" icon="warning" heading=${t('security.error')} text=${error}></lcm-empty-state>`
        : html`<lcm-skeleton shape="row" lines="6" label=${t('security.loading')}></lcm-skeleton>`;
    }
    const query = cveQueryFromUrl(router.state.query);
    const filtered = hasCveFilter(query);
    return html`<div class=${loading ? 'stale' : ''}>
      <div class="stack">${this.#renderSummary(data)} ${this.#renderToolbar(query.source, filtered)} ${this.#renderList(data, filtered)}</div>
    </div>`;
  }

  override render() {
    return html`<div class="stack">
      <lcm-sec-scanner .scanner=${this.#scanner.data} ?can-manage=${auth.can('settings:manage')}></lcm-sec-scanner>
      ${this.#renderBody()}
    </div>`;
  }
}
customElements.define('lcm-sec-cve', SecCve);
