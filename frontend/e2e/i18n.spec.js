// Prüfungen der Sprachkataloge - ohne Browser, reine Datenprüfung.
//
// Beide Anlässe sind echt: Beim Bau des Anwendungskatalogs fehlte ein
// Schlüssel („Keine Aktion"), und in der Oberfläche stand daraufhin der rohe
// Schlüsselname im Auswahlfeld. Aufgefallen ist das niemandem, weil solche
// Lücken nur in dem Zweig sichtbar werden, den man gerade nicht ansieht.
import { test, expect } from '@playwright/test';
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import de from '../src/locales/de.js';
import en from '../src/locales/en.js';

const src = join(dirname(fileURLToPath(import.meta.url)), '..', 'src');

// Plural-Objekte ({one, other}) sind Blätter, keine Zweige.
const flatten = (obj, prefix = '') =>
  Object.entries(obj).flatMap(([k, v]) =>
    v && typeof v === 'object' && !('one' in v && 'other' in v)
      ? flatten(v, `${prefix}${k}.`)
      : [`${prefix}${k}`]
  );

function sourceFiles(dir) {
  return readdirSync(dir).flatMap((entry) => {
    const path = join(dir, entry);
    if (statSync(path).isDirectory()) return sourceFiles(path);
    return /\.(svelte|js)$/.test(path) && !path.includes(`${'locales'}`) ? [path] : [];
  });
}

test('Sprachkataloge: Deutsch und Englisch tragen dieselben Schlüssel', () => {
  const german = new Set(flatten(de));
  const english = new Set(flatten(en));
  expect([...german].filter((k) => !english.has(k))).toEqual([]);
  expect([...english].filter((k) => !german.has(k))).toEqual([]);
});

test('Sprachkataloge: jeder benutzte Schlüssel existiert auch', () => {
  const german = new Set(flatten(de));
  const english = new Set(flatten(en));
  const missing = [];

  for (const file of sourceFiles(src)) {
    // Kommentarzeilen raus: Dort steht das Beispiel aus der Doku, kein Aufruf.
    const text = readFileSync(file, 'utf8')
      .split('\n')
      .filter((line) => !line.trimStart().startsWith('//'))
      .join('\n');
    for (const match of text.matchAll(/\bt\(\s*'([a-zA-Z0-9_.]+)'/g)) {
      const key = match[1];
      // Zusammengesetzte Schlüssel (t('cron.day.' + n)) enden auf einen Punkt
      // und lassen sich hier nicht prüfen.
      if (key.endsWith('.')) continue;
      if (!german.has(key) || !english.has(key)) {
        missing.push(`${key} (${file.replace(/.*\/src\//, 'src/')})`);
      }
    }
  }
  expect(missing).toEqual([]);
});
