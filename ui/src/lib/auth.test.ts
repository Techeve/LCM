import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('./router', () => ({ router: { navigate: vi.fn(async () => {}), reevaluate: vi.fn(async () => {}) } }));
vi.mock('./events', () => ({
  events: { start: vi.fn(), stop: vi.fn(), onAuthLost: vi.fn(() => () => {}) },
}));

const { Auth } = await import('./auth');
const { router } = await import('./router');
const { events } = await import('./events');
const { store } = await import('./store');
const { i18n } = await import('./i18n');
const { onUnauthorized, hasSessionToken, setSessionToken } = await import('./api');

const me = {
  id: 1,
  username: 'tony',
  roles: ['admin'],
  permissions: ['servers:read', 'jobs:read'],
  totp_enabled: false,
  must_change_password: false,
  must_setup_2fa: false,
  csrf_token: 'abc',
  session_mode: 'cookie',
  language: 'de',
};
const memoryMe = { ...me, csrf_token: undefined, session_mode: 'memory' };

const fetchMock = vi.fn();
const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });

function route(handlers: Record<string, () => Response>) {
  fetchMock.mockImplementation(async (url: string, init?: RequestInit) => {
    const handler = handlers[`${init?.method ?? 'GET'} ${url}`];
    return handler ? handler() : json({ code: 'test.unhandled' }, 500);
  });
}

beforeEach(async () => {
  vi.stubGlobal('fetch', fetchMock);
  localStorage.clear();
  await i18n.setLanguage('de');
});

afterEach(() => {
  setSessionToken(null);
  fetchMock.mockReset();
  vi.clearAllMocks();
  vi.unstubAllGlobals();
});

describe('Auth', () => {
  it('loads the user, answers can() and starts the event channel', async () => {
    route({ 'GET /api/v1/auth/me': () => json(me) });
    const auth = new Auth();
    await auth.start();
    expect(auth.isAuthenticated).toBe(true);
    expect(auth.can('servers:read')).toBe(true);
    expect(auth.can('settings')).toBe(false);
    expect(events.start).toHaveBeenCalled();
  });

  it('stays logged out on 401 without treating it as an expired session', async () => {
    route({ 'GET /api/v1/auth/me': () => json({ code: 'auth.required' }, 401) });
    const expired = vi.fn();
    const off = onUnauthorized(expired);
    const auth = new Auth();
    await auth.start();
    expect(auth.isAuthenticated).toBe(false);
    expect(auth.can('servers:read')).toBe(false);
    expect(expired).not.toHaveBeenCalled();
    off();
  });

  it('takes the language of the account', async () => {
    route({ 'GET /api/v1/auth/me': () => json({ ...me, language: 'en' }) });
    await new Auth().start();
    expect(i18n.language).toBe('en');
  });

  it('asks for the second factor when the server demands it', async () => {
    route({
      'POST /api/v1/auth/login': () => json({ twofa_required: true, challenge: 'c1' }),
      'POST /api/v1/auth/login/2fa': () => json({}),
      'GET /api/v1/auth/me': () => json(me),
    });
    const auth = new Auth();
    expect(await auth.login('tony', 'pw')).toEqual({ kind: 'two-factor', challenge: 'c1' });
    expect(auth.isAuthenticated).toBe(false);
    await auth.loginTwoFactor('c1', '123456');
    expect(auth.isAuthenticated).toBe(true);
    const body = JSON.parse(fetchMock.mock.calls[1][1].body);
    expect(body).toEqual({ challenge: 'c1', code: '123456' });
  });

  it('logs in in one step when no second factor is needed', async () => {
    route({ 'POST /api/v1/auth/login': () => json({}), 'GET /api/v1/auth/me': () => json(me) });
    const auth = new Auth();
    expect(await auth.login('tony', 'pw')).toEqual({ kind: 'done' });
    expect(auth.user?.username).toBe('tony');
  });

  it('logs out a cookie session everywhere: server, other tabs, store, events, then the login page', async () => {
    route({ 'GET /api/v1/auth/me': () => json(me), 'POST /api/v1/auth/logout': () => new Response(null, { status: 204 }) });
    const auth = new Auth();
    await auth.start();
    const otherTab = new BroadcastChannel('lcm');
    const received = new Promise((resolve) => (otherTab.onmessage = (event) => resolve(event.data)));
    const clear = vi.spyOn(store, 'clear');

    await auth.logout();

    expect(await received).toEqual({ type: 'logout' });
    expect(clear).toHaveBeenCalled();
    expect(events.stop).toHaveBeenCalled();
    expect(router.navigate).toHaveBeenCalledWith('/login', { replace: true });
    expect(auth.isAuthenticated).toBe(false);
    otherTab.close();
  });

  it('keeps the session token of the login response in memory and sends it afterwards', async () => {
    route({
      'POST /api/v1/auth/login': () => json({ session_token: 'tab-wert' }),
      'GET /api/v1/auth/me': () => json(memoryMe),
    });
    const auth = new Auth();
    await auth.login('tony', 'pw');
    expect(auth.sessionMode).toBe('memory');
    expect(new Headers(fetchMock.mock.calls[1][1].headers).get('X-LCM-Session')).toBe('tab-wert');
    expect(hasSessionToken()).toBe(true);
    for (const storage of [localStorage, sessionStorage]) {
      expect(Object.keys(storage).map((key) => storage.getItem(key)).join()).not.toContain('tab-wert');
    }
  });

  it('takes the token of the second login step too', async () => {
    route({
      'POST /api/v1/auth/login/2fa': () => json({ session_token: 'zweiter-schritt' }),
      'GET /api/v1/auth/me': () => json(memoryMe),
    });
    await new Auth().loginTwoFactor('c1', '123456');
    expect(new Headers(fetchMock.mock.calls[1][1].headers).get('X-LCM-Session')).toBe('zweiter-schritt');
  });

  it('logs out a memory session without telling the other tabs, and drops the token', async () => {
    route({
      'POST /api/v1/auth/login': () => json({ session_token: 'tab-wert' }),
      'GET /api/v1/auth/me': () => json(memoryMe),
      'POST /api/v1/auth/logout': () => new Response(null, { status: 204 }),
    });
    const auth = new Auth();
    await auth.login('tony', 'pw');
    const otherTab = new BroadcastChannel('lcm');
    const heard = vi.fn();
    otherTab.onmessage = heard;

    await auth.logout();
    await new Promise((resolve) => setTimeout(resolve, 20));

    expect(new Headers(fetchMock.mock.calls.at(-1)![1].headers).get('X-LCM-Session')).toBe('tab-wert');
    expect(heard).not.toHaveBeenCalled();
    expect(hasSessionToken()).toBe(false);
    expect(auth.isAuthenticated).toBe(false);
    otherTab.close();
  });

  it('ignores the logout of another tab in the memory variant, but follows it with a cookie session', async () => {
    const sender = new BroadcastChannel('lcm');
    route({ 'GET /api/v1/auth/me': () => json(memoryMe) });
    const memory = new Auth();
    await memory.start();
    sender.postMessage({ type: 'logout' });
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(memory.isAuthenticated).toBe(true);

    route({ 'GET /api/v1/auth/me': () => json(me) });
    const cookie = new Auth();
    await cookie.start();
    sender.postMessage({ type: 'logout' });
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(cookie.isAuthenticated).toBe(false);
    sender.close();
  });

  it('ends the server session with a keepalive logout when the tab goes (pagehide)', async () => {
    route({
      'POST /api/v1/auth/login': () => json({ session_token: 'tab-wert' }),
      'GET /api/v1/auth/me': () => json(memoryMe),
      'POST /api/v1/auth/logout': () => new Response(null, { status: 204 }),
    });
    const auth = new Auth();
    await auth.start();
    await auth.login('tony', 'pw');
    fetchMock.mockClear();

    window.dispatchEvent(new Event('pagehide')); // jede Auth dieser Testdatei hört mit, das Ergebnis ist dasselbe

    expect(fetchMock).toHaveBeenCalled();
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/auth/logout');
    expect(fetchMock.mock.calls[0][1]).toMatchObject({ method: 'POST', keepalive: true });
  });

  it('treats a page restored from the back/forward cache as logged out in the memory variant', async () => {
    route({ 'GET /api/v1/auth/me': () => json(memoryMe) });
    const auth = new Auth();
    await auth.start();
    const restored = new Event('pageshow');
    Object.defineProperty(restored, 'persisted', { value: true });
    window.dispatchEvent(restored);
    expect(auth.isAuthenticated).toBe(false);
    expect(router.reevaluate).toHaveBeenCalled();
  });

  it('forgets the token on 401', async () => {
    route({
      'POST /api/v1/auth/login': () => json({ session_token: 'tab-wert' }),
      'GET /api/v1/auth/me': () => json(memoryMe),
      'GET /api/v1/servers': () => json({ code: 'auth.required' }, 401),
    });
    const auth = new Auth();
    await auth.start();
    await auth.login('tony', 'pw');
    expect(hasSessionToken()).toBe(true);
    const { api } = await import('./api');
    await api.get('/api/v1/servers').catch(() => {});
    expect(hasSessionToken()).toBe(false);
  });

  it('goes to the login page with a reason after the operator switched the session mode', async () => {
    route({ 'GET /api/v1/auth/me': () => json(me) });
    const auth = new Auth();
    await auth.start();
    await auth.leaveAfterSessionModeChange('/settings/general');
    expect(auth.isAuthenticated).toBe(false);
    expect(router.navigate).toHaveBeenCalledWith('/login?next=%2Fsettings%2Fgeneral&reason=session-mode', { replace: true });
  });

  it('leaves quietly when the logout request fails', async () => {
    route({ 'GET /api/v1/auth/me': () => json(me) });
    const auth = new Auth();
    await auth.start();
    await auth.logout();
    expect(auth.isAuthenticated).toBe(false);
  });

  it('forgets the session on 401 and lets the router add next', async () => {
    route({ 'GET /api/v1/auth/me': () => json(me), 'GET /api/v1/servers': () => json({ code: 'auth.required' }, 401) });
    const auth = new Auth();
    await auth.start();
    const { api } = await import('./api');
    await api.get('/api/v1/servers').catch(() => {});
    expect(auth.isAuthenticated).toBe(false);
    expect(router.reevaluate).toHaveBeenCalled();
    expect(router.navigate).not.toHaveBeenCalled();
  });

  it('saves the language at the account only when logged in', async () => {
    route({
      'GET /api/v1/auth/me': () => json(me),
      'PATCH /api/v1/auth/me/preferences': () => new Response(null, { status: 204 }),
    });
    const auth = new Auth();
    await auth.changeLanguage('en');
    expect(fetchMock).not.toHaveBeenCalled();
    await auth.start();
    await auth.changeLanguage('en');
    const patch = fetchMock.mock.calls.at(-1)!;
    expect(patch[1].method).toBe('PATCH');
    expect(JSON.parse(patch[1].body)).toEqual({ language: 'en' });
  });
});
