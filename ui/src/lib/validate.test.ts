import { describe, expect, it } from 'vitest';
import {
  charCount,
  digitsOnly,
  hasControlChars,
  invalidAddresses,
  invalidDomains,
  invalidPackages,
  isAddressOrNet,
  isComposeName,
  isEmail,
  parseSender,
  senderProblem,
  isHost,
  isHostname,
  isIPv4,
  isIPv6,
  isLine,
  isLinuxUser,
  isPackageName,
  isSafeAbsolutePath,
  isPort,
  isTimezone,
  isWholeNumber,
  splitNames,
} from './validate';

describe('validate', () => {
  it('erkennt IPv4 ohne führende Nullen und über 255', () => {
    expect(isIPv4('192.168.1.10')).toBe(true);
    expect(isIPv4('0.0.0.0')).toBe(true);
    expect(isIPv4('256.1.1.1')).toBe(false);
    expect(isIPv4('01.2.3.4')).toBe(false);
    expect(isIPv4('1.2.3')).toBe(false);
  });

  it('erkennt IPv6 samt Kurzform und IPv4-Schwanz', () => {
    for (const ok of ['::1', '::', '2001:db8::1', 'fe80::1:2:3:4', '::ffff:192.0.2.1', '1:2:3:4:5:6:7:8']) expect(isIPv6(ok), ok).toBe(true);
    for (const bad of [':::', '1::2::3', ':1', '1:', 'abc', 'deadbeef', '1:2:3:4:5:6:7:8:9', '12345::1', 'g::1']) expect(isIPv6(bad), bad).toBe(false);
  });

  it('nimmt Adressen und Netze, aber keine Hostnamen als Quelle', () => {
    for (const ok of ['10.0.0.0/8', '10.1.2.3', '2001:db8::/32', '::/0', '0.0.0.0/0']) expect(isAddressOrNet(ok), ok).toBe(true);
    for (const bad of ['example.com', 'abc', '10.0.0.0/33', '2001:db8::/129', '10.0.0.0/', '10.0.0.0/8/8', '10.0.0.0/08']) expect(isAddressOrNet(bad), bad).toBe(false);
    expect(invalidAddresses('10.0.0.1, host.lan 2001:db8::1,x')).toEqual(['host.lan', 'x']);
  });

  it('prüft Hostnamen und Hosts', () => {
    expect(isHostname('web-01.example.com')).toBe(true);
    expect(isHostname('-bad.example')).toBe(false);
    expect(isHostname('a'.repeat(64))).toBe(false);
    expect(isHostname('with space')).toBe(false);
    expect(isHost('[2001:db8::1]')).toBe(true);
    expect(isHost('192.168.0.1')).toBe(true);
    expect(isHost('host;rm')).toBe(false);
  });

  it('prüft Ports und filtert Ziffern', () => {
    expect(isPort('22')).toBe(true);
    expect(isPort(65535)).toBe(true);
    expect(isPort('0')).toBe(false);
    expect(isPort('65536')).toBe(false);
    expect(isPort('8o')).toBe(false);
    expect(isPort('022')).toBe(false);
    expect(digitsOnly('8a0b8 0')).toBe('8080');
    expect(digitsOnly('1234567')).toBe('12345');
  });

  it('zählt Zeichen wie das Backend und verbietet Steuerzeichen', () => {
    expect(charCount('😀ä')).toBe(2);
    expect(isLine('Name', 4)).toBe(true);
    expect(isLine('Namen', 4)).toBe(false);
    expect(isLine('a\nb', 10)).toBe(false);
  });

  it('prüft E-Mail und Linux-Benutzer', () => {
    expect(isEmail('tony@example.com')).toBe(true);
    expect(isEmail('Tony <tony@example.com>')).toBe(false);
    expect(isEmail('kein-at.example.com')).toBe(false);
    expect(isLinuxUser('deploy_1')).toBe(true);
    expect(isLinuxUser('Root')).toBe(false);
    expect(isLinuxUser('1abc')).toBe(false);
    expect(isLinuxUser('web$')).toBe(false);
    expect(isLinuxUser('a'.repeat(33))).toBe(false);
  });
});

describe('Absender', () => {
  it('nimmt Adressen mit und ohne Anzeigenamen', () => {
    expect(senderProblem('lcm@example.com')).toBeUndefined();
    expect(senderProblem('LCM <lcm@example.com>')).toBeUndefined();
    expect(senderProblem('LCM Überwachung <lcm@example.com>')).toBeUndefined();
    expect(senderProblem('"LCM, Betrieb" <lcm@example.com>')).toBeUndefined();
    expect(senderProblem('<lcm@example.com>')).toBeUndefined();
    expect(parseSender(' LCM  <lcm@example.com> ')).toEqual({ name: 'LCM', address: 'lcm@example.com' });
    expect(parseSender('"LCM \\"Betrieb\\"" <lcm@example.com>')).toEqual({ name: 'LCM "Betrieb"', address: 'lcm@example.com' });
  });

  it('lehnt Unfug ab', () => {
    expect(senderProblem('')).toBeDefined();
    expect(senderProblem('LCM')).toBeDefined();
    expect(senderProblem('LCM <lcm>')).toBeDefined();
    expect(senderProblem('LCM <lcm@example.com')).toBeDefined();
    expect(senderProblem('LCM lcm@example.com>')).toBeDefined();
    expect(senderProblem('LCM, Betrieb <lcm@example.com>')).toBeDefined();
    expect(senderProblem('"LCM <lcm@example.com>')).toBeDefined();
    expect(senderProblem('a@example.com, b@example.com')).toBeDefined();
    expect(senderProblem('LCM <lcm@example.com>\nBcc: x@y.z')).toBeDefined();
  });

  it('begrenzt den Namen auf 64 und die Adresse auf 254 Zeichen', () => {
    expect(senderProblem(`${'n'.repeat(64)} <lcm@example.com>`)).toBeUndefined();
    expect(senderProblem(`${'n'.repeat(65)} <lcm@example.com>`)).toBe('name');
    expect(senderProblem(`LCM <${'a'.repeat(250)}@example.com>`)).toBe('invalid');
    expect(senderProblem('LCM')).toBe('invalid');
    expect(senderProblem('lcm@example.com')).toBeUndefined();
  });
});

describe('lists and names', () => {
  it('finds control characters, with line breaks only where allowed', () => {
    expect(hasControlChars('a\nb')).toBe(true);
    expect(hasControlChars('a\nb', true)).toBe(false);
    expect(hasControlChars('a\u0007b', true)).toBe(true);
    expect(hasControlChars('Müller 日本')).toBe(false);
  });

  it('splits names on commas, semicolons and white space', () => {
    expect(splitNames('htop, unzip;git\nvim\t curl')).toEqual(['htop', 'unzip', 'git', 'vim', 'curl']);
  });

  it('accepts package names as the backend does', () => {
    expect(['htop', 'libstdc++6', 'python3.12', 'OpenSSH-Server', 'a_b'].every(isPackageName)).toBe(true);
    expect(['-x', '.hidden', 'a b', 'a/b', 'a$(id)', ''].some(isPackageName)).toBe(false);
    expect(invalidPackages('htop, rm -rf, git;$x')).toEqual(['-rf', '$x']);
  });

  it('accepts compose project and container names', () => {
    expect(['shop', 'web_1', 'my-app.v2'].every(isComposeName)).toBe(true);
    expect(['-x', '_x', 'a b', 'a/b', ''].some(isComposeName)).toBe(false);
  });

  it('accepts DNS test domains', () => {
    expect(invalidDomains('intranet.example\ngit.example.org, a_b.c')).toEqual([]);
    expect(invalidDomains('ok.example, bad domain!, x/y')).toEqual(['domain!', 'x/y']);
  });

  it('accepts safe absolute paths only', () => {
    expect(['/var/lib/lcm/backups', '/srv/Backup 1', '/a/b..c'].every(isSafeAbsolutePath)).toBe(true);
    expect(['', 'relativ/pfad', '/a/../b', '/a;rm', '/a$HOME', '/a"b', '/a\nb', "/a'b"].some(isSafeAbsolutePath)).toBe(false);
  });

  it('accepts an IPv6 zone identifier on link-local addresses', () => {
    expect(isHost('fe80::1%eth0')).toBe(true);
    expect(isHost('10.0.0.1%eth0')).toBe(false);
    expect(isHost('fe80::1%')).toBe(false);
  });

  it('accepts time zones', () => {
    expect(['UTC', 'Europe/Berlin', 'America/Argentina/Buenos_Aires', 'Etc/GMT+1'].every(isTimezone)).toBe(true);
    expect(['', 'Europe/', '/etc/passwd', 'Europe/Berlin; id', '1x'].some(isTimezone)).toBe(false);
  });

  it('checks whole numbers in a range', () => {
    expect(isWholeNumber('0', 0, 10)).toBe(true);
    expect(isWholeNumber(' 10 ', 0, 10)).toBe(true);
    expect(['11', '-1', '1.5', '', '1e3', 'x'].some((v) => isWholeNumber(v, 0, 10))).toBe(false);
  });
});

describe('addresses like the backend', () => {
  it('refuses a network with host bits', () => {
    for (const entry of ['10.0.0.5/24', '2001:db8::1/32', '192.168.1.1/0']) expect(isAddressOrNet(entry), entry).toBe(false);
    for (const entry of ['10.0.0.0/24', '10.0.0.5/32', '0.0.0.0/0', '2001:db8::/32', '::/0', '::1/128', '::ffff:192.0.2.0/120']) expect(isAddressOrNet(entry), entry).toBe(true);
  });

  it('does not take a mistyped address for a hostname', () => {
    for (const value of ['300.1.1.1', '192.168.1.x', '192.168.1', '10']) expect(isHost(value), value).toBe(false);
    for (const value of ['server01', 'web-1.example.com', '1a.example.com', '10.0.0.5']) expect(isHost(value), value).toBe(true);
  });
});
