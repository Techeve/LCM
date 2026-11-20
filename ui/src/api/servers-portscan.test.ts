import { describe, expect, it } from 'vitest';
import { canPortScan, familyLabel, hintKey, normalisePortScan, portScanUrl, serviceName, silentTargets } from './servers-portscan';
import { portRows } from '../pages/servers/sec-portscan';

const raw = {
  scanned_at: '2026-11-08T10:00:00Z',
  job_id: 'j1',
  targets: [
    { address: '192.0.2.10', family: 'v4', open: [22, 80, 3306], closed: 27, filtered: 0 },
    { address: '2001:db8::10', family: 'v6', open: [], closed: 0, filtered: 30 },
  ],
  open_ports: [
    { port: 22, service: 'ssh', families: ['v4'], in_firewall: true, risky: false, severity: 'ok', reasons: [] },
    { port: 80, service: 'http', families: ['v4'], in_firewall: false, risky: false, severity: 'warning', reasons: ['not_in_firewall'] },
    { port: 3306, service: 'mysql', families: ['v4'], in_firewall: true, risky: true, severity: 'critical', reasons: ['risky_service', 'erfunden'] },
  ],
  firewall_compared: true,
  firewall_active: true,
  critical: 1,
  warnings: 1,
};

describe('normalisePortScan', () => {
  it('reads the latest run with its rating', () => {
    const scan = normalisePortScan(raw);
    expect(scan.scannedAt).toBe('2026-11-08T10:00:00Z');
    expect(scan.targets[0]).toEqual({ address: '192.0.2.10', family: 'v4', open: [22, 80, 3306], closed: 27, filtered: 0 });
    expect(scan.ports.map((p) => `${p.port}:${p.severity}`)).toEqual(['22:ok', '80:warning', '3306:critical']);
    expect(scan.ports[2].reasons).toEqual(['risky_service']);
    expect(scan.firewallCompared).toBe(true);
  });

  it('treats a never scanned server and broken data as empty', () => {
    const never = normalisePortScan({ scanned_at: null, targets: [], open_ports: [], firewall_compared: false });
    expect(never.scannedAt).toBeNull();
    expect(never.ports).toEqual([]);
    const broken = normalisePortScan({ open_ports: [{ port: 'x', severity: 'egal', in_firewall: 'ja' }] });
    expect(broken.ports[0]).toMatchObject({ port: 0, severity: 'ok', inFirewall: null });
    expect(normalisePortScan(null).targets).toEqual([]);
  });
});

describe('port scan helpers', () => {
  it('builds the address of the endpoint from the server id only', () => {
    expect(portScanUrl(7)).toBe('/api/v1/servers/7/port-scan');
  });

  it('names services and hints, unknown ones stay as they are', () => {
    expect(serviceName('mysql')).toBe('MySQL/MariaDB');
    expect(serviceName('')).toBe('');
    expect(serviceName('custom')).toBe('custom');
    expect(hintKey('postgresql')).toBe('serverSecurity.portScan.hint.database');
    expect(hintKey('custom')).toBeNull();
    expect(familyLabel(['v4', 'v6'])).toBe('IPv4, IPv6');
  });

  it('scans only servers with an address that does not point at LCM itself', () => {
    expect(canPortScan({ host: '192.0.2.10', transport: 'ssh' })).toBe(true);
    expect(canPortScan({ host: 'nas.example', transport: 'ssh' })).toBe(true);
    expect(canPortScan({ host: ' Localhost ', transport: 'ssh' })).toBe(false);
    expect(canPortScan({ host: '127.0.0.1', transport: 'ssh' })).toBe(false);
    expect(canPortScan({ host: '', transport: 'agent' })).toBe(false);
    expect(canPortScan({ host: '192.0.2.10', transport: 'agent' })).toBe(false);
  });

  it('finds addresses that answered on no port at all', () => {
    expect(silentTargets(normalisePortScan(raw).targets)).toEqual(['2001:db8::10']);
  });

  it('lists critical ports first', () => {
    expect(portRows(normalisePortScan(raw)).map((r) => r.port)).toEqual([3306, 80, 22]);
  });
});
