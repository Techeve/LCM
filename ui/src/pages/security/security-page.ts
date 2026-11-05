import { LitElement, css, html, type TemplateResult } from 'lit';
import '../../components/lcm-explainer';
import '../../components/lcm-tabs';
import '../../components/lcm-term';
import './sec-advisories';
import './sec-bulk';
import './sec-caches';
import './sec-cve';
import type { TabItem } from '../../components/lcm-tabs';
import { baseStyles } from '../../components/shared';
import { ADVISORIES_URL, CACHES_URL, SCANNER_URL, VULNS_URL } from '../../api/security';
import { I18nController, SubscribeController } from '../../lib/controllers';
import { events } from '../../lib/events';
import { t } from '../../lib/i18n';
import { router } from '../../lib/router';
import { store } from '../../lib/store';
import { parseTab, tabPath, type SecurityTab } from './security-state';

function stillRunning(data: unknown): boolean {
  return typeof data === 'object' && data !== null && (data as { running?: unknown }).running === true;
}

/**
 * Sicherheit: CVE-Funde der ganzen Flotte, Frühwarnung und Zwischenspeicher als Reiter (im Pfad), darüber das
 * Sammel-Update mit Fortschritt. Rechts erklärt eine Spalte, wie die Funde gewichtet werden und was die Frühwarnung
 * vom täglichen Scan unterscheidet.
 */
export class SecurityPage extends LitElement {
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
        justify-content: space-between;
        gap: var(--space-3) var(--space-5);
        margin-block-end: var(--space-5);
      }
      h1 {
        margin: 0;
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

  #stops: Array<() => void> = [];

  constructor() {
    super();
    new I18nController(this);
    new SubscribeController(this, (listener) => router.onChange(listener));
  }

  override connectedCallback(): void {
    super.connectedCallback();
    this.#stops = [
      events.subscribe(['bulk', 'jobs']),
      events.onEvent((message) => {
        // Nach dem Sammel-Update und nach jedem Job (Scan, Datenbank, Durchgang) ändern sich die Funde.
        if (message.event === 'bulk.progress' && !stillRunning(message.data)) this.#reloadFindings();
        if (message.event === 'job.finished') {
          this.#reloadFindings();
          store.invalidate(SCANNER_URL);
          store.invalidate(CACHES_URL);
        }
      }),
    ];
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    this.#stops.forEach((stop) => stop());
    this.#stops = [];
  }

  #reloadFindings(): void {
    store.invalidate(VULNS_URL);
    store.invalidate(ADVISORIES_URL);
  }

  get #tab(): SecurityTab {
    return parseTab(router.state.match?.params.tab);
  }

  get #tabs(): TabItem[] {
    return [
      { id: 'cve', label: t('security.tabs.cve') },
      { id: 'advisories', label: t('security.tabs.advisories') },
      { id: 'caches', label: t('security.tabs.caches') },
    ];
  }

  #explainer(tab: SecurityTab): TemplateResult {
    const attrs = { collapse: t('security.explainer.collapse'), expand: t('security.explainer.expand') };
    if (tab === 'advisories') {
      return html`<lcm-explainer topic="security-advisories" heading=${t('security.explainer.advisories.title')} collapse-label=${attrs.collapse} expand-label=${attrs.expand}>
        <p>${t('security.explainer.advisories.intro')}</p>
        <ul>
          <li>${t('security.explainer.advisories.fast')}</li>
          <li>${t('security.explainer.advisories.malware')}</li>
          <li>${t('security.explainer.advisories.noFix')}</li>
          <li>${t('security.explainer.advisories.ack')}</li>
        </ul>
      </lcm-explainer>`;
    }
    if (tab === 'caches') {
      return html`<lcm-explainer topic="security-caches" heading=${t('security.explainer.caches.title')} collapse-label=${attrs.collapse} expand-label=${attrs.expand}>
        <p>${t('security.explainer.caches.intro')}</p>
        <p>${t('security.explainer.caches.reading')}</p>
      </lcm-explainer>`;
    }
    return html`<lcm-explainer topic="security-cve" heading=${t('security.explainer.cve.title')} collapse-label=${attrs.collapse} expand-label=${attrs.expand}>
      <p>${t('security.explainer.cve.intro')}</p>
      <ul>
        <li>${t('security.explainer.cve.container')}</li>
        <li><lcm-term heading=${t('security.term.exposed.title')} definition=${t('security.term.exposed.definition')}>${t('security.term.exposed.title')}</lcm-term>: ${t('security.explainer.cve.exposed')}</li>
      </ul>
      <p>${t('security.explainer.cve.raw')}</p>
      <p>${t('security.explainer.cve.bulk')}</p>
    </lcm-explainer>`;
  }

  #renderTab(tab: SecurityTab): TemplateResult {
    if (tab === 'advisories') return html`<lcm-sec-advisories></lcm-sec-advisories>`;
    if (tab === 'caches') return html`<lcm-sec-caches></lcm-sec-caches>`;
    return html`<lcm-sec-cve></lcm-sec-cve>`;
  }

  override render() {
    const tab = this.#tab;
    return html`<div class="head">
        <h1>${t('security.title')}</h1>
        <lcm-sec-bulk></lcm-sec-bulk>
      </div>
      <lcm-tabs label=${t('security.tabs.label')} .tabs=${this.#tabs} value=${tab} @lcm-tab-change=${(e: CustomEvent<{ id: string }>) => void router.navigate(tabPath(parseTab(e.detail.id)))}>
        <div class="layout">
          <div class="content">${this.#renderTab(tab)}</div>
          ${this.#explainer(tab)}
        </div>
      </lcm-tabs>`;
  }
}
customElements.define('lcm-page-security', SecurityPage);
