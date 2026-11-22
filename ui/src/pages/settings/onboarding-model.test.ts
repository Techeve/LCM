import { describe, expect, it } from 'vitest';
import { installKeyCommands, onboardingUser, switchUserCommand } from './onboarding-model';

const KEY = 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample lcm-onboarding';

describe('Benutzer des Onboarding-Schlüssels', () => {
  it('nimmt die Vorgabe für neue Server', () => {
    expect(onboardingUser('deploy')).toBe('deploy');
    expect(onboardingUser(' admin ')).toBe('admin');
  });

  it('fällt auf root zurück, wenn nichts oder etwas Unsicheres steht', () => {
    expect(onboardingUser('')).toBe('root');
    expect(onboardingUser(undefined)).toBe('root');
    expect(onboardingUser('a; rm -rf /')).toBe('root');
    expect(onboardingUser('$(id)')).toBe('root');
  });

  it('wechselt mit sudo in die Anmeldeshell des Benutzers', () => {
    expect(switchUserCommand('root')).toBe('sudo -iu root');
  });
});

describe('Befehle für den Schlüssel', () => {
  it('Verzeichnis 700, Schlüssel anhängen, Datei 600', () => {
    expect(installKeyCommands(KEY, 'debian').split('\n')).toEqual([
      'mkdir -p ~/.ssh && chmod 700 ~/.ssh',
      `echo '${KEY}' >> ~/.ssh/authorized_keys`,
      'chmod 600 ~/.ssh/authorized_keys',
    ]);
  });

  it('RHEL-Familie: zusätzlich der SELinux-Kontext', () => {
    expect(installKeyCommands(KEY, 'rhel').split('\n').at(-1)).toBe('restorecon -R ~/.ssh');
  });

  it('schützt einfache Anführungszeichen im Schlüssel', () => {
    expect(installKeyCommands("ssh-ed25519 AAAA it's", 'debian')).toContain(`echo 'ssh-ed25519 AAAA it'\\''s' >>`);
  });
});
