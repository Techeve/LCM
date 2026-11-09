import { describe, expect, it } from 'vitest';
import { linuxActivationBody } from '../../api/provisioning';
import { activationMode } from './activate-page';
import { canActivate, publicKeyProblem, type LinuxActivationDraft } from './linux-activate-page';

const KEY = 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl anna@laptop';

const draft = (patch: Partial<LinuxActivationDraft> = {}): LinuxActivationDraft => ({
  password: '',
  passwordValid: false,
  keyMode: 'none',
  publicKey: '',
  ...patch,
});

describe('activationMode', () => {
  it('reads the mode from the path', () => {
    expect(activationMode('/password-reset')).toBe('reset');
    expect(activationMode('/activate')).toBe('activate');
  });
});

describe('linux activation', () => {
  it('needs a password or a key', () => {
    expect(canActivate(draft())).toBe(false);
    expect(canActivate(draft({ password: 'Nordlicht-Wal!73x', passwordValid: true }))).toBe(true);
    expect(canActivate(draft({ keyMode: 'generate' }))).toBe(true);
    expect(canActivate(draft({ keyMode: 'paste', publicKey: KEY }))).toBe(true);
  });

  it('rejects a typed password that breaks the rules, even with a key', () => {
    expect(canActivate(draft({ password: 'kurz', passwordValid: false, keyMode: 'generate' }))).toBe(false);
  });

  it('checks a pasted key', () => {
    expect(publicKeyProblem(draft({ keyMode: 'paste' }))).toBe('missing');
    expect(publicKeyProblem(draft({ keyMode: 'paste', publicKey: 'hallo welt' }))).toBe('invalid');
    expect(publicKeyProblem(draft({ keyMode: 'paste', publicKey: KEY }))).toBe('');
    expect(canActivate(draft({ keyMode: 'paste', publicKey: 'hallo welt' }))).toBe(false);
  });

  it('sends only what the chosen key mode needs', () => {
    const base = { token: 't', password: '', keyName: ' Laptop ', publicKey: KEY };
    expect(linuxActivationBody({ ...base, keyMode: 'none' })).toMatchObject({ key_name: '', public_key: '', generate_key: false });
    expect(linuxActivationBody({ ...base, keyMode: 'paste' })).toMatchObject({ key_name: 'Laptop', public_key: KEY, generate_key: false });
    expect(linuxActivationBody({ ...base, keyMode: 'generate' })).toMatchObject({ key_name: 'Laptop', public_key: '', generate_key: true });
  });
});
