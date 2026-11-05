// Router mit History API und echten Pfaden. Alles, was eine Ansicht ausmacht
// (Reiter, Filter, Sortierung, Seite, Suche, Fenster), steht in der Adresse:
// Reiter als Pfadsegment, der Rest als Query (`setQuery`). Zurück und Vor
// funktionieren damit auch dafür.

import { isRedirect, matchRoute, type PageRoute, type Params, type RedirectRoute, type Route, type RouteMatch } from './route-match';
import { isInternalPath, loginHref, safeNext } from './safe-path';
import { unsaved } from './unsaved';

export interface RouterOptions {
  routes: Route[];
  isAuthenticated: () => boolean;
  /** Leitet angemeldete Aufrufe um (Pflichtschritte): Zieladresse oder `null`, wenn die Adresse bleiben darf. */
  guard?: (path: string) => string | null;
}

export interface RouterState {
  path: string;
  query: URLSearchParams;
  match: RouteMatch<PageRoute> | null;
  /** Das Modul der Seite ließ sich nicht laden (z. B. nach einer neuen Version). */
  loadError: unknown;
}

export type QueryValue = string | number | boolean | null | undefined;
type Mode = 'push' | 'replace' | 'pop';

const MAX_REDIRECTS = 5;

function currentHref(): string {
  return location.pathname + location.search + location.hash;
}

/** Alte Adressen mit `#/pfad` werden zu `/pfad`. */
function withoutLegacyHash(url: URL): URL {
  if (!url.hash.startsWith('#/')) return url;
  const target = url.hash.slice(1);
  return new URL(isInternalPath(target) ? target : '/', location.origin);
}

async function redirectDestination(route: RedirectRoute, params: Params): Promise<string | null> {
  try {
    return await route.redirect(params);
  } catch {
    return null; // z. B. Namenssuche ohne Antwort: die Adresse gilt dann als unbekannt
  }
}

function isInternalLink(anchor: HTMLAnchorElement): boolean {
  if (!anchor.hasAttribute('href') || anchor.hasAttribute('download')) return false;
  if (anchor.target && anchor.target !== '_self') return false;
  if (anchor.origin !== location.origin || anchor.pathname.startsWith('/api/')) return false;
  const samePage = anchor.pathname === location.pathname && anchor.search === location.search;
  return !(samePage && anchor.hash); // Sprung innerhalb der Seite bleibt dem Browser
}

export class Router {
  #routes: Route[] = [];
  #isAuthenticated: () => boolean = () => false;
  #guard: (path: string) => string | null = () => null;
  #state: RouterState = { path: '', query: new URLSearchParams(), match: null, loadError: undefined };
  #listeners = new Set<() => void>();
  #sequence = 0;
  #pageChange = new AbortController();

  configure(options: RouterOptions): void {
    this.#routes = options.routes;
    this.#isAuthenticated = options.isAuthenticated;
    this.#guard = options.guard ?? (() => null);
  }

  get state(): RouterState {
    return this.#state;
  }

  /** Endet beim nächsten Seitenwechsel; für Abrufe, die nur zur Seite gehören. */
  get navigationSignal(): AbortSignal {
    return this.#pageChange.signal;
  }

  onChange(listener: () => void): () => void {
    this.#listeners.add(listener);
    return () => this.#listeners.delete(listener);
  }

  /** Hört auf Zurück/Vor und Links und zeigt die aktuelle Adresse. */
  start(): Promise<void> {
    addEventListener('popstate', () => void this.#userGo(this.#currentUrl(), 'pop'));
    document.addEventListener('click', (event) => this.#onClick(event));
    return this.#go(this.#currentUrl(), 'pop');
  }

  /** `ask`: Ein Benutzer will wechseln, ungespeicherte Änderungen führen zur Rückfrage. Nach dem Speichern bleibt sie aus. */
  navigate(to: string, { replace = false, ask = false }: { replace?: boolean; ask?: boolean } = {}): Promise<void> {
    const url = new URL(to, location.origin);
    if (url.origin !== location.origin) throw new RangeError(`not an internal address: ${to}`);
    const mode = replace ? 'replace' : 'push';
    return ask ? this.#userGo(url, mode) : this.#go(url, mode);
  }

  /** Prüft die aktuelle Adresse erneut, z. B. nachdem die Sitzung abgelaufen ist. */
  reevaluate(): Promise<void> {
    return this.#go(this.#currentUrl(), 'replace');
  }

  /** Nach der Anmeldung dorthin, wo der Benutzer hin wollte. */
  afterLogin(): Promise<void> {
    return this.navigate(safeNext(this.#state.query.get('next')) ?? '/', { replace: true });
  }

  /** Ändert Query-Parameter. `null`, `undefined`, `''` und `false` entfernen den Parameter. */
  setQuery(patch: Record<string, QueryValue>, { replace = false }: { replace?: boolean } = {}): void {
    const query = new URLSearchParams(this.#state.query);
    for (const [name, value] of Object.entries(patch)) {
      if (value === null || value === undefined || value === '' || value === false) query.delete(name);
      else query.set(name, String(value));
    }
    if (query.toString() === this.#state.query.toString()) return;
    const url = new URL(location.href);
    url.search = query.toString();
    this.#writeHistory(url, replace ? 'replace' : 'push');
    this.#state = { ...this.#state, query };
    this.#notify();
  }

  #currentUrl(): URL {
    return new URL(currentHref(), location.origin);
  }

  /** Wechsel durch den Benutzer: Link, Menü, Zurück und Vor. */
  async #userGo(start: URL, mode: Mode): Promise<void> {
    if (await this.#mayLeave(start, mode)) return this.#go(start, mode);
  }

  async #go(start: URL, mode: Mode): Promise<void> {
    const sequence = ++this.#sequence;
    const target = await this.#resolve(start, sequence);
    if (!target || sequence !== this.#sequence) return;

    let loadError: unknown;
    if (target.match) {
      try {
        await target.match.route.load();
      } catch (error) {
        loadError = error;
      }
      if (sequence !== this.#sequence) return;
    }
    this.#writeHistory(target.url, mode);
    this.#commit(target.url, target.match, loadError);
  }

  /** Bei Zurück und Vor hat der Browser die Adresse schon gewechselt; bleibt man, muss sie wieder stimmen. */
  async #mayLeave(url: URL, mode: Mode): Promise<boolean> {
    if (url.pathname === this.#state.path) return true;
    const leave = await unsaved.mayLeave();
    if (!leave && mode === 'pop') {
      const query = this.#state.query.toString();
      history.pushState(null, '', query ? `${this.#state.path}?${query}` : this.#state.path);
    }
    return leave;
  }

  /** Folgt Umleitungen, Anmeldepflicht und Pflichtschritten bis zu einer Seite (oder keiner). */
  async #resolve(start: URL, sequence: number): Promise<{ url: URL; match: RouteMatch<PageRoute> | null } | null> {
    let url = withoutLegacyHash(start);
    for (let hop = 0; hop < MAX_REDIRECTS; hop++) {
      const match = matchRoute(this.#routes, url.pathname);
      if (!match?.route.public && !this.#isAuthenticated()) {
        url = new URL(loginHref(url.pathname + url.search), location.origin);
        return { url, match: this.#pageAt('/login') };
      }
      const detour = match?.route.public ? null : this.#guard(url.pathname);
      if (detour) {
        url = new URL(detour, location.origin);
        continue;
      }
      if (!match || !isRedirect(match.route)) return { url, match: match as RouteMatch<PageRoute> | null };
      const destination = await redirectDestination(match.route, match.params);
      if (sequence !== this.#sequence) return null;
      if (destination === null) return { url, match: null };
      url = this.#redirectTarget(destination, url);
    }
    return { url, match: null };
  }

  #pageAt(path: string): RouteMatch<PageRoute> | null {
    const match = matchRoute(this.#routes, path);
    return match && !isRedirect(match.route) ? (match as RouteMatch<PageRoute>) : null;
  }

  /** Das Ziel einer Umleitung; die Query der alten Adresse bleibt, wenn das Ziel keine hat. */
  #redirectTarget(destination: string, from: URL): URL {
    const url = new URL(destination, location.origin);
    if (url.origin !== location.origin) return new URL('/', location.origin);
    if (!url.search) url.search = from.search;
    return url;
  }

  #writeHistory(url: URL, mode: Mode): void {
    const href = url.pathname + url.search + url.hash;
    if (href === currentHref()) return;
    if (mode === 'push') history.pushState(null, '', href);
    else history.replaceState(null, '', href);
  }

  #commit(url: URL, match: RouteMatch<PageRoute> | null, loadError: unknown): void {
    if (url.pathname !== this.#state.path) {
      this.#pageChange.abort();
      this.#pageChange = new AbortController();
    }
    this.#state = { path: url.pathname, query: url.searchParams, match, loadError };
    this.#notify();
  }

  #notify(): void {
    this.#listeners.forEach((listener) => listener());
  }

  #onClick(event: MouseEvent): void {
    if (event.defaultPrevented || event.button !== 0) return;
    if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    const anchor = event.composedPath().find((node): node is HTMLAnchorElement => node instanceof HTMLAnchorElement);
    if (!anchor || !isInternalLink(anchor)) return;
    event.preventDefault();
    void this.navigate(anchor.pathname + anchor.search + anchor.hash, { ask: true });
  }
}

export const router = new Router();
