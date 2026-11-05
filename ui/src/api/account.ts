// Eigenes Konto: Profil, Passwort, Zwei-Faktor-Anmeldung. Die Sprache speichert
// `auth.changeLanguage` (PATCH /auth/me/preferences), Theme und Dichte bleiben im Browser.

import { api } from '../lib/api';

export interface ProfileInput {
  email: string;
  first_name: string;
  last_name: string;
  /** Nur nötig, wenn sich die E-Mail-Adresse ändert: sie empfängt die Links zum Zurücksetzen. */
  current_password: string;
}

export interface PasswordChange {
  password: string;
  current_password: string;
  /** Code der Authenticator-App, wenn 2FA aktiv ist. */
  code: string;
}

/** Antwort von `POST /auth/2fa/setup`: das Geheimnis, solange 2FA noch nicht aktiv ist. */
export interface TotpSetup {
  secret: string;
  provisioning_uri: string;
  /** PNG als data:-URI (die CSP erlaubt `img-src data:`). */
  qr_code: string;
}

export function updateProfile(userId: number, input: ProfileInput): Promise<unknown> {
  return api.patch(`/api/v1/users/${userId}/profile`, input);
}

/** Ändert das eigene Passwort. Danach gelten alle Sitzungen des Kontos als beendet. */
export function changeOwnPassword(userId: number, change: PasswordChange): Promise<void> {
  return api.post(`/api/v1/users/${userId}/reset-password`, change);
}

export function startTotpSetup(): Promise<TotpSetup> {
  return api.post<TotpSetup>('/api/v1/auth/2fa/setup');
}

export function enableTotp(code: string): Promise<void> {
  return api.post('/api/v1/auth/2fa/enable', { code });
}

export function disableTotp(code: string): Promise<void> {
  return api.post('/api/v1/auth/2fa/disable', { code });
}

/** Zeichen, die ein Code-Feld annimmt: sechs Ziffern und das Leerzeichen der Gruppierung „123 456“. */
export const TOTP_INPUT_MAX = 7;

/** Zieht die Gruppierung „123 456“ zusammen: höchstens sechs Ziffern. */
export function cleanTotpCode(raw: string): string {
  return raw.replace(/\D/g, '').slice(0, 6);
}

/** Das Geheimnis in Vierergruppen, damit es sich abtippen lässt. */
export function groupSecret(secret: string): string {
  return secret.replace(/\s+/g, '').replace(/(.{4})(?=.)/g, '$1 ');
}
