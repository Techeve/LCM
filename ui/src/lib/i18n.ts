// Mehrsprachigkeit: eine JSON-Datei je Sprache in src/locales/, Schlüssel nach
// Bereichen verschachtelt. Geladen wird nur die aktive Sprache (je Sprache ein
// eigener Teil des Builds). Eine weitere Sprache ist eine weitere Datei.
//
// Rückfall: fehlt ein Schlüssel, gilt Englisch, dann Deutsch. Diese Sprachen
// werden erst beim ersten fehlenden Schlüssel nachgeladen. Deutsch und
// Englisch sind laut CI immer vollständig, wer sie nutzt, lädt also nie eine
// zweite Datei; nur unvollständige Sprachen zahlen für den Rückfall.

import { ApiError } from './api';
import { readPref, writePref } from './prefs';

export type Params = Record<string, string | number>;
type Messages = Map<string, string>;
type Loader = () => Promise<unknown>;

export const LANGUAGE_KEY = 'lcm.lang';
const FALLBACKS = ['en', 'de'];
const RIGHT_TO_LEFT = new Set(['ar', 'he', 'fa', 'ur', 'ps', 'sd', 'yi', 'dv', 'ckb']);

/** Verschachtelten JSON-Baum in `a.b.c` -> Text umwandeln. */
export function flatten(tree: unknown, prefix = '', out: Messages = new Map()): Messages {
  if (typeof tree === 'string') {
    out.set(prefix, tree);
  } else if (tree && typeof tree === 'object') {
    for (const [name, child] of Object.entries(tree)) flatten(child, prefix ? `${prefix}.${name}` : name, out);
  }
  return out;
}

/** Ersetzt `{name}` durch die Werte; unbekannte Platzhalter bleiben stehen. */
export function interpolate(text: string, params?: Params): string {
  if (!params) return text;
  return text.replace(/\{(\w+)\}/g, (placeholder, name: string) =>
    Object.hasOwn(params, name) ? String(params[name]) : placeholder,
  );
}

/** Erste Wunschsprache, die es gibt (`de-AT` passt auf `de`); sonst Englisch. */
export function pickLanguage(wishes: Array<string | null | undefined>, available: string[]): string {
  for (const wish of wishes) {
    if (!wish) continue;
    const exact = available.find((language) => language.toLowerCase() === wish.toLowerCase());
    if (exact) return exact;
    const base = wish.split('-')[0].toLowerCase();
    const related = available.find((language) => language.toLowerCase() === base);
    if (related) return related;
  }
  return 'en';
}

function languageOf(path: string): string | null {
  return /([^/]+)\.json$/.exec(path)?.[1] ?? null;
}

export class I18n {
  #loaders = new Map<string, Loader>();
  #catalogs = new Map<string, Messages>();
  #loading = new Map<string, Promise<void>>();
  #rules = new Map<string, Intl.PluralRules>();
  #listeners = new Set<() => void>();
  #language = 'en';
  #sequence = 0;
  #fallbackPending = false;

  constructor(loaders: Record<string, Loader>) {
    for (const [path, loader] of Object.entries(loaders)) {
      const language = languageOf(path);
      if (language) this.#loaders.set(language, loader);
    }
  }

  get languages(): string[] {
    return [...this.#loaders.keys()].sort();
  }

  get language(): string {
    return this.#language;
  }

  /** Startsprache: gemerkte Wahl, sonst Browsersprache, sonst Englisch. */
  async init(browserLanguages: readonly string[] = navigator.languages): Promise<void> {
    const language = pickLanguage([readPref(LANGUAGE_KEY), ...browserLanguages], this.languages);
    await this.#activate(language);
  }

  /** Wechselt die Sprache und merkt sie im Browser. Das Konto speichert `auth`. */
  async setLanguage(language: string): Promise<void> {
    if (!this.#loaders.has(language)) throw new RangeError(`unknown language: ${language}`);
    if (await this.#activate(language)) writePref(LANGUAGE_KEY, language);
  }

  t(key: string, params?: Params): string {
    return this.tryT(key, params) ?? key;
  }

  /** Wie `t`, aber `undefined` statt des Schlüssels, wenn es keinen Text gibt. */
  tryT(key: string, params?: Params): string | undefined {
    for (const language of this.#chain()) {
      const text = this.#lookup(language, key, params?.count);
      if (text !== undefined) return interpolate(text, params);
    }
    if (this.#catalogs.has(this.#language)) this.#loadFallbacks(); // vor dem Start ist nichts „fehlend“
    return undefined;
  }

  onChange(listener: () => void): () => void {
    this.#listeners.add(listener);
    return () => this.#listeners.delete(listener);
  }

  #chain(): string[] {
    return [...new Set([this.#language, ...FALLBACKS])];
  }

  #lookup(language: string, key: string, count: unknown): string | undefined {
    const messages = this.#catalogs.get(language);
    if (!messages) return undefined;
    if (typeof count !== 'number') return messages.get(key);
    const form = this.#pluralRules(language).select(count);
    return messages.get(`${key}.${form}`) ?? messages.get(`${key}.other`) ?? messages.get(key);
  }

  #pluralRules(language: string): Intl.PluralRules {
    let rules = this.#rules.get(language);
    if (!rules) {
      rules = new Intl.PluralRules(language);
      this.#rules.set(language, rules);
    }
    return rules;
  }

  async #activate(language: string): Promise<boolean> {
    const sequence = ++this.#sequence;
    await this.#load(language);
    if (sequence !== this.#sequence) return false; // inzwischen wurde eine andere Sprache gewählt
    this.#language = language;
    document.documentElement.lang = language;
    document.documentElement.dir = RIGHT_TO_LEFT.has(language.split('-')[0]) ? 'rtl' : 'ltr';
    this.#notify();
    return true;
  }

  #load(language: string): Promise<void> {
    const running = this.#loading.get(language);
    if (running) return running;
    const loader = this.#loaders.get(language);
    if (!loader || this.#catalogs.has(language)) return Promise.resolve();
    const promise = loader()
      .then((tree) => void this.#catalogs.set(language, flatten(tree)))
      .finally(() => this.#loading.delete(language));
    this.#loading.set(language, promise);
    return promise;
  }

  #loadFallbacks(): void {
    if (this.#fallbackPending) return;
    const missing = this.#chain().filter((language) => this.#loaders.has(language) && !this.#catalogs.has(language));
    if (missing.length === 0) return;
    this.#fallbackPending = true;
    Promise.all(missing.map((language) => this.#load(language)))
      .then(() => this.#notify())
      .catch((error) => console.error('i18n: fallback language failed to load', error))
      .finally(() => (this.#fallbackPending = false));
  }

  #notify(): void {
    this.#listeners.forEach((listener) => listener());
  }
}

export const i18n = new I18n(import.meta.glob('../locales/*.json', { import: 'default' }));

export const t = (key: string, params?: Params): string => i18n.t(key, params);

/** Name einer Sprache in ihr selbst („Deutsch“), für die Sprachauswahl. */
export function languageName(language: string): string {
  return new Intl.DisplayNames([language], { type: 'language' }).of(language) ?? language;
}

/** `password.too_weak` nennt die verletzten Regeln als Codes (`problems`); die Meldung schreibt sie aus. */
function weakPasswordParams(params: Record<string, unknown>): Params {
  const codes = Array.isArray(params.problems) ? params.problems : [];
  const reasons = codes.map((code) => i18n.tryT(`password.problem.${String(code)}`)).filter(Boolean);
  return { reasons: reasons.join(' ') };
}

/** Text zu einem Fehler: `errors.<code>` mit den Parametern der API, sonst allgemein. */
export function errorText(error: unknown): string {
  if (!(error instanceof ApiError)) return error instanceof Error ? error.message : String(error);
  const params = error.code === 'password.too_weak' ? weakPasswordParams(error.params) : (error.params as Params);
  const specific = error.code ? i18n.tryT(`errors.${error.code}`, params) : undefined;
  return (specific ?? t('errors.http.generic', { status: error.status })).trim();
}
