import { describe, expect, it } from 'vitest';
import type { Column } from '../../components/lcm-data-table';
import { shownRows } from './tab-packages';

type Row = { id: string; name: string };
const COLUMNS: Column<Row>[] = [{ key: 'name', label: 'Name', sortable: true }];

describe('Paketliste', () => {
  it('sortiert die ganze Liste, bevor sie abschneidet', () => {
    const rows = Array.from({ length: 300 }, (_, i) => ({ id: String(i), name: `pkg-${String(i).padStart(3, '0')}` }));
    const shown = shownRows(rows, COLUMNS, { key: 'name', dir: 'desc' }, 200);
    expect(shown).toHaveLength(200);
    expect(shown[0].name).toBe('pkg-299');
    expect(shown[199].name).toBe('pkg-100');
  });

  it('lässt die Reihenfolge ohne Sortierung, wie sie kommt', () => {
    const rows = [
      { id: 'b', name: 'b' },
      { id: 'a', name: 'a' },
    ];
    expect(shownRows(rows, COLUMNS, null, 1)).toEqual([{ id: 'b', name: 'b' }]);
  });
});
