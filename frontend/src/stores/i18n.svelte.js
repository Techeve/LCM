// Sprach-/Übersetzungs-Store der App (rune-basiert, wie der theme-Store).
// i18n.t('area.key', { param: wert }) liefert den übersetzten Text
// in der aktuellen Sprache; fehlt ein Schlüssel, greift Deutsch als Fallback.
// Die Wahl wird in localStorage gemerkt und setzt zugleich <html lang>.
//
// Reaktivität: t() liest die aktuelle Sprache (ein $state) - wird t() in einer
// Komponente aufgerufen, rendert Svelte bei Sprachwechsel automatisch neu.
import de from '../locales/de.js';
import en from '../locales/en.js';

const STORAGE_KEY = 'lcm.locale';
const CATALOGS = { de, en };
export const LOCALES = ['de', 'en'];

function sanitize(value) {
  return LOCALES.includes(value) ? value : 'de';
}

// Verschachtelten Schlüssel ("a.b.c") in einem Katalog nachschlagen.
function dig(catalog, key) {
  return key.split('.').reduce((o, k) => (o == null ? undefined : o[k]), catalog);
}

function createI18n() {
  let locale = $state(sanitize(localStorage.getItem(STORAGE_KEY)));

  function apply() {
    document.documentElement.setAttribute('lang', locale);
  }
  apply();

  return {
    get locale() {
      return locale;
    },
    set(value) {
      locale = sanitize(value);
      localStorage.setItem(STORAGE_KEY, locale);
      apply();
    },
    /**
     * Wählt aus einem Datensatz das Textfeld der aktuellen Sprache.
     *
     * Für Inhalte, die in der DATENBANK stehen statt im Sprachkatalog: die
     * mitgelieferten Regelbausteine und Anwendungen tragen Name und
     * Beschreibung je einmal deutsch und einmal englisch. Fehlt die englische
     * Fassung - bei selbst angelegten Einträgen der Regelfall -, bleibt es bei
     * der deutschen: ein deutscher Name ist besser als ein leeres Feld.
     */
    field(record, name) {
      if (!record) return '';
      const english = record[`${name}_en`];
      return locale === 'en' && english ? english : (record[name] ?? '');
    },
    /**
     * Übersetzt einen Schlüssel; {param}-Platzhalter werden ersetzt.
     *
     * Steht im Katalog statt eines Textes ein Objekt mit one/other, wählt
     * params.count die Form - so bleiben Ein- und Mehrzahl im Katalog und
     * nicht in der Komponente (Status-Befunde vom Server nutzen das).
     */
    t(key, params) {
      let text = dig(CATALOGS[locale], key);
      if (text == null) text = dig(CATALOGS.de, key); // Fallback Deutsch
      if (text == null) return key; // Schlüssel unbekannt → sichtbar machen
      if (text && typeof text === 'object' && !Array.isArray(text)) {
        text = Number(params?.count) === 1 ? text.one : text.other;
      }
      if (params && typeof text === 'string') {
        for (const [k, v] of Object.entries(params)) {
          text = text.replaceAll(`{${k}}`, v);
        }
      }
      return text;
    },
  };
}

export const i18n = createI18n();
