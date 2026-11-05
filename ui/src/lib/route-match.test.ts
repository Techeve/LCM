import { describe, expect, it } from 'vitest';
import { matchRoute, type Route } from './route-match';

const page = (path: string): Route => ({ path, tag: 'x-page', load: async () => ({}) });
const routes = [
  page('/'),
  page('/servers'),
  page('/servers/:id/:tab?'),
  page('/docs/*'),
];

describe('matchRoute', () => {
  it('matches static paths and the root', () => {
    expect(matchRoute(routes, '/')?.route.path).toBe('/');
    expect(matchRoute(routes, '/servers')?.route.path).toBe('/servers');
  });

  it('fills params and handles the optional last segment', () => {
    expect(matchRoute(routes, '/servers/42')?.params).toEqual({ id: '42' });
    expect(matchRoute(routes, '/servers/42/packages')?.params).toEqual({ id: '42', tab: 'packages' });
    expect(matchRoute(routes, '/servers/42/packages/extra')).toBeNull();
  });

  it('tolerates a trailing slash', () => {
    expect(matchRoute(routes, '/servers/42/')?.params).toEqual({ id: '42' });
  });

  it('matches wildcards with zero or more rest segments', () => {
    expect(matchRoute(routes, '/docs')?.params).toEqual({ '*': '' });
    expect(matchRoute(routes, '/docs/a/b')?.params).toEqual({ '*': 'a/b' });
  });

  it('decodes params and rejects broken encoding', () => {
    expect(matchRoute(routes, '/servers/web%2003')?.params.id).toBe('web 03');
    expect(matchRoute(routes, '/servers/%E0%A4%A')).toBeNull();
  });

  it('returns null for unknown paths', () => {
    expect(matchRoute(routes, '/nope')).toBeNull();
  });
});
