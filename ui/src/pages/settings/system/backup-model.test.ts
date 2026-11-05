import { describe, expect, it } from 'vitest';
import { addRecipient, isBackupFile, scheduleBlocked, scheduleBody, secretState } from './backup-model';

describe('Backup-Schlüssel', () => {
  it('bevorzugt Empfänger-Schlüssel vor der Passphrase', () => {
    expect(secretState({ backup_recipients_set: true, backup_passphrase_set: true })).toBe('recipients');
    expect(secretState({ backup_passphrase_set: true })).toBe('passphrase');
    expect(secretState({})).toBe('missing');
  });

  it('meldet geplante Backups ohne Schlüssel als blockiert', () => {
    expect(scheduleBlocked({ backup_enabled: true })).toBe(true);
    expect(scheduleBlocked({ backup_enabled: true, backup_passphrase_set: true })).toBe(false);
    expect(scheduleBlocked({ backup_enabled: false })).toBe(false);
  });
});

describe('Zeitplan-Körper', () => {
  it('schickt alle Felder, Zahlen als Zahl', () => {
    const draft = {
      backup_enabled: true,
      backup_interval_hours: '12',
      backup_time: ' 03:30 ',
      backup_retention: '7',
      backup_dir: '',
      backup_recipients: 'age1abc',
      backup_passphrase: '',
      restore_auto_restart: false,
    };
    expect(scheduleBody(draft)).toEqual({ enabled: true, interval_hours: 12, retention: 7, time: '03:30', dir: '', auto_restart: false, passphrase: '', recipients: 'age1abc' });
  });

  it('fällt bei unlesbaren Zahlen auf die Vorgaben zurück', () => {
    expect(scheduleBody({ backup_interval_hours: 'x', backup_retention: '' })).toMatchObject({ interval_hours: 24, retention: 14 });
  });
});

describe('Empfänger', () => {
  it('hängt Schlüssel an und vermeidet Doppelte', () => {
    expect(addRecipient('', 'age1a')).toBe('age1a');
    expect(addRecipient('age1a\n', 'age1b')).toBe('age1a\nage1b');
    expect(addRecipient('age1a\nage1b', 'age1a')).toBe('age1a\nage1b');
  });

  it('erkennt Backup-Dateien', () => {
    expect(isBackupFile('lcm-2026.lcmbak')).toBe(true);
    expect(isBackupFile('LCM.LCMBAK')).toBe(true);
    expect(isBackupFile('lcm.zip')).toBe(false);
  });
});
