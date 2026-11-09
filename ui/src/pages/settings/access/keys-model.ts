// API-Schlüssel: Formular, Prüfung und Status.

import type { ApiKey, KeyScope } from '../../../api/settings';
import { LIMITS, charCount, hasControlChars, isWholeNumber } from '../../../lib/validate';

export interface KeyForm {
  name: string;
  scope: KeyScope;
  /** Gültigkeit in Tagen; leer heißt ohne Ablauf. */
  days: string;
}

export function emptyKeyForm(scope: KeyScope): KeyForm {
  return { name: '', scope, days: '' };
}

export interface KeyFormErrors {
  name?: string;
  days?: string;
}

export function validateKeyForm(form: KeyForm): KeyFormErrors {
  const errors: KeyFormErrors = {};
  const name = form.name.trim();
  if (name === '') errors.name = 'settings.apikeys.form.nameRequired';
  else if (charCount(name) > LIMITS.name) errors.name = 'settings.apikeys.form.nameTooLong';
  else if (hasControlChars(name)) errors.name = 'check.oneLine';
  const days = form.days.trim();
  if (days !== '' && !isWholeNumber(days, 1, LIMITS.apiKeyDays)) errors.days = 'settings.apikeys.form.daysInvalid';
  return errors;
}

/** Tage bis zum Ablauf oder `null` ohne Ablauf. */
export function expiryDays(form: KeyForm): number | null {
  const days = form.days.trim();
  return days === '' ? null : Number(days);
}

export type KeyState = 'active' | 'revoked' | 'expired';

export function keyState(key: ApiKey, now: number = Date.now()): KeyState {
  if (key.revoked) return 'revoked';
  if (key.expiresAt && new Date(key.expiresAt).getTime() <= now) return 'expired';
  return 'active';
}

/** Welche Schlüssel gehören auf die Seite? Die REST-Seite zeigt keine MCP-Schlüssel und umgekehrt. */
export function keysFor(keys: ApiKey[], mode: 'rest' | 'mcp'): ApiKey[] {
  return keys.filter((key) => (mode === 'mcp') === (key.scope === 'mcp'));
}
