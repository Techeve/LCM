// Backups: Schlüsselstand, Körper der Zeitplan-Anfrage und Formate. Reine Funktionen ohne Oberfläche.

import type { BackupScheduleBody, GlobalSettings } from '../../../api/settings';
import { LIMITS } from '../../../lib/validate';
import { flag, int, secret, text, type Draft, type FieldSpec } from './form-state';

export const BACKUP_FIELDS: FieldSpec[] = [
  flag('backup_enabled'),
  int('backup_interval_hours', 1, 8760),
  text('backup_time', LIMITS.name),
  int('backup_retention', 1, 3650),
  text('backup_dir', LIMITS.path, 'path'),
  text('backup_recipients', LIMITS.list, 'text'),
  secret('backup_passphrase', LIMITS.password),
  flag('restore_auto_restart'),
];

export type SecretState = 'recipients' | 'passphrase' | 'missing';

/** Womit werden geplante Backups verschlüsselt? Empfänger-Schlüssel gehen vor der Passphrase. */
export function secretState(settings: GlobalSettings): SecretState {
  if (settings.backup_recipients_set === true) return 'recipients';
  if (settings.backup_passphrase_set === true) return 'passphrase';
  return 'missing';
}

/** Automatische Backups sind eingeschaltet, können aber ohne Schlüssel nicht laufen. */
export function scheduleBlocked(settings: GlobalSettings): boolean {
  return settings.backup_enabled === true && secretState(settings) === 'missing';
}

function whole(value: string | boolean | undefined, fallback: number): number {
  const trimmed = String(value ?? '').trim();
  const parsed = Number(trimmed);
  return trimmed !== '' && Number.isInteger(parsed) ? parsed : fallback;
}

/** Der Endpunkt ersetzt den ganzen Zeitplan; deshalb gehen immer alle Felder mit. */
export function scheduleBody(draft: Draft): BackupScheduleBody {
  return {
    enabled: draft.backup_enabled === true,
    interval_hours: whole(draft.backup_interval_hours, 24),
    retention: whole(draft.backup_retention, 14),
    time: String(draft.backup_time ?? '').trim(),
    dir: String(draft.backup_dir ?? '').trim(),
    auto_restart: draft.restore_auto_restart === true,
    passphrase: String(draft.backup_passphrase ?? ''),
    recipients: String(draft.backup_recipients ?? ''),
  };
}

/** Fügt einen öffentlichen Schlüssel an die Empfängerliste (einer je Zeile) an, ohne Doppelte. */
export function addRecipient(list: string, publicKey: string): string {
  const lines = list.split('\n').map((line) => line.trim()).filter(Boolean);
  if (lines.includes(publicKey)) return lines.join('\n');
  return [...lines, publicKey].join('\n');
}

/** Dateiname ohne Pfad für die Anzeige beim Hochladen. */
export function isBackupFile(name: string): boolean {
  return name.toLowerCase().endsWith('.lcmbak');
}
