import { describe, expect, it } from 'vitest';
import { buildId, precacheAssets } from './sw.ts';

describe('precacheAssets', () => {
  it('keeps only hashed files below assets/, as absolute URLs, without source maps', () => {
    const bundle = [
      'index.html',
      'assets/index-abc.js',
      'assets/index-abc.js.map',
      'assets/de-def.js',
      'assets/style-123.css',
      'assets/font-456.woff2',
      'sw.js',
      'favicon.svg',
    ];
    expect(precacheAssets(bundle)).toEqual([
      '/assets/de-def.js',
      '/assets/font-456.woff2',
      '/assets/index-abc.js',
      '/assets/style-123.css',
    ]);
  });
});

describe('buildId', () => {
  it('changes exactly when the file list changes', () => {
    const a = ['/assets/a-1.js', '/assets/b-1.js'];
    expect(buildId(a)).toBe(buildId([...a]));
    expect(buildId(a)).not.toBe(buildId(['/assets/a-1.js', '/assets/b-2.js']));
    expect(buildId(a)).toMatch(/^[0-9a-f]{12}$/);
  });
});
