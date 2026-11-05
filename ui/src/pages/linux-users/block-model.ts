// Formular, Prüfungen und Ableitungen für Regelbausteine. Hier steht nur Logik ohne Oberfläche.

import { PATH_MODES, type Block, type BlockBody, type BlockVariant } from '../../api/linux-users';
import { LIMITS, charCount, hasControlChars } from '../../lib/validate';
import { slugFromName, validSlug } from './profile-model';

export const MAX_BLOCK_SLUG = 40;

export interface BlockForm {
  name: string;
  slug: string;
  description: string;
  name_en: string;
  description_en: string;
  params: string;
  variants: BlockVariant[];
}

export const emptyVariant = (): BlockVariant => ({ family: 'all', run_as: '', sudo_commands: '', edit_paths: '', path_rules: '' });

export function emptyBlockForm(): BlockForm {
  return { name: '', slug: '', description: '', name_en: '', description_en: '', params: '', variants: [emptyVariant()] };
}

export function blockFormOf(block: Block): BlockForm {
  return {
    name: block.name,
    slug: block.slug,
    description: block.description,
    name_en: block.name_en,
    description_en: block.description_en,
    params: block.params,
    variants: block.variants.map((v) => ({ ...v })),
  };
}

export const sameBlockForm = (a: BlockForm, b: BlockForm): boolean => JSON.stringify(a) === JSON.stringify(b);

const variantFilled = (v: BlockVariant): boolean => [v.sudo_commands, v.edit_paths, v.path_rules].some((text) => text.trim() !== '');

/** Nach Abzug der leeren Varianten übrig: der Baustein braucht mindestens eine. */
export function filledVariants(form: BlockForm): BlockVariant[] {
  return form.variants.filter(variantFilled);
}

export interface BlockErrors {
  name?: string;
  nameEn?: string;
  slug?: string;
  description?: string;
  descriptionEn?: string;
  params?: string;
  variants?: string;
  /** Fehler je Variante (Index im Formular): zu lang oder ungültige Zeile bei den Verzeichnisrechten. */
  rows: Record<number, string>;
}

const PARAM_NAME = /^[a-z0-9_]+$/;

/** Die Platzhalter des Bausteins; leere Teile entfallen. */
export function paramNames(params: string): string[] {
  return params
    .split(',')
    .map((p) => p.trim())
    .filter(Boolean);
}

/** Jede nicht leere Zeile muss „modus pfad“ mit bekanntem Modus sein. */
export function pathRuleLinesValid(text: string): boolean {
  return text
    .split('\n')
    .map((l) => l.trim())
    .filter((l) => l && !l.startsWith('#'))
    .every((line) => {
      const fields = line.split(/\s+/);
      return fields.length === 2 && (PATH_MODES as readonly string[]).includes(fields[0]!);
    });
}

export function validateBlockForm(form: BlockForm, creating: boolean): BlockErrors {
  const errors: BlockErrors = { rows: {} };
  if (!form.name.trim()) errors.name = 'linuxUsers.blocks.error.nameRequired';
  else if (charCount(form.name.trim()) > LIMITS.name) errors.name = 'check.tooLong';
  else if (hasControlChars(form.name)) errors.name = 'check.oneLine';
  else if (creating && !validSlug(form.slug.trim() || slugFromName(form.name, MAX_BLOCK_SLUG), MAX_BLOCK_SLUG)) errors.slug = 'linuxUsers.blocks.error.slugInvalid';
  if (charCount(form.name_en.trim()) > LIMITS.name) errors.nameEn = 'check.tooLong';
  else if (hasControlChars(form.name_en)) errors.nameEn = 'check.oneLine';
  if (charCount(form.description.trim()) > LIMITS.description) errors.description = 'check.tooLong';
  else if (hasControlChars(form.description, true)) errors.description = 'check.controlChars';
  if (charCount(form.description_en.trim()) > LIMITS.description) errors.descriptionEn = 'check.tooLong';
  else if (hasControlChars(form.description_en, true)) errors.descriptionEn = 'check.controlChars';
  if (charCount(form.params.trim()) > LIMITS.command) errors.params = 'check.tooLong';
  else if (!paramNames(form.params).every((name) => PARAM_NAME.test(name))) errors.params = 'linuxUsers.blocks.error.paramName';
  if (filledVariants(form).length === 0) errors.variants = 'linuxUsers.blocks.error.noRules';
  form.variants.forEach((variant, i) => {
    const texts = [variant.sudo_commands, variant.edit_paths, variant.path_rules];
    if (texts.some((text) => charCount(text) > LIMITS.script) || charCount(variant.run_as.trim()) > LIMITS.name) errors.rows[i] = 'check.tooLong';
    else if (texts.some((text) => hasControlChars(text, true)) || hasControlChars(variant.run_as)) errors.rows[i] = 'check.controlChars';
    else if (!pathRuleLinesValid(variant.path_rules)) errors.rows[i] = 'linuxUsers.blocks.error.pathLine';
  });
  return errors;
}

export function isBlockValid(errors: BlockErrors): boolean {
  return !errors.name && !errors.nameEn && !errors.slug && !errors.description && !errors.descriptionEn && !errors.params && !errors.variants && Object.keys(errors.rows).length === 0;
}

export function blockBody(form: BlockForm): BlockBody {
  return {
    name: form.name.trim(),
    description: form.description.trim(),
    name_en: form.name_en.trim(),
    description_en: form.description_en.trim(),
    params: paramNames(form.params).join(','),
    variants: filledVariants(form),
  };
}

export function createBlockBody(form: BlockForm): BlockBody & { slug: string } {
  return { ...blockBody(form), slug: form.slug.trim() || slugFromName(form.name, MAX_BLOCK_SLUG) };
}

/** Beispielwerte für die Vorschau: jeder Platzhalter bekommt „beispiel“. */
export function previewValues(params: string): string {
  return paramNames(params)
    .map((name) => `${name}=beispiel`)
    .join('\n');
}

export type SourceFilter = 'all' | 'builtin' | 'own';

/** Suche über Namen (beide Sprachen), Kurzname und Beschreibung, dazu die Herkunft. */
export function filterBlocks(blocks: Block[], query: string, source: SourceFilter): Block[] {
  const needle = query.trim().toLowerCase();
  return blocks.filter((b) => {
    if (source === 'builtin' && !b.builtin) return false;
    if (source === 'own' && b.builtin) return false;
    return !needle || [b.name, b.name_en, b.slug, b.description, b.description_en].join(' ').toLowerCase().includes(needle);
  });
}

export function parseSource(raw: string | null): SourceFilter {
  return raw === 'builtin' || raw === 'own' ? raw : 'all';
}
