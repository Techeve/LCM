import { describe, expect, it } from 'vitest';
import { DEFAULT_MAIL_SECURITY, MAIL_SECURITY, mailSecurityOptions, portForSecurity, readMailSecurity, securityFromLegacy } from './mail-security';

describe('Verschlüsselung des Mailversands', () => {
  it('liest den Modus, sonst das alte use_tls, sonst STARTTLS', () => {
    expect(readMailSecurity('tls', false)).toBe(MAIL_SECURITY.tls);
    expect(readMailSecurity('none', true)).toBe(MAIL_SECURITY.none);
    expect(readMailSecurity(undefined, false)).toBe(MAIL_SECURITY.none);
    expect(readMailSecurity(undefined, true)).toBe(MAIL_SECURITY.starttls);
    expect(readMailSecurity('', undefined)).toBe(MAIL_SECURITY.starttls);
    expect(readMailSecurity('ssl', undefined)).toBe(MAIL_SECURITY.starttls);
  });

  it('bildet das alte Ja/Nein ab', () => {
    expect(securityFromLegacy(true)).toBe('starttls');
    expect(securityFromLegacy(false)).toBe('none');
  });

  it('neue Kanäle starten mit STARTTLS', () => {
    expect(DEFAULT_MAIL_SECURITY).toBe('starttls');
  });

  it('setzt den Port mit, wenn er auf einem Standardport steht', () => {
    expect(portForSecurity('587', MAIL_SECURITY.tls)).toBe('465');
    expect(portForSecurity('465', MAIL_SECURITY.none)).toBe('25');
    expect(portForSecurity('25', MAIL_SECURITY.starttls)).toBe('587');
    expect(portForSecurity(' 587 ', MAIL_SECURITY.none)).toBe('25');
  });

  it('lässt einen eigenen Port stehen', () => {
    expect(portForSecurity('2525', MAIL_SECURITY.tls)).toBe('2525');
    expect(portForSecurity('', MAIL_SECURITY.tls)).toBe('');
  });

  it('bietet drei Modi mit dem üblichen Port an', () => {
    const options = mailSecurityOptions();
    expect(options.map((o) => o.value)).toEqual(['tls', 'starttls', 'none']);
    expect(options.every((o) => o.label.length > 0)).toBe(true);
  });
});
