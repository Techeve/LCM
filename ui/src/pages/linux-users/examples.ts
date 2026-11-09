// Beispielwerte für Eingabefelder: Befehle, Pfade und Namen, die in jeder Sprache gleich bleiben.

export const EXAMPLES = {
  blockCommand: '/usr/bin/systemctl --no-pager restart {service}',
  blockPathRule: 'readwrite /etc/nginx',
  blockParams: 'service,path',
  sudoCommand: '/usr/bin/systemctl restart nginx',
  editFile: '/etc/nginx/sites-available/kunde.conf',
  directory: '/srv/www',
  publicKey: 'ssh-ed25519 AAAA…',
  username: 'deploy',
} as const;
