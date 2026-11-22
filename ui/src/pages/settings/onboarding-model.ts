// Onboarding-Schlüssel auf einen neuen Server bringen: der Benutzer, bei dem LCM sich anmeldet, und die Befehle je Systemfamilie.

/** Debian, Ubuntu und Linux Mint; die RHEL-Familie (Rocky, Alma, Fedora) mit SELinux. Andere Systeme nehmen die Befehle von Debian. */
export type SystemFamily = 'debian' | 'rhel';

const SIMPLE_USER = /^[a-z_][a-z0-9_-]{0,31}$/i;

/** Der Benutzer, bei dem sich LCM mit dem Schlüssel anmeldet: die Vorgabe für neue Server, sonst `root`. */
export function onboardingUser(value: unknown): string {
  const user = typeof value === 'string' ? value.trim() : '';
  return SIMPLE_USER.test(user) ? user : 'root';
}

/** Ein Wert in einfachen Anführungszeichen für die Shell. */
function quote(value: string): string {
  return `'${value.replace(/'/g, `'\\''`)}'`;
}

/** Wechsel in eine Anmeldeshell des Benutzers, in dessen Verzeichnis der Schlüssel gehört. */
export function switchUserCommand(user: string): string {
  return `sudo -iu ${user}`;
}

/** Legt den Schlüssel im Verzeichnis des angemeldeten Benutzers ab: Verzeichnis 700, Datei 600; mit SELinux zusätzlich der Kontext. */
export function installKeyCommands(key: string, family: SystemFamily): string {
  const lines = ['mkdir -p ~/.ssh && chmod 700 ~/.ssh', `echo ${quote(key.trim())} >> ~/.ssh/authorized_keys`, 'chmod 600 ~/.ssh/authorized_keys'];
  if (family === 'rhel') lines.push('restorecon -R ~/.ssh');
  return lines.join('\n');
}
