import { describe, expect, it } from 'vitest';
import type { SettingsForm } from '../../api/servers-settings';
import { addressSuggestions, settingsErrors, validNameserver, validTimeServer, validTimezone } from './set-model';

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

describe('settingsErrors', () => {
  it('accepts the loaded settings', () => {
    expect(settingsErrors(base, base)).toEqual({});
  });

  it('requires name and address and checks the port', () => {
    expect(settingsErrors(base, { ...base, name: ' ', host: '', port: 70000 })).toEqual({
      name: 'serverPackages.settings.invalid.name',
      host: 'serverPackages.settings.invalid.host',
      port: 'serverPackages.settings.invalid.port',
    });
    expect(settingsErrors(base, { ...base, port: 0 }).port).toBeDefined();
  });

  it('rejects control characters and overlong names like the backend', () => {
    expect(settingsErrors(base, { ...base, name: 'web\u000101' }).name).toBe('serverPackages.settings.invalid.nameFormat');
    expect(settingsErrors(base, { ...base, name: 'x'.repeat(65) }).name).toBe('serverPackages.settings.invalid.nameFormat');
    expect(settingsErrors(base, { ...base, name: 'ä'.repeat(64) })).toEqual({});
  });

  it('accepts hostnames and IPs as address, nothing else', () => {
    for (const host of ['web01.example.org', '192.168.1.5', '2001:db8::1', '[2001:db8::1]']) expect(settingsErrors(base, { ...base, host })).toEqual({});
    for (const host of ['web 01', 'http://web01', 'a..b', `${'a'.repeat(63)}.${'b'.repeat(63)}.${'c'.repeat(63)}.${'d'.repeat(63)}.example`]) {
      expect(settingsErrors(base, { ...base, host }).host).toBe('serverPackages.settings.invalid.hostFormat');
    }
  });

  it('leaves unchanged stored values alone', () => {
    const odd = { ...base, host: 'fe80::1%eth0' };
    expect(settingsErrors(odd, { ...odd, maintenance: true })).toEqual({});
  });

  it('checks the grace period only while unreachability is uncritical', () => {
    expect(settingsErrors(base, { ...base, unreachable_uncritical: true, unreachable_grace_days: 0 })).toEqual({ grace: 'serverPackages.settings.invalid.grace' });
    expect(settingsErrors(base, { ...base, unreachable_uncritical: true, unreachable_grace_days: 366 }).grace).toBeDefined();
    expect(settingsErrors(base, { ...base, unreachable_grace_days: 0 })).toEqual({});
  });

  it('skips address and port for agent servers', () => {
    const agent = { ...base, host: '', port: 0 };
    expect(settingsErrors(agent, { ...agent, maintenance: true }, false)).toEqual({});
  });
});

describe('action fields', () => {
  it('takes only IP addresses as name servers', () => {
    expect(['', ' ', '1.1.1.1', '2606:4700:4700::1111'].every(validNameserver)).toBe(true);
    expect(['dns.google', '1.1.1', '1.1.1.1/32', '[::1]'].some(validNameserver)).toBe(false);
  });

  it('takes hostnames and IPs as time servers', () => {
    expect(['', '0.debian.pool.ntp.org', '192.168.1.1', 'ntp1'].every(validTimeServer)).toBe(true);
    expect(['pool ntp org', 'ntp.example.org;reboot', 'http://ntp.org'].some(validTimeServer)).toBe(false);
  });

  it('takes IANA time zones', () => {
    expect(['Europe/Berlin', 'UTC', 'America/Argentina/Buenos_Aires', 'Etc/GMT+1'].every(validTimezone)).toBe(true);
    expect(['', 'Europe/Berlin; reboot', '/etc/localtime', 'Europe//Berlin', 'a/b/c/d'].some(validTimezone)).toBe(false);
  });
});

describe('addressSuggestions', () => {
  it('offers the scanned IPs and the FQDN, without the current address', () => {
    expect(addressSuggestions('10.0.0.1, 192.168.1.5, 2001:db8::5', 'web01.example.org', '10.0.0.1')).toEqual(['192.168.1.5', '2001:db8::5', 'web01.example.org']);
  });

  it('drops loopback, link-local, short hostnames and duplicates', () => {
    expect(addressSuggestions('127.0.0.1, ::1, fe80::1, 169.254.3.4, 10.0.0.2, 10.0.0.2', 'web01', '')).toEqual(['10.0.0.2']);
  });

  it('compares case-insensitively and ignores IPv6 brackets', () => {
    expect(addressSuggestions('2001:db8::5', 'Web01.Example.org', '[2001:DB8::5]')).toEqual(['Web01.Example.org']);
    expect(addressSuggestions('', 'web01.example.org', 'WEB01.example.org')).toEqual([]);
  });
});
