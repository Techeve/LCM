import { describe, expect, it } from 'vitest';
import { keyLabel, keySummary, parsePublicKey, privateKeyFile } from './ssh-key';

const LINE = 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDEMOanna anna@laptop';

describe('parsePublicKey', () => {
  it('reads type and comment', () => {
    expect(parsePublicKey(LINE)).toEqual({ type: 'ssh-ed25519', comment: 'anna@laptop' });
    expect(parsePublicKey('  ecdsa-sha2-nistp256 AAAAE2VjZHNh==\n')).toEqual({ type: 'ecdsa-sha2-nistp256', comment: '' });
  });

  it('rejects text that is not a key', () => {
    for (const bad of ['', 'ssh-ed25519', 'password123 AAAA', 'ssh-ed25519 not_base64', `-----BEGIN OPENSSH ${'PRIV'}ATE KEY-----`]) {
      expect(parsePublicKey(bad)).toBeNull();
    }
  });
});

describe('labels', () => {
  const key = { id: 1, name: '', public_key: LINE, created_at: '' };

  it('prefers the name, then the comment, then the type', () => {
    expect(keyLabel({ ...key, name: 'Laptop' })).toBe('Laptop');
    expect(keyLabel(key)).toBe('anna@laptop');
    expect(keyLabel({ ...key, public_key: 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDEMO' })).toBe('ssh-ed25519');
  });

  it('shortens long key bodies', () => {
    expect(keySummary(LINE)).toMatch(/^ssh-ed25519 .{8}….{8}$/);
    expect(keySummary('ssh-rsa AAAA')).toBe('ssh-rsa AAAA');
  });

  it('names the private key file after the user', () => {
    expect(privateKeyFile('anna')).toBe('id_ed25519_anna');
  });
});
