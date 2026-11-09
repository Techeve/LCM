// Sortierung und Seiten der Tabellen auf der Seite Linux-Benutzer. Beides steht in der Adresse
// (`?sort=-name&page=2`), damit Zurück und Links die Ansicht wiederherstellen.

import type { Column, SortState } from '../../components/lcm-data-table';
import type { QueryValue } from '../../lib/router';

/** So viele Zeilen samt Blätterleiste passen bei 900 px Höhe ohne Scrollen auf den Schirm. */
export const PAGE_SIZE = 12;

export interface TableView {
  page: number;
  sort: SortState | null;
}

export interface PageSlice<T> {
  rows: T[];
  page: number;
  from: number;
  to: number;
  total: number;
}

const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' });

export function parseView(query: URLSearchParams): TableView {
  const page = Number(query.get('page'));
  const raw = query.get('sort') ?? '';
  const key = raw.replace(/^-/, '');
  return {
    page: Number.isInteger(page) && page > 1 ? page : 1,
    sort: key ? { key, dir: raw.startsWith('-') ? 'desc' : 'asc' } : null,
  };
}

/** Neue Sortierung: zurück auf Seite 1. */
export function sortQuery(sort: SortState | null): Record<string, QueryValue> {
  return { sort: sort ? `${sort.dir === 'desc' ? '-' : ''}${sort.key}` : null, page: null };
}

export function pageQuery(page: number): Record<string, QueryValue> {
  return { page: page > 1 ? page : null };
}

/** Sortiert wie `lcm-data-table`: eigener Sortierwert, sonst `value`, sonst das Feld der Zeile. */
export function sortRows<T extends { id: string | number }>(rows: T[], columns: Column<T>[], sort: SortState | null): T[] {
  const column = sort ? columns.find((c) => c.key === sort.key && c.sortable) : undefined;
  if (!sort || !column) return rows;
  const pick = column.sortValue ?? column.value ?? ((row: T) => (row as Record<string, unknown>)[column.key] as string | number);
  const factor = sort.dir === 'asc' ? 1 : -1;
  return [...rows].sort((a, b) => {
    const x = pick(a);
    const y = pick(b);
    return factor * (typeof x === 'number' && typeof y === 'number' ? x - y : collator.compare(String(x ?? ''), String(y ?? '')));
  });
}

/** Die Zeilen einer Seite; eine Seite hinter dem Ende zeigt die letzte. */
export function pageSlice<T>(rows: T[], page: number, size = PAGE_SIZE): PageSlice<T> {
  const last = Math.max(1, Math.ceil(rows.length / size));
  const current = Math.min(Math.max(1, page), last);
  const start = (current - 1) * size;
  const slice = rows.slice(start, start + size);
  return { rows: slice, page: current, from: slice.length ? start + 1 : 0, to: start + slice.length, total: rows.length };
}
