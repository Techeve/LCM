import { describe, expect, it } from 'vitest';
import { compressible } from './compress';

describe('compressible', () => {
  it('takes scripts and styles of a useful size', () => {
    expect(compressible('assets/index-abc.js', 2048)).toBe(true);
    expect(compressible('assets/index-abc.css', 2048)).toBe(true);
  });

  it('leaves small files and compressed formats alone', () => {
    expect(compressible('assets/index-abc.js', 100)).toBe(false);
    expect(compressible('assets/font-abc.woff2', 90_000)).toBe(false);
    expect(compressible('assets/index-abc.js.br', 2048)).toBe(false);
  });
});
