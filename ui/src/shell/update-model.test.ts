import { describe, expect, it } from 'vitest';
import { normaliseSelfUpdate, normaliseSystemInfo, normaliseUpdateInfo } from '../api/system';
import { updateBanner, updateSteps } from './update-model';

const info = (latest: string, available: boolean) => normaliseUpdateInfo({ current_version: '1.0.0', latest_version: latest, update_available: available });
const self = (phase: string, extra: Record<string, unknown> = {}) => normaliseSelfUpdate({ supported: true, phase, ...extra });

describe('updateBanner', () => {
  it('shows a running self-update before anything else', () => {
    expect(updateBanner(info('1.1.0', true), self('waiting', { waiting_for: ['Backup'] }), '')?.kind).toBe('busy');
    expect(updateBanner(undefined, self('backup'), '')?.kind).toBe('busy');
    expect(updateBanner(undefined, self('running'), '')?.kind).toBe('busy');
  });

  it('offers a new version until it is dismissed', () => {
    expect(updateBanner(info('1.1.0', true), self('idle'), '')?.kind).toBe('available');
    expect(updateBanner(info('1.1.0', true), self('failed'), '')?.kind).toBe('available');
    expect(updateBanner(info('1.1.0', true), self('idle'), '1.1.0')).toBeNull();
    expect(updateBanner(info('1.2.0', true), null, '1.1.0')?.kind).toBe('available');
  });

  it('stays silent without a new version', () => {
    expect(updateBanner(info('1.0.0', false), null, '')).toBeNull();
    expect(updateBanner(undefined, null, '')).toBeNull();
  });
});

describe('system normalisers', () => {
  it('drops a build date that is no date and keeps unknown phases idle', () => {
    expect(normaliseSystemInfo({ version: '1.0.0', built_at: 'unbekannt', dirty: true }).built_at).toBe('');
    expect(normaliseSystemInfo({ version: '1.0.0', built_at: '2026-11-01T10:00:00Z' }).built_at).toBe('2026-11-01T10:00:00Z');
    expect(normaliseSelfUpdate({ phase: 'exploded', waiting_for: ['a', 3] })).toMatchObject({ phase: 'idle', waiting_for: ['a'] });
  });
});

describe('updateSteps', () => {
  it('follows the phases of the self-update', () => {
    expect(updateSteps(self('waiting')).map((s) => s.state)).toEqual(['current', 'pending', 'pending']);
    expect(updateSteps(self('backup')).map((s) => s.state)).toEqual(['done', 'current', 'pending']);
    expect(updateSteps(self('running')).map((s) => s.state)).toEqual(['done', 'done', 'current']);
  });

  it('has no steps without a running update', () => {
    expect(updateSteps(self('idle'))).toEqual([]);
    expect(updateSteps(self('failed'))).toEqual([]);
    expect(updateSteps(null)).toEqual([]);
  });
});
