// Service Worker: nur Cache für die gehashten Dateien des aktuellen Builds.
// Der Build (vite-plugins/sw.ts) setzt die Dateiliste und die Build-Kennung ein.

import { broadcast } from './lib/broadcast';
import { isPrecachedRequest } from './lib/sw-rules';

declare const self: ServiceWorkerGlobalScope;
declare const __BUILD_ID__: string;
declare const __ASSETS__: string[];

const CACHE_NAME = `lcm-${__BUILD_ID__}`;
const assets = new Set(__ASSETS__);

async function precache(): Promise<void> {
  const cache = await caches.open(CACHE_NAME);
  await cache.addAll(__ASSETS__);
  await self.skipWaiting();
}

async function activate(): Promise<void> {
  const stale = (await caches.keys()).filter((name) => name !== CACHE_NAME);
  await Promise.all(stale.map((name) => caches.delete(name)));
  await self.clients.claim();
  // Gab es keinen alten Build, ist es die Erstinstallation: kein Neuladen nötig.
  if (stale.length > 0) broadcast({ type: 'new-version' });
}

async function fromCache(request: Request): Promise<Response> {
  const cache = await caches.open(CACHE_NAME);
  return (await cache.match(request)) ?? fetch(request);
}

self.addEventListener('install', (event) => event.waitUntil(precache()));
self.addEventListener('activate', (event) => event.waitUntil(activate()));
self.addEventListener('fetch', (event) => {
  if (isPrecachedRequest(event.request, self.location.origin, assets)) event.respondWith(fromCache(event.request));
});
