import { describe, expect, it } from 'vitest';
import { normaliseKnownRepos, normalisePins, normaliseRepoState, normaliseUnknownApps, normaliseVersions, pinFor, pinInputValid, reposFor, versionLess } from './servers-packages';

describe('normalisePins', () => {
  const raw = {
    global: [{ id: 2, server_id: 0, name: 'openssl', no_remove: true, hold: true, note: 'Test' }],
    server: [{ id: 1, server_id: 7, name: 'linux-image-*', no_remove: true, hold: false }],
    available: true,
    kernel_preset: ['linux-image-*', 'linux-headers-*'],
  };

  it('lists global pins first, then the server ones', () => {
    const view = normalisePins(raw);
    expect(view.pins.map((p) => p.name)).toEqual(['openssl', 'linux-image-*']);
    expect(view.pins[0]).toMatchObject({ serverId: 0, noRemove: true, hold: true, note: 'Test' });
    expect(view.kernelPreset).toEqual(['linux-image-*', 'linux-headers-*']);
  });

  it('treats a missing answer as unavailable without pins', () => {
    expect(normalisePins(null)).toEqual({ pins: [], available: false, reason: '', kernelPreset: [] });
  });
});

describe('pinFor', () => {
  const pins = normalisePins({ global: [{ id: 1, name: 'linux-image-*' }, { id: 2, name: 'OpenSSL' }] }).pins;

  it('matches a trailing * as prefix', () => {
    expect(pinFor(pins, 'linux-image-6.1.0-13-amd64')?.id).toBe(1);
    expect(pinFor(pins, 'linux-headers-6.1')).toBeUndefined();
  });

  it('compares exact names without regard to case', () => {
    expect(pinFor(pins, 'openssl')?.id).toBe(2);
    expect(pinFor(pins, 'openssl-dev')).toBeUndefined();
  });
});

describe('pinInputValid', () => {
  it('needs a name and at least one effect', () => {
    expect(pinInputValid({ name: ' ', no_remove: true, hold: false, note: '', global: false })).toBe(false);
    expect(pinInputValid({ name: 'nginx', no_remove: false, hold: false, note: '', global: false })).toBe(false);
    expect(pinInputValid({ name: 'nginx', no_remove: false, hold: true, note: '', global: false })).toBe(true);
  });
});

describe('versions', () => {
  it('turns null (demo server) into an empty list', () => {
    expect(normaliseVersions(null)).toEqual([]);
    expect(normaliseVersions(['1.2', '', 3])).toEqual(['1.2']);
  });

  it('compares numeric parts as numbers', () => {
    expect(versionLess('1.9', '1.10')).toBe(true);
    expect(versionLess('1.10', '1.9')).toBe(false);
    expect(versionLess('7.88.1-10', '7.88.1-10+deb12u5')).toBe(true);
    expect(versionLess('2.0', '2.0')).toBe(false);
  });
});

describe('repositories', () => {
  it('offers only catalog sources for the server package manager; empty means apt', () => {
    const repos = normaliseKnownRepos([
      { key: 'docker', name: 'Docker CE', key_url: 'https://x/gpg' },
      { key: 'epel', name: 'EPEL', package_manager: 'dnf' },
    ]);
    expect(reposFor(repos, 'apt').map((r) => r.key)).toEqual(['docker']);
    expect(reposFor(repos, '').map((r) => r.key)).toEqual(['docker']);
    expect(reposFor(repos, 'dnf').map((r) => r.key)).toEqual(['epel']);
  });

  it('reads revert candidates and the APT cache from the server', () => {
    const state = normaliseRepoState({ package_manager: 'apt', https_revert_urls: 'http://a, http://b,', apt_proxy_active: true, apt_proxy_url: 'http://c:3142', proxmox_type: 'pve' });
    expect(state).toEqual({ packageManager: 'apt', proxmox: true, revertUrls: ['http://a', 'http://b'], aptProxyActive: true, aptProxyUrl: 'http://c:3142' });
  });
});

describe('normaliseUnknownApps', () => {
  it('reads the services without package', () => {
    expect(normaliseUnknownApps({ unknown: [{ id: 4, unit: 'gitea.service', exec_path: '/usr/local/bin/gitea', fragment_path: '/etc/systemd/system/gitea.service' }] })).toEqual([
      { id: '4', unit: 'gitea.service', execPath: '/usr/local/bin/gitea', fragmentPath: '/etc/systemd/system/gitea.service' },
    ]);
    expect(normaliseUnknownApps({})).toEqual([]);
  });
});
