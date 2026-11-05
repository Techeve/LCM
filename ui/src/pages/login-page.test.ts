import { describe, expect, it } from 'vitest';
import { cleanCode } from './login-page';

describe('cleanCode', () => {
  it('keeps digits only, so a pasted code with spaces or dashes works', () => {
    expect(cleanCode('123 456')).toBe('123456');
    expect(cleanCode('123-456')).toBe('123456');
    expect(cleanCode(' 12a3 ')).toBe('123');
  });

  it('cuts at six digits', () => {
    expect(cleanCode('12345678')).toBe('123456');
    expect(cleanCode('')).toBe('');
  });
});
