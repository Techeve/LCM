import { LitElement, css, html, nothing, type TemplateResult } from 'lit';
import '../components/lcm-button';
import '../components/lcm-command-palette';
import '../components/lcm-empty-state';
import '../components/lcm-main-nav';
import '../components/lcm-notice';
import '../components/lcm-shortcut-help';
import '../components/lcm-toaster';
import '../pages/not-found-page';
import './leave-dialog';
import './session-mode-dialog';
import { SYSTEM_INFO_URL, normaliseSystemInfo, type SystemInfo } from '../api/system';
import { toast } from '../components/lcm-toaster';
import { baseStyles } from '../components/shared';
import { auth } from '../lib/auth';
import { I18nController, SubscribeController } from '../lib/controllers';
import { loadEntities } from '../lib/entity-search';
import { Hotkeys } from '../lib/hotkeys';
import { errorText, i18n, t } from '../lib/i18n';
import { focusPageSearch } from '../lib/focus-nav';
import { LiveData } from '../lib/live-data';
import { FadeOnChange } from '../lib/motion';
import { isAvailable, pageFor, titleKeyFor } from '../lib/navigation';
import { palette } from '../lib/palette';
import { router } from '../lib/router';
import { getTheme, onThemeChange, setTheme, type Theme } from '../lib/theme';
import { versionWatcher } from '../lib/version';
import './about-dialog';
import { versionLine } from './about-dialog';
import { accountItems, currentNavId, initialsOf, languageItems, mainNavItems, navLabels, themeItems, userName } from './nav-model';
import { buildSections, type ShellActions } from './palette-model';
import { shortcutSections } from './shortcut-sections';
import './update-banner';
import { about } from './update-model';

const THEMES: readonly string[] = ['auto', 'light', 'dark'];

function firstSegment(path: string): string {
  return path.split('/')[1] ?? '';
}

/** Die Shell: Hauptmenü, Seite der aktuellen Route, Befehlspalette, Tastenkürzel-Hilfe, Hinweise und Toaster. */
export class LcmApp extends LitElement {
  static override properties = {
    _paletteOpen: { state: true },
    _helpOpen: { state: true },
    _aboutOpen: { state: true },
    _query: { state: true },
    _pendingHidden: { state: true },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: flex;
        flex-direction: column;
        min-block-size: 100dvh;
      }
      .skip {
        position: fixed;
        inset-block-start: var(--space-3);
        inset-inline-start: var(--space-3);
        z-index: var(--z-skip);
        padding: var(--space-2) var(--space-4);
        border-radius: var(--radius-m);
        background: var(--accent-solid);
        color: var(--text-on-accent);
        border: 0;
        font: inherit;
        font-weight: var(--fw-semibold);
        cursor: pointer;
        transform: translateY(-200%);
      }
      .skip:focus {
        transform: none;
      }
      lcm-main-nav {
        position: sticky;
        inset-block-start: 0;
        z-index: var(--z-nav);
      }
      main {
        flex: 1;
        min-inline-size: 0;
      }
      main.page {
        padding: var(--space-4) var(--page-pad) var(--space-7);
      }
      main:focus {
        outline: none;
      }
    `,
  ];

  declare _paletteOpen: boolean;
  declare _helpOpen: boolean;
  declare _aboutOpen: boolean;
  declare _query: string;
  /** Den Hinweis auf die neue Version hat jemand weggeklickt. */
  declare _pendingHidden: boolean;

  #page: HTMLElement | undefined;
  #path = '';
  #moveFocus = false;
  #stopHotkeys: (() => void) | undefined;
  #stopRoute: (() => void) | undefined;

  readonly #can = (permission: string) => auth.can(permission);
  readonly #system = new LiveData<SystemInfo>(this, { url: () => (auth.isAuthenticated ? SYSTEM_INFO_URL : null), transform: normaliseSystemInfo });

  readonly #actions: ShellActions = {
    go: (path) => {
      this._helpOpen = false;
      void router.navigate(path, { ask: true });
    },
    back: () => history.back(),
    setTheme: (theme) => setTheme(theme),
    setLanguage: (language) => void this.#changeLanguage(language),
    showShortcuts: () => (this._helpOpen = true),
    logout: () => void auth.logout(),
  };

  readonly #hotkeys = new Hotkeys({
    isAuthenticated: () => auth.isAuthenticated && !auth.remediation,
    can: this.#can,
    go: (path) => this.#actions.go(path),
    toggleHelp: () => (this._helpOpen = !this._helpOpen),
    pageKey: (key) => this.#runPageKey(key),
    focusSearch: () => focusPageSearch(this.renderRoot.querySelector('main')),
  });

  constructor() {
    super();
    this._paletteOpen = false;
    this._helpOpen = false;
    this._aboutOpen = false;
    this._query = '';
    this._pendingHidden = false;
    new I18nController(this);
    for (const subscribe of [
      router.onChange.bind(router),
      auth.onChange.bind(auth),
      versionWatcher.onPending.bind(versionWatcher),
      onThemeChange,
      palette.onChange.bind(palette),
    ]) {
      new SubscribeController(this, subscribe);
    }
    // Neue Seite (anderes Element im Inhaltsbereich): kurz einblenden. Die View Transitions API haben wir bewusst
    // nicht genommen: Sie friert die Seite ein, bis die neue gerendert ist (Seitenmodule laden nach), und ihr Overlay
    // fängt währenddessen die Klicks ab.
    const main = () => this.renderRoot.querySelector('main');
    new FadeOnChange(this, () => main()?.firstElementChild, main);
    new SubscribeController(this, () => about.onOpen(() => (this._aboutOpen = true)));
  }

  /** Seitenbefehl mit eigener Taste (`n`, `u` …), nur ohne offene Palette oder Hilfe. */
  #runPageKey(key: string): boolean {
    if (this._paletteOpen || this._helpOpen || this._aboutOpen) return false;
    const item = palette.byKey(key);
    item?.run();
    return item !== undefined;
  }

  override connectedCallback(): void {
    super.connectedCallback();
    this.#stopHotkeys = this.#hotkeys.start();
    this.#stopRoute = router.onChange(() => this.#onRouteChange());
  }

  override disconnectedCallback(): void {
    this.#stopHotkeys?.();
    this.#stopRoute?.();
    super.disconnectedCallback();
  }

  /** Neue Seite: Befehle der alten verwerfen; bei einem Wechsel des Bereichs den Fokus in den Inhalt setzen. */
  #onRouteChange(): void {
    const { path } = router.state;
    if (path === this.#path) return;
    this.#moveFocus = this.#path !== '' && firstSegment(path) !== firstSegment(this.#path);
    this.#path = path;
    palette.clear();
  }

  override updated(): void {
    document.title = this.#documentTitle();
    if (!this.#moveFocus) return;
    this.#moveFocus = false;
    this.renderRoot.querySelector<HTMLElement>('main')?.focus({ preventScroll: true });
    scrollTo(0, 0);
  }

  #documentTitle(): string {
    const { match, path } = router.state;
    const key = match ? titleKeyFor(path) : 'notFound.title';
    return key ? `${t(key)} · LCM` : 'LCM';
  }

  async #changeLanguage(language: string): Promise<void> {
    try {
      await auth.changeLanguage(language);
    } catch (error) {
      toast({ tone: 'crit', title: t('app.languageFailed'), message: errorText(error) });
    }
  }

  #skipToContent(): void {
    this.renderRoot.querySelector<HTMLElement>('main')?.focus();
  }

  // ---- Befehlspalette ----

  #togglePalette(): void {
    if (auth.remediation) return;
    this._paletteOpen = !this._paletteOpen;
    if (!this._paletteOpen) return;
    this._query = '';
    void loadEntities(this.#can).then(() => this.requestUpdate());
  }

  #sections() {
    return buildSections(
      { can: this.#can, actions: this.#actions, pageItems: palette.items, languages: i18n.languages, language: i18n.language },
      this._query,
    );
  }

  #selectInPalette(id: string): void {
    const item = this.#sections()
      .flatMap((section) => section.items)
      .find((candidate) => candidate.id === id);
    this._paletteOpen = false;
    item?.run();
  }

  // ---- Darstellung ----

  #renderNav() {
    const locked = auth.remediation !== null;
    return html`<lcm-main-nav
      .items=${mainNavItems(this.#can, locked)}
      current=${currentNavId(router.state.path)}
      brand-label=${t('nav.brand')}
      docs-href="/docs"
      .labels=${{ ...navLabels(), account: userName(auth.user) ? t('account.menuOf', { name: userName(auth.user) }) : t('nav.accountMenu') }}
      .themeOptions=${themeItems()}
      theme=${getTheme()}
      .languages=${languageItems(i18n.languages)}
      language=${i18n.language}
      ?search-disabled=${locked}
      account-initials=${initialsOf(auth.user)}
      .accountItems=${accountItems(auth.user, versionLine(this.#system.data))}
      @lcm-search=${this.#togglePalette}
      @lcm-theme-change=${(event: CustomEvent<{ id: string }>) => THEMES.includes(event.detail.id) && setTheme(event.detail.id as Theme)}
      @lcm-language-change=${(event: CustomEvent<{ id: string }>) => this.#changeLanguage(event.detail.id)}
      @lcm-account-action=${(event: CustomEvent<{ id: string }>) => this.#onAccountAction(event.detail.id)}
    ></lcm-main-nav>`;
  }

  #onAccountAction(id: string): void {
    if (id === 'account') this.#actions.go('/account');
    else if (id === 'shortcuts') this.#actions.showShortcuts();
    else if (id === 'about') this._aboutOpen = true;
    else if (id === 'logout') this.#actions.logout();
  }

  /** Systemhinweise als Band unter dem Menü: bleiben, solange der Zustand dauert; reine Information lässt sich wegklicken. */
  #renderNotices() {
    const remediation = auth.remediation;
    return html`${remediation
      ? html`<lcm-notice variant="bar" tone="warn">${remediation === 'password' ? t('app.remediation.password') : t('app.remediation.twoFactor')}</lcm-notice>`
      : nothing}
    ${versionWatcher.pending && !this._pendingHidden
      ? html`<lcm-notice variant="bar" tone="info" dismissible close-label=${t('app.noticeClose')} @lcm-dismiss=${() => (this._pendingHidden = true)}
          >${t('app.update.pending')}</lcm-notice
        >`
      : nothing}
    <lcm-update-banner></lcm-update-banner>`;
  }

  /** Neue Version: Ansage vor dem Neuladen, auch auf der Anmeldeseite. */
  #renderReloading() {
    if (!versionWatcher.reloading) return nothing;
    return html`<lcm-notice variant="bar" tone="ok"
      >${t('reload.text')}
      <lcm-button slot="action" size="small" variant="outline" @click=${() => versionWatcher.reloadNow()}>${t('reload.now')}</lcm-button>
    </lcm-notice>`;
  }

  #renderOverlays() {
    const sections = this.#sections();
    const count = sections.reduce((sum, section) => sum + section.items.length, 0);
    return html`<lcm-command-palette
        ?open=${this._paletteOpen}
        label=${t('palette.label')}
        placeholder=${t('palette.placeholder')}
        empty-text=${t('palette.empty')}
        status-text=${t('palette.results', { count })}
        .groups=${sections}
        @lcm-palette-query=${(event: CustomEvent<{ query: string }>) => (this._query = event.detail.query)}
        @lcm-palette-select=${(event: CustomEvent<{ id: string }>) => this.#selectInPalette(event.detail.id)}
        @lcm-palette-close=${() => (this._paletteOpen = false)}
      ></lcm-command-palette>
      <lcm-shortcut-help
        ?open=${this._helpOpen}
        heading=${t('hotkeys.title')}
        intro=${t('hotkeys.intro')}
        close-label=${t('hotkeys.closeLabel')}
        .sections=${shortcutSections(this.#can, palette.items)}
        @lcm-help-close=${() => (this._helpOpen = false)}
      ></lcm-shortcut-help>`;
  }

  #renderPage(): HTMLElement | TemplateResult | typeof nothing {
    const { path, match, loadError } = router.state;
    if (path === '') return nothing; // der Router hat noch keine Adresse aufgelöst
    if (loadError) return this.#renderLoadError();
    if (!match) return this.#pageElement('lcm-page-not-found');
    const entry = pageFor(path);
    if (auth.isAuthenticated && entry && !isAvailable(entry, this.#can)) return this.#renderForbidden();
    return this.#pageElement(match.route.tag);
  }

  /** Dasselbe Element bleibt stehen, solange die Seite gleich bleibt; es liest Parameter und Query selbst aus dem Router. */
  #pageElement(tag: string): HTMLElement {
    if (this.#page?.localName !== tag) this.#page = document.createElement(tag);
    return this.#page;
  }

  #renderLoadError() {
    return html`<lcm-empty-state icon="warning" level="1" heading=${t('app.loadFailed.title')} text=${t('app.loadFailed.text')}>
      <lcm-button slot="action" variant="primary" icon="refresh" @click=${() => void router.reevaluate()}>${t('app.retry')}</lcm-button>
    </lcm-empty-state>`;
  }

  #renderForbidden() {
    return html`<lcm-empty-state icon="lock" level="1" heading=${t('app.forbidden.title')} text=${t('app.forbidden.text')}>
      <lcm-button slot="action" variant="primary" href="/">${t('app.forbidden.back')}</lcm-button>
    </lcm-empty-state>`;
  }

  override render() {
    const bare = !auth.isAuthenticated || router.state.match?.route.path === '/login';
    return html`
      <nav aria-label=${t('app.skipNav')}>
        <button class="skip" type="button" @click=${this.#skipToContent}>${t('app.skip')}</button>
      </nav>
      ${bare ? nothing : this.#renderNav()} ${this.#renderReloading()} ${bare ? nothing : this.#renderNotices()}
      <main id="main" class=${bare ? 'bare' : 'page'} tabindex="-1">${this.#renderPage()}</main>
      ${bare ? nothing : this.#renderOverlays()} ${bare ? nothing : html`<lcm-session-mode-dialog></lcm-session-mode-dialog>`}
      <lcm-about-dialog ?open=${this._aboutOpen} @lcm-close=${() => (this._aboutOpen = false)}></lcm-about-dialog>
      <lcm-leave-dialog></lcm-leave-dialog>
      <lcm-toaster
        close-label=${t('app.toastClose')}
        .moreText=${(count: number) => t('app.toastMore', { count })}
        .waitingText=${(count: number) => t('app.toastWaiting', { count })}
      ></lcm-toaster>
      <div class="sr-only" role="status">${this.#documentTitle()}</div>
    `;
  }
}
customElements.define('lcm-app', LcmApp);
