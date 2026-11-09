import type { PageRoute, RouteMatch } from '../../lib/route-match';

export const DOCS_TAG = 'lcm-page-docs';

/** Die Seite aus der Adresse: erstes Segment hinter `/docs/`. */
export function slugFromPath(rest: string | undefined): string {
  return (rest ?? '').split('/')[0] ?? '';
}

/**
 * Die Doku-Seite zur aktuellen Route, `null`, wenn die Route nicht mehr die Doku ist. Beim Wechsel ins Hauptmenü
 * meldet der Router die neue Adresse noch an die alte Seite; ohne diese Prüfung hielte sie `/servers` für „Doku ohne
 * Seite“ und leitete zurück auf die erste Doku-Seite.
 */
export function docsSlug(match: RouteMatch<PageRoute> | null): string | null {
  if (match?.route.tag !== DOCS_TAG) return null;
  return slugFromPath(match.params['*']);
}
