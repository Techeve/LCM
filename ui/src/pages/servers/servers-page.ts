import { LitElement, css, html, nothing } from 'lit';
import '../../components/lcm-button';
import { baseStyles } from '../../components/shared';
import { auth } from '../../lib/auth';
import { I18nController, SubscribeController } from '../../lib/controllers';
import { t } from '../../lib/i18n';
import { palette } from '../../lib/palette';
import { router } from '../../lib/router';
import './servers-list';
import './server-detail';

/**
 * Server: die Liste links, rechts das Serverdetail (`/servers/:id/:tab?`). Ab 80rem (1280 px) stehen beide nebeneinander
 * und die Liste schrumpft auf Ampel, Name, OS-Logo und Updates; darunter gilt die normale Seitenfolge (Liste oder Detail).
 */
export class ServersPage extends LitElement {
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
        gap: var(--space-3) var(--space-5);
        margin-block-end: var(--space-3);
      }
      h1 {
        margin: 0;
        font-size: var(--fs-2xl);
        font-weight: var(--head-weight);
        letter-spacing: var(--head-tracking);
      }
      .head lcm-button {
        margin-inline-start: auto;
      }
      .layout {
        display: grid;
        grid-template-columns: minmax(0, 1fr);
        gap: var(--gap);
        align-items: start;
      }
      .back {
        display: none;
        margin-block-end: var(--space-3);
        color: var(--accent-text);
        font-weight: var(--fw-semibold);
        text-decoration: none;
      }
      .layout.detail .list {
        display: none;
      }
      .layout.detail .back {
        display: inline-block;
      }
      @media (min-width: 80rem) {
        .layout.detail {
          grid-template-columns: minmax(21rem, 25rem) minmax(0, 1fr);
        }
        .layout.detail .list {
          display: block;
          position: sticky;
          inset-block-start: var(--space-3);
          --table-max-h: calc(100dvh - 17rem);
        }
        .layout.detail .back {
          display: none;
        }
      }
    `,
  ];

  constructor() {
    super();
    new I18nController(this);
    new SubscribeController(this, (listener) => router.onChange(listener));
    new SubscribeController(this, (listener) => auth.onChange(listener));
  }

  #paletteFor = '';

  /** „Server hinzufügen“ mit der Taste n. Nach jedem Wechsel der Adresse neu, weil die Shell die Seitenbefehle dann leert. */
  override willUpdate(): void {
    const path = router.state.path;
    if (path === this.#paletteFor) return;
    this.#paletteFor = path;
    if (!auth.can('servers:write')) return;
    palette.register(this, [{ id: 'servers.add', label: t('servers.add'), icon: 'plus', key: 'n', keywords: ['join', 'onboarding'], run: () => void router.navigate('/servers/join') }]);
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    palette.unregister(this);
    this.#paletteFor = '';
  }

  get #id(): number | null {
    const raw = router.state.match?.params.id ?? '';
    return /^\d+$/.test(raw) ? Number(raw) : null;
  }

  override render() {
    const id = this.#id;
    const query = location.search;
    return html`<div class="head">
        <h1>${t('servers.title')}</h1>
        ${auth.can('servers:write') ? html`<lcm-button variant="primary" icon="plus" href="/servers/join">${t('servers.add')}</lcm-button>` : nothing}
      </div>
      <div class="layout ${id === null ? '' : 'detail'}">
        <div class="list"><lcm-servers-list active-id=${id === null ? '' : String(id)} ?compact=${id !== null}></lcm-servers-list></div>
        ${id === null
          ? nothing
          : html`<div class="pane">
              <a class="back" href=${`/servers${query}`}>${t('servers.back')}</a>
              <lcm-server-detail server-id=${id} tab=${router.state.match?.params.tab ?? ''}></lcm-server-detail>
            </div>`}
      </div>`;
  }
}
customElements.define('lcm-page-servers', ServersPage);
