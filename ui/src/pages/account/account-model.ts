// Reine Logik der Kontoseite: Abschnitte, Rückkehrziel nach dem Pflichtschritt, Profiländerungen.

import type { CurrentUser } from '../../lib/auth';
import { REMEDIATION_PATH } from '../../lib/remediation';
import { safeNext } from '../../lib/safe-path';
import { LIMITS, charCount, hasControlChars, isEmail } from '../../lib/validate';
import type { PasswordIdentity } from './password-policy';

export const ACCOUNT_SECTIONS = ['profile', 'security', 'preferences'] as const;
export type AccountSection = (typeof ACCOUNT_SECTIONS)[number];

export function sectionOf(raw: string | undefined): AccountSection | null {
  if (!raw) return 'profile';
  return ACCOUNT_SECTIONS.find((section) => section === raw) ?? null;
}

/** Wohin es nach dem Pflichtschritt weitergeht: `next`, wenn es ein interner Pfad außerhalb der Kontosicherheit ist. */
export function continueTarget(next: string | null): string {
  const target = safeNext(next);
  if (!target || target === REMEDIATION_PATH || target.startsWith(`${REMEDIATION_PATH}?`)) return '/';
  return target;
}

export interface ProfileDraft {
  email: string;
  firstName: string;
  lastName: string;
}

export function draftOf(user: Pick<CurrentUser, 'email' | 'first_name' | 'last_name'> | null): ProfileDraft {
  return { email: user?.email ?? '', firstName: user?.first_name ?? '', lastName: user?.last_name ?? '' };
}

export type ProfileErrors = Partial<Record<keyof ProfileDraft, string>>;

/** Fehlerschlüssel je Feld; leer heißt gültig. Die E-Mail-Adresse darf leer bleiben, wie im Backend. */
export function profileErrors(draft: ProfileDraft): ProfileErrors {
  const errors: ProfileErrors = {};
  const email = draft.email.trim();
  if (email !== '' && !isEmail(email)) errors.email = 'check.email';
  for (const key of ['firstName', 'lastName'] as const) {
    if (charCount(draft[key].trim()) > LIMITS.personName) errors[key] = 'check.tooLong';
    else if (hasControlChars(draft[key])) errors[key] = 'check.oneLine';
  }
  return errors;
}

const sameMail = (a: string, b: string) => a.trim().toLowerCase() === b.trim().toLowerCase();

/** Eine neue E-Mail-Adresse verlangt das aktuelle Passwort (sie empfängt die Links zum Zurücksetzen). */
export function emailChanged(draft: ProfileDraft, saved: ProfileDraft): boolean {
  return !sameMail(draft.email, saved.email);
}

export function profileDirty(draft: ProfileDraft, saved: ProfileDraft): boolean {
  return emailChanged(draft, saved) || draft.firstName.trim() !== saved.firstName.trim() || draft.lastName.trim() !== saved.lastName.trim();
}

/** Angaben, die nicht im eigenen Passwort stehen dürfen. */
export function identityOf(user: Pick<CurrentUser, 'username' | 'email' | 'first_name' | 'last_name'> | null): PasswordIdentity {
  return { username: user?.username, email: user?.email, firstName: user?.first_name, lastName: user?.last_name };
}

/** Nur ein PNG als data:-URI darf als QR-Code ins Bild. */
export function safeQrSource(value: string | undefined): string {
  return value && /^data:image\/png;base64,[A-Za-z0-9+/=]+$/.test(value) ? value : '';
}
