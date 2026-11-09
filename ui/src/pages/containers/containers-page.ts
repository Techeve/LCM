import { LitElement, css, html, nothing, type TemplateResult } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-explainer';
import '../../components/lcm-tabs';
import '../../components/lcm-term';
import './ctr-compose';
import './ctr-images';
import './ctr-instances';
import type { TabItem } from '../../components/lcm-tabs';
import { baseStyles } from '../../components/shared';
import { CONTAINER_IMAGES_URL, RUNTIME, normaliseImages, type ContainerImage } from '../../api/containers';
import { I18nController, SubscribeController } from '../../lib/controllers';
import { events } from '../../lib/events';
import { t } from '../../lib/i18n';
import { LiveData } from '../../lib/live-data';
import { router } from '../../lib/router';
import { store } from '../../lib/store';
import { parseTab, seriousCount, tabPath, updateCount, type ContainerTab } from './containers-state';

const DOCKER_PREFIX = '/api/v1/docker';

/**
 * Container: Images, Container und Compose-Projekte aller Server (bisher „Docker“). Die Reiter stehen im Pfad
 * (`/containers`, `/containers/instances`, `/containers/compose`), Filter und Seite in der Query. Die Daten stammen
 * heute aus der Laufzeit Docker; das Kennzeichen am Titel sagt das, damit weitere Laufzeiten Platz haben.
 */
export class ContainersPage extends LitElement {
  static override styles = [
    baseStyles,
    css`
      :host {
        display: block;
      }
      .head {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: var(--space-3);
        margin-block-end: var(--space-5);
      }
      h1 {
        margin: 0 var(--space-2) 0 0;
        font-size: var(--fs-2xl);
        font-weight: var(--head-weight);
        letter-spacing: var(--head-tracking);
        line-height: 1.15;
      }
      .layout {
        display: grid;
        grid-template-columns: minmax(0, 1fr) auto;
        gap: var(--gap);
        align-items: start;
      }
      .content {
        min-inline-size: 0;
      }
      lcm-explainer ul {
        margin: 0 0 var(--space-3);
        padding-inline-start: 1.1rem;
      }
      lcm-explainer li {
        margin-block-end: var(--space-2);
      }
      @media (max-width: 75rem) {
        .layout {
          grid-template-columns: minmax(0, 1fr);
        }
        lcm-explainer {
          inline-size: auto;
        }
      }
    `,
  ];

  readonly #images = new LiveData<ContainerImage[]>(this, { url: () => CONTAINER_IMAGES_URL, transform: normaliseImages });

  constructor() {
    super();
    new I18nController(this);
    new SubscribeController(this, (listener) => router.onChange(listener));
    // Pull und Compose-Update laufen als Jobs; ist einer fertig, stimmen Images und Container nicht mehr.
    new SubscribeController(this, () => events.subscribe(['jobs']));
    new SubscribeController(this, () => events.onEvent((message) => message.event === 'job.finished' && store.invalidate(DOCKER_PREFIX)));
  }

  get #tab(): ContainerTab {
    return parseTab(router.state.match?.params['*']);
  }

  get #tabs(): TabItem[] {
    return [
      { id: 'images', label: t('containers.tabs.images') },
      { id: 'instances', label: t('containers.tabs.instances') },
      { id: 'compose', label: t('containers.tabs.compose') },
    ];
  }

  #renderBadges(): TemplateResult | typeof nothing {
    const images = this.#images.data;
    if (!images) return nothing;
    const updates = updateCount(images);
    const serious = seriousCount(images);
    return html`${updates > 0 ? html`<lcm-badge tone="warn">${t('containers.badge.updates', { count: updates })}</lcm-badge>` : nothing}
    ${serious > 0 ? html`<lcm-badge tone="crit">${t('containers.badge.serious', { count: serious })}</lcm-badge>` : nothing}`;
  }

  #explainer(tab: ContainerTab): TemplateResult {
    const collapse = t('containers.explainer.collapse');
    const expand = t('containers.explainer.expand');
    if (tab === 'instances') {
      return html`<lcm-explainer topic="containers-instances" heading=${t('containers.explainer.instances.title')} collapse-label=${collapse} expand-label=${expand}>
        <p>${t('containers.explainer.instances.intro')}</p>
        <ul>
          <li>${t('containers.explainer.instances.stopped')}</li>
          <li>${t('containers.explainer.instances.symptoms')}</li>
        </ul>
      </lcm-explainer>`;
    }
    if (tab === 'compose') {
      return html`<lcm-explainer topic="containers-compose" heading=${t('containers.explainer.compose.title')} collapse-label=${collapse} expand-label=${expand}>
        <p>${t('containers.explainer.compose.intro')}</p>
        <p>${t('containers.explainer.compose.incomplete')}</p>
      </lcm-explainer>`;
    }
    return html`<lcm-explainer topic="containers-images" heading=${t('containers.explainer.images.title')} collapse-label=${collapse} expand-label=${expand}>
      <p>${t('containers.explainer.images.intro')}</p>
      <ul>
        <li>${t('containers.explainer.images.update')}</li>
        <li>${t('containers.explainer.images.unverifiable')}</li>
        <li>${t('containers.explainer.images.local')}</li>
      </ul>
      <p>${t('containers.explainer.images.cves')}</p>
    </lcm-explainer>`;
  }

  #renderTab(tab: ContainerTab): TemplateResult {
    if (tab === 'instances') return html`<lcm-ctr-instances></lcm-ctr-instances>`;
    if (tab === 'compose') return html`<lcm-ctr-compose></lcm-ctr-compose>`;
    return html`<lcm-ctr-images></lcm-ctr-images>`;
  }

  override render() {
    const tab = this.#tab;
    return html`<div class="head">
        <h1>${t('containers.title')}</h1>
        <lcm-badge tone="info"><lcm-term heading=${t('containers.runtime.title', { runtime: RUNTIME })} definition=${t('containers.runtime.definition', { runtime: RUNTIME })}>${RUNTIME}</lcm-term></lcm-badge>
        ${this.#renderBadges()}
      </div>
      <lcm-tabs label=${t('containers.tabs.label')} .tabs=${this.#tabs} value=${tab} @lcm-tab-change=${(e: CustomEvent<{ id: string }>) => void router.navigate(tabPath(parseTab(e.detail.id)))}>
        <div class="layout">
          <div class="content">${this.#renderTab(tab)}</div>
          ${this.#explainer(tab)}
        </div>
      </lcm-tabs>`;
  }
}
customElements.define('lcm-page-containers', ContainersPage);
