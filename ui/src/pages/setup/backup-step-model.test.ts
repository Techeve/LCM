import { describe, expect, it } from 'vitest';
import type { GlobalSettings } from '../../api/settings';
import { backupChanged, backupDraft, backupErrors, needsPassphrase, randomBackupTime } from './backup-step-model';

const stored: GlobalSettings = { backup_enabled: false, backup_interval_hours: 24, backup_time: '02:00', backup_retention: 14, backup_dir: '' };
const first = () => 0;

describe('backup step', () => {
  it('starts from the stored schedule and has nothing to send while untouched', () => {
    const base = backupDraft(stored, first);
    const draft = { ...base };
    expect(draft).toMatchObject({ backup_enabled: false, backup_interval_hours: '24', backup_retention: '14' });
    expect(backupChanged(draft, base)).toBe(false);
    expect(backupChanged({ ...draft, backup_enabled: true }, base)).toBe(true);
  });

  it('presets a random time between 02:00 and 05:30 while nothing is set up', () => {
    expect(randomBackupTime(() => 0)).toBe('02:00');
    expect(randomBackupTime(() => 0.999)).toBe('05:30');
    expect(randomBackupTime(() => 0.5)).toBe('03:50');
    const times = Array.from({ length: 200 }, () => randomBackupTime());
    for (const time of times) expect(time).toMatch(/^0[2-5]:[0-5]0$/);
    expect(times.every((time) => time >= '02:00' && time <= '05:30')).toBe(true);
    expect(backupDraft({ ...stored, backup_time: '' }, () => 0.5).backup_time).toBe('03:50');
    expect(backupDraft(stored, () => 0.5).backup_time).toBe('03:50');
  });

  it('keeps the time of a schedule that is switched on', () => {
    expect(backupDraft({ ...stored, backup_enabled: true, backup_time: '04:40' }, () => 0).backup_time).toBe('04:40');
  });

  it('asks for a passphrase only when switched on and no key is stored', () => {
    const on = { ...backupDraft(stored, first), backup_enabled: true };
    expect(needsPassphrase(backupDraft(stored, first), stored)).toBe(false);
    expect(needsPassphrase(on, stored)).toBe(true);
    expect(needsPassphrase(on, { ...stored, backup_passphrase_set: true })).toBe(false);
    expect(needsPassphrase(on, { ...stored, backup_recipients_set: true })).toBe(false);
  });

  it('rejects a missing passphrase and numbers out of range', () => {
    const on = { ...backupDraft(stored, first), backup_enabled: true };
    expect(backupErrors(on, stored)).toEqual({ backup_passphrase: 'setup.backup.passphraseRequired' });
    expect(backupErrors({ ...on, backup_passphrase: 'a long enough passphrase' }, stored)).toEqual({});
    expect(Object.keys(backupErrors({ ...on, backup_passphrase: 'x', backup_interval_hours: '0', backup_retention: 'many' }, stored))).toEqual(['backup_interval_hours', 'backup_retention']);
  });
});
