// Prüfungen für die Sprachdateien und die Texte im Code (Aufruf: check-locales.mjs).
// Reine Funktionen ohne Dateizugriff, damit sie testbar bleiben.

export const SUFFIXES = ['label', 'help', 'on', 'off', 'confirm', 'explainer'];
const PLURAL_FORMS = ['zero', 'one', 'two', 'few', 'many', 'other'];
const EXPRESSION = '\u0000';

/** Verschachtelten JSON-Baum in `a.b.c` -> Text umwandeln. */
export function flatten(tree, prefix = '', out = new Map()) {
  if (typeof tree === 'string') {
    out.set(prefix, tree);
  } else if (tree && typeof tree === 'object') {
    for (const [name, child] of Object.entries(tree)) flatten(child, prefix ? `${prefix}.${name}` : name, out);
  }
  return out;
}

export function placeholders(text) {
  return new Set([...text.matchAll(/\{(\w+)\}/g)].map((match) => match[1]));
}

/** Gruppen von Pluralformen: `a.b.one`, `a.b.other` ergeben die Gruppe `a.b`. */
function pluralGroups(keys) {
  return new Set([...keys].filter((key) => key.endsWith('.other')).map((key) => key.slice(0, -'.other'.length)));
}

/** Jeder Schlüssel auf seinen Namen ohne Pluralform abgebildet, mit den Texten dahinter. */
function groupByBase(messages) {
  const groups = pluralGroups(messages.keys());
  const result = new Map();
  for (const [key, text] of messages) {
    const dot = key.lastIndexOf('.');
    const base = groups.has(key.slice(0, dot)) && PLURAL_FORMS.includes(key.slice(dot + 1)) ? key.slice(0, dot) : key;
    result.set(base, [...(result.get(base) ?? []), text]);
  }
  return result;
}

function union(texts) {
  return new Set(texts.flatMap((text) => [...placeholders(text)]));
}

function sameSet(a, b) {
  return a.size === b.size && [...a].every((item) => b.has(item));
}

/**
 * Vergleicht die Sprachdateien. Deutsch und Englisch müssen vollständig und
 * gleich sein (Fehler). Bei weiteren Sprachen werden fehlende Schlüssel nur
 * berichtet; abweichende Platzhalter sind überall ein Fehler.
 */
export function compareLanguages(catalogs) {
  const errors = [];
  const reports = [];
  const { de, en } = catalogs;
  if (!de || !en) return { errors: ['de.json and en.json must both exist'], reports };

  for (const key of de.keys()) if (!en.has(key)) errors.push(`key "${key}" is missing in en`);
  for (const key of en.keys()) if (!de.has(key)) errors.push(`key "${key}" is missing in de`);

  const reference = groupByBase(en);
  for (const [language, messages] of Object.entries(catalogs)) {
    const own = groupByBase(messages);
    for (const [base, texts] of own) {
      const expected = reference.get(base);
      if (!expected) {
        if (language !== 'de') reports.push(`${language}: unknown key "${base}"`);
        continue;
      }
      if (!sameSet(union(texts), union(expected))) {
        errors.push(`placeholders of "${base}" differ in ${language}: {${[...union(texts)]}} instead of {${[...union(expected)]}} (as in en)`);
      }
    }
    if (language === 'de' || language === 'en') continue;
    for (const base of reference.keys()) if (!own.has(base)) reports.push(`${language}: missing key "${base}"`);
  }
  return { errors, reports };
}

/** Alle Zeichenketten im Code, die wie ein Schlüssel `bereich.name` aussehen. */
export function collectLiterals(source) {
  return new Set([...source.matchAll(/(['"`])([A-Za-z][\w-]*(?:\.[\w-]+)+)\1/g)].map((match) => match[2]));
}

/** Schlüssel aus `t('a.b.c'` und `tryT('a.b.c'`. */
export function collectTranslationCalls(source) {
  return [...source.matchAll(/\b(?:t|tryT)\(\s*(['"`])([^'"`$]+)\1/g)].map((match) => match[2]);
}

function matchesWhitelist(key, patterns) {
  return patterns.some((pattern) => (pattern.endsWith('*') ? key.startsWith(pattern.slice(0, -1)) : key === pattern));
}

/** Schlüssel (Deutsch/Englisch), die im Code nirgends vorkommen. */
export function findOrphans(messages, literals, whitelist = []) {
  const groups = pluralGroups(messages.keys());
  const orphans = [];
  for (const key of messages.keys()) {
    if (matchesWhitelist(key, whitelist)) continue;
    const dot = key.lastIndexOf('.');
    const base = groups.has(key.slice(0, dot)) && PLURAL_FORMS.includes(key.slice(dot + 1)) ? key.slice(0, dot) : key;
    const parent = base.slice(0, base.lastIndexOf('.'));
    const suffix = base.slice(base.lastIndexOf('.') + 1);
    const used = literals.has(key) || literals.has(base) || (SUFFIXES.includes(suffix) && literals.has(parent));
    if (!used) orphans.push(key);
  }
  return orphans;
}

/** Aufgerufene Schlüssel, die es in der Sprachdatei nicht gibt. */
export function findMissing(messages, calls, whitelist = []) {
  const groups = pluralGroups(messages.keys());
  return calls.filter((key) => !matchesWhitelist(key, whitelist) && !messages.has(key) && !groups.has(key));
}

// ---- fest eingetragener Text in html``-Vorlagen ----

function skipString(source, start) {
  const quote = source[start];
  let i = start + 1;
  while (i < source.length && source[i] !== quote) i += source[i] === '\\' ? 2 : 1;
  return i + 1;
}

function readTemplate(source, start) {
  let text = '';
  let i = start;
  const nested = [];
  while (i < source.length) {
    const char = source[i];
    if (char === '\\') {
      i += 2;
    } else if (char === '`') {
      return { text, end: i + 1, nested };
    } else if (char === '$' && source[i + 1] === '{') {
      const end = readExpression(source, i + 2);
      nested.push(...scanTemplates(source.slice(i + 2, end - 1), i + 2));
      text += EXPRESSION;
      i = end;
    } else {
      text += char;
      i += 1;
    }
  }
  return { text, end: i, nested };
}

function readExpression(source, start) {
  let depth = 1;
  let i = start;
  while (i < source.length) {
    const char = source[i];
    if (char === "'" || char === '"') {
      i = skipString(source, i);
      continue;
    }
    if (char === '`') {
      i = readTemplate(source, i + 1).end;
      continue;
    }
    if (char === '{') depth += 1;
    if (char === '}' && --depth === 0) return i + 1;
    i += 1;
  }
  return i;
}

/** Inhalte aller html`…`- und svg`…`-Vorlagen, mit \u0000 statt ${…}; offset = Lage im Quelltext. */
export function scanTemplates(source, offset = 0) {
  const found = [];
  const start = /\b(?:html|svg)\s*`/g;
  let match;
  while ((match = start.exec(source))) {
    const bodyStart = match.index + match[0].length;
    const template = readTemplate(source, bodyStart);
    found.push({ text: template.text, offset: offset + match.index }, ...template.nested);
    start.lastIndex = template.end;
  }
  return found;
}

const TEXT_ATTRIBUTES = ['aria-label', 'aria-description', 'title', 'placeholder', 'alt', 'label'];

/** Gibt es Buchstaben außerhalb von ${…}, Zeichenverweisen und erlaubten Wörtern (Produktnamen)? */
function hasLetters(text, allowedWords) {
  let rest = text.replaceAll(EXPRESSION, '').replace(/&[#\w]+;/g, '');
  for (const word of allowedWords) rest = rest.replaceAll(word, '');
  return /\p{L}/u.test(rest);
}

/** Sichtbarer Text und Attribute (title, aria-label …) in einer Vorlage ohne ${…}. */
export function hardcodedTexts(template, allowedWords = []) {
  const found = [];
  const withoutBlocks = template.replace(/<!--[\s\S]*?-->/g, '').replace(/<(style|script)[\s\S]*?<\/\1>/g, '');
  const attribute = new RegExp(`\\s(${TEXT_ATTRIBUTES.join('|')})\\s*=\\s*"([^"]*)"`, 'g');
  for (const tag of withoutBlocks.matchAll(/<\/?[a-zA-Z][^>]*>/g)) {
    for (const [, name, value] of tag[0].matchAll(attribute)) {
      if (hasLetters(value, allowedWords)) found.push(`${name}="${value}"`);
    }
  }
  for (const piece of withoutBlocks.split(/<\/?[a-zA-Z][^>]*>/)) {
    if (hasLetters(piece, allowedWords)) found.push(piece.replaceAll(EXPRESSION, '…').trim());
  }
  return found;
}

/** Fest eingetragene Texte einer Quelldatei als { line, text }. */
export function lintHardcodedText(source, allowedWords = []) {
  return scanTemplates(source).flatMap(({ text, offset }) => {
    const line = source.slice(0, offset).split('\n').length;
    return hardcodedTexts(text, allowedWords).map((found) => ({ line, text: found }));
  });
}
