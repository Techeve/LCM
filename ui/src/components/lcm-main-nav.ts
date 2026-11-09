import { LitElement, css, html, nothing } from 'lit';
import './lcm-logo.js';
import './lcm-menu.js';
import { icon } from './icons.js';
import type { MenuItem } from './lcm-menu.js';
import { baseStyles, emit, uid } from './shared.js';

export interface NavItem {
  id: string;
  label: string;
  href: string;
}

export interface MainNavLabels {
  nav: string;
  search: string;
  docs: string;
  theme: string;
  language: string;
  account: string;
  menu: string;
}

const isMac = typeof navigator !== 'undefined' && /mac|iphone|ipad/i.test(navigator.platform || navigator.userAgent);

/**
 * <lcm-main-nav> – Hauptmenü oben, volle Breite: Logo, Menüpunkte, rechts Suche, Doku, Theme, Sprache, Konto.
 * Alle Texte kommen über Properties. Klicks auf Menüpunkte melden `lcm-navigate` (abbrechbar; die Links
 * sind echte <a href>, der Router übernimmt). Strg/Cmd+K und der Suchknopf melden `lcm-search`.
 * Weitere Ereignisse: `lcm-theme-change`, `lcm-language-change`, `lcm-account-action` (je mit { id }).
 */
export class LcmMainNav extends LitElement {
  static override properties = {
    items: { attribute: false },
    current: { type: String },
    brandLabel: { type: String, attribute: 'brand-label' },
    brandHref: { type: String, attribute: 'brand-href' },
    docsHref: { type: String, attribute: 'docs-href' },
    labels: { attribute: false },
    searchHint: { type: String, attribute: 'search-hint' },
    searchDisabled: { type: Boolean, attribute: 'search-disabled' },
    themeOptions: { attribute: false },
    theme: { type: String },
    languages: { attribute: false },
    language: { type: String },
    accountInitials: { type: String, attribute: 'account-initials' },
    accountItems: { attribute: false },
    _burger: { state: true },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: block;
        inline-size: 100%;
        container-type: inline-size;
      }
      header {
        background: var(--nav-bg);
        color: var(--nav-fg);
        border-block-end: 1px solid var(--nav-line);
      }
      .bar {
        display: flex;
        align-items: center;
        gap: var(--space-4);
        inline-size: 100%;
        min-block-size: calc(var(--control-h) + 1.25rem);
        padding-inline: var(--page-pad);
        --menu-fg: var(--nav-fg);
        --menu-hover: var(--nav-hover-bg);
      }
      .brand {
        display: flex;
        align-items: center;
        gap: var(--space-3);
        flex: none;
        margin-inline-end: var(--space-2);
        border-radius: var(--radius-m);
        color: var(--nav-fg);
        font-size: var(--fs-l);
        font-weight: var(--fw-bold);
        letter-spacing: 0.04em;
        text-decoration: none;
      }
      ul {
        display: flex;
        align-items: center;
        gap: 2px;
        margin: 0;
        padding: 0;
        list-style: none;
      }
      .panel {
        display: contents;
      }
      .mobile-tools {
        display: none;
      }
      .items a {
        display: inline-flex;
        align-items: center;
        min-block-size: var(--control-h);
        padding-inline: var(--space-3);
        border-radius: var(--radius-m);
        color: var(--nav-fg-2);
        font-size: var(--fs-m);
        font-weight: var(--fw-medium);
        text-decoration: none;
        white-space: nowrap;
        transition:
          background var(--dur-1) var(--ease),
          color var(--dur-1) var(--ease);
      }
      .items a:hover {
        background: var(--nav-hover-bg);
        color: var(--nav-fg);
      }
      .items a[aria-current='page'] {
        background: var(--nav-active-bg);
        color: var(--nav-active-fg);
        font-weight: var(--fw-semibold);
      }
      .tools {
        display: flex;
        align-items: center;
        gap: var(--space-1);
        margin-inline-start: auto;
      }
      .search {
        display: inline-flex;
        align-items: center;
        gap: var(--space-2);
        min-block-size: var(--control-h);
        padding-inline: var(--space-3) var(--space-2);
        margin-inline-end: var(--space-2);
        border: 0;
        border-radius: var(--radius-pill);
        background: var(--nav-hover-bg);
        color: var(--nav-fg-2);
        font-size: var(--fs-s);
        cursor: pointer;
      }
      .search:hover:not(:disabled) {
        color: var(--nav-fg);
      }
      .search:disabled {
        opacity: 0.5;
        cursor: not-allowed;
      }
      kbd {
        padding: 0.1rem 0.45rem;
        border-radius: var(--radius-s);
        background: color-mix(in srgb, var(--nav-fg) 12%, transparent);
        color: var(--nav-fg-2);
        font-family: var(--font-mono);
        font-size: 0.7rem;
        line-height: 1.5;
      }
      .tools-inline {
        display: inline-flex;
        align-items: center;
        gap: var(--space-1);
      }
      .tool-link {
        display: inline-grid;
        place-items: center;
        inline-size: var(--control-h);
        block-size: var(--control-h);
        border-radius: var(--radius-m);
        color: var(--nav-fg);
      }
      .tool-link:hover {
        background: var(--nav-hover-bg);
      }
      .burger {
        display: none;
        place-items: center;
        inline-size: var(--control-h);
        block-size: var(--control-h);
        border: 0;
        border-radius: var(--radius-m);
        background: transparent;
        color: var(--nav-fg);
        cursor: pointer;
      }
      .burger:hover {
        background: var(--nav-hover-bg);
      }
      @container (max-width: 90rem) {
        .search .search-text {
          display: none;
        }
        .search {
          padding-inline: var(--space-3);
        }
      }
      @container (max-width: 70rem) {
        .burger {
          display: inline-grid;
        }
        header {
          position: relative;
        }
        .panel {
          display: none;
        }
        .panel.open {
          display: block;
          position: absolute;
          inset-inline: 0;
          inset-block-start: 100%;
          z-index: var(--z-panel);
          padding: var(--space-3) var(--page-pad) var(--space-4);
          background: var(--nav-bg);
          border-block-end: 1px solid var(--nav-line);
          box-shadow: var(--elev-2);
        }
        .items {
          flex-direction: column;
          align-items: stretch;
        }
        .search kbd {
          display: none;
        }
      }
      @container (max-width: 40rem) {
        .hide-narrow {
          display: none;
        }
        .panel.open .mobile-tools {
          display: flex;
          gap: var(--space-2);
          margin-block-start: var(--space-3);
          padding-block-start: var(--space-3);
          border-block-start: 1px solid var(--nav-line);
        }
      }
    `,
  ];

  declare items: NavItem[];
  declare current: string;
  declare brandLabel: string;
  declare brandHref: string;
  declare docsHref: string;
  declare labels: MainNavLabels;
  declare searchHint: string;
  declare searchDisabled: boolean;
  declare themeOptions: MenuItem[];
  declare theme: string;
  declare languages: MenuItem[];
  declare language: string;
  declare accountInitials: string;
  declare accountItems: MenuItem[];
  declare _burger: boolean;
  private readonly listId = uid('nav-items');

  constructor() {
    super();
    this.items = [];
    this.current = '';
    this.brandLabel = 'LCM';
    this.brandHref = '/';
    this.docsHref = '';
    this.labels = { nav: '', search: '', docs: '', theme: '', language: '', account: '', menu: '' };
    this.searchHint = isMac ? '⌘ K' : 'Ctrl K';
    this.searchDisabled = false;
    this.themeOptions = [];
    this.theme = '';
    this.languages = [];
    this.language = '';
    this.accountInitials = '';
    this.accountItems = [];
    this._burger = false;
  }

  override connectedCallback(): void {
    super.connectedCallback();
    window.addEventListener('keydown', this.onGlobalKey);
  }

  override disconnectedCallback(): void {
    window.removeEventListener('keydown', this.onGlobalKey);
    super.disconnectedCallback();
  }

  private readonly onGlobalKey = (event: KeyboardEvent): void => {
    if (!this.searchDisabled && (event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') {
      event.preventDefault();
      emit(this, 'lcm-search');
    }
  };

  private navigate(event: Event, item: NavItem): void {
    const proceed = this.dispatchEvent(
      new CustomEvent('lcm-navigate', { detail: { id: item.id, href: item.href }, bubbles: true, composed: true, cancelable: true }),
    );
    if (!proceed) event.preventDefault();
    this._burger = false;
  }

  private docsLink() {
    return this.docsHref
      ? html`<a class="tool-link" href=${this.docsHref} aria-label=${this.labels.docs} title=${this.labels.docs}>${icon('book')}</a>`
      : nothing;
  }

  private themeMenu() {
    return html`<lcm-menu
      label=${this.labels.theme}
      .icon=${this.theme === 'dark' ? 'moon' : this.theme === 'light' ? 'sun' : 'auto'}
      .items=${this.themeOptions}
      value=${this.theme}
      @lcm-select=${(e: CustomEvent<{ id: string }>) => emit(this, 'lcm-theme-change', e.detail)}
    ></lcm-menu>`;
  }

  private languageMenu() {
    return html`<lcm-menu
      label=${this.labels.language}
      icon="globe"
      .items=${this.languages}
      value=${this.language}
      @lcm-select=${(e: CustomEvent<{ id: string }>) => emit(this, 'lcm-language-change', e.detail)}
    ></lcm-menu>`;
  }

  override render() {
    const l = this.labels;
    return html`<header>
      <nav class="bar" aria-label=${l.nav}>
        <a class="brand" href=${this.brandHref} aria-label=${this.brandLabel} @click=${(e: Event) => this.navigate(e, { id: 'home', label: '', href: this.brandHref })}>
          <lcm-logo label=${this.brandLabel}></lcm-logo><span aria-hidden="true">LCM</span>
        </a>
        <div id=${this.listId} class=${this._burger ? 'panel open' : 'panel'}>
          <ul class="items">
            ${this.items.map(
              (item) => html`<li>
                <a href=${item.href} aria-current=${item.id === this.current ? 'page' : nothing} @click=${(e: Event) => this.navigate(e, item)}>${item.label}</a>
              </li>`,
            )}
          </ul>
          <div class="mobile-tools">${this.docsLink()}${this.themeMenu()}${this.languageMenu()}</div>
        </div>
        <div class="tools">
          <!-- Der Name enthält den sichtbaren Text, das Kürzel (WCAG 2.5.3, „Label in Name“). -->
          <button class="search" type="button" aria-label=${`${l.search}, ${this.searchHint}`} ?disabled=${this.searchDisabled} @click=${() => emit(this, 'lcm-search')}>
            ${icon('search')}<span class="search-text">${l.search}</span><kbd aria-hidden="true">${this.searchHint}</kbd>
          </button>
          <span class="hide-narrow tools-inline">${this.docsLink()}${this.themeMenu()}${this.languageMenu()}</span>
          <lcm-menu
            label=${l.account}
            .avatar=${this.accountInitials}
            .items=${this.accountItems}
            @lcm-select=${(e: CustomEvent<{ id: string }>) => emit(this, 'lcm-account-action', e.detail)}
          ></lcm-menu>
          <button class="burger" type="button" aria-label=${l.menu} aria-expanded=${String(this._burger)} aria-controls=${this.listId} @click=${() => (this._burger = !this._burger)}>
            ${icon(this._burger ? 'x' : 'menu')}
          </button>
        </div>
      </nav>
    </header>`;
  }
}
customElements.define('lcm-main-nav', LcmMainNav);
