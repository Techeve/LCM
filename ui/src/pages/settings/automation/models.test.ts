import { describe, expect, it } from 'vitest';
import type { ScheduleEntry } from '../../../api/settings-more';
import { commandLines, hasSecrets, maskSecrets, validateActionForm } from './action-model';
import { triggerImpacts, triggerPreview, whenText } from './schedule-model';

describe('Geheimnisse maskieren', () => {
  it('maskiert wie die Befehlsvorschau', () => {
    expect(maskSecrets('DB_PASSWORD=geheim mysqldump app')).toBe('DB_PASSWORD=•••• mysqldump app');
    expect(maskSecrets('curl -u admin:pw https://x')).toBe('curl -u admin:•••• https://x');
    expect(maskSecrets('curl -H "Authorization: Bearer abc123" https://x')).toBe('curl -H "Authorization: Bearer ••••" https://x');
    expect(maskSecrets('git clone https://user:pw@host/repo')).toBe('git clone https://user:••••@host/repo');
    expect(maskSecrets('tool --token abc')).toBe('tool --token ••••');
    expect(maskSecrets("echo 'pw' | sudo -S true")).toBe('echo •••• | sudo -S true');
  });

  it('lässt Harmloses stehen', () => {
    expect(hasSecrets('systemctl restart nginx\nPasswordAuthentication no')).toBe(false);
  });
});

describe('Eigene Aktion', () => {
  it('zählt nur ausgeführte Zeilen', () => {
    expect(commandLines('a\n\n# Kommentar\n  b  ')).toEqual(['a', 'b']);
    expect(validateActionForm({ name: 'x', description: '', commands: '# nur Kommentar' }).commands).toBeDefined();
    expect(validateActionForm({ name: '', description: '', commands: 'ls' }).name).toBeDefined();
  });
});

describe('Zeitpläne', () => {
  const base: ScheduleEntry = { kind: 'schedule', schedule_id: 1, name: 'Nachts', type: '', cron_expr: '0 3 * * *', group_id: 2, next_run: '', enabled: true, rules: [] };

  it('beschreibt eingebaute Intervalle', () => {
    expect(whenText('@every 30m')).toBe('settings.schedules.everyMinutes');
    expect(whenText('@every 3h')).toBe('settings.schedules.everyHours');
  });

  it('zeigt die aktiven Regeln und warnt vor Neustarts', () => {
    const entry = {
      ...base,
      rules: [
        { id: 1, name: 'Updates', type: 'update', enabled: true },
        { id: 2, name: 'Neustart', type: 'reboot', enabled: true },
        { id: 3, name: 'Aus', type: 'health', enabled: false },
      ],
    };
    const preview = triggerPreview(entry);
    expect(preview.commands).toHaveLength(2);
    expect(preview.reboot).toBe(true);
    expect(triggerImpacts(entry, 'Staging').some((impact) => impact.tone === 'warn')).toBe(true);
  });

  it('beschreibt System-Zeitpläne über ihre Art', () => {
    const cleanup = { ...base, kind: 'cleanup', schedule_id: null, group_id: null };
    expect(triggerPreview(cleanup)).toEqual({ reversible: false, reboot: false });
    expect(triggerImpacts(cleanup, '')).toHaveLength(2);
  });
});
