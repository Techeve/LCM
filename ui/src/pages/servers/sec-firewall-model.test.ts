import { describe, expect, it } from 'vitest';
import type { ListeningPort } from '../../api/servers-security';
import { emptyFirewallRow } from '../groups/rule-model';
import { hasEditorErrors, initialRows, listeningSuggestions, openPortsSummary, ruleFromListening, rulesPayload, sshLocksOut, sshPayload, sshSourceErrors, withLcmAddress } from './sec-firewall-model';
import { emptyInstallForm, installPayload } from './sec-tool-install';
import { chosenTargets } from './sec-harden';

const port = (p: Partial<ListeningPort>): ListeningPort => ({ port: 80, proto: 'tcp', bind: '0.0.0.0', ip_version: 'v4', process: '', ...p });

describe('initialRows', () => {
  it('prefers the stored rules and falls back to the old port list', () => {
    expect(initialRows({ firewallRules: '[{"port":443,"proto":"udp"}]', firewallPorts: '80' }).map((r) => `${r.port}/${r.proto}`)).toEqual(['443/udp']);
    expect(initialRows({ firewallRules: '', firewallPorts: '80, 443' }).map((r) => r.port)).toEqual(['80', '443']);
    expect(initialRows({ firewallRules: '[]', firewallPorts: '' })).toEqual([]);
  });
});

describe('listeningSuggestions', () => {
  it('leaves out SSH, Docker, covered ports and the second wildcard socket', () => {
    const listening = [
      port({ port: 22, process: 'sshd' }),
      port({ port: 8080, process: 'docker-proxy' }),
      port({ port: 80, process: 'nginx' }),
      port({ port: 443, process: 'nginx' }),
      port({ port: 443, bind: '::', ip_version: 'v6', process: 'nginx' }),
      port({ port: 53, proto: 'udp', bind: '127.0.0.53', process: 'systemd-resolve' }),
    ];
    const rows = [{ ...emptyFirewallRow(), port: '80' }];
    expect(listeningSuggestions(listening, rows, 22, [8080]).map((l) => `${l.port}/${l.proto}`)).toEqual(['443/tcp', '53/udp']);
  });

  it('turns a suggestion into a rule with the service as note', () => {
    expect(ruleFromListening(port({ port: 9100, bind: '10.0.0.1', ip_version: 'v4', process: 'node_exporter' }))).toMatchObject({ port: '9100', proto: 'tcp', ip_version: 'v4', comment: 'node_exporter' });
    expect(ruleFromListening(port({ bind: '::', ip_version: 'v6' })).ip_version).toBe('any');
  });
});

describe('SSH sources', () => {
  it('warns only when own addresses restrict SSH without the LCM address', () => {
    expect(sshLocksOut({ allowlist_ids: [], source_ips: '' }, '10.0.0.2')).toBe(false);
    expect(sshLocksOut({ allowlist_ids: [], source_ips: '192.168.1.0/24' }, '10.0.0.2')).toBe(true);
    expect(sshLocksOut({ allowlist_ids: [], source_ips: '192.168.1.0/24, 10.0.0.2' }, '10.0.0.2')).toBe(false);
    expect(sshLocksOut({ allowlist_ids: [4], source_ips: '192.168.1.0/24' }, '10.0.0.2')).toBe(false);
    expect(sshLocksOut({ allowlist_ids: [], source_ips: '192.168.1.0/24' }, '')).toBe(false);
  });

  it('adds the LCM address once', () => {
    const once = withLcmAddress({ allowlist_ids: [], source_ips: '1.2.3.4' }, '10.0.0.2');
    expect(once.source_ips).toBe('1.2.3.4, 10.0.0.2');
    expect(withLcmAddress(once, '10.0.0.2')).toBe(once);
    expect(sshPayload(once)).toEqual({ allowlist_ids: [], source_ips: ['1.2.3.4', '10.0.0.2'] });
  });
});

describe('payload and validation', () => {
  it('leaves empty sources and notes out', () => {
    const rows = [
      { ...emptyFirewallRow(), port: '443', comment: ' Shop ' },
      { ...emptyFirewallRow(), port: '53', proto: 'udp' as const, ip_version: 'v4' as const, allowlist_ids: [2], source_ips: '10.0.0.0/8' },
    ];
    expect(rulesPayload(rows)).toEqual([
      { port: 443, proto: 'tcp', ip_version: 'any', comment: 'Shop' },
      { port: 53, proto: 'udp', ip_version: 'v4', allowlist_ids: [2], source_ips: ['10.0.0.0/8'] },
    ]);
    expect(openPortsSummary(rows, 22)).toBe('22/tcp, 443/tcp, 53/udp');
  });

  it('sends a TCP/UDP row as two rules and shows it as one entry', () => {
    const rows = [{ ...emptyFirewallRow(), port: '53', proto: 'both' as const }];
    expect(rulesPayload(rows)).toEqual([
      { port: 53, proto: 'tcp', ip_version: 'any' },
      { port: 53, proto: 'udp', ip_version: 'any' },
    ]);
    expect(openPortsSummary(rows, 22)).toBe('22/tcp, 53/tcp+udp');
  });

  it('counts a TCP/UDP row as covering the listening socket of both protocols', () => {
    const rows = [{ ...emptyFirewallRow(), port: '53', proto: 'both' as const }];
    const listening = [port({ port: 53, proto: 'udp' }), port({ port: 53, proto: 'tcp' })];
    expect(listeningSuggestions(listening, rows, 22, [])).toEqual([]);
  });

  it('names what is wrong with the SSH sources', () => {
    expect(sshSourceErrors({ source_ips: '10.0.0.1, host.lan' }).badSources).toEqual(['host.lan']);
    expect(sshSourceErrors({ source_ips: '10.0.0.1, 2001:db8::/32' }).sources).toBe(false);
  });

  it('finds bad ports and bad SSH sources', () => {
    const ok = { allowlist_ids: [], source_ips: '10.0.0.1' };
    expect(hasEditorErrors([{ ...emptyFirewallRow(), port: '80' }], ok)).toBe(false);
    expect(hasEditorErrors([{ ...emptyFirewallRow(), port: '70000' }], ok)).toBe(true);
    expect(hasEditorErrors([], { allowlist_ids: [], source_ips: 'not-an-ip' })).toBe(true);
  });
});

describe('security tool install', () => {
  it('sends only what the chosen tool understands', () => {
    const form = { ...emptyInstallForm('10.0.0.2'), tool: 'fail2ban' as const, allowlistIds: [1] };
    expect(installPayload(form)).toEqual({ tool: 'fail2ban', allowlist_ips: ['10.0.0.2'], allowlist_ids: [1] });
    expect(installPayload({ ...form, tool: 'crowdsec', lapiMode: 'remote', lapiId: 7 })).toEqual({
      tool: 'crowdsec',
      allowlist_ips: ['10.0.0.2'],
      allowlist_ids: [1],
      bouncer: true,
      collections: ['crowdsecurity/sshd'],
      lapi_mode: 'remote',
      lapi_id: 7,
    });
    expect(installPayload({ ...form, tool: 'crowdsec' })).not.toHaveProperty('lapi_id');
    expect(installPayload({ ...form, tool: 'ssh-2fa' })).toBeNull();
  });
});

describe('chosenTargets', () => {
  it('hardens the ticked suggestions with the group of their contents', () => {
    const suggestions = [
      { path: '/srv', mode: '755', group: 'root', serviceGroup: 'www-data', kind: 'data' as const },
      { path: '/opt', mode: '755', group: 'root', serviceGroup: '', kind: 'data' as const },
    ];
    expect(chosenTargets(suggestions, ['/srv'])).toEqual([{ path: '/srv', group: 'www-data', unit: '' }]);
  });
});
