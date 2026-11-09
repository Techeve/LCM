import { describe, expect, it } from 'vitest';
import {
  collectLiterals,
  collectTranslationCalls,
  compareLanguages,
  findMissing,
  findOrphans,
  flatten,
  lintHardcodedText,
  placeholders,
} from './locale-lib.mjs';

const catalog = (tree) => flatten(tree);

describe('compareLanguages', () => {
  const de = catalog({ a: 'Hallo {name}', n: { one: 'ein Ding', other: '{count} Dinge' } });
  const en = catalog({ a: 'Hello {name}', n: { one: 'one thing', other: '{count} things' } });

  it('accepts equal key sets and placeholders', () => {
    expect(compareLanguages({ de, en })).toEqual({ errors: [], reports: [] });
  });

  it('fails for keys missing in de or en', () => {
    const { errors } = compareLanguages({ de, en: catalog({ a: 'Hello {name}' }) });
    expect(errors).toContain('key "n.one" is missing in en');
  });

  it('fails for different placeholders, in any language', () => {
    const wrong = catalog({ a: 'Hello {user}', n: { one: 'x', other: '{count} y' } });
    const { errors } = compareLanguages({ de, en: wrong });
    expect(errors).toHaveLength(1);
    expect(errors[0]).toContain('"a"');
  });

  it('compares placeholders per key across all plural forms', () => {
    const fr = catalog({ a: 'Salut {name}', n: { one: 'une chose', other: '{count} choses' } });
    expect(compareLanguages({ de, en, fr }).errors).toEqual([]);
  });

  it('only reports missing keys of other languages and accepts extra plural forms', () => {
    const ru = catalog({ n: { one: 'x', few: '{count} y', many: '{count} z', other: '{count} w' } });
    const result = compareLanguages({ de, en, ru });
    expect(result.errors).toEqual([]);
    expect(result.reports).toEqual(['ru: missing key "a"']);
  });

  it('requires de and en', () => {
    expect(compareLanguages({ de }).errors).toHaveLength(1);
  });
});

describe('placeholders', () => {
  it('finds named placeholders', () => {
    expect([...placeholders('{a} und {count} {a}')].sort()).toEqual(['a', 'count']);
  });
});

describe('orphans and missing keys', () => {
  const messages = catalog({
    servers: { packages: { upgrade: { label: 'L', help: 'H' } } },
    page: { used: 'x', unused: 'y', count: { one: 'a', other: 'b' } },
    errors: { http: { generic: 'g' } },
  });

  it('finds literal, suffix-prefix, plural and whitelisted references', () => {
    const literals = collectLiterals(
      `t('page.used'); const k = { help: "servers.packages.upgrade" }; t('page.count', {count})`,
    );
    expect(findOrphans(messages, literals, ['errors.*'])).toEqual(['page.unused']);
  });

  it('collects t() and tryT() calls and reports calls without text', () => {
    const calls = collectTranslationCalls("t('page.used'); i18n.tryT('nope.here'); t(`dyn.${x}`); t('page.count')");
    expect(calls).toEqual(['page.used', 'nope.here', 'page.count']);
    expect(findMissing(messages, calls)).toEqual(['nope.here']);
  });
});

describe('lintHardcodedText', () => {
  const lint = (template, allowed = []) => lintHardcodedText(`const x = ${template};`, allowed).map((f) => f.text);

  it('flags text nodes with letters', () => {
    expect(lint('html`<button>Speichern</button>`')).toEqual(['Speichern']);
    expect(lint('html`<p>Hallo ${name}</p>`')).toEqual(['Hallo …']);
  });

  it('accepts expressions, symbols, entities and comments', () => {
    expect(lint('html`<button>${t("a.b")}</button><span>·</span><i>&times;</i> 42 <!-- Kommentar -->`')).toEqual([]);
  });

  it('flags literal text attributes only', () => {
    expect(lint('html`<input placeholder="Suchen" aria-label=${t("a.b")} class="wide" />`')).toEqual(['placeholder="Suchen"']);
    expect(lint('html`<lcm-icon title="Hilfe"></lcm-icon>`')).toEqual(['title="Hilfe"']);
  });

  it('finds templates nested in expressions and reports the line', () => {
    const source = 'const a = 1;\nhtml`<ul>${items.map((i) => html`<li>Eintrag</li>`)}</ul>`;';
    expect(lintHardcodedText(source)).toEqual([{ line: 2, text: 'Eintrag' }]);
  });

  it('ignores css templates, styles and plain strings', () => {
    expect(lint('css`.a { color: red; }`')).toEqual([]);
    expect(lint('html`<style>.a { color: red }</style><p>${x}</p>`')).toEqual([]);
    expect(lintHardcodedText("const s = 'Speichern';")).toEqual([]);
  });

  it('allows product names', () => {
    expect(lint('html`<span>LCM</span>`', ['LCM'])).toEqual([]);
    expect(lint('html`<span>LCM Server</span>`', ['LCM'])).toEqual(['LCM Server']);
  });

  it('copes with braces and quotes inside expressions', () => {
    expect(lint('html`<p>${cond ? "}" : `{${x}}`}</p><b>Fehler</b>`')).toEqual(['Fehler']);
  });
});
