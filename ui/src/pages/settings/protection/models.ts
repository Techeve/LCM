// Schutz: Formulare für CrowdSec-LAPI und IP-Allowlist, Prüfung wie im Backend und die Zuordnung
// der Server zu ihrer LAPI.

import type { Allowlist, Lapi, ServerLink } from '../../../api/settings-more';
import { LIMITS, charCount, hasControlChars, isAddressOrNet, splitList } from '../../../lib/validate';

/** Länge der Maschinen-Anmeldung an einer LAPI (`reLapiLogin` im Backend). */
export const LAPI_LOGIN_MAX = 128;

// Namen landen in Auswahllisten, nicht in Skripten: Umlaute sind erlaubt, Anführungszeichen nicht.
const BAD_NAME = /["'`\\\n\r\t]/;

function nameProblem(name: string, required: string, invalid: string): string | undefined {
  const value = name.trim();
  if (!value) return required;
  if (charCount(value) > LIMITS.name || BAD_NAME.test(value)) return invalid;
  return undefined;
}

export interface LapiForm {
  name: string;
  url: string;
  login: string;
  password: string;
}

export function emptyLapiForm(): LapiForm {
  return { name: '', url: '', login: 'lcm-managed', password: '' };
}

export function lapiFormOf(lapi: Lapi): LapiForm {
  return { name: lapi.name, url: lapi.url, login: lapi.login, password: '' };
}

export type LapiErrors = Partial<Record<keyof LapiForm, string>>;

export function validateLapiForm(form: LapiForm, isNew: boolean): LapiErrors {
  const errors: LapiErrors = {};
  const name = nameProblem(form.name, 'settings.crowdsec.form.nameRequired', 'settings.crowdsec.form.nameInvalid');
  if (name) errors.name = name;
  const url = form.url.trim();
  if (!/^https?:\/\/[^\s/'"`\\]+[^\s'"`\\]*$/.test(url) || charCount(url) > LIMITS.path) errors.url = 'settings.crowdsec.form.urlInvalid';
  if (!/^[A-Za-z0-9._@-]+$/.test(form.login.trim()) || charCount(form.login.trim()) > LAPI_LOGIN_MAX) errors.login = 'settings.crowdsec.form.loginInvalid';
  if (isNew && !form.password) errors.password = 'settings.crowdsec.form.passwordRequired';
  else if (charCount(form.password) > LIMITS.password) errors.password = 'check.tooLong';
  return errors;
}

export function lapiBody(form: LapiForm): Omit<Lapi, 'id'> & { password: string } {
  return { name: form.name.trim(), url: form.url.trim().replace(/\/+$/, ''), login: form.login.trim(), password: form.password };
}

const trimSlash = (url: string) => url.replace(/\/+$/, '');

/** Server mit CrowdSec, die laut ihrer Konfiguration an eine der hinterlegten LAPIs melden. */
export function lapiServers(servers: ServerLink[], lapis: Lapi[]): ServerLink[] {
  const urls = new Set(lapis.map((lapi) => trimSlash(lapi.url)));
  return servers.filter((server) => server.crowdsec_installed && urls.has(trimSlash(server.crowdsec_lapi_url)));
}

export function lapiNameFor(lapis: Lapi[], url: string): string {
  return lapis.find((lapi) => trimSlash(lapi.url) === trimSlash(url))?.name ?? url;
}

// ---- IP-Allowlist --------------------------------------------------------------------------------

export interface AllowlistForm {
  name: string;
  description: string;
  entries: string;
}

export type AllowlistErrors = Partial<Record<keyof AllowlistForm, string>>;

/** Einträge aus dem Textfeld: getrennt durch Zeilen, Kommas oder Leerzeichen. */
export function allowlistEntries(text: string): string[] {
  return splitList(text);
}

export function invalidEntries(text: string): string[] {
  return allowlistEntries(text).filter((entry) => !isAddressOrNet(entry));
}

export function validateAllowlistForm(form: AllowlistForm): AllowlistErrors {
  const errors: AllowlistErrors = {};
  const name = nameProblem(form.name, 'settings.allowlists.form.nameRequired', 'settings.allowlists.form.nameInvalid');
  if (name) errors.name = name;
  if (charCount(form.description.trim()) > LIMITS.description) errors.description = 'check.tooLong';
  else if (hasControlChars(form.description)) errors.description = 'check.oneLine';
  if (charCount(form.entries) > LIMITS.script) errors.entries = 'check.tooLong';
  else if (invalidEntries(form.entries).length > 0) errors.entries = 'settings.allowlists.form.entriesInvalid';
  return errors;
}

export function allowlistBody(form: AllowlistForm): Omit<Allowlist, 'id'> {
  return { name: form.name.trim(), description: form.description.trim(), entries: allowlistEntries(form.entries).join('\n') };
}

/** Kurzvorschau für die Tabelle: die ersten Einträge, Rest als Zahl. */
export function entryPreview(entries: string, shown = 3): { text: string; more: number } {
  const list = allowlistEntries(entries);
  return { text: list.slice(0, shown).join(', '), more: Math.max(0, list.length - shown) };
}
