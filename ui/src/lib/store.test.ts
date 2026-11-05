import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { Store, urlMatches } from './store';

const fetchMock = vi.fn();

function respond(body: unknown, etag?: string): Response {
  return new Response(JSON.stringify(body), {
    headers: { 'Content-Type': 'application/json', ...(etag ? { ETag: etag } : {}) },
  });
}

beforeEach(() => vi.stubGlobal('fetch', fetchMock));
afterEach(() => {
  fetchMock.mockReset();
  vi.unstubAllGlobals();
});

describe('urlMatches', () => {
  it('matches by path with or without children', () => {
    expect(urlMatches('/api/v1/servers?status=red', '/api/v1/servers', false)).toBe(true);
    expect(urlMatches('/api/v1/servers/42/packages', '/api/v1/servers', false)).toBe(false);
    expect(urlMatches('/api/v1/servers/42/packages', '/api/v1/servers', true)).toBe(true);
    expect(urlMatches('/api/v1/servers/420', '/api/v1/servers/42', true)).toBe(false);
  });
});

describe('Store', () => {
  it('loads on first get and answers from memory afterwards while refreshing', async () => {
    const store = new Store();
    fetchMock.mockResolvedValueOnce(respond(['a']));
    expect(await store.get('/api/v1/servers')).toEqual(['a']);

    fetchMock.mockResolvedValueOnce(respond(['b']));
    const onData = vi.fn();
    store.subscribe('/api/v1/servers', onData);
    expect(await store.get('/api/v1/servers')).toEqual(['a']); // sofort der alte Stand
    await vi.waitFor(() => expect(onData).toHaveBeenCalledWith(['b']));
    expect(store.peek('/api/v1/servers')).toEqual(['b']);
  });

  it('keeps the cached value and stays quiet on 304', async () => {
    const store = new Store();
    fetchMock.mockResolvedValueOnce(respond(['a'], '"v1"'));
    await store.get('/api/v1/servers');
    const onData = vi.fn();
    store.subscribe('/api/v1/servers', onData);

    fetchMock.mockResolvedValueOnce(new Response(null, { status: 304 }));
    await store.refresh('/api/v1/servers');
    expect(new Headers(fetchMock.mock.calls[1][1].headers).get('If-None-Match')).toBe('"v1"');
    expect(onData).not.toHaveBeenCalled();
    expect(store.peek('/api/v1/servers')).toEqual(['a']);
  });

  it('refreshes only subscribed urls under the prefix on invalidate', async () => {
    const store = new Store();
    fetchMock.mockImplementation(async () => respond({}));
    await store.get('/api/v1/servers/1');
    await store.get('/api/v1/servers/2');
    await store.get('/api/v1/jobs');
    store.subscribe('/api/v1/servers/1', () => {});
    store.subscribe('/api/v1/jobs', () => {});
    fetchMock.mockClear();

    store.invalidate('/api/v1/servers');
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledOnce());
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/servers/1');
  });

  it('runs one more load when invalidated during a load', async () => {
    const store = new Store();
    const resolvers: Array<(r: Response) => void> = [];
    fetchMock.mockImplementation(() => new Promise<Response>((r) => resolvers.push(r)));
    const first = store.refresh('/api/v1/x');
    store.refresh('/api/v1/x').catch(() => {});
    expect(fetchMock).toHaveBeenCalledOnce();
    resolvers[0](respond(1));
    await first;
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
    resolvers[1](respond(2));
  });

  it('tells subscribers about background errors', async () => {
    const store = new Store();
    fetchMock.mockResolvedValueOnce(respond(1));
    await store.get('/api/v1/x');
    const onError = vi.fn();
    store.subscribe('/api/v1/x', () => {}, onError);
    fetchMock.mockRejectedValueOnce(new TypeError('offline'));
    await store.get('/api/v1/x');
    await vi.waitFor(() => expect(onError).toHaveBeenCalled());
  });

  it('forgets everything on clear and drops loads that were in flight', async () => {
    const store = new Store();
    fetchMock.mockResolvedValueOnce(respond('secret'));
    await store.get('/api/v1/x');

    let resolve!: (r: Response) => void;
    fetchMock.mockImplementationOnce(() => new Promise<Response>((r) => (resolve = r)));
    const pending = store.refresh('/api/v1/x').catch((e) => e);
    store.clear();
    resolve(respond('late'));
    expect(await pending).toBeInstanceOf(DOMException);
    expect(store.peek('/api/v1/x')).toBeUndefined();
  });

  it('never writes API data to browser storage', async () => {
    const setItem = vi.spyOn(Storage.prototype, 'setItem');
    const store = new Store();
    fetchMock.mockResolvedValueOnce(respond(['a']));
    await store.get('/api/v1/servers');
    expect(setItem).not.toHaveBeenCalled();
    setItem.mockRestore();
  });
});
