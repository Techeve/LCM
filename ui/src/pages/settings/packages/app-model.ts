// Steckbrief einer Anwendung im Katalog: Formular, Prüfung (wie `domain.ValidateAppEntry`) und
// Körper für die API. Merkmale und Versionskommando laufen beim Scan als root auf jedem passenden
// Server, deshalb ist die Prüfung hier genauso streng wie im Backend.

import type { AppBody, AppEntry } from '../../../api/settings-more';
import { LIMITS, charCount, hasControlChars } from '../../../lib/validate';

export const COMPARE_KEYS: Record<string, string> = {
  semver: 'settings.apps.form.compareSemver',
  exact: 'settings.apps.form.compareExact',
  none: 'settings.apps.form.compareNone',
};

export const MARKER_KINDS = ['path', 'unit', 'bin', 'proc'] as const;

// Zeichen, die das Backend in Merkmalen ablehnt (`unsafeShellChars`); Leerzeichen trennt Art und Wert.
const UNSAFE_MARKER = /[&|$`'"\\;<>(){}*?[\]!\t ]/;

export interface AppForm {
  slug: string;
  name: string;
  description: string;
  name_en: string;
  description_en: string;
  enabled: boolean;
  markers: string;
  version_command: string;
  version_pattern: string;
  compare: string;
  latest_source: string;
  latest_pattern: string;
  backup_action_id: string;
  update_action_id: string;
}

export function emptyAppForm(): AppForm {
  return {
    slug: '',
    name: '',
    description: '',
    name_en: '',
    description_en: '',
    enabled: true,
    markers: '',
    version_command: '',
    version_pattern: '',
    compare: 'semver',
    latest_source: '',
    latest_pattern: '',
    backup_action_id: '',
    update_action_id: '',
  };
}

export function appFormOf(app: AppEntry): AppForm {
  return {
    slug: app.slug,
    name: app.name,
    description: app.description,
    name_en: app.name_en,
    description_en: app.description_en,
    enabled: app.enabled,
    markers: app.markers,
    version_command: app.version_command,
    version_pattern: app.version_pattern,
    compare: app.compare,
    latest_source: app.latest_source,
    latest_pattern: app.latest_pattern,
    backup_action_id: app.backup_action_id ? String(app.backup_action_id) : '',
    update_action_id: app.update_action_id ? String(app.update_action_id) : '',
  };
}

/** Zeichengrenzen der Textfelder, wie im Backend (`checkAppRequest`, `MaxAppSlugLen`). */
export const APP_LIMITS = {
  slug: 40,
  name: LIMITS.name,
  name_en: LIMITS.name,
  description: LIMITS.description,
  description_en: LIMITS.description,
  markers: LIMITS.list,
  version_command: LIMITS.command,
  version_pattern: LIMITS.command,
  latest_source: LIMITS.path,
  latest_pattern: LIMITS.command,
} as const;

export type AppTextField = keyof typeof APP_LIMITS;

export type AppErrors = Partial<Record<AppTextField, string>>;

/** Prüft die Merkmal-Zeilen: „art wert“, Art aus path|unit|bin|proc, Wert ohne Sonderzeichen. */
export function markerProblem(markers: string): string | undefined {
  const lines = markers
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => line && !line.startsWith('#'));
  if (lines.length === 0) return 'settings.apps.form.markersRequired';
  for (const line of lines) {
    const space = line.indexOf(' ');
    const kind = space < 0 ? line : line.slice(0, space);
    const value = space < 0 ? '' : line.slice(space + 1).trim();
    if (!(MARKER_KINDS as readonly string[]).includes(kind) || !value) return 'settings.apps.form.markersFormat';
    if (UNSAFE_MARKER.test(value)) return 'settings.apps.form.markersUnsafe';
  }
  return undefined;
}

function badPattern(pattern: string): boolean {
  if (!pattern) return false;
  try {
    new RegExp(pattern);
    return false;
  } catch {
    return true;
  }
}

export function sourceProblem(source: string): string | undefined {
  const value = source.trim();
  if (!value) return undefined;
  if (value.startsWith('github:') && /^[A-Za-z0-9._-]+\/[A-Za-z0-9._-]+$/.test(value.slice(7))) return undefined;
  if (value.startsWith('url:https://') && value.length > 12) return undefined;
  return 'settings.apps.form.sourceInvalid';
}

export function validateAppForm(form: AppForm): AppErrors {
  const errors: AppErrors = {};
  for (const field of Object.keys(APP_LIMITS) as AppTextField[]) {
    const multiline = field === 'markers' || field.startsWith('description');
    if (charCount(form[field].trim()) > APP_LIMITS[field]) errors[field] = 'check.tooLong';
    else if (hasControlChars(form[field], multiline)) errors[field] = multiline ? 'check.controlChars' : 'check.oneLine';
  }
  const slug = form.slug.trim().toLowerCase();
  if (!errors.slug && (!/^[a-z][a-z0-9-]*$/.test(slug) || slug.length > APP_LIMITS.slug)) errors.slug = 'settings.apps.form.slugInvalid';
  if (!errors.name && !form.name.trim()) errors.name = 'settings.apps.form.nameRequired';
  const markers = errors.markers ? undefined : markerProblem(form.markers);
  if (markers) errors.markers = markers;
  if (!errors.version_pattern && badPattern(form.version_pattern)) errors.version_pattern = 'settings.apps.form.patternInvalid';
  if (!errors.latest_pattern && badPattern(form.latest_pattern)) errors.latest_pattern = 'settings.apps.form.patternInvalid';
  const source = errors.latest_source ? undefined : sourceProblem(form.latest_source);
  if (source) errors.latest_source = source;
  return errors;
}

const ref = (value: string): number | null => (value ? Number(value) : null);

export function appBody(form: AppForm): AppBody {
  return {
    slug: form.slug.trim().toLowerCase(),
    name: form.name.trim(),
    description: form.description.trim(),
    name_en: form.name_en.trim(),
    description_en: form.description_en.trim(),
    enabled: form.enabled,
    markers: form.markers,
    version_command: form.version_command.trim(),
    version_pattern: form.version_pattern,
    compare: form.compare,
    latest_source: form.latest_source.trim(),
    latest_pattern: form.latest_pattern,
    backup_action_id: ref(form.backup_action_id),
    update_action_id: ref(form.update_action_id),
  };
}

/** Name und Beschreibung in der Sprache der Oberfläche; ohne englische Fassung die deutsche. */
export function localised(app: AppEntry, language: string): { name: string; description: string } {
  const english = language.startsWith('en');
  return {
    name: (english && app.name_en) || app.name,
    description: (english && app.description_en) || app.description,
  };
}
