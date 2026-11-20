// Service Worker: nur Cache für die gehashten Dateien des aktuellen Builds,
// dazu der Cache des vorigen Builds für Tabs, die noch nicht neu geladen haben.
// Der Build (vite-plugins/sw.ts) setzt die Dateiliste und die Build-Kennung ein.

import { broadcast } from './lib/broadcast';
import { isBuildFile, isPrecachedRequest, outdatedCaches } from './lib/sw-rules';

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
  const others = (await caches.keys()).filter((name) => name !== CACHE_NAME);
  await Promise.all(outdatedCaches(others).map((name) => caches.delete(name)));
  await self.clients.claim();
  // Gab es keinen alten Build, ist es die Erstinstallation: kein Neuladen nötig.
  if (others.length > 0) broadcast({ type: 'new-version' });
}

async function fromCache(request: Request): Promise<Response> {
  const cache = await caches.open(CACHE_NAME);
  return (await cache.match(request)) ?? fetch(request);
}

/** Datei eines älteren Builds: aus dessen Cache, falls noch da. */
async function fromAnyCache(request: Request): Promise<Response> {
  return (await caches.match(request)) ?? fetch(request);
}

self.addEventListener('install', (event) => event.waitUntil(precache()));
self.addEventListener('activate', (event) => event.waitUntil(activate()));
self.addEventListener('fetch', (event) => {
  const { request } = event;
  if (isPrecachedRequest(request, self.location.origin, assets)) event.respondWith(fromCache(request));
  else if (isBuildFile(request, self.location.origin)) event.respondWith(fromAnyCache(request));
});
