import { describe, expect, it } from 'vitest';
import { isBuildFile, isPrecachedRequest, outdatedCaches } from './sw-rules';

const origin = 'https://lcm.example';
const assets = new Set(['/assets/index-abc123.js', '/assets/de-def456.js']);
const request = (url: string, method = 'GET') => ({ url, method });

describe('isPrecachedRequest', () => {
  it('serves hashed assets of the current build', () => {
    expect(isPrecachedRequest(request(`${origin}/assets/index-abc123.js`), origin, assets)).toBe(true);
  });

  it.each([
    ['index.html', `${origin}/index.html`],
    ['navigation', `${origin}/servers/42`],
    ['API', `${origin}/api/v1/servers`],
    ['the worker itself', `${origin}/sw.js`],
    ['an asset of an older build', `${origin}/assets/index-old000.js`],
    ['an asset with a query', `${origin}/assets/index-abc123.js?x=1`],
    ['another origin', 'https://evil.example/assets/index-abc123.js'],
  ])('goes to the network for %s', (_name, url) => {
    expect(isPrecachedRequest(request(url), origin, assets)).toBe(false);
  });

  it('never caches non-GET requests', () => {
    expect(isPrecachedRequest(request(`${origin}/assets/index-abc123.js`, 'POST'), origin, assets)).toBe(false);
  });
});

describe('isBuildFile', () => {
  it('erkennt Dateien auch älterer Builds', () => {
    expect(isBuildFile(request(`${origin}/assets/index-old000.js`), origin)).toBe(true);
  });

  it.each([`${origin}/api/v1/servers`, `${origin}/sw.js`, `${origin}/assets/x.js?v=1`, 'https://evil.example/assets/x.js'])('nicht %s', (url) => {
    expect(isBuildFile(request(url), origin)).toBe(false);
  });
});

describe('outdatedCaches', () => {
  it('behält den jüngsten älteren Cache', () => {
    expect(outdatedCaches(['lcm-a', 'lcm-b', 'lcm-c'])).toEqual(['lcm-a', 'lcm-b']);
    expect(outdatedCaches(['lcm-a'])).toEqual([]);
    expect(outdatedCaches([])).toEqual([]);
  });
});
