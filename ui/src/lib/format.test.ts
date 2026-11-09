import { beforeAll, describe, expect, it } from 'vitest';
import { formatBytes, formatDate, formatDuration, formatNumber, formatRelative } from './format';
import { i18n } from './i18n';

const NOW = Date.UTC(2026, 10, 1, 12, 0, 0);

describe('formatters in german', () => {
  beforeAll(() => i18n.setLanguage('de'));

  it('formats relative times with Intl.RelativeTimeFormat', () => {
    expect(formatRelative(NOW - 3 * 60_000, NOW)).toBe('vor 3 Minuten');
    expect(formatRelative(NOW + 2 * 3_600_000, NOW)).toBe('in 2 Stunden');
    expect(formatRelative(NOW - 86_400_000, NOW)).toBe('gestern');
    expect(formatRelative(NOW, NOW)).toBe('jetzt');
  });

  it('formats numbers and dates by locale', () => {
    expect(formatNumber(1234.5)).toBe('1.234,5');
    expect(formatDate(new Date(Date.UTC(2026, 10, 1, 12)))).toContain('2026');
  });

  it('formats bytes with base 1024', () => {
    expect(formatBytes(512)).toMatch(/^512\s?B/);
    expect(formatBytes(1536)).toMatch(/^1,5\s?kB/i);
    expect(formatBytes(5 * 1024 ** 3)).toMatch(/^5\s?GB/i);
  });

  it('keeps the two largest duration units', () => {
    expect(formatDuration(3725)).toMatch(/^1\s?Std\..*2\s?Min/);
    expect(formatDuration(3605)).not.toMatch(/s\b|Sek/);
    expect(formatDuration(0)).toMatch(/^0\s?(s|Sek)/);
  });

  it('shows a dash for missing values', () => {
    expect(formatDate(null)).toBe('–');
    expect(formatRelative('kaputt')).toBe('–');
    expect(formatBytes(undefined)).toBe('–');
  });
});

describe('formatters follow the language', () => {
  it('switches to english', async () => {
    await i18n.setLanguage('en');
    expect(formatRelative(NOW - 3 * 60_000, NOW)).toBe('3 minutes ago');
    expect(formatNumber(1234.5)).toBe('1,234.5');
  });
});
