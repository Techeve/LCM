import { describe, expect, it } from 'vitest';
import type { Column, Row } from '../../components/lcm-data-table';
import { pageQuery, pageSlice, parseView, sortQuery, sortRows } from './lu-paging';

describe('parseView', () => {
  it('liest Seite und Sortierung', () => {
    expect(parseView(new URLSearchParams('page=3&sort=-name'))).toEqual({ page: 3, sort: { key: 'name', dir: 'desc' } });
    expect(parseView(new URLSearchParams('sort=keys'))).toEqual({ page: 1, sort: { key: 'keys', dir: 'asc' } });
  });

  it('ignoriert ungültige Seiten', () => {
    expect(parseView(new URLSearchParams('page=0')).page).toBe(1);
    expect(parseView(new URLSearchParams('page=abc')).page).toBe(1);
    expect(parseView(new URLSearchParams('page=2.5')).page).toBe(1);
  });
});

describe('Query', () => {
  it('setzt beim Sortieren die Seite zurück', () => {
    expect(sortQuery({ key: 'name', dir: 'desc' })).toEqual({ sort: '-name', page: null });
    expect(sortQuery(null)).toEqual({ sort: null, page: null });
  });

  it('lässt Seite 1 aus der Adresse', () => {
    expect(pageQuery(1)).toEqual({ page: null });
    expect(pageQuery(4)).toEqual({ page: 4 });
  });
});

describe('sortRows', () => {
  const rows: Row[] = [
    { id: '1', name: 'web10', keys: 2 },
    { id: '2', name: 'web2', keys: 10 },
    { id: '3', name: 'Alpha', keys: 1 },
  ];
  const columns: Column[] = [
    { key: 'name', label: 'Name', sortable: true },
    { key: 'keys', label: 'Keys', sortable: true, value: (r) => r.keys as number },
    { key: 'note', label: 'Note' },
  ];

  it('sortiert Text natürlich und Zahlen numerisch', () => {
    expect(sortRows(rows, columns, { key: 'name', dir: 'asc' }).map((r) => r.id)).toEqual(['3', '2', '1']);
    expect(sortRows(rows, columns, { key: 'keys', dir: 'desc' }).map((r) => r.id)).toEqual(['2', '1', '3']);
  });

  it('lässt unbekannte oder nicht sortierbare Spalten in Ruhe', () => {
    expect(sortRows(rows, columns, { key: 'note', dir: 'asc' })).toBe(rows);
    expect(sortRows(rows, columns, null)).toBe(rows);
  });
});

describe('pageSlice', () => {
  const items = Array.from({ length: 23 }, (_, i) => i + 1);

  it('schneidet eine Seite aus', () => {
    expect(pageSlice(items, 2, 10)).toEqual({ rows: [11, 12, 13, 14, 15, 16, 17, 18, 19, 20], page: 2, from: 11, to: 20, total: 23 });
  });

  it('zeigt hinter dem Ende die letzte Seite', () => {
    const slice = pageSlice(items, 9, 10);
    expect(slice.page).toBe(3);
    expect(slice.rows).toEqual([21, 22, 23]);
  });

  it('kommt mit leeren Listen zurecht', () => {
    expect(pageSlice([], 1, 10)).toEqual({ rows: [], page: 1, from: 0, to: 0, total: 0 });
  });
});
