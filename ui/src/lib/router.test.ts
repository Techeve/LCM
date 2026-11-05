import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { Route } from './route-match';
import { Router } from './router';
import { unsaved } from './unsaved';

const load = vi.fn(async () => ({}));
const page = (path: string, options: { public?: boolean } = {}): Route => ({ path, tag: `x-${path}`, load, ...options });

const routes: Route[] = [
  { path: '/docker/*', redirect: ({ '*': rest }) => `/containers/${rest}` },
  { path: '/servers/by-name/:name', redirect: async ({ name }) => (name === 'web03' ? '/servers/7' : Promise.reject(new Error('unknown'))) },
  page('/'),
  page('/servers'),
  page('/servers/:id/:tab?'),
  page('/containers/*'),
  page('/login', { public: true }),
  page('/docs/*', { public: true }),
];

function createRouter(authenticated: boolean): Router {
  const router = new Router();
  router.configure({ routes, isAuthenticated: () => authenticated });
  return router;
}

const here = () => location.pathname + location.search + location.hash;

beforeEach(() => {
  history.replaceState(null, '', '/');
  load.mockClear();
});

describe('guard', () => {
  it('sends protected routes without session to the login with next', async () => {
    const router = createRouter(false);
    await router.navigate('/servers/42?tab=x');
    expect(here()).toBe(`/login?next=${encodeURIComponent('/servers/42?tab=x')}`);
    expect(router.state.match?.route.path).toBe('/login');
  });

  it('treats unknown paths as protected', async () => {
    await createRouter(false).navigate('/nope');
    expect(location.pathname).toBe('/login');
  });

  it('leaves public routes alone', async () => {
    const router = createRouter(false);
    await router.navigate('/docs/intro');
    expect(here()).toBe('/docs/intro');
    expect(router.state.match?.params).toEqual({ '*': 'intro' });
  });

  it('returns to next after login and ignores foreign targets', async () => {
    const router = createRouter(false);
    await router.navigate('/servers/42');
    const login = createRouter(true);
    await login.navigate(here(), { replace: true });
    await login.afterLogin();
    expect(here()).toBe('/servers/42');

    await login.navigate(`/login?next=${encodeURIComponent('//evil.example')}`);
    await login.afterLogin();
    expect(here()).toBe('/');
  });

  it('re-checks the current address when the session ends', async () => {
    let authenticated = true;
    const router = new Router();
    router.configure({ routes, isAuthenticated: () => authenticated });
    await router.navigate('/servers/42/jobs');
    authenticated = false;
    await router.reevaluate();
    expect(here()).toBe(`/login?next=${encodeURIComponent('/servers/42/jobs')}`);
  });
});

describe('guard option (mandatory steps)', () => {
  const guard = (path: string) => (path.startsWith('/account/security') ? null : '/account/security?required=1');
  const accountRoutes: Route[] = [...routes, page('/account/:section?')];

  function guarded(): Router {
    const router = new Router();
    router.configure({ routes: accountRoutes, isAuthenticated: () => true, guard });
    return router;
  }

  it('redirects every protected page to the remediation page', async () => {
    const router = guarded();
    await router.navigate('/servers/42');
    expect(here()).toBe('/account/security?required=1');
    expect(router.state.match?.params).toEqual({ section: 'security' });

    await router.navigate('/nope');
    expect(here()).toBe('/account/security?required=1');
  });

  it('lets the remediation page and public routes stay', async () => {
    const router = guarded();
    await router.navigate('/account/security');
    expect(here()).toBe('/account/security');
    await router.navigate('/docs/intro');
    expect(here()).toBe('/docs/intro');
  });

  it('does not interfere without a guard', async () => {
    await createRouter(true).navigate('/servers');
    expect(here()).toBe('/servers');
  });

  it('asks the guard again when the account state changes', async () => {
    let locked = false;
    const router = new Router();
    router.configure({ routes: accountRoutes, isAuthenticated: () => true, guard: (path) => (locked ? guard(path) : null) });
    await router.navigate('/servers');
    expect(here()).toBe('/servers');
    locked = true;
    await router.reevaluate();
    expect(here()).toBe('/account/security?required=1');
  });
});

describe('redirects', () => {
  it('turns legacy hash links into paths', async () => {
    history.replaceState(null, '', '/#/servers/42/packages?sort=name');
    const router = createRouter(true);
    await router.reevaluate();
    expect(here()).toBe('/servers/42/packages?sort=name');
    expect(router.state.match?.params).toEqual({ id: '42', tab: 'packages' });
  });

  it('does not follow a legacy hash to another origin', async () => {
    history.replaceState(null, '', '/#//evil.example/x');
    const router = createRouter(true);
    await router.reevaluate();
    expect(here()).toBe('/');
  });

  it('maps /docker to /containers and keeps the query', async () => {
    const router = createRouter(true);
    await router.navigate('/docker/images?sort=name');
    expect(here()).toBe('/containers/images?sort=name');
    expect(router.state.match?.route.path).toBe('/containers/*');
  });

  it('resolves /servers/by-name/:name through the route', async () => {
    const router = createRouter(true);
    await router.navigate('/servers/by-name/web03');
    expect(here()).toBe('/servers/7');
  });

  it('shows no page when the name cannot be resolved', async () => {
    const router = createRouter(true);
    await router.navigate('/servers/by-name/unknown');
    expect(router.state.match).toBeNull();
  });

  it('refuses addresses on another origin', () => {
    expect(() => createRouter(true).navigate('https://evil.example/')).toThrow(RangeError);
    expect(() => createRouter(true).navigate('//evil.example/')).toThrow(RangeError);
  });
});

describe('pages and query state', () => {
  it('loads the page module and reports load errors', async () => {
    const router = createRouter(true);
    await router.navigate('/servers');
    expect(load).toHaveBeenCalledOnce();
    load.mockRejectedValueOnce(new Error('chunk missing'));
    await router.navigate('/');
    expect(router.state.loadError).toBeInstanceOf(Error);
  });

  it('keeps tab, filters and modal in the address and supports history', async () => {
    const router = createRouter(true);
    await router.navigate('/servers/42/packages');
    router.setQuery({ sort: 'name', open: 7 });
    router.setQuery({ page: 2 });
    expect(here()).toBe('/servers/42/packages?sort=name&open=7&page=2');
    expect(router.state.query.get('open')).toBe('7');
  });

  it('removes parameters with null, empty and false and skips no-ops', async () => {
    const router = createRouter(true);
    await router.navigate('/servers?a=1&b=2&c=3');
    const changed = vi.fn();
    router.onChange(changed);
    router.setQuery({ a: null, b: '', c: false });
    expect(here()).toBe('/servers');
    router.setQuery({ a: undefined });
    expect(changed).toHaveBeenCalledOnce();
  });

  it('pushes a history entry by default and replaces on request', async () => {
    const router = createRouter(true);
    await router.navigate('/servers');
    const length = history.length;
    router.setQuery({ q: 'a' }, { replace: true });
    expect(history.length).toBe(length);
    router.setQuery({ q: 'ab' });
    expect(history.length).toBe(length + 1);
  });

  it('aborts the navigation signal on page change but not on query change', async () => {
    const router = createRouter(true);
    await router.navigate('/servers');
    const signal = router.navigationSignal;
    router.setQuery({ q: 'x' });
    expect(signal.aborted).toBe(false);
    await router.navigate('/');
    expect(signal.aborted).toBe(true);
  });
});

describe('links and history', () => {
  it('intercepts same-origin links but not modified clicks, targets or external links', async () => {
    const router = createRouter(true);
    await router.start();
    // Dieser Hörer läuft nach dem des Routers und hält happy-dom vom echten Navigieren ab.
    let intercepted = false;
    document.addEventListener('click', (event) => {
      intercepted = event.defaultPrevented;
      event.preventDefault();
    });
    const click = (anchor: HTMLAnchorElement, init: MouseEventInit = {}) => {
      document.body.append(anchor);
      anchor.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true, composed: true, button: 0, ...init }));
      anchor.remove();
      return intercepted;
    };
    const link = (href: string, attributes: Record<string, string> = {}) => {
      const anchor = document.createElement('a');
      anchor.href = href;
      for (const [name, value] of Object.entries(attributes)) anchor.setAttribute(name, value);
      return anchor;
    };

    expect(click(link('/servers/42'))).toBe(true);
    await vi.waitFor(() => expect(here()).toBe('/servers/42'));
    expect(click(link('/servers/43'), { ctrlKey: true })).toBe(false);
    expect(click(link('/servers/43'), { metaKey: true })).toBe(false);
    expect(click(link('/servers/43', { target: '_blank' }))).toBe(false);
    expect(click(link('/api/v1/export'))).toBe(false);
    expect(click(link('https://other.example/'))).toBe(false);
    expect(here()).toBe('/servers/42');
  });

  it('shows the address the browser went back to', async () => {
    const router = createRouter(true);
    await router.start();
    await router.navigate('/servers/5');
    history.replaceState(null, '', '/servers?sort=name'); // das Ziel von „Zurück“
    dispatchEvent(new PopStateEvent('popstate'));
    await vi.waitFor(() => expect(router.state.path).toBe('/servers'));
    expect(router.state.query.get('sort')).toBe('name');
  });
});

describe('unsaved changes', () => {
  const owner = {};
  const ask = vi.fn(async () => false);

  async function dirtyAt(path: string): Promise<Router> {
    const router = createRouter(true);
    await router.navigate(path);
    unsaved.setPrompt(ask);
    unsaved.set(owner, true);
    return router;
  }

  beforeEach(() => {
    unsaved.set(owner, false);
    ask.mockReset().mockResolvedValue(false);
  });

  it('stays on the page when the answer is no', async () => {
    const router = await dirtyAt('/servers');
    await router.navigate('/', { ask: true });
    expect(ask).toHaveBeenCalledOnce();
    expect(here()).toBe('/servers');
    expect(router.state.path).toBe('/servers');
    unsaved.set(owner, false);
  });

  it('changes the page when the answer is yes', async () => {
    const router = await dirtyAt('/servers');
    ask.mockResolvedValue(true);
    await router.navigate('/', { ask: true });
    expect(router.state.path).toBe('/');
    unsaved.set(owner, false);
  });

  it('does not ask for query changes, the same page or code that navigates after saving', async () => {
    const router = await dirtyAt('/servers/1');
    await router.navigate('/servers/1?tab=x', { ask: true });
    router.setQuery({ a: 1 });
    await router.navigate('/');
    expect(ask).not.toHaveBeenCalled();
    expect(router.state.path).toBe('/');
    unsaved.set(owner, false);
  });

  it('puts the address back after Back when the answer is no', async () => {
    const router = await dirtyAt('/servers?sort=name');
    void router.start();
    await Promise.resolve();
    history.replaceState(null, '', '/');
    dispatchEvent(new PopStateEvent('popstate'));
    await vi.waitFor(() => expect(here()).toBe('/servers?sort=name'));
    expect(router.state.path).toBe('/servers');
    unsaved.set(owner, false);
  });
});
