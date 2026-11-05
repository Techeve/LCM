// Was die Serverliste zeigt, steht in der Adresse (`/servers?status=red,yellow&q=web&sort=-updates`):
// ein kopierter Link zeigt dieselbe Ansicht. Diese Datei übersetzt zwischen Adresse,
// Filtern der Oberfläche und Abfrage an die API.

import type { SortState } from '../../components/lcm-data-table';
import { DEFAULT_SERVER_SORT, SERVER_SORTS, SERVER_STATUSES, type ServerSort, type ServersQuery } from '../../api/servers';
import type { QueryValue } from '../../lib/router';

export interface ServerFilters {
  q: string;
  /** Ampelstufen, kommagetrennt in fester Reihenfolge. */
  status: string;
  os: string;
  kernel: string;
  group: string;
  incapable: boolean;
  sort: string;
  page: number;
}

export const NO_FILTERS: ServerFilters = {
  q: '',
  status: '',
  os: '',
  kernel: '',
  group: '',
  incapable: false,
  sort: DEFAULT_SERVER_SORT,
  page: 1,
};

/** Nur bekannte Stufen, ohne Doppelte, in der Reihenfolge der Ampel. */
export function validStatus(value: string): string {
  const wanted = new Set(value.split(',').map((part) => part.trim()));
  return SERVER_STATUSES.filter((status) => wanted.has(status)).join(',');
}

/** `name`, `-name` usw.; alles andere ergibt die Vorgabe. */
export function validSort(value: string | null): string {
  if (!value) return DEFAULT_SERVER_SORT;
  const key = value.startsWith('-') ? value.slice(1) : value;
  return (SERVER_SORTS as readonly string[]).includes(key) ? value : DEFAULT_SERVER_SORT;
}

export function filtersFromQuery(query: URLSearchParams): ServerFilters {
  const page = Number.parseInt(query.get('page') ?? '1', 10);
  const group = query.get('group') ?? '';
  return {
    // `search` heißt der Parameter in alten Links (`/servers/by-name/…` ohne Treffer).
    q: (query.get('q') ?? query.get('search') ?? '').trim(),
    status: validStatus(query.get('status') ?? ''),
    os: (query.get('os') ?? '').trim(),
    kernel: (query.get('kernel') ?? '').trim(),
    group: /^\d+$/.test(group) ? group : '',
    incapable: query.get('incapable') === '1',
    sort: validSort(query.get('sort')),
    page: Number.isFinite(page) && page > 1 ? page : 1,
  };
}

/**
 * Query-Änderung für `router.setQuery`: Vorgaben entfallen aus der Adresse. Jede Änderung
 * außer der Seite springt zurück auf Seite 1.
 */
export function queryPatch(change: Partial<ServerFilters>): Record<string, QueryValue> {
  const patch: Record<string, QueryValue> = {};
  for (const [name, value] of Object.entries(change) as [keyof ServerFilters, ServerFilters[keyof ServerFilters]][]) {
    patch[name] = value === NO_FILTERS[name] ? null : value === true ? 1 : (value as QueryValue);
  }
  if (!('page' in change)) patch.page = null;
  if ('q' in change) patch.search = null;
  return patch;
}

/** Alles zurück auf die Vorgabe; nur die Sortierung bleibt. */
export function resetPatch(filters: ServerFilters): Record<string, QueryValue> {
  return queryPatch({ ...NO_FILTERS, sort: filters.sort });
}

export function hasFilters(filters: ServerFilters): boolean {
  return filters.q !== '' || filters.status !== '' || filters.os !== '' || filters.kernel !== '' || filters.group !== '' || filters.incapable;
}

export function toServersQuery(filters: ServerFilters): ServersQuery {
  return { ...filters };
}

// ---- Sortierung der Tabelle ----

/** Spalte und Richtung der Tabelle als Sortierung der API. */
export function sortFromTable(sort: SortState | null): string {
  if (!sort) return DEFAULT_SERVER_SORT;
  return sort.dir === 'desc' ? `-${sort.key}` : sort.key;
}

/** Anzeige der Sortierung in der Tabelle; die Vorgabe (Name aufsteigend) zeigt keinen Pfeil. */
export function sortToTable(sort: string): SortState | null {
  const desc = sort.startsWith('-');
  const key = (desc ? sort.slice(1) : sort) as ServerSort;
  if (key === DEFAULT_SERVER_SORT && !desc) return null;
  return { key, dir: desc ? 'desc' : 'asc' };
}
