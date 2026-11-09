// Öffentliche SSH-Schlüssel: grobe Prüfung vor dem Senden, Anzeigename und Download des privaten Schlüssels.
// Maßgeblich prüft das Backend (OpenSSH-Format); hier geht es um schnelle Rückmeldung beim Einfügen.

import type { SshKey } from '../../api/linux-users';
import { saveBlob } from '../../lib/download';

const KEY_TYPE = /^(ssh-[a-z0-9]+|ecdsa-sha2-[a-z0-9]+|sk-[a-z0-9@.-]+)$/;
const BASE64 = /^[A-Za-z0-9+/]+={0,3}$/;

export interface ParsedKey {
  type: string;
  comment: string;
}

/** Zerlegt eine Zeile im OpenSSH-Format („Typ Schlüssel Kommentar"); `null`, wenn sie so nicht aussieht. */
export function parsePublicKey(line: string): ParsedKey | null {
  const [type, body, ...rest] = line.trim().split(/\s+/);
  if (!type || !body || !KEY_TYPE.test(type) || !BASE64.test(body)) return null;
  return { type, comment: rest.join(' ') };
}

/** Name, sonst Kommentar, sonst Typ des Schlüssels. */
export function keyLabel(key: SshKey): string {
  if (key.name) return key.name;
  const parsed = parsePublicKey(key.public_key);
  return parsed?.comment || parsed?.type || key.public_key.slice(0, 24);
}

/** Typ und Anfang und Ende des Schlüsselkörpers, damit sich zwei Schlüssel unterscheiden lassen. */
export function keySummary(publicKey: string): string {
  const [type = '', body = ''] = publicKey.trim().split(/\s+/);
  const short = body.length > 20 ? `${body.slice(0, 8)}…${body.slice(-8)}` : body;
  return `${type} ${short}`.trim();
}

/** Dateiname des privaten Schlüssels, wie ihn die Anleitung nennt. */
export function privateKeyFile(username: string): string {
  return `id_ed25519_${username}`;
}

/** Bietet Text als Datei zum Speichern an. */
export function downloadText(filename: string, content: string): void {
  saveBlob(filename, new Blob([content], { type: 'application/octet-stream' }));
}
