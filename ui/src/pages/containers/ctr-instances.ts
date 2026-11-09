import { LitElement, html, nothing, type TemplateResult } from 'lit';
import '../../components/lcm-data-table';
import '../../components/lcm-empty-state';
import '../../components/lcm-list-pager';
import '../../components/lcm-skeleton';
import '../../components/lcm-status';
import '../../components/lcm-switch';
import '../security/sec-cell';
import type { Column } from '../../components/lcm-data-table';
import { baseStyles } from '../../components/shared';
import { CONTAINERS_URL, PAGE_SIZE, normaliseContainers, stateLevel, type ContainerItem } from '../../api/containers';
import { I18nController, SubscribeController } from '../../lib/controllers';
import { formatNumber } from '../../lib/format';
import { t } from '../../lib/i18n';
import { LiveData } from '../../lib/live-data';
import { router } from '../../lib/router';
import { plainLabels, secStyles } from '../security/sec-styles';
import { queryFromUrl, queryPatch, slicePage, stateText, visibleContainers } from './containers-state';

/** <lcm-ctr-instances> – Container aller Server. Standardmäßig nur die laufenden; Schalter und Seite stehen in der Adresse. */
export class CtrInstances extends LitElement {
  static override styles = [
    baseStyles,
    secStyles,
  ];

  readonly #containers = new LiveData<ContainerItem[]>(this, { url: () => CONTAINERS_URL, transform: normaliseContainers });

  constructor() {
    super();
    new I18nController(this);
    new SubscribeController(this, (listener) => router.onChange(listener));
  }

  get #columns(): Column<ContainerItem>[] {
    return [
      { key: 'name', label: t('containers.instances.column.container'), render: (r) => html`<lcm-sec-cell strong>${r.name}</lcm-sec-cell>` },
      { key: 'server', label: t('containers.instances.column.server'), render: (r) => html`<lcm-sec-cell href=${`/servers/${r.serverId}`}>${r.serverName}</lcm-sec-cell>` },
      { key: 'image', label: t('containers.instances.column.image'), kind: 'mono' },
      { key: 'state', label: t('containers.instances.column.state'), render: (r) => this.#stateCell(r) },
      { key: 'ports', label: t('containers.instances.column.ports'), kind: 'mono', render: (r) => r.ports || '–' },
      { key: 'project', label: t('containers.instances.column.project'), render: (r) => this.#projectCell(r) },
    ];
  }

  #stateCell(item: ContainerItem): TemplateResult {
    return html`<lcm-status level=${stateLevel(item.state)} label=${stateText(item.state)}></lcm-status>${item.status ? html`<lcm-sec-cell block muted>${item.status}</lcm-sec-cell>` : nothing}`;
  }

  #projectCell(item: ContainerItem): TemplateResult | string {
    if (!item.project) return '–';
    return html`${item.project}${item.service ? html`<lcm-sec-cell block muted>${item.service}</lcm-sec-cell>` : nothing}`;
  }

  #renderList(containers: ContainerItem[]): TemplateResult {
    if (containers.length === 0) {
      return html`<lcm-empty-state level="3" icon="container" heading=${t('containers.instances.empty.title')} text=${t('containers.instances.empty.text')}></lcm-empty-state>`;
    }
    const query = queryFromUrl(router.state.query);
    const slice = slicePage(visibleContainers(containers, query.showAll), query.page);
    return html`<div class="toolbar">
        <lcm-switch label=${t('containers.instances.showAll')} .checked=${query.showAll} @lcm-change=${(e: CustomEvent<{ checked: boolean }>) => router.setQuery(queryPatch({ showAll: e.detail.checked }))}></lcm-switch>
      </div>
      <lcm-data-table external no-toolbar .columns=${this.#columns} .rows=${slice.items} .labels=${plainLabels(t('containers.instances.caption'), t('containers.instances.noRunning'))}></lcm-data-table>
      ${slice.total === 0
        ? nothing
        : html`<lcm-list-pager
            page=${slice.page}
            page-size=${PAGE_SIZE}
            total=${slice.total}
            range-label=${t('containers.pager.range', { from: slice.from, to: slice.to, total: formatNumber(slice.total) })}
            previous-label=${t('containers.pager.previous')}
            next-label=${t('containers.pager.next')}
            @lcm-page=${(e: CustomEvent<{ page: number }>) => router.setQuery(queryPatch({ page: e.detail.page }))}
          ></lcm-list-pager>`}`;
  }

  override render() {
    const { data, error, loading } = this.#containers;
    if (!data) {
      return error
        ? html`<lcm-empty-state level="3" icon="warning" heading=${t('containers.error')} text=${error}></lcm-empty-state>`
        : html`<lcm-skeleton shape="row" lines="6" label=${t('containers.loading')}></lcm-skeleton>`;
    }
    return html`<div class=${`stack ${loading ? 'stale' : ''}`}>${this.#renderList(data)}</div>`;
  }
}
customElements.define('lcm-ctr-instances', CtrInstances);
