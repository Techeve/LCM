import { LitElement, html, nothing, type TemplateResult } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-data-table';
import '../../components/lcm-empty-state';
import '../../components/lcm-list-pager';
import '../../components/lcm-skeleton';
import '../../components/lcm-switch';
import type { Column } from '../../components/lcm-data-table';
import { baseStyles } from '../../components/shared';
import { CONTAINER_IMAGES_URL, PAGE_SIZE, normaliseImages, type ContainerImage, type ImageStatus } from '../../api/containers';
import { opsApi } from '../../api/servers-ops';
import { auth } from '../../lib/auth';
import { I18nController, SubscribeController } from '../../lib/controllers';
import { formatNumber } from '../../lib/format';
import { t } from '../../lib/i18n';
import { LiveData } from '../../lib/live-data';
import { router } from '../../lib/router';
import { plainLabels, secStyles } from '../security/sec-styles';
import { serverLinks } from './ctr-cells';
import './ctr-fleet-action';
import { queryFromUrl, queryPatch, slicePage, visibleImages } from './containers-state';

const STATUS: Record<ImageStatus, { tone: 'warn' | 'neutral' | 'ok'; text: string }> = {
  update: { tone: 'warn', text: 'containers.images.status.update' },
  unverifiable: { tone: 'neutral', text: 'containers.images.status.unverifiable' },
  local: { tone: 'neutral', text: 'containers.images.status.local' },
  current: { tone: 'ok', text: 'containers.images.status.current' },
};

/** <lcm-ctr-images> – Images aller Server mit Update-Stand und Sicherheitslücken. Filter und Seite stehen in der Adresse. */
export class CtrImages extends LitElement {
  static override styles = [
    baseStyles,
    secStyles,
  ];

  readonly #images = new LiveData<ContainerImage[]>(this, { url: () => CONTAINER_IMAGES_URL, transform: normaliseImages });

  constructor() {
    super();
    new I18nController(this);
    new SubscribeController(this, (listener) => router.onChange(listener));
  }

  get #columns(): Column<ContainerImage>[] {
    return [
      { key: 'name', label: t('containers.images.column.image'), kind: 'mono' },
      { key: 'servers', label: t('containers.images.column.servers'), render: (r) => serverLinks(r.servers, r.serverCount) },
      { key: 'inUse', label: t('containers.images.column.used'), render: (r) => (r.inUse > 0 ? t('containers.images.usedValue', { count: r.inUse }) : '–') },
      { key: 'status', label: t('containers.images.column.status'), render: (r) => html`<lcm-badge tone=${STATUS[r.status].tone}>${t(STATUS[r.status].text)}</lcm-badge>` },
      { key: 'cves', label: t('containers.images.column.cves'), render: (r) => this.#cves(r) },
      ...(auth.can('servers:write') ? [{ key: 'action', label: t('serverOps.fleet.column'), render: (r: ContainerImage) => this.#pull(r) }] : []),
    ];
  }

  #cves(image: ContainerImage): TemplateResult | string {
    if (image.critical === 0 && image.high === 0) return '–';
    return html`<div>
      ${image.critical > 0 ? html`<lcm-badge tone="crit">${t('containers.images.critical', { count: image.critical })}</lcm-badge>` : nothing}
      ${image.high > 0 ? html`<lcm-badge tone="warn">${t('containers.images.high', { count: image.high })}</lcm-badge>` : nothing}
    </div>`;
  }

  #pull(image: ContainerImage): TemplateResult | string {
    if (image.status !== 'update' || !image.tag) return '';
    return html`<lcm-ctr-fleet-action
      icon="download"
      label=${t('serverOps.fleet.pull.label')}
      help=${t('serverOps.fleet.pull.help', { image: image.name })}
      meaning=${t('serverOps.fleet.pull.meaning')}
      heading=${t('serverOps.fleet.pull.heading', { image: image.name })}
      .servers=${image.servers}
      .run=${(serverId: number) => opsApi.dockerPull(serverId, image.name)}
    ></lcm-ctr-fleet-action>`;
  }

  #renderList(images: ContainerImage[]): TemplateResult {
    const query = queryFromUrl(router.state.query);
    if (images.length === 0) {
      return html`<lcm-empty-state level="3" icon="container" heading=${t('containers.images.empty.title')} text=${t('containers.images.empty.text')}></lcm-empty-state>`;
    }
    const shown = visibleImages(images, query.onlyUpdates);
    const slice = slicePage(shown, query.page);
    return html`<div class="toolbar">
        <lcm-switch label=${t('containers.images.onlyUpdates')} .checked=${query.onlyUpdates} @lcm-change=${(e: CustomEvent<{ checked: boolean }>) => router.setQuery(queryPatch({ onlyUpdates: e.detail.checked }))}></lcm-switch>
      </div>
      <lcm-data-table external no-toolbar .columns=${this.#columns} .rows=${slice.items} .labels=${plainLabels(t('containers.images.caption'), t('containers.images.noUpdates'))}></lcm-data-table>
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
    const { data, error, loading } = this.#images;
    if (!data) {
      return error
        ? html`<lcm-empty-state level="3" icon="warning" heading=${t('containers.error')} text=${error}></lcm-empty-state>`
        : html`<lcm-skeleton shape="row" lines="6" label=${t('containers.loading')}></lcm-skeleton>`;
    }
    return html`<div class=${`stack ${loading ? 'stale' : ''}`}>${this.#renderList(data)}</div>`;
  }
}
customElements.define('lcm-ctr-images', CtrImages);
