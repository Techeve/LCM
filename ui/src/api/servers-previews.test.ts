import { describe, expect, it } from 'vitest';
import { configPreviewUrl, normaliseConfigPreview } from './servers-previews';

function query(url: string): URLSearchParams {
  return new URL(url, 'http://lcm.test').searchParams;
}

describe('Adresse der Konfigurations-Vorschau', () => {
  it('kommt ohne Argumente ohne Query aus', () => {
    expect(configPreviewUrl(7, 'harden-ssh')).toBe('/api/v1/servers/7/actions/harden-ssh/preview');
  });

  it('schickt Firewall-Regeln und SSH-Quellen als JSON', () => {
    const q = query(configPreviewUrl(1, 'firewall-apply', { rules: [{ port: 443, proto: 'tcp', ip_version: 'any' }], sshSources: { allowlist_ids: [2], source_ips: [] } }));
    expect(JSON.parse(q.get('rules') ?? '')).toEqual([{ port: 443, proto: 'tcp', ip_version: 'any' }]);
    expect(JSON.parse(q.get('ssh_sources') ?? '')).toEqual({ allowlist_ids: [2], source_ips: [] });
  });

  it('übersetzt Einrichtung und Verwaltung der Sicherheitswerkzeuge', () => {
    const install = query(configPreviewUrl(1, 'security-tool', { tool: { tool: 'crowdsec', allowlist_ips: ['192.0.2.1', '198.51.100.0/24'], allowlist_ids: [3], bouncer: false, lapi_mode: 'remote', lapi_id: 4 } }));
    expect(install.get('tool')).toBe('crowdsec');
    expect(install.get('allowlist_ips')).toBe('192.0.2.1,198.51.100.0/24');
    expect(install.get('bouncer')).toBe('false');
    expect(install.get('lapi_id')).toBe('4');
    expect(install.has('op')).toBe(false);
    const manage = query(configPreviewUrl(1, 'security-tool-manage', { tool: { tool: 'fail2ban', action: 'unban', unban_ip: '192.0.2.9' } }));
    expect(manage.get('op')).toBe('unban');
    expect(manage.get('ip')).toBe('192.0.2.9');
    expect(manage.has('bouncer')).toBe(false);
  });

  it('nimmt Listen kommagetrennt und lässt Leeres weg', () => {
    const q = query(configPreviewUrl(1, 'dns', { servers: ['1.1.1.1', '9.9.9.9'], timezone: '' }));
    expect(q.get('servers')).toBe('1.1.1.1,9.9.9.9');
    expect(q.has('timezone')).toBe(false);
    expect(query(configPreviewUrl(1, 'dns', { servers: [] })).has('servers')).toBe(false);
  });
});

describe('Konfigurations-Vorschau normalisieren', () => {
  it('liest die Bedingung je Schritt, Unbekanntes gilt als immer', () => {
    const data = normaliseConfigPreview({ commands: [{ run: 'a', as: 'root' }, { run: 'b', as: 'root', when: 'on_failure' }, { run: 'c', as: 'root', when: 'später' }] });
    expect(data.commands.map((c) => c.when)).toEqual(['', 'on_failure', '']);
    expect(normaliseConfigPreview(null).commands).toEqual([]);
  });
});
