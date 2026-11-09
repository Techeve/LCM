// Routentabelle und Zuordnung eines Pfades. Muster: `/servers/:id/:tab?` mit
// optionalem letzten Segment, `/docs/*` für beliebig viele Restsegmente
// (steht dann in `params['*']`).

export type Params = Record<string, string>;

interface RouteBase {
  path: string;
  /** Ohne Anmeldung erreichbar (Anmeldung, Doku, Aktivierung ...). */
  public?: boolean;
}

/** Zeigt ein Custom Element; das Modul wird erst beim Aufruf geladen. */
export interface PageRoute extends RouteBase {
  tag: string;
  load: () => Promise<unknown>;
}

/** Leitet auf einen anderen Pfad um (alte Adressen, Namenssuche). */
export interface RedirectRoute extends RouteBase {
  redirect: (params: Params) => string | Promise<string>;
}

export type Route = PageRoute | RedirectRoute;

export interface RouteMatch<R extends Route = Route> {
  route: R;
  params: Params;
}

export function isRedirect(route: Route): route is RedirectRoute {
  return 'redirect' in route;
}

function segments(path: string): string[] {
  return path.split('/').filter(Boolean);
}

function matchPattern(pattern: string, path: string): Params | null {
  const want = segments(pattern);
  const have = segments(path);
  const params: Params = {};
  for (const [index, part] of want.entries()) {
    if (part === '*') {
      params['*'] = have.slice(index).join('/');
      return params;
    }
    const optional = part.endsWith('?');
    if (index >= have.length) {
      if (optional) continue;
      return null;
    }
    if (!part.startsWith(':')) {
      if (part !== have[index]) return null;
      continue;
    }
    params[part.slice(1).replace(/\?$/, '')] = have[index];
  }
  return have.length <= want.length ? params : null; // zu viele Segmente passen nicht
}

function decodeParams(params: Params): Params | null {
  try {
    return Object.fromEntries(Object.entries(params).map(([name, value]) => [name, decodeURIComponent(value)]));
  } catch {
    return null; // ungültige %-Kodierung
  }
}

/** Erste passende Route, Reihenfolge der Tabelle entscheidet. */
export function matchRoute(routes: Route[], path: string): RouteMatch | null {
  for (const route of routes) {
    const raw = matchPattern(route.path, path);
    const params = raw && decodeParams(raw);
    if (params) return { route, params };
  }
  return null;
}
