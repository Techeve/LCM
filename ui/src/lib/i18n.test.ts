import { beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from './api';
import { errorText, flatten, I18n, i18n, interpolate, pickLanguage } from './i18n';

const catalogs = {
  de: {
    greeting: 'Hallo {name}',
    servers: { one: '{count} Server', other: '{count} Server' },
    only: { de: 'nur deutsch' },
    errors: { http: { generic: 'Fehler {status}' }, 'server.not_found': 'Server {server} fehlt' },
  },
  en: {
    greeting: 'Hello {name}',
    servers: { one: '{count} server', other: '{count} servers' },
    only: { en: 'only english' },
    shared: 'shared english',
    errors: { http: { generic: 'Error {status}' } },
  },
  ru: {
    greeting: 'Привет, {name}',
    files: { one: '{count} файл', few: '{count} файла', many: '{count} файлов', other: '{count} файла' },
  },
};

function create() {
  const loads: string[] = [];
  const loaders = Object.fromEntries(
    Object.entries(catalogs).map(([language, tree]) => [
      `../locales/${language}.json`,
      () => {
        loads.push(language);
        return Promise.resolve(tree);
      },
    ]),
  );
  return { instance: new I18n(loaders), loads };
}

beforeEach(() => localStorage.clear());

describe('flatten, interpolate, pickLanguage', () => {
  it('flattens nested trees to dotted keys', () => {
    expect([...flatten({ a: { b: 'x', c: { d: 'y' } } })]).toEqual([
      ['a.b', 'x'],
      ['a.c.d', 'y'],
    ]);
  });

  it('replaces named placeholders and keeps unknown ones', () => {
    expect(interpolate('{a} und {b}', { a: 1 })).toBe('1 und {b}');
  });

  it('picks exact, then base language, then english', () => {
    expect(pickLanguage(['xx', 'de-AT'], ['de', 'en'])).toBe('de');
    expect(pickLanguage(['pt-BR'], ['pt-BR', 'pt'])).toBe('pt-BR');
    expect(pickLanguage([null, 'ja'], ['de', 'en'])).toBe('en');
  });
});

describe('I18n', () => {
  it('lists languages from the loaders without loading them', () => {
    const { instance, loads } = create();
    expect(instance.languages).toEqual(['de', 'en', 'ru']);
    expect(loads).toEqual([]);
  });

  it('loads only the active language', async () => {
    const { instance, loads } = create();
    await instance.init(['de-DE']);
    expect(instance.t('greeting', { name: 'Tony' })).toBe('Hallo Tony');
    expect(loads).toEqual(['de']);
  });

  it('prefers the stored language over the browser language', async () => {
    localStorage.setItem('lcm.lang', 'en');
    const { instance } = create();
    await instance.init(['de']);
    expect(instance.language).toBe('en');
  });

  it('sets lang and dir on <html> and remembers the choice', async () => {
    const { instance } = create();
    await instance.setLanguage('ru');
    expect(document.documentElement.lang).toBe('ru');
    expect(document.documentElement.dir).toBe('ltr');
    expect(localStorage.getItem('lcm.lang')).toBe('ru');
  });

  it('rejects unknown languages', async () => {
    const { instance } = create();
    await expect(instance.setLanguage('xx')).rejects.toThrow(RangeError);
  });

  it('selects plural forms with Intl.PluralRules, also with more than two forms', async () => {
    const { instance } = create();
    await instance.setLanguage('en');
    expect(instance.t('servers', { count: 1 })).toBe('1 server');
    expect(instance.t('servers', { count: 2 })).toBe('2 servers');
    await instance.setLanguage('ru');
    expect(instance.t('files', { count: 1 })).toBe('1 файл');
    expect(instance.t('files', { count: 3 })).toBe('3 файла');
    expect(instance.t('files', { count: 5 })).toBe('5 файлов');
  });

  it('falls back to english and german only when a key is missing, and re-renders', async () => {
    const { instance, loads } = create();
    await instance.setLanguage('ru');
    expect(loads).toEqual(['ru']);
    const changed = vi.fn();
    instance.onChange(changed);

    expect(instance.t('shared')).toBe('shared');
    await vi.waitFor(() => expect(changed).toHaveBeenCalled());
    expect(loads).toEqual(['ru', 'en', 'de']);
    expect(instance.t('shared')).toBe('shared english');
    expect(instance.t('only.de')).toBe('nur deutsch');
    expect(instance.t('missing.everywhere')).toBe('missing.everywhere');
  });

  it('uses the plural rules of the language the text came from when falling back', async () => {
    const { instance } = create();
    await instance.setLanguage('ru');
    instance.t('servers', { count: 2 });
    await vi.waitFor(() => expect(instance.t('servers', { count: 2 })).toBe('2 servers'));
  });

  it('keeps the last requested language when changes overlap', async () => {
    const { instance } = create();
    await Promise.all([instance.setLanguage('de'), instance.setLanguage('en')]);
    expect(instance.language).toBe('en');
    expect(localStorage.getItem('lcm.lang')).toBe('en');
  });
});

describe('errorText', () => {
  beforeEach(async () => {
    await i18n.setLanguage('de');
  });

  it('translates errors.<code> with the params of the API', async () => {
    // Der Schlüssel `errors.network` steht in der echten de.json.
    expect(errorText(new ApiError(0, 'network', {}, 'network error'))).toBe(i18n.t('errors.network'));
  });

  it('falls back to the generic text with the status for unknown codes', () => {
    const text = errorText(new ApiError(500, 'area.unknown', {}, 'boom'));
    expect(text).toBe(i18n.t('errors.http.generic', { status: 500 }));
    expect(text).toContain('500');
  });

  it('uses the generic text when the API sent no code', () => {
    expect(errorText(new ApiError(502, '', {}, 'bad gateway'))).toContain('502');
  });

  it('names the violated password rules', () => {
    const text = errorText(new ApiError(422, 'password.too_weak', { problems: ['common_password', 'contains_identity'] }, 'weak'));
    expect(text).toContain(i18n.t('password.problem.common_password'));
    expect(text).toContain(i18n.t('password.problem.contains_identity'));
  });

  it('shows a weak password without rules as plain text, without a placeholder', () => {
    const text = errorText(new ApiError(422, 'password.too_weak', {}, 'weak'));
    expect(text).toBe(i18n.t('errors.password.too_weak', { reasons: '' }).trim());
    expect(text).not.toContain('{');
  });
});
