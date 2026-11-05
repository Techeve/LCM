import { describe, expect, it } from 'vitest';
import { addPreset, cleanList, fillSlots, normalisePresets, normaliseServerState, parsePresets, sameList, settingsPatch, type SettingsForm } from './servers-settings';

const base: SettingsForm = {
  name: 'web01',
  host: '10.0.0.1',
  port: 22,
  console_disabled: false,
  maintenance: false,
  user_sync_disabled: false,
  unreachable_uncritical: false,
  unreachable_grace_days: 28,
  docker_updates_disabled: false,
  docker_cves_ignored: false,
};

describe('normaliseServerState', () => {
  it('reads form values and the state shown beside them', () => {
    const state = normaliseServerState({
      name: 'web01',
      host: '10.0.0.1',
      ssh_port: 2222,
      unreachable_grace_days: 0,
      ssh_root_login_disabled: true,
      dns_servers: '1.1.1.1, 9.9.9.9',
      ip_addresses: '10.0.0.1, 192.168.1.5',
      ntp_servers: '0.pool.ntp.org',
      maintenance: true,
      maintenance_since: '2026-11-01T10:00:00Z',
    });
    expect(state.form.port).toBe(2222);
    expect(state.form.unreachable_grace_days).toBe(28);
    expect(state.rootLoginDisabled).toBe(true);
    expect(state.dnsServers).toEqual(['1.1.1.1', '9.9.9.9']);
    expect(state.ipAddresses).toBe('10.0.0.1, 192.168.1.5');
    expect(state.ntpServers).toEqual(['0.pool.ntp.org']);
    expect(state.maintenanceSince).toBe('2026-11-01T10:00:00Z');
  });
});

describe('settingsPatch', () => {
  it('sends only what changed, trimmed', () => {
    expect(settingsPatch(base, { ...base, name: ' web02 ', maintenance: true })).toEqual({ name: 'web02', maintenance: true });
  });

  it('leaves the console out (own route)', () => {
    expect(settingsPatch(base, { ...base, console_disabled: true })).toEqual({});
  });

  it('sends the grace period only while unreachability is uncritical', () => {
    expect(settingsPatch(base, { ...base, unreachable_grace_days: 10 })).toEqual({});
    expect(settingsPatch(base, { ...base, unreachable_uncritical: true, unreachable_grace_days: 10 })).toEqual({ unreachable_uncritical: true, unreachable_grace_days: 10 });
  });
});

describe('lists and presets', () => {
  it('cleans and compares server lists', () => {
    expect(cleanList([' 1.1.1.1 ', '', '1.1.1.1', '9.9.9.9'])).toEqual(['1.1.1.1', '9.9.9.9']);
    expect(sameList(['a', 'b'], ['a', 'b'])).toBe(true);
    expect(sameList(['a', 'b'], ['b', 'a'])).toBe(false);
    expect(fillSlots(['a'], 3)).toEqual(['a', '', '']);
  });

  it('parses "Name = value" lines and bare values', () => {
    expect(parsePresets('Cloudflare = 1.1.1.1\n\n9.9.9.9\nleer =  ')).toEqual([
      { label: 'Cloudflare', value: '1.1.1.1' },
      { label: '9.9.9.9', value: '9.9.9.9' },
    ]);
    expect(normalisePresets({ dns_server_presets: 'A = 1.1.1.1', default_timezone: 'Europe/Berlin' })).toEqual({ dns: [{ label: 'A', value: '1.1.1.1' }], ntp: [], timezone: 'Europe/Berlin' });
  });

  it('puts a preset into the first free slot once', () => {
    expect(addPreset(['1.1.1.1', '', ''], '9.9.9.9')).toEqual(['1.1.1.1', '9.9.9.9', '']);
    expect(addPreset(['1.1.1.1', '', ''], '1.1.1.1')).toEqual(['1.1.1.1', '', '']);
    expect(addPreset(['a', 'b'], 'c')).toEqual(['a', 'b']);
  });
});
