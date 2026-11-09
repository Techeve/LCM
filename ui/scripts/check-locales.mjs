// Prüft die Sprachdateien im CI (npm run check:locales):
//  - Deutsch und Englisch vollständig und mit gleichen Schlüsseln,
//  - gleiche Platzhalter in allen Sprachen,
//  - keine verwaisten Schlüssel, keine aufgerufenen Schlüssel ohne Text,
//  - kein fest eingetragener Text in Komponenten und Seiten.
// Fehlende Schlüssel weiterer Sprachen werden berichtet, brechen aber nicht ab.

import { readdirSync, readFileSync } from 'node:fs';
import { basename, join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';
import {
  collectLiterals,
  collectTranslationCalls,
  compareLanguages,
  findMissing,
  findOrphans,
  flatten,
  lintHardcodedText,
} from './locale-lib.mjs';

const root = join(fileURLToPath(import.meta.url), '../..');
const src = join(root, 'src');

function sourceFiles(directory) {
  return readdirSync(directory, { recursive: true, withFileTypes: true })
    .filter((entry) => entry.isFile() && entry.name.endsWith('.ts') && !entry.name.endsWith('.test.ts'))
    .map((entry) => join(entry.parentPath, entry.name));
}

const catalogs = {};
for (const file of readdirSync(join(src, 'locales')).filter((name) => name.endsWith('.json'))) {
  catalogs[basename(file, '.json')] = flatten(JSON.parse(readFileSync(join(src, 'locales', file), 'utf8')));
}

const config = JSON.parse(readFileSync(join(root, 'scripts/locale-dynamic-keys.json'), 'utf8'));
const whitelist = config.patterns;
const files = sourceFiles(src).map((file) => ({ file, source: readFileSync(file, 'utf8') }));

const { errors, reports } = compareLanguages(catalogs);
const english = catalogs.en ?? new Map();

const literals = new Set(files.flatMap(({ source }) => [...collectLiterals(source)]));
for (const key of findOrphans(english, literals, whitelist)) errors.push(`key "${key}" is not used in the code`);

for (const { file, source } of files) {
  for (const key of findMissing(english, collectTranslationCalls(source), whitelist)) {
    errors.push(`${relative(root, file)}: key "${key}" has no text`);
  }
}

const linted = files.filter(({ file }) => /\/src\/(components|pages|shell)\//.test(file));
for (const { file, source } of linted) {
  for (const { line, text } of lintHardcodedText(source, config.allowedWords)) {
    errors.push(`${relative(root, file)}:${line}: hardcoded text "${text}" (use t())`);
  }
}

for (const line of reports) console.log(`note: ${line}`);
for (const line of errors) console.error(`error: ${line}`);
console.log(
  `check:locales: ${Object.keys(catalogs).length} languages, ${english.size} keys, ${errors.length} errors, ${reports.length} notes`,
);
process.exit(errors.length > 0 ? 1 : 0);
