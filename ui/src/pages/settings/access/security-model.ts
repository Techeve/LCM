// Sicherheit: Rollen mit 2FA-Pflicht (kommagetrennt gespeichert) und Prüfung des CVE-Zeitplans.

import { parseCron } from '../../../components/cron';

/** Die Rollen aus `require_2fa_roles` (klein geschrieben, ohne Leereinträge und Doppelte). */
export function twoFactorRoles(value: string): string[] {
  const names = value
    .split(',')
    .map((name) => name.trim().toLowerCase())
    .filter(Boolean);
  return [...new Set(names)];
}

/** Schaltet eine Rolle in der kommagetrennten Liste um. */
export function toggleTwoFactorRole(value: string, role: string): string {
  const roles = twoFactorRoles(value);
  const name = role.toLowerCase();
  return (roles.includes(name) ? roles.filter((entry) => entry !== name) : [...roles, name]).join(',');
}

const DESCRIPTORS = ['@yearly', '@annually', '@monthly', '@weekly', '@daily', '@midnight', '@hourly'];
const MONTHS = ['jan', 'feb', 'mar', 'apr', 'may', 'jun', 'jul', 'aug', 'sep', 'oct', 'nov', 'dec'];
const WEEKDAYS = ['sun', 'mon', 'tue', 'wed', 'thu', 'fri', 'sat'];

/** Monats- und Tagesnamen (`JAN`, `MON`) werden zu Zahlen, wie sie der Server auch versteht. */
function withNumbers(expression: string): string {
  const fields = expression.trim().split(/\s+/);
  if (fields.length !== 5) return expression;
  const swap = (field: string, names: string[], offset: number) => field.replace(/[a-z]+/gi, (name) => {
    const index = names.indexOf(name.toLowerCase());
    return index < 0 ? name : String(index + offset);
  });
  fields[3] = swap(fields[3]!, MONTHS, 1);
  fields[4] = swap(fields[4]!, WEEKDAYS, 0);
  return fields.join(' ');
}

/**
 * Ein leerer Zeitplan lässt den gespeicherten stehen; sonst muss er ein gültiger Cron-Ausdruck mit fünf Feldern
 * oder ein Kürzel wie `@daily` sein. Der Server prüft dasselbe noch einmal.
 */
export function cronError(value: string): string | undefined {
  const expression = value.trim();
  if (expression === '') return undefined;
  if (expression.startsWith('@')) return DESCRIPTORS.includes(expression.toLowerCase()) || /^@every\s+\S+$/i.test(expression) ? undefined : 'settings.security.cve.cronInvalid';
  return parseCron(withNumbers(expression)) ? undefined : 'settings.security.cve.cronInvalid';
}

/** Sitzungsdauer: 0 heißt Vorgabe der Konfiguration; der Server hebt 1 bis 4 auf 5 Minuten an. */
export function sessionTtlError(value: string): string | undefined {
  const minutes = Number(value.trim());
  return Number.isInteger(minutes) && minutes > 0 && minutes < 5 ? 'settings.security.session.ttlTooShort' : undefined;
}

/** Trifft die 2FA-Pflicht dich selbst, ohne dass du 2FA eingerichtet hast? Dann musst du es gleich nach dem Speichern tun. */
export function hitsSelfWithout2fa(value: string, ownRoles: string[], totpEnabled: boolean): boolean {
  if (totpEnabled) return false;
  const chosen = twoFactorRoles(value);
  return ownRoles.some((role) => chosen.includes(role.toLowerCase()));
}
