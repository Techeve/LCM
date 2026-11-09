import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { api, ApiError, endSessionOnUnload, hasSessionToken, onUnauthorized, setCsrfToken, setSessionToken } from './api';

const fetchMock = vi.fn();

function json(body: unknown, init: ResponseInit = {}): Response {
  return new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' }, ...init });
}

function sentHeaders(call = 0): Headers {
  return new Headers(fetchMock.mock.calls[call][1].headers);
}

beforeEach(() => {
  setCsrfToken(null);
  setSessionToken(null);
  vi.stubGlobal('fetch', fetchMock);
  document.cookie = '__Host-lcm_csrf=token%2B123; path=/; Secure';
});

afterEach(() => {
  fetchMock.mockReset();
  vi.unstubAllGlobals();
  document.cookie = '__Host-lcm_csrf=; path=/; max-age=0; Secure';
});

describe('requests', () => {
  it('sends the CSRF header of the cookie session on changing methods only, with same-origin credentials', async () => {
    fetchMock.mockImplementation(async () => json({}));
    await api.get('/api/v1/servers');
    await api.post('/api/v1/servers', { a: 1 });
    await api.put('/api/v1/x', {});
    await api.patch('/api/v1/x', {});
    await api.delete('/api/v1/x');

    expect(sentHeaders(0).has('X-CSRF-Token')).toBe(false);
    for (const call of [1, 2, 3, 4]) expect(sentHeaders(call).get('X-CSRF-Token')).toBe('token+123');
    expect(fetchMock.mock.calls[0][1]).toMatchObject({ credentials: 'same-origin', cache: 'no-store' });
  });

  it('sends JSON bodies with a content type and passes FormData through', async () => {
    fetchMock.mockImplementation(async () => json({}));
    await api.post('/api/v1/x', { name: 'a' });
    expect(fetchMock.mock.calls[0][1].body).toBe('{"name":"a"}');
    expect(sentHeaders(0).get('Content-Type')).toBe('application/json');

    const form = new FormData();
    await api.post('/api/v1/upload', form);
    expect(fetchMock.mock.calls[1][1].body).toBe(form);
    expect(sentHeaders(1).has('Content-Type')).toBe(false);
  });

  it('returns undefined for 204', async () => {
    fetchMock.mockResolvedValue(new Response(null, { status: 204 }));
    expect(await api.delete('/api/v1/x')).toBeUndefined();
  });
});

describe('errors', () => {
  it('maps the error body to ApiError', async () => {
    fetchMock.mockResolvedValue(
      json({ error: 'server not found', code: 'server.not_found', params: { server: 'web03' } }, { status: 404 }),
    );
    const error = await api.get('/api/v1/servers/9').catch((e) => e);
    expect(error).toBeInstanceOf(ApiError);
    expect(error).toMatchObject({ status: 404, code: 'server.not_found', params: { server: 'web03' }, message: 'server not found' });
  });

  it('copes with bodies that are not JSON', async () => {
    fetchMock.mockResolvedValue(new Response('<html>', { status: 502, statusText: 'Bad Gateway' }));
    const error = await api.get('/api/v1/x').catch((e) => e);
    expect(error).toMatchObject({ status: 502, code: '', params: {} });
  });

  it('turns network failures into code "network"', async () => {
    fetchMock.mockRejectedValue(new TypeError('Failed to fetch'));
    const error = await api.get('/api/v1/x').catch((e) => e);
    expect(error).toMatchObject({ status: 0, code: 'network' });
  });

  it('lets aborts through unchanged', async () => {
    fetchMock.mockRejectedValue(new DOMException('aborted', 'AbortError'));
    const error = await api.get('/api/v1/x', { signal: new AbortController().signal }).catch((e) => e);
    expect(error).toBeInstanceOf(DOMException);
  });

  it('reports 401 unless the call was anonymous', async () => {
    fetchMock.mockImplementation(async () => json({ code: 'auth.required' }, { status: 401 }));
    const listener = vi.fn();
    const off = onUnauthorized(listener);
    await api.get('/api/v1/servers').catch(() => {});
    expect(listener).toHaveBeenCalledOnce();
    await api.get('/api/v1/auth/me', { anonymous: true }).catch(() => {});
    expect(listener).toHaveBeenCalledOnce();
    off();
  });
});

describe('dedupe and ETag', () => {
  it('shares one request between identical GETs in flight', async () => {
    let resolve!: (response: Response) => void;
    fetchMock.mockImplementation(() => new Promise<Response>((r) => (resolve = r)));
    const first = api.get('/api/v1/servers');
    const second = api.get('/api/v1/servers');
    resolve(json([1]));
    expect(await first).toEqual([1]);
    expect(await second).toEqual([1]);
    expect(fetchMock).toHaveBeenCalledOnce();

    fetchMock.mockImplementation(async () => json([2]));
    expect(await api.get('/api/v1/servers')).toEqual([2]);
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it('does not share requests that carry their own signal', async () => {
    fetchMock.mockImplementation(async () => json({}));
    const signal = new AbortController().signal;
    await Promise.all([api.get('/api/v1/x', { signal }), api.get('/api/v1/x', { signal })]);
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it('sends If-None-Match and reports 304 as notModified', async () => {
    fetchMock.mockResolvedValueOnce(json([1], { headers: { 'Content-Type': 'application/json', ETag: '"v1"' } }));
    const first = await api.getWithEtag('/api/v1/servers', null);
    expect(first).toMatchObject({ data: [1], etag: '"v1"', notModified: false });

    fetchMock.mockResolvedValueOnce(new Response(null, { status: 304 }));
    const second = await api.getWithEtag('/api/v1/servers', '"v1"');
    expect(second).toMatchObject({ etag: '"v1"', notModified: true });
    expect(sentHeaders(1).get('If-None-Match')).toBe('"v1"');
  });
});

describe('CSRF self-healing', () => {
  const csrfError = () => json({ error: 'csrf', code: 'auth.csrf_invalid' }, { status: 403 });

  it('refetches /auth/me after auth.csrf_invalid and retries once with the fresh token', async () => {
    fetchMock
      .mockResolvedValueOnce(csrfError())
      .mockResolvedValueOnce(json({ csrf_token: 'fresh' }))
      .mockResolvedValueOnce(json({ ok: true }));
    expect(await api.post('/api/v1/servers', { a: 1 })).toEqual({ ok: true });

    expect(fetchMock.mock.calls.map((call) => `${call[1].method} ${call[0]}`)).toEqual([
      'POST /api/v1/servers',
      'GET /api/v1/auth/me',
      'POST /api/v1/servers',
    ]);
    expect(sentHeaders(0).get('X-CSRF-Token')).toBe('token+123');
    expect(sentHeaders(2).get('X-CSRF-Token')).toBe('fresh');
    expect(fetchMock.mock.calls[2][1].body).toBe('{"a":1}');
  });

  it('keeps using the fresh token afterwards', async () => {
    fetchMock.mockResolvedValue(json({}));
    setCsrfToken('from-me');
    await api.delete('/api/v1/x');
    expect(sentHeaders(0).get('X-CSRF-Token')).toBe('from-me');
  });

  it('gives up after one retry', async () => {
    fetchMock.mockImplementation(async (url: string) => (url.endsWith('/auth/me') ? json({ csrf_token: 'fresh' }) : csrfError()));
    const error = await api.post('/api/v1/servers', {}).catch((e) => e);
    expect(error).toMatchObject({ status: 403, code: 'auth.csrf_invalid' });
    expect(fetchMock).toHaveBeenCalledTimes(3);
  });

  it('reports the original error when /auth/me fails', async () => {
    fetchMock.mockResolvedValueOnce(csrfError()).mockResolvedValueOnce(json({ code: 'auth.required' }, { status: 401 }));
    const error = await api.post('/api/v1/servers', {}).catch((e) => e);
    expect(error).toMatchObject({ status: 403, code: 'auth.csrf_invalid' });
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it('does not retry other 403 answers or GET requests', async () => {
    fetchMock.mockResolvedValue(json({ code: 'auth.forbidden' }, { status: 403 }));
    await api.post('/api/v1/servers', {}).catch(() => {});
    expect(fetchMock).toHaveBeenCalledOnce();

    fetchMock.mockReset();
    fetchMock.mockResolvedValue(csrfError());
    await api.get('/api/v1/servers').catch(() => {});
    expect(fetchMock).toHaveBeenCalledOnce();
  });
});

describe('memory session', () => {
  it('sends the session token as X-LCM-Session, and no CSRF token', async () => {
    fetchMock.mockImplementation(async () => json({}));
    setSessionToken('geheim-1');
    setCsrfToken('wird-nicht-gebraucht');
    await api.get('/api/v1/servers');
    await api.post('/api/v1/servers', {});
    for (const call of [0, 1]) {
      expect(sentHeaders(call).get('X-LCM-Session')).toBe('geheim-1');
      expect(sentHeaders(call).has('X-CSRF-Token')).toBe(false);
    }
    expect(sentHeaders(0).has('Authorization')).toBe(false);
  });

  it('sends nothing without a session', async () => {
    fetchMock.mockImplementation(async () => json({}));
    setCsrfToken(null);
    document.cookie = '__Host-lcm_csrf=; path=/; max-age=0; Secure';
    await api.get('/api/v1/auth/me');
    await api.post('/api/v1/auth/login', {}, { anonymous: true });
    for (const call of [0, 1]) {
      expect(sentHeaders(call).has('X-LCM-Session')).toBe(false);
      expect(sentHeaders(call).has('X-CSRF-Token')).toBe(false);
    }
  });

  it('keeps the token in the module only, never in web storage', async () => {
    setSessionToken('geheim-2');
    expect(hasSessionToken()).toBe(true);
    for (const storage of [localStorage, sessionStorage]) {
      const values = Object.keys(storage).map((key) => storage.getItem(key) ?? '');
      expect(values.some((value) => value.includes('geheim-2'))).toBe(false);
    }
    expect(document.cookie).not.toContain('geheim-2');
    setSessionToken(null);
    expect(hasSessionToken()).toBe(false);
  });

  it('ends the session with a keepalive logout on pagehide, and does nothing without a token', () => {
    fetchMock.mockResolvedValue(json({}));
    endSessionOnUnload();
    expect(fetchMock).not.toHaveBeenCalled();

    setSessionToken('geheim-3');
    endSessionOnUnload();
    expect(fetchMock).toHaveBeenCalledOnce();
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('/api/v1/auth/logout');
    expect(init).toMatchObject({ method: 'POST', keepalive: true });
    expect(new Headers(init.headers).get('X-LCM-Session')).toBe('geheim-3');
  });
});

describe('download', () => {
  it('fetches the file with the session and returns the blob', async () => {
    fetchMock.mockResolvedValue(new Response('archiv', { status: 200 }));
    setSessionToken('geheim-4');
    const blob = await api.download('/api/v1/system/backups/a.tar/download');
    expect(await blob.text()).toBe('archiv');
    expect(sentHeaders(0).get('X-LCM-Session')).toBe('geheim-4');
  });

  it('turns an error answer into ApiError', async () => {
    fetchMock.mockResolvedValue(json({ code: 'backup.not_found' }, { status: 404 }));
    expect(await api.download('/api/v1/x').catch((e) => e)).toMatchObject({ status: 404, code: 'backup.not_found' });
  });
});

describe('stream', () => {
  const sse = (...chunks: string[]) =>
    new Response(
      new ReadableStream({
        start(controller) {
          for (const chunk of chunks) controller.enqueue(new TextEncoder().encode(chunk));
          controller.close();
        },
      }),
      { status: 200, headers: { 'Content-Type': 'text/event-stream' } },
    );

  it('reads events across chunk boundaries and sends the session', async () => {
    fetchMock.mockResolvedValue(sse('event: log\ndata: {"a"', ':1}\n\n: ping\n\nevent: ping\ndata: {}\n\n'));
    setSessionToken('geheim-5');
    const seen: Array<[string, string]> = [];
    await api.stream('/api/v1/system/logs/stream', (event) => seen.push([event.event, event.data]), {});
    expect(seen).toEqual([
      ['log', '{"a":1}'],
      ['ping', '{}'],
    ]);
    expect(sentHeaders(0).get('X-LCM-Session')).toBe('geheim-5');
    expect(sentHeaders(0).get('Accept')).toBe('text/event-stream');
  });

  it('rejects with ApiError when the server refuses', async () => {
    fetchMock.mockResolvedValue(json({ code: 'auth.forbidden' }, { status: 403 }));
    expect(await api.stream('/api/v1/x', () => {}, {}).catch((e) => e)).toMatchObject({ status: 403 });
  });
});
