import { describe, expect, it } from 'vitest';
import {
  assignableUsers,
  composeGroups,
  groupByCategory,
  imageActions,
  loginKind,
  loginSeconds,
  monitorBody,
  monitorMode,
  normaliseLogins,
  normaliseOpsDeepScan,
  normaliseOpsDocker,
  normaliseOpsStorage,
  normaliseOpsUsers,
  normaliseServerFlags,
  normaliseStorageHistory,
  parseCveRelevant,
  parseThreshold,
  parseTitles,
  shortDigest,
  userActions,
  vulnsByImage,
  type OpsContainer,
  type OpsImage,
} from './servers-ops';

const container = (name: string, project = '', service = ''): OpsContainer => ({ id: name, name, image: 'x', state: 'running', status: '', ports: '', project, service });
const image = (patch: Partial<OpsImage>): OpsImage => ({ id: 'i', ref: 'nginx:1', tag: '1', digest: '', size: '', inUse: true, superseded: false, olderCopies: 0, updateAvailable: false, checkStatus: '', critical: 0, high: 0, ...patch });

describe('server flags', () => {
  it('reads the first item of the list answer', () => {
    const flags = normaliseServerFlags({ items: [{ docker_updates_disabled: true, cve_relevant_containers: ' Web , db,,', ssh_2fa_enabled: true }], total: 1 });
    expect(flags).toEqual({ dockerUpdatesDisabled: true, cveRelevant: ['web', 'db'], ssh2fa: true });
  });

  it('is empty for nothing', () => {
    expect(normaliseServerFlags(null)).toEqual({ dockerUpdatesDisabled: false, cveRelevant: [], ssh2fa: false });
    expect(parseCveRelevant('')).toEqual([]);
  });
});

describe('docker', () => {
  it('keeps superseded images and builds the reference', () => {
    const data = normaliseOpsDocker({
      has_docker: true,
      containers: [{ id: 'c1', name: 'web', compose_project: 'shop', compose_service: 'web' }],
      images: [
        { id: 'a', repository: 'nginx', tag: '1.25', update_available: true, check_status: 'ok', critical_vulns: 2, older_copies: 1 },
        { id: 'b', repository: 'nginx', tag: '', superseded: true, check_status: 'weird' },
      ],
    });
    expect(data.hasDocker).toBe(true);
    expect(data.hasCompose).toBe(false);
    expect(data.containers[0]).toMatchObject({ project: 'shop', service: 'web' });
    expect(data.images.map((i) => i.ref)).toEqual(['nginx:1.25', 'nginx']);
    expect(data.images[0]).toMatchObject({ updateAvailable: true, checkStatus: 'ok', critical: 2, olderCopies: 1 });
    expect(data.images[1]).toMatchObject({ superseded: true, checkStatus: '' });
  });

  it('groups containers by project with standalone last', () => {
    const groups = composeGroups([container('a'), container('b', 'zeta'), container('c', 'alpha'), container('d', 'zeta')]);
    expect(groups.map((g) => g.project)).toEqual(['alpha', 'zeta', '']);
    expect(groups[1]!.containers.map((c) => c.name)).toEqual(['b', 'd']);
  });

  it('shortens digests', () => {
    expect(shortDigest('')).toBe('–');
    expect(shortDigest('nginx@sha256:0123456789abcdef0123')).toBe('sha256:0123456789ab…');
    expect(shortDigest('sha256:abcdef')).toBe('sha256:abcdef…');
  });

  it('offers pull only for tagged images with an update and delete only for unused ones', () => {
    expect(imageActions(image({ updateAvailable: true }))).toEqual({ pull: true, remove: false });
    expect(imageActions(image({ updateAvailable: true, tag: '' }))).toEqual({ pull: false, remove: false });
    expect(imageActions(image({ inUse: false }))).toEqual({ pull: false, remove: true });
    expect(imageActions(image({ inUse: false, superseded: true }))).toEqual({ pull: false, remove: false });
  });

  it('maps vulnerabilities to image references', () => {
    const map = vulnsByImage([image({ ref: 'a:1', high: 3 }), image({ ref: 'b:1' })]);
    expect(map.get('a:1')).toEqual({ critical: 0, high: 3 });
    expect(map.has('b:1')).toBe(false);
  });
});

describe('users', () => {
  it('reads accounts and their login source', () => {
    const [user] = normaliseOpsUsers([{ id: 3, username: 'anna', uid: 1001, password_status: 'none', ssh_key_count: 2, two_factor_enrolled: true, login_count: 3, logins_from_lcm: true, managed: true }]);
    expect(user).toMatchObject({ username: 'anna', twoFactor: true, loginCount: 3, loginsFromLcm: true, managed: true, lastLogin: null });
  });

  it('names how an account logs in', () => {
    expect(loginKind({ passwordStatus: 'set', sshKeys: 2 })).toBe('password');
    expect(loginKind({ passwordStatus: 'none', sshKeys: 1 })).toBe('keyOnly');
    expect(loginKind({ passwordStatus: 'unknown', sshKeys: 0 })).toBe('unknown');
    expect(loginKind({ passwordStatus: 'none', sshKeys: 0 })).toBe('none');
  });

  it('protects root and the service user and keeps managed accounts', () => {
    const [root, svc, managed, local] = normaliseOpsUsers([{ username: 'root' }, { username: 'lcm-svc' }, { username: 'anna', managed: true }, { username: 'old' }]);
    expect(userActions(root!, 'lcm-svc')).toEqual({ toggle: false, remove: false });
    expect(userActions(svc!, 'lcm-svc')).toEqual({ toggle: false, remove: false });
    expect(userActions(managed!, 'lcm-svc')).toEqual({ toggle: true, remove: false });
    expect(userActions(local!, 'lcm-svc')).toEqual({ toggle: true, remove: true });
  });

  it('computes the session length only for finished logins', () => {
    const [done, active, open] = normaliseLogins([
      { id: 1, started_at: '2026-11-03T12:00:00Z', ended_at: '2026-11-03T13:35:00Z' },
      { id: 2, started_at: '2026-11-04T12:00:00Z', still_active: true },
      { id: 3, started_at: '2026-11-04T12:00:00Z', ended_at: null },
    ]);
    expect(loginSeconds(done!)).toBe(95 * 60);
    expect(loginSeconds(active!)).toBeNull();
    expect(loginSeconds(open!)).toBeNull();
  });

  it('offers only active LCM users that are not on the server yet', () => {
    const linux = [
      { id: 1, username: 'anna', full_name: 'Anna', active: true },
      { id: 2, username: 'bob', full_name: '', active: true },
      { id: 3, username: 'carl', full_name: '', active: false },
    ];
    expect(assignableUsers(linux, [{ username: 'anna' }])).toEqual([{ id: 2, username: 'bob', fullName: '' }]);
  });
});

describe('deep scan', () => {
  it('reads reports with their progress', () => {
    const data = normaliseOpsDeepScan({
      findings: [{ id: 'f', severity: 'WARNING', category: 'restart', title: 't', is_new: true }],
      kernel_vulns: null,
      reports: [{ id: 'r1', created_at: '2026-01-01T12:00:00Z', warnings: 1, new_findings: 1, resolved_findings: 2, resolved_titles: '["a","b"]', hardening_index: 72, tools: 'lynis' }],
      hardening_index: 72,
      deep_scan_at: '2026-01-01T12:00:00Z',
      kernel_reboot_pending: true,
    });
    expect(data.findings[0]).toMatchObject({ severity: 'warning', isNew: true });
    expect(data.kernelVulns).toEqual([]);
    expect(data.reports[0]).toMatchObject({ id: 'r1', newFindings: 1, resolvedFindings: 2, resolvedTitles: ['a', 'b'], hardeningIndex: 72 });
    expect(data.kernelRebootPending).toBe(true);
    expect(data.scannerAvailable).toBe(true);
  });

  it('treats unreadable resolved titles as none', () => {
    expect(parseTitles('')).toEqual([]);
    expect(parseTitles('{nope')).toEqual([]);
    expect(parseTitles('[1,"x"]')).toEqual(['x']);
  });

  it('groups findings by category in their order', () => {
    const groups = groupByCategory([
      { id: '1', severity: 'critical', category: 'kernel', title: 'a', detail: '', isNew: false },
      { id: '2', severity: 'info', category: 'misconfig', title: 'b', detail: '', isNew: false },
      { id: '3', severity: 'warning', category: 'kernel', title: 'c', detail: '', isNew: false },
    ]);
    expect(groups.map((g) => [g.category, g.findings.map((f) => f.id)])).toEqual([
      ['kernel', ['1', '3']],
      ['misconfig', ['2']],
    ]);
  });
});

describe('storage', () => {
  const storage = normaliseOpsStorage({
    volumes: [
      { mountpoint: '/', monitorable: false, usage_percent: 31 },
      { mountpoint: '/data', monitorable: true, monitor: { warn_percent: 80, crit_percent: 95, inode_warn_percent: 70 } },
      { mountpoint: '/boot', monitorable: true },
      { mountpoint: '/mnt/nas', monitorable: false, is_network: true },
    ],
    storage_health: [{ id: 2, kind: 'mdraid', name: 'md0', state: 'healthy', raw_state: 'active', meta_percent: 4 }],
  });

  it('reads monitoring and pools', () => {
    expect(storage.volumes[1]!.monitor).toEqual({ warn: 80, crit: 95, inodeWarn: 70 });
    expect(storage.pools[0]).toMatchObject({ rawState: 'active', metaPercent: 4 });
    expect(storage.volumes.map(monitorMode)).toEqual(['root', 'monitored', 'off', 'network']);
  });

  it('keeps the other limits when the warning threshold changes', () => {
    expect(monitorBody(storage.volumes[1]!, true, 85)).toEqual({ mountpoint: '/data', enabled: true, warn_percent: 85, crit_percent: 95, inode_warn_percent: 70 });
    expect(monitorBody(storage.volumes[2]!, false, 0)).toEqual({ mountpoint: '/boot', enabled: false, warn_percent: 0, crit_percent: 0, inode_warn_percent: 0 });
  });

  it('accepts whole percents from 1 to 100', () => {
    expect(parseThreshold('85')).toBe(85);
    expect(parseThreshold(' 1 ')).toBe(1);
    expect(parseThreshold('100')).toBe(100);
    expect(parseThreshold('0')).toBeNull();
    expect(parseThreshold('101')).toBeNull();
    expect(parseThreshold('8.5')).toBeNull();
    expect(parseThreshold('')).toBeNull();
  });

  it('turns the history into daily percents', () => {
    const history = normaliseStorageHistory({
      history: [{ day: '2026-11-01', disk_total_mb: 1000, disk_used_mb: 250 }, { day: '2026-11-02', disk_total_mb: 0, disk_used_mb: 5 }],
      current_percent: 31,
      forecast: { days_remaining: 40, unlimited: false, insufficient_data: false },
    });
    expect(history.points.map((p) => p.percent)).toEqual([25, 0]);
    expect(history.forecast).toEqual({ days: 40, unlimited: false, insufficient: false });
    expect(normaliseStorageHistory({}).forecast).toBeNull();
  });
});
