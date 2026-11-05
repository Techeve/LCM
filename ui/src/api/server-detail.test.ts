import { hasConsole } from './servers';
import { describe, expect, it } from 'vitest';
import { normaliseServer } from './servers';
import {
  clockState,
  distinctHostname,
  dnsLevel,
  filterByName,
  groupChanges,
  isPostQuantum,
  kernelStates,
  lcmHostMode,
  normaliseAppActions,
  normaliseConsoles,
  normaliseLcmHost,
  normaliseStatusDetail,
  packageManagers,
  platformOf,
  splitList,
  supportState,
} from './server-detail';

describe('Serverdetail: Einordnung der Rohwerte', () => {
  it('ordnet die Plattform aus systemd-detect-virt ein', () => {
    expect(platformOf('')).toEqual({ kind: 'unknown', name: '' });
    expect(platformOf('none')).toEqual({ kind: 'bare', name: '' });
    expect(platformOf('kvm')).toEqual({ kind: 'vm', name: 'KVM' });
    expect(platformOf('lxc')).toEqual({ kind: 'container', name: 'LXC' });
    expect(platformOf('bhyve')).toEqual({ kind: 'vm', name: 'Bhyve' });
  });

  it('nennt die Paketverwaltungen samt Snap', () => {
    expect(packageManagers({ package_manager: 'apt', has_snap: true })).toEqual(['APT (dpkg)', 'Snap']);
    expect(packageManagers({ package_manager: 'apk', has_snap: false })).toEqual(['apk (Alpine)']);
    expect(packageManagers({ package_manager: '', has_snap: false })).toEqual([]);
  });

  it('teilt Listen an Komma und Leerraum', () => {
    expect(splitList('10.0.0.1, 1.1.1.1  fe80::1')).toEqual(['10.0.0.1', '1.1.1.1', 'fe80::1']);
    expect(splitList('')).toEqual([]);
  });

  it('bewertet die Uhr erst ab 30 Sekunden Abweichung', () => {
    expect(clockState({ time_checked_at: null, clock_offset_seconds: 500 })).toBe('unknown');
    expect(clockState({ time_checked_at: '2026-01-01T00:00:00Z', clock_offset_seconds: 29 })).toBe('ok');
    expect(clockState({ time_checked_at: '2026-01-01T00:00:00Z', clock_offset_seconds: 30 })).toBe('ahead');
    expect(clockState({ time_checked_at: '2026-01-01T00:00:00Z', clock_offset_seconds: -94 })).toBe('behind');
  });

  it('übersetzt DNS-Test, Support und Schlüsselaustausch in Stufen', () => {
    expect([dnsLevel('full'), dnsLevel('partial'), dnsLevel('none'), dnsLevel('')]).toEqual(['ok', 'warn', 'crit', 'unknown']);
    const support = normaliseServer({ os_support: { known: true, supported: true, eol_soon: true } }).os_support;
    expect(supportState(support)).toBe('eol-soon');
    expect(supportState({ ...support, supported: false })).toBe('eol');
    expect(supportState({ ...support, known: false })).toBe('unknown');
    expect(isPostQuantum('mlkem768x25519-sha256')).toBe(true);
    expect(isPostQuantum('curve25519-sha256')).toBe(false);
  });

  it('meldet den Hostnamen nur, wenn er neu ist', () => {
    expect(distinctHostname({ hostname: 'web01', name: 'web01', host: '10.0.0.1' })).toBe('');
    expect(distinctHostname({ hostname: 'web01.intern', name: 'web01', host: '10.0.0.1' })).toBe('web01.intern');
  });
});

describe('Serverdetail: Antworten des Backends', () => {
  it('liest Kernel und Docker-Ports aus dem Status', () => {
    const status = normaliseStatusDetail({
      kernel: {
        running: '6.8.12-1-pve',
        managed: true,
        reboot_pending: true,
        installed: [{ name: 'k4', release: '6.8.12-4-pve' }, { name: 'k1', release: '6.8.12-1-pve', running: true }, { name: 'k0', release: '6.5' }, { name: 'kx', release: '6.4' }],
        removable: [{ name: 'kx' }],
      },
      docker_port_exposures: [{ container: 'web', host_ip: '0.0.0.0', host_port: '80', proto: 'tcp' }],
    });
    expect(status.kernel?.reboot_pending).toBe(true);
    expect(kernelStates(status.kernel!)).toEqual(['awaiting', 'running', 'fallback', 'removable']);
    expect(status.exposures).toHaveLength(1);
    expect(normaliseStatusDetail(null)).toEqual({ kernel: null, exposures: [] });
  });

  it('liest den LCM-Host-Status und wählt die Darstellung', () => {
    const status = normaliseLcmHost({ is_lcm_host: true, package_manager: 'apt', trivy_installed: true, scanner: { available: true, version: '0.67.2', freshness: 'fresh' } });
    expect(status.scanner?.version).toBe('0.67.2');
    expect(lcmHostMode(status)).toBe('setup');
    expect(lcmHostMode({ ...status, package_manager: 'dnf' })).toBe('not-apt');
    expect(lcmHostMode({ ...status, in_container: true })).toBe('container');
    expect(normaliseLcmHost({}).scanner).toBeNull();
  });

  it('übernimmt is_lcm_host aus der Serverliste', () => {
    expect(normaliseServer({ is_lcm_host: true }).is_lcm_host).toBe(true);
    expect(normaliseServer({}).is_lcm_host).toBe(false);
  });

  it('filtert offene Konsolen auf diesen Server', () => {
    const raw = [{ server_id: 3, actor: 'admin', since: 'x' }, { server_id: 4, actor: 'bob', since: 'y' }];
    expect(normaliseConsoles(raw, 3)).toEqual([{ server_id: 3, actor: 'admin', since: 'x' }]);
  });
});

describe('Serverdetail: Gruppen zuordnen', () => {
  it('berechnet, was hinzukommt und was wegfällt', () => {
    expect(groupChanges([1, 2], [2, 3])).toEqual({ add: [3], remove: [1] });
    expect(groupChanges([1], [1])).toEqual({ add: [], remove: [] });
  });

  it('filtert nach Namen ohne Groß-/Kleinschreibung', () => {
    const groups = [{ name: 'Produktion' }, { name: 'Staging' }];
    expect(filterByName(groups, 'prod')).toEqual([{ name: 'Produktion' }]);
    expect(filterByName(groups, '  ')).toEqual(groups);
  });
});

describe('Serverdetail: App-Aktionen', () => {
  it('ordnet Aktionen der App zu wie die App-Liste (ID, sonst Slug)', () => {
    const actions = normaliseAppActions({ detected: [{ id: 7, slug: 'nextcloud', can_backup: true, can_update: true }, { slug: 'gitea', can_update: true }] });
    expect(actions.get('7')).toEqual({ slug: 'nextcloud', can_backup: true, can_update: true });
    expect(actions.get('gitea')?.can_backup).toBe(false);
  });
});

describe('hasConsole', () => {
  const server = { os_id: 'debian', transport: 'ssh', console_disabled: false };

  it('gibt es über SSH', () => {
    expect(hasConsole(server)).toBe(true);
  });

  it('gibt es nicht über den Agenten, auf Geräten und wenn sie abgeschaltet ist', () => {
    expect(hasConsole({ ...server, transport: 'agent' })).toBe(false);
    expect(hasConsole({ ...server, os_id: 'routeros' })).toBe(false);
    expect(hasConsole({ ...server, console_disabled: true })).toBe(false);
  });
});
