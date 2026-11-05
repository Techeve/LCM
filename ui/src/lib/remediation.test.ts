import { describe, expect, it } from 'vitest';
import { remediationOf, remediationRedirect, REMEDIATION_PATH } from './remediation';

const user = (overrides: Partial<{ must_change_password: boolean; must_setup_2fa: boolean }> = {}) => ({
  must_change_password: false,
  must_setup_2fa: false,
  ...overrides,
});

describe('remediationOf', () => {
  it('is empty without a user or duty', () => {
    expect(remediationOf(null)).toBeNull();
    expect(remediationOf(user())).toBeNull();
  });

  it('asks for the password first, as the backend does', () => {
    expect(remediationOf(user({ must_change_password: true, must_setup_2fa: true }))).toBe('password');
    expect(remediationOf(user({ must_setup_2fa: true }))).toBe('two-factor');
  });
});

describe('remediationRedirect', () => {
  it('lets everything through without a duty', () => {
    expect(remediationRedirect(user(), '/servers')).toBeNull();
    expect(remediationRedirect(null, '/servers')).toBeNull();
  });

  it('sends every other page to the account security page and remembers it as next', () => {
    expect(remediationRedirect(user({ must_change_password: true }), '/')).toBe(`${REMEDIATION_PATH}?required=1`);
    for (const path of ['/servers/42', '/settings/alerts', '/account', '/nope']) {
      expect(remediationRedirect(user({ must_change_password: true }), path)).toBe(
        `${REMEDIATION_PATH}?required=1&next=${encodeURIComponent(path)}`,
      );
    }
  });

  it('keeps the account security page and its sub-paths', () => {
    expect(remediationRedirect(user({ must_setup_2fa: true }), REMEDIATION_PATH)).toBeNull();
    expect(remediationRedirect(user({ must_setup_2fa: true }), `${REMEDIATION_PATH}/totp`)).toBeNull();
    expect(remediationRedirect(user({ must_setup_2fa: true }), `${REMEDIATION_PATH}-evil`)).not.toBeNull();
  });
});
