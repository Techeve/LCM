// Welche Anfragen der Service Worker aus dem Cache bedient: nur GET auf
// gehashte Dateien des aktuellen Builds am eigenen Ursprung. Alles andere
// (index.html, /api/, /sw.js, Navigationen) geht direkt ans Netz.

export function isPrecachedRequest(
  request: { method: string; url: string },
  origin: string,
  assets: ReadonlySet<string>,
): boolean {
  if (request.method !== 'GET') return false;
  const url = new URL(request.url);
  return url.origin === origin && url.search === '' && assets.has(url.pathname);
}
