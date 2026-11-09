// Formular, Prüfungen und Ableitungen für Berechtigungsprofile. Hier steht nur Logik ohne Oberfläche.

import type { AccountType, Block, BlockUse, PathRule, Profile, ProfileBody, SudoRule } from '../../api/linux-users';
import { LIMITS, charCount, hasControlChars } from '../../lib/validate';

export const MAX_PROFILE_SLUG = 20;

export interface ProfileForm {
  name: string;
  slug: string;
  description: string;
  account_type: AccountType;
  sudo_rules: SudoRule[];
  edit_rules: string[];
  path_rules: PathRule[];
  block_uses: BlockUse[];
}

export const emptySudoRule = (): SudoRule => ({ command: '', run_as: 'root', require_password: false, allow_root_equivalent: false });
export const emptyPathRule = (): PathRule => ({ path: '', mode: 'read' });

export function emptyProfileForm(): ProfileForm {
  return { name: '', slug: '', description: '', account_type: 'shell', sudo_rules: [], edit_rules: [], path_rules: [], block_uses: [] };
}

export function profileFormOf(profile: Profile): ProfileForm {
  return {
    name: profile.name,
    slug: profile.slug,
    description: profile.description,
    account_type: profile.account_type,
    sudo_rules: profile.sudo_rules.map((r) => ({ ...r })),
    edit_rules: [...profile.edit_rules],
    path_rules: profile.path_rules.map((r) => ({ ...r })),
    block_uses: profile.block_uses.map((u) => ({ ...u })),
  };
}

export const sameProfileForm = (a: ProfileForm, b: ProfileForm): boolean => JSON.stringify(a) === JSON.stringify(b);

/** Vorschlag für den Kurznamen: Kleinbuchstaben, Ziffern, Bindestriche, mit Buchstaben am Anfang, höchstens 20 Zeichen. */
export function slugFromName(name: string, max = MAX_PROFILE_SLUG): string {
  return name
    .toLowerCase()
    .replace(/[äöüß]/g, (c) => ({ ä: 'ae', ö: 'oe', ü: 'ue', ß: 'ss' })[c] ?? c)
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^[^a-z]+/, '')
    .slice(0, max)
    .replace(/-+$/, '');
}

/**
 * Freier Kurzname für eine Kopie. Der Kurzname ist auf `max` Zeichen gekürzt und eindeutig: „Name (Kopie)“ und
 * „Name (Kopie) 2“ ergeben gekürzt denselben, darum bekommt der zweite einen Zähler (`-2`) und wird dafür weiter gekürzt.
 */
export function freeSlug(name: string, taken: string[], max = MAX_PROFILE_SLUG): string {
  const used = new Set(taken);
  const slug = slugFromName(name, max);
  if (!used.has(slug)) return slug;
  for (let n = 2; ; n++) {
    const counter = `-${n}`;
    const candidate = slugFromName(name, max - counter.length) + counter;
    if (!used.has(candidate)) return candidate;
  }
}

const SLUG = /^[a-z][a-z0-9-]*$/;

export function validSlug(slug: string, max = MAX_PROFILE_SLUG): boolean {
  return slug.length > 0 && slug.length <= max && SLUG.test(slug);
}

/** Fehlerschlüssel je Zeile (Index in der Liste, wie sie im Formular steht). */
export interface ProfileErrors {
  name?: string;
  slug?: string;
  description?: string;
  sudo: Record<number, string>;
  edit: Record<number, string>;
  path: Record<number, string>;
  blocks: Record<number, string>;
}

const blank = (value: string) => value.trim() === '';

/** Fehler einer einzeiligen Eingabe: zu lang oder mit Steuerzeichen; `undefined`, wenn sie passt. */
function lineError(value: string, max: number): string | undefined {
  if (charCount(value.trim()) > max) return 'check.tooLong';
  return hasControlChars(value) ? 'check.oneLine' : undefined;
}
const absolute = (value: string) => value.trim().startsWith('/');

/** Sinnvolle Parameterwerte: nicht leer, ohne Leerzeichen (sie landen in einer sudoers-Zeile). */
export function validParamValue(value: string): boolean {
  return value !== '' && !/\s/.test(value);
}

export function validateProfileForm(form: ProfileForm, creating: boolean, blocks: Block[]): ProfileErrors {
  const errors: ProfileErrors = { sudo: {}, edit: {}, path: {}, blocks: {} };
  if (blank(form.name)) errors.name = 'linuxUsers.profiles.error.nameRequired';
  else {
    const name = lineError(form.name, LIMITS.name);
    if (name) errors.name = name;
  }
  if (charCount(form.description.trim()) > LIMITS.description) errors.description = 'check.tooLong';
  else if (hasControlChars(form.description, true)) errors.description = 'check.controlChars';
  if (creating) {
    const slug = form.slug.trim() || slugFromName(form.name);
    if (blank(form.name)) {
      // der Name fehlt schon; ein Kurzname dazu wäre doppelte Kritik
    } else if (!validSlug(slug)) errors.slug = 'linuxUsers.profiles.error.slugInvalid';
  }
  form.sudo_rules.forEach((rule, i) => {
    const long = lineError(rule.command, LIMITS.command) ?? lineError(rule.run_as, LIMITS.name);
    if (long) errors.sudo[i] = long;
    else if (!blank(rule.command) && !absolute(rule.command)) errors.sudo[i] = 'linuxUsers.profiles.error.commandRelative';
  });
  form.edit_rules.forEach((path, i) => {
    const long = lineError(path, LIMITS.path);
    if (long) errors.edit[i] = long;
    else if (!blank(path) && !absolute(path)) errors.edit[i] = 'linuxUsers.profiles.error.pathRelative';
  });
  form.path_rules.forEach((rule, i) => {
    const long = lineError(rule.path, LIMITS.path);
    if (long) errors.path[i] = long;
    else if (!blank(rule.path) && !absolute(rule.path)) errors.path[i] = 'linuxUsers.profiles.error.pathRelative';
  });
  form.block_uses.forEach((use, i) => {
    const block = blocks.find((b) => b.id === use.block_id);
    if (charCount(use.values) > LIMITS.description) errors.blocks[i] = 'check.tooLong';
    else if (block && blockParams(block).some((name) => !validParamValue(paramValue(use.values, name)))) errors.blocks[i] = 'linuxUsers.profiles.error.paramValue';
  });
  return errors;
}

export function isProfileValid(errors: ProfileErrors): boolean {
  return !errors.name && !errors.slug && !errors.description && [errors.sudo, errors.edit, errors.path, errors.blocks].every((rows) => Object.keys(rows).length === 0);
}

/** Leere Zeilen fallen weg: Eine angefangene und wieder verworfene Regel soll das Speichern nicht blockieren. */
export function profileBody(form: ProfileForm): ProfileBody {
  return {
    name: form.name.trim(),
    description: form.description.trim(),
    account_type: form.account_type,
    sudo_rules: form.sudo_rules.filter((r) => !blank(r.command)).map((r) => ({ ...r, command: r.command.trim() })),
    edit_rules: form.edit_rules.filter((p) => !blank(p)).map((path) => ({ path: path.trim() })),
    path_rules: form.path_rules.filter((r) => !blank(r.path)).map((r) => ({ ...r, path: r.path.trim() })),
    block_uses: form.block_uses,
  };
}

export function createProfileBody(form: ProfileForm): ProfileBody & { slug: string } {
  return { ...profileBody(form), slug: form.slug.trim() || slugFromName(form.name) };
}

/** Anzahl der eigenen Regelzeilen eines Profils (Bausteine zählen extra). */
export function ruleCount(profile: Profile): number {
  return profile.sudo_rules.length + profile.edit_rules.length + profile.path_rules.length;
}

/**
 * Was die Spalte „Regeln“ zeigt. Die mitgelieferten Profile tragen ihre Rechte nicht als Regeln: „Voll-Administrator“
 * gibt sudo über `grants_full_root` frei, „Standardbenutzer“ hat bewusst keine. Eine nackte 0 wäre dort irreführend.
 */
export function ruleCell(profile: Profile): number | 'all' | 'none' {
  if (profile.grants_full_root) return 'all';
  const count = ruleCount(profile);
  return count === 0 ? 'none' : count;
}

// ---- Bausteine und ihre Parameter -------------------------------------------

/** Die Platzhalter eines Bausteins (`service,path` → `['service', 'path']`). */
export function blockParams(block: Block): string[] {
  return block.params
    .split(',')
    .map((p) => p.trim())
    .filter(Boolean);
}

/** Wert eines Parameters aus den Zeilen `name=wert`. */
export function paramValue(values: string, name: string): string {
  const line = values.split('\n').find((l) => l.trim().startsWith(`${name}=`));
  return line ? line.slice(line.indexOf('=') + 1).trim() : '';
}

/** Setzt einen Parameter und lässt die anderen Zeilen unberührt. */
export function setParamValue(values: string, name: string, value: string): string {
  const kept = values.split('\n').filter((l) => l.trim() && !l.trim().startsWith(`${name}=`));
  return [...kept, `${name}=${value}`].join('\n');
}

/** Bausteine, die noch nicht im Profil hängen (jeder höchstens einmal). */
export function availableBlocks(blocks: Block[], uses: BlockUse[]): Block[] {
  const taken = new Set(uses.map((u) => u.block_id));
  return blocks.filter((b) => !taken.has(b.id));
}

/** Suche über Name, Kurzname und Beschreibung. */
export function filterProfiles(profiles: Profile[], query: string): Profile[] {
  const needle = query.trim().toLowerCase();
  if (!needle) return profiles;
  return profiles.filter((p) => [p.name, p.slug, p.description].join(' ').toLowerCase().includes(needle));
}

/** Freier Name für eine Kopie: „Name (Kopie)“, bei Bedarf „… (Kopie) 2“. */
export function freeCopyName(base: string, suffix: string, taken: string[]): string {
  const used = new Set(taken.map((name) => name.toLowerCase()));
  let name = `${base} ${suffix}`;
  for (let n = 2; used.has(name.toLowerCase()); n++) name = `${base} ${suffix} ${n}`;
  return name;
}
