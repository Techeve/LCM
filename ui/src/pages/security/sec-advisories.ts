import { LitElement, css, html, nothing, type TemplateResult } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-button';
import '../../components/lcm-data-table';
import '../../components/lcm-modal';
import '../../components/lcm-empty-state';
import '../../components/lcm-list-pager';
import '../../components/lcm-notice';
import '../../components/lcm-select';
import '../../components/lcm-server-action';
import '../../components/lcm-skeleton';
import '../../components/lcm-switch';
import './sec-cell';
import type { Column } from '../../components/lcm-data-table';
import type { Option } from '../../components/lcm-select';
import { toast } from '../../components/lcm-toaster';
import { baseStyles } from '../../components/shared';
import { ADVISORY_PAGE_SIZE, SEVERITIES, advisoriesUrl, normaliseAdvisoryPage, securityApi, type AdvisoryItem, type AdvisoryPage } from '../../api/security';
import { auth } from '../../lib/auth';
import { I18nController, SubscribeController } from '../../lib/controllers';
import { formatDateTime, formatNumber, formatRelative } from '../../lib/format';
import { errorText, t } from '../../lib/i18n';
import { LiveData } from '../../lib/live-data';
import { router } from '../../lib/router';
import { actionLabels } from '../servers/preview';
import '../servers/srv-action';
import { severityLabel, severityTone } from './sec-cve';
import { jobStartedToast, plainLabels, secStyles } from './sec-styles';
import { advisoryPatch, advisoryQueryFromUrl, hasAdvisoryFilter, openAdvisory, pageRange } from './security-state';

/**
 * Eine lokale Kopie ohne Spiegellauf meldet für jedes Paket „nichts gefunden“. Der Server hält die Frühwarnung
 * deshalb für ausgeschaltet; gemeint ist: noch nie gespiegelt, also nichts geprüft.
 */
function neverMirrored(status: AdvisoryPage['status']): boolean {
  return status.localCopy && !status.mirroredAt;
}

const SUMMARY_TEXT = {
  critical: 'security.cve.summary.critical',
  high: 'security.cve.summary.high',
  medium: 'security.cve.summary.medium',
  low: 'security.cve.summary.low',
} as const;

/**
 * <lcm-sec-advisories> – Frühwarnung: Befunde der Online-Quelle (OSV) zum installierten Paketbestand, die schnelle
 * Spur neben dem täglichen Scan. Filter, Seite und der geöffnete Befund stehen in der Adresse; der Befund öffnet sich
 * als Fenster mit den Aktionen.
 */
export class SecAdvisories extends LitElement {
  static override properties = { _polling: { state: true } };
  static override styles = [
    baseStyles,
    secStyles,
    css`
      .badges {
        display: flex;
        flex-wrap: wrap;
        gap: var(--space-1);
        margin-block-start: var(--space-1);
      }
      .id {
        font-weight: var(--fw-semibold);
      }
      .push {
        margin-inline-start: auto;
      }
      dl {
        display: grid;
        grid-template-columns: max-content 1fr;
        gap: var(--space-3) var(--space-5);
        margin: 0;
      }
      dt {
        color: var(--text-2);
        font-size: var(--fs-s);
      }
      dd {
        margin: 0;
        min-inline-size: 0;
        overflow-wrap: anywhere;
      }
      .actions {
        display: flex;
        flex-wrap: wrap;
        gap: var(--space-2);
        margin-block-start: var(--space-5);
      }
    `,
  ];

  declare _polling: boolean;
  readonly #advisories = new LiveData<AdvisoryPage>(this, { url: () => advisoriesUrl(advisoryQueryFromUrl(router.state.query)), transform: normaliseAdvisoryPage });

  constructor() {
    super();
    this._polling = false;
    new I18nController(this);
    new SubscribeController(this, (listener) => router.onChange(listener));
    new SubscribeController(this, (listener) => auth.onChange(listener));
  }

  get #severityOptions(): Option[] {
    return [
      { value: '', label: t('security.advisories.filter.all') },
      ...SEVERITIES.slice()
        .reverse()
        .map((severity) => ({ value: severity, label: severityLabel(severity) })),
    ];
  }

  #setQuery(change: Parameters<typeof advisoryPatch>[0]): void {
    router.setQuery(advisoryPatch(change));
  }

  async #poll(): Promise<void> {
    this._polling = true;
    try {
      const job = await securityApi.pollAdvisories();
      if (job.job_id) jobStartedToast(t('security.advisories.poll.label'), job.job_id);
      else toast({ tone: 'info', title: t('security.advisories.poll.started') });
    } catch (error) {
      toast({ tone: 'crit', title: t('security.advisories.poll.failed'), message: errorText(error) });
    } finally {
      this._polling = false;
    }
  }

  #open(id: string): void {
    router.setQuery({ advisory: id });
  }

  #close(): void {
    router.setQuery({ advisory: null }, { replace: true });
  }

  get #columns(): Column<AdvisoryItem>[] {
    return [
      { key: 'severity', label: t('security.advisories.column.severity'), render: (r) => this.#severityCell(r) },
      { key: 'age', label: t('security.advisories.column.age'), render: (r) => this.#ageCell(r) },
      { key: 'server', label: t('security.advisories.column.server'), render: (r) => html`<lcm-sec-cell href=${`/servers/${r.serverId}`} strong>${r.serverName}</lcm-sec-cell>` },
      { key: 'advisory', label: t('security.advisories.column.advisory'), render: (r) => html`<lcm-sec-cell strong>${r.advisoryId}</lcm-sec-cell>${r.title ? html`<lcm-sec-cell block muted title=${r.title}>${r.title}</lcm-sec-cell>` : nothing}` },
      { key: 'pkg', label: t('security.advisories.column.package'), render: (r) => html`${r.pkg}<lcm-sec-cell block muted mono>${r.installed || '–'}</lcm-sec-cell>` },
      { key: 'fixed', label: t('security.advisories.column.fixed'), kind: 'mono', render: (r) => r.fixed || html`<lcm-sec-cell muted>${t('security.advisories.noFix')}</lcm-sec-cell>` },
    ];
  }

  #severityCell(item: AdvisoryItem): TemplateResult {
    return html`<lcm-badge tone=${item.malware ? 'crit' : severityTone(item.severity)}>${item.malware ? t('security.advisories.malware') : severityLabel(item.severity)}</lcm-badge>
      ${item.exploited ? html`<lcm-sec-cell block><lcm-badge tone="warn">${t('security.advisories.exploited')}</lcm-badge></lcm-sec-cell>` : nothing}`;
  }

  #ageCell(item: AdvisoryItem): TemplateResult | string {
    if (item.resolvedAt) return html`<lcm-badge>${t('security.advisories.resolved')}</lcm-badge>`;
    return item.firstSeenAt ? html`<span title=${formatDateTime(item.firstSeenAt)}>${formatRelative(item.firstSeenAt)}</span>` : '';
  }

  // ---- Fenster ----

  #detail(item: AdvisoryItem): TemplateResult {
    const row = (label: string, value: unknown) => html`<dt>${label}</dt><dd>${value}</dd>`;
    return html`<dl>
        ${row(t('security.advisories.column.severity'), this.#severityCell(item))}
        ${row(t('security.advisories.column.server'), html`<a href=${`/servers/${item.serverId}`}>${item.serverName}</a>`)}
        ${row(t('security.advisories.column.package'), html`${item.pkg} <span class="muted mono">${item.installed || '–'}</span>`)}
        ${row(t('security.advisories.column.fixed'), item.fixed ? html`<span class="mono">${item.fixed}</span>` : t('security.advisories.noFix'))}
        ${item.title ? row(t('security.advisories.detail.title'), item.title) : nothing}
        ${item.firstSeenAt ? row(t('security.advisories.detail.firstSeen'), formatDateTime(item.firstSeenAt)) : nothing}
        ${item.resolvedAt ? row(t('security.advisories.detail.resolved'), formatDateTime(item.resolvedAt)) : nothing}
        ${item.acknowledgedBy ? row(t('security.advisories.detail.acknowledged'), t('security.advisories.acked', { who: item.acknowledgedBy })) : nothing}
        ${item.url ? row(t('security.advisories.detail.source'), html`<a href=${item.url} target="_blank" rel="noopener noreferrer">${item.advisoryId}</a>`) : nothing}
      </dl>
      ${this.#actions(item)}`;
  }

  #actions(item: AdvisoryItem): TemplateResult | typeof nothing {
    if (item.resolvedAt || !auth.can('servers:write')) return nothing;
    return html`<div class="actions">
      ${item.fixed
        ? html`<lcm-srv-action
            variant="primary"
            icon="download"
            server-id=${item.serverId}
            server-name=${item.serverName}
            action="update"
            .params=${{ package: [item.pkg] }}
            label=${t('security.advisories.update.label')}
            help=${t('security.advisories.update.help', { package: item.pkg })}
            meaning=${t('security.advisories.update.meaning')}
            .run=${() => securityApi.updatePackage(item.serverId, item.pkg)}
          ></lcm-srv-action>`
        : html`<lcm-srv-action
            variant="outline"
            icon="lock"
            server-id=${item.serverId}
            server-name=${item.serverName}
            label=${t('security.advisories.freeze.label')}
            help=${t('security.advisories.freeze.help', { package: item.pkg })}
            meaning=${t('security.advisories.freeze.meaning')}
            done=${t('security.advisories.freeze.done', { package: item.pkg })}
            .run=${() => securityApi.freezePackage(item.serverId, item.pkg, t('security.advisories.freeze.note', { id: item.advisoryId }))}
          ></lcm-srv-action>`}
      ${item.acknowledgedBy
        ? nothing
        : html`<lcm-srv-action
            variant="outline"
            icon="check"
            no-confirm
            server-id=${item.serverId}
            server-name=${item.serverName}
            label=${t('security.advisories.ack.label')}
            help=${t('security.advisories.ack.help')}
            meaning=${t('security.advisories.ack.meaning')}
            done=${t('security.advisories.ack.done')}
            .run=${() => securityApi.acknowledge(item.id)}
          ></lcm-srv-action>`}
    </div>`;
  }

  #renderModal(page: AdvisoryPage | undefined): TemplateResult {
    const id = openAdvisory(router.state.query);
    const item = page?.items.find((entry) => entry.id === id);
    return html`<lcm-modal ?open=${item !== undefined} heading=${item?.advisoryId ?? ''} subtitle=${item ? `${item.serverName} · ${item.pkg}` : ''} close-label=${t('security.modal.close')} @lcm-close=${() => this.#close()}>
      ${item ? this.#detail(item) : nothing}
    </lcm-modal>`;
  }

  // ---- Liste ----

  #renderSummary(page: AdvisoryPage): TemplateResult | typeof nothing {
    const badges = SEVERITIES.filter((severity) => page.summary[severity] > 0).map(
      (severity) => html`<lcm-badge tone=${severityTone(severity)}>${t(SUMMARY_TEXT[severity], { count: page.summary[severity] })}</lcm-badge>`,
    );
    return badges.length > 0 ? html`<div class="summary">${badges}</div>` : nothing;
  }

  #renderBanners(page: AdvisoryPage): TemplateResult | typeof nothing {
    const { status } = page;
    const disabled =
      status.enabled || neverMirrored(status)
        ? nothing
        : html`<lcm-notice
            variant="band"
            action-label=${auth.can('settings:manage') ? t('security.advisories.disabledLink') : ''}
            href="/settings/security"
            >${t('security.advisories.disabled')}</lcm-notice
          >`;
    const local = status.localCopy
      ? html`<lcm-notice variant="band">${status.mirroredAt ? t('security.advisories.localState', { when: formatDateTime(status.mirroredAt) }) : t('security.advisories.localNever')}</lcm-notice>`
      : nothing;
    return html`${disabled}${local}`;
  }

  #renderToolbar(page: AdvisoryPage): TemplateResult {
    const query = advisoryQueryFromUrl(router.state.query);
    return html`<div class="toolbar">
      ${auth.can('servers:write')
        ? html`<lcm-server-action
            variant="primary"
            icon="refresh"
            label=${t('security.advisories.poll.label')}
            help=${t('security.advisories.poll.help')}
            meaning=${t('security.advisories.poll.meaning')}
            ?disabled=${!page.status.enabled}
            ?busy=${this._polling}
            .labels=${actionLabels()}
            @click=${() => void this.#poll()}
          ></lcm-server-action>`
        : nothing}
      <lcm-select label=${t('security.advisories.filter.severity')} .options=${this.#severityOptions} .value=${query.minSeverity} @lcm-change=${(e: CustomEvent<{ value: string }>) => this.#setQuery({ minSeverity: e.detail.value as 'high' })}></lcm-select>
      <lcm-switch label=${t('security.advisories.filter.resolved')} .checked=${query.resolved} @lcm-change=${(e: CustomEvent<{ checked: boolean }>) => this.#setQuery({ resolved: e.detail.checked })}></lcm-switch>
      ${hasAdvisoryFilter(query) ? html`<lcm-button size="small" variant="ghost" icon="undo" @click=${() => this.#setQuery({ minSeverity: '', resolved: false })}>${t('security.filter.reset')}</lcm-button>` : nothing}
      <span class="muted push">${page.status.lastPollAt ? t('security.advisories.lastPoll', { when: formatRelative(page.status.lastPollAt) }) : t('security.advisories.neverPolled')}</span>
    </div>`;
  }

  #renderPager(page: AdvisoryPage): TemplateResult | typeof nothing {
    const range = pageRange(page.page, ADVISORY_PAGE_SIZE, page.total);
    if (!range) return nothing;
    return html`<lcm-list-pager
      page=${page.page}
      page-size=${ADVISORY_PAGE_SIZE}
      total=${page.total}
      range-label=${t('security.pager.range', { from: range.from, to: range.to, total: formatNumber(page.total) })}
      previous-label=${t('security.pager.previous')}
      next-label=${t('security.pager.next')}
      @lcm-page=${(e: CustomEvent<{ page: number }>) => this.#setQuery({ page: e.detail.page })}
    ></lcm-list-pager>`;
  }

  #emptyText(status: AdvisoryPage['status']): string {
    if (neverMirrored(status)) return t('security.advisories.empty.neverMirrored');
    return status.enabled ? t('security.advisories.empty.text') : t('security.advisories.empty.disabled');
  }

  #renderList(page: AdvisoryPage): TemplateResult {
    const filtered = hasAdvisoryFilter(advisoryQueryFromUrl(router.state.query));
    if (page.items.length === 0 && !filtered) {
      return html`<lcm-empty-state level="2" icon="bell" heading=${t('security.advisories.empty.title')} text=${this.#emptyText(page.status)}></lcm-empty-state>`;
    }
    return html`<lcm-data-table
        external
        no-toolbar
        .columns=${this.#columns}
        .rows=${page.items}
        .labels=${plainLabels(t('security.advisories.caption'), t('security.advisories.noMatch'))}
        active-id=${openAdvisory(router.state.query)}
        @lcm-row-activate=${(e: CustomEvent<{ id: string }>) => this.#open(e.detail.id)}
      ></lcm-data-table>
      ${this.#renderPager(page)}`;
  }

  #renderBody(): TemplateResult {
    const { data, error, loading } = this.#advisories;
    if (!data) {
      return error
        ? html`<lcm-empty-state level="2" icon="warning" heading=${t('security.error')} text=${error}></lcm-empty-state>`
        : html`<lcm-skeleton shape="row" lines="6" label=${t('security.loading')}></lcm-skeleton>`;
    }
    return html`<div class=${loading ? 'stale' : ''}>
      <div class="stack">${this.#renderBanners(data)} ${this.#renderSummary(data)} ${this.#renderToolbar(data)} ${this.#renderList(data)}</div>
    </div>`;
  }

  override render() {
    return html`${this.#renderBody()} ${this.#renderModal(this.#advisories.data)}`;
  }
}
customElements.define('lcm-sec-advisories', SecAdvisories);
