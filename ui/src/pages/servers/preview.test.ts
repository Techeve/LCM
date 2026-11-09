import { beforeAll, describe, expect, it } from 'vitest';
import { i18n } from '../../lib/i18n';
import { normalisePreview } from '../../api/servers';
import { normaliseConfigPreview } from '../../api/servers-previews';
import { clipCommand, durationText, effectImpacts, fullCommands, noteKey, noteText, packageLine, previewNotes, toBubblePreview, toImpacts, whenText } from './preview';

beforeAll(async () => {
  await i18n.setLanguage('de');
});

const raw = {
  action: 'upgrade-all',
  commands: [{ run: 'a\nb\nc\nd\ne\nf\ng\nh', as: 'root' }],
  packages: [{ name: 'openssl', from: '3.0.11-1', to: '3.0.14-1' }],
  reboot: 'maybe',
  reversible: false,
  duration: { min_seconds: 22, max_seconds: 150 },
  notes: [{ code: 'preview.restart_services', params: {} }, { code: 'preview.unbekannt', params: {} }],
};

describe('Vorschau normalisieren', () => {
  it('liest alle Felder und fängt Fehlendes ab', () => {
    const data = normalisePreview(raw);
    expect(data.packages[0]).toEqual({ name: 'openssl', from: '3.0.11-1', to: '3.0.14-1' });
    expect(data.duration).toEqual({ min: 22, max: 150 });
    expect(data.reboot).toBe('maybe');
    const empty = normalisePreview(null);
    expect(empty.commands).toEqual([]);
    expect(empty.reboot).toBe('no');
    expect(empty.duration).toBeNull();
  });
});

describe('Anzeige der Vorschau', () => {
  it('kürzt lange Skripte und nennt die übrigen Zeilen', () => {
    const clipped = clipCommand('1\n2\n3\n4\n5\n6\n7\n8');
    expect(clipped.split('\n')).toHaveLength(7);
    expect(clipped).toContain('2 weitere Zeilen');
    expect(clipCommand('1\n2\n')).toBe('1\n2');
  });

  it('zeigt Pakete mit Versionssprung, ohne Ziel nur die alte Version', () => {
    expect(packageLine({ name: 'curl', from: '1', to: '2' })).toBe('curl 1 → 2');
    expect(packageLine({ name: 'curl', from: '1', to: '' })).toBe('curl 1');
    expect(packageLine({ name: 'curl', from: '', to: '' })).toBe('curl');
  });

  it('macht aus der Zeitspanne einen Satz', () => {
    expect(durationText({ min: 22, max: 150 })).toContain('bis');
    expect(durationText({ min: 0, max: 30 })).toContain('30');
    expect(durationText(null)).toBe('');
    expect(durationText({ min: 0, max: 0 })).toBe('');
  });

  it('übersetzt Notizen über den Code ohne Präfix', () => {
    expect(noteKey('preview.demo_server')).toBe('servers.preview.notes.demo_server');
    expect(noteText({ code: 'preview.restart_services', params: {} })).not.toContain('servers.preview');
    expect(noteText({ code: 'preview.unbekannt', params: {} })).toBe('preview.unbekannt');
  });

  it('macht „Neustart vielleicht“ zum Hinweis statt zum Ja oder Nein', () => {
    const data = normalisePreview(raw);
    expect(toBubblePreview(data).reboot).toBeUndefined();
    expect(previewNotes(data)[0]).toContain('Neustart');
    expect(toBubblePreview({ ...data, reboot: 'yes' }).reboot).toBe(true);
    expect(toBubblePreview({ ...data, reboot: 'no' }).reboot).toBe(false);
  });

  it('baut die Wirkung für den Dialog', () => {
    const impacts = toImpacts('web03', normalisePreview(raw));
    expect(impacts.map((i) => i.label)).toHaveLength(5);
    expect(impacts[0].value).toBe('web03');
    expect(impacts.find((i) => i.tone === 'warn')).toBeDefined();
    expect(toImpacts('web03', undefined)).toHaveLength(1);
  });

  it('liefert die Befehle ungekürzt mit dem ausführenden Benutzer', () => {
    expect(fullCommands(normalisePreview(raw))[0]).toBe('# root\na\nb\nc\nd\ne\nf\ng\nh');
  });
});

const configRaw = {
  action: 'ssh-port',
  commands: [
    { run: 'open', as: 'root' },
    { run: 'rollback', as: 'root', when: 'on_failure' },
    { run: 'install', as: 'root', when: 'if_needed' },
  ],
  packages: [],
  reboot: 'no',
  reversible: true,
  duration: { min_seconds: 5, max_seconds: 60 },
  notes: [{ code: 'preview.ssh_port_reconnect', params: { old: 22, new: 2222 } }],
};

describe('Vorschau der Konfigurations-Aktionen', () => {
  it('übersetzt die Hinweise aus dem Block previews', () => {
    expect(noteText({ code: 'preview.ssh_port_reconnect', params: { old: 22, new: 2222 } })).toContain('2222');
    expect(noteText({ code: 'preview.restart_services', params: {} })).toContain('Dienste');
  });

  it('nennt die Bedingung eines Schritts im Dialog', () => {
    const data = normaliseConfigPreview(configRaw);
    expect(whenText(data.commands[0])).toBe('');
    expect(fullCommands(data)[1]).toBe('# root · nur beim Zurückrollen\nrollback');
    expect(fullCommands(data)[2]).toContain('nur falls nötig');
  });

  it('lässt Rollback-Schritte aus der Blase weg', () => {
    const bubble = toBubblePreview(normaliseConfigPreview(configRaw));
    expect(bubble.commands).toEqual(['open', '# nur falls nötig\ninstall']);
  });

  it('zeigt als Wirkung nur Umkehrbarkeit und Dauer', () => {
    const impacts = effectImpacts(normaliseConfigPreview(configRaw));
    expect(impacts).toHaveLength(2);
    expect(impacts[0].tone).toBe('ok');
    expect(effectImpacts(undefined)).toEqual([]);
  });
});
