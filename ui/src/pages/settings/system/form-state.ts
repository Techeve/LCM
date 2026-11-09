// Entwurf und Änderungen eines Einstellungsformulars: Ausgangswerte, Prüfung, Körper der Anfrage und
// Zuordnung von Serverfehlern zu Feldern. Das Backend prüft alles noch einmal; hier geht es um
// frühes, verständliches Feedback und darum, nur Geändertes zu senden.

import type { GlobalSettings } from '../../../api/settings';
import { parseCron } from '../../../components/cron';
import { charCount, hasControlChars, invalidDomains, isEmail, isHost, isHttpUrl, isSafeAbsolutePath, isTimezone, senderProblem } from '../../../lib/validate';

export type FieldKind = 'text' | 'int' | 'bool' | 'secret' | 'choice';

/**
 * Inhaltliche Prüfung eines Textfelds: `line` einzeilig, `text` mehrzeilig, sonst zusätzlich die Form
 * (`host`, `email`, `sender` mit oder ohne Anzeigenamen, `emails` mit Komma, Semikolon oder Zeilen, `url`, `cron`, `timezone`, `domains`, `path`).
 */
export type TextCheck = 'line' | 'text' | 'host' | 'email' | 'sender' | 'emails' | 'url' | 'cron' | 'timezone' | 'domains' | 'path';

export interface FieldSpec {
  key: string;
  kind: FieldKind;
  min?: number;
  max?: number;
  /** Höchstzahl Zeichen eines Textfelds, wie im Backend (`LIMITS`). */
  limit?: number;
  check?: TextCheck;
  /** Auswahlfeld: Wert, wenn der Server das Feld nicht (mehr) liefert, etwa aus einem älteren Feld abgeleitet. */
  fallback?: (settings: GlobalSettings) => string;
}

export const text = (key: string, limit: number, check: TextCheck = 'line'): FieldSpec => ({ key, kind: 'text', limit, check });
export const secret = (key: string, limit: number): FieldSpec => ({ key, kind: 'secret', limit, check: 'line' });
export const choice = (key: string, fallback?: FieldSpec['fallback']): FieldSpec => ({ key, kind: 'choice', fallback });
export const flag = (key: string): FieldSpec => ({ key, kind: 'bool' });
export const int = (key: string, min?: number, max?: number): FieldSpec => ({ key, kind: 'int', min, max });

export type Draft = Record<string, string | boolean>;

/** Fehlertext je Feld: ein Übersetzungsschlüssel oder fertiger Text vom Server. */
export type FieldErrors = Record<string, string>;

export function draftOf(settings: GlobalSettings, fields: FieldSpec[]): Draft {
  const draft: Draft = {};
  for (const field of fields) {
    const value = settings[field.key];
    if (field.kind === 'bool') draft[field.key] = value === true;
    else if (field.kind === 'secret') draft[field.key] = '';
    else if (field.kind === 'choice') draft[field.key] = typeof value === 'string' && value !== '' ? value : (field.fallback?.(settings) ?? '');
    else draft[field.key] = value === undefined ? '' : String(value);
  }
  return draft;
}

function whole(textValue: string): number | null {
  return /^-?\d+$/.test(textValue.trim()) ? Number(textValue.trim()) : null;
}

/** Wurde etwas geändert? Ein neu getipptes Geheimnis zählt, ein leeres Feld nicht. */
export function changedKeys(draft: Draft, base: Draft, fields: FieldSpec[]): string[] {
  return fields.filter((field) => draft[field.key] !== base[field.key]).map((field) => field.key);
}

export function isDirty(draft: Draft, base: Draft, fields: FieldSpec[]): boolean {
  return changedKeys(draft, base, fields).length > 0;
}

const EMAIL_SEPARATORS = /[,;\n\r]+/;

/** Die Adressen eines Felds mit mehreren Empfängern, ohne Leerstellen am Rand. */
export function emailEntries(value: string): string[] {
  return value.split(EMAIL_SEPARATORS).map((entry) => entry.trim()).filter(Boolean);
}

function textProblem(field: FieldSpec, raw: string): string | undefined {
  const value = raw.trim();
  if (field.limit !== undefined && charCount(value) > field.limit) return 'check.tooLong';
  if (hasControlChars(raw, field.check === 'text' || field.check === 'domains' || field.check === 'emails')) return field.check === 'line' || !field.check ? 'check.oneLine' : 'check.controlChars';
  if (value === '') return undefined;
  switch (field.check) {
    case 'host':
      return isHost(value) ? undefined : 'check.host';
    case 'email':
      return isEmail(value) ? undefined : 'check.email';
    case 'sender': {
      const problem = senderProblem(value);
      return problem === undefined ? undefined : problem === 'name' ? 'check.senderName' : 'check.sender';
    }
    case 'emails':
      return emailEntries(raw).every(isEmail) ? undefined : 'check.emails';
    case 'url':
      return isHttpUrl(value) ? undefined : 'check.url';
    case 'cron':
      return parseCron(value) === null ? 'check.cron' : undefined;
    case 'timezone':
      return isTimezone(value) ? undefined : 'check.timezone';
    case 'domains':
      return invalidDomains(value).length > 0 ? 'check.domains' : undefined;
    case 'path':
      return isSafeAbsolutePath(value) ? undefined : 'check.path';
    default:
      return undefined;
  }
}

/** Platzhalter der Fehlertexte eines Felds: Bereich, Grenze und die ungültigen Einträge. */
export function errorParams(field: FieldSpec | undefined, raw: string): Record<string, string | number> {
  const bad =
    field?.check === 'emails' ? emailEntries(raw).filter((entry) => !isEmail(entry)) : field?.check === 'domains' ? invalidDomains(raw) : [];
  return { min: field?.min ?? 0, max: field?.limit ?? field?.max ?? 0, list: bad.length > 3 ? `${bad.slice(0, 3).join(', ')} …` : bad.join(', ') };
}

/** Zahlenfelder müssen ganze Zahlen im erlaubten Bereich sein, Textfelder in Länge und Form passen; der Fehler ist ein Übersetzungsschlüssel. */
export function validateDraft(draft: Draft, fields: FieldSpec[]): FieldErrors {
  const errors: FieldErrors = {};
  for (const field of fields) {
    if (field.kind === 'text' || field.kind === 'secret') {
      const problem = textProblem(field, String(draft[field.key] ?? ''));
      if (problem) errors[field.key] = problem;
      continue;
    }
    if (field.kind !== 'int') continue;
    const value = whole(String(draft[field.key] ?? ''));
    if (value === null) errors[field.key] = 'settings.form.errors.number';
    else if ((field.min !== undefined && value < field.min) || (field.max !== undefined && value > field.max)) errors[field.key] = 'settings.form.errors.range';
  }
  return errors;
}

/** Nur die geänderten Felder, in der Form, die das Backend erwartet. */
export function patchBody(draft: Draft, base: Draft, fields: FieldSpec[]): Record<string, unknown> {
  const body: Record<string, unknown> = {};
  for (const field of fields) {
    if (draft[field.key] === base[field.key]) continue;
    const value = draft[field.key];
    if (field.kind === 'int') body[field.key] = whole(String(value)) ?? 0;
    else if (field.kind === 'text') body[field.key] = String(value).trim();
    else body[field.key] = value;
  }
  return body;
}

/**
 * Das Backend nennt den Wert, den es abgelehnt hat, im Fehlertext (`session_ttl_minutes muss zwischen …`).
 * Steht der Schlüssel eines Feldes darin, gehört der Fehler an dieses Feld; sonst bleibt er allgemein.
 */
export function fieldForDetail(detail: string, keys: string[]): string | undefined {
  const lower = detail.toLowerCase();
  return [...keys].sort((a, b) => b.length - a.length).find((key) => lower.includes(key));
}

/** Das Feld, auf das sich ein Fehler mit `params.field` (validation.*) bezieht. */
export function fieldOfParams(params: Record<string, unknown>, keys: string[]): string | undefined {
  const field = params.field;
  return typeof field === 'string' && keys.includes(field) ? field : undefined;
}

/**
 * Neue Werte vom Server übernehmen, ohne die Eingaben des Benutzers zu verlieren: Felder, die er nicht
 * angefasst hat (Entwurf = alter Stand), folgen dem Server; angefasste bleiben.
 */
export function mergeDraft(draft: Draft, oldBase: Draft, newBase: Draft, fields: FieldSpec[]): Draft {
  const merged: Draft = { ...draft };
  for (const field of fields) {
    if (draft[field.key] === oldBase[field.key]) merged[field.key] = newBase[field.key] as string | boolean;
  }
  return merged;
}
