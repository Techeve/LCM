// Formular einer Paketquelle im Katalog: Ausgangswerte, Prüfung (wie das Backend in
// `SaveKnownRepo`) und Körper für die API.

import type { KnownRepo } from '../../../api/settings-more';
import { LIMITS, charCount, hasControlChars } from '../../../lib/validate';

export const PACKAGE_MANAGERS = ['apt', 'dnf', 'zypper', 'pacman', 'apk'] as const;

/** Übersetzungsschlüssel je Paketverwaltung (vollständig ausgeschrieben, damit die Prüfung sie findet). */
export const MANAGER_KEYS: Record<string, string> = {
  apt: 'settings.repositories.manager.apt',
  dnf: 'settings.repositories.manager.dnf',
  zypper: 'settings.repositories.manager.zypper',
  pacman: 'settings.repositories.manager.pacman',
  apk: 'settings.repositories.manager.apk',
};

export const URL_HINT_KEYS: Record<string, string> = {
  dnf: 'settings.repositories.form.urlHint.dnf',
  zypper: 'settings.repositories.form.urlHint.zypper',
  pacman: 'settings.repositories.form.urlHint.pacman',
  apk: 'settings.repositories.form.urlHint.apk',
};

export interface RepoForm {
  name: string;
  key: string;
  package_manager: string;
  description: string;
  key_url: string;
  line: string;
}

export function emptyRepoForm(): RepoForm {
  return { name: '', key: '', package_manager: 'apt', description: '', key_url: '', line: '' };
}

export function repoFormOf(repo: KnownRepo): RepoForm {
  const { id: _id, ...form } = repo;
  return form;
}

export type RepoErrors = Partial<Record<'name' | 'key' | 'key_url' | 'line' | 'description', string>>;

const SLUG = /^[a-z0-9][a-z0-9-]*$/;
// Die Werte landen in Anführungszeichen auf dem Server; das Backend lehnt diese Zeichen ab.
const UNSAFE = /["'`\\]|\$\(/;

export function validateRepoForm(form: RepoForm): RepoErrors {
  const errors: RepoErrors = {};
  if (!form.name.trim()) errors.name = 'settings.repositories.form.nameRequired';
  else if (charCount(form.name.trim()) > LIMITS.name) errors.name = 'check.tooLong';
  else if (hasControlChars(form.name)) errors.name = 'check.oneLine';
  if (!SLUG.test(form.key.trim()) || charCount(form.key.trim()) > LIMITS.name) errors.key = 'settings.repositories.form.keyInvalid';
  if (charCount(form.description.trim()) > LIMITS.description) errors.description = 'check.tooLong';
  else if (hasControlChars(form.description)) errors.description = 'check.oneLine';
  const keyUrl = form.key_url.trim();
  if (charCount(keyUrl) > LIMITS.path) errors.key_url = 'check.tooLong';
  else if (keyUrl && !keyUrl.startsWith('https://')) errors.key_url = 'settings.repositories.form.keyUrlHttps';
  else if (UNSAFE.test(keyUrl)) errors.key_url = 'settings.repositories.form.unsafe';
  const line = form.line.trim();
  if (charCount(line) > LIMITS.path) errors.line = 'check.tooLong';
  else if (form.package_manager === 'apt' ? !line.startsWith('deb ') : !/^https?:\/\/\S+$/.test(line)) {
    errors.line = form.package_manager === 'apt' ? 'settings.repositories.form.lineDeb' : 'settings.repositories.form.lineUrl';
  } else if (UNSAFE.test(line) || line.includes('\n')) errors.line = 'settings.repositories.form.unsafe';
  return errors;
}

export function repoBody(form: RepoForm): Omit<KnownRepo, 'id'> {
  return {
    name: form.name.trim(),
    key: form.key.trim(),
    package_manager: form.package_manager,
    description: form.description.trim(),
    key_url: form.key_url.trim(),
    line: form.line.trim(),
  };
}
