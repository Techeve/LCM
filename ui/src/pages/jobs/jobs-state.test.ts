import { describe, expect, it } from 'vitest';
import {
  filtersFromQuery,
  hasFilters,
  NO_FILTERS,
  queryPatch,
  resetPatch,
  sortFromTable,
  sortToTable,
  toActiveQuery,
  toJobsQuery,
} from './jobs-state';

const params = (query: string) => new URLSearchParams(query);
const NOW = Date.parse('2026-11-01T12:07:30Z');

describe('filtersFromQuery', () => {
  it('has defaults for an empty address', () => {
    expect(filtersFromQuery(params(''))).toEqual(NO_FILTERS);
  });

  it('reads every filter', () => {
    const f = filtersFromQuery(params('status=failed,aborted&kind=update&server=4&range=custom&since=2026-10-01&until=2026-10-31&q=%20nacht%20&by=admin&health=1&sort=-duration&page=3'));
    expect(f).toEqual({
      status: 'failed,aborted',
      kind: 'update',
      server: '4',
      by: 'admin',
      range: 'custom',
      since: '2026-10-01',
      until: '2026-10-31',
      q: 'nacht',
      health: true,
      sort: '-duration',
      page: 3,
    });
  });

  it('drops nonsense instead of passing it on', () => {
    const f = filtersFromQuery(params('status=quatsch&server=abc&range=ever&since=gestern&sort=name&page=-2'));
    expect(f).toEqual(NO_FILTERS);
  });

  it('keeps the aliases', () => {
    expect(filtersFromQuery(params('status=problems')).status).toBe('problems');
    expect(filtersFromQuery(params('status=all')).status).toBe('all');
  });
});

describe('queryPatch', () => {
  it('removes defaults and resets the page', () => {
    expect(queryPatch({ status: '' })).toEqual({ status: null, page: null });
    expect(queryPatch({ status: 'problems' })).toEqual({ status: 'problems', page: null });
  });

  it('keeps the page when only the page changes', () => {
    expect(queryPatch({ page: 2 })).toEqual({ page: 2 });
    expect(queryPatch({ page: 1 })).toEqual({ page: null });
  });

  it('writes the health switch as 1', () => {
    expect(queryPatch({ health: true }).health).toBe(1);
    expect(queryPatch({ health: false }).health).toBeNull();
  });

  it('resets everything but the sort', () => {
    const patch = resetPatch({ ...NO_FILTERS, status: 'failed', sort: '-duration', q: 'x' });
    expect(patch.status).toBeNull();
    expect(patch.q).toBeNull();
    expect(patch.sort).toBe('-duration');
  });
});

describe('toJobsQuery', () => {
  it('shows finished jobs and hides health checks by default', () => {
    const q = toJobsQuery(NO_FILTERS, NOW);
    expect(q.status).toEqual(['success', 'failed', 'aborted', 'blocked']);
    expect(q.hideHealth).toBe(true);
    expect(q.offset).toBe(0);
  });

  it('passes the trigger on as is', () => {
    expect(toJobsQuery({ ...NO_FILTERS, by: 'scheduler' }, NOW).triggeredBy).toBe('scheduler');
    expect(toJobsQuery(NO_FILTERS, NOW).triggeredBy).toBe('');
  });

  it('shows health checks when asked, or when filtering for them', () => {
    expect(toJobsQuery({ ...NO_FILTERS, health: true }, NOW).hideHealth).toBe(false);
    expect(toJobsQuery({ ...NO_FILTERS, kind: 'health' }, NOW).hideHealth).toBe(false);
  });

  it('maps the status choices', () => {
    expect(toJobsQuery({ ...NO_FILTERS, status: 'problems' }, NOW).status).toEqual(['failed', 'aborted', 'blocked']);
    expect(toJobsQuery({ ...NO_FILTERS, status: 'all' }, NOW).status).toEqual([]);
    expect(toJobsQuery({ ...NO_FILTERS, status: 'running,pending' }, NOW).status).toEqual(['running', 'pending']);
  });

  it('rounds a relative range down so the address stays stable', () => {
    const f = { ...NO_FILTERS, range: '24h' };
    expect(toJobsQuery(f, NOW).since).toBe('2026-10-31T12:05:00.000Z');
    expect(toJobsQuery(f, NOW + 60_000).since).toBe(toJobsQuery(f, NOW).since);
  });

  it('covers the whole last day of a custom range', () => {
    const q = toJobsQuery({ ...NO_FILTERS, range: 'custom', since: '2026-10-01', until: '2026-10-31' }, NOW);
    expect(new Date(q.since).getDate()).toBe(1);
    expect(new Date(q.until).getHours()).toBe(23);
  });

  it('pages by offset', () => {
    expect(toJobsQuery({ ...NO_FILTERS, page: 3 }, NOW)).toMatchObject({ limit: 50, offset: 100 });
  });
});

describe('toActiveQuery', () => {
  it('asks for running and waiting jobs of the chosen server only', () => {
    const q = toActiveQuery({ ...NO_FILTERS, server: '2', status: 'failed', q: 'x' });
    expect(q).toMatchObject({ status: ['running', 'pending'], serverId: 2, q: '', hideHealth: true });
  });
});

describe('table sort', () => {
  it('maps in both directions', () => {
    expect(sortFromTable(null)).toBe('-started_at');
    expect(sortFromTable({ key: 'started', dir: 'asc' })).toBe('started_at');
    expect(sortFromTable({ key: 'started', dir: 'desc' })).toBe('-started_at');
    expect(sortFromTable({ key: 'duration', dir: 'asc' })).toBe('-duration');
    expect(sortToTable('-started_at')).toBeNull();
    expect(sortToTable('-duration')).toEqual({ key: 'duration', dir: 'desc' });
  });
});

describe('hasFilters', () => {
  it('ignores sort and page', () => {
    expect(hasFilters({ ...NO_FILTERS, sort: '-duration', page: 4 })).toBe(false);
    expect(hasFilters({ ...NO_FILTERS, q: 'a' })).toBe(true);
  });
});
