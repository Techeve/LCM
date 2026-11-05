import { beforeAll, describe, expect, it } from 'vitest';
import { i18n } from './i18n';
import { causeText, durationSince, insightText } from './insight-text';

const NOW = Date.parse('2026-11-01T12:00:00Z');

beforeAll(async () => {
  await i18n.setLanguage('de');
});

describe('insightText', () => {
  it('uses the catalog with the plural form chosen by count', () => {
    expect(insightText({ message: 'x', key: 'criticalVulns', params: { count: '1' } })).toBe('1 kritische Sicherheitslücke (CVE)');
    expect(insightText({ message: 'x', key: 'criticalVulns', params: { count: '2' } })).toBe('2 kritische Sicherheitslücken (CVE)');
  });

  it('fills plain placeholders', () => {
    expect(insightText({ message: 'x', key: 'diskLow', params: { percent: '88' } })).toBe('Festplattenspeicher wird knapp (88% belegt)');
  });

  it('falls back to the server message without key or catalog text', () => {
    expect(insightText({ message: 'Klartext' })).toBe('Klartext');
    expect(insightText({ message: 'Klartext', key: 'doesNotExist' })).toBe('Klartext');
  });

  it('composes duration and cause for an incapable server', () => {
    const text = insightText(
      {
        message: 'x',
        key: 'actionFailed.rules',
        params: { since: '2026-10-30T12:00:00Z', count: '3', causeKey: 'disk_full', path: '/var', job: 'Updates' },
      },
      NOW,
    );
    expect(text).toBe('Regeln und Aktionen schlagen seit 2 Tagen fehl (3×): Kein Speicherplatz mehr auf /var - Job: Updates');
  });
});

describe('durationSince', () => {
  it('never shows NaN or negative spans', () => {
    expect(durationSince('kaputt', NOW)).toBe('weniger als einer Minute');
    expect(durationSince('2026-11-02T00:00:00Z', NOW)).toBe('weniger als einer Minute');
  });

  it('picks the largest unit', () => {
    expect(durationSince('2026-11-01T11:15:00Z', NOW)).toBe('45 Minuten');
    expect(durationSince('2026-11-01T09:00:00Z', NOW)).toBe('3 Stunden');
  });
});

describe('causeText', () => {
  it('uses the variant without the missing parameter', () => {
    expect(causeText('lock_busy', {})).toContain('dpkg-Sperre');
    expect(causeText('lock_busy', { pid: '12', process: 'apt' })).toContain('PID 12');
  });

  it('knows nothing unknown', () => {
    expect(causeText('whatever')).toContain('Ursache unbekannt');
  });
});
