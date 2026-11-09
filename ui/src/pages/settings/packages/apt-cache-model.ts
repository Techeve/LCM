// APT-Cache: Formular eines Caches und die Zuordnung der Server zu ihrem Cache.

import type { AptCache, ServerLink } from '../../../api/settings-more';
import { LIMITS, charCount, hasControlChars } from '../../../lib/validate';

export interface CacheForm {
  name: string;
  url: string;
}

export type CacheErrors = Partial<Record<keyof CacheForm, string>>;

export function validateCacheForm(form: CacheForm): CacheErrors {
  const errors: CacheErrors = {};
  const name = form.name.trim();
  if (!name) errors.name = 'settings.aptCache.form.nameRequired';
  else if (charCount(name) > LIMITS.name) errors.name = 'check.tooLong';
  else if (hasControlChars(name) || /["'`\\]/.test(name)) errors.name = 'settings.aptCache.form.nameInvalid';
  const url = form.url.trim();
  // Wie das Backend: http(s) mit Host; Anführungszeichen, Leerzeichen und $( landen sonst in apt-Dateien.
  if (!/^https?:\/\/[^\s/"'`\\]+[^\s"'`\\]*$/.test(url) || url.includes('$(') || charCount(url) > LIMITS.path) errors.url = 'settings.aptCache.form.urlInvalid';
  return errors;
}

export function cacheBody(form: CacheForm): Omit<AptCache, 'id'> {
  return { name: form.name.trim(), url: form.url.trim() };
}

/** Server, die ihre APT-Anfragen über einen Cache leiten. */
export function connectedServers(servers: ServerLink[]): ServerLink[] {
  return servers.filter((server) => server.apt_proxy_active);
}

/** Wie viele Server über diesen Cache laufen (Abgleich über die Adresse, wie im Backend). */
export function connectedCount(servers: ServerLink[], cache: AptCache): number {
  return connectedServers(servers).filter((server) => sameUrl(server.apt_proxy_url, cache.url)).length;
}

export function cacheNameFor(caches: AptCache[], url: string): string {
  return caches.find((cache) => sameUrl(cache.url, url))?.name ?? url;
}

function sameUrl(a: string, b: string): boolean {
  return a.replace(/\/+$/, '') === b.replace(/\/+$/, '');
}

/** Der Cache, dessen Statistik gezeigt wird: der aus der Adresse, sonst der erste. */
export function selectedCache(caches: AptCache[], raw: string | null): AptCache | undefined {
  const id = Number(raw);
  return caches.find((cache) => cache.id === id) ?? caches[0];
}
