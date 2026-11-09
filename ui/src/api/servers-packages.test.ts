import { describe, expect, it } from 'vitest';
import {
  httpsMirrorAlternative,
  initialHttpsSelection,
  httpsCheckMissingCerts,
  normaliseHttpsCheck,
  normaliseKnownRepos,
  normalisePins,
  normaliseRepoState,
  normaliseUnknownApps,
  normaliseVersions,
  pinFor,
  pinInputValid,
  repoUri,
  reposFor,
  toggleHttpsSelection,
  versionLess,
} from './servers-packages';

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
    expect(state).toEqual({ packageManager: 'apt', proxmox: true, revertUrls: ['http://a', 'http://b'], unavailableUrls: [], aptProxyActive: true, aptProxyUrl: 'http://c:3142' });
  });

  it('reads the sources without HTTPS from the last check', () => {
    expect(normaliseRepoState({ https_unavailable_urls: 'http://de.archive.ubuntu.com/ubuntu, ,http://x' }).unavailableUrls).toEqual(['http://de.archive.ubuntu.com/ubuntu', 'http://x']);
  });

  it('takes the source URL from both apt formats', () => {
    expect(repoUri('deb [arch=amd64] http://de.archive.ubuntu.com/ubuntu noble main')).toBe('http://de.archive.ubuntu.com/ubuntu');
    expect(repoUri('URIs: https://deb.debian.org/debian,')).toBe('https://deb.debian.org/debian');
    expect(repoUri('deb cdrom:[Debian]/ bookworm main')).toBe('');
  });
});

describe('HTTPS check before switching', () => {
  const results = normaliseHttpsCheck({
    results: [
      { uri: 'http://de.archive.ubuntu.com/ubuntu', state: 'no_https', alternative: 'https://archive.ubuntu.com/ubuntu' },
      { uri: 'http://security.ubuntu.com/ubuntu', state: 'ok' },
      { uri: 'http://repo.example/apt', state: 'seltsam' },
      { state: 'ok' },
    ],
  });

  it('reads one result per source, unknown states count as unchecked', () => {
    expect(results).toEqual([
      { uri: 'http://de.archive.ubuntu.com/ubuntu', state: 'no_https', alternative: 'https://archive.ubuntu.com/ubuntu' },
      { uri: 'http://security.ubuntu.com/ubuntu', state: 'ok', alternative: '' },
      { uri: 'http://repo.example/apt', state: 'unknown', alternative: '' },
    ]);
    expect(normaliseHttpsCheck(null)).toEqual([]);
    expect(httpsCheckMissingCerts({ results: [], missing_certificates: true })).toBe(true);
    expect(httpsCheckMissingCerts({ results: [] })).toBe(false);
  });

  it('preselects only the sources that speak HTTPS', () => {
    expect(initialHttpsSelection(results)).toEqual(['http://security.ubuntu.com/ubuntu']);
  });

  it('keeps the order of the check when toggling', () => {
    const chosen = toggleHttpsSelection(results, ['http://security.ubuntu.com/ubuntu'], 'http://repo.example/apt', true);
    expect(chosen).toEqual(['http://security.ubuntu.com/ubuntu', 'http://repo.example/apt']);
    expect(toggleHttpsSelection(results, chosen, 'http://security.ubuntu.com/ubuntu', false)).toEqual(['http://repo.example/apt']);
  });

  it('recommends the central Ubuntu and Debian mirrors like the backend', () => {
    expect(httpsMirrorAlternative('http://de.archive.ubuntu.com/ubuntu/')).toBe('https://archive.ubuntu.com/ubuntu');
    expect(httpsMirrorAlternative('http://de.ports.ubuntu.com/ubuntu-ports')).toBe('https://ports.ubuntu.com/ubuntu-ports');
    expect(httpsMirrorAlternative('http://ftp.de.debian.org/debian-ports')).toBe('');
    expect(httpsMirrorAlternative('http://archive.debian.org/debian')).toBe('');
    expect(httpsMirrorAlternative('http://ftp.de.debian.org/debian/')).toBe('https://deb.debian.org/debian');
    expect(httpsMirrorAlternative('http://ftp.de.debian.org/debian')).toBe('https://deb.debian.org/debian');
    expect(httpsMirrorAlternative('http://ftp.de.debian.org/debian-security')).toBe('https://security.debian.org/debian-security');
    // Die gescheiterte Adresse selbst ist keine Empfehlung, und Fremdquellen kennt LCM nicht.
    expect(httpsMirrorAlternative('http://archive.ubuntu.com/ubuntu')).toBe('');
    expect(httpsMirrorAlternative('http://debian.org.example/debian')).toBe('');
    expect(httpsMirrorAlternative('kaputt')).toBe('');
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
