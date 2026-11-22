// Schritt „Automatisches Backup“: Entwurf, Prüfung und Änderungen. Der Zeitplan geht über denselben Endpunkt wie in
// Einstellungen → Backups (`scheduleBody`); reine Logik, der Schritt steht in step-backup.ts.

import type { GlobalSettings } from '../../api/settings';
import { BACKUP_FIELDS, secretState } from '../settings/system/backup-model';
import { changedKeys, draftOf, validateDraft, type Draft, type FieldErrors } from '../settings/system/form-state';

/** Die Uhrzeit der Vorgabe liegt zwischen 02:00 und 05:30 im 10-Minuten-Raster: So starten nicht alle Installationen zur selben Minute. */
const FIRST_MINUTE = 2 * 60;
const LAST_MINUTE = 5 * 60 + 30;
const STEP_MINUTES = 10;

export function randomBackupTime(random: () => number = Math.random): string {
  const slots = (LAST_MINUTE - FIRST_MINUTE) / STEP_MINUTES + 1;
  const minute = FIRST_MINUTE + Math.floor(random() * slots) * STEP_MINUTES;
  const pad = (value: number): string => String(value).padStart(2, '0');
  return `${pad(Math.floor(minute / 60))}:${pad(minute % 60)}`;
}

/** Der Entwurf aus dem gespeicherten Zeitplan. Ist noch nichts eingerichtet (Backup aus oder ohne Uhrzeit), steht eine zufällige Uhrzeit darin. */
export function backupDraft(settings: GlobalSettings, random: () => number = Math.random): Draft {
  const draft = draftOf(settings, BACKUP_FIELDS);
  const unset = settings.backup_enabled !== true || String(draft.backup_time ?? '').trim() === '';
  return unset ? { ...draft, backup_time: randomBackupTime(random) } : draft;
}

/** Geplante Backups sind verschlüsselt: Ohne Empfänger-Schlüssel und ohne gespeicherte Passphrase braucht der Schritt eine Passphrase. */
export function needsPassphrase(draft: Draft, settings: GlobalSettings): boolean {
  return draft.backup_enabled === true && secretState(settings) === 'missing';
}

/** Was nicht durchgeht: Werte sind Übersetzungsschlüssel, die Schlüssel die der Felder. */
export function backupErrors(draft: Draft, settings: GlobalSettings): FieldErrors {
  const errors = validateDraft(draft, BACKUP_FIELDS);
  if (needsPassphrase(draft, settings) && String(draft.backup_passphrase ?? '').trim() === '' && !errors.backup_passphrase) {
    errors.backup_passphrase = 'setup.backup.passphraseRequired';
  }
  return errors;
}

/** Nur wenn sich etwas geändert hat, geht der Zeitplan an das Backend. */
export function backupChanged(draft: Draft, base: Draft): boolean {
  return changedKeys(draft, base, BACKUP_FIELDS).length > 0;
}
