import { describe, expect, it } from 'vitest';
import { normaliseSelfUpdate, normaliseSystemInfo, normaliseUpdateInfo } from '../api/system';
import { updateBanner } from './update-model';

const info = (latest: string, available: boolean) => normaliseUpdateInfo({ current_version: '1.0.0', latest_version: latest, update_available: available });
const self = (phase: string, extra: Record<string, unknown> = {}) => normaliseSelfUpdate({ supported: true, phase, ...extra });

describe('updateBanner', () => {
  it('shows a running self-update before anything else', () => {
    expect(updateBanner(info('1.1.0', true), self('waiting', { waiting_for: ['Backup'] }), '', false)?.kind).toBe('busy');
    expect(updateBanner(undefined, self('backup'), '', false)?.kind).toBe('busy');
    expect(updateBanner(undefined, self('running'), '', false)?.kind).toBe('busy');
  });

  it('offers a new version until it is dismissed', () => {
    expect(updateBanner(info('1.1.0', true), self('idle'), '', false)?.kind).toBe('available');
    expect(updateBanner(info('1.1.0', true), self('failed'), '', false)?.kind).toBe('available');
    expect(updateBanner(info('1.1.0', true), self('idle'), '1.1.0', false)).toBeNull();
    expect(updateBanner(info('1.2.0', true), null, '1.1.0', false)?.kind).toBe('available');
  });

  it('answers a manual check with "up to date", otherwise stays silent', () => {
    expect(updateBanner(info('1.0.0', false), null, '', false)).toBeNull();
    expect(updateBanner(info('1.0.0', false), null, '', true)?.kind).toBe('current');
    expect(updateBanner(undefined, null, '', true)).toBeNull();
  });
});

describe('system normalisers', () => {
  it('drops a build date that is no date and keeps unknown phases idle', () => {
    expect(normaliseSystemInfo({ version: '1.0.0', built_at: 'unbekannt', dirty: true }).built_at).toBe('');
    expect(normaliseSystemInfo({ version: '1.0.0', built_at: '2026-11-01T10:00:00Z' }).built_at).toBe('2026-11-01T10:00:00Z');
    expect(normaliseSelfUpdate({ phase: 'exploded', waiting_for: ['a', 3] })).toMatchObject({ phase: 'idle', waiting_for: ['a'] });
  });
});
