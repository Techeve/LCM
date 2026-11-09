import { describe, expect, it } from 'vitest';
import { hasFilters, filtersFromQuery, queryPatch, resetPatch, sortFromTable, sortToTable, validStatus } from './servers-state';
import { serversUrl } from '../../api/servers';
import { toServersQuery } from './servers-state';

const parse = (query: string) => filtersFromQuery(new URLSearchParams(query));

describe('Filter aus der Adresse', () => {
  it('liest die Parameter der Dashboard-Kacheln', () => {
    expect(parse('status=red').status).toBe('red');
    expect(parse('incapable=1').incapable).toBe(true);
  });

  it('ordnet und bereinigt die Stufen', () => {
    expect(validStatus('red, yellow,red,foo')).toBe('yellow,red');
    expect(parse('status=foo').status).toBe('');
  });

  it('versteht den alten Parameter search', () => {
    expect(parse('search=web03').q).toBe('web03');
    expect(parse('q=db&search=web03').q).toBe('db');
  });

  it('verwirft ungültige Gruppe, Sortierung und Seite', () => {
    const filters = parse('group=abc&sort=cpu&page=-3');
    expect(filters.group).toBe('');
    expect(filters.sort).toBe('name');
    expect(filters.page).toBe(1);
  });

  it('nimmt Sortierung mit Vorzeichen an', () => {
    expect(parse('sort=-updates').sort).toBe('-updates');
    expect(parse('page=3').page).toBe(3);
  });
});

describe('Änderung der Adresse', () => {
  it('lässt Vorgaben aus der Adresse fallen und springt auf Seite 1', () => {
    expect(queryPatch({ status: '' })).toEqual({ status: null, page: null });
    expect(queryPatch({ q: 'web' })).toEqual({ q: 'web', search: null, page: null });
  });

  it('schreibt den Schalter als 1', () => {
    expect(queryPatch({ incapable: true }).incapable).toBe(1);
  });

  it('behält bei Seitenwechsel die Seite', () => {
    expect(queryPatch({ page: 2 })).toEqual({ page: 2 });
  });

  it('setzt zurück, behält aber die Sortierung', () => {
    const patch = resetPatch(parse('status=red&sort=-updates'));
    expect(patch.status).toBeNull();
    expect(patch.sort).toBe('-updates');
  });

  it('erkennt aktive Filter', () => {
    expect(hasFilters(parse(''))).toBe(false);
    expect(hasFilters(parse('sort=-updates'))).toBe(false);
    expect(hasFilters(parse('os=debian'))).toBe(true);
  });
});

describe('Sortierung der Tabelle', () => {
  it('übersetzt hin und zurück', () => {
    expect(sortFromTable({ key: 'updates', dir: 'desc' })).toBe('-updates');
    expect(sortFromTable({ key: 'kernel', dir: 'asc' })).toBe('kernel');
    expect(sortFromTable(null)).toBe('name');
    expect(sortToTable('-updates')).toEqual({ key: 'updates', dir: 'desc' });
    expect(sortToTable('name')).toBeNull();
    expect(sortToTable('-name')).toEqual({ key: 'name', dir: 'desc' });
  });
});

describe('Abfrage an die API', () => {
  it('lässt leere Filter weg und blättert in 100ern', () => {
    const url = serversUrl(toServersQuery(parse('status=red,yellow&q=web&page=2&incapable=1&group=3')));
    const params = new URL(url, 'http://x').searchParams;
    expect(params.get('status')).toBe('yellow,red');
    expect(params.get('q')).toBe('web');
    expect(params.get('group_id')).toBe('3');
    expect(params.get('incapable')).toBe('1');
    expect(params.get('offset')).toBe('100');
    expect(params.has('os')).toBe(false);
  });
});
