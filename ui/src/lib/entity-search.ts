// Server und Gruppen für die Befehlspalette. Die Listen werden einmal in den
// Datenspeicher geladen und im Tab gefiltert: So antwortet die Palette ohne
// Verzögerung bei jedem Tastendruck.

import { filterItems } from './palette';
import { store } from './store';

export const SERVERS_URL = '/api/v1/servers?limit=1000';
export const GROUPS_URL = '/api/v1/server-groups';
export const MAX_HITS_PER_KIND = 8;

interface EntityRef {
  id: number;
  name: string;
  host?: string;
}

export interface EntityHit {
  id: number;
  label: string;
  hint?: string;
}

/** Lädt die Listen, soweit der Benutzer sie lesen darf. Ein Fehler lässt die Palette einfach weniger zeigen. */
export async function loadEntities(can: (permission: string) => boolean): Promise<void> {
  await Promise.allSettled([
    can('servers:read') ? store.get(SERVERS_URL) : undefined,
    can('groups:read') ? store.get(GROUPS_URL) : undefined,
  ]);
}

function matches(url: string, query: string): EntityHit[] {
  const list = store.peek<{ items?: EntityRef[] }>(url)?.items;
  if (!Array.isArray(list)) return [];
  const hits = list.map((entity) => ({ id: entity.id, label: entity.name, hint: entity.host }));
  return filterItems(hits, query).slice(0, MAX_HITS_PER_KIND);
}

/** Server (nach Name oder Host) und Gruppen (nach Name), die zur Eingabe passen. */
export function findEntities(query: string): { servers: EntityHit[]; groups: EntityHit[] } {
  if (query.trim() === '') return { servers: [], groups: [] };
  return { servers: matches(SERVERS_URL, query), groups: matches(GROUPS_URL, query) };
}
