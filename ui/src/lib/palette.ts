// Befehle der Befehlspalette. Seiten melden ihre wenigen Kernfunktionen mit
// `palette.register(this, [...])` an; beim Seitenwechsel räumt die Shell alles
// ab. `filterItems` sucht ohne Groß-/Kleinschreibung und Akzente, jedes Wort der
// Eingabe muss vorkommen; Treffer am Wortanfang stehen vorn.

import type { IconName } from '../components/icons';

export interface Searchable {
  label: string;
  /** Grauer Zusatz rechts (Host, Tastenkürzel ...). */
  hint?: string;
  /** Weitere Suchbegriffe, die nicht im Namen stehen. */
  keywords?: string[];
}

export interface PaletteItem extends Searchable {
  id: string;
  icon?: IconName;
  /** Taste, die den Befehl auf der Seite auch ohne Palette auslöst (`n` = neu, `u` = Sammel-Update). */
  key?: string;
  run: () => void;
}

function normalize(text: string): string {
  return text.normalize('NFD').replace(/\p{M}/gu, '').toLowerCase();
}

function words(query: string): string[] {
  return normalize(query).split(/\s+/).filter(Boolean);
}

// Kleiner ist besser: 0 Name beginnt so, 1 Wortanfang im Namen, 2 irgendwo im Namen,
// 3 nur in Hinweis oder Suchbegriffen, `null` kein Treffer.
function rank(item: Searchable, word: string): number | null {
  const label = normalize(item.label);
  if (label.startsWith(word)) return 0;
  if (label.split(/[\s\-_/.:]+/).some((part) => part.startsWith(word))) return 1;
  if (label.includes(word)) return 2;
  const rest = normalize([item.hint ?? '', ...(item.keywords ?? [])].join(' '));
  return rest.includes(word) ? 3 : null;
}

function score(item: Searchable, queryWords: string[]): number | null {
  let total = 0;
  for (const word of queryWords) {
    const ranked = rank(item, word);
    if (ranked === null) return null;
    total += ranked;
  }
  return total;
}

/** Die Einträge, die zur Eingabe passen, die besten zuerst; bei gleichem Rang bleibt die Reihenfolge. */
export function filterItems<T extends Searchable>(items: T[], query: string): T[] {
  const queryWords = words(query);
  if (queryWords.length === 0) return items;
  return items
    .map((item, order) => ({ item, order, score: score(item, queryWords) }))
    .filter((entry): entry is { item: T; order: number; score: number } => entry.score !== null)
    .sort((a, b) => a.score - b.score || a.order - b.order)
    .map((entry) => entry.item);
}

export class PaletteRegistry {
  #owners = new Map<object, PaletteItem[]>();
  #listeners = new Set<() => void>();

  /** Meldet die Befehle eines Besitzers an; ein erneuter Aufruf ersetzt seine bisherigen. */
  register(owner: object, items: PaletteItem[]): void {
    this.#owners.set(owner, items);
    this.#notify();
  }

  unregister(owner: object): void {
    if (this.#owners.delete(owner)) this.#notify();
  }

  /** Beim Seitenwechsel: alle Befehle der verlassenen Seite entfernen. */
  clear(): void {
    if (this.#owners.size === 0) return;
    this.#owners.clear();
    this.#notify();
  }

  get items(): PaletteItem[] {
    return [...this.#owners.values()].flat();
  }

  /** Der Seitenbefehl mit dieser Taste (der zuletzt angemeldete gewinnt). */
  byKey(key: string): PaletteItem | undefined {
    return this.items.findLast((item) => item.key === key);
  }

  onChange(listener: () => void): () => void {
    this.#listeners.add(listener);
    return () => this.#listeners.delete(listener);
  }

  #notify(): void {
    this.#listeners.forEach((listener) => listener());
  }
}

export const palette = new PaletteRegistry();
