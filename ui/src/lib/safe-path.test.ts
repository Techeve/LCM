import { describe, expect, it } from 'vitest';
import { isInternalPath, loginHref, safeNext } from './safe-path';

describe('safeNext', () => {
  it('accepts internal paths with query', () => {
    expect(safeNext('/servers/42/packages?sort=name')).toBe('/servers/42/packages?sort=name');
    expect(safeNext('/')).toBe('/');
  });

  it.each([
    '//evil.example',
    '//evil.example/path',
    '/\\evil.example',
    '\\\\evil.example',
    'https://evil.example',
    'http://evil.example/servers',
    'javascript:alert(1)',
    'evil.example',
    '',
    '/\t/evil.example',
    '/\n/evil.example',
    '/\r/evil.example',
    '/servers\\..\\evil',
  ])('rejects the open redirect attempt %j', (value) => {
    expect(safeNext(value)).toBeNull();
  });

  it('rejects missing values and the login page itself', () => {
    expect(safeNext(null)).toBeNull();
    expect(safeNext(undefined)).toBeNull();
    expect(safeNext('/login')).toBeNull();
    expect(safeNext('/login?next=/servers')).toBeNull();
  });

  it('keeps percent-encoded slashes as path characters', () => {
    expect(isInternalPath('/%2F%2Fevil.example')).toBe(true);
  });
});

describe('loginHref', () => {
  it('carries the target as encoded next', () => {
    expect(loginHref('/servers/42?tab=a&b=c')).toBe(`/login?next=${encodeURIComponent('/servers/42?tab=a&b=c')}`);
  });

  it('omits next for the start page and for unsafe targets', () => {
    expect(loginHref('/')).toBe('/login');
    expect(loginHref('//evil.example')).toBe('/login');
  });
});
