// Welche Anfragen der Service Worker aus dem Cache bedient: nur GET auf
// gehashte Dateien am eigenen Ursprung, vor allem die des aktuellen Builds.
// Alles andere (index.html, /api/, /sw.js, Navigationen) geht direkt ans Netz.

export function isPrecachedRequest(
  request: { method: string; url: string },
  origin: string,
  assets: ReadonlySet<string>,
): boolean {
  if (request.method !== 'GET') return false;
  const url = new URL(request.url);
  return url.origin === origin && url.search === '' && assets.has(url.pathname);
}

/** Eine gehashte Datei irgendeines Builds. Ein Tab, der nach einem Update noch den vorigen Build zeigt, lädt sie nach. */
export function isBuildFile(request: { method: string; url: string }, origin: string): boolean {
  if (request.method !== 'GET') return false;
  const url = new URL(request.url);
  return url.origin === origin && url.search === '' && url.pathname.startsWith('/assets/');
}

/**
 * Welche Caches älterer Builds beim Aktivieren weg dürfen: alle bis auf den jüngsten (`others` in Anlage-Reihenfolge,
 * wie `caches.keys()` sie liefert). Den jüngsten braucht ein Tab, der noch nicht neu geladen hat (offenes Formular,
 * Ansage vor dem Neuladen): Er holt seine Seiten erst beim Navigieren, und der Server kennt die alten Dateien nicht mehr.
 * Weg ist er mit dem nächsten Update.
 */
export function outdatedCaches(others: readonly string[]): string[] {
  return others.slice(0, -1);
}
