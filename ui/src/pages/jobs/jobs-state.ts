// Was die Jobliste zeigt, steht in der Adresse (`/jobs?status=problems&server=3&q=nacht`):
// ein kopierter Link zeigt dieselbe Ansicht. Diese Datei übersetzt zwischen Adresse,
// Filtern der Oberfläche und Abfrage an die API.

import type { SortState } from '../../components/lcm-data-table';
import {
  ACTIVE_STATUSES,
  DEFAULT_SORT,
  FINISHED_STATUSES,
  PAGE_SIZE,
  PROBLEM_STATUSES,
  type JobSort,
  type JobsQuery,
} from '../../api/jobs';
import type { QueryValue } from '../../lib/router';

export const RANGE_CHOICES = ['', '1h', '24h', '7d', '30d', 'custom'] as const;

const RANGE_MS: Record<string, number> = { '1h': 3_600_000, '24h': 86_400_000, '7d': 7 * 86_400_000, '30d': 30 * 86_400_000 };
const SORTS: readonly JobSort[] = ['-started_at', 'started_at', '-duration'];
const DATE = /^\d{4}-\d{2}-\d{2}$/;
const SINCE_STEP_MS = 5 * 60_000;

export interface JobFilters {
  status: string;
  kind: string;
  server: string;
  /** Ausgelöst von (`triggered_by`). */
  by: string;
  range: string;
  since: string;
  until: string;
  q: string;
  /** Health-Checks mitzeigen (sonst ausgeblendet). */
  health: boolean;
  sort: JobSort;
  page: number;
}

export const NO_FILTERS: JobFilters = {
  status: '',
  kind: '',
  server: '',
  by: '',
  range: '',
  since: '',
  until: '',
  q: '',
  health: false,
  sort: DEFAULT_SORT,
  page: 1,
};

function validStatus(value: string): string {
  if (value === 'problems' || value === 'all') return value;
  const all: readonly string[] = [...ACTIVE_STATUSES, ...FINISHED_STATUSES];
  const parts = value.split(',').filter(Boolean);
  return parts.length > 0 && parts.every((part) => all.includes(part)) ? parts.join(',') : '';
}

function validDate(value: string | null): string {
  return value && DATE.test(value) ? value : '';
}

export function filtersFromQuery(query: URLSearchParams): JobFilters {
  const range = query.get('range') ?? '';
  const sort = query.get('sort') as JobSort | null;
  const page = Number.parseInt(query.get('page') ?? '1', 10);
  return {
    status: validStatus(query.get('status') ?? ''),
    kind: (query.get('kind') ?? '').split(',').filter(Boolean).join(','),
    server: /^\d+$/.test(query.get('server') ?? '') ? (query.get('server') as string) : '',
    by: (query.get('by') ?? '').trim(),
    range: (RANGE_CHOICES as readonly string[]).includes(range) ? range : '',
    since: validDate(query.get('since')),
    until: validDate(query.get('until')),
    q: (query.get('q') ?? '').trim(),
    health: query.get('health') === '1',
    sort: sort && SORTS.includes(sort) ? sort : DEFAULT_SORT,
    page: Number.isFinite(page) && page > 1 ? page : 1,
  };
}

/**
 * Query-Änderung für `router.setQuery`: Vorgaben entfallen aus der Adresse. Jede Änderung
 * außer der Seite springt zurück auf Seite 1.
 */
export function queryPatch(change: Partial<JobFilters>): Record<string, QueryValue> {
  const patch: Record<string, QueryValue> = {};
  for (const [name, value] of Object.entries(change) as [keyof JobFilters, JobFilters[keyof JobFilters]][]) {
    const standard = NO_FILTERS[name];
    patch[name] = value === standard ? null : (value as QueryValue);
  }
  if (patch.health === true) patch.health = 1;
  if (!('page' in change)) patch.page = null;
  return patch;
}

/** Alles zurück auf die Vorgabe; nur die Sortierung bleibt. */
export function resetPatch(filters: JobFilters): Record<string, QueryValue> {
  return queryPatch({ ...NO_FILTERS, sort: filters.sort });
}

export function hasFilters(filters: JobFilters): boolean {
  return (
    filters.status !== '' ||
    filters.kind !== '' ||
    filters.server !== '' ||
    filters.by !== '' ||
    filters.range !== '' ||
    filters.q !== '' ||
    filters.health
  );
}

export function statusesFor(choice: string): string[] {
  if (choice === '') return [...FINISHED_STATUSES];
  if (choice === 'problems') return [...PROBLEM_STATUSES];
  if (choice === 'all') return [];
  return choice.split(',');
}

function startOfDay(date: string): string {
  return new Date(`${date}T00:00:00`).toISOString();
}

function endOfDay(date: string): string {
  return new Date(`${date}T23:59:59.999`).toISOString();
}

/** Beginn des Zeitraums. Auf fünf Minuten abgerundet, damit die Adresse beim Rendern stabil bleibt. */
function sinceFor(filters: JobFilters, now: number): string {
  const span = RANGE_MS[filters.range];
  if (span) return new Date(Math.floor((now - span) / SINCE_STEP_MS) * SINCE_STEP_MS).toISOString();
  return filters.range === 'custom' && filters.since ? startOfDay(filters.since) : '';
}

function untilFor(filters: JobFilters): string {
  return filters.range === 'custom' && filters.until ? endOfDay(filters.until) : '';
}

function kinds(filters: JobFilters): string[] {
  return filters.kind.split(',').filter(Boolean);
}

export function toJobsQuery(filters: JobFilters, now: number = Date.now()): JobsQuery {
  const kind = kinds(filters);
  return {
    serverId: Number(filters.server) || undefined,
    status: statusesFor(filters.status),
    kind,
    q: filters.q,
    triggeredBy: filters.by,
    since: sinceFor(filters, now),
    until: untilFor(filters),
    hideHealth: !filters.health && !kind.includes('health'),
    sort: filters.sort,
    limit: PAGE_SIZE,
    offset: (filters.page - 1) * PAGE_SIZE,
  };
}

/** Die laufenden und wartenden Jobs; sie folgen nur Server und Health-Schalter, nicht den Verlaufsfiltern. */
export function toActiveQuery(filters: JobFilters): JobsQuery {
  return {
    serverId: Number(filters.server) || undefined,
    status: ACTIVE_STATUSES,
    kind: [],
    q: '',
    since: '',
    until: '',
    hideHealth: !filters.health,
    sort: '-started_at',
    limit: 100,
    offset: 0,
  };
}

// ---- Sortierung der Tabelle ----

/** Spalte und Richtung der Tabelle als Sortierung der API; „Dauer“ gibt es nur absteigend. */
export function sortFromTable(sort: SortState | null): JobSort {
  if (!sort) return DEFAULT_SORT;
  if (sort.key === 'duration') return '-duration';
  return sort.dir === 'asc' ? 'started_at' : '-started_at';
}

/** Anzeige der Sortierung in der Tabelle; die Vorgabe (neueste zuerst) zeigt keinen Pfeil. */
export function sortToTable(sort: JobSort): SortState | null {
  if (sort === '-duration') return { key: 'duration', dir: 'desc' };
  if (sort === 'started_at') return { key: 'started', dir: 'asc' };
  return null;
}
