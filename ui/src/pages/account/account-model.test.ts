import { describe, expect, it } from 'vitest';
import { cleanTotpCode, groupSecret } from '../../api/account';
import { continueTarget, draftOf, emailChanged, profileDirty, safeQrSource, sectionOf } from './account-model';
import { newPasswordState } from './new-password';

describe('sectionOf', () => {
  it('opens the profile without a section and rejects unknown ones', () => {
    expect(sectionOf(undefined)).toBe('profile');
    expect(sectionOf('security')).toBe('security');
    expect(sectionOf('nope')).toBeNull();
  });
});

describe('continueTarget', () => {
  it('continues to an internal next', () => {
    expect(continueTarget('/servers/42?tab=packages')).toBe('/servers/42?tab=packages');
  });

  it('falls back to the dashboard for foreign, login or remediation targets', () => {
    for (const next of [null, '', '//evil.example', 'https://evil.example', '/login?next=/x', '/account/security', '/account/security?required=1']) {
      expect(continueTarget(next)).toBe('/');
    }
  });
});

describe('profile draft', () => {
  const saved = draftOf({ email: 'Anna@Example.com', first_name: 'Anna', last_name: 'Berg' });

  it('ignores case and spaces in the mail address', () => {
    expect(emailChanged({ ...saved, email: ' anna@example.com ' }, saved)).toBe(false);
    expect(emailChanged({ ...saved, email: 'anna@berg.de' }, saved)).toBe(true);
  });

  it('is dirty only with a real change', () => {
    expect(profileDirty({ ...saved }, saved)).toBe(false);
    expect(profileDirty({ ...saved, lastName: 'Berg ' }, saved)).toBe(false);
    expect(profileDirty({ ...saved, lastName: 'Bergmann' }, saved)).toBe(true);
  });

  it('reads missing fields as empty', () => {
    expect(draftOf(null)).toEqual({ email: '', firstName: '', lastName: '' });
  });
});

describe('newPasswordState', () => {
  it('is valid only when the rules hold and both fields match', () => {
    expect(newPasswordState('Nordlicht-Wal!73x', 'Nordlicht-Wal!73x', {}).valid).toBe(true);
    expect(newPasswordState('Nordlicht-Wal!73x', 'Nordlicht-Wal!73', {}).mismatch).toBe(true);
    expect(newPasswordState('Nordlicht-Wal!73x', '', {})).toMatchObject({ valid: false, mismatch: false });
    expect(newPasswordState('kurz', 'kurz', {}).valid).toBe(false);
  });
});

describe('two-factor helpers', () => {
  it('keeps six digits of a pasted code', () => {
    expect(cleanTotpCode('123 456 7')).toBe('123456');
  });

  it('groups the secret in fours', () => {
    expect(groupSecret('ABCDEFGHIJ')).toBe('ABCD EFGH IJ');
  });

  it('accepts only a PNG data URI as QR code', () => {
    expect(safeQrSource('data:image/png;base64,iVBORw0KGgo=')).toBe('data:image/png;base64,iVBORw0KGgo=');
    expect(safeQrSource('javascript:alert(1)')).toBe('');
    expect(safeQrSource('data:image/svg+xml;base64,PHN2Zz4=')).toBe('');
  });
});
