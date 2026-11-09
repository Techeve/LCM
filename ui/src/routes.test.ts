import { describe, expect, it, vi } from 'vitest';
import { matchRoute, isRedirect } from './lib/route-match';

vi.mock('./lib/server-lookup', () => ({ serverPathByName: async (name: string) => `/servers/by/${name}` }));
const { routes } = await import('./routes');

async function redirectOf(path: string): Promise<string | null> {
  const match = matchRoute(routes, path);
  return match && isRedirect(match.route) ? match.route.redirect(match.params) : null;
}

describe('route table', () => {
  it('redirects legacy addresses', async () => {
    expect(await redirectOf('/docker')).toBe('/containers');
    expect(await redirectOf('/docker/images')).toBe('/containers/images');
    expect(await redirectOf('/doku/einstieg')).toBe('/docs/einstieg');
    expect(await redirectOf('/linux-aktivierung/abc')).toBe('/linux-activate/abc');
    expect(await redirectOf('/aktivierung/abc')).toBe('/activate/abc');
  });

  it('resolves names before the server detail route', async () => {
    expect(await redirectOf('/servers/by-name/web03')).toBe('/servers/by/web03');
    expect(matchRoute(routes, '/servers/42/packages')?.params).toEqual({ id: '42', tab: 'packages' });
  });

  it('keeps the join wizard out of the server detail route', () => {
    expect(matchRoute(routes, '/servers/join')?.params).toEqual({});
    expect((matchRoute(routes, '/servers/join')?.route as { tag: string }).tag).toBe('lcm-page-server-join');
    expect((matchRoute(routes, '/servers/42')?.route as { tag: string }).tag).toBe('lcm-page-servers');
  });

  it('keeps exactly docs, login, activation and password reset public', () => {
    const publicPaths = routes.filter((route) => route.public).map((route) => route.path);
    expect(publicPaths).toEqual(
      expect.arrayContaining(['/login', '/docs/*', '/activate/*', '/linux-activate/*', '/password-reset/*']),
    );
    expect(matchRoute(routes, '/servers')?.route.public).toBeUndefined();
    expect(matchRoute(routes, '/settings/alerts')?.route.public).toBeUndefined();
  });
});
