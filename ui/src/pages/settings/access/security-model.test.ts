import { describe, expect, it } from 'vitest';
import { cronError, hitsSelfWithout2fa, sessionTtlError, toggleTwoFactorRole, twoFactorRoles } from './security-model';

describe('2FA-Pflicht', () => {
  it('liest die Rollen klein, ohne Leerstellen und Doppelte', () => {
    expect(twoFactorRoles(' Admin, manager,,admin ')).toEqual(['admin', 'manager']);
    expect(twoFactorRoles('')).toEqual([]);
  });

  it('warnt, wenn die Pflicht dich selbst ohne 2FA trifft', () => {
    expect(hitsSelfWithout2fa('admin', ['admin'], false)).toBe(true);
    expect(hitsSelfWithout2fa('admin', ['admin'], true)).toBe(false);
    expect(hitsSelfWithout2fa('manager', ['admin'], false)).toBe(false);
  });

  it('schaltet eine Rolle um', () => {
    expect(toggleTwoFactorRole('admin', 'manager')).toBe('admin,manager');
    expect(toggleTwoFactorRole('admin,manager', 'Admin')).toBe('manager');
    expect(toggleTwoFactorRole('', 'admin')).toBe('admin');
  });
});

describe('Zeitplan und Sitzungsdauer', () => {
  it('prüft Cron-Ausdrücke mit fünf Feldern, Namen und Kürzeln', () => {
    expect(cronError('30 2 * * *')).toBeUndefined();
    expect(cronError('')).toBeUndefined();
    expect(cronError('@daily')).toBeUndefined();
    expect(cronError('@every 6h')).toBeUndefined();
    expect(cronError('0 4 * JAN-MAR MON-FRI')).toBeUndefined();
    expect(cronError('0 4 * * FOO')).toBe('settings.security.cve.cronInvalid');
    expect(cronError('@sometimes')).toBe('settings.security.cve.cronInvalid');
    expect(cronError('30 2 * *')).toBe('settings.security.cve.cronInvalid');
    expect(cronError('61 2 * * *')).toBe('settings.security.cve.cronInvalid');
  });

  it('lehnt 1 bis 4 Minuten ab, 0 heißt Vorgabe', () => {
    expect(sessionTtlError('0')).toBeUndefined();
    expect(sessionTtlError('5')).toBeUndefined();
    expect(sessionTtlError('4')).toBe('settings.security.session.ttlTooShort');
  });
});
