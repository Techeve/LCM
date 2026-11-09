import { describe, expect, it } from 'vitest';
import { filterVulns, normaliseBans, normaliseSecurityVulns, normaliseServerSecurity, normaliseSuggestions, parseVulnSource, publishedPorts, splitList } from './servers-security';

describe('normaliseServerSecurity', () => {
  it('reads the firewall, the SSH sources and the listening services from their JSON fields', () => {
    const s = normaliseServerSecurity({
      name: 'web01',
      ssh_port: 2222,
      firewall_active: true,
      firewall_tool: '',
      firewall_backend: 'nftables',
      firewall_rules: '[{"port":80,"proto":"tcp"}]',
      firewall_ssh_sources: '{"allowlist_ids":[3],"source_ips":["10.0.0.0/24"]}',
      listening_ports: '[{"port":9100,"proto":"tcp","bind":"10.0.0.1","ip_version":"v4","process":"node_exporter"}]',
      lcm_source_ip: '10.10.0.2',
      fail2ban_installed: true,
      crowdsec_active: true,
      ssh_2fa_enabled: true,
      has_acl: true,
    });
    expect(s).toMatchObject({ sshPort: 2222, firewallActive: true, firewallBackend: 'nftables', twoFactor: true, hasAcl: true, aclUsable: false, lcmSourceIp: '10.10.0.2' });
    expect(s.sshSources).toEqual({ allowlist_ids: [3], source_ips: ['10.0.0.0/24'] });
    expect(s.listening).toEqual([{ port: 9100, proto: 'tcp', bind: '10.0.0.1', ip_version: 'v4', process: 'node_exporter' }]);
    expect(s.installed).toEqual({ fail2ban: true, crowdsec: false });
    expect(s.active).toEqual({ fail2ban: false, crowdsec: true });
  });

  it('survives broken JSON and prefers the detected tool', () => {
    const s = normaliseServerSecurity({ firewall_ssh_sources: '{oops', listening_ports: 'nope', firewall_tool: 'ufw', firewall_backend: 'nftables' });
    expect(s.sshSources).toEqual({ allowlist_ids: [], source_ips: [] });
    expect(s.listening).toEqual([]);
    expect(s.firewallBackend).toBe('ufw');
    expect(s.sshPort).toBe(22);
  });
});

describe('publishedPorts', () => {
  it('takes the host ports of running containers only', () => {
    const ports = publishedPorts([
      { state: 'running', ports: '0.0.0.0:8443->443/tcp, :::8443->443/tcp, 0.0.0.0:8080->80/tcp' },
      { state: 'exited', ports: '0.0.0.0:9000->9000/tcp' },
      { state: 'running', ports: '5432/tcp' },
    ]);
    expect(ports).toEqual([8080, 8443]);
  });
});

describe('vulnerabilities', () => {
  const data = normaliseSecurityVulns({
    vulnerabilities: [
      { id: 'a', cve_id: 'CVE-1', package_name: 'openssl', severity: 'CRITICAL', source: 'os' },
      { id: 'b', cve_id: 'CVE-2', package_name: 'libc', severity: 'high', source: 'docker', image_ref: 'nginx:1', image_unused: true },
      { id: 'c', cve_id: 'CVE-3', package_name: 'curl', severity: 'low' },
    ],
    summary: { critical: 1, low: 1 },
    unused_summary: { high: 1, medium: 2 },
    last_scan_at: '2026-10-01T00:00:00Z',
  });

  it('reads source, image and the unused flag', () => {
    expect(data.items[0]).toMatchObject({ severity: 'critical', source: 'os', unused: false });
    expect(data.items[1]).toMatchObject({ source: 'docker', imageRef: 'nginx:1', unused: true });
    expect(data.items[2].source).toBe('os');
    expect(data.unusedCount).toBe(3);
    expect(data.scannerAvailable).toBe(true);
  });

  it('filters by source', () => {
    expect(filterVulns(data.items, '').map((v) => v.id)).toEqual(['a', 'b', 'c']);
    expect(filterVulns(data.items, 'os').map((v) => v.id)).toEqual(['a', 'c']);
    expect(filterVulns(data.items, 'docker').map((v) => v.id)).toEqual(['b']);
  });

  it('accepts only known sources from the address', () => {
    expect(parseVulnSource('docker')).toBe('docker');
    expect(parseVulnSource('os')).toBe('os');
    expect(parseVulnSource('snap')).toBe('');
    expect(parseVulnSource(null)).toBe('');
  });
});

describe('small readers', () => {
  it('reads bans with a stable id', () => {
    expect(normaliseBans({ bans: [{ ip: '1.2.3.4', scope: 'sshd', tool: 'fail2ban' }] })).toEqual([{ id: 'sshd|1.2.3.4', ip: '1.2.3.4', scope: 'sshd', since: '', cause: '' }]);
    expect(normaliseBans(null)).toEqual([]);
  });

  it('maps the backend kind of a suggestion', () => {
    expect(normaliseSuggestions([{ path: '/etc/nginx', kind: 'konfig', service_group: 'www-data' }, { path: '/srv', kind: 'daten' }]).map((s) => s.kind)).toEqual(['config', 'data']);
  });

  it('splits free text lists', () => {
    expect(splitList(' 1.2.3.4, 5.6.7.8\n10.0.0.0/8 ')).toEqual(['1.2.3.4', '5.6.7.8', '10.0.0.0/8']);
    expect(splitList('')).toEqual([]);
  });
});
