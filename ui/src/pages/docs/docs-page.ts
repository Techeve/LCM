import { LitElement, css, html, nothing } from 'lit';
import '../../components/lcm-button';
import '../../components/lcm-empty-state';
import '../../components/lcm-logo';
import '../../components/lcm-notice';
import '../../components/lcm-skeleton';
import { baseStyles } from '../../components/shared';
import { api } from '../../lib/api';
import { auth } from '../../lib/auth';
import { I18nController, SubscribeController } from '../../lib/controllers';
import { errorText, i18n, t } from '../../lib/i18n';
import { router } from '../../lib/router';
import { onThemeChange } from '../../lib/theme';
import { publicMenus } from '../public/public-frame';
import { headingsOf, parseDocHtml, renderDocNode, type DocNode } from './docs-render';
import { docsSlug } from './docs-route';
import { docsStyles } from './docs-styles';

interface DocSummary {
  slug: string;
  title: string;
}

interface DocPage extends DocSummary {
  html: string;
}

const anonymous = { anonymous: true };

/**
 * Doku (`/docs/:slug`, öffentlich): Seitenliste, Inhalt, „Auf dieser Seite“. Abschnitte sind direkt verlinkbar
 * (`/docs/ssh-schluessel#windows-mit-openssh`). Die Sprache folgt der Oberfläche. Ohne Anmeldung steht die Seite
 * ohne Hauptmenü, mit eigener Kopfzeile.
 */
export class DocsPage extends LitElement {
  static override properties = {
    _pages: { state: true },
    _page: { state: true },
    _tree: { state: true },
    _loading: { state: true },
    _error: { state: true },
  };
  static override styles = [
    baseStyles,
    docsStyles,
    css`
      :host {
        display: block;
      }
      :host([bare]) {
        min-block-size: 100dvh;
        padding: 0 var(--page-pad) var(--space-6);
      }
      .bar {
        display: flex;
        align-items: center;
        gap: var(--space-3);
        max-inline-size: 84rem;
        margin: 0 auto var(--space-5);
        padding-block: var(--space-3);
        border-block-end: 1px solid var(--line);
      }
      .bar a.brand {
        display: inline-flex;
        align-items: center;
        gap: var(--space-2);
        color: var(--text-1);
        font-weight: var(--fw-semibold);
        text-decoration: none;
      }
      .bar lcm-logo {
        inline-size: 1.75rem;
        block-size: 1.75rem;
      }
      .bar .spacer {
        flex: 1;
      }
      .layout {
        display: grid;
        grid-template-columns: minmax(12rem, 15rem) minmax(0, 46rem) minmax(11rem, 14rem);
        gap: var(--space-6);
        align-items: start;
        max-inline-size: 84rem;
        margin-inline: auto;
      }
      nav.pages,
      nav.toc {
        position: sticky;
        inset-block-start: var(--space-4);
        display: grid;
        gap: 2px;
      }
      .label {
        margin: 0 0 var(--space-2);
        padding-inline: var(--space-3);
        color: var(--text-3);
        font-size: var(--fs-xs);
        font-weight: var(--fw-semibold);
        letter-spacing: var(--label-spacing);
        text-transform: var(--label-transform);
      }
      nav.pages a {
        padding: var(--space-2) var(--space-3);
        border-radius: var(--radius-m);
        color: var(--text-2);
        text-decoration: none;
        line-height: 1.35;
      }
      nav.pages a:hover {
        background: var(--surface-2);
        color: var(--text-1);
      }
      nav.pages a[aria-current='page'] {
        background: var(--nav-active-bg);
        color: var(--nav-active-fg);
        font-weight: var(--fw-semibold);
      }
      nav.toc a {
        padding: var(--space-1) var(--space-3);
        border-inline-start: 2px solid var(--line);
        color: var(--text-2);
        font-size: var(--fs-s);
        text-decoration: none;
        line-height: 1.4;
      }
      nav.toc a.sub {
        padding-inline-start: var(--space-5);
      }
      nav.toc a:hover,
      nav.toc a.here {
        border-inline-start-color: var(--accent);
        color: var(--text-1);
      }
      @media (max-width: 72rem) {
        .layout {
          grid-template-columns: minmax(12rem, 15rem) minmax(0, 1fr);
        }
        nav.toc {
          display: none;
        }
      }
      @media (max-width: 48rem) {
        .layout {
          grid-template-columns: minmax(0, 1fr);
        }
        nav.pages {
          position: static;
        }
      }
    `,
  ];

  declare _pages: DocSummary[];
  declare _page: DocPage | null;
  declare _tree: DocNode[];
  declare _loading: boolean;
  declare _error: string;
  #loadedKey = '';
  #abort: AbortController | undefined;
  #scrolledFor = '';

  constructor() {
    super();
    this._pages = [];
    this._page = null;
    this._tree = [];
    this._loading = true;
    this._error = '';
    new I18nController(this);
    new SubscribeController(this, (listener) => router.onChange(listener));
    new SubscribeController(this, (listener) => auth.onChange(listener));
    new SubscribeController(this, onThemeChange);
  }

  override connectedCallback(): void {
    super.connectedCallback();
    addEventListener('hashchange', this.#onHashChange);
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    removeEventListener('hashchange', this.#onHashChange);
    this.#abort?.abort();
    this.#loadedKey = ''; // beim Wiederkommen frisch laden
  }

  get #slug(): string {
    return docsSlug(router.state.match) ?? '';
  }

  override willUpdate(): void {
    this.toggleAttribute('bare', !auth.isAuthenticated);
    const slug = docsSlug(router.state.match);
    if (slug === null || !this.isConnected) return; // die Adresse gehört schon der nächsten Seite
    const key = `${i18n.language}:${slug}`;
    if (key !== this.#loadedKey) {
      this.#loadedKey = key;
      void this.#load(i18n.language, slug);
    }
  }

  override updated(): void {
    const target = `${this._page?.slug ?? ''}${location.hash}`;
    if (this._page && location.hash && target !== this.#scrolledFor) {
      this.#scrolledFor = target;
      this.#scrollToHash();
    }
  }

  async #load(language: string, slug: string): Promise<void> {
    this.#abort?.abort();
    const abort = new AbortController();
    this.#abort = abort;
    this._loading = true;
    this._error = '';
    try {
      const lang = encodeURIComponent(language);
      const pages = await api.get<DocSummary[]>(`/api/v1/docs?lang=${lang}`, { ...anonymous, signal: abort.signal });
      const wanted = slug || pages[0]?.slug;
      if (!slug && wanted) {
        if (abort.signal.aborted || docsSlug(router.state.match) === null) return;
        await router.navigate(`/docs/${wanted}${location.hash}`, { replace: true });
        return;
      }
      const page = wanted ? await api.get<DocPage>(`/api/v1/docs/${encodeURIComponent(wanted)}?lang=${lang}`, { ...anonymous, signal: abort.signal }) : null;
      this._pages = pages;
      this._page = page;
      this._tree = page ? parseDocHtml(page.html) : [];
      if (!location.hash) scrollTo(0, 0);
    } catch (error) {
      if (abort.signal.aborted) return;
      this._page = null;
      this._tree = [];
      this._error = errorText(error);
    } finally {
      if (!abort.signal.aborted) this._loading = false;
    }
  }

  #onHashChange = (): void => {
    this.#scrolledFor = `${this._page?.slug ?? ''}${location.hash}`;
    this.#scrollToHash();
    this.requestUpdate();
  };

  /** Die Überschriften liegen im Shadow DOM, der Browser findet den Anker dort nicht selbst. */
  #scrollToHash(): void {
    const id = decodeURIComponent(location.hash.slice(1));
    if (!id) return;
    const target = this.renderRoot.querySelector<HTMLElement>(`article [id="${CSS.escape(id)}"]`);
    if (!target) return;
    target.scrollIntoView({ block: 'start' });
    target.setAttribute('tabindex', '-1');
    target.focus({ preventScroll: true });
  }

  #bar() {
    return html`<div class="bar">
      <a class="brand" href="/docs"><lcm-logo label=${t('nav.brand')}></lcm-logo><span>${t('docs.brand')}</span></a>
      <span class="spacer"></span>
      ${publicMenus()}
      <lcm-button variant="outline" size="small" href="/login">${t('docs.login')}</lcm-button>
    </div>`;
  }

  #pagesNav() {
    const current = this._page?.slug ?? this.#slug;
    return html`<nav class="pages" aria-label=${t('docs.pages')}>
      <p class="label">${t('docs.title')}</p>
      ${this._pages.map(
        (page) => html`<a href=${`/docs/${page.slug}`} aria-current=${page.slug === current ? 'page' : 'false'}>${page.title}</a>`,
      )}
    </nav>`;
  }

  #toc() {
    const headings = headingsOf(this._tree);
    if (headings.length < 2) return html`<span></span>`;
    const here = decodeURIComponent(location.hash.slice(1));
    return html`<nav class="toc" aria-label=${t('docs.onThisPage')}>
      <p class="label">${t('docs.onThisPage')}</p>
      ${headings.map(
        (heading) => html`<a class=${`${heading.level === 3 ? 'sub' : ''} ${heading.id === here ? 'here' : ''}`} href=${`#${heading.id}`}>${heading.text}</a>`,
      )}
    </nav>`;
  }

  #article() {
    if (this._error) return html`<article><lcm-notice tone="crit">${this._error}</lcm-notice></article>`;
    if (this._loading && !this._page) return html`<article><lcm-skeleton shape="text" lines="12" label=${t('docs.loading')}></lcm-skeleton></article>`;
    if (!this._page) {
      return html`<article><lcm-empty-state level="1" icon="book" heading=${t('docs.emptyTitle')} text=${t('docs.empty')}></lcm-empty-state></article>`;
    }
    const options = { anchorLabel: t('docs.anchor') };
    return html`<article class="doc" aria-busy=${this._loading ? 'true' : 'false'}>${this._tree.map((node) => renderDocNode(node, options))}</article>`;
  }

  override render() {
    return html`${auth.isAuthenticated ? nothing : this.#bar()}
      <div class="layout">${this.#pagesNav()} ${this.#article()} ${this.#toc()}</div>`;
  }
}
customElements.define('lcm-page-docs', DocsPage);
