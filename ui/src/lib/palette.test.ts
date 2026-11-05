import { describe, expect, it, vi } from 'vitest';
import { filterItems, PaletteRegistry, type PaletteItem } from './palette';

const item = (id: string, label: string, extra: Partial<PaletteItem> = {}): PaletteItem => ({ id, label, run: () => {}, ...extra });

describe('filterItems', () => {
  const items = [item('a', 'Linux-Benutzer'), item('b', 'Benutzer'), item('c', 'Alarme'), item('d', 'Zurück')];

  it('returns everything for an empty query', () => {
    expect(filterItems(items, '  ')).toEqual(items);
  });

  it('ignores case and accents', () => {
    expect(filterItems(items, 'ZURUCK').map((i) => i.id)).toEqual(['d']);
    expect(filterItems([item('x', 'Café')], 'cafe')).toHaveLength(1);
  });

  it('ranks names that start with the query before word starts and substrings', () => {
    expect(filterItems(items, 'benutzer').map((i) => i.id)).toEqual(['b', 'a']);
    expect(filterItems([item('1', 'xxbenutzer'), item('2', 'benutzer')], 'benutzer').map((i) => i.id)).toEqual(['2', '1']);
  });

  it('keeps the original order for equal ranks', () => {
    expect(filterItems([item('1', 'Alpha'), item('2', 'Alarm')], 'al').map((i) => i.id)).toEqual(['1', '2']);
  });

  it('needs every word of the query', () => {
    const servers = [item('1', 'web01', { hint: '10.10.0.11' }), item('2', 'web02', { hint: '10.10.0.12' })];
    expect(filterItems(servers, 'web 0.12').map((i) => i.id)).toEqual(['2']);
    expect(filterItems(servers, 'web db')).toEqual([]);
  });

  it('finds items by keywords and ranks them after name matches', () => {
    const list = [item('1', 'Hell', { keywords: ['licht'] }), item('2', 'Lichtschalter')];
    expect(filterItems(list, 'licht').map((i) => i.id)).toEqual(['2', '1']);
  });
});

describe('PaletteRegistry', () => {
  it('collects the commands of all owners', () => {
    const registry = new PaletteRegistry();
    registry.register({}, [item('1', 'Pakete aktualisieren')]);
    registry.register({ modal: true }, [item('2', 'Terminal öffnen')]);
    expect(registry.items.map((i) => i.id)).toEqual(['1', '2']);
  });

  it('replaces the commands of an owner that registers again', () => {
    const registry = new PaletteRegistry();
    const owner = {};
    registry.register(owner, [item('1', 'Alt')]);
    registry.register(owner, [item('2', 'Neu')]);
    expect(registry.items.map((i) => i.id)).toEqual(['2']);
  });

  it('removes one owner or all of them on a page change', () => {
    const registry = new PaletteRegistry();
    const [a, b] = [{}, {}];
    registry.register(a, [item('1', 'A')]);
    registry.register(b, [item('2', 'B')]);
    registry.unregister(a);
    expect(registry.items.map((i) => i.id)).toEqual(['2']);
    registry.clear();
    expect(registry.items).toEqual([]);
  });

  it('notifies listeners about changes, but not about no-ops', () => {
    const registry = new PaletteRegistry();
    const listener = vi.fn();
    const off = registry.onChange(listener);
    registry.clear();
    registry.unregister({});
    expect(listener).not.toHaveBeenCalled();
    registry.register({}, [item('1', 'A')]);
    registry.clear();
    expect(listener).toHaveBeenCalledTimes(2);
    off();
    registry.register({}, []);
    expect(listener).toHaveBeenCalledTimes(2);
  });
});
