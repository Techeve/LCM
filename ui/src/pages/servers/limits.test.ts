import { describe, expect, it } from 'vitest';
import { pinInputValid, pinNameProblem } from '../../api/servers-packages';
import { dsmProblems, emptyDsmForm, emptyRouterOSForm, emptySshForm, hostProblem, routerOSProblems, sshProblems } from './join/join-model';
import { hardenErrors } from './sec-harden';
import { emptyInstallForm, installErrors } from './sec-tool-install';

describe('Server hinzufügen: Grenzen', () => {
  it('prüft Hosts mit isHost, auch IPv6 mit Zone, und lehnt zu lange ab', () => {
    for (const ok of ['web01', 'web01.lan', '10.0.0.5', '2001:db8::1', '[2001:db8::1]', 'fe80::1%eth0']) expect(hostProblem(ok), ok).toBeUndefined();
    for (const bad of ['http://x', 'a b', 'a/b', 'a..b', '-x.lan']) expect(hostProblem(bad), bad).toBe('host');
    expect(hostProblem('a'.repeat(254))).toBe('long');
    expect(hostProblem('  ')).toBe('required');
  });

  it('begrenzt Name, Anmeldename und Passwort in allen Verbindungsarten', () => {
    const ssh = { ...emptySshForm(), name: 'web01', host: '10.0.0.5' };
    expect(sshProblems(ssh)).toEqual({});
    expect(sshProblems({ ...ssh, name: 'n'.repeat(65) }).name).toBe('long');
    expect(sshProblems({ ...ssh, name: 'a\nb' }).name).toBe('line');
    expect(sshProblems({ ...ssh, loginUser: 'u'.repeat(65) }).loginUser).toBe('long');
    expect(sshProblems({ ...ssh, password: 'p'.repeat(1025) }).password).toBe('long');
    expect(sshProblems({ ...ssh, authMethod: 'key', password: 'p'.repeat(1025) }).password).toBeUndefined();
    const router = { ...emptyRouterOSForm(), name: 'r1', host: '192.168.88.1' };
    expect(routerOSProblems({ ...router, password: 'p'.repeat(1025) }).password).toBe('long');
    const nas = { ...emptyDsmForm(), name: 'nas', host: '10.0.0.9', account: 'admin', password: 'x' };
    expect(dsmProblems(nas)).toEqual({});
    expect(dsmProblems({ ...nas, account: 'a'.repeat(65) }).account).toBe('long');
    expect(dsmProblems({ ...nas, port: '99999' }).port).toBe('port');
  });
});

describe('Server: Verzeichnisse abschotten', () => {
  it('lehnt relative Pfade, Sonderzeichen, Platzhalter und schlechte Gruppen oder Dienste ab', () => {
    expect(hardenErrors({ path: '/srv/kundendaten', group: 'www-data', unit: 'nginx' })).toEqual({});
    expect(hardenErrors({ path: 'srv', group: '', unit: '' }).path).toBe('serverSecurity.harden.error.relative');
    expect(hardenErrors({ path: '/srv/a;rm', group: '', unit: '' }).path).toBe('serverSecurity.harden.error.meta');
    expect(hardenErrors({ path: '/srv/*', group: '', unit: '' }).path).toBe('serverSecurity.harden.error.meta');
    expect(hardenErrors({ path: '/srv', group: 'Root!', unit: '' }).group).toBe('check.linuxUser');
    expect(hardenErrors({ path: '/srv', group: '', unit: 'ng inx' }).unit).toBe('serverSecurity.harden.error.unit');
  });
});

describe('Server: Sicherheitswerkzeug und Pins', () => {
  it('nennt ungültige Allowlist-Einträge und Collections', () => {
    const form = { ...emptyInstallForm('10.0.0.2'), tool: 'crowdsec' as const };
    expect(installErrors(form)).toEqual({ allowlistIps: [], collections: [] });
    expect(installErrors({ ...form, allowlistIps: '10.0.0.2 host.lan 10.0.0.0/33' }).allowlistIps).toEqual(['host.lan', '10.0.0.0/33']);
    expect(installErrors({ ...form, collections: 'crowdsecurity/sshd bad$name' }).collections).toEqual(['bad$name']);
    expect(installErrors({ ...form, tool: 'fail2ban', collections: 'bad$name' }).collections).toEqual([]);
  });

  it('nimmt Pin-Namen wie das Backend', () => {
    for (const ok of ['nginx', 'linux-image-*', 'libstdc++6', 'Python3.12']) expect(pinNameProblem(ok), ok).toBeUndefined();
    expect(pinNameProblem('a b')).toBe('serverPackages.pins.new.nameInvalid');
    expect(pinNameProblem('*')).toBe('serverPackages.pins.new.nameInvalid');
    expect(pinNameProblem('a'.repeat(129))).toBe('check.tooLong');
    const pin = { name: 'nginx', no_remove: true, hold: false, note: '', global: false };
    expect(pinInputValid(pin)).toBe(true);
    expect(pinInputValid({ ...pin, name: 'ng$inx' })).toBe(false);
    expect(pinInputValid({ ...pin, note: 'n'.repeat(1001) })).toBe(false);
  });
});
