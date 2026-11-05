// Erscheinungsbild: Theme (hell, dunkel, automatisch) und Dichte (komfortabel,
// kompakt) als Attribute am <html>. Die Farben stehen als `light-dark(hell, dunkel)` in styles/tokens.css:
// `[data-theme='light|dark']` zwingt `color-scheme`, bei `auto` gilt die Systemeinstellung.
// Beides wird im Browser gemerkt, nicht am Konto.

import { readPref, writePref } from './prefs';

export type Theme = 'light' | 'dark' | 'auto';
export type Density = 'comfortable' | 'compact';

const THEME_KEY = 'lcm.theme';
const DENSITY_KEY = 'lcm.density';
const THEMES: readonly Theme[] = ['light', 'dark', 'auto'];
const DENSITIES: readonly Density[] = ['comfortable', 'compact'];

const listeners = new Set<() => void>();
let darkQuery: MediaQueryList | undefined;
// Der Stand liegt im Speicher, damit er auch ohne nutzbaren localStorage gilt.
let theme: Theme = 'auto';
let density: Density = 'comfortable';

function stored<T extends string>(key: string, allowed: readonly T[], fallback: T): T {
  const value = readPref(key);
  return allowed.find((candidate) => candidate === value) ?? fallback;
}

export function getTheme(): Theme {
  return theme;
}

export function getDensity(): Density {
  return density;
}

function apply(): void {
  document.documentElement.dataset.theme = theme;
  document.documentElement.dataset.density = density;
  listeners.forEach((listener) => listener());
}

export function setTheme(value: Theme): void {
  theme = value;
  writePref(THEME_KEY, value);
  apply();
}

export function setDensity(value: Density): void {
  density = value;
  writePref(DENSITY_KEY, value);
  apply();
}

/** Meldet Änderungen von Theme, Dichte und (bei „automatisch“) der Systemeinstellung. */
export function onThemeChange(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function initTheme(): void {
  theme = stored(THEME_KEY, THEMES, 'auto');
  density = stored(DENSITY_KEY, DENSITIES, 'comfortable');
  darkQuery = matchMedia('(prefers-color-scheme: dark)');
  darkQuery.addEventListener('change', () => {
    if (theme === 'auto') listeners.forEach((listener) => listener());
  });
  apply();
}
